package admin

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"dbsrv/internal/store"
)

func TestTrackPassages(t *testing.T) {
	ev := func(kind, from, to string) store.ItemTrackEvent {
		return store.ItemTrackEvent{Kind: kind, FromAccount: from, ToAccount: to, At: time.Now()}
	}
	// A change of effects is not a passage: it stays in ANA's hands.
	changed := newTrackRow(store.ItemTrack{Status: "active", HolderAccount: "ANA", Events: []store.ItemTrackEvent{
		ev(store.TrackPickup, "", "ANA"), ev(store.TrackChanged, "ANA", "ANA")}})
	if p := trackPassages(changed, "ANA"); len(changed.Chain) != 1 || len(p) != 1 || !p[0].Holding {
		t.Fatalf("changed: %+v %+v", changed.Chain, p)
	}
	// ANA picks it up, moves it between her characters through the ground,
	// trades it to BIA, gets it back in a store and still has it.
	row := newTrackRow(store.ItemTrack{Status: "active", HolderAccount: "ANA", Mob: "Lobo", Events: []store.ItemTrackEvent{
		ev(store.TrackPickup, "", "ANA"), ev(store.TrackGround, "ANA", "ANA"), ev(store.TrackTrade, "ANA", "BIA"),
		ev(store.TrackStore, "BIA", "ANA")}})
	if len(row.Chain) != 3 || row.Chain[0].Account != "ANA" || row.Chain[1].Account != "BIA" || row.Chain[2].Account != "ANA" {
		t.Fatalf("chain: %+v", row.Chain)
	}
	ana := trackPassages(row, "ANA")
	if len(ana) != 2 || ana[0].Out == nil || ana[0].Out.Other.Account != "BIA" || ana[0].Holding ||
		ana[1].In == nil || ana[1].In.Other.Account != "BIA" || ana[1].Out != nil || !ana[1].Holding {
		t.Fatalf("ANA: %+v", ana)
	}
	if !strings.Contains(ana[0].In.Text, "Lobo") {
		t.Fatalf("origin: %q", ana[0].In.Text)
	}
	bia := trackPassages(row, "BIA")
	if len(bia) != 1 || bia[0].In == nil || bia[0].Out == nil || bia[0].Holding {
		t.Fatalf("BIA: %+v", bia)
	}
	// An item that left the game from the account, and one whose arrival was not recorded.
	gone := newTrackRow(store.ItemTrack{Status: "gone", Events: []store.ItemTrackEvent{ev(store.TrackNPCBuy, "", "ANA"), ev(store.TrackGone, "ANA", "")}})
	if p := trackPassages(gone, "ANA"); len(p) != 1 || p[0].Out == nil || p[0].Out.Other.Account != "" || p[0].Holding {
		t.Fatalf("gone: %+v", p)
	}
	lost := newTrackRow(store.ItemTrack{Status: "active", HolderAccount: "ANA"})
	if p := trackPassages(lost, "ANA"); len(p) != 1 || p[0].In != nil || !p[0].Holding {
		t.Fatalf("no arrival: %+v", p)
	}
}

func TestTrackItemLabel(t *testing.T) {
	got := trackItemLabel("Espada", 1103, [3]store.ItemEffect{{Effect: efSanc, Value: 9}, {Effect: 2, Value: 12}})
	if got != "Espada [1103] +9 (Aumento de Dano 12)" {
		t.Fatal(got)
	}
}

func TestTracksPages(t *testing.T) {
	h := newPanel(t)
	h.login()
	if err := h.st.SaveItem(h.ctx, &store.Item{ID: 1103, Name: "Espada_Longa"}, "test"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, e := range []store.ItemTrackEvent{
		{TrackID: 7, Kind: store.TrackPickup, ToAccount: "ANA", ToChar: "Ana", Item: 1103, Mob: "Lobo", DroppedAt: now, KillerAccount: "ANA", KillerChar: "Ana"},
		{TrackID: 7, Kind: store.TrackTrade, FromAccount: "ANA", FromChar: "Ana", ToAccount: "BIA", ToChar: "Bia", Item: 1103},
		{TrackID: 8, Kind: store.TrackNPCBuy, ToAccount: "CAIO", ToChar: "Caio", Item: 1103, Mob: "Ferreiro", DroppedAt: now, Price: 300},
	} {
		e.At, e.Channel = now, 1
		if err := h.st.AddItemTrackEvent(h.ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	has := func(path string, want ...string) string {
		t.Helper()
		code, body, _ := h.get(path)
		if code != http.StatusOK {
			t.Fatalf("%s: %d", path, code)
		}
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Fatalf("%s: %q not on the page", path, w)
			}
		}
		return body
	}
	has("/tracks", "Últimas movimentações", "Espada_Longa [1103]", "Ferreiro")
	// The account that gave the item away still lists it, with who got it.
	body := has("/tracks?account=ana", "Itens que passaram pela conta", "Drop de Lobo", "Troca com", `href="/tracks?account=BIA"`, "/tracks/7")
	if strings.Contains(body, "/tracks/8") || !strings.Contains(body, "0 ainda na conta") {
		t.Fatal("another account's item, or an item still held, on ANA's page")
	}
	has("/tracks?account=BIA", `<span class="pill ok">ainda na conta</span>`, "1 ainda na conta")
	body = has("/tracks?item=espada", "Por onde passou o item", `href="/tracks?account=ANA"`, " → ", "/tracks/8", "Com CAIO")
	if !strings.Contains(body, "2 unidades rastreadas") {
		t.Fatal("unit count missing")
	}
	has("/tracks?item=9999", "Nenhum item rastreado com esse número ou nome")
	has("/tracks?account=ninguem", "Nenhum item rastreado passou por esta conta")
	if err := h.st.SyncItemTracks(h.ctx, 1, "BIA", "Bia", []store.ItemTrackPlace{{TrackID: 7, Type: 1, Slot: 0, Item: 1103,
		Effects: [3]store.ItemEffect{{Effect: efSanc, Value: 9}}}}); err != nil {
		t.Fatal(err)
	}
	has("/tracks/7", "Efeitos mudaram", `<span class="muted">Espada_Longa [1103]</span> → Espada_Longa [1103] &#43;9`)
	has("/tracks/7", "Histórico", "Pegou o drop", "Troca", "Conta de quem matou", "Com BIA", "Era")
	for _, p := range []string{"/tracks/999", "/tracks/abc"} {
		if code, _, _ := h.get(p); code != http.StatusNotFound {
			t.Fatalf("%s: %d", p, code)
		}
	}
	has("/accounts/EXISTING", `/tracks?account=EXISTING`)
}
