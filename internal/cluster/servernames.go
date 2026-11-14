package cluster

import (
	"bytes"
	"fmt"
	"strconv"
)

// sn.bin is a text file the client reads in NewApp::InitServerName: line i
// is "<group> <name>" (sscanf "%d %s") and fills g_nServerCountList[i] and
// g_szServerNameList[i] (char[16]). The server list shows line i as group
// g_nServerCountList[i], but the channel list names group g with line g-1,
// so line g-1 has to be group g: a group without a server is "0".
//
// Every line is padded with spaces to the same length, so the file always
// has ServerNamesSize bytes (the launcher checks each file by its size).

const (
	// MaxNameBytes is what fits in g_szServerNameList[i] (char[16]).
	MaxNameBytes    = 15
	snLineSize      = 32
	ServerNamesSize = MaxGroups * snLineSize
)

// EncodeName converts a server name to the client's code page (Windows-1252,
// whose letters with accents match Latin-1) and checks that the client reads
// it whole: sscanf "%s" stops at a space and the buffer has 16 bytes.
func EncodeName(name string) ([]byte, error) {
	out := make([]byte, 0, len(name))
	for _, r := range name {
		if !(r > ' ' && r < 0x7f) && !(r >= 0xa1 && r <= 0xff) {
			return nil, fmt.Errorf("name %q: only letters, digits and symbols, without spaces (character %q)", name, r)
		}
		out = append(out, byte(r))
	}
	if len(out) > MaxNameBytes {
		return nil, fmt.Errorf("name %q: at most %d characters, got %d", name, MaxNameBytes, len(out))
	}
	return out, nil
}

// ServerNames builds sn.bin from the servers of the configuration.
func (c *Config) ServerNames() ([]byte, error) {
	var lines [MaxGroups][]byte
	for i := range c.Servers {
		s := &c.Servers[i]
		name, err := EncodeName(s.Name)
		if err != nil {
			return nil, fmt.Errorf("server %d: %w", s.Group, err)
		}
		line := []byte(strconv.Itoa(s.Group))
		if len(name) > 0 {
			line = append(append(line, ' '), name...)
		}
		lines[s.Group-1] = line
	}
	out := make([]byte, 0, ServerNamesSize)
	for _, line := range lines {
		if line == nil {
			line = []byte("0")
		}
		out = append(out, line...)
		out = append(out, bytes.Repeat([]byte{' '}, snLineSize-1-len(line))...)
		out = append(out, '\n')
	}
	return out, nil
}
