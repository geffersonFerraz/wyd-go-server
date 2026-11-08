package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Server is the DBSRV. One goroutine reads each TMSRV connection; packets
// are then handled on a per-client "lane" goroutine, so one player's slow
// request (password hashing, a database round trip) never stalls the rest
// of the channel while each player's requests stay in order.
type Server struct {
	cfg  Config
	st   *store.Store
	log  *slog.Logger
	base [4]wire.Mob // npc/TK, FM, BM, HT: new character templates (pBaseSet)

	sess     *sessions
	sapphire atomic.Int32
	world    *world

	mu       sync.Mutex
	channels [MaxChannels]*channel
}

func New(cfg Config, st *store.Store, base [4]wire.Mob, sapphire int, log *slog.Logger) *Server {
	if cfg.ChangeServerTokenTTL == 0 {
		cfg.ChangeServerTokenTTL = defaultTokenTTL
	}
	if cfg.DeliveryInterval == 0 {
		cfg.DeliveryInterval = time.Second
	}
	s := &Server{cfg: cfg, st: st, log: log, base: base, sess: newSessions(),
		world: &world{guilds: map[int]*store.Guild{}, ally: map[int]int{}, war: map[int]int{},
			kefra: store.KefraState{FirstChannel: maxServerGroup}}}
	s.sapphire.Store(int32(sapphire))
	return s
}

// Serve accepts TMSRV connections until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	// Nobody can be online before a TMSRV connects; drop rows a crash left.
	if err := s.st.ClearOnline(ctx, -1); err != nil {
		return fmt.Errorf("clear online accounts: %w", err)
	}
	s.log.Info("listening", "addr", ln.Addr().String(), "channels", s.cfg.ChannelIPs)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runJobs(ctx)
	}()
	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.closeAll()
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			s.log.Warn("accept", "err", err)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handleConn(nc)
		}()
	}
}

func (s *Server) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.channels {
		if ch != nil {
			ch.conn.Close()
		}
	}
}

// assignChannel picks the first free channel configured for this source IP
// (C++ WSA_ACCEPT, matched against serverlist.txt).
func (s *Server) assignChannel(ip string, ch *channel) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	known := false
	for i, want := range s.cfg.ChannelIPs {
		if want != ip {
			continue
		}
		known = true
		if s.channels[i] == nil {
			s.channels[i] = ch
			return i, nil
		}
	}
	if !known {
		return -1, fmt.Errorf("%s is not a channel in serverlist", ip)
	}
	return -1, fmt.Errorf("every channel for %s is already connected", ip)
}

func (s *Server) releaseChannel(i int, ch *channel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.channels[i] == ch {
		s.channels[i] = nil
	}
}

func (s *Server) channel(i int) *channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.channels) {
		return nil
	}
	return s.channels[i]
}

func (s *Server) handleConn(nc net.Conn) {
	ip, _, _ := net.SplitHostPort(nc.RemoteAddr().String())
	ch := &channel{srv: s, conn: protocol.NewConn(nc), lanes: map[uint16]chan protocol.Packet{}}
	idx, err := s.assignChannel(ip, ch)
	if err != nil {
		s.log.Warn("rejected connection", "remote", nc.RemoteAddr().String(), "err", err)
		nc.Close()
		return
	}
	ch.idx = idx
	ch.log = s.log.With("channel", idx)
	ch.log.Info("TMSRV connected", "remote", nc.RemoteAddr().String())

	// C++: SendDBSignalParm2(userId, 0, 0x423, 1, Sapphire). That helper
	// sent sizeof(MSG_STANDARDPARM3), so the packet is 24 bytes.
	ch.send(&wire.Parm3{Header: wire.NewHeader(wire.OpServerHello, 0), Parm1: 1, Parm2: s.sapphire.Load()})

	ch.readLoop()

	// Clear this channel's sessions before freeing the slot, so a TMSRV that
	// reconnects into it cannot log someone in whose row we then delete.
	n := s.sess.dropChannel(idx)
	cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.st.ClearOnline(cctx, idx); err != nil {
		ch.log.Error("clear online accounts", "err", err)
	}
	cancel()
	s.releaseChannel(idx, ch)
	ch.log.Info("TMSRV disconnected", "sessions_dropped", n)
}

// channel is one connected TMSRV.
type channel struct {
	srv   *Server
	idx   int
	conn  *protocol.Conn
	log   *slog.Logger
	staff atomic.Int32

	lanes map[uint16]chan protocol.Packet // only touched by readLoop
	wg    sync.WaitGroup
}

func (ch *channel) readLoop() {
	defer func() {
		for _, l := range ch.lanes {
			close(l)
		}
		ch.wg.Wait()
		ch.conn.Close()
	}()
	for {
		pkt, err := ch.conn.ReadPacket()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				ch.log.Info("read", "err", err)
			}
			return
		}
		lane := ch.lanes[pkt.Header.ClientID]
		if lane == nil {
			lane = make(chan protocol.Packet, 64)
			ch.lanes[pkt.Header.ClientID] = lane
			ch.wg.Add(1)
			go ch.runLane(lane)
		}
		lane <- pkt
	}
}

func (ch *channel) runLane(lane chan protocol.Packet) {
	defer ch.wg.Done()
	for pkt := range lane {
		ch.handleSafe(pkt)
	}
}

func (ch *channel) handleSafe(pkt protocol.Packet) {
	defer func() {
		if r := recover(); r != nil {
			ch.log.Error("handler panic", "packet", fmt.Sprintf("0x%X", pkt.Header.PacketID),
				"client", pkt.Header.ClientID, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	ch.srv.handle(ch, pkt)
}

// send marshals a wire struct (header first) and writes it.
func (ch *channel) send(v any) {
	b, err := wire.Marshal(v)
	if err != nil {
		ch.log.Error("marshal", "type", fmt.Sprintf("%T", v), "err", err)
		return
	}
	if err := ch.conn.SendRaw(b); err != nil {
		ch.log.Warn("send", "err", err)
	}
}

// signal sends a header-only packet (SendDBSignal).
func (ch *channel) signal(client uint16, op uint16) {
	ch.send(&wire.Signal{Header: wire.NewHeader(op, client)})
}

// Online reports how many accounts are logged in on each channel.
// ChannelConnected reports whether channel i (index) has its TMSRV linked.
func (s *Server) ChannelConnected(i int) bool { return s.channel(i) != nil }

func (s *Server) Online() []int {
	out := make([]int, len(s.cfg.ChannelIPs))
	for i := range out {
		out[i] = s.sess.count(i)
	}
	return out
}
