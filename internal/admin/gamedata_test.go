package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"dbsrv/internal/store"
)

func seedGameData(h *panelHarness) {
	h.t.Helper()
	gd := &store.GameData{
		EffectNames: []store.EffectName{{ID: 2, Name: "EF_DAMAGE"}, {ID: 61, Name: "EF_AMOUNT"}},
		Items:       []store.Item{{ID: 911, Name: "Solaris", Price: 150000, Effects: [12]store.ItemEffect{{Effect: 2, Value: 333}}}},
		Skills:      []store.Skill{{ID: 27, Name: "Cura", Mana: 15, Delay: 2, InstanceType: 6}},
		Drops:       []store.Drop{{Name: "Guer Caveira ", MaxDrops: 19, Gold: 20000, Items: []store.DropItem{{Kills: 250, Item: 911, Effect: 2, Value: 45}}}},
		Teleports:   []store.Teleport{{ID: 0, SrcX: 2116, SrcY: 2100, DstX: 2480, DstY: 1716, Price: 700}},
	}
	if err := h.st.ImportGameData(h.ctx, gd, "test"); err != nil {
		h.t.Fatal(err)
	}
}

func TestGameDataScreens(t *testing.T) {
	h := newPanel(t)
	seedGameData(h)
	csrf := h.login()

	// Item: list, edit price and an effect, history.
	if code, body, _ := h.get("/gamedata/items?q=sol"); code != http.StatusOK || !strings.Contains(body, "Solaris") {
		t.Fatalf("items search: %d", code)
	}
	form := url.Values{"csrf": {csrf}, "name": {"Solaris"}, "price": {"99"}, "ef0": {"2"}, "ev0": {"400"}}
	if code, _, hdr := h.post("/gamedata/items/911", form); code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "saved=1") {
		t.Fatalf("save item: %d", code)
	}
	it, err := h.st.GetItem(h.ctx, 911)
	if err != nil || it.Price != 99 || it.Effects[0] != (store.ItemEffect{Effect: 2, Value: 400}) {
		t.Fatalf("item after save: %+v %v", it, err)
	}
	if _, body, _ := h.get("/gamedata/items/911?saved=1"); !strings.Contains(body, "recarregam") || !strings.Contains(body, "admin") {
		t.Fatal("saved message or history missing")
	}
	// Bad values are refused with a message, nothing saved.
	form.Set("price", "-5")
	if code, body, _ := h.post("/gamedata/items/911", form); code != http.StatusUnprocessableEntity || !strings.Contains(body, "Preço") {
		t.Fatalf("bad price: %d", code)
	}

	// Skill.
	sf := url.Values{"csrf": {csrf}, "name": {"Cura"}, "mana": {"20"}, "delay": {"3"}, "instance_type": {"6"}}
	if code, _, _ := h.post("/gamedata/skills/27", sf); code != http.StatusSeeOther {
		t.Fatalf("save skill: %d", code)
	}
	if k, _ := h.st.GetSkill(h.ctx, 27); k == nil || k.Mana != 20 || k.Delay != 3 {
		t.Fatalf("skill after save: %+v", k)
	}

	// Drop: the trailing space in the name survives the round trip; edit
	// a line, add one, remove none.
	if code, body, _ := h.get(dropURL("Guer Caveira ", "")); code != http.StatusOK || !strings.Contains(body, "Solaris") {
		t.Fatalf("drop page: %d", code)
	}
	df := url.Values{"csrf": {csrf}, "old_name": {"Guer Caveira "}, "name": {"Guer Caveira "}, "max_drops": {"5"}, "gold": {"10"},
		"rows": {"3"}, "kills0": {"100"}, "item0": {"911"}, "effect0": {"2"}, "value0": {"45"},
		"kills1": {"7"}, "item1": {"911"}, "kills2": {""}, "item2": {""}}
	if code, _, _ := h.post("/gamedata/drop", df); code != http.StatusSeeOther {
		t.Fatalf("save drop: %d", code)
	}
	d, err := h.st.GetDrop(h.ctx, "Guer Caveira ")
	if err != nil || d.MaxDrops != 5 || len(d.Items) != 2 || d.Items[0].Kills != 100 || d.Items[1].Kills != 7 {
		t.Fatalf("drop after save: %+v %v", d, err)
	}
	// New monster, then delete it.
	if code, _, _ := h.post("/gamedata/drops", url.Values{"csrf": {csrf}, "name": {"Novo "}}); code != http.StatusSeeOther {
		t.Fatalf("create drop: %d", code)
	}
	if _, err := h.st.GetDrop(h.ctx, "Novo "); err != nil {
		t.Fatalf("created drop: %v", err)
	}
	h.post("/gamedata/drop/delete", url.Values{"csrf": {csrf}, "name": {"Novo "}})
	if _, err := h.st.GetDrop(h.ctx, "Novo "); err != store.ErrNotFound {
		t.Fatalf("deleted drop still there: %v", err)
	}

	// Teleport.
	tf := url.Values{"csrf": {csrf}, "id": {"0"}, "src_x": {"2116"}, "src_y": {"2100"}, "dst_x": {"2480"}, "dst_y": {"1716"}, "price": {"1"}}
	if code, _, _ := h.post("/gamedata/teleports", tf); code != http.StatusSeeOther {
		t.Fatalf("save teleport: %d", code)
	}
	if tps, _ := h.st.ListTeleports(h.ctx); len(tps) != 1 || tps[0].Price != 1 {
		t.Fatalf("teleports after save: %+v", tps)
	}

	// Every page renders.
	for _, p := range []string{"/gamedata/items", "/gamedata/items/5000", "/gamedata/skills", "/gamedata/skills/27", "/gamedata/drops", "/gamedata/teleports?id=0"} {
		if code, _, _ := h.get(p); code != http.StatusOK {
			t.Errorf("%s: %d", p, code)
		}
	}
	changes, _ := h.st.RecentChanges(h.ctx, "", "", 50)
	if len(changes) < 6 {
		t.Fatalf("change log: %d rows", len(changes))
	}
}
