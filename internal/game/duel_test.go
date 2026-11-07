package game

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"dbsrv/internal/wire"
)

func TestLockAndDuelPacketSizes(t *testing.T) {
	for _, c := range []struct {
		v    any
		size int
	}{{LockAccount{}, 32}, {UnlockAccount{}, 24}, {LockStatus{}, 20}, {Duel{}, 20}, {EnvEffect{}, 24}} {
		if got := binary.Size(c.v); got != c.size {
			t.Errorf("%T: %d bytes, want %d", c.v, got, c.size)
		}
	}
}

func TestAccountLock(t *testing.T) {
	g := worldGame(t)
	g.cfg.CacheDir = t.TempDir()
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	wire.PutCString(u.account.Username[:], "TESTE")
	pl := &g.mobs[1].info.Player

	lock := LockAccount{Header: wire.NewHeader(opCliLock, 1)}
	copy(lock.Password[:], "1234567")
	g.requestLock(u, packet(&lock))
	if u.lock.set() {
		t.Fatal("7-character password accepted")
	}
	copy(lock.Password[:], "12345678")
	lock.Conf[lockUnequip], lock.Conf[lockTrade] = 1, 1
	g.requestLock(u, packet(&lock))
	path := filepath.Join(g.cfg.CacheDir, "LockAccount", "TESTE.bin")
	if raw, err := os.ReadFile(path); err != nil || len(raw) != 18 || string(raw[:8]) != "12345678" || raw[10] != 1 || raw[11] != 1 || raw[12] != 0 {
		t.Fatalf("lock file %q err %v", raw, err)
	}
	if len(sent(u, opLockStatus)) == 0 {
		t.Fatal("no lock status sent")
	}

	// Using items is blocked.
	pot := findItem(t, g, 1, nil)
	pl.Inventory[3] = wire.Item{Index: pot}
	useSlot(g, u, 3)
	if pl.Inventory[3].Index != pot {
		t.Fatal("item used while locked")
	}

	// The lock comes back from the file at the next login.
	u.lock = accountLock{}
	g.loadLock(u)
	if !u.lock.desc[lockTrade] || u.lock.desc[lockBuy] {
		t.Fatalf("reloaded lock %+v", u.lock)
	}

	unlock := UnlockAccount{Header: wire.NewHeader(opLockStatus, 1)}
	copy(unlock.Password[:], "00000000")
	g.requestUnlock(u, packet(&unlock))
	if !u.lock.set() {
		t.Fatal("unlocked with the wrong password")
	}
	copy(unlock.Password[:], "12345678")
	g.requestUnlock(u, packet(&unlock))
	if u.lock.set() {
		t.Fatal("not unlocked")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("lock file left behind (the lock would come back)")
	}
}

// secondPlayer puts user 2 playing next to user 1.
func secondPlayer(g *Game) *User {
	u := g.users[2]
	u.status = userPlay
	m := &g.mobs[2]
	m.mode = 1
	m.info.Player.Status.CurHP, m.info.Player.Status.MaxHP = 100, 100
	m.info.Player.Inventory[63].Effect[0].Index = 75
	copy(m.info.Player.Name[:], "Segundo")
	x, y := int(g.mobs[1].target.X)+2, int(g.mobs[1].target.Y)
	g.emptyMobGrid(2, &x, &y)
	m.target = wire.Position{X: uint16(x), Y: uint16(y)}
	g.setCell(x, y, 2)
	return u
}

func TestArenaDuel(t *testing.T) {
	g := worldGame(t)
	a := placePlayer(g, 2112, 2042, 0)
	b := secondPlayer(g)
	listen(a, b)
	copy(g.mobs[1].info.Player.Name[:], "Primeiro")
	g.mobs[1].info.Player.Status.MaxHP = 100

	g.requestDuel(a, packet(&Duel{Header: wire.NewHeader(opDuel, 1), MobID: 2, Type: 0}))
	if a.challengeTo != 2 || len(sent(b, opDuel)) != 1 {
		t.Fatal("invite not forwarded")
	}
	// Only the invited player can accept.
	g.requestDuel(a, packet(&Duel{Header: wire.NewHeader(opDuel, 1), MobID: 2, Type: 4}))
	if g.ranking.process != 0 {
		t.Fatal("inviter accepted its own duel")
	}
	g.requestDuel(b, packet(&Duel{Header: wire.NewHeader(opDuel, 2), MobID: 1, Type: 4}))
	if g.ranking.process != 1 || g.ranking.timer != 121 {
		t.Fatalf("battle not started: %+v", g.ranking)
	}
	for _, id := range []int{1, 2} {
		p := g.mobs[id].target
		if !in(duelArena, int(p.X), int(p.Y)) {
			t.Fatalf("player %d not in the arena: %v", id, p)
		}
	}
	// A second duel waits.
	c := g.users[3]
	c.status = userPlay
	g.mobs[3].mode = 1
	g.requestDuel(c, packet(&Duel{Header: wire.NewHeader(opDuel, 3), MobID: 1, Type: 0}))
	a.challengeTo = 0
	c.challengeTo, c.challengeType = 1, 0
	g.requestDuel(a, packet(&Duel{Header: wire.NewHeader(opDuel, 1), MobID: 3, Type: 4}))
	if g.ranking.side1 != 2 || g.ranking.side2 != 1 { // DoRanking(type, acceptor, inviter)
		t.Fatalf("a second battle replaced the first: %+v", g.ranking)
	}

	// Player 2 falls: the next 5-turn check ends the battle, 1 wins.
	g.mobs[2].info.Player.Status.CurHP = 0
	sent(a, wire.OpSrvMessage)
	for range 5 {
		g.rankingTimer()
	}
	if g.ranking.process != 0 {
		t.Fatalf("battle still on: %+v", g.ranking)
	}
	msgs := sent(a, wire.OpSrvMessage)
	if len(msgs) == 0 {
		t.Fatal("no result message")
	}
	for _, id := range []int{1, 2} {
		p := g.mobs[id].target
		if in(duelArena, int(p.X), int(p.Y)) {
			t.Fatalf("player %d left in the arena", id)
		}
	}
	if g.mobs[2].info.Player.Status.CurHP != 1 {
		t.Fatal("dead fighter not revived with 1 HP")
	}
}
