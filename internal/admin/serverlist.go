package admin

import (
	"log/slog"
	"net/http"

	"dbsrv/internal/cluster"
)

// ServerListPath is where the public listener serves the client's
// serverlist.bin, built from the servers of tmsrv.yaml (all of them).
const ServerListPath = "/serverlist.bin"

// ServerListHandler serves serverlist.bin. The settings file is read on
// every request, so a server or channel added to it shows up without a restart.
func ServerListHandler(configPath string, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := BuildServerList(configPath)
		if err != nil {
			log.Error("serverlist.bin", "err", err)
			http.Error(w, "serverlist unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="serverlist.bin"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
}

// BuildServerList encodes serverlist.bin from the settings file.
func BuildServerList(configPath string) ([]byte, error) {
	c, err := cluster.Load(configPath)
	if err != nil {
		return nil, err
	}
	return cluster.EncodeServerList(c.Table())
}
