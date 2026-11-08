package game

import (
	"encoding/binary"
	"testing"

	"dbsrv/internal/wire"
)

func rouletteGame(t *testing.T) (*Game, *User, *wire.CharInfo) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	b := &g.data.Roulette[7]
	*b = RouletteBox{Status: true, HasSuperDrop: true, KeyID: 3}
	b.Roll[0], b.Meta[0] = wire.Item{Index: 412}, RollMeta{Chance: 10, Rare: 2, EvolBar: 40}
	b.Roll[1], b.Meta[1] = wire.Item{Index: 413}, RollMeta{Chance: 1, Rare: 4, Super: 1, SendMsg: 1}
	info := &g.mobs[1].info
	info.Player.Inventory[0] = wire.Item{Index: itemRouletteBox}
	info.Player.Inventory[0].Effect[0] = wire.ItemEffect{Index: efInformation, Value: 7}
	info.Player.Inventory[0].Effect[1] = wire.ItemEffect{Index: EF_AMOUNT, Value: 3}
	info.Player.Inventory[1] = wire.Item{Index: itemRouletteKey}
	info.Player.Inventory[1].Effect[0] = wire.ItemEffect{Index: efInformation, Value: 3}
	return g, u, info
}

func TestRollItemSize(t *testing.T) {
	if got := binary.Size(wire.RollItem{}); got != 218 {
		t.Fatalf("MSG_RollItem = %d", got)
	}
}

func TestRouletteSpinNeedsKeyAndGivesPrize(t *testing.T) {
	g, u, info := rouletteGame(t)
	roll := rnd
	defer func() { rnd = roll }()
	rnd = func() int { return 0 } // always the first prize

	g.openRoulette(u, 0)
	if info.Roleta.BauSlot != 0 {
		t.Fatalf("box slot %d", info.Roleta.BauSlot)
	}
	key := info.Player.Inventory[1]
	info.Player.Inventory[1] = wire.Item{}
	g.spinRoulette(u)
	if u.rouletteReward.Index != 0 || itemAmount(&info.Player.Inventory[0]) != 3 {
		t.Fatal("spun without the key")
	}
	info.Player.Inventory[1] = key

	g.spinRoulette(u)
	if u.rouletteReward.Index != 412 || info.Player.Inventory[1].Index != 0 || itemAmount(&info.Player.Inventory[0]) != 2 || u.rouletteEvol[7] != 40 {
		t.Fatalf("spin: reward %d key %d box %d bar %d", u.rouletteReward.Index, info.Player.Inventory[1].Index,
			itemAmount(&info.Player.Inventory[0]), u.rouletteEvol[7])
	}
	// No second spin while the prize is pending.
	g.spinRoulette(u)
	if itemAmount(&info.Player.Inventory[0]) != 2 {
		t.Fatal("spun with a prize pending")
	}
	g.getRouletteReward(u)
	if u.rouletteReward.Index != 0 || firstSlotOf(&info.Player, 412) < 0 {
		t.Fatal("prize not delivered")
	}
	// Claiming twice gives nothing.
	before := info.Player.Inventory
	g.getRouletteReward(u)
	if before != info.Player.Inventory {
		t.Fatal("prize given twice")
	}
}

func TestRouletteSuperDrop(t *testing.T) {
	g, u, info := rouletteGame(t)
	g.data.Roulette[7].KeyID = -1
	u.rouletteEvol[7] = rouletteBarFull
	g.openRoulette(u, 0)
	g.spinRoulette(u)
	if u.rouletteReward.Index != 413 || u.rouletteEvol[7] != 0 {
		t.Fatalf("super: reward %d bar %d", u.rouletteReward.Index, u.rouletteEvol[7])
	}
	_ = info
}

// A bad box id, or a box with no prizes, must not crash nor eat the box.
func TestRouletteBadBoxes(t *testing.T) {
	g, u, info := rouletteGame(t)
	info.Player.Inventory[0].Effect[0].Value = 200
	g.openRoulette(u, 0)
	info.Roleta.BauSlot = 0
	g.spinRoulette(u)
	if u.rouletteReward.Index != 0 || itemAmount(&info.Player.Inventory[0]) != 3 {
		t.Fatal("spun a box out of range")
	}
	info.Player.Inventory[0].Effect[0].Value = 7
	g.data.Roulette[7].Meta = [maxRouletteItem]RollMeta{}
	g.spinRoulette(u)
	if u.rouletteReward.Index != 0 || itemAmount(&info.Player.Inventory[0]) != 3 || info.Player.Inventory[1].Index == 0 {
		t.Fatal("a box without prizes took the box or the key")
	}
}

// Leaving with a prize pending and a full inventory sends it to the storage.
func TestRouletteRewardOnLeaving(t *testing.T) {
	g, u, info := rouletteGame(t)
	out := captureDB(g)
	for i := range invSlots {
		info.Player.Inventory[i] = wire.Item{Index: 1}
	}
	u.rouletteReward = wire.Item{Index: 412}
	g.settleRouletteReward(u)
	if u.rouletteReward.Index != 0 {
		t.Fatal("prize kept")
	}
	var p wire.ItemDelivery
	if err := wire.Unmarshal(<-out, &p); err != nil {
		t.Fatal(err)
	}
	if p.Header.PacketID != wire.OpItemDeliveryReturn || p.Item.Index != 412 {
		t.Fatalf("delivery %+v", p)
	}
}
