package game

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// TestScoreMatchesC++ recomputes every character of a real database
// (saved by the C++ TMSRV, whose Status came from its GetCurrentScore) and
// reports differences. Read-only; set SCORE_REAL_DSN to run it.
func TestScoreMatchesCpp(t *testing.T) {
	dsn := os.Getenv("SCORE_REAL_DSN")
	if dsn == "" {
		t.Skip("SCORE_REAL_DSN not set")
	}
	if _, err := os.Stat(realDataDir); err != nil {
		t.Skip("no TMSRV data folder")
	}
	ctx := context.Background()
	st, err := store.OpenUnprivileged(ctx, dsn, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d, err := LoadData(realDataDir)
	if err != nil {
		t.Fatal(err)
	}
	applyLegacyGameData(t, d)
	g := New(Config{Channel: 1, NewbieZone: 35}, d, slog.New(slog.DiscardHandler))
	accounts, err := st.SearchAccounts(ctx, "", 500)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, a := range accounts {
		rec, err := st.LoadAccount(ctx, a.Username)
		if err != nil {
			t.Fatal(err)
		}
		for slot := range rec.Account.Mob {
			ci := rec.Account.Mob[slot]
			if ci.Player.Name[0] == 0 {
				continue
			}
			u := g.users[1]
			u.charList = wire.CharList{}
			for k := range rec.Account.Mob {
				u.charList.Status[k] = rec.Account.Mob[k].Player.BStatus
			}
			m := &g.mobs[1]
			*m = Mob{info: ci, mode: 1}
			saved := ci.Player.Status
			g.getCurrentScore(1)
			got := m.info.Player.Status
			name := wire.GameString(ci.Player.Name[:])
			checked++
			if os.Getenv("SCORE_DEBUG") == name {
				for i, e := range ci.Player.Equip {
					if e.Index != 0 {
						t.Logf("equip %2d: %4d %-20s pos=%d eff=%v dmg=%d", i, e.Index, d.Item(e.Index).Name, d.Item(e.Index).Pos, e.Effect, d.itemAbility(&e, EF_DAMAGE))
					}
				}
				for _, a := range ci.Affects {
					if a.Index != 0 {
						t.Logf("affect %+v", a)
					}
				}
				t.Logf("bstatus %+v learn=%#x class=%d mobAbility dmg=%d str=%d dex=%d runspeed=%d attspeed=%d",
					ci.Player.BStatus, ci.Player.Learn, ci.Player.ClassInfo, d.mobAbility(&ci.Player, EF_DAMAGE), d.mobAbility(&ci.Player, EF_STR),
					d.mobAbility(&ci.Player, EF_DEX), d.mobAbility(&ci.Player, EF_RUNSPEED), d.mobAbility(&ci.Player, EF_ATTSPEED))
			}
			if got.Attack != saved.Attack || got.Defense != saved.Defense || got.MaxHP != saved.MaxHP || got.MaxMP != saved.MaxMP ||
				got.STR != saved.STR || got.INT != saved.INT || got.DEX != saved.DEX || got.CON != saved.CON || got.Move != saved.Move {
				t.Errorf("%s/%s lv%d evo%d: saved atk=%d def=%d hp=%d mp=%d str=%d int=%d dex=%d con=%d move=%#x\n%*s go    atk=%d def=%d hp=%d mp=%d str=%d int=%d dex=%d con=%d move=%#x",
					a.Username, name, ci.Player.BStatus.Level, evolution(&ci.Player),
					saved.Attack, saved.Defense, saved.MaxHP, saved.MaxMP, saved.STR, saved.INT, saved.DEX, saved.CON, saved.Move,
					len(a.Username)+len(name)+10, "",
					got.Attack, got.Defense, got.MaxHP, got.MaxMP, got.STR, got.INT, got.DEX, got.CON, got.Move)
			}
		}
	}
	t.Logf("checked %d characters", checked)
}
