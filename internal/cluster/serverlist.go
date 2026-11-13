package cluster

import (
	"errors"
	"fmt"
)

// serverlist.bin is char g_pServerList[MAX_SERVERGROUP][MAX_SERVERNUMBER][64]
// (10 x 11 x 64 = 7040 bytes) with every byte shifted by a 64-byte key
// (BASE_InitializeServerList: plain = stored - key[63-i]). Entry [g][0] is
// the URL of the online counts (servNN.htm), [g][1..10] the channels.

const (
	entrySize      = 64
	entriesPerGrp  = MaxChannels + 1
	ServerListSize = MaxGroups * entriesPerGrp * entrySize
)

// serverListKey is the client's szList: "¤¡¤¤¤§…°¡³ª´Ù" in the build's
// ANSI code page (the same bytes as the original CP949 "ㄱㄴㄷ…가나다").
var serverListKey = [entrySize]byte{
	0xa4, 0xa1, 0xa4, 0xa4, 0xa4, 0xa7, 0xa4, 0xa9, 0xa4, 0xb1, 0xa4, 0xb2, 0xa4, 0xb5, 0xa4, 0xb7,
	0xa4, 0xb8, 0xa4, 0xba, 0xa4, 0xbb, 0xa4, 0xbc, 0xa4, 0xbd, 0xa4, 0xbe, 0xa4, 0xbf, 0xa4, 0xc1,
	0xa4, 0xc3, 0xa4, 0xc5, 0xa4, 0xc7, 0xa4, 0xcb, 0xa4, 0xcc, 0xa4, 0xd0, 0xa4, 0xd1, 0xa4, 0xd3,
	0xa4, 0xbf, 0xa4, 0xc4, 0xa4, 0xd3, 0xa4, 0xc7, 0xa4, 0xcc, 0xb0, 0xa1, 0xb3, 0xaa, 0xb4, 0xd9,
}

// ServerListTable is the plain table: [group][0] status URL, [group][n] channel n.
type ServerListTable [MaxGroups][entriesPerGrp]string

// Table is the serverlist of this configuration: one group per server.
func (c *Config) Table() ServerListTable {
	var t ServerListTable
	for i := range c.Servers {
		s := &c.Servers[i]
		g := s.Group - 1
		t[g][0] = s.StatusURL
		for _, ch := range s.Channels {
			t[g][ch.Number] = s.Address(ch.Number)
		}
	}
	return t
}

// EncodeServerList builds serverlist.bin.
func EncodeServerList(t ServerListTable) ([]byte, error) {
	out := make([]byte, ServerListSize)
	for g := range t {
		for n, s := range t[g] {
			if len(s) > entrySize-1 {
				return nil, fmt.Errorf("serverlist [%d][%d]: %q is longer than %d bytes", g, n, s, entrySize-1)
			}
			off := (g*entriesPerGrp + n) * entrySize
			for i := range entrySize {
				var b byte
				if i < len(s) {
					b = s[i]
				}
				out[off+i] = b + serverListKey[entrySize-1-i]
			}
		}
	}
	return out, nil
}

// DecodeServerList reads serverlist.bin (each entry up to its first NUL).
func DecodeServerList(b []byte) (ServerListTable, error) {
	var t ServerListTable
	if len(b) < ServerListSize {
		return t, errors.New("serverlist.bin: file too short")
	}
	for g := range t {
		for n := range t[g] {
			off := (g*entriesPerGrp + n) * entrySize
			var e [entrySize]byte
			for i := range entrySize {
				e[i] = b[off+i] - serverListKey[entrySize-1-i]
			}
			end := 0
			for end < entrySize && e[end] != 0 {
				end++
			}
			t[g][n] = string(e[:end])
		}
	}
	return t, nil
}
