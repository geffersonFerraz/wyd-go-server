package store

import (
	"context"
	"testing"
)

func TestExpRuleCalculation(t *testing.T) {
	r := DefaultExpRule() // range 2, equal 100, +5/level up to 200, -5/level down to 10
	for _, c := range []struct {
		char, mob, ev, want int
	}{
		{100, 100, EvoMortal, 100}, {98, 100, EvoMortal, 100}, {102, 100, EvoMortal, 100}, // within the range
		{90, 100, EvoMortal, 140}, {50, 100, EvoMortal, 200}, // below the monster, capped
		{110, 100, EvoMortal, 60}, {200, 100, EvoMortal, 10}, // above the monster, floored
	} {
		if got := r.LevelPercent(c.char, c.mob, c.ev); got != c.want {
			t.Errorf("char %d mob %d: %d%%, want %d%%", c.char, c.mob, got, c.want)
		}
	}
	// Evolution and the global multiplier.
	if got := r.Apply(1000, 100, 100, EvoMortal); got != 1000 {
		t.Fatalf("mortal: %d", got)
	}
	if got := r.Apply(1000, 100, 100, EvoCelestial); got != 400 {
		t.Fatalf("celestial keeps 40%%: %d", got)
	}
	r.GlobalPercent = 300
	if got := r.Apply(1000, 90, 100, EvoArch); got != 1000*140/100*70/100*3 {
		t.Fatalf("arch, below, event: %d", got)
	}
	r.GlobalPercent = 0
	if got := r.Apply(1000, 90, 100, EvoArch); got != 0 {
		t.Fatalf("global 0: %d", got)
	}
	// The level offset moves the comparison: a Celestial 10 counts as 110.
	r.GlobalPercent = 100
	r.Evolutions[EvoCelestial].LevelOffset = 100
	if got := r.LevelPercent(10, 100, EvoCelestial); got != 60 {
		t.Fatalf("offset: %d%%", got)
	}
	// Base by the monster's level instead of the template.
	r.BaseMode, r.BaseFixed, r.BasePerLevel = ExpBaseLevel, 500, 20
	if got := r.Apply(999999, 100, 100, EvoMortal); got != 2500 {
		t.Fatalf("base by level: %d", got)
	}
	// An unknown evolution (an NPC) is not reduced.
	if got := r.Apply(0, 100, 100, 0); got != 2500 {
		t.Fatalf("no evolution: %d", got)
	}

	bad := DefaultExpRule()
	bad.LowerMaxPercent = 50
	if bad.Check() == nil {
		t.Fatal("a cap below the equal value was accepted")
	}
	bad = DefaultExpRule()
	bad.BaseMode = "x"
	if bad.Check() == nil {
		t.Fatal("unknown base mode accepted")
	}
}

func TestExpRuleStored(t *testing.T) {
	s, ctx := testStore(t), context.Background()
	if r, err := s.ExpRule(ctx); err != nil || r != DefaultExpRule() || r.Enabled {
		t.Fatalf("default: %+v %v", r, err)
	}
	if gd, err := s.LoadGameData(ctx); err != nil || gd.ExpRule != nil {
		t.Fatalf("no rule saved: %+v %v", gd.ExpRule, err)
	}
	r := DefaultExpRule()
	r.Enabled, r.GlobalPercent, r.Evolutions[EvoArch].Percent = true, 250, 55
	if err := s.SaveExpRule(ctx, &r, "admin", "global"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ExpRule(ctx); err != nil || got != r {
		t.Fatalf("saved: %+v %v", got, err)
	}
	gd, err := s.LoadGameData(ctx)
	if err != nil || gd.ExpRule == nil || *gd.ExpRule != r {
		t.Fatalf("LoadGameData: %+v %v", gd.ExpRule, err)
	}
	r.GlobalPercent = -1
	if err := s.SaveExpRule(ctx, &r, "admin", "x"); err == nil {
		t.Fatal("invalid rule saved")
	}
	// An import without a rule keeps it; with one, replaces it.
	if err := s.ImportGameData(ctx, &GameData{Items: []Item{{ID: 1, Name: "a"}}}, "t"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ExpRule(ctx); got.GlobalPercent != 250 {
		t.Fatalf("import without rule: %+v", got)
	}
	off := DefaultExpRule()
	if err := s.ImportGameData(ctx, &GameData{Items: []Item{{ID: 1, Name: "a"}}, ExpRule: &off}, "t"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ExpRule(ctx); got != off {
		t.Fatalf("import with rule: %+v", got)
	}
}
