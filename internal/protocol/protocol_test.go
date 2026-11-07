package protocol

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The table must be byte-identical to the one TMSRV is compiled with.
func TestKeyTableMatchesCppHeader(t *testing.T) {
	src, err := os.ReadFile("../../../Source Servidor/Common/KeyTable.h")
	if err != nil {
		t.Skipf("C++ source not available: %v", err)
	}
	hex := regexp.MustCompile(`0x[0-9a-fA-F]{2}\b`).FindAll(src, -1)
	if len(hex) != len(KeyTable) {
		t.Fatalf("header has %d entries, Go table %d", len(hex), len(KeyTable))
	}
	for i, h := range hex {
		v, _ := strconv.ParseUint(string(h[2:]), 16, 8)
		if byte(v) != KeyTable[i] {
			t.Fatalf("KeyTable[%d] = %#02x, header has %s", i, KeyTable[i], h)
		}
	}
}

// encryptReference is a literal transliteration of CPSock::AddMessage, using
// signed char arithmetic and int sums exactly as the C++ does, to pin the
// optimized Encrypt to the original semantics.
func encryptReference(packet []byte, rnd int32, ts uint32) []byte {
	size := len(packet)
	binary.LittleEndian.PutUint16(packet[0:], uint16(size))
	packet[2] = byte(rnd)
	packet[3] = 0
	binary.LittleEndian.PutUint32(packet[8:], ts)

	send := make([]byte, size)
	var sum1, sum2 int32
	pos := int32(KeyTable[(rnd&255)*2])
	for i := 4; i < size; i, pos = i+1, pos+1 {
		sum1 += int32(int8(packet[i]))
		rst := pos & int32(-0x7FFFFF01) // 0x800000FF
		trans := int32(KeyTable[(rst&255)*2+1])
		switch i & 3 {
		case 0:
			send[i] = byte(int32(int8(packet[i])) + ((trans & 0xFF) << 1))
		case 1:
			send[i] = byte(int32(int8(packet[i])) - ((trans & 0xFF) >> 3))
		case 2:
			send[i] = byte(int32(int8(packet[i])) + ((trans & 0xFF) << 2))
		case 3:
			send[i] = byte(int32(int8(packet[i])) - ((trans & 0xFF) >> 5))
		}
		sum2 += int32(int8(send[i]))
	}
	packet[3] = byte(((sum2 & 0xFF) - (sum1 & 0xFF)) & 255)
	copy(send[:4], packet[:4])
	return send
}

func samplePacket(n int) []byte {
	p := make([]byte, n)
	for i := HeaderSize; i < n; i++ {
		p[i] = byte(i*37 + 11)
	}
	binary.LittleEndian.PutUint16(p[4:], 0x803)
	binary.LittleEndian.PutUint16(p[6:], 42)
	return p
}

func TestEncryptMatchesReference(t *testing.T) {
	for key := 0; key < 256; key++ {
		for _, n := range []int{12, 13, 36, 120, 2376, 4048} {
			ref := encryptReference(samplePacket(n), int32(key), 0xDEADBEEF)
			got := samplePacket(n)
			Encrypt(got, uint8(key), 0xDEADBEEF)
			if !bytes.Equal(got, ref) {
				t.Fatalf("key %d size %d: Encrypt differs from AddMessage reference", key, n)
			}
		}
	}
}

func TestDecryptRoundTrip(t *testing.T) {
	for key := 0; key < 256; key++ {
		plain := samplePacket(300)
		binary.LittleEndian.PutUint16(plain[0:], 300)
		binary.LittleEndian.PutUint32(plain[8:], 1234)
		plain[2] = byte(key)
		want := append([]byte(nil), plain...)

		Encrypt(plain, uint8(key), 1234)
		if bytes.Equal(plain[4:], want[4:]) {
			t.Fatalf("key %d: body not encrypted", key)
		}
		if !Decrypt(plain) {
			t.Fatalf("key %d: checksum mismatch on our own packet", key)
		}
		want[3] = plain[3] // checksum byte is set by Encrypt
		if !bytes.Equal(plain, want) {
			t.Fatalf("key %d: round trip mismatch", key)
		}
	}
}

// The legacy checksum is sum(cipher) - sum(plain). Each cipher byte is the
// plain byte plus/minus a key-derived constant, so the checksum depends only
// on key and length: it cannot detect corruption. This test pins that fact so
// nobody relies on ChecksumOK for integrity (TCP checksums are what we have).
func TestChecksumIsKeyOnly(t *testing.T) {
	a, b := samplePacket(64), samplePacket(64)
	for i := HeaderSize; i < 64; i++ {
		b[i] ^= 0x5A
	}
	Encrypt(a, 9, 0)
	Encrypt(b, 9, 0)
	if a[3] != b[3] {
		t.Fatal("checksum now depends on content; update Decrypt docs")
	}
	a[40] ^= 0x10
	if !Decrypt(a) {
		t.Fatal("a corrupted byte unexpectedly changed the checksum")
	}
}

func TestConnHandshakeAndFraming(t *testing.T) {
	a, b := net.Pipe()
	server, client := NewConn(a), NewDialedConn(b)
	defer server.Close()
	defer client.Close()

	go func() {
		_ = client.Hello()
		_ = client.SendRaw(samplePacket(120))
		_ = client.SendRaw(samplePacket(36))
	}()

	for _, n := range []int{120, 36} {
		pkt, err := server.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if len(pkt.Raw) != n || pkt.Header.Size != uint16(n) {
			t.Fatalf("got %d bytes (size field %d), want %d", len(pkt.Raw), pkt.Header.Size, n)
		}
		if pkt.Header.PacketID != 0x803 || pkt.Header.ClientID != 42 || !pkt.ChecksumOK {
			t.Fatalf("bad header after decrypt: %+v ok=%v", pkt.Header, pkt.ChecksumOK)
		}
		want := samplePacket(n)
		if !bytes.Equal(pkt.Raw[HeaderSize:], want[HeaderSize:]) {
			t.Fatal("body mismatch after decrypt")
		}
	}
}

func TestConnRejectsBadHello(t *testing.T) {
	a, b := net.Pipe()
	server := NewConn(a)
	defer a.Close()
	go func() { _, _ = b.Write([]byte{1, 2, 3, 4}); b.Close() }()
	if _, err := server.ReadPacket(); err == nil {
		t.Fatal("expected error for bad init code")
	}
}

func TestConnRejectsOversize(t *testing.T) {
	a, b := net.Pipe()
	server := NewConn(a)
	defer a.Close()
	go func() {
		var buf [16]byte
		binary.LittleEndian.PutUint32(buf[0:], InitCode)
		binary.LittleEndian.PutUint16(buf[4:], MaxMessageSize+1)
		_, _ = b.Write(buf[:])
		b.Close()
	}()
	if _, err := server.ReadPacket(); err == nil {
		t.Fatal("expected size error")
	}
}
