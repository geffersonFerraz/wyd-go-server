package protocol

import (
	"bufio"
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

	// Clock, if set, supplies the header TimeStamp (TMSRV stamps every
	// packet with one server-wide CurrentTime that clients sync to).
	Clock func() uint32

	// MaxSize overrides MaxMessageSize for incoming packets (0 = default).
	MaxSize int
}

// NewConn wraps an accepted connection: the peer's first 4 bytes must be
// the INITCODE hello.
func NewConn(nc net.Conn) *Conn {
	return &Conn{nc: nc, r: bufio.NewReaderSize(nc, 64*1024), started: time.Now()}
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
		var hello [4]byte
		if _, err := io.ReadFull(c.r, hello[:]); err != nil {
			return Packet{}, err
		}
		if code := binary.LittleEndian.Uint32(hello[:]); code != InitCode {
			return Packet{}, fmt.Errorf("%w: 0x%08X", ErrBadInitCode, code)
		}
		c.inited = true
	}

	sizeBytes, err := c.r.Peek(2)
	if err != nil {
		return Packet{}, err
	}
	size := int(binary.LittleEndian.Uint16(sizeBytes))
	limit := MaxMessageSize
	if c.MaxSize > 0 {
		limit = c.MaxSize
	}
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

// SendRaw encrypts and writes an already-marshaled packet (header included).
// pkt is modified in place.
func (c *Conn) SendRaw(pkt []byte) error {
	if len(pkt) < HeaderSize || len(pkt) > 0xFFFF {
		return fmt.Errorf("%w: %d", ErrBadSize, len(pkt))
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
