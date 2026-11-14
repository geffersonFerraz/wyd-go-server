package protocol

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
)

// Secure transport of the client link. The KeyTable cipher of the original
// game is a fixed table every client has, so anyone on the network path could
// read the login. Here each connection gets its own keys:
//
//	client -> server   INITCODE (4) | client ephemeral X25519 key (32)
//	server -> client   server ephemeral X25519 key (32) | confirmation tag (16)
//
//	ee  = X25519(client ephemeral, server ephemeral)
//	es  = X25519(client ephemeral, server static)
//	okm = BLAKE2b-512(key = ee | es, msg = label | client eph | server eph | server static)
//	client->server key = okm[0:32], server->client key = okm[32:64]
//
// The client ships with the server's static public key (serverkey.txt), so
// only the real server can derive the keys: es needs the static private key.
// The confirmation tag is ChaCha20-Poly1305 over an empty message with nonce
// 0, which lets the client refuse a wrong server before sending the login.
//
// After the handshake every packet travels as one frame:
//
//	uint16 W (frame length, little endian) | ciphertext | tag (16)
//
// The ciphertext is the plain packet from byte 2 on (the plain Size field is
// W-16 and is not sent), sealed with ChaCha20-Poly1305 (RFC 8439). W is the
// additional data. The nonce is a per-direction counter starting at 1, so a
// replayed, reordered or dropped frame fails authentication. The DBSRV link
// keeps the old cipher: it never leaves the server's private network.
//
// The same scheme is implemented in the client's CPSock.cpp (Monocypher).
const (
	// TransportKeySize is the size of the X25519 keys.
	TransportKeySize = 32
	// TransportOverhead is what a frame adds to the plain packet.
	TransportOverhead = chacha20poly1305.Overhead

	transportLabel = "WYD-transport-v1"
)

// handshakeTimeout is how long an accepted client has to finish the hello.
var handshakeTimeout = 10 * time.Second

var (
	ErrHandshake = errors.New("protocol: secure handshake failed")
	ErrBadFrame  = errors.New("protocol: frame failed authentication")
)

// secureState is the keys and counters of one connection.
type secureState struct {
	send, recv   cipher.AEAD
	sendN, recvN uint64
}

func transportNonce(n uint64) []byte {
	nonce := make([]byte, chacha20poly1305.NonceSize)
	binary.LittleEndian.PutUint64(nonce, n)
	return nonce
}

// deriveKeys returns the client->server and server->client keys.
func deriveKeys(ee, es, clientEph, serverEph, serverStatic []byte) (c2s, s2c []byte, err error) {
	h, err := blake2b.New512(append(append([]byte{}, ee...), es...))
	if err != nil {
		return nil, nil, err
	}
	h.Write([]byte(transportLabel))
	h.Write(clientEph)
	h.Write(serverEph)
	h.Write(serverStatic)
	okm := h.Sum(nil)
	return okm[:32], okm[32:], nil
}

func newSecureState(sendKey, recvKey []byte) (*secureState, error) {
	send, err := chacha20poly1305.New(sendKey)
	if err != nil {
		return nil, err
	}
	recv, err := chacha20poly1305.New(recvKey)
	if err != nil {
		return nil, err
	}
	return &secureState{send: send, recv: recv}, nil
}

// confirmation is the tag that proves the server derived the keys.
func confirmation(s2c cipher.AEAD) []byte {
	return s2c.Seal(nil, transportNonce(0), nil, []byte(transportLabel))
}

// NewSecureConn wraps an accepted client connection that must use the
// secure transport. The handshake runs inside the first ReadPacket, with a
// deadline; SendRaw waits for it.
func NewSecureConn(nc net.Conn, key *ecdh.PrivateKey) *Conn {
	c := NewConn(nc)
	c.staticKey = key
	c.secure = true
	return c
}

// NewDialedSecureConn wraps a connection we opened to a secure listener;
// call ClientHandshake before anything else.
func NewDialedSecureConn(nc net.Conn) *Conn {
	c := NewDialedConn(nc)
	c.secure = true
	return c
}

// serverHandshake runs right after the INITCODE.
func (c *Conn) serverHandshake() error {
	var clientEph [TransportKeySize]byte
	if _, err := io.ReadFull(c.r, clientEph[:]); err != nil {
		return err
	}
	peer, err := ecdh.X25519().NewPublicKey(clientEph[:])
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	ee, err := eph.ECDH(peer)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	es, err := c.staticKey.ECDH(peer)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	serverEph := eph.PublicKey().Bytes()
	c2s, s2c, err := deriveKeys(ee, es, clientEph[:], serverEph, c.staticKey.PublicKey().Bytes())
	if err != nil {
		return err
	}
	st, err := newSecureState(s2c, c2s)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.nc.Write(append(serverEph, confirmation(st.send)...)); err != nil {
		return err
	}
	c.sec = st
	return nil
}

// ClientHandshake plays the game client: it sends the INITCODE and the
// ephemeral key, and checks that the answer comes from the owner of
// serverKey. Used by tests and tools in place of Hello.
func (c *Conn) ClientHandshake(serverKey *ecdh.PublicKey) error {
	defer c.finishHandshake()
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	clientEph := eph.PublicKey().Bytes()
	hello := binary.LittleEndian.AppendUint32(nil, InitCode)
	if _, err := c.nc.Write(append(hello, clientEph...)); err != nil {
		return err
	}
	var reply [TransportKeySize + TransportOverhead]byte
	if _, err := io.ReadFull(c.r, reply[:]); err != nil {
		return err
	}
	peer, err := ecdh.X25519().NewPublicKey(reply[:TransportKeySize])
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	ee, err := eph.ECDH(peer)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	es, err := eph.ECDH(serverKey)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	c2s, s2c, err := deriveKeys(ee, es, clientEph, reply[:TransportKeySize], serverKey.Bytes())
	if err != nil {
		return err
	}
	st, err := newSecureState(c2s, s2c)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(confirmation(st.recv), reply[TransportKeySize:]) != 1 {
		return fmt.Errorf("%w: the server does not own the expected key", ErrHandshake)
	}
	c.sec = st
	return nil
}

// finishHandshake releases the writers; with c.sec still nil they fail.
func (c *Conn) finishHandshake() {
	c.readyOnce.Do(func() { close(c.ready) })
}

// readFrame reads and opens one frame, returning the plain packet.
func (c *Conn) readFrame(limit int) ([]byte, error) {
	sizeBytes, err := c.r.Peek(2)
	if err != nil {
		return nil, err
	}
	w := int(binary.LittleEndian.Uint16(sizeBytes))
	size := w - TransportOverhead
	if size < HeaderSize || size > limit {
		return nil, fmt.Errorf("%w: %d", ErrBadSize, size)
	}
	frame := make([]byte, w)
	if _, err := io.ReadFull(c.r, frame); err != nil {
		return nil, err
	}
	c.sec.recvN++
	if _, err := c.sec.recv.Open(frame[2:2], transportNonce(c.sec.recvN), frame[2:], frame[:2]); err != nil {
		return nil, ErrBadFrame
	}
	raw := frame[:size]
	binary.LittleEndian.PutUint16(raw, uint16(size))
	return raw, nil
}

// sealFrame turns a stamped plain packet into a frame. The caller holds wmu:
// the counter and the write must stay in the same order.
func (c *Conn) sealFrame(pkt []byte) []byte {
	frame := make([]byte, 2, len(pkt)+TransportOverhead)
	binary.LittleEndian.PutUint16(frame, uint16(len(pkt)+TransportOverhead))
	c.sec.sendN++
	return c.sec.send.Seal(frame, transportNonce(c.sec.sendN), pkt[2:], frame[:2])
}

// GenerateTransportKey creates the server's static key.
func GenerateTransportKey() (*ecdh.PrivateKey, error) {
	return ecdh.X25519().GenerateKey(rand.Reader)
}

// FormatTransportKey is the private key as it goes in the environment
// (TMSRV_TRANSPORT_KEY): base64.
func FormatTransportKey(key *ecdh.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(key.Bytes())
}

// ParseTransportKey reads what FormatTransportKey wrote.
func ParseTransportKey(s string) (*ecdh.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != TransportKeySize {
		return nil, errors.New("protocol: the transport key must be 32 bytes in base64")
	}
	return ecdh.X25519().NewPrivateKey(b)
}

// FormatPublicKey is the public key as the client reads it from
// serverkey.txt: 64 hexadecimal digits.
func FormatPublicKey(key *ecdh.PublicKey) string {
	return hex.EncodeToString(key.Bytes())
}

// ParsePublicKey reads what FormatPublicKey wrote.
func ParsePublicKey(s string) (*ecdh.PublicKey, error) {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != TransportKeySize {
		return nil, errors.New("protocol: the public key must be 64 hexadecimal digits")
	}
	return ecdh.X25519().NewPublicKey(b)
}
