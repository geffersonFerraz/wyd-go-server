package admin

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"dbsrv/internal/store"
)

func TestGMCommandQueued(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	if err := h.st.Heartbeat(h.ctx, store.DeliveryProtocol); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.post("/gm", url.Values{"csrf": {csrf}, "channel": {"1"}, "command": {"+coin"}, "args": {"5"}}); code != http.StatusUnprocessableEntity {
		t.Fatalf("no character: %d", code)
	}
	if code, _, _ := h.post("/gm", url.Values{"csrf": {csrf}, "channel": {"1"}, "command": {"+inventado"}}); code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown command: %d", code)
	}
	if code, _, _ := h.post("/gm", url.Values{"csrf": {csrf}, "channel": {"1"}, "char": {"Heroi"}, "command": {"+coin"}, "args": {"5"}}); code != http.StatusSeeOther {
		t.Fatalf("send: %d", code)
	}
	list, err := h.st.GMCommands(h.ctx, 5)
	if err != nil || len(list) != 1 || list[0].Command != "+coin 5" || list[0].Char != "Heroi" || list[0].Channel != 1 {
		t.Fatalf("queued %+v err %v", list, err)
	}
	if err := h.st.SetGMCommandOutput(h.ctx, list[0].ID, "Gold setado para: 5."); err != nil {
		t.Fatal(err)
	}
	code, body, _ := h.get("/gm")
	if code != http.StatusOK || !strings.Contains(body, "Gold setado para: 5.") {
		t.Fatalf("page: %d", code)
	}
}

func TestGMMessagesPage(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	if err := h.st.AddGMMessage(h.ctx, store.GMMessage{Channel: 2, Username: "existing", CharName: "Heroi", Text: "Fiquei preso em Armia"}); err != nil {
		t.Fatal(err)
	}
	code, body, _ := h.get("/accounts")
	if code != http.StatusOK || !strings.Contains(body, `href="/messages">Mensagens <span class="pill bad">1</span>`) {
		t.Fatalf("no unread badge: %d", code)
	}
	if code, body, _ = h.get("/messages"); code != http.StatusOK || !strings.Contains(body, "Fiquei preso") {
		t.Fatalf("messages: %d", code)
	}
	if code, _, _ := h.post("/messages/0/seen", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("mark: %d", code)
	}
	if n, _ := h.st.UnseenGMMessages(h.ctx); n != 0 {
		t.Fatalf("%d unread", n)
	}
}

func TestDropPageSavesRateLines(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	if err := h.st.SaveDrop(h.ctx, "", &store.Drop{Name: "Lobo", MaxDrops: 1}, "t"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {csrf}, "old_name": {"Lobo"}, "name": {"Lobo"}, "max_drops": {"1"}, "gold": {"0"}, "rate_gold": {"50"}, "rows": {"0"}, "rate_rows": {"2"},
		"rslot0": {"3"}, "ritem0": {"412"}, "rchance0": {"9"}, "ref0_0": {"43"}, "rval0_0": {"2"}, "ref0_1": {"0"}, "rval0_1": {"0"}, "ref0_2": {"0"}, "rval0_2": {"0"},
		"rslot1": {"4"}, "ritem1": {""}}
	if code, _, _ := h.post("/gamedata/drop", form); code != http.StatusSeeOther {
		t.Fatalf("save: %d", code)
	}
	d, err := h.st.GetDrop(h.ctx, "Lobo")
	if err != nil || d.RateGold != 50 || len(d.Rate) != 1 || d.Rate[0].Slot != 3 || d.Rate[0].Chance != 9 || d.Rate[0].Effects[0].Effect != 43 {
		t.Fatalf("saved %+v err %v", d, err)
	}
	form.Set("rate_rows", "2")
	form.Set("rslot1", "3")
	form.Set("ritem1", "413")
	form.Set("rchance1", "0")
	for _, k := range []string{"ref1_0", "rval1_0", "ref1_1", "rval1_1", "ref1_2", "rval1_2"} {
		form.Set(k, "0")
	}
	if code, _, _ := h.post("/gamedata/drop", form); code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate slot: %d", code)
	}
}

func TestDropRatesPage(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	form := url.Values{"csrf": {csrf}, "blocked": {"412, 413"}}
	for i := range 64 {
		form.Set(fmt.Sprintf("base%d", i), "100")
		form.Set(fmt.Sprintf("rate%d", i), "200")
	}
	if code, _, _ := h.post("/gamedata/droprates", form); code != http.StatusSeeOther {
		t.Fatalf("save: %d", code)
	}
	slots, _ := h.st.DropSlots(h.ctx)
	blocked, _ := h.st.BlockedDropItems(h.ctx)
	if len(slots) != 64 || slots[9].Base != 100 || slots[9].Rate != 200 || len(blocked) != 2 {
		t.Fatalf("slots %d blocked %v", len(slots), blocked)
	}
	if code, body, _ := h.get("/gamedata/droprates"); code != http.StatusOK || !strings.Contains(body, "Taxas de drop") {
		t.Fatalf("page %d", code)
	}
}

func TestMacBanRemovedInPanel(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	mac := [8]byte{1, 2, 3, 4, 5, 6, 0, 0}
	if err := h.st.SetMacBan(h.ctx, mac, true); err != nil {
		t.Fatal(err)
	}
	if code, body, _ := h.get("/gm"); code != http.StatusOK || !strings.Contains(body, "0102030405060000") {
		t.Fatalf("page: %d", code)
	}
	if code, _, _ := h.post("/macbans/zz/remove", url.Values{"csrf": {csrf}}); code != http.StatusNotFound {
		t.Fatalf("bad mac: %d", code)
	}
	if code, _, _ := h.post("/macbans/0102030405060000/remove", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("remove: %d", code)
	}
	if list, _ := h.st.MacBans(h.ctx); len(list) != 0 {
		t.Fatalf("still banned: %v", list)
	}
}

func TestStoresPage(t *testing.T) {
	h := newPanel(t)
	h.login()
	if err := h.st.AddStoreSale(h.ctx, store.StoreSale{Channel: 1, Buyer: "Comprador", Seller: "Vendedor", Item: 412, Price: 9, Category: 3}); err != nil {
		t.Fatal(err)
	}
	if code, body, _ := h.get("/stores"); code != http.StatusOK || !strings.Contains(body, "Comprador") || !strings.Contains(body, "Barras") {
		t.Fatalf("page: %d", code)
	}
}
