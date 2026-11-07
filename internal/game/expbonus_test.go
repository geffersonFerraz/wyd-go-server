package game

import (
	"path/filepath"
	"testing"

	"dbsrv/internal/wire"
)

func TestExpBonusFiles(t *testing.T) {
	var b ExpBonus
	if err := b.load(filepath.Join(realDataDir, "Controlador EXP", "ControleExp.txt"), "ControleExp.txt"); err != nil {
		t.Skip(err)
	}
	// ControleExp.txt also lists the Água/Pesa values, but like the C++ only
	// the desert keys are read from it (the others come from bonusmortal.txt).
	if b.DesertMortal != 100 || b.DesertArch != 20 || b.DesertCele != 143 || b.DesertSub != 142 || b.WaterArch != 0 {
		t.Fatalf("bonus: %+v", b)
	}
}

func TestZoneExp(t *testing.T) {
	g := worldGame(t)
	g.data.ExpBonus = ExpBonus{DesertMortal: 100, DesertArch: 20, DesertCele: 150, WaterArch: 50, PesaMortal: 200}
	u := placePlayer(g, 2100, 2100, 0)
	m := &g.mobs[1]
	pl := &m.info.Player
	at := func(zx, zy int) { m.target = wire.Position{X: uint16(zx*128 + 5), Y: uint16(zy*128 + 5)} }
	setEv := func(ev uint8, level int16) {
		pl.Equip[0].Effect[1].Value = ev
		pl.Status.Level, pl.BStatus.Level = level, level
	}
	for _, c := range []struct {
		name   string
		ev     uint8
		level  int16
		zx, zy int
		want   int64
	}{
		{"mortal outside", evoMortal, 50, 16, 16, 1000},
		{"mortal desert", evoMortal, 50, 9, 13, 2000},
		{"mortal pesadelo A", evoMortal, 50, 9, 1, 3000},
		{"mortal blocked zone", evoMortal, 50, 29, 28, 0},
		{"arch outside", evoArch, 50, 16, 16, 20},
		{"arch desert", evoArch, 50, 12, 13, 800},
		{"arch agua N", evoArch, 50, 8, 27, 2000},
		{"cele desert", evoCelestial, 100, 9, 13, 0},
		{"cele desert above 180", evoCelestial, 181, 9, 13, 0},
		{"cele agua A above 180", evoCelestial, 181, 10, 27, 800},
		{"cele agua N above 99", evoCelestial, 100, 8, 27, 0},
	} {
		setEv(c.ev, c.level)
		at(c.zx, c.zy)
		if got := g.zoneExp(1000, 1); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}

	// The killer's chest doubles, its fairy adds.
	setEv(evoMortal, 50)
	at(9, 13)
	m.info.Affects[3] = wire.Affect{Index: 39, Time: 100}
	pl.Equip[13] = wire.Item{Index: 3902}
	if got := g.expApply2(1000, 1, 1, true); got != 4000+4000*32/100 {
		t.Fatalf("chest + fairy: %d", got)
	}
	if got := g.expApply2(1000, 1, 1, false); got != 2000+2000*32/100 {
		t.Fatalf("no chest: %d", got)
	}

	// A quarter of the level refills HP and MP.
	pl.Equip[13] = wire.Item{}
	listen(u)
	setEv(evoMortal, 50)
	prev := nextLevel[evoMortal][49]
	quarter := (nextLevel[evoMortal][50] - prev) / 4
	pl.Exp = prev + quarter - 1
	pl.Status.CurHP, pl.Status.MaxHP = 1, 500
	g.checkQuarter(u, 10)
	if pl.Status.CurHP != pl.Status.MaxHP {
		t.Fatal("crossing 1/4 did not refill HP")
	}
	pl.Exp += 10
	pl.Status.CurHP = 1
	g.checkQuarter(u, 10) // still inside the same quarter
	if pl.Status.CurHP == pl.Status.MaxHP && pl.Status.MaxHP > 1 {
		t.Fatal("refilled without crossing a mark")
	}
}

func TestExpNeverNegative(t *testing.T) {
	g := worldGame(t)
	g.data.ExpBonus = ExpBonus{DesertArch: 150}
	placePlayer(g, 2100, 2100, 0)
	m := &g.mobs[1]
	m.info.Player.Equip[0].Effect[1].Value = evoArch
	m.info.Player.Status.Level, m.info.Player.BStatus.Level = 100, 100
	m.target = wire.Position{X: 9*128 + 5, Y: 13*128 + 5}
	if got := g.expApply2(1000, 1, 1, true); got != 0 {
		t.Fatalf("a 150%% desert cut gave %d", got)
	}
}

// The kill packet carries the killer's exp after the reward, so a level up
// (whose 0x337 goes first) never shows a negative gain.
func TestKillPacketExpAfterReward(t *testing.T) {
	g := worldGame(t)
	npc := 0
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		if m.mode != 0 && m.info.Player.Info&0x3F == 0 && m.info.Player.CapeInfo <= 2 && m.info.Player.Exp > 0 {
			npc = id
			break
		}
	}
	mt := g.mobs[npc].target
	u := placePlayer(g, int(mt.X)+1, int(mt.Y), 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	pl.Exp = nextLevel[evoMortal][9] - 1 // one short of the next level
	g.mobKilled(npc, 1)
	var last int64 = -1
	for _, raw := range sent(u, wire.OpSrvKilled) {
		var k wire.Killed
		if wire.Unmarshal(raw, &k) == nil && int(k.Killer) == 1 {
			last = k.Exp
		}
	}
	if last != pl.Exp {
		t.Fatalf("kill packet exp %d, character exp %d", last, pl.Exp)
	}
}

func TestLastMemberLeavingEndsParty(t *testing.T) {
	g := worldGame(t)
	leader := placePlayer(g, 2100, 2100, 0)
	member := secondPlayer(g)
	listen(leader, member)
	g.joinParty(member, 1)
	leader.out = nil
	g.removeParty(2)
	if g.isLeaderWithParty(1) || g.mobs[2].npc.leader != 0 {
		t.Fatal("party still up")
	}
	cleared := false
	for _, raw := range sent(leader, wire.OpCliPartyExit) {
		var p wire.PartyExit
		if wire.Unmarshal(raw, &p) == nil && p.MobID == 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("the leader's party window was not cleared")
	}
}

func TestPartyWindowLines(t *testing.T) {
	g := worldGame(t)
	leader := placePlayer(g, 2100, 2100, 0)
	a := secondPlayer(g)
	b := g.users[3]
	b.status = userPlay
	g.mobs[3].mode = 1
	g.mobs[3].info.Player.Status.CurHP, g.mobs[3].info.Player.Status.MaxHP = 100, 100
	copy(g.mobs[3].info.Player.Name[:], "Terceiro")
	listen(leader, a, b)
	g.joinParty(a, 1)
	b.out = nil
	g.joinParty(b, 1)
	self := false
	for _, raw := range sent(b, wire.OpSrvPartyAdd) {
		var p wire.PartyMember
		if wire.Unmarshal(raw, &p) == nil && p.PartyID == 3 {
			self = true
		}
	}
	if !self {
		t.Fatal("the second member does not see itself in the party window")
	}
}
