package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ManifestPath is where the public listener serves the list of files the
// launcher keeps up to date, each with its size and SHA-256. The launcher
// compares it with what is on disk and only downloads what changed.
const ManifestPath = "/manifest.json"

// ManifestVersion changes when the format stops being readable by older
// launchers; they then fall back to downloading every file.
const ManifestVersion = 1

// manifestTTL is how long a built manifest answers the public listener
// without hashing the files again.
const manifestTTL = 5 * time.Second

// Manifest is the JSON document served at ManifestPath.
type Manifest struct {
	Version int            `json:"version"`
	Files   []ManifestFile `json:"files"`
}

// ManifestFile is one downloadable file. Name is how the client spells it
// on disk; Path is where this listener serves it.
type ManifestFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

// ManifestSource is one file of the manifest. Content returns the bytes
// being served right now; nil or an error leaves the file out, and the
// launcher keeps its local copy.
type ManifestSource struct {
	Name    string
	Path    string
	Content func(ctx context.Context) ([]byte, error)
}

// ServerListSource is the manifest entry of serverlist.bin.
func ServerListSource(configPath string) ManifestSource {
	return ManifestSource{Name: "serverlist.bin", Path: ServerListPath,
		Content: func(context.Context) ([]byte, error) { return BuildServerList(configPath) }}
}

// Source is the manifest entry of ItemList.bin.
func (c *ItemListCache) Source() ManifestSource {
	return ManifestSource{Name: "ItemList.bin", Path: ItemListPath,
		Content: func(ctx context.Context) ([]byte, error) {
			b, _ := c.Current(ctx)
			return b, nil
		}}
}

// BuildManifest hashes the current content of every source.
func BuildManifest(ctx context.Context, sources []ManifestSource, log *slog.Logger) Manifest {
	m := Manifest{Version: ManifestVersion, Files: []ManifestFile{}}
	for _, s := range sources {
		b, err := s.Content(ctx)
		if err != nil {
			log.Error("manifest", "file", s.Name, "err", err)
			continue
		}
		if b == nil {
			continue
		}
		sum := sha256.Sum256(b)
		m.Files = append(m.Files, ManifestFile{Name: s.Name, Path: s.Path, Size: len(b), SHA256: hex.EncodeToString(sum[:])})
	}
	return m
}

// ManifestHandler serves the manifest. The listener is public, so the
// document is rebuilt at most once per manifestTTL.
func ManifestHandler(sources []ManifestSource, log *slog.Logger) http.Handler {
	var (
		mu    sync.Mutex
		body  []byte
		built time.Time
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if body == nil || time.Since(built) >= manifestTTL {
			b, err := json.Marshal(BuildManifest(r.Context(), sources, log))
			if err != nil {
				mu.Unlock()
				log.Error("manifest", "err", err)
				http.Error(w, "manifest unavailable", http.StatusInternalServerError)
				return
			}
			body, built = b, time.Now()
		}
		out := body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(out)
	})
}
