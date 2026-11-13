package admin

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"dbsrv/internal/cluster"
)

func TestServerListHandlerServesShippedSettings(t *testing.T) {
	srv := httptest.NewServer(ServerListHandler("../../tmsrv.yaml", slog.New(slog.DiscardHandler)))
	defer srv.Close()
	res, err := http.Get(srv.URL + ServerListPath)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || len(b) != cluster.ServerListSize {
		t.Fatalf("status %d, %d bytes", res.StatusCode, len(b))
	}
	want, err := cluster.Load("../../tmsrv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got, err := cluster.DecodeServerList(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range want.Servers {
		g := s.Group - 1
		if got[g][0] != s.StatusURL || got[g][1] != s.Address(1) {
			t.Errorf("group %d: %q", g+1, got[g][:3])
		}
	}

	bad := httptest.NewServer(ServerListHandler("missing.yaml", slog.New(slog.DiscardHandler)))
	defer bad.Close()
	if res, err := http.Get(bad.URL); err != nil || res.StatusCode != http.StatusInternalServerError {
		t.Errorf("missing settings: %v %v", res.StatusCode, err)
	}
}
