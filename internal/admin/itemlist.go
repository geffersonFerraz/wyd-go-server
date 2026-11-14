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

// ItemListCache keeps the ItemList.bin of the database in memory. The public
// listener serves it to anyone, so the database is only asked for the file's
// date, every few seconds; the file handler and the manifest share one copy.
type ItemListCache struct {
	src AssetSource
	log *slog.Logger

	mu      sync.Mutex
	content []byte
	modTime time.Time
	checked time.Time
}

// NewItemListCache starts empty; the file is loaded on the first request.
func NewItemListCache(src AssetSource, log *slog.Logger) *ItemListCache {
	return &ItemListCache{src: src, log: log}
}

// Current is the file and its date; nil when the database has none. On a
// database error it answers with the last good copy.
func (c *ItemListCache) Current(ctx context.Context) ([]byte, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.checked) < itemListRecheck {
		return c.content, c.modTime
	}
	at, ok, err := c.src.AssetUpdatedAt(ctx, store.AssetItemList)
	if err != nil {
		c.log.Error("itemlist.bin", "err", err)
		return c.content, c.modTime
	}
	c.checked = time.Now()
	switch {
	case !ok:
		c.content, c.modTime = nil, time.Time{}
	case !at.Equal(c.modTime) || c.content == nil:
		b, loaded, err := c.src.LoadAsset(ctx, store.AssetItemList)
		if err != nil {
			c.log.Error("itemlist.bin", "err", err)
			c.checked = time.Time{}
			return c.content, c.modTime
		}
		c.content, c.modTime = b, loaded
	}
	return c.content, c.modTime
}

// ItemListHandler serves the ItemList.bin of the database, so replacing it
// needs no restart. Every request asks again; an unchanged file costs a 304.
func ItemListHandler(src AssetSource, log *slog.Logger) http.Handler {
	return NewItemListCache(src, log).Handler()
}

// Handler serves the cached file.
func (c *ItemListCache) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, at := c.Current(r.Context())
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
