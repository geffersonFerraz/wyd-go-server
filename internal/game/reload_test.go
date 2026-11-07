package game

import (
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
	}
	g.handleEvent(event{kind: evGameData, gd: gd})
	if g.data.Item(1103).Price != 777 || g.data.Skills[27].Mana != 15 || g.data.Drops["Teste "] == nil || len(g.data.Teleports) != 4 || g.data.Teleports[3].Price != 5 {
		t.Fatal("reload not applied")
	}
	// An empty or broken table is refused and the current data stays.
	g.handleEvent(event{kind: evGameData, gd: &store.GameData{}})
	if g.data.Item(1103).Price != 777 {
		t.Fatal("empty reload replaced the items")
	}
}
