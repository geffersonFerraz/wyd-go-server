package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `
other_section: { anything: 1 }
serverlist:
  group: 1
  public_ip: 203.0.113.10
  status_url: http://203.0.113.10/serv00.htm
channels:
  - { number: 1, ip: 172.30.87.21, port: 8281 }
  - { number: 2, ip: 172.30.87.22, port: 8282 }
`

func write(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tmsrv.yaml")
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ChannelIPs(); len(got) != 2 || got[1] != "172.30.87.22" {
		t.Errorf("ips %v", got)
	}
	if a := c.Address(1); a != "203.0.113.10" {
		t.Errorf("channel 1 on the default port should be a bare IP, got %q", a)
	}
	if a := c.Address(2); a != "203.0.113.10:8282" {
		t.Errorf("channel 2 address %q", a)
	}
}

func TestLoadRejects(t *testing.T) {
	for name, tc := range map[string]struct{ from, to, want string }{
		"gap":          {"number: 2, ip", "number: 3, ip", "number must be 2"},
		"same ip":      {"172.30.87.22", "172.30.87.21", "already channel 1"},
		"same port":    {"port: 8282", "port: 8281", "already channel 1"},
		"host name":    {"public_ip: 203.0.113.10", "public_ip: example.com", "IPv4"},
		"typo":         {"port: 8282 }", "port: 8282, prot: 1 }", "prot"},
		"bad group":    {"group: 1", "group: 11", "group"},
		"missing list": {"channels:", "chanels:", "missing keys"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, strings.Replace(sample, tc.from, tc.to, 1)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want %q", err, tc.want)
			}
		})
	}
}

func TestServerListRoundTrip(t *testing.T) {
	c, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodeServerList(c.Table())
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 7040 {
		t.Fatalf("size %d", len(b))
	}
	// First byte of [0][0] is 'h' + key[63] (0xd9).
	if b[0] != byte('h')+serverListKey[63] {
		t.Errorf("byte 0 = %#x", b[0])
	}
	// An empty entry is the key reversed.
	off := (1*11 + 0) * 64
	if b[off] != 0xd9 || b[off+63] != 0xa4 {
		t.Errorf("empty entry not the reversed key: %#x %#x", b[off], b[off+63])
	}
	got, err := DecodeServerList(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != c.Table() {
		t.Errorf("round trip:\n%v\n%v", got[0], c.Table()[0])
	}
	if got[0][2] != "203.0.113.10:8282" || got[0][3] != "" {
		t.Errorf("entries %q", got[0][:4])
	}
}
