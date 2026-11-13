package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `
other_section: { anything: 1 }
servers:
  - group: 1
    public_ip: 203.0.113.10
    status_url: http://203.0.113.10/serv00.htm
    dbsrv: { ip: 172.30.87.11, port: 7514 }
    channels:
      - { number: 1, ip: 172.30.87.21, port: 8281 }
      - { number: 2, ip: 172.30.87.22, port: 8282 }
`

// second is appended to sample: another server on the same network.
const second = `  - group: 2
    status_url: http://wyd2.example.com/serv01.htm
    dbsrv: { ip: 172.30.87.41, port: 7514 }
    channels:
      - { number: 1, ip: 172.30.87.51, port: 8281, host: wyd2-srv1.example.com }
      - { number: 2, ip: 172.30.87.52, port: 8292, host: wyd2-srv2.example.com }
`

func write(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tmsrv.yaml")
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func loadOne(t *testing.T, yaml string) Server {
	t.Helper()
	c, err := Load(write(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Server(0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLoad(t *testing.T) {
	c := loadOne(t, sample)
	if got := c.ChannelIPs(); len(got) != 2 || got[1] != "172.30.87.22" {
		t.Errorf("ips %v", got)
	}
	if a := c.Address(1); a != "203.0.113.10" {
		t.Errorf("channel 1 on the default port should be a bare IP, got %q", a)
	}
	if a := c.Address(2); a != "203.0.113.10:8282" {
		t.Errorf("channel 2 address %q", a)
	}
	if a := c.DBSRV.Addr(); a != "172.30.87.11:7514" {
		t.Errorf("dbsrv %q", a)
	}
}

func TestLoadRejects(t *testing.T) {
	for name, tc := range map[string]struct{ from, to, want string }{
		"gap":          {"number: 2, ip", "number: 3, ip", "number must be 2"},
		"same ip":      {"172.30.87.22", "172.30.87.21", "already channel 1"},
		"same port":    {"port: 8282", "port: 8281", "already channel 1"},
		"bad public":   {"public_ip: 203.0.113.10", "public_ip: http://x.com", "public_ip"},
		"bad host":     {"port: 8282 }", "port: 8282, host: 'a_b.com' }", "channel 2: host"},
		"no address":   {"    public_ip: 203.0.113.10\n", "", "set host"},
		"typo":         {"port: 8282 }", "port: 8282, prot: 1 }", "prot"},
		"bad group":    {"group: 1", "group: 11", "group"},
		"missing list": {"servers:", "server:", "missing keys"},
		"no channels":  {"    channels:", "    chanels:", "chanels"},
		"no dbsrv":     {"    dbsrv: { ip: 172.30.87.11, port: 7514 }\n", "", "dbsrv.ip"},
		"dbsrv port":   {"port: 7514", "port: 0", "dbsrv.port"},
		"old layout":   {"servers:", "serverlist: { group: 1 }\nchannels:", "missing keys: servers"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, strings.Replace(sample, tc.from, tc.to, 1)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTwoServers(t *testing.T) {
	c, err := Load(write(t, sample+second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Server(0); err == nil || !strings.Contains(err.Error(), "-server") {
		t.Errorf("two servers and no -server: %v", err)
	}
	if _, err := c.Server(3); err == nil || !strings.Contains(err.Error(), "servers: 1, 2") {
		t.Errorf("server 3: %v", err)
	}
	s1, err1 := c.Server(1)
	s2, err2 := c.Server(2)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	// Each DBSRV only knows the channels of its own server.
	if ips := s1.ChannelIPs(); len(ips) != 2 || ips[0] != "172.30.87.21" {
		t.Errorf("server 1 ips %v", ips)
	}
	if s1.StatusFile() != "serv00.htm" || s2.StatusFile() != "serv01.htm" {
		t.Errorf("status files %q %q", s1.StatusFile(), s2.StatusFile())
	}
	if ips := s2.ChannelIPs(); len(ips) != 2 || ips[0] != "172.30.87.51" || s2.DBSRV.Addr() != "172.30.87.41:7514" {
		t.Errorf("server 2 ips %v dbsrv %s", ips, s2.DBSRV.Addr())
	}
	// One serverlist.bin holds both groups.
	b, err := EncodeServerList(c.Table())
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeServerList(b)
	if err != nil {
		t.Fatal(err)
	}
	if got[0][0] != "http://203.0.113.10/serv00.htm" || got[0][2] != "203.0.113.10:8282" || got[0][3] != "" {
		t.Errorf("group 1: %q", got[0][:4])
	}
	if got[1][0] != "http://wyd2.example.com/serv01.htm" || got[1][1] != "wyd2-srv1.example.com" || got[1][2] != "wyd2-srv2.example.com:8292" || got[1][3] != "" {
		t.Errorf("group 2: %q", got[1][:4])
	}
	if got[2][0] != "" || got[2][1] != "" {
		t.Errorf("group 3 should be empty: %q", got[2][:2])
	}
}

func TestTwoServersRejects(t *testing.T) {
	for name, tc := range map[string]struct{ from, to, want string }{
		"same group":   {"group: 2", "group: 1", "group 1 is already another server"},
		"same status":  {"serv01.htm", "SERV00.htm", "already the one of server 1"},
		"same address": {"wyd2-srv1.example.com", "203.0.113.10", "is already server 1 channel 1"},
		"bad channel":  {"number: 2, ip: 172.30.87.52", "number: 5, ip: 172.30.87.52", "server 2: channels[1]"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, sample+strings.Replace(second, tc.from, tc.to, 1)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want %q", err, tc.want)
			}
		})
	}
	// Each server on its own machine: the same port on another host name
	// and the same private addresses are fine.
	other := strings.NewReplacer("port: 8292", "port: 8282", "172.30.87.41", "172.30.87.11",
		"172.30.87.51", "172.30.87.21", "172.30.87.52", "172.30.87.22").Replace(second)
	if _, err := Load(write(t, sample+other)); err != nil {
		t.Errorf("same private addresses on another machine: %v", err)
	}
}

func TestChannelHostsAreDNSNames(t *testing.T) {
	c := loadOne(t, `
servers:
  - group: 1
    status_url: http://wyd.example.com/serv00.htm
    dbsrv: { ip: 172.30.87.11, port: 7514 }
    channels:
      - { number: 1, ip: 172.30.87.21, port: 8281, host: wyd-srv1.example.com }
      - { number: 2, ip: 172.30.87.22, port: 8282, host: wyd-srv2.example.com }
      - { number: 3, ip: 172.30.87.23, port: 8283, host: 203.0.113.7 }
`)
	for n, want := range map[int]string{1: "wyd-srv1.example.com", 2: "wyd-srv2.example.com:8282", 3: "203.0.113.7:8283"} {
		if got := c.Address(n); got != want {
			t.Errorf("channel %d: %q, want %q", n, got, want)
		}
	}
}

func TestChannelHostOverridesPublicIP(t *testing.T) {
	c := loadOne(t, strings.Replace(sample, "port: 8282 }", "port: 8282, host: srv2.example.com }", 1))
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

// Guild numbers are split between the servers and never overlap.
func TestGuildRanges(t *testing.T) {
	prev := 0
	for g := 1; g <= MaxGroups; g++ {
		first, last := GuildRange(g)
		if first != prev+1 || last-first+1 != GuildsPerServer {
			t.Errorf("group %d: %d..%d after %d", g, first, last, prev)
		}
		prev = last
	}
	if prev > 65534 {
		t.Errorf("last guild number %d is beyond the client's MAX_GUILD", prev)
	}
	if first, last := GuildRange(1); first != 1 || last != 6553 {
		t.Errorf("group 1: %d..%d", first, last)
	}
	if first, _ := GuildRange(0); first != 1 {
		t.Errorf("group 0 should count as 1, got %d", first)
	}
}
