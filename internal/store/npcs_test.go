package store

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestGenerators(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	gens := []Generator{
		{ID: 0, Minute: -1, MaxMobs: 100, MinGroup: 2, MaxGroup: 5, Route: 2, Leader: "Ciclope_Wild", Follower: "Mago_Ciclope",
			Segments: []GeneratorSegment{{Segment: 0, X: 2635, Y: 1726, Range: 5, Wait: 9, Action: "Olá, viajante"}, {Segment: 4, X: 2627, Y: 1726}}},
		{ID: 3, Minute: 4, MaxMobs: 1, Leader: "Zakum_Lord", FightActions: []string{"a", "", "c"}},
	}
	if n, err := s.SeedGenerators(ctx, gens, "t"); err != nil || n != 2 {
		t.Fatalf("seed: %d %v", n, err)
	}
	// Only an empty table is seeded.
	if n, err := s.SeedGenerators(ctx, []Generator{{ID: 9}}, "t"); err != nil || n != 0 {
		t.Fatalf("second seed: %d %v", n, err)
	}
	back, err := s.Generators(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Empty lists come back empty, not nil.
	gens[0].FightActions, gens[0].DieActions, gens[1].DieActions, gens[1].Segments = []string{}, []string{}, []string{}, []GeneratorSegment{}
	if !reflect.DeepEqual(back, gens) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", back, gens)
	}

	g := gens[1]
	g.MaxMobs, g.Follower = 7, "Zakum_Lord"
	if err := s.SaveGenerator(ctx, &g, "admin"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetGenerator(ctx, 3); err != nil || got.MaxMobs != 7 || got.Follower != "Zakum_Lord" {
		t.Fatalf("saved: %+v %v", got, err)
	}
	if list, total, err := s.SearchGenerators(ctx, GeneratorFilter{Q: "zakum", Limit: 10}); err != nil || total != 1 || len(list) != 1 || list[0].ID != 3 {
		t.Fatalf("search by name: %+v %d %v", list, total, err)
	}
	if list, _, err := s.SearchGenerators(ctx, GeneratorFilter{Q: "0", Limit: 10}); err != nil || len(list) != 1 || list[0].ID != 0 {
		t.Fatalf("search by id: %+v %v", list, err)
	}
	if ids, err := s.GeneratorsUsing(ctx, "Zakum_Lord"); err != nil || !reflect.DeepEqual(ids, []int{3}) {
		t.Fatalf("using: %v %v", ids, err)
	}
	if next, err := s.NextGeneratorID(ctx); err != nil || next != 4 {
		t.Fatalf("next id: %d %v", next, err)
	}
	for _, bad := range []Generator{{ID: -1}, {ID: MaxGenerators}, {ID: 1, DieActions: make([]string, 5)},
		{ID: 1, Segments: []GeneratorSegment{{Segment: 5}}}, {ID: 1, Segments: []GeneratorSegment{{Segment: 1}, {Segment: 1}}}} {
		if err := s.SaveGenerator(ctx, &bad, "admin"); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if err := s.DeleteGenerator(ctx, 3, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetGenerator(ctx, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted generator: %v", err)
	}

	// An import with generators replaces them; without, it keeps them.
	if err := s.ImportGameData(ctx, &GameData{Items: []Item{{ID: 1, Name: "a"}}}, "t"); err != nil {
		t.Fatal(err)
	}
	if gd, err := s.LoadGameData(ctx); err != nil || len(gd.Generators) != 1 {
		t.Fatalf("import without generators: %v", err)
	}
	if err := s.ImportGameData(ctx, &GameData{Items: []Item{{ID: 1, Name: "a"}}, Generators: []Generator{{ID: 5, Leader: "x"}}}, "t"); err != nil {
		t.Fatal(err)
	}
	if gd, err := s.LoadGameData(ctx); err != nil || len(gd.Generators) != 1 || gd.Generators[0].ID != 5 {
		t.Fatalf("import with generators: %+v %v", gd.Generators, err)
	}
}

func TestMobTemplates(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	img := bytes.Repeat([]byte{7}, 848)
	if err := s.SaveMobTemplate(ctx, MobTemplate{Kind: TemplateNPC, Name: "Orc", Content: img}, true, "admin", "criado"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMobTemplate(ctx, MobTemplate{Kind: TemplateNPC, Name: "Orc", Content: img}, true, "admin", "criado"); err == nil {
		t.Fatal("created the same name twice")
	}
	img[0] = 9
	if err := s.SaveMobTemplate(ctx, MobTemplate{Kind: TemplateNPC, Name: "Orc", Content: img}, false, "admin", "nível"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetMobTemplate(ctx, TemplateNPC, "Orc"); err != nil || !bytes.Equal(got, img) {
		t.Fatalf("template: %v", err)
	}
	if list, total, err := s.SearchMobTemplates(ctx, TemplateNPC, "or", 10); err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("search: %d %v", total, err)
	}
	if _, total, _ := s.SearchMobTemplates(ctx, TemplateNPCBase, "", 10); total != 0 {
		t.Fatalf("other kind: %d", total)
	}
	// The game loads them with the tables.
	gd, err := s.LoadGameData(ctx)
	if err != nil || !bytes.Equal(gd.NPCTemplates[TemplateNPC]["Orc"], img) {
		t.Fatalf("LoadGameData templates: %v", err)
	}
	if ch, err := s.RecentChanges(ctx, "npc", "npc/Orc", 5); err != nil || len(ch) != 2 {
		t.Fatalf("change log: %d %v", len(ch), err)
	}
	if err := s.DeleteMobTemplate(ctx, TemplateNPC, "Orc", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMobTemplate(ctx, TemplateNPC, "Orc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted template: %v", err)
	}
}
