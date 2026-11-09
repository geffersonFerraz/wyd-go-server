package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

func pistaGame(t *testing.T) *Game {
	g := warGame(t, 3)
	g.mobs[1].npc.followers[0] = 2
	return g
}

func TestUxmalRegisters(t *testing.T) {
	g := pistaGame(t)
	u := g.users[1]
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = wire.Item{Index: itemPista}
	npc := firstNPC + 1
	g.npcUxmal(u, npc, time.Date(2026, 11, 9, 10, 10, 0, 0, time.Local))
	if pl.Inventory[0].Index != itemPista {
		t.Fatal("registered outside the window")
	}
	g.npcUxmal(u, npc, time.Date(2026, 11, 9, 10, 17, 0, 0, time.Local))
	c := g.pista[0].clients[0]
	if pl.Inventory[0].Index != 0 || c[pistaLeader] != 1 || c[0] != 2 {
		t.Fatalf("item %d party %v", pl.Inventory[0].Index, c)
	}
}

func TestPistaRunsAndGivesRunes(t *testing.T) {
	g := pistaGame(t)
	g.pista[0].clients[0][pistaLeader] = 1
	g.pistaTimer(time.Date(2026, 11, 9, 10, 20, 0, 0, time.Local))
	p := &g.pista[0]
	if !p.status || p.timer != pistaSeconds {
		t.Fatalf("not started: %+v %d", p.status, p.timer)
	}
	if !pistaArea(0).contains(int(g.mobs[1].target.X), int(g.mobs[1].target.Y)) {
		t.Fatalf("leader at %v", g.mobs[1].target)
	}
	g.giveRuna(0, 0)
	pl := &g.mobs[1].info.Player
	if firstSlotOf(pl, itemPista) < 0 {
		t.Fatal("no next Pista")
	}
	if it := pl.Inventory[firstSlotOf(pl, itemPista)]; itemSanc(&it) != 1 {
		t.Fatalf("next Pista +%d", itemSanc(&it))
	}
	found := false
	for _, id := range pistaRunes[0] {
		found = found || firstSlotOf(pl, uint16(id)) >= 0
	}
	if !found {
		t.Fatal("no rune")
	}
	p.timer = 2
	g.pistaTimer(time.Date(2026, 11, 9, 10, 35, 0, 0, time.Local))
	if g.pista[0].status || g.pista[0].clients[0][pistaLeader] != 0 {
		t.Fatal("room not reset")
	}
}

func TestPistaLogoutAndTransfer(t *testing.T) {
	g := pistaGame(t)
	g.pista[3].clients[1][pistaLeader], g.pista[3].clients[1][0] = 1, 2
	g.pistaTransfer(1, 2)
	if c := g.pista[3].clients[1]; c[pistaLeader] != 2 || c[0] != 1 {
		t.Fatalf("transfer: %v", c)
	}
	g.pistaUnregister(2)
	if g.pista[3].clients[1][pistaLeader] != 0 {
		t.Fatal("still registered")
	}
}

func TestPartyCannotHitOwnTower(t *testing.T) {
	g := pistaGame(t)
	id := firstNPC + 2
	g.mobs[id].mode, g.mobs[id].npc.generateID = modeBattle, genTorre+1
	if !g.pistaHitBlocked(1, id) {
		t.Fatal("tower hit with the Pista off")
	}
	g.pista[1].status = true
	g.pista[1].clients[1][0] = 1
	if !g.pistaHitBlocked(1, id) || g.pistaHitBlocked(2, id) {
		t.Fatal("wrong side of the tower")
	}
}

func TestHitEventGivesPrize(t *testing.T) {
	g := warGame(t, 1)
	u := g.users[1]
	id := firstNPC + 3
	g.mobs[id].mode = modeNPCIdle
	wire.PutCString(g.mobs[id].info.Player.Name[:], hitEventBoss)
	g.mobs[1].target = wire.Position{X: 10*128 + 1, Y: 11*128 + 1}
	for range hitEventHits {
		if !g.hitEventProgress(u, id) {
			t.Fatal("hit not counted")
		}
	}
	if firstSlotOf(&g.mobs[1].info.Player, hitEventPrize) < 0 || u.hitEventCount != 0 {
		t.Fatalf("no prize; count %d", u.hitEventCount)
	}
}

func TestArenaShopTakesCoupons(t *testing.T) {
	g := warGame(t, 1)
	u := g.users[1]
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = wire.Item{Index: itemArenaCoupon}
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: 61, Value: 3}
	id := firstNPC + 4
	wire.PutCString(g.mobs[id].info.Player.Name[:], "Loja Arena")
	if !g.shopSpecial(u, id, &wire.Item{Index: 3314}) || firstSlotOf(pl, 3314) < 0 || inventoryAmount(pl, itemArenaCoupon) != 1 {
		t.Fatalf("bought: coupons left %d", inventoryAmount(pl, itemArenaCoupon))
	}
	if g.shopSpecial(u, id, &wire.Item{Index: 412}); firstSlotOf(pl, 412) >= 0 {
		t.Fatal("an ordinary item was handled as special")
	}
}

func TestEventBoxPrize(t *testing.T) {
	g := warGame(t, 1)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = wire.Item{Index: 475}
	g.openEventBox(g.users[1], 475)
	if pl.Inventory[0].Index == 475 {
		t.Fatal("box not consumed")
	}
	got := false
	for _, id := range []uint16{480, 4020, 578, 1774, 4026} {
		got = got || firstSlotOf(pl, id) >= 0
	}
	if !got {
		t.Fatal("no prize")
	}
}

func TestServerAreaTimers(t *testing.T) {
	g := warGame(t, 2)
	g.mobs[1].target = wire.Position{X: 2200, Y: 3600} // valley without the fairy
	g.users[2].questAccess = 3
	g.secCounter = 1200
	g.serverAreaTimer()
	if insideValley(g.mobs[1].target) {
		t.Fatal("stayed in the valley without the fairy")
	}
	if g.users[2].questAccess != 0 {
		t.Fatal("quest access not reset")
	}
	g.users[1].status = userEmpty // only player 2 is under test
	g.users[2].conn = &clientConn{out: make(chan [][]byte, 8)}
	g.users[2].lastReceiveSec, g.secCounter = 100, 100+idleLimitTicks
	g.idleTimer()
	if g.users[2].status != userPlay {
		t.Fatal("disconnected at the limit")
	}
	g.secCounter++
	g.idleTimer()
	if g.users[2].status == userPlay {
		t.Fatal("idle client still playing")
	}
}

func TestBigCuboPrize(t *testing.T) {
	g := warGame(t, 1)
	u := g.users[1]
	g.mobs[1].target = wire.Position{X: 1310, Y: 1450}
	g.bigCuboCount = bigCuboKills - 1
	g.bigCuboKill(u)
	if firstSlotOf(&g.mobs[1].info.Player, bigCuboPrize) < 0 || g.bigCuboCount != 0 {
		t.Fatal("no prize")
	}
}
