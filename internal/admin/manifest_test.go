package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func getManifest(t *testing.T, url string) Manifest {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, headers %v", res.StatusCode, res.Header)
	}
	var m Manifest
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

// The hashes are those of the bytes the file handlers serve: a launcher
// that trusts the manifest must end up with the same file.
func TestManifestMatchesServedFiles(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	items := NewItemListCache(&memAsset{content: bytes.Repeat([]byte{0x5A}, 6500*156), at: time.Now()}, log)
	mux := http.NewServeMux()
	mux.Handle("GET "+ServerListPath, ServerListHandler("../../tmsrv.yaml", log))
	mux.Handle("GET "+ItemListPath, items.Handler())
	mux.Handle("GET "+ManifestPath, ManifestHandler([]ManifestSource{ServerListSource("../../tmsrv.yaml"), items.Source()}, log))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	m := getManifest(t, srv.URL+ManifestPath)
	if m.Version != ManifestVersion || len(m.Files) != 2 {
		t.Fatalf("%+v", m)
	}
	if m.Files[0].Name != "serverlist.bin" || m.Files[1].Name != "ItemList.bin" {
		t.Fatalf("names: %+v", m.Files)
	}
	for _, f := range m.Files {
		res, err := http.Get(srv.URL + f.Path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		sum := sha256.Sum256(body)
		if len(body) != f.Size || hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Errorf("%s: manifest says %d bytes %s, served %d bytes %x", f.Name, f.Size, f.SHA256, len(body), sum)
		}
	}
}

// A file that cannot be built is left out instead of failing the whole
// manifest: the launcher keeps its local copy of that one.
func TestManifestLeavesOutUnavailableFiles(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	sources := []ManifestSource{
		ServerListSource("missing.yaml"),
		NewItemListCache(&memAsset{}, log).Source(),
		{Name: "ok.bin", Path: "/ok.bin", Content: func(context.Context) ([]byte, error) { return []byte("abc"), nil }},
		{Name: "bad.bin", Path: "/bad.bin", Content: func(context.Context) ([]byte, error) { return nil, errors.New("boom") }},
	}
	srv := httptest.NewServer(ManifestHandler(sources, log))
	defer srv.Close()

	m := getManifest(t, srv.URL)
	if len(m.Files) != 1 || m.Files[0].Name != "ok.bin" || m.Files[0].Size != 3 ||
		m.Files[0].SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("%+v", m.Files)
	}
}

func TestManifestWithoutFilesIsAnEmptyList(t *testing.T) {
	srv := httptest.NewServer(ManifestHandler(nil, slog.New(slog.DiscardHandler)))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if string(body) != `{"version":1,"files":[]}` {
		t.Fatalf("%s", body)
	}
}
