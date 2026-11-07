// Package protocol implements the TMSRV <-> DBSRV transport: the INITCODE
// handshake, the 12-byte packet header, and the KeyTable body cipher.
//
// Ported from Source Servidor/Projects/Dataserver/CPSock.cpp
// (CPSock::AddMessage and PacketDecrypt).
package protocol

import "encoding/binary"

const (
	// HeaderSize is sizeof(PacketHeader).
	HeaderSize = 12
	// InitCode is the 4-byte hello every TMSRV sends right after connecting.
	InitCode uint32 = 0x1F11F311
	// MaxMessageSize mirrors MAX_MESSAGE_SIZE in Dataserver/CUser.h.
	MaxMessageSize = 20000
	// MaxClientMessageSize mirrors TMSRV's MAX_MESSAGE_SIZE for client packets.
	MaxClientMessageSize = 8096
)

// Header mirrors PacketHeader (Common/SharedStructs.h).
type Header struct {
	Size      uint16
	Key       uint8
	CheckSum  uint8
	PacketID  uint16
	ClientID  uint16
	TimeStamp uint32
}

// ParseHeader reads the plaintext header fields. Size/Key/CheckSum are never
// encrypted; PacketID/ClientID/TimeStamp are only valid after Decrypt.
func ParseHeader(b []byte) Header {
	return Header{
		Size:      binary.LittleEndian.Uint16(b[0:]),
		Key:       b[2],
		CheckSum:  b[3],
		PacketID:  binary.LittleEndian.Uint16(b[4:]),
		ClientID:  binary.LittleEndian.Uint16(b[6:]),
		TimeStamp: binary.LittleEndian.Uint32(b[8:]),
	}
}

// Encrypt seals a plaintext packet in place, exactly like CPSock::AddMessage:
// it stamps Size, Key, CheckSum and TimeStamp into the header, then mixes
// every byte from offset 4 onward with the KeyTable.
func Encrypt(pkt []byte, key uint8, timeStamp uint32) {
	binary.LittleEndian.PutUint16(pkt[0:], uint16(len(pkt)))
	pkt[2] = key
	pkt[3] = 0
	binary.LittleEndian.PutUint32(pkt[8:], timeStamp)

	var sumPlain, sumEnc uint8
	pos := int(KeyTable[int(key)*2])
	for i := 4; i < len(pkt); i, pos = i+1, pos+1 {
		trans := KeyTable[(pos&0xFF)*2+1]
		p := pkt[i]
		var e uint8
		switch i & 3 {
		case 0:
			e = p + trans<<1
		case 1:
			e = p - trans>>3
		case 2:
			e = p + trans<<2
		case 3:
			e = p - trans>>5
		}
		sumPlain += p
		sumEnc += e
		pkt[i] = e
	}
	pkt[3] = sumEnc - sumPlain
}

// Decrypt reverses Encrypt in place and reports whether the checksum matched.
// The checksum is a function of key and length only (see
// TestChecksumIsKeyOnly), so a match proves nothing about content; a mismatch
// only means the peer computes it differently. The C++ server never checked
// it, so callers should log a mismatch rather than drop the packet.
func Decrypt(pkt []byte) (checksumOK bool) {
	size := int(binary.LittleEndian.Uint16(pkt[0:]))
	if size > len(pkt) {
		size = len(pkt)
	}
	var sumPlain, sumEnc uint8
	pos := int(KeyTable[int(pkt[2])*2])
	for i := 4; i < size; i, pos = i+1, pos+1 {
		trans := KeyTable[(pos&0xFF)*2+1]
		e := pkt[i]
		var p uint8
		switch i & 3 {
		case 0:
			p = e - trans<<1
		case 1:
			p = e + trans>>3
		case 2:
			p = e - trans<<2
		case 3:
			p = e + trans>>5
		}
		sumEnc += e
		sumPlain += p
		pkt[i] = p
	}
	return pkt[3] == sumEnc-sumPlain
}
