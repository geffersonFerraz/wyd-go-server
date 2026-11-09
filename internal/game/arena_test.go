package game

import (
	"slices"
	"testing"
	"time"

	"dbsrv/internal/wire"
)

func arenaClock(hour, minute int) time.Time {
	return time.Date(2026, 11, 8, hour, minute, 0, 0, time.Local)
}

// arenaGame has n players of different computers, player 1 the strongest.
func arenaGame(t *testing.T, n int) *Game {
	g := worldGame(t)
	g.cfg.Arena = ArenaSettings{
		Hours:            []int{11, 23},
		WinnerItems:      []ArenaItemSettings{{Item: 4132, Amount: 2}},
		TopKillItems:     []ArenaItemSettings{{Item: 3251, Amount: 3}},
		ParticipantItems: []ArenaItemSettings{{Item: 3251}},
	}
	for i := 1; i <= n; i++ {
		u := g.users[i]
		u.status, u.mac[0] = userPlay, byte(i)
		listen(u)
		m := &g.mobs[i]
		m.mode = 1
		wire.PutCString(m.info.Player.Name[:], "Jogador")
		s := &m.info.Player.Status
		s.CurHP, s.MaxHP, s.CurMP, s.MaxMP, s.Level = 50, 100, 50, 80, int16(400-i)
		x, y := 2100+i*2, 2100
		g.emptyMobGrid(i, &x, &y)
		m.target = wire.Position{X: uint16(x), Y: uint16(y)}
		g.setCell(x, y, uint16(i))
	}
	return g
}

func startedArena(t *testing.T, n int) *Game {
	g := arenaGame(t, n)
	for i := 1; i <= n; i++ {
		g.arenaRegister(g.users[i], arenaClock(11, 1))
	}
	g.arena.status = arenaRegistering
	g.arenaTick(arenaClock(11, 5))
	if g.arena.status != arenaRunning {
		t.Fatalf("arena did not start: %v", g.arena.status)
	}
	return g
}

func inArena(g *Game, id int) bool {
	return arenaArea.contains(int(g.mobs[id].target.X), int(g.mobs[id].target.Y))
}

func TestArenaRegistration(t *testing.T) {
	g := arenaGame(t, 3)
	u := g.users[1]
	if g.arenaRegister(u, arenaClock(12, 1)); len(g.arena.registered) != 0 {
		t.Fatal("registered at an hour not listed")
	}
	if g.arenaRegister(u, arenaClock(11, 5)); len(g.arena.registered) != 0 {
		t.Fatal("registered after the first five minutes")
	}
	g.arenaRegister(u, arenaClock(11, 4))
	g.arenaRegister(u, arenaClock(11, 4))
	if len(g.arena.registered) != 1 {
		t.Fatalf("registered twice: %v", g.arena.registered)
	}
	g.users[2].mac = u.mac // the same computer
	g.arenaRegister(g.users[2], arenaClock(11, 4))
	if len(g.arena.registered) != 1 {
		t.Fatal("two accounts of one computer")
	}
	g.arenaUnregister(u)
	if len(g.arena.registered) != 0 {
		t.Fatal("still registered after leaving")
	}
}

func TestArenaOpensAndNeedsFourPlayers(t *testing.T) {
	g := arenaGame(t, 3)
	g.arenaTick(arenaClock(12, 0))
	if g.arena.status != arenaOff {
		t.Fatal("opened at an hour not listed")
	}
	g.arenaTick(arenaClock(11, 0))
	if g.arena.status != arenaRegistering {
		t.Fatal("did not open")
	}
	for i := 1; i <= 3; i++ {
		g.arenaRegister(g.users[i], arenaClock(11, 1))
	}
	g.arenaTick(arenaClock(11, 5))
	if g.arena.status != arenaOff || len(g.arena.registered) != 0 || inArena(g, 1) {
		t.Fatalf("started without enough players: %v", g.arena.status)
	}
}

func TestArenaTeams(t *testing.T) {
	g := startedArena(t, 8)
	a := &g.arena
	if a.pointsToWin != arenaBasePoints {
		t.Fatalf("points to win %d", a.pointsToWin)
	}
	want := [arenaTeams][]int{{1, 5}, {2, 6}, {3, 7}, {4, 8}}
	for i := range want {
		if !slices.Equal(a.teams[i], want[i]) {
			t.Fatalf("teams by power: %v", a.teams)
		}
	}
	for i := 1; i <= 8; i++ {
		if !inArena(g, i) || !g.mobs[i].guildDisable || g.users[i].arenaTeam == 0 {
			t.Fatalf("player %d not in the match at %+v", i, g.mobs[i].target)
		}
	}
	if g.mobs[5].npc.leader != 1 || !slices.Contains(g.mobs[1].npc.followers[:], 5) {
		t.Fatal("team is not a party")
	}
	p := g.createMobPacket(1)
	if got := wire.CString(p.Tab[:]); got != "Grupo Mestre (0)" {
		t.Fatalf("tag %q", got)
	}
}

func TestArenaGoalGrowsWithPlayers(t *testing.T) {
	g := startedArena(t, 20)
	if g.arena.pointsToWin != arenaBasePoints+20 {
		t.Fatalf("points to win %d", g.arena.pointsToWin)
	}
}

func TestArenaDeathRespawns(t *testing.T) {
	g := startedArena(t, 8)
	m := &g.mobs[2]
	m.info.Affects[3].Index = arenaProtectAura
	m.info.Player.Status.CurHP = 0
	g.mobKilled(2, 1)
	if g.users[1].arenaKills != 1 || g.users[2].arenaKills != 0 {
		t.Fatalf("kills %d / %d", g.users[1].arenaKills, g.users[2].arenaKills)
	}
	if s := m.info.Player.Status; s.CurHP != s.MaxHP || s.CurMP != s.MaxMP || !inArena(g, 2) {
		t.Fatalf("not respawned: %+v at %+v", s, m.target)
	}
	if m.info.Affects[3].Index != 0 {
		t.Fatal("aura kept")
	}

	before := g.users[1].arenaKills
	mob := MaxPlayer + 10
	g.mobs[mob] = Mob{mode: modeNPCIdle}
	g.mobKilled(2, mob) // a monster's kill scores for nobody
	g.mobKilled(2, 2)   // nor does killing oneself
	if g.users[1].arenaKills != before || g.users[2].arenaKills != 0 {
		t.Fatal("a point from a death nobody earned")
	}
}

func TestArenaWinnerAndRewards(t *testing.T) {
	g := startedArena(t, 8)
	g.users[1].arenaKills = 10 // top killer, team 0
	g.users[5].arenaKills = 4  // team 0, reaches the goal with the kill below
	g.users[2].arenaKills = 1  // scored in a losing team
	g.mobs[3].info.Player.Status.CurHP = 0
	g.mobKilled(3, 5)

	if g.arena.status != arenaOff {
		t.Fatal("no winner at the goal")
	}
	has := func(id int, item uint16, amount int) bool {
		for i := range g.mobs[id].info.Player.Inventory {
			it := &g.mobs[id].info.Player.Inventory[i]
			if it.Index == item && itemAmount(it) == amount {
				return true
			}
		}
		return false
	}
	if !has(1, 4132, 2) || !has(5, 4132, 2) {
		t.Fatal("winners not paid")
	}
	if !has(1, 3251, 3) || has(5, 3251, 3) {
		t.Fatal("top killer prize wrong")
	}
	if !has(2, 3251, 1) || !has(5, 3251, 1) || has(3, 3251, 1) {
		t.Fatal("participant prize wrong")
	}
	if has(2, 4132, 2) {
		t.Fatal("loser paid as winner")
	}
	for i := 1; i <= 8; i++ {
		if inArena(g, i) || g.mobs[i].guildDisable || g.users[i].arenaTeam != 0 {
			t.Fatalf("player %d not released", i)
		}
	}
}

func TestArenaTimeoutTiePaysNothing(t *testing.T) {
	g := startedArena(t, 8)
	end := g.arena.endAt
	g.arenaTick(end.Add(-time.Second))
	if g.arena.status != arenaRunning {
		t.Fatal("ended early")
	}
	g.users[1].arenaKills, g.users[2].arenaKills = 3, 3
	g.arenaTick(end)
	if g.arena.status != arenaOff || inArena(g, 1) {
		t.Fatal("did not end")
	}
	if g.mobs[1].info.Player.Inventory[0].Index != 0 {
		t.Fatal("tie paid a prize")
	}
}

func TestArenaTopKillTiePaysParticipants(t *testing.T) {
	g := startedArena(t, 8)
	g.users[1].arenaKills, g.users[5].arenaKills = 5, 5
	g.giveArenaRewards(0)
	for _, id := range []int{1, 5} {
		found := false
		for _, it := range g.mobs[id].info.Player.Inventory {
			found = found || (it.Index == 3251 && itemAmount(&it) == 1)
		}
		if !found {
			t.Fatalf("player %d lost the participant prize", id)
		}
	}
}

func TestArenaLeaverKeepsTheMatchGoing(t *testing.T) {
	g := startedArena(t, 8)
	g.arenaUnregister(g.users[5])
	if slices.Contains(g.arena.teams[0], 5) || g.users[5].arenaTeam != 0 || g.mobs[5].guildDisable {
		t.Fatalf("still in the team: %v", g.arena.teams[0])
	}
	g.arenaTick(g.arena.endAt)
	if g.arena.status != arenaOff {
		t.Fatal("did not end")
	}
}

func TestArenaSettingsValidation(t *testing.T) {
	for name, a := range map[string]ArenaSettings{
		"hour":   {Hours: []int{24}},
		"repeat": {Hours: []int{11, 11}},
		"item":   {TopKillItems: []ArenaItemSettings{{Item: MaxItemList}}},
		"amount": {WinnerItems: []ArenaItemSettings{{Item: 3251, Amount: 65536}}},
	} {
		if err := a.validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ok := ArenaSettings{Hours: []int{0, 23}, ParticipantItems: []ArenaItemSettings{{Item: 3251}}}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
}
