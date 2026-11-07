package game

import (
	"log/slog"
	"os"
	"testing"
)

const realDataDir = "../../data/tmsrv"

func TestLoadWorldData(t *testing.T) {
	if _, err := os.Stat(realDataDir); err != nil {
		t.Skip("no TMSRV data folder")
	}
	d, err := LoadData(realDataDir)
	if err != nil {
		t.Fatal(err)
	}
	applyLegacyGameData(t, d)
	if len(d.Geners) < 1000 {
		t.Errorf("geners = %d, want thousands", len(d.Geners))
	}
	leaders := 0
	for _, g := range d.Geners {
		if g.HasLeader {
			leaders++
		}
	}
	if leaders == 0 {
		t.Error("no generator leader mob loaded from npc/")
	}
	if len(d.Teleports) == 0 {
		t.Error("no teleports loaded")
	}
	if distance(0, 0, 3, 2) != 4 || distance(0, 0, 20, 3) != 21 {
		t.Error("distance table mismatch")
	}
	t.Logf("geners=%d leaders=%d teleports=%d", len(d.Geners), leaders, len(d.Teleports))

	// A few minute ticks spawn NPCs onto the grid, within MaxNumMob.
	g := New(Config{Channel: 1, NewbieZone: 35}, d, slog.New(slog.DiscardHandler))
	for range 3 {
		g.minTimer()
	}
	alive, merchants := 0, 0
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		if m.mode == 0 {
			continue
		}
		alive++
		if g.cell(int(m.target.X), int(m.target.Y)) != uint16(id) {
			t.Fatalf("npc %d not on its grid cell", id)
		}
		if m.info.Player.BStatus.Merchant&0x0F != 0 {
			merchants++
		}
	}
	for i, c := range g.genCount {
		// As in the C++, MaxNumMob only stops new leaders: one group's
		// followers may overshoot it.
		if gen := &d.Geners[i]; c > gen.MaxNumMob+max(gen.MinGroup, gen.MaxGroup) && gen.MinuteGenerate < 500 {
			t.Errorf("generator %d: %d mobs > MaxNumMob %d + MaxGroup %d", i, c, gen.MaxNumMob, gen.MaxGroup)
		}
	}
	if alive == 0 || merchants == 0 {
		t.Fatalf("alive=%d merchants=%d", alive, merchants)
	}
	t.Logf("npcs alive=%d merchants=%d", alive, merchants)
}
