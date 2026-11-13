// Package cluster is the server layout shared by DBSRV, TMSRV and the admin
// panel: the "servers" section of tmsrv.yaml. One file describes every server
// (world) the client can reach; each server has its own DBSRV (and database)
// and up to ten channels (TMSRV). It replaces data/dbsrv/serverlist.txt and is
// the source of the client's serverlist.bin, which lists all of them.
package cluster

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxChannels is the client's MAX_SERVER: channels per server group
// (g_pServerList[group][1..10]).
const MaxChannels = 10

// ClientDefaultPort is TM_CONNECTION_PORT: the port an unpatched client
// always connects to. Entries on this port are written as a bare IP so such
// a client still reads them.
const ClientDefaultPort = 8281

// MaxGroups is MAX_SERVERGROUP: server groups in serverlist.bin.
const MaxGroups = 10

// GuildsPerServer is how many guild numbers each server has. The numbers of
// the client (1..65534, MAX_GUILD) are split between the server groups, so a
// guild number never repeats between servers: the guild marks, which the
// client downloads by number from one address, are the same folder for all.
const GuildsPerServer = 65534 / MaxGroups

// GuildRange returns the guild numbers a server (its group) creates, both
// inclusive: 1..6553 for group 1, 6554..13106 for group 2 and so on. A group
// out of range counts as group 1.
func GuildRange(group int) (first, last int) {
	if group < 1 || group > MaxGroups {
		group = 1
	}
	return (group-1)*GuildsPerServer + 1, group * GuildsPerServer
}

// Channel is one TMSRV.
type Channel struct {
	// Number is the channel as players see it (1..10). Channels are
	// numbered 1..N without gaps: the client maps the list position to it.
	Number int `yaml:"number"`
	// IP is the address the TMSRV listens on and the source address of its
	// DBSRV link; DBSRV tells the channels apart by it, so every channel
	// (of every server) needs its own. In docker-compose it is the
	// container's fixed IP.
	IP string `yaml:"ip"`
	// Port is the TCP port the TMSRV listens on and the port players
	// connect to (published on the host with the same number).
	Port int `yaml:"port"`
	// Host is the name or IPv4 address players use to reach this channel
	// (what serverlist.bin holds for it). Optional: without it the channel
	// uses the server's public_ip. The client resolves names with the DNS
	// of the player's machine, so a name must point at the public address
	// of this channel's port.
	Host string `yaml:"host,omitempty"`
}

// DBSRV is where the DBSRV of a server listens.
type DBSRV struct {
	IP   string `yaml:"ip"`
	Port int    `yaml:"port"`
}

// StatusFile is the file name of status_url (servNN.htm): how the admin
// panel, which serves the status of every server, tells them apart.
func (s *Server) StatusFile() string {
	u, err := url.Parse(s.StatusURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(path.Base(u.Path))
}

// Addr is "ip:port".
func (d DBSRV) Addr() string { return net.JoinHostPort(d.IP, strconv.Itoa(d.Port)) }

// Server is one world: a group of the client's server list, served by one
// DBSRV (one database) and its channels.
type Server struct {
	// Group is the server group in the client (1..10, the group number of
	// sn.bin): serverlist.bin entry [Group-1]. It is also how each program
	// picks its server (-server).
	Group int `yaml:"group"`
	// PublicIP is the address players connect to when a channel has no host
	// of its own (an IPv4 address or a host name). Optional if every channel
	// sets host.
	PublicIP string `yaml:"public_ip,omitempty"`
	// StatusURL is the servNN.htm served by the public listener of this
	// server's admin panel (serverlist.bin entry [Group-1][0]).
	StatusURL string `yaml:"status_url"`
	// DBSRV is where this server's DBSRV listens and its channels connect.
	DBSRV    DBSRV     `yaml:"dbsrv"`
	Channels []Channel `yaml:"channels"`
}

// Config is the layout of every server.
type Config struct {
	Servers []Server `yaml:"servers"`
}

// Load reads the servers section of tmsrv.yaml (the other sections belong to
// the TMSRV and are checked by it).
func Load(path string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("settings file: %w", err)
	}
	var top struct {
		Servers yaml.Node `yaml:"servers"`
	}
	if err := yaml.Unmarshal(raw, &top); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if top.Servers.Kind == 0 {
		return c, fmt.Errorf("%s: missing keys: servers", path)
	}
	if err := decodeStrict(&top.Servers, &c.Servers); err != nil {
		return c, fmt.Errorf("%s: servers: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// decodeStrict decodes n into v, rejecting unknown keys (typos).
func decodeStrict(n *yaml.Node, v any) error {
	out, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(out))
	dec.KnownFields(true)
	return dec.Decode(v)
}

// Validate checks every server and what they must not share: the group, the
// status file and the addresses players connect to. Channel IPs and the DBSRV
// address only have to be unique inside a server: each server runs on its own
// machine, where the same private addresses may repeat.
func (c *Config) Validate() error {
	if len(c.Servers) == 0 {
		return errors.New("servers: at least one server is required")
	}
	if len(c.Servers) > MaxGroups {
		return fmt.Errorf("servers: at most %d, got %d", MaxGroups, len(c.Servers))
	}
	groups := map[int]bool{}
	status := map[string]int{}
	addrs := map[string]string{}
	for i := range c.Servers {
		s := &c.Servers[i]
		if s.Group < 1 || s.Group > MaxGroups {
			return fmt.Errorf("servers[%d]: group must be 1..%d, got %d", i, MaxGroups, s.Group)
		}
		if groups[s.Group] {
			return fmt.Errorf("servers[%d]: group %d is already another server", i, s.Group)
		}
		groups[s.Group] = true
		if err := s.validate(); err != nil {
			return fmt.Errorf("server %d: %w", s.Group, err)
		}
		// One admin panel serves the online counts of every server and
		// tells them apart by the file name.
		if o, dup := status[s.StatusFile()]; dup {
			return fmt.Errorf("server %d: status_url file %q is already the one of server %d (use serv00.htm, serv01.htm...)", s.Group, s.StatusFile(), o)
		}
		status[s.StatusFile()] = s.Group
		for _, ch := range s.Channels {
			who := fmt.Sprintf("server %d channel %d", s.Group, ch.Number)
			addr := strings.ToLower(s.Address(ch.Number))
			if o, dup := addrs[addr]; dup {
				return fmt.Errorf("%s: players' address %s is already %s", who, addr, o)
			}
			addrs[addr] = who
		}
	}
	return nil
}

// validate checks one server on its own.
func (s *Server) validate() error {
	if s.PublicIP != "" && !ValidHost(s.PublicIP) {
		return fmt.Errorf("public_ip must be an IPv4 address or a host name, got %q", s.PublicIP)
	}
	if s.StatusURL == "" || len(s.StatusURL) > entrySize-1 {
		return fmt.Errorf("status_url must be 1..%d characters, got %q", entrySize-1, s.StatusURL)
	}
	if net.ParseIP(s.DBSRV.IP) == nil && !ValidHost(s.DBSRV.IP) {
		return fmt.Errorf("dbsrv.ip must be an IP address or a host name, got %q", s.DBSRV.IP)
	}
	if s.DBSRV.Port < 1 || s.DBSRV.Port > 65535 {
		return fmt.Errorf("dbsrv.port must be 1..65535, got %d", s.DBSRV.Port)
	}
	if len(s.Channels) == 0 {
		return errors.New("channels: at least one channel is required")
	}
	if len(s.Channels) > MaxChannels {
		return fmt.Errorf("channels: at most %d, got %d", MaxChannels, len(s.Channels))
	}
	ips := map[string]int{}
	ports := map[int]int{}
	for i, ch := range s.Channels {
		if ch.Number != i+1 {
			return fmt.Errorf("channels[%d]: number must be %d (channels are numbered 1..N in order, without gaps), got %d", i, i+1, ch.Number)
		}
		ip := net.ParseIP(ch.IP)
		if ip == nil || ip.IsUnspecified() {
			return fmt.Errorf("channel %d: ip must be a concrete IP address (DBSRV identifies the channel by it), got %q", ch.Number, ch.IP)
		}
		if ch.Port < 1 || ch.Port > 65535 {
			return fmt.Errorf("channel %d: port must be 1..65535, got %d", ch.Number, ch.Port)
		}
		if o, dup := ips[ip.String()]; dup {
			return fmt.Errorf("channel %d: ip %s is already channel %d (DBSRV identifies the channel by its IP)", ch.Number, ch.IP, o)
		}
		if o, dup := ports[ch.Port]; dup {
			return fmt.Errorf("channel %d: port %d is already channel %d (players reach every channel through its port on the public address)", ch.Number, ch.Port, o)
		}
		ips[ip.String()], ports[ch.Port] = ch.Number, ch.Number
		switch {
		case ch.Host != "" && !ValidHost(ch.Host):
			return fmt.Errorf("channel %d: host must be an IPv4 address or a host name, got %q", ch.Number, ch.Host)
		case ch.Host == "" && s.PublicIP == "":
			return fmt.Errorf("channel %d: set host (or the server's public_ip) so players know where to connect", ch.Number)
		}
		if len(s.Address(ch.Number)) > entrySize-1 {
			return fmt.Errorf("channel %d: address %q is too long for serverlist.bin", ch.Number, s.Address(ch.Number))
		}
	}
	return nil
}

// Server returns the server of a group. Group 0 is "the only server of the
// file", so a single-server setup needs no -server.
func (c *Config) Server(group int) (Server, error) {
	if group == 0 {
		if len(c.Servers) != 1 {
			return Server{}, fmt.Errorf("%d servers configured: choose one with -server (or WYD_SERVER)", len(c.Servers))
		}
		return c.Servers[0], nil
	}
	var groups []string
	for _, s := range c.Servers {
		if s.Group == group {
			return s, nil
		}
		groups = append(groups, strconv.Itoa(s.Group))
	}
	return Server{}, fmt.Errorf("server %d is not in the settings (servers: %s)", group, strings.Join(groups, ", "))
}

// Channel returns channel n (1-based).
func (s *Server) Channel(n int) (Channel, bool) {
	if n < 1 || n > len(s.Channels) {
		return Channel{}, false
	}
	return s.Channels[n-1], true
}

// ChannelIPs returns the IP of each channel, index = number-1.
func (s *Server) ChannelIPs() []string {
	ips := make([]string, len(s.Channels))
	for i, ch := range s.Channels {
		ips[i] = ch.IP
	}
	return ips
}

var hostLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// ValidHost reports whether s is an IPv4 address or a DNS host name (letters,
// digits and hyphens in dot-separated labels). No port, scheme or IPv6.
func ValidHost(s string) bool {
	if ip := net.ParseIP(s); ip != nil {
		return ip.To4() != nil && !strings.Contains(s, ":")
	}
	if s == "" || len(s) > 253 || strings.Trim(s, "0123456789.") == "" {
		return false // Only digits and dots must be a valid IPv4, which 300.1.1.1 is not.
	}
	for _, label := range strings.Split(s, ".") {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// Host is the name or address players use for channel n.
func (s *Server) Host(n int) string {
	ch, ok := s.Channel(n)
	if !ok {
		return ""
	}
	if ch.Host != "" {
		return ch.Host
	}
	return s.PublicIP
}

// Address is what serverlist.bin holds for channel n: the host alone on the
// client's default port, "host:port" otherwise (needs the patched client).
func (s *Server) Address(n int) string {
	ch, ok := s.Channel(n)
	if !ok {
		return ""
	}
	if ch.Port == ClientDefaultPort {
		return s.Host(n)
	}
	return net.JoinHostPort(s.Host(n), strconv.Itoa(ch.Port))
}
