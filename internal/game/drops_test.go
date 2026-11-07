package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func TestLoadRealDropTable(t *testing.T) {
	g := worldGame(t)
	d := g.data.Drops["Guer Caveira "]
	if d == nil || d.MaxDrops != 19 || d.Gold != 20000 || len(d.Items) != 19 {
		t.Fatalf("Guer Caveira: %+v", d)
	}
	if it := d.Items[5]; it.Kills != 250 || it.Index != 911 || it.Effect != 2 || it.Value != 45 {
		t.Fatalf("item 5: %+v", it)
	}
	if g.data.Drops["Guer Caveira"] == nil {
		t.Fatal(`"Guer Caveira" (no trailing space) is a different monster and must be kept`)
	}
	// Spawned monster names must match table names exactly.
	matched := map[string]bool{}
	for id := firstNPC; id < MaxMob; id++ {
		if g.mobs[id].mode != 0 {
			if n := mobName(&g.mobs[id].info.Player); g.data.Drops[n] != nil {
				matched[n] = true
			}
		}
	}
	if len(matched) == 0 {
		t.Fatal("no spawned monster name matches the drop table")
	}
	t.Logf("%d drop table entries, %d matched by spawned monsters", len(g.data.Drops), len(matched))
}

func writeDrops(t *testing.T, g *Game, mobs ...*MobDrop) {
	t.Helper()
	g.data.Drops = map[string]*MobDrop{}
	for _, m := range mobs {
		g.data.Drops[m.Name] = m
	}
}

func TestDropEveryNKills(t *testing.T) {
	g, a, _ := tradeGame(t)
	writeDrops(t, g, &MobDrop{Name: "Teste", MaxDrops: 5, Gold: 100, Items: []DropItem{{Kills: 3, Index: 1103}, {Kills: 1, Index: 4117}}})
	pl := &g.mobs[1].info.Player
	pl.Gold = 0
	for range 3 {
		g.handleMobDrop(a, "Teste")
	}
	if pl.Gold != 300 {
		t.Fatalf("gold %d, want 300", pl.Gold)
	}
	swords, stack := 0, -1
	for i := range 30 {
		switch pl.Inventory[i].Index {
		case 1103:
			swords++
		case 4117:
			if stack != -1 {
				t.Fatalf("4117 in two slots (%d and %d), should stack", stack, i)
			}
			stack = i
		}
	}
	if swords != 1 {
		t.Fatalf("%d x 1103 after 3 kills, want 1", swords)
	}
	if stack == -1 || itemAmount(&pl.Inventory[stack]) != 3 {
		t.Fatalf("4117 stack: slot %d, want amount 3", stack)
	}
}

func TestDropFilter(t *testing.T) {
	g, a, _ := tradeGame(t)
	writeDrops(t, g, &MobDrop{Name: "Teste", MaxDrops: 5, Items: []DropItem{{Kills: 1, Index: 1103}, {Kills: 1, Index: 1104}}})
	ci := &g.mobs[1].info
	ci.FilterDrop.IsEnabled = true
	ci.FilterDrop.Items[0] = wire.Item{Index: 1104}
	g.handleMobDrop(a, "Teste")
	got := map[uint16]bool{}
	for i := range 30 {
		got[ci.Player.Inventory[i].Index] = true
	}
	if got[1103] || !got[1104] {
		t.Fatalf("filter: got %v, want only 1104", got)
	}
}
