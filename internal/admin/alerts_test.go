package admin

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbsrv/internal/store"
)

func TestAlertsPage(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	if _, err := h.st.AddAlert(h.ctx, store.Alert{Kind: store.AlertKindWPE, Username: "EXISTING", CharName: "Heroi", IP: "10.0.0.7", Channel: 1, Detail: "detectou o WPE"}); err != nil {
		t.Fatal(err)
	}
	code, body, _ := h.get("/accounts")
	if code != http.StatusOK || !strings.Contains(body, `href="/alerts">Alertas <span class="pill bad">1</span>`) {
		t.Fatalf("no unseen badge in the menu: %d", code)
	}
	code, body, _ = h.get("/alerts")
	if code != http.StatusOK || !strings.Contains(body, "Heroi") || !strings.Contains(body, "10.0.0.7") || !strings.Contains(body, "WPE") {
		t.Fatalf("alerts page: %d %s", code, body)
	}
	if code, _, _ := h.post("/alerts/0/seen", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("mark all: %d", code)
	}
	if n, _ := h.st.UnseenAlerts(h.ctx); n != 0 {
		t.Fatalf("%d unseen", n)
	}
}

func TestCitiesSeedAndReset(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	zone := filepath.Join(t.TempDir(), "guild_zone.txt")
	os.WriteFile(zone, []byte("0,5,0,0\n1,1,0,0\n2,0,0,0\n"), 0o644)
	if err := h.st.SetCityOwner(h.ctx, 1, 9, "admin"); err != nil {
		t.Fatal(err)
	}
	n, err := SeedCityOwners(h.ctx, h.st, zone)
	if err != nil || n != 2 {
		t.Fatalf("seeded %d err %v", n, err)
	}
	owners, _ := h.st.CityOwners(h.ctx)
	got := map[int]int{}
	for _, o := range owners {
		got[o.City] = o.GuildID
	}
	if got[0] != 5 || got[1] != 9 || got[2] != 0 {
		t.Fatalf("owners after seeding %v (the panel's city 1 must be kept)", got)
	}
	if code, _, _ := h.post("/cities/reset", url.Values{"csrf": {csrf}}); code != http.StatusOK {
		t.Fatalf("reset: %d", code)
	}
	owners, _ = h.st.CityOwners(h.ctx)
	for _, o := range owners {
		if o.GuildID != 0 {
			t.Fatalf("city %d still owned by %d", o.City, o.GuildID)
		}
	}
	if len(owners) != store.MaxCity {
		t.Fatalf("%d rows after reset", len(owners))
	}
}
