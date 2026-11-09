package admin

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"dbsrv/internal/store"
)

// ItemListPath is where the public listener serves the game client's
// ItemList.bin.
const ItemListPath = "/itemlist.bin"

// itemListRecheck is how often the database is asked whether the file changed.
const itemListRecheck = 5 * time.Second

// AssetSource is where the public files come from (the store).
type AssetSource interface {
	AssetUpdatedAt(ctx context.Context, name string) (time.Time, bool, error)
	LoadAsset(ctx context.Context, name string) ([]byte, time.Time, error)
}

// ItemListHandler serves the ItemList.bin of the database, so replacing it
// needs no restart. Every request asks again; an unchanged file costs a 304.
// The listener is public, so the file is kept in memory and the database is
// only asked for its date, every few seconds.
func ItemListHandler(src AssetSource, log *slog.Logger) http.Handler {
	var (
		mu      sync.Mutex
		content []byte
		modTime time.Time
		checked time.Time
	)
	current := func(ctx context.Context) ([]byte, time.Time) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(checked) < itemListRecheck {
			return content, modTime
		}
		at, ok, err := src.AssetUpdatedAt(ctx, store.AssetItemList)
		if err != nil {
			log.Error("itemlist.bin", "err", err)
			return content, modTime // the last good copy
		}
		checked = time.Now()
		switch {
		case !ok:
			content, modTime = nil, time.Time{}
		case !at.Equal(modTime) || content == nil:
			b, loaded, err := src.LoadAsset(ctx, store.AssetItemList)
			if err != nil {
				log.Error("itemlist.bin", "err", err)
				checked = time.Time{}
				return content, modTime
			}
			content, modTime = b, loaded
		}
		return content, modTime
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, at := current(r.Context())
		if b == nil {
			http.Error(w, "itemlist unavailable", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="ItemList.bin"`)
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "", at, bytes.NewReader(b))
	})
}
