package admin

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestItemListHandler(t *testing.T) {
	want, err := os.ReadFile("../../data/client/ItemList.bin")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET "+ItemListPath, ItemListHandler("../../data/client/ItemList.bin", slog.New(slog.DiscardHandler)))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	res, err := http.Get(srv.URL + ItemListPath)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !bytes.Equal(got, want) || len(got) != 6500*156 {
		t.Fatalf("status %d, %d bytes", res.StatusCode, len(got))
	}
	if res.Header.Get("Last-Modified") == "" || res.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("headers %v", res.Header)
	}
	// Unchanged: 304, nothing to download.
	req, _ := http.NewRequest("GET", srv.URL+ItemListPath, nil)
	req.Header.Set("If-Modified-Since", res.Header.Get("Last-Modified"))
	again, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	again.Body.Close()
	if again.StatusCode != http.StatusNotModified {
		t.Fatalf("second request: %d", again.StatusCode)
	}
}

func TestItemListHandlerMissingAndReplaced(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ItemList.bin")
	srv := httptest.NewServer(ItemListHandler(p, slog.New(slog.DiscardHandler)))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file: %v %v", res.StatusCode, err)
	}
	res.Body.Close()
	os.WriteFile(p, []byte("novo"), 0o644)
	res, err = http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if b, _ := io.ReadAll(res.Body); string(b) != "novo" {
		t.Fatalf("got %q", b)
	}
}
