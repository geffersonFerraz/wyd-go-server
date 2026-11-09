package admin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLauncherHandlerServesThePage(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET "+LauncherPath, LauncherHandler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := http.Get(srv.URL + LauncherPath)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "X-UA-Compatible") {
		t.Fatalf("status %d, body %q", res.StatusCode, body)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}
}
