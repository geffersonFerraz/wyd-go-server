package protocol

import (
	"bufio"
	"crypto/ecdh"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"sync"
	"time"
)

var (
	ErrBadInitCode = errors.New("protocol: bad init code")
	ErrBadSize     = errors.New("protocol: packet size out of range")
)

// Packet is one decrypted message. Raw holds the whole packet, header included,
// so it can be decoded straight into a wire struct.
type Packet struct {
	Header     Header
	Raw        []byte
	ChecksumOK bool
}

// Conn wraps one TMSRV connection. Reads must come from a single goroutine;
// Send is safe to call concurrently (handlers broadcast to every channel).
type Conn struct {
	nc      net.Conn
	r       *bufio.Reader
	wmu     sync.Mutex
	started time.Time
	inited  bool

	// Secure transport (secure.go). secure says the link must be encrypted;
	// sec is nil until the handshake ends, and ready is closed when it does,
	// well or badly.
	secure    bool
	staticKey *ecdh.PrivateKey
	sec       *secureState
	ready     chan struct{}
	readyOnce sync.Once

	// Clock, if set, supplies the header TimeStamp (TMSRV stamps every
	// packet with one server-wide CurrentTime that clients sync to).
	Clock func() uint32

	// MaxSize overrides MaxMessageSize for incoming packets (0 = default).
	MaxSize int
}

// NewConn wraps an accepted connection: the peer's first 4 bytes must be
// the INITCODE hello.
func NewConn(nc net.Conn) *Conn {
	return &Conn{nc: nc, r: bufio.NewReaderSize(nc, 64*1024), started: time.Now(), ready: make(chan struct{})}
}

// NewDialedConn wraps a connection we opened (the TMSRV role): we send the
// hello with Hello() and the listener never sends one back.
func NewDialedConn(nc net.Conn) *Conn {
	c := NewConn(nc)
	c.inited = true
	return c
}

func (c *Conn) RemoteAddr() net.Addr { return c.nc.RemoteAddr() }
func (c *Conn) Close() error         { return c.nc.Close() }

// ReadPacket blocks for the next packet. The first call consumes the
// INITCODE hello (CPSock::ReadMessage, Init == 0 branch).
func (c *Conn) ReadPacket() (Packet, error) {
	if !c.inited {
		if err := c.accept(); err != nil {
			return Packet{}, err
		}
		c.inited = true
	}
	limit := MaxMessageSize
	if c.MaxSize > 0 {
		limit = c.MaxSize
	}
	if c.secure {
		if c.sec == nil {
			return Packet{}, ErrHandshake
		}
		raw, err := c.readFrame(limit)
		if err != nil {
			return Packet{}, err
		}
		return Packet{Header: ParseHeader(raw), Raw: raw, ChecksumOK: true}, nil
	}

	sizeBytes, err := c.r.Peek(2)
	if err != nil {
		return Packet{}, err
	}
	size := int(binary.LittleEndian.Uint16(sizeBytes))
	if size < HeaderSize || size > limit {
		return Packet{}, fmt.Errorf("%w: %d", ErrBadSize, size)
	}

	raw := make([]byte, size)
	if _, err := io.ReadFull(c.r, raw); err != nil {
		return Packet{}, err
	}
	ok := Decrypt(raw)
	return Packet{Header: ParseHeader(raw), Raw: raw, ChecksumOK: ok}, nil
}

// accept reads the hello of a connection we accepted: the INITCODE and, on
// the secure transport, the key exchange. A client that connects and stays
// silent is dropped when the deadline passes.
func (c *Conn) accept() error {
	if c.secure {
		defer c.finishHandshake()
		_ = c.nc.SetReadDeadline(time.Now().Add(handshakeTimeout))
		defer c.nc.SetReadDeadline(time.Time{})
	}
	var hello [4]byte
	if _, err := io.ReadFull(c.r, hello[:]); err != nil {
		return err
	}
	if code := binary.LittleEndian.Uint32(hello[:]); code != InitCode {
		return fmt.Errorf("%w: 0x%08X", ErrBadInitCode, code)
	}
	if c.secure {
		return c.serverHandshake()
	}
	return nil
}

// SendRaw encrypts and writes an already-marshaled packet (header included).
// pkt is modified in place.
func (c *Conn) SendRaw(pkt []byte) error {
	if len(pkt) < HeaderSize || len(pkt) > 0xFFFF-TransportOverhead {
		return fmt.Errorf("%w: %d", ErrBadSize, len(pkt))
	}
	if c.secure {
		return c.sendSecure(pkt)
	}
	// C++ used Rand() & 0x800000FF, i.e. a random byte.
	key := uint8(rand.UintN(256))
	ts := uint32(time.Since(c.started).Milliseconds())
	if c.Clock != nil {
		ts = c.Clock()
	}
	Encrypt(pkt, key, ts)

	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.nc.Write(pkt)
	return err
}

// Hello writes the INITCODE; used by test clients that play the TMSRV role.
func (c *Conn) Hello() error {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], InitCode)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.nc.Write(b[:])
	return err
}

// sendSecure stamps the header like Encrypt does and writes one frame. The
// Key and CheckSum fields of the old cipher travel as zero.
func (c *Conn) sendSecure(pkt []byte) error {
	<-c.ready
	ts := uint32(time.Since(c.started).Milliseconds())
	if c.Clock != nil {
		ts = c.Clock()
	}
	binary.LittleEndian.PutUint16(pkt[0:], uint16(len(pkt)))
	pkt[2], pkt[3] = 0, 0
	binary.LittleEndian.PutUint32(pkt[8:], ts)

	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.sec == nil {
		return ErrHandshake
	}
	_, err := c.nc.Write(c.sealFrame(pkt))
	return err
}
