package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestDeliverItem(t *testing.T) {
	h := newPanel(t)
	seedGameData(h)
	csrf := h.login()

	if _, body, _ := h.get("/accounts/EXISTING"); !strings.Contains(body, "Entregar item") || !strings.Contains(body, "61 · Quantidade") {
		t.Fatal("no item form on the account page")
	}
	form := func(kv ...string) url.Values {
		v := url.Values{"csrf": {csrf}, "amount": {"1"}, "sanc": {"0"}, "copies": {"1"}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v
	}

	// Unknown item: refused, the typed values stay.
	code, body, _ := h.post("/accounts/EXISTING/item", form("item_id", "912"))
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "não existe") || !strings.Contains(body, `value="912"`) {
		t.Fatalf("unknown item: %d", code)
	}
	// Too many effects.
	code, body, _ = h.post("/accounts/EXISTING/item", form("item_id", "911", "amount", "5", "sanc", "7",
		"ef0", "2", "ev0", "10", "ef1", "2", "ev1", "20"))
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "no máximo 3") {
		t.Fatalf("4 effects: %d", code)
	}

	// Stack of 5, +11, one bonus, two copies.
	code, _, hdr := h.post("/accounts/EXISTING/item", form("item_id", "911", "amount", "5", "sanc", "11",
		"ef0", "2", "ev0", "45", "copies", "2"))
	if code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "req=") {
		t.Fatalf("deliver: %d", code)
	}
	list, _ := h.st.PendingDeliveries(h.ctx, 10)
	if len(list) != 2 {
		t.Fatalf("%d deliveries, want 2", len(list))
	}
	kind, user, by, payload := h.lastPayload()
	effects, _ := payload["effects"].([]any)
	if kind != "item" || user != "EXISTING" || by != "admin" || payload["item_id"] != float64(911) || len(effects) != 3 {
		t.Fatalf("payload %s %s %s %v", kind, user, by, payload)
	}
	want := [][2]float64{{61, 5}, {43, 234}, {2, 45}}
	for i, e := range effects {
		p := e.([]any)
		if p[0] != want[i][0] || p[1] != want[i][1] {
			t.Errorf("effect %d = %v, want %v", i, p, want[i])
		}
	}

	// The request list names the item.
	if _, body, _ := h.get("/accounts/EXISTING"); !strings.Contains(body, "Solaris [911] ×5 &#43;11 (2:45)") {
		t.Error("item label missing from the request list")
	}
}

func TestSancLabels(t *testing.T) {
	for sanc := range 16 {
		if got := sancLevel(sancValue(sanc)); got != sanc {
			t.Errorf("+%d -> %d -> +%d", sanc, sancValue(sanc), got)
		}
	}
}
