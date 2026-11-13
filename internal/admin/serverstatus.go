package admin

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/store"
)

// The game client's server list reads the online count of each channel
// from g_pServerList[group][0] (serverlist.bin), e.g.
// http://host/serv00.htm, with
//
//	sscanf(buf, "%d\\n%d\\n%d\\n...")
//
// i.e. numbers separated by a literal backslash + "n" (not a newline).
// Value 0 is not shown; values 1..10 are channels 1..10, and a negative
// value shows the channel as offline. Two optional trailing values (week
// and day of the castle rotation) are left out: the client then uses its
// own calendar. (The C++ DBSRV wrote "n n n " with spaces, which this
// client parses as a single number.)

// statusChannels is MAX_SERVERNUMBER-1: channel slots the client reads.
const statusChannels = 10

var statusFileRe = regexp.MustCompile(`^serv[0-9]{2}\.htm$`)

type channelStatusSource interface {
	ChannelStatus(ctx context.Context) ([]int, time.Duration, error)
}

// StatusSource is the servNN.htm of one server: File is the name in its
// status_url (serverlist.bin points each server group at its own file).
type StatusSource struct {
	File  string
	Store *store.Store
}

// ServerStatusHandler serves the servNN.htm of each server for the game
// client. It shows every channel offline when that server's dbsrv has
// stopped heartbeating. With a single server any servNN.htm is its status.
func ServerStatusHandler(log *slog.Logger, sources ...StatusSource) http.Handler {
	byFile := map[string]channelStatusSource{}
	for _, s := range sources {
		byFile[strings.ToLower(s.File)] = s.Store
	}
	var only channelStatusSource
	if len(sources) == 1 {
		only = sources[0].Store
	}
	return serverStatusHandler(byFile, only, log)
}

func serverStatusHandler(byFile map[string]channelStatusSource, only channelStatusSource, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", func(w http.ResponseWriter, r *http.Request) {
		file := strings.ToLower(r.PathValue("file"))
		if !statusFileRe.MatchString(file) {
			http.NotFound(w, r)
			return
		}
		st := byFile[file]
		if st == nil {
			st = only
		}
		if st == nil {
			http.NotFound(w, r) // not the status file of any server
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		online, age, err := st.ChannelStatus(ctx)
		if err != nil {
			log.Warn("server status", "err", err)
			online = nil
		} else if age < 0 || age > heartbeatMaxAge {
			online = nil // dbsrv is down: no channel can take a login
		}
		w.Header().Set("Content-Type", "text/plain; charset=us-ascii")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(formatServerStatus(online)))
	})
	return mux
}

// formatServerStatus renders the counts (index i = channel i+1; missing
// channels are offline) in the client's format.
func formatServerStatus(online []int) string {
	vals := make([]string, 0, statusChannels+1)
	total := 0
	for i := 0; i < statusChannels; i++ {
		n := -1
		if i < len(online) {
			n = online[i]
		}
		if n > 0 {
			total += n
		}
		vals = append(vals, strconv.Itoa(n))
	}
	vals = append([]string{strconv.Itoa(total)}, vals...)
	return strings.Join(vals, `\n`) + `\n`
}
