// Package server is the DBSRV TCP server: it accepts TMSRV channel
// connections on port 7514 and answers their database requests.
package server

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxChannels mirrors MAX_SERVER: TMSRV channels per DBSRV.
	MaxChannels = 3
	// MaxClients mirrors MAX_PLAYER: client ids per channel.
	MaxClients = 1000
)

// Config holds the server settings.
type Config struct {
	Listen string // ":7514"

	// ChannelIPs[i] is the source IP channel i connects from, i.e.
	// serverlist.txt rows "<group> <i+1> <ip>". Several channels may share
	// an IP; the first free one is used.
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

// ReadServerList returns the channel IPs for one group of serverlist.txt
// ("group number address" per line; number 0 is the DBSRV itself).
func ReadServerList(path string, group int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	byNumber := map[int]string{}
	maxNum := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		g, err1 := strconv.Atoi(fields[0])
		n, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil || g != group || n < 1 || n > MaxChannels {
			continue
		}
		byNumber[n] = fields[2]
		maxNum = max(maxNum, n)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if maxNum == 0 {
		return nil, fmt.Errorf("%s: no channels for group %d", path, group)
	}
	ips := make([]string, maxNum)
	for n, ip := range byNumber {
		ips[n-1] = ip
	}
	return ips, nil
}
