package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

func npcImage(t *testing.T, name string, level int16, hp, exp int64) []byte {
	t.Helper()
	m := wire.Mob{Exp: exp}
	wire.PutCString(m.Name[:], name)
	m.BStatus.Level, m.BStatus.MaxHP, m.Status.Level, m.Status.MaxHP = level, hp, level, hp
	m.Equip[0].Index = 220
	b, err := wire.PatchLegacyMob(nil, &m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadNPC(h *panelHarness, name string) wire.Mob {
	h.t.Helper()
	b, err := h.st.GetMobTemplate(h.ctx, store.TemplateNPC, name)
	if err != nil {
		h.t.Fatal(err)
	}
	var m wire.Mob
	if err := wire.UnmarshalLegacyMob(b, &m); err != nil {
		h.t.Fatal(err)
	}
	return m
}

func TestNPCScreens(t *testing.T) {
	h := newPanel(t)
	seedGameData(h)
	csrf := h.login()
	if err := h.st.UpsertWorldAssets(h.ctx, &store.WorldAssets{Templates: []store.MobTemplate{
		{Kind: store.TemplateNPC, Name: "Orc_Guerreiro", Content: npcImage(t, "Orc_Guerreiro", 40, 5000, 1200)},
		{Kind: store.TemplateNPCBase, Name: "Lobo", Content: npcImage(t, "Lobo", 10, 300, 0)},
	}}); err != nil {
		t.Fatal(err)
	}

	// The mounts of the C++ carry item ids above the item list: the page
	// opens and saves them as they are.
	odd := wire.Mob{}
	wire.PutCString(odd.Name[:], "Condor")
	odd.Equip[19].Index, odd.Inventory[2].Index = 65535, 7710
	oddImg, _ := wire.PatchLegacyMob(nil, &odd)
	if err := h.st.UpsertWorldAssets(h.ctx, &store.WorldAssets{Templates: []store.MobTemplate{{Kind: store.TemplateNPCBase, Name: "Condor", Content: oddImg}}}); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.get("/gamedata/npc?kind=npc_base&name=Condor"); code != http.StatusOK {
		t.Fatalf("template with item 65535: %d", code)
	}
	oddForm := url.Values{"csrf": {csrf}, "kind": {"npc_base"}, "name": {"Condor"}, "game_name": {"Condor"}, "exp": {"5"}, "eq19_0": {"65535"}, "inv2_0": {"7710"}}
	if code, _, _ := h.post("/gamedata/npc", oddForm); code != http.StatusSeeOther {
		t.Fatalf("save template with item 65535: %d", code)
	}
	if b, _ := h.st.GetMobTemplate(h.ctx, store.TemplateNPCBase, "Condor"); len(b) == 0 {
		t.Fatal("Condor lost")
	} else {
		var m wire.Mob
		_ = wire.UnmarshalLegacyMob(b, &m)
		if m.Equip[19].Index != 65535 || m.Inventory[2].Index != 7710 || m.Exp != 5 {
			t.Fatalf("odd ids not kept: %d %d exp %d", m.Equip[19].Index, m.Inventory[2].Index, m.Exp)
		}
	}

	// List and search, by kind.
	if code, body, _ := h.get("/gamedata/npcs?q=orc"); code != http.StatusOK || !strings.Contains(body, "Orc_Guerreiro") || !strings.Contains(body, "1200") {
		t.Fatalf("npc list: %d", code)
	}
	if code, body, _ := h.get("/gamedata/npcs?kind=npc_base"); code != http.StatusOK || !strings.Contains(body, "Lobo") || strings.Contains(body, "Orc_Guerreiro") {
		t.Fatalf("npc_base list: %d", code)
	}

	// The edit form posts every field back: start from what the page shows.
	code, body, _ := h.get("/gamedata/npc?kind=npc&name=Orc_Guerreiro")
	if code != http.StatusOK || !strings.Contains(body, `name="exp"`) || !strings.Contains(body, `name="eq0_0"`) {
		t.Fatalf("npc page: %d", code)
	}
	form := url.Values{"csrf": {csrf}, "kind": {"npc"}, "name": {"Orc_Guerreiro"}, "game_name": {"Orc_Guerreiro"},
		"exp": {"9000"}, "b_level": {"41"}, "b_max_hp": {"7000"}, "s_level": {"41"}, "b_merchant": {"1"}, "b_direction": {"2"},
		"eq0_0": {"220"}, "eq6_0": {"911"}, "eq6_1": {"43"}, "eq6_2": {"9"}, "inv5_0": {"412"}, "inv5_5": {"61"}, "inv5_6": {"20"}}
	if code, _, hdr := h.post("/gamedata/npc", form); code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "saved=1") {
		t.Fatalf("save npc: %d", code)
	}
	m := loadNPC(h, "Orc_Guerreiro")
	if m.Exp != 9000 || m.BStatus.Level != 41 || m.BStatus.MaxHP != 7000 || m.Status.Level != 41 || m.BStatus.Merchant != 0x21 ||
		m.Equip[6] != (wire.Item{Index: 911, Effect: [3]wire.ItemEffect{{Index: 43, Value: 9}}}) ||
		m.Inventory[5].Index != 412 || m.Inventory[5].Effect[2] != (wire.ItemEffect{Index: 61, Value: 20}) {
		t.Fatalf("saved npc: exp %d level %d hp %d merchant %#x equip %+v inv %+v", m.Exp, m.BStatus.Level, m.BStatus.MaxHP, m.BStatus.Merchant, m.Equip[6], m.Inventory[5])
	}
	if ch, _ := h.st.RecentChanges(h.ctx, "npc", "npc/Orc_Guerreiro", 5); len(ch) != 1 || !strings.Contains(ch[0].Summary, "Experiência 1200 → 9000") {
		t.Fatalf("change log: %+v", ch)
	}

	// Values the 32-bit template cannot hold are refused and nothing is saved.
	form.Set("b_max_hp", "99999999999")
	if code, body, _ := h.post("/gamedata/npc", form); code != http.StatusUnprocessableEntity || !strings.Contains(body, "HP máximo") {
		t.Fatalf("hp above the limit: %d", code)
	}
	form.Set("b_max_hp", "7000")
	form.Set("eq6_2", "300")
	if code, _, _ := h.post("/gamedata/npc", form); code != http.StatusUnprocessableEntity {
		t.Fatalf("effect value above 255: %d", code)
	}
	if loadNPC(h, "Orc_Guerreiro").BStatus.MaxHP != 7000 {
		t.Fatal("a refused save changed the template")
	}

	// Copy under a new name.
	create := url.Values{"csrf": {csrf}, "kind": {"npc"}, "name": {"Orc_Chefe"}, "from": {"Orc_Guerreiro"}}
	if code, _, hdr := h.post("/gamedata/npcs", create); code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "Orc_Chefe") {
		t.Fatalf("copy npc: %d %s", code, hdr.Get("Location"))
	}
	if c := loadNPC(h, "Orc_Chefe"); c.Exp != 9000 || wire.CString(c.Name[:]) != "Orc_Chefe" {
		t.Fatalf("copy: exp %d name %q", c.Exp, wire.CString(c.Name[:]))
	}
	for _, bad := range []string{"Orc Chefe", "Orc_Chefe", "NomeGrandeDemais16", "Órc"} {
		create.Set("name", bad)
		if code, _, hdr := h.post("/gamedata/npcs", create); code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "error=") {
			t.Fatalf("name %q accepted: %d %s", bad, code, hdr.Get("Location"))
		}
	}

	// Generator: create, validate, list.
	gen := url.Values{"csrf": {csrf}, "leader": {"Orc_Chefe"}, "follower": {"Orc_Guerreiro"}, "minute": {"4"}, "max_mobs": {"6"},
		"min_group": {"1"}, "max_group": {"3"}, "route": {"2"}, "seg_x0": {"2100"}, "seg_y0": {"2105"}, "seg_range0": {"3"}, "seg_wait0": {"9"},
		"seg_x4": {"2110"}, "seg_y4": {"2105"}, "seg_action4": {"Cheguei"}, "fight1": {"Morra!"}}
	if code, _, hdr := h.post("/gamedata/generators/12", gen); code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "saved=1") {
		t.Fatalf("save generator: %d", code)
	}
	g, err := h.st.GetGenerator(h.ctx, 12)
	if err != nil || g.Leader != "Orc_Chefe" || g.MaxMobs != 6 || len(g.Segments) != 2 || g.Segments[1].Segment != 4 || g.Segments[1].Action != "Cheguei" ||
		len(g.FightActions) != 2 || g.FightActions[1] != "Morra!" || len(g.DieActions) != 0 {
		t.Fatalf("saved generator: %+v %v", g, err)
	}
	if code, body, _ := h.get("/gamedata/generators?q=chefe"); code != http.StatusOK || !strings.Contains(body, "/gamedata/generators/12") || !strings.Contains(body, "2100 x 2105") {
		t.Fatalf("generator list: %d", code)
	}
	if code, body, _ := h.get("/gamedata/npc?kind=npc&name=Orc_Chefe"); code != http.StatusOK || !strings.Contains(body, `href="/gamedata/generators/12"`) {
		t.Fatalf("npc page does not list its generator: %d", code)
	}
	gen.Set("min_group", "5")
	if code, body, _ := h.post("/gamedata/generators/12", gen); code != http.StatusUnprocessableEntity || !strings.Contains(body, "grupo mínimo") {
		t.Fatalf("min above max: %d", code)
	}
	gen.Set("min_group", "1")
	gen.Set("seg_x0", "0")
	if code, _, _ := h.post("/gamedata/generators/12", gen); code != http.StatusUnprocessableEntity {
		t.Fatalf("no birth point: %d", code)
	}
	gen.Set("seg_x0", "2100")
	gen.Set("leader", "Nao_Existe")
	if code, _, _ := h.post("/gamedata/generators/12", gen); code != http.StatusSeeOther {
		t.Fatalf("unknown leader must save with a warning: %d", code)
	}
	if code, body, _ := h.get("/gamedata/generators/12"); code != http.StatusOK || !strings.Contains(body, "Nao_Existe não existe") {
		t.Fatalf("warning about the missing template: %d", code)
	}
	if code, _, _ := h.get("/gamedata/generators/8192"); code != http.StatusNotFound {
		t.Fatalf("id out of range: %d", code)
	}

	// A template in use is not removed; a free one is.
	del := url.Values{"csrf": {csrf}, "kind": {"npc"}, "name": {"Orc_Guerreiro"}}
	if code, _, hdr := h.post("/gamedata/npc/delete", del); code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "error=") {
		t.Fatalf("delete in use: %d", code)
	}
	if code, _, _ := h.post("/gamedata/generators/12/delete", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("delete generator: %d", code)
	}
	if code, _, hdr := h.post("/gamedata/npc/delete", del); code != http.StatusSeeOther || strings.Contains(hdr.Get("Location"), "error=") {
		t.Fatalf("delete free template: %d %s", code, hdr.Get("Location"))
	}
	if _, err := h.st.GetMobTemplate(h.ctx, store.TemplateNPC, "Orc_Guerreiro"); err != store.ErrNotFound {
		t.Fatalf("template still there: %v", err)
	}
}
