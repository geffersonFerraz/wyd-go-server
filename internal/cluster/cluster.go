// Package cluster is the channel layout shared by DBSRV, TMSRV and the admin
// panel: the "serverlist" and "channels" sections of tmsrv.yaml. It replaces
// data/dbsrv/serverlist.txt and is the source of the client's serverlist.bin.
package cluster

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
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

// Channel is one TMSRV.
type Channel struct {
	// Number is the channel as players see it (1..10). Channels are
	// numbered 1..N without gaps: the client maps the list position to it.
	Number int `yaml:"number"`
	// IP is the address the TMSRV listens on and the source address of its
	// DBSRV link; DBSRV tells the channels apart by it, so every channel
	// needs its own. In docker-compose it is the container's fixed IP.
	IP string `yaml:"ip"`
	// Port is the TCP port the TMSRV listens on and the port players
	// connect to (published on the host with the same number).
	Port int `yaml:"port"`
	// Host is the name or IPv4 address players use to reach this channel
	// (what serverlist.bin holds for it). Optional: without it the channel
	// uses serverlist.public_ip. The client resolves names with the DNS of
	// the player's machine, so a name must point at the public address of
	// this channel's port.
	Host string `yaml:"host,omitempty"`
}

// ServerList is what goes into the client's serverlist.bin.
type ServerList struct {
	// Group is the server group of this world in the client (1..10, the
	// group number of sn.bin): serverlist.bin entry [Group-1].
	Group int `yaml:"group"`
	// PublicIP is the address players connect to when a channel has no host
	// of its own (an IPv4 address or a host name). Optional if every channel
	// sets host.
	PublicIP string `yaml:"public_ip,omitempty"`
	// StatusURL is the servNN.htm served by the admin panel's public
	// listener (serverlist.bin entry [Group-1][0]).
	StatusURL string `yaml:"status_url"`
}

// Config is the channel layout.
type Config struct {
	ServerList ServerList `yaml:"serverlist"`
	Channels   []Channel  `yaml:"channels"`
}

// Load reads the serverlist and channels sections of tmsrv.yaml (the other
// sections belong to the TMSRV and are checked by it).
func Load(path string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("settings file: %w", err)
	}
	var top struct {
		ServerList yaml.Node `yaml:"serverlist"`
		Channels   yaml.Node `yaml:"channels"`
	}
	if err := yaml.Unmarshal(raw, &top); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if top.ServerList.Kind == 0 || top.Channels.Kind == 0 {
		return c, fmt.Errorf("%s: missing keys: serverlist, channels", path)
	}
	if err := decodeStrict(&top.ServerList, &c.ServerList); err != nil {
		return c, fmt.Errorf("%s: serverlist: %w", path, err)
	}
	if err := decodeStrict(&top.Channels, &c.Channels); err != nil {
		return c, fmt.Errorf("%s: channels: %w", path, err)
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

// Validate checks the layout.
func (c *Config) Validate() error {
	sl := &c.ServerList
	if sl.Group < 1 || sl.Group > MaxGroups {
		return fmt.Errorf("serverlist.group must be 1..%d, got %d", MaxGroups, sl.Group)
	}
	if sl.PublicIP != "" && !ValidHost(sl.PublicIP) {
		return fmt.Errorf("serverlist.public_ip must be an IPv4 address or a host name, got %q", sl.PublicIP)
	}
	if sl.StatusURL == "" || len(sl.StatusURL) > entrySize-1 {
		return fmt.Errorf("serverlist.status_url must be 1..%d characters, got %q", entrySize-1, sl.StatusURL)
	}
	if len(c.Channels) == 0 {
		return errors.New("channels: at least one channel is required")
	}
	if len(c.Channels) > MaxChannels {
		return fmt.Errorf("channels: at most %d, got %d", MaxChannels, len(c.Channels))
	}
	ips := map[string]int{}
	ports := map[int]int{}
	for i, ch := range c.Channels {
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
		case ch.Host == "" && sl.PublicIP == "":
			return fmt.Errorf("channel %d: set host (or serverlist.public_ip) so players know where to connect", ch.Number)
		}
		if len(c.Address(ch.Number)) > entrySize-1 {
			return fmt.Errorf("channel %d: address %q is too long for serverlist.bin", ch.Number, c.Address(ch.Number))
		}
	}
	return nil
}

// Channel returns channel n (1-based).
func (c *Config) Channel(n int) (Channel, bool) {
	if n < 1 || n > len(c.Channels) {
		return Channel{}, false
	}
	return c.Channels[n-1], true
}

// ChannelIPs returns the IP of each channel, index = number-1.
func (c *Config) ChannelIPs() []string {
	ips := make([]string, len(c.Channels))
	for i, ch := range c.Channels {
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
func (c *Config) Host(n int) string {
	ch, ok := c.Channel(n)
	if !ok {
		return ""
	}
	if ch.Host != "" {
		return ch.Host
	}
	return c.ServerList.PublicIP
}

// Address is what serverlist.bin holds for channel n: the host alone on the
// client's default port, "host:port" otherwise (needs the patched client).
func (c *Config) Address(n int) string {
	ch, ok := c.Channel(n)
	if !ok {
		return ""
	}
	if ch.Port == ClientDefaultPort {
		return c.Host(n)
	}
	return net.JoinHostPort(c.Host(n), strconv.Itoa(ch.Port))
}
