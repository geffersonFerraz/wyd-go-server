package game

import (
	"dbsrv/internal/wire"
	"testing"

	"dbsrv/internal/store"
)

func TestGameDataReloadEvent(t *testing.T) {
	g, _, _ := tradeGame(t)
	gd := &store.GameData{
		Items:     []store.Item{{ID: 1103, Name: "Espada", Price: 777}},
		Skills:    []store.Skill{{ID: 27, Mana: 15}},
		Drops:     []store.Drop{{Name: "Teste ", MaxDrops: 1, Items: []store.DropItem{{Kills: 2, Item: 1103}}}},
		Teleports: []store.Teleport{{ID: 3, SrcX: 10, SrcY: 20, DstX: 30, DstY: 40, Price: 5}},
		// Generator 1 is left without a row: an empty generator.
		Generators: []store.Generator{{ID: 0}, {ID: 2, Minute: 4, MaxMobs: 3, Leader: "Novo", Follower: "Sumiu",
			Segments: []store.GeneratorSegment{{Segment: 0, X: 2100, Y: 2101, Wait: 7, Action: "Olá"}}}},
	}
	tmpl := wire.Mob{Exp: 555}
	wire.PutCString(tmpl.Name[:], "Novo")
	img, err := wire.PatchLegacyMob(nil, &tmpl)
	if err != nil {
		t.Fatal(err)
	}
	gd.NPCTemplates = map[string]map[string][]byte{store.TemplateNPC: {"Novo": img}, store.TemplateNPCBase: {}}
	g.kefra.dead = true
	g.handleEvent(event{kind: evGameData, gd: gd})
	// Generators and templates edited in the panel arrive with the tables.
	if len(g.data.Geners) != 3 || len(g.genCount) < 3 {
		t.Fatalf("generators: %d, counters %d", len(g.data.Geners), len(g.genCount))
	}
	if ge := &g.data.Geners[2]; !ge.HasLeader || ge.HasFollower || ge.Leader.Exp != 555 || ge.MaxNumMob != 3 ||
		ge.SegmentX[0] != 2100 || ge.SegmentY[0] != 2101 || ge.SegmentWait[0] != 7 || ge.SegmentAction[0] != wire.EncodeGame("Olá") {
		t.Fatalf("generator 2: leader %v follower %v exp %d max %d action %q", ge.HasLeader, ge.HasFollower, ge.Leader.Exp, ge.MaxNumMob, ge.SegmentAction[0])
	}
	if g.data.Geners[1].HasLeader || g.data.Geners[1].MinuteGenerate != 0 {
		t.Fatal("generator 1 has no row and must be empty")
	}
	if g.data.npcTemplate("Novo") == nil {
		t.Fatal("the templates of the reload were not installed")
	}
	// A new image of the template reaches the generator on the next reload.
	tmpl.Exp = 900
	img, _ = wire.PatchLegacyMob(img, &tmpl)
	gd.NPCTemplates[store.TemplateNPC]["Novo"] = img
	g.handleEvent(event{kind: evGameData, gd: gd})
	if g.data.Geners[2].Leader.Exp != 900 {
		t.Fatalf("template change not applied: exp %d", g.data.Geners[2].Leader.Exp)
	}
	g.kefra.dead = false
	// No generators (a table emptied by mistake) keeps the current world.
	none := *gd
	none.Generators = nil
	none.Items = []store.Item{{ID: 1103, Name: "Espada", Price: 1}}
	g.handleEvent(event{kind: evGameData, gd: &none})
	if len(g.data.Geners) != 3 || g.data.Item(1103).Price != 777 {
		t.Fatal("a reload without generators was applied")
	}
	if g.data.Item(1103).Price != 777 || g.data.Skills[27].Mana != 15 || g.data.Drops["Teste "] == nil || len(g.data.Teleports) != 4 || g.data.Teleports[3].Price != 5 {
		t.Fatal("reload not applied")
	}
	// An empty or broken table is refused and the current data stays.
	g.handleEvent(event{kind: evGameData, gd: &store.GameData{}})
	if g.data.Item(1103).Price != 777 {
		t.Fatal("empty reload replaced the items")
	}
}
