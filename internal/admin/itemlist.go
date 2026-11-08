package admin

import (
	"log/slog"
	"net/http"
	"os"
)

// ItemListPath is where the public listener serves the game client's
// ItemList.bin.
const ItemListPath = "/itemlist.bin"

// ItemListHandler serves the file as it is on disk, so replacing it needs
// no restart. Every request asks again; an unchanged file costs a 304.
func ItemListHandler(path string, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(path)
		if err != nil {
			log.Error("itemlist.bin", "err", err)
			http.Error(w, "itemlist unavailable", http.StatusNotFound)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			log.Error("itemlist.bin", "path", path, "err", err)
			http.Error(w, "itemlist unavailable", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="ItemList.bin"`)
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "", info.ModTime(), f)
	})
}
