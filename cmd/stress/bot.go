package main

import (
	"context"
	"crypto/ecdh"
	crand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// botConfig is what every bot shares.
type botConfig struct {
	addr         string
	serverKey    *ecdh.PublicKey
	password     string
	numeric      string
	slot         int
	class        int // -1 = random
	move         time.Duration
	wander       int
	chat         time.Duration
	stepTimeout  time.Duration
	reconnect    bool
	reconnectGap time.Duration
}

type bot struct {
	cfg   *botConfig
	st    *stats
	login string
	mac   [8]byte

	// Server clock: TMSRV stamps every packet with its CurrentTime and
	// checks the movement timestamps against it.
	serverTS atomic.Uint32
	localAt  atomic.Int64

	idx     uint16 // client index in the world
	pos     atomic.Uint32
	spawn   wire.Position
	lastMsg atomic.Pointer[string]
	inWorld bool
	hasChar bool
}

// errRetry ends a session that should be tried again at once (the server
// kicked a previous session of the account).
var errRetry = errors.New("retry")

func newBot(cfg *botConfig, st *stats, login string) *bot {
	b := &bot{cfg: cfg, st: st, login: login}
	_, _ = crand.Read(b.mac[:])
	return b
}

func (b *bot) clock() uint32 {
	at := b.localAt.Load()
	if at == 0 {
		return 0
	}
	return b.serverTS.Load() + uint32(time.Since(time.Unix(0, at)).Milliseconds())
}

// live runs sessions until ctx ends (or, without -reconnect, until the
// first one ends).
func (b *bot) live(ctx context.Context) {
	for ctx.Err() == nil {
		err := b.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errRetry) {
			b.st.retries.Add(1)
			sleep(ctx, 2*time.Second)
			continue
		}
		if !b.cfg.reconnect {
			return
		}
		sleep(ctx, b.cfg.reconnectGap+time.Duration(rand.Int64N(int64(time.Second))))
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// session is one connection, from dial to disconnect.
func (b *bot) session(ctx context.Context) (err error) {
	b.inWorld = false
	b.localAt.Store(0)
	b.lastMsg.Store(nil)
	b.st.connecting.Add(1)
	connecting := true
	defer func() {
		if connecting {
			b.st.connecting.Add(-1)
		}
		if err != nil && !errors.Is(err, errRetry) && ctx.Err() == nil {
			b.st.fail(b.reason(err))
		}
	}()

	t0 := time.Now()
	d := net.Dialer{Timeout: b.cfg.stepTimeout}
	nc, err := d.DialContext(ctx, "tcp", b.cfg.addr)
	if err != nil {
		return fmt.Errorf("conexão: %w", err)
	}
	defer nc.Close()
	stop := context.AfterFunc(ctx, func() { nc.Close() })
	defer stop()

	_ = nc.SetDeadline(time.Now().Add(b.cfg.stepTimeout))
	pc := protocol.NewDialedSecureConn(nc)
	pc.Clock = b.clock
	if err := pc.ClientHandshake(b.cfg.serverKey); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	b.st.handshake.add(time.Since(t0))

	login := wire.AccountLogin{Header: wire.NewHeader(wire.OpCliLogin, 0), Unknow84: 1, Mac: b.mac}
	wire.PutCString(login.Login[:], b.login)
	wire.PutCString(login.Password[:], b.cfg.password)
	tLogin := time.Now()
	if err := b.send(pc, &login); err != nil {
		return err
	}

	var actorDone chan struct{}
	defer func() {
		if actorDone != nil {
			<-actorDone
		}
	}()
	actorCtx, cancelActor := context.WithCancel(ctx)
	defer cancelActor()

	for {
		pkt, err := pc.ReadPacket()
		if err != nil {
			if b.inWorld {
				b.st.inWorld.Add(-1)
				b.inWorld = false
				if ctx.Err() == nil {
					b.st.dropped.Add(1)
				}
			}
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return errors.New("servidor fechou a conexão")
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return errors.New("timeout")
			}
			return err
		}
		b.st.pktIn.Add(1)
		b.st.bytesIn.Add(uint64(len(pkt.Raw)))
		if ts := pkt.Header.TimeStamp; ts != 0 {
			b.serverTS.Store(ts)
			b.localAt.Store(time.Now().UnixNano())
		}

		switch pkt.Header.PacketID {
		case wire.OpSrvMessage:
			var m wire.Message128
			if wire.Unmarshal(pkt.Raw, &m) == nil {
				s := strings.TrimSpace(wire.GameString(m.Msg[:]))
				b.lastMsg.Store(&s)
			}

		case wire.OpSrvCharList:
			b.st.login.add(time.Since(tLogin))
			var p wire.CNFAccountLogin
			if wire.Unmarshal(pkt.Raw, &p) != nil {
				return errors.New("lista de personagens inválida")
			}
			b.hasChar = p.CharList.Name[b.cfg.slot][0] != 0
			num := wire.NumericPassword{Header: wire.NewHeader(wire.OpCliNumericPass, 0)}
			wire.PutCString(num.Num[:], b.cfg.numeric)
			if err := b.send(pc, &num); err != nil {
				return err
			}

		case wire.OpCliNumericPass: // accepted
			if b.hasChar {
				err = b.enter(pc)
			} else {
				err = b.create(pc)
			}
			if err != nil {
				return err
			}

		case wire.OpSrvNumericBad:
			return errors.New("senha numérica incorreta")

		case wire.OpSrvCharListNew:
			b.st.created.Add(1)
			if err := b.enter(pc); err != nil {
				return err
			}

		case wire.OpSrvCreateFail:
			return errors.New("criação de personagem recusada (nome em uso?)")

		case wire.OpSrvLoginFail:
			return errors.New("login recusado")

		case wire.OpSrvKickedOther:
			return errRetry

		case wire.OpSrvEnterWorld:
			var p wire.CNFCharacterLogin
			if wire.Unmarshal(pkt.Raw, &p) != nil {
				return errors.New("entrada no mundo inválida")
			}
			b.idx = uint16(p.ClientIndex)
			b.spawn = p.WorldPos
			b.setPos(p.WorldPos)
			b.st.enter.add(time.Since(t0))
			b.st.inWorld.Add(1)
			b.st.connecting.Add(-1)
			connecting = false
			b.inWorld = true
			_ = nc.SetDeadline(time.Time{})
			actorDone = make(chan struct{})
			go func() {
				defer close(actorDone)
				b.act(actorCtx, pc)
			}()

		case wire.OpCliMove, wire.OpCliMoveStop, wire.OpCliMoveIllus:
			// The server corrects our own position (occupied cell, walked
			// too far) with a movement addressed to us.
			if b.inWorld && pkt.Header.ClientID == b.idx {
				var a wire.Action
				if wire.Unmarshal(pkt.Raw, &a) == nil && a.Destiny.X != 0 {
					b.setPos(a.Destiny)
				}
			}
		}
	}
}

func (b *bot) create(pc *protocol.Conn) error {
	class := b.cfg.class
	if class < 0 {
		class = rand.IntN(4)
	}
	p := wire.NewCharacter{Header: wire.NewHeader(wire.OpCliCreateChar, 0), SlotID: uint32(b.cfg.slot), ClassID: uint32(class)}
	wire.PutCString(p.Nick[:], b.login)
	return b.send(pc, &p)
}

func (b *bot) enter(pc *protocol.Conn) error {
	return b.send(pc, &wire.CharacterLogin{Header: wire.NewHeader(wire.OpCliEnterWorld, 0), CharIndex: int32(b.cfg.slot)})
}

func (b *bot) send(pc *protocol.Conn, v any) error {
	raw, err := wire.Marshal(v)
	if err != nil {
		return err
	}
	if err := pc.SendRaw(raw); err != nil {
		return err
	}
	b.st.pktOut.Add(1)
	return nil
}

func (b *bot) setPos(p wire.Position) { b.pos.Store(uint32(p.X)<<16 | uint32(p.Y)) }
func (b *bot) getPos() wire.Position {
	v := b.pos.Load()
	return wire.Position{X: uint16(v >> 16), Y: uint16(v)}
}

// act is the bot's life in the world: walk around the spawn point, talk
// now and then, and keep the connection alive.
func (b *bot) act(ctx context.Context, pc *protocol.Conn) {
	keepAlive := time.NewTicker(time.Minute)
	defer keepAlive.Stop()
	var moveC, chatC <-chan time.Time
	if b.cfg.move > 0 {
		t := time.NewTicker(jitter(b.cfg.move))
		defer t.Stop()
		moveC = t.C
	}
	if b.cfg.chat > 0 {
		t := time.NewTicker(jitter(b.cfg.chat))
		defer t.Stop()
		chatC = t.C
	}
	for {
		var err error
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			err = b.send(pc, &wire.Signal{Header: wire.NewHeader(wire.OpCliKeepAlive, 0)})
		case <-moveC:
			err = b.step(pc)
		case <-chatC:
			c := wire.Chat{Header: wire.NewHeader(wire.OpCliChat, 0)}
			wire.PutCString(c.Text[:], fmt.Sprintf("stress %d", rand.IntN(10000)))
			err = b.send(pc, &c)
		}
		if err != nil {
			return
		}
	}
}

// jitter spreads the bots' timers so they do not all fire together.
func jitter(d time.Duration) time.Duration {
	return d + time.Duration(rand.Int64N(int64(d)/2+1)) - d/4
}

// step walks 1 to 3 cells, staying within -wander of the spawn point.
func (b *bot) step(pc *protocol.Conn) error {
	cur := b.getPos()
	if cur.X == 0 {
		return nil
	}
	clamp := func(v, center int) uint16 {
		return uint16(min(max(v, center-b.cfg.wander), center+b.cfg.wander))
	}
	dst := wire.Position{
		X: clamp(int(cur.X)+rand.IntN(7)-3, int(b.spawn.X)),
		Y: clamp(int(cur.Y)+rand.IntN(7)-3, int(b.spawn.Y)),
	}
	if dst == cur {
		return nil
	}
	a := wire.Action{Header: wire.NewHeader(wire.OpCliMove, 0), LastPos: cur, MoveSpeed: 2, Destiny: dst}
	a.Command[0] = byte('0' + direction(int(dst.X)-int(cur.X), int(dst.Y)-int(cur.Y)))
	if err := b.send(pc, &a); err != nil {
		return err
	}
	b.st.moves.Add(1)
	b.setPos(dst)
	return nil
}

// direction is the numeric-keypad digit of a step (the route format the
// client sends: 8 north, 2 south, ...). Y grows to the south on the map.
func direction(dx, dy int) int {
	sx, sy := sign(dx), sign(dy)
	return [3][3]int{{7, 8, 9}, {4, 5, 6}, {1, 2, 3}}[sy+1][sx+1]
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

// reason turns the error of a session into a failure category, with the
// last message the server showed, if any.
func (b *bot) reason(err error) string {
	r := err.Error()
	if msg := b.lastMsg.Load(); msg != nil && *msg != "" {
		r += ": " + *msg
	}
	return r
}
