package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"dbsrv/internal/wire"
)

func TestAccountEditor(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()

	rec, err := h.st.LoadAccount(h.ctx, "EXISTING")
	if err != nil {
		t.Fatal(err)
	}
	pl := &rec.Account.Mob[1].Player
	wire.PutCString(pl.Name[:], "Heroi")
	pl.Gold = 321
	pl.BStatus.Level = 9
	pl.Equip[0].Effect[1].Value = 1
	pl.Inventory[3] = wire.Item{Index: 412, Effect: [3]wire.ItemEffect{{Index: 61, Value: 2}}}
	rec.Account.StorageItems[0] = wire.Item{Index: 413}
	if err := h.st.SaveAccount(h.ctx, rec); err != nil {
		t.Fatal(err)
	}

	code, body, _ := h.get("/accounts/EXISTING/chars/1?where=1&slot=3")
	if code != http.StatusOK || !strings.Contains(body, "Heroi") || !strings.Contains(body, `id="editor"`) ||
		!strings.Contains(body, `name="expect" value="412,61,2,0,0,0,0"`) {
		t.Fatalf("character page %d", code)
	}
	if code, _, _ := h.get("/accounts/EXISTING/chars/2"); code != http.StatusNotFound {
		t.Fatalf("empty character slot: %d", code)
	}
	if code, body, _ := h.get("/accounts/EXISTING/storage"); code != http.StatusOK || !strings.Contains(body, "[413]") {
		t.Fatalf("storage page %d", code)
	}

	back := "/accounts/EXISTING/chars/1"
	code, _, hdr := h.post("/accounts/EXISTING/edit", url.Values{"csrf": {csrf}, "back": {back},
		"op": {"gold"}, "char": {"1"}, "value": {"999"}})
	if code != http.StatusSeeOther || !strings.HasPrefix(hdr.Get("Location"), back+"?req=") {
		t.Fatalf("gold edit: %d %s", code, hdr.Get("Location"))
	}
	kind, user, by, payload := h.lastPayload()
	if kind != "edit" || user != "EXISTING" || by != "admin" || payload["op"] != "gold" || payload["value"] != float64(999) || payload["char"] != float64(1) {
		t.Fatalf("queued %s %s %s %v", kind, user, by, payload)
	}

	// Remove several items in one request, each with the item the admin saw.
	h.post("/accounts/EXISTING/edit", url.Values{"csrf": {csrf}, "back": {back}, "op": {"remove"}, "char": {"1"},
		"sel": {"1:3:412,61,2,0,0,0,0", "0:6:805,0,0,0,0,0,0"}})
	_, _, _, payload = h.lastPayload()
	removes, _ := payload["removes"].([]any)
	if payload["op"] != "remove" || len(removes) != 2 || removes[0].(map[string]any)["expect"].([]any)[0] != float64(412) {
		t.Fatalf("remove payload %v", payload)
	}
	before2, _ := h.st.PendingDeliveries(h.ctx, 100)
	if _, _, loc := h.post("/accounts/EXISTING/edit", url.Values{"csrf": {csrf}, "back": {back}, "op": {"remove"}, "char": {"1"}}); !strings.Contains(loc.Get("Location"), "err=") {
		t.Fatalf("empty selection accepted: %s", loc.Get("Location"))
	}
	if after2, _ := h.st.PendingDeliveries(h.ctx, 100); len(after2) != len(before2) {
		t.Fatal("empty selection was queued")
	}

	// Readable effect names, on the page and in the editor.
	if _, body, _ := h.get("/accounts/EXISTING/chars/1?where=1&slot=3"); !strings.Contains(body, "Quantidade 2") || !strings.Contains(body, "61 · Quantidade") || strings.Contains(body, "EF_AMOUNT") {
		t.Fatal("effects are not shown with readable names")
	}

	// GM access from the account page.
	h.post("/accounts/EXISTING/edit", url.Values{"csrf": {csrf}, "back": {"/accounts/EXISTING"}, "op": {"access"}, "value": {"1"}})
	if _, _, _, payload = h.lastPayload(); payload["op"] != "access" {
		t.Fatalf("access payload %v", payload)
	}

	// Bad input comes back as an error, nothing queued; the face slot is refused.
	before, _ := h.st.PendingDeliveries(h.ctx, 100)
	_, _, hdr = h.post("/accounts/EXISTING/edit", url.Values{"csrf": {csrf}, "back": {back}, "op": {"level"}, "char": {"1"}, "value": {"0"}})
	_, _, hdr2 := h.post("/accounts/EXISTING/edit", url.Values{"csrf": {csrf}, "back": {back}, "op": {"item"}, "char": {"1"},
		"where": {"0"}, "slot": {"0"}, "item_id": {"412"}})
	_, _, hdr3 := h.post("/accounts/EXISTING/edit", url.Values{"csrf": {csrf}, "back": {"https://evil.example/"}, "op": {"gold"}, "value": {"-5"}})
	after, _ := h.st.PendingDeliveries(h.ctx, 100)
	if len(after) != len(before) || !strings.Contains(hdr.Get("Location"), "err=") || !strings.Contains(hdr2.Get("Location"), "err=") ||
		!strings.HasPrefix(hdr3.Get("Location"), "/accounts/EXISTING?err=") {
		t.Fatalf("bad edits: %d -> %d, %s / %s / %s", len(before), len(after), hdr.Get("Location"), hdr2.Get("Location"), hdr3.Get("Location"))
	}

	// The request list describes the edits.
	if _, body, _ := h.get("/accounts/EXISTING"); !strings.Contains(body, "gold de personagem 2 = 999") || !strings.Contains(body, "Acesso (GM)") {
		t.Fatal("account page does not show the edit or the access form")
	}
}

func TestStatsEditor(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()

	rec, err := h.st.LoadAccount(h.ctx, "EXISTING")
	if err != nil {
		t.Fatal(err)
	}
	pl := &rec.Account.Mob[1].Player
	wire.PutCString(pl.Name[:], "Heroi")
	pl.BStatus.STR, pl.BStatus.DEX, pl.StatusPoint = 40, 9, 5
	pl.Learn[0] = 1<<30 | 1<<4
	if err := h.st.SaveAccount(h.ctx, rec); err != nil {
		t.Fatal(err)
	}

	code, body, _ := h.get("/accounts/EXISTING/chars/1")
	for _, want := range []string{`name="str"`, `name="orig_str" value="40"`, `name="status_point"`, `name="mastery3"`, `name="skill_point"`, `name="learn_orig" value="16"`, `value="4" checked`} {
		if code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("character page %d: no %s", code, want)
		}
	}

	back := "/accounts/EXISTING/chars/1"
	form := func(str, status string, learn ...string) url.Values {
		v := url.Values{"csrf": {csrf}, "back": {back}, "op": {"stats"}, "char": {"1"}, "class": {"0"}, "learn_orig": {"16"}, "learn": learn,
			"str": {str}, "orig_str": {"40"}, "dex": {"9"}, "orig_dex": {"9"}, "status_point": {status}, "orig_status_point": {"5"}}
		return v
	}
	code, _, hdr := h.post("/accounts/EXISTING/edit", form("100", "5", "4", "6"))
	if code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "?req=") {
		t.Fatalf("stats edit: %d %s", code, hdr.Get("Location"))
	}
	_, _, _, payload := h.lastPayload()
	stats, _ := payload["stats"].(map[string]any)
	if payload["op"] != "stats" || payload["char"] != float64(1) || len(stats) != 1 || stats["str"] != float64(100) || payload["learn_add"] != float64(64) || payload["learn_del"] != nil {
		t.Fatalf("payload %v", payload)
	}
	if _, body, _ := h.get(back); !strings.Contains(body, "Força = 100") || !strings.Contains(body, "1 skills adicionadas") {
		t.Fatal("the request list does not describe the edit")
	}

	before, _ := h.st.PendingDeliveries(h.ctx, 100)
	_, _, same := h.post("/accounts/EXISTING/edit", form("40", "5", "4"))
	_, _, high := h.post("/accounts/EXISTING/edit", form("500000001", "5", "4"))
	_, _, points := h.post("/accounts/EXISTING/edit", form("40", "32768", "4"))
	_, _, badBit := h.post("/accounts/EXISTING/edit", form("40", "5", "24"))
	if after, _ := h.st.PendingDeliveries(h.ctx, 100); len(after) != len(before) {
		t.Fatalf("bad stats queued: %d -> %d", len(before), len(after))
	}
	for _, h := range []http.Header{same, high, points, badBit} {
		if !strings.Contains(h.Get("Location"), "err=") {
			t.Fatalf("no error: %s", h.Get("Location"))
		}
	}
}
