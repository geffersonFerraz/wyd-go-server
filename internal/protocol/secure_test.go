package protocol

import (
	"bytes"
	"crypto/ecdh"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func testKey(t *testing.T) *ecdh.PrivateKey {
	t.Helper()
	key, err := GenerateTransportKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// securePair is a server and a client that finished the handshake.
func securePair(t *testing.T) (server, client *Conn, raw net.Conn) {
	t.Helper()
	key := testKey(t)
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	server, client = NewSecureConn(a, key), NewDialedSecureConn(b)
	done := make(chan error, 1)
	go func() { done <- client.ClientHandshake(key.PublicKey()) }()
	// The server runs its side inside the first read; feed it one packet.
	go func() {
		if err := <-done; err != nil {
			t.Error(err)
			return
		}
		_ = client.SendRaw(framePacket(0x20D, 7, []byte("first")))
	}()
	pkt, err := server.ReadPacket()
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Header.PacketID != 0x20D || pkt.Header.ClientID != 7 || string(pkt.Raw[HeaderSize:]) != "first" {
		t.Fatalf("%+v %q", pkt.Header, pkt.Raw)
	}
	return server, client, b
}

func framePacket(id, client uint16, body []byte) []byte {
	pkt := make([]byte, HeaderSize+len(body))
	binary.LittleEndian.PutUint16(pkt[4:], id)
	binary.LittleEndian.PutUint16(pkt[6:], client)
	copy(pkt[HeaderSize:], body)
	return pkt
}

func TestSecureRoundTripBothWays(t *testing.T) {
	server, client, _ := securePair(t)
	body := bytes.Repeat([]byte{0xAB}, 3000)
	go func() {
		for i := range 3 {
			_ = server.SendRaw(framePacket(uint16(0x100+i), 0, body))
		}
	}()
	for i := range 3 {
		pkt, err := client.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if int(pkt.Header.Size) != HeaderSize+len(body) || pkt.Header.PacketID != uint16(0x100+i) ||
			pkt.Header.Key != 0 || !pkt.ChecksumOK || !bytes.Equal(pkt.Raw[HeaderSize:], body) {
			t.Fatalf("packet %d: %+v", i, pkt.Header)
		}
	}
}

// What travels must not show the packet: the old cipher left the size, and
// anyone with the table could read the rest.
func TestSecureFrameHidesThePacket(t *testing.T) {
	key := testKey(t)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	server := NewSecureConn(a, key)
	wire := make(chan []byte, 1)
	go func() {
		// A hand-made client: hello, then capture the server's bytes.
		client := NewDialedSecureConn(b)
		if err := client.ClientHandshake(key.PublicKey()); err != nil {
			t.Error(err)
			return
		}
		_ = client.SendRaw(framePacket(1, 1, nil))
		frame := make([]byte, HeaderSize+len("senha-secreta")+TransportOverhead)
		_, _ = io.ReadFull(b, frame)
		wire <- frame
	}()
	if _, err := server.ReadPacket(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.SendRaw(framePacket(0x20D, 0, []byte("senha-secreta"))) }()
	frame := <-wire
	if bytes.Contains(frame, []byte("senha")) {
		t.Fatal("o texto do pacote aparece no fio")
	}
	if got := int(binary.LittleEndian.Uint16(frame)); got != len(frame) {
		t.Fatalf("tamanho do quadro: %d, quadro de %d bytes", got, len(frame))
	}
}

func TestClientRefusesServerWithAnotherKey(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	server, client := NewSecureConn(a, testKey(t)), NewDialedSecureConn(b)
	go func() { _, _ = server.ReadPacket() }()
	err := client.ClientHandshake(testKey(t).PublicKey())
	if !errors.Is(err, ErrHandshake) {
		t.Fatalf("aceitou um servidor sem a chave esperada: %v", err)
	}
	if err := client.SendRaw(framePacket(1, 1, nil)); !errors.Is(err, ErrHandshake) {
		t.Fatalf("enviou sem chave: %v", err)
	}
}

// A client of the old protocol sends the INITCODE and a KeyTable packet.
func TestServerRefusesLegacyClient(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	server := NewSecureConn(a, testKey(t))
	go func() { _, _ = io.Copy(io.Discard, b) }() // the handshake reply
	go func() {
		old := NewDialedConn(b)
		_ = old.Hello()
		_ = old.SendRaw(framePacket(0x20D, 0, bytes.Repeat([]byte{1}, 104)))
		// The server reads the encrypted bytes as a frame size: when it asks
		// for more than was sent, only the client hanging up ends the read.
		b.Close()
	}()
	if _, err := server.ReadPacket(); err == nil {
		t.Fatal("aceitou um cliente do protocolo antigo")
	}
}

func TestTamperedAndReplayedFramesAreRejected(t *testing.T) {
	for _, mode := range []string{"tamper", "replay"} {
		key := testKey(t)
		a, b := net.Pipe()
		server, client := NewSecureConn(a, key), NewDialedSecureConn(b)
		// The client writes into a buffer so the frames can be altered.
		var sent bytes.Buffer
		go func() {
			if err := client.ClientHandshake(key.PublicKey()); err != nil {
				t.Error(err)
				return
			}
			client.nc = bufferConn{Conn: b, w: &sent}
			_ = client.SendRaw(framePacket(1, 1, []byte("abc")))
			frame := append([]byte(nil), sent.Bytes()...)
			if mode == "tamper" {
				frame[len(frame)/2] ^= 1
				_, _ = b.Write(frame)
				return
			}
			_, _ = b.Write(frame)
			_, _ = b.Write(frame)
		}()
		pkt, err := server.ReadPacket()
		if mode == "replay" {
			if err != nil || string(pkt.Raw[HeaderSize:]) != "abc" {
				t.Fatalf("replay: first frame: %v", err)
			}
			_, err = server.ReadPacket()
		}
		if !errors.Is(err, ErrBadFrame) {
			t.Fatalf("%s: %v", mode, err)
		}
		a.Close()
		b.Close()
	}
}

// bufferConn sends the writes to a buffer instead of the peer.
type bufferConn struct {
	net.Conn
	w *bytes.Buffer
}

func (c bufferConn) Write(p []byte) (int, error) { return c.w.Write(p) }

// A connection that never says hello must not hold the writer forever.
func TestSilentClientIsDroppedAtTheDeadline(t *testing.T) {
	old := handshakeTimeout
	handshakeTimeout = 50 * time.Millisecond
	defer func() { handshakeTimeout = old }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	peer, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	nc, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	server := NewSecureConn(nc, testKey(t))
	writer := make(chan error, 1)
	go func() { writer <- server.SendRaw(framePacket(1, 1, nil)) }()
	if _, err := server.ReadPacket(); err == nil {
		t.Fatal("handshake sem cliente")
	}
	select {
	case err := <-writer:
		if !errors.Is(err, ErrHandshake) {
			t.Fatalf("writer: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("o writer ficou preso")
	}
}

func TestTransportKeyText(t *testing.T) {
	key := testKey(t)
	back, err := ParseTransportKey(" " + FormatTransportKey(key) + "\n")
	if err != nil || !back.Equal(key) {
		t.Fatalf("private: %v", err)
	}
	pub, err := ParsePublicKey(FormatPublicKey(key.PublicKey()) + "\r\n")
	if err != nil || !pub.Equal(key.PublicKey()) {
		t.Fatalf("public: %v", err)
	}
	for _, bad := range []string{"", "abc", FormatPublicKey(key.PublicKey())} {
		if _, err := ParseTransportKey(bad); err == nil {
			t.Errorf("ParseTransportKey(%q) accepted", bad)
		}
	}
	if _, err := ParsePublicKey(FormatTransportKey(key)); err == nil {
		t.Error("ParsePublicKey accepted base64")
	}
}

// The client is C++ (CPSock.cpp, Monocypher) and must derive the same keys
// and build the same frames. These values were produced by that code with
// the inputs below; see docs/transporte-seguro.md.
func TestVectorsMatchTheClient(t *testing.T) {
	seq := func(start byte) []byte {
		b := make([]byte, 32)
		for i := range b {
			b[i] = start + byte(i)
		}
		return b
	}
	static, _ := ecdh.X25519().NewPrivateKey(seq(0x01))
	clientEph, _ := ecdh.X25519().NewPrivateKey(seq(0x41))
	serverEph, _ := ecdh.X25519().NewPrivateKey(seq(0x81))

	ee, _ := clientEph.ECDH(serverEph.PublicKey())
	es, _ := clientEph.ECDH(static.PublicKey())
	c2s, s2c, err := deriveKeys(ee, es, clientEph.PublicKey().Bytes(), serverEph.PublicKey().Bytes(), static.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	st, err := newSecureState(c2s, s2c)
	if err != nil {
		t.Fatal(err)
	}
	c := &Conn{sec: st}
	pkt := framePacket(0x20D, 3, []byte("WYD"))
	binary.LittleEndian.PutUint16(pkt, uint16(len(pkt)))
	binary.LittleEndian.PutUint32(pkt[8:], 0x11223344)

	got := map[string]string{
		"c2s":     hex.EncodeToString(c2s),
		"s2c":     hex.EncodeToString(s2c),
		"confirm": hex.EncodeToString(confirmation(st.recv)),
		"frame":   hex.EncodeToString(c.sealFrame(pkt)),
	}
	for name, want := range clientVectors {
		if got[name] != want {
			t.Errorf("%s:\n got  %s\n want %s", name, got[name], want)
		}
	}
}

var clientVectors = map[string]string{
	"c2s":     "20cc6de30405849899a90aae4481923ce0e828a7d9289848a3db759f6f74e371",
	"s2c":     "dd4cdb87c52c7d0f0affc39c702c9585a8fb2c58748efe12b00a01659ff54ace",
	"confirm": "da7b87439ec60da873035b6f32b011bf",
	"frame":   "1f0014ca9c6f4d9b793bd99539e4c67c679ab1ca114fa26e819bd692d100aa",
}
