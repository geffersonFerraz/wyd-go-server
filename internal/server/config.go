// Package server is the DBSRV TCP server: it accepts TMSRV channel
// connections on port 7514 and answers their database requests.
package server

import (
	"time"

	"dbsrv/internal/cluster"
)

const (
	// MaxChannels mirrors the client's MAX_SERVER: TMSRV channels per DBSRV
	// (the C++ DBSRV allowed 3).
	MaxChannels = cluster.MaxChannels
	// MaxClients mirrors MAX_PLAYER: client ids per channel.
	MaxClients = 1000
)

// Config holds the server settings.
type Config struct {
	Listen string // ":7514"

	// Group is the server (group) this DBSRV serves: new guilds must be in
	// its range of guild numbers (cluster.GuildRange). 0 counts as 1.
	Group int

	// ChannelIPs[i] is the source IP channel i+1 connects from (the
	// channels of this server in tmsrv.yaml; formerly serverlist.txt). Several
	// channels may share an IP (tests); the first free one is used.
	ChannelIPs []string

	// MasterPasswordHash, when set, is an argon2id hash of a staff password
	// that logs into any account and skips bans and the numeric password.
	// The C++ server hard-coded "DEFINAASENHAMESTRAAQUI"; it is off by default here.
	MasterPasswordHash string

	// Loc is the game's local time zone (bans are wall-clock dates).
	Loc *time.Location

	// ChangeServerTokenTTL bounds how long a 0x52A one-time login token lives.
	ChangeServerTokenTTL time.Duration

	// DeliveryInterval is how often the deliveries table is polled (1s).
	DeliveryInterval time.Duration

	// StatusFile, if set, receives the online count per channel every 30s
	// (the C++ wrote C:\xampp\htdocs\servNN.htm).
	StatusFile string
}
