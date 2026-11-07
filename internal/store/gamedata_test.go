package store

import (
	"context"
	"reflect"
	"testing"
)

func TestGameDataRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	gd := &GameData{
		EffectNames: []EffectName{{ID: 2, Name: "EF_DAMAGE"}, {ID: 61, Name: "EF_AMOUNT"}},
		Items: []Item{
			{ID: 412, Name: "Pedra da Luz", Price: 100, Pos: 0, Effects: [12]ItemEffect{{Effect: 61, Value: 1}, {}, {Effect: 2, Value: -5}}},
			{ID: 1103, Name: "Espada Ácida", Level: 10, ReqStr: 20, Price: 900, Unique: 41, Pos: 192, Grade: 3},
		},
		Skills: []Skill{{ID: 0, Name: "Giro_da_Fúria", Points: 66, Mana: 10, Delay: 5, InstanceType: 1, InstanceValue: 1470, MaxTarget: 2, Act1: "7.0", Act2: "7.0"}},
		Drops: []Drop{
			{Name: "Guer Caveira ", Section: "LAN N", MaxDrops: 19, Gold: 20000, Items: []DropItem{{Kills: 50, Item: 4019}, {Kills: 250, Item: 911, Effect: 2, Value: 45}}},
			{Name: "Guer Caveira", MaxDrops: 1, Items: []DropItem{{Kills: 3, Item: 412}}},
		},
		Teleports: []Teleport{{ID: 0, SrcX: 2116, SrcY: 2100, DstX: 2480, DstY: 1716, Price: 700}},
	}
	if err := s.ImportGameData(ctx, gd, "test"); err != nil {
		t.Fatal(err)
	}
	back, err := s.LoadGameData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, gd) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", back, gd)
	}

	// Importing again replaces everything.
	gd2 := &GameData{Items: []Item{{ID: 5, Name: "x"}}}
	if err := s.ImportGameData(ctx, gd2, "test"); err != nil {
		t.Fatal(err)
	}
	back, err = s.LoadGameData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Items) != 1 || len(back.Skills) != 0 || len(back.Drops) != 0 || len(back.Teleports) != 0 {
		t.Fatalf("second import did not replace: %+v", back)
	}

	// The database rejects bad rows.
	for name, bad := range map[string]*GameData{
		"item id":    {Items: []Item{{ID: 7000, Name: "x"}}},
		"kills 0":    {Drops: []Drop{{Name: "A", Items: []DropItem{{Kills: 0, Item: 5}}}}},
		"empty name": {Drops: []Drop{{Name: ""}}},
		"dup skill":  {Skills: []Skill{{ID: 1}, {ID: 1}}},
	} {
		if err := s.ImportGameData(ctx, bad, "test"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A failed import leaves the previous data.
	back, err = s.LoadGameData(ctx)
	if err != nil || len(back.Items) != 1 {
		t.Fatalf("failed import changed the data: %+v %v", back, err)
	}
}
