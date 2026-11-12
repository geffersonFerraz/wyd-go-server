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
		"bad public":   {"public_ip: 203.0.113.10", "public_ip: http://x.com", "public_ip"},
		"bad host":     {"port: 8282 }", "port: 8282, host: 'a_b.com' }", "channel 2: host"},
		"no address":   {"  public_ip: 203.0.113.10\n", "", "set host"},
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

func TestChannelHostsAreDNSNames(t *testing.T) {
	c, err := Load(write(t, `
serverlist:
  group: 1
  status_url: http://wyd.example.com/serv00.htm
channels:
  - { number: 1, ip: 172.30.87.21, port: 8281, host: wyd-srv1.example.com }
  - { number: 2, ip: 172.30.87.22, port: 8282, host: wyd-srv2.example.com }
  - { number: 3, ip: 172.30.87.23, port: 8283, host: 203.0.113.7 }
`))
	if err != nil {
		t.Fatal(err)
	}
	for n, want := range map[int]string{1: "wyd-srv1.example.com", 2: "wyd-srv2.example.com:8282", 3: "203.0.113.7:8283"} {
		if got := c.Address(n); got != want {
			t.Errorf("channel %d: %q, want %q", n, got, want)
		}
	}
	b, err := EncodeServerList(c.Table())
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeServerList(b)
	if err != nil || back[0][0] != "http://wyd.example.com/serv00.htm" || back[0][2] != "wyd-srv2.example.com:8282" {
		t.Errorf("%v, %q", err, back[0])
	}
}

func TestChannelHostOverridesPublicIP(t *testing.T) {
	c, err := Load(write(t, strings.Replace(sample, "port: 8282 }", "port: 8282, host: srv2.example.com }", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if a, b := c.Address(1), c.Address(2); a != "203.0.113.10" || b != "srv2.example.com:8282" {
		t.Errorf("%q, %q", a, b)
	}
}

func TestValidHost(t *testing.T) {
	for _, ok := range []string{"1.2.3.4", "wyd.gefferson.com", "wyd-srv1.gefferson.com", "localhost"} {
		if !ValidHost(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "::1", "a:80", "-a.com", "a..com", "a_b.com", "300.1.1.1", "http://a.com", "a.com/x", "1.2.3.4:80"} {
		if ValidHost(bad) {
			t.Errorf("%q accepted", bad)
		}
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
