package admin

import (
	"embed"
	"io/fs"
	"net/http"
)

// LauncherPath is where the public listener serves the page shown inside the
// game launcher.
const LauncherPath = "/launcher/"

//go:embed launcher/*
var launcherFiles embed.FS

// LauncherHandler serves the launcher page and whatever sits next to it.
func LauncherHandler() http.Handler {
	files, err := fs.Sub(launcherFiles, "launcher")
	if err != nil {
		panic(err) // the path is fixed at build time
	}
	server := http.StripPrefix(LauncherPath, http.FileServerFS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		server.ServeHTTP(w, r)
	})
}
