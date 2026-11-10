package game

import (
	"testing"

	"dbsrv/internal/acctedit"
	"dbsrv/internal/wire"
)

func TestAdminEditLive(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 10)
	listen(u)
	wire.PutCString(u.account.Username[:], "TESTER")
	u.charSlot = 0
	pl := &g.mobs[1].info.Player
	apply := func(e acctedit.Edit, user string) {
		t.Helper()
		p := e.Packet(user, 1)
		g.adminEdit(u, packet(&p))
	}

	apply(acctedit.Edit{Op: acctedit.OpGold, Value: 123456}, "TESTER")
	apply(acctedit.Edit{Op: acctedit.OpLevel, Value: 80}, "TESTER")
	apply(acctedit.Edit{Op: acctedit.OpItem, Where: wire.SlotInv, Slot: 4, Item: []int{412, 0, 0, 0, 0, 0, 0}}, "TESTER")
	apply(acctedit.Edit{Op: acctedit.OpItem, Where: wire.SlotStorage, Slot: 9, Item: []int{4140, 61, 5, 0, 0, 0, 0}}, "TESTER")
	apply(acctedit.Edit{Op: acctedit.OpCoin, Value: 77}, "TESTER")
	apply(acctedit.Edit{Op: acctedit.OpAccess, Value: 2}, "TESTER")
	if pl.Gold != 123456 || pl.BStatus.Level != 79 || pl.Inventory[4].Index != 412 ||
		u.account.StorageItems[9].Index != 4140 || u.account.StorageCoin != 77 || u.accessLevel != 2 {
		t.Fatalf("gold %d level %d inv %d storage %d coin %d access %d", pl.Gold, pl.BStatus.Level,
			pl.Inventory[4].Index, u.account.StorageItems[9].Index, u.account.StorageCoin, u.accessLevel)
	}
	if len(sent(u, wire.OpSrvItem)) == 0 {
		t.Fatal("the client was not sent the new items")
	}

	// Another account's packet, another character, a changed slot: ignored.
	apply(acctedit.Edit{Op: acctedit.OpGold, Value: 1}, "OTHER")
	apply(acctedit.Edit{Op: acctedit.OpGold, Char: 2, Value: 1}, "TESTER")
	apply(acctedit.Edit{Op: acctedit.OpItem, Where: wire.SlotInv, Slot: 4, Item: []int{0, 0, 0, 0, 0, 0, 0},
		Expect: []int{413, 0, 0, 0, 0, 0, 0}}, "TESTER")
	if pl.Gold != 123456 || pl.Inventory[4].Index != 412 {
		t.Fatal("a refused edit changed the character")
	}
	// The removal with the right expected item works.
	apply(acctedit.Edit{Op: acctedit.OpItem, Where: wire.SlotInv, Slot: 4, Item: []int{0, 0, 0, 0, 0, 0, 0},
		Expect: []int{412, 0, 0, 0, 0, 0, 0}}, "TESTER")
	if pl.Inventory[4].Index != 0 {
		t.Fatal("item not removed")
	}
}

func TestCashDeliveryInGame(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	wire.PutCString(u.account.Username[:], "TESTER")
	p := wire.CashDelivery{Header: wire.NewHeader(wire.OpCashDelivery, 1), Cash: 50}
	wire.PutCString(p.Username[:], "TESTER")
	g.cashDelivery(u, packet(&p))
	if u.account.Cash != 50 {
		t.Fatalf("cash %d", u.account.Cash)
	}
}

func TestAdminLockReload(t *testing.T) {
	g := worldGame(t)
	locks := memLocks{}
	g.cfg.Locks = locks
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	wire.PutCString(u.account.Username[:], "TESTER")
	lock := LockAccount{Header: wire.NewHeader(opCliLock, 1)}
	copy(lock.Password[:], "12345678")
	lock.Conf[lockTrade] = 1
	g.requestLock(u, packet(&lock))

	// The panel writes a new password: the live lock follows it.
	copy(locks["TESTER"][:8], "ABCDEFGH")
	e := acctedit.Edit{Op: acctedit.OpLock}
	p := e.Packet("TESTER", 1)
	g.adminEdit(u, packet(&p))
	if wire.CString(u.lock.pass[:]) != "ABCDEFGH" || !u.lock.desc[lockTrade] {
		t.Fatalf("lock after reset %+v", u.lock)
	}

	// The panel removes the lock: the player is unlocked at once.
	delete(locks, "TESTER")
	g.adminEdit(u, packet(&p))
	if u.lock.set() || u.lock.desc[lockTrade] {
		t.Fatalf("lock after removal %+v", u.lock)
	}
}

func TestAdminMoveAndRemoveInGame(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	pl.Inventory[4] = wire.Item{Index: 805, Effect: [3]wire.ItemEffect{{Index: EF_SANC, Value: 300}}}
	pl.Inventory[5] = wire.Item{Index: 412}
	pl.Equip[6] = wire.Item{Index: 806}

	g.applyAdminEdit(u, acctedit.Edit{Op: acctedit.OpMove, Slot: 4, To: 6,
		Expect: acctedit.Ints(pl.Inventory[4]), ExpectTo: acctedit.Ints(pl.Equip[6])})
	if pl.Equip[6].Index != 805 || pl.Equip[6].Effect[0].Value != 300 || pl.Inventory[4].Index != 806 {
		t.Fatalf("move: %+v / %+v", pl.Equip[6], pl.Inventory[4])
	}

	// One stale slot refuses the whole removal.
	refs := []acctedit.SlotRef{{Where: wire.SlotInv, Slot: 5, Expect: acctedit.Ints(pl.Inventory[5])},
		{Where: wire.SlotEquip, Slot: 6, Expect: []int{1, 0, 0, 0, 0, 0, 0}}}
	p := (&acctedit.Edit{Op: acctedit.OpRemove, Removes: refs}).RemovePacket(wire.CString(u.account.Username[:]), 1)
	g.adminRemove(u, packet(&p))
	if pl.Inventory[5].Index != 412 {
		t.Fatal("a removal with a changed slot was applied")
	}
	refs[1].Expect = acctedit.Ints(pl.Equip[6])
	p = (&acctedit.Edit{Op: acctedit.OpRemove, Removes: refs}).RemovePacket(wire.CString(u.account.Username[:]), 1)
	g.adminRemove(u, packet(&p))
	if pl.Inventory[5].Index != 0 || pl.Equip[6].Index != 0 {
		t.Fatalf("remove: %+v / %+v", pl.Inventory[5], pl.Equip[6])
	}
}

// The free points are derived from the level on every recalculation; what
// the panel sets must survive that, and a level up must still add points.
func TestAdminStatsSurviveRecalculation(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	wire.PutCString(u.account.Username[:], "TESTER")
	u.charSlot = 0
	pl := &g.mobs[1].info.Player
	pl.ClassInfo = 0
	pl.BStatus.Level = 9
	pl.BStatus.STR, pl.BStatus.INT, pl.BStatus.DEX, pl.BStatus.CON = 8, 4, 7, 6
	g.getCurrentScore(1)
	if pl.StatusPoint != 45 || pl.SkillPoint != 27 || pl.MasterPoint != 18 {
		t.Fatalf("starting points %d %d %d", pl.StatusPoint, pl.SkillPoint, pl.MasterPoint)
	}

	e := acctedit.Edit{Op: acctedit.OpStats, LearnAdd: 1 << 2, LearnCost: g.data.Skills[2].Points, Stats: map[string]int64{
		"str": 1008, "status_point": 7, "mastery0": 50, "master_point": 3, "skill_point": 11, "exp": 12345}}
	p := e.StatsPacket("TESTER", 1)
	g.adminStats(u, packet(&p))
	check := func(status, master, skill uint16) {
		t.Helper()
		if pl.BStatus.STR != 1008 || pl.BStatus.Mastery[0] != 50 || pl.Learn[0]&(1<<2) == 0 || pl.Exp != 12345 ||
			pl.StatusPoint != status || pl.MasterPoint != master || pl.SkillPoint != skill {
			t.Fatalf("STR %d mastery %d learn %x exp %d points %d %d %d", pl.BStatus.STR, pl.BStatus.Mastery[0], pl.Learn[0], pl.Exp,
				pl.StatusPoint, pl.MasterPoint, pl.SkillPoint)
		}
	}
	check(7, 3, 11)
	if pl.Status.STR < 1008 || len(sent(u, wire.OpSrvEtc)) == 0 {
		t.Fatalf("live status %d, or the client was not told", pl.Status.STR)
	}
	g.getCurrentScore(1)
	check(7, 3, 11)

	pl.BStatus.Level = 19 // ten levels: +50 status, +20 mastery, +30 skill
	g.getCurrentScore(1)
	check(57, 23, 41)

	// Other values stay when they are not in the edit.
	only := acctedit.Edit{Op: acctedit.OpStats, Stats: map[string]int64{"dex": 17}}
	p = only.StatsPacket("TESTER", 1)
	g.adminStats(u, packet(&p))
	g.getCurrentScore(1)
	if pl.BStatus.DEX != 17 {
		t.Fatalf("DEX %d", pl.BStatus.DEX)
	}
	check(57, 23, 41)
}
