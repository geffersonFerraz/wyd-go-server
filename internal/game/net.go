package game

import (
	"context"
	"crypto/ecdh"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// clientConn is one client socket: a reader goroutine posting packets to
// the loop and a writer goroutine draining queued batches.
type clientConn struct {
	nc   net.Conn
	pc   *protocol.Conn
	ip   string
	slot int // set by the loop on accept

	out       chan [][]byte
	closeOnce sync.Once
}

func newClientConn(nc net.Conn, clock func() uint32, key *ecdh.PrivateKey) *clientConn {
	pc := protocol.NewConn(nc)
	if key != nil {
		pc = protocol.NewSecureConn(nc, key)
	}
	pc.Clock = clock
	pc.MaxSize = protocol.MaxClientMessageSize
	ip, _, _ := net.SplitHostPort(nc.RemoteAddr().String())
	return &clientConn{nc: nc, pc: pc, ip: ip, out: make(chan [][]byte, 256)}
}

// start launches the reader and writer once the loop assigned a slot.
func (c *clientConn) start(g *Game) {
	go func() {
		for batch := range c.out {
			for _, b := range batch {
				if err := c.pc.SendRaw(b); err != nil {
					c.nc.Close()
					break
				}
			}
		}
		c.nc.Close()
	}()
	go func() {
		for {
			pkt, err := c.pc.ReadPacket()
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					g.log.Debug("client read", "slot", c.slot, "err", err)
				}
				g.post(event{kind: evClientClosed, conn: c})
				return
			}
			g.post(event{kind: evClientPacket, conn: c, pkt: pkt})
		}
	}()
}

// enqueue hands a batch to the writer; false when the client is too slow.
func (c *clientConn) enqueue(batch [][]byte) bool {
	select {
	case c.out <- batch:
		return true
	default:
		return false
	}
}

// close stops the writer after it drains; the reader then fails and exits.
func (c *clientConn) close() {
	c.closeOnce.Do(func() { close(c.out) })
}

// ServeClients accepts game clients until ctx ends.
func (g *Game) ServeClients(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			continue
		}
		g.post(event{kind: evAccept, conn: newClientConn(nc, g.now, g.cfg.TransportKey)})
	}
}

// dbLink is the connection to DBSRV (ConnectDB + Receive/ReadMessageDB).
type dbLink struct {
	g   *Game
	mu  sync.Mutex
	pc  *protocol.Conn
	out chan []byte
}

// ConnectDB dials DBSRV from the configured local IP (serverip.txt) and
// keeps reconnecting; the C++ server connected once and never retried.
func (g *Game) ConnectDB(ctx context.Context) {
	l := &dbLink{g: g, out: make(chan []byte, 8192)}
	g.db = l
	go l.run(ctx)
}

func (l *dbLink) run(ctx context.Context) {
	cfg := l.g.cfg
	d := net.Dialer{Timeout: 5 * time.Second}
	if ip := net.ParseIP(cfg.ListenIP); ip != nil {
		// DBSRV identifies the channel by our source IP (serverlist.txt).
		d.LocalAddr = &net.TCPAddr{IP: ip}
	}
	addr := net.JoinHostPort(cfg.DBIP, fmt.Sprint(cfg.DBPort))
	for ctx.Err() == nil {
		nc, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			l.g.log.Warn("DBSRV dial failed, retrying", "addr", addr, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		pc := protocol.NewDialedConn(nc)
		pc.Clock = l.g.now
		if err := pc.Hello(); err != nil {
			nc.Close()
			continue
		}
		l.mu.Lock()
		l.pc = pc
		l.mu.Unlock()
		l.g.post(event{kind: evDBUp})

		done := make(chan struct{})
		go func() { // writer
			for {
				select {
				case b := <-l.out:
					if err := pc.SendRaw(b); err != nil {
						nc.Close()
						return
					}
				case <-done:
					return
				}
			}
		}()
		for {
			pkt, err := pc.ReadPacket()
			if err != nil {
				break
			}
			l.g.post(event{kind: evDBPacket, pkt: pkt})
		}
		close(done)
		nc.Close()
		l.mu.Lock()
		l.pc = nil
		l.mu.Unlock()
		l.g.post(event{kind: evDBDown})
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// send queues a packet to DBSRV (AddMessageDB).
func (l *dbLink) send(v any) {
	b, err := wire.Marshal(v)
	if err != nil {
		l.g.log.Error("marshal db packet", "err", err)
		return
	}
	select {
	case l.out <- b:
	default:
		l.g.log.Error("DBSRV send queue full, packet dropped", "id", fmt.Sprintf("0x%X", b[4:6]))
	}
}
