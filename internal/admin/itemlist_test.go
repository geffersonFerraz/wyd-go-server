package admin

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// memAsset is an AssetSource with one file.
type memAsset struct {
	content []byte
	at      time.Time
}

func (m *memAsset) AssetUpdatedAt(context.Context, string) (time.Time, bool, error) {
	return m.at, m.content != nil, nil
}

func (m *memAsset) LoadAsset(context.Context, string) ([]byte, time.Time, error) {
	return m.content, m.at, nil
}

func TestItemListHandler(t *testing.T) {
	want := bytes.Repeat([]byte{0x5A}, 6500*156)
	mux := http.NewServeMux()
	mux.Handle("GET "+ItemListPath, ItemListHandler(&memAsset{content: want, at: time.Now()}, slog.New(slog.DiscardHandler)))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	res, err := http.Get(srv.URL + ItemListPath)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !bytes.Equal(got, want) {
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

func TestItemListHandlerMissing(t *testing.T) {
	srv := httptest.NewServer(ItemListHandler(&memAsset{}, slog.New(slog.DiscardHandler)))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing file: %v %v", res.StatusCode, err)
	}
	res.Body.Close()
}
