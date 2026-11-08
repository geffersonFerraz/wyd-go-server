package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func duelGame(t *testing.T) (*Game, *User, *wire.Mob) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	g.mobs[1].target = wire.Position{X: 1046, Y: 1690}
	pl := &g.mobs[1].info.Player
	pl.Inventory[3] = wire.Item{Index: 3172}
	return g, u, pl
}

func clearDuelRoom(g *Game) {
	for id := firstNPC; id < MaxMob; id++ {
		if gid := g.mobs[id].npc.generateID; g.mobs[id].mode != 0 && gid >= duelFirstGener && gid <= duelLastGener {
			g.deleteMob(id, 1)
		}
	}
}

func inDuel(g *Game, id int) bool {
	return duelArea.contains(int(g.mobs[id].target.X), int(g.mobs[id].target.Y))
}

func TestDuelLetterNeedsTheAltar(t *testing.T) {
	g, u, pl := duelGame(t)
	g.mobs[1].target = wire.Position{X: 2100, Y: 2100}
	useTicket(g, u, 3)
	if pl.Inventory[3].Index != 3172 || g.duel.active {
		t.Fatal("started away from the altar")
	}
}

func TestDuelLetterWalksTheRooms(t *testing.T) {
	g, u, pl := duelGame(t)
	pl.Equip[duelSymbolSlot] = wire.Item{Index: itemCourageSymbol}
	useTicket(g, u, 3)
	d := &g.duel
	if !d.active || pl.Inventory[3].Index != 0 || !inDuel(g, 1) {
		t.Fatalf("did not start: active %v, at %+v", d.active, g.mobs[1].target)
	}
	if !d.courage || pl.Equip[duelSymbolSlot].Index != 0 {
		t.Fatal("symbol not consumed")
	}
	if g.duelMobsLeft() == 0 || len(d.runes) != 1 || len(d.ground) != 1 {
		t.Fatalf("room empty: %d mobs, runes %v, ground %v", g.duelMobsLeft(), d.runes, d.ground)
	}

	g.duelLetterTimer()
	if d.room != 0 {
		t.Fatal("moved on with monsters alive")
	}
	for room := 1; room <= duelRoomFourth; room++ {
		clearDuelRoom(g)
		d.timer = 50
		g.duelLetterTimer()
		if d.room != room || d.timer != duelLetterSeconds || !inDuel(g, 1) {
			t.Fatalf("room %d: at room %d, timer %d", room, d.room, d.timer)
		}
		if len(d.runes) != room+1 {
			t.Fatalf("room %d: runes %v", room, d.runes)
		}
	}

	d.runes = []uint16{1733, 1734, 1735}
	clearDuelRoom(g)
	g.duelLetterTimer()
	if d.room != duelRoomBoss || d.timer == duelLetterSeconds || g.duelMobsLeft() == 0 {
		t.Fatalf("no boss: room %d, timer %d, mobs %d", d.room, d.timer, g.duelMobsLeft())
	}
}

func TestDuelLetterStopsWithoutTheSymbol(t *testing.T) {
	g, u, _ := duelGame(t)
	useTicket(g, u, 3)
	d := &g.duel
	for range 2 {
		clearDuelRoom(g)
		g.duelLetterTimer()
	}
	clearDuelRoom(g)
	g.duelLetterTimer()
	if d.room != 2 {
		t.Fatalf("reached room %d without the symbol", d.room)
	}
}

func TestDuelLetterTimeout(t *testing.T) {
	g, u, _ := duelGame(t)
	useTicket(g, u, 3)
	rune := g.duel.ground[0]
	g.duel.timer = 1
	g.duelLetterTimer()
	if g.duel.active || inDuel(g, 1) {
		t.Fatal("the party stayed after the time ran out")
	}
	if g.ground[rune].open {
		t.Fatal("rune left on the ground")
	}
	for id := firstNPC; id < MaxMob; id++ {
		if gid := g.mobs[id].npc.generateID; g.mobs[id].mode != 0 && gid >= duelFirstGener && gid <= duelLastGener {
			t.Fatalf("monster %d of generator %d left behind", id, gid)
		}
	}
}

func TestDuelLetterBusyAndIntruder(t *testing.T) {
	g, u, _ := duelGame(t)
	useTicket(g, u, 3)

	o := g.users[2]
	o.status = userPlay
	listen(o)
	g.mobs[2].mode = 1
	g.mobs[2].info.Player.Status.CurHP = 100
	g.mobs[2].target = wire.Position{X: 800, Y: 3680}
	g.setCell(800, 3680, 2)

	g.duelLetterTimer()
	if inDuel(g, 2) {
		t.Fatal("intruder stayed")
	}
	if !inDuel(g, 1) || !g.duel.active {
		t.Fatal("the party was affected")
	}

	g.mobs[2].target = wire.Position{X: 1046, Y: 1690}
	g.mobs[2].info.Player.Inventory[3] = wire.Item{Index: 3172}
	useTicket(g, o, 3)
	if g.mobs[2].info.Player.Inventory[3].Index != 3172 {
		t.Fatal("second party started while busy")
	}
}
