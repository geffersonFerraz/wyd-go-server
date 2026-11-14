package admin

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// One panel for two servers on the same database: the account is the same,
// and the selector decides whose characters are shown.
func TestPanelSwitchesServers(t *testing.T) {
	h := newPanel(t)
	// A single server has no selector.
	h.login()
	if _, body, _ := h.get("/accounts/EXISTING"); strings.Contains(body, "/server-select") {
		t.Error("server selector shown with a single server")
	}

	dsn := os.Getenv("DBSRV_TEST_DSN")
	st2, err := store.OpenServerUnprivileged(h.ctx, dsn, time.Local, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st2.Close)
	if err := st2.Migrate(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := st2.Heartbeat(h.ctx, store.DeliveryProtocol); err != nil {
		t.Fatal(err)
	}
	// A character of the same account on each server.
	for server, name := range map[int]string{1: "HeroOne", 2: "HeroTwo"} {
		w, err := store.OpenServer(h.ctx, dsn, time.Local, server)
		if err != nil {
			t.Fatal(err)
		}
		rec, err := w.LoadAccount(h.ctx, "EXISTING")
		if err != nil {
			t.Fatal(err)
		}
		wire.PutCString(rec.Account.Mob[0].Player.Name[:], name)
		rec.Account.Mob[0].Player.Equip[0] = wire.Item{Index: 1}
		if err := w.SaveAccount(h.ctx, rec); err != nil {
			t.Fatal(err)
		}
		w.Close()
	}

	p2, err := New(h.panel.cfg, st2, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(Handler(h.panel, p2))
	t.Cleanup(ts.Close)
	h.ts = ts
	csrf := h.login()

	_, body, _ := h.get("/accounts/EXISTING")
	if !strings.Contains(body, "HeroOne") || strings.Contains(body, "HeroTwo") {
		t.Error("the first server should be shown until another is picked")
	}
	if !strings.Contains(body, `action="/server-select"`) {
		t.Fatal("no server selector with two servers")
	}

	// Without the CSRF token nothing changes.
	if code, _, _ := h.post("/server-select", url.Values{"server": {"2"}}); code != http.StatusForbidden {
		t.Errorf("select without csrf: %d", code)
	}
	if code, _, _ := h.post("/server-select", url.Values{"server": {"9"}, "csrf": {csrf}}); code != http.StatusBadRequest {
		t.Errorf("unknown server: %d", code)
	}
	if code, _, hdr := h.post("/server-select", url.Values{"server": {"2"}, "csrf": {csrf}}); code != http.StatusSeeOther || hdr.Get("Location") != "/accounts" {
		t.Fatalf("select server 2: %d %s", code, hdr.Get("Location"))
	}
	_, body, _ = h.get("/accounts/EXISTING")
	if !strings.Contains(body, "HeroTwo") || strings.Contains(body, "HeroOne") {
		t.Error("server 2 selected but its characters are not the ones shown")
	}

	// What is queued goes to the DBSRV of the selected server.
	if code, _, _ := h.post("/accounts/EXISTING/kick", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("kick: %d", code)
	}
	if p, _ := st2.PendingDeliveries(h.ctx, 10); len(p) != 1 || p[0].Kind != "kick" {
		t.Errorf("deliveries of server 2: %+v", p)
	}
	if p, _ := h.st.PendingDeliveries(h.ctx, 10); len(p) != 0 {
		t.Errorf("deliveries of server 1: %+v", p)
	}

	// Back to server 1; the login is the same session.
	if code, _, _ := h.post("/server-select", url.Values{"server": {"1"}, "csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("select server 1: %d", code)
	}
	if _, body, _ = h.get("/accounts/EXISTING"); !strings.Contains(body, "HeroOne") {
		t.Error("back on server 1 without its characters")
	}
}
