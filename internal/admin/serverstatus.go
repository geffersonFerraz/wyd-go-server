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

// ServerStatusHandler serves servNN.htm for the game client. It shows
// every channel offline when dbsrv has stopped heartbeating.
func ServerStatusHandler(st *store.Store, log *slog.Logger) http.Handler {
	return serverStatusHandler(st, log)
}

func serverStatusHandler(st channelStatusSource, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{file}", func(w http.ResponseWriter, r *http.Request) {
		if !statusFileRe.MatchString(r.PathValue("file")) {
			http.NotFound(w, r)
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
