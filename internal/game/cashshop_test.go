package game

import (
	"encoding/binary"
	"testing"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

func TestCashShopPacketSizes(t *testing.T) {
	for name, c := range map[string]struct {
		v    any
		want int
	}{"info": {wire.CashShopInfo{}, 1488}, "buy": {wire.CashShopBuy{}, 184}, "honor": {wire.HonorShopList{}, 400}} {
		if got := binary.Size(c.v); got != c.want {
			t.Errorf("%s = %d, want %d", name, got, c.want)
		}
	}
}

// The shops converted from CashShop.xml and MarketHonra.txt, as configured.
func TestShippedShops(t *testing.T) {
	s, err := LoadSettings("../../tmsrv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tabs, honor := s.cashShopTabs(), s.honorMarketItems()
	if len(tabs) != 8 || tabs[0].ID != 0 || tabs[0].Name != "BIGSALE" {
		t.Fatalf("tabs: %d", len(tabs))
	}
	first := tabs[0].Slots[0]
	if first.Item.Index != 4533 || first.Item.Effect[0] != (wire.ItemEffect{Index: 61, Value: 1}) || first.Price != 200 || first.Amount != 1 || first.SlotFree != 1000 {
		t.Fatalf("first item %+v", first)
	}
	if tabs[0].Slots[64].SlotFree != -1 || tabs[0].Slots[64].Item.Index != 0 {
		t.Fatalf("empty slot %+v", tabs[0].Slots[64])
	}
	sold := 0
	for i := range tabs {
		for _, it := range tabs[i].Slots {
			if it.Item.Index != 0 {
				sold++
			}
		}
	}
	if sold != 92 {
		t.Fatalf("%d cash items, want 92", sold)
	}
	if len(honor) != 20 {
		t.Fatalf("%d honor items, want 20", len(honor))
	}
	for _, h := range honor {
		if h.Item.Index != 3915 {
			continue
		}
		want := [3]wire.ItemEffect{{Index: 106}, {Index: 107, Value: 3}}
		if h.Price != 100 || h.Item.Effect != want {
			t.Fatalf("3915: %+v", h)
		}
	}
}

func TestShopSettingsRejected(t *testing.T) {
	ok := CashShopItemSettings{Slot: 1, Item: 412, Price: 10, Amount: 1, Limit: 1}
	bad := func(change func(*CashShopItemSettings)) Settings {
		it := ok
		change(&it)
		return Settings{CashShop: []CashShopTabSettings{{ID: 1, Name: "x", Items: []CashShopItemSettings{it}}}}
	}
	for name, s := range map[string]Settings{
		"free":      bad(func(i *CashShopItemSettings) { i.Price = 0 }),
		"slot":      bad(func(i *CashShopItemSettings) { i.Slot = 65 }),
		"item":      bad(func(i *CashShopItemSettings) { i.Item = MaxItemList }),
		"effects":   bad(func(i *CashShopItemSettings) { i.Effects = make([][2]int, 4) }),
		"amount":    bad(func(i *CashShopItemSettings) { i.Amount = 0 }),
		"limit":     bad(func(i *CashShopItemSettings) { i.Limit = -2 }),
		"tab twice": {CashShop: []CashShopTabSettings{{ID: 1}, {ID: 1}}},
		"honor":     {HonorMarket: []HonorItemSettings{{Item: 412}}},
		"honor2":    {HonorMarket: []HonorItemSettings{{Item: 412, Price: 1}, {Item: 412, Price: 2}}},
	} {
		if err := s.validateShops(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	gift := Settings{CashShop: []CashShopTabSettings{{ID: 1, Items: []CashShopItemSettings{{Item: 412, Amount: 1, Effects: [][2]int{{efGiftID, 3}}}}}}}
	if err := gift.validateShops(); err != nil {
		t.Errorf("gift refused: %v", err)
	}
}

func cashGame(t *testing.T) (*Game, *User, *CashShopItem) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	tab := CashShopTab{ID: 3}
	for i := range tab.Slots {
		tab.Slots[i].SlotFree = -1
	}
	tab.Slots[2] = CashShopItem{Item: wire.Item{Index: 412}, Price: 10, SlotFree: 4}
	tab.Slots[3] = CashShopItem{Item: wire.Item{Index: 413, Effect: [3]wire.ItemEffect{{Index: efGiftID, Value: 6}}}, Price: 10, SlotFree: 1}
	g.cfg.CashShop = []CashShopTab{tab}
	u.account.Cash = 100
	return g, u, &g.cfg.CashShop[0].Slots[2]
}

func buyCash(g *Game, u *User, s *CashShopItem, quant int16) {
	p := wire.CashShopBuy{Header: wire.NewHeader(opCashShopBuy, 1), Item: s.Item, Price: int16(s.Price), Quant: quant, Aba: 3}
	g.requestCashShopBuy(u, protocol.Packet{Raw: wire.MustMarshal(&p)})
}

func TestCashShopBuy(t *testing.T) {
	g, u, s := cashGame(t)
	pl := &g.mobs[1].info.Player
	buyCash(g, u, s, 3)
	if u.account.Cash != 70 || firstSlotOf(pl, 412) < 0 {
		t.Fatalf("buy 3: cash %d", u.account.Cash)
	}
	// Negative, zero and above the limit are refused.
	for _, q := range []int16{-5, 0, 5} {
		buyCash(g, u, s, q)
		if u.account.Cash != 70 {
			t.Fatalf("quant %d: cash %d", q, u.account.Cash)
		}
	}
	// Not enough cash.
	u.account.Cash = 5
	buyCash(g, u, s, 1)
	if u.account.Cash != 5 {
		t.Fatal("bought without cash")
	}
	// A price the shop does not have.
	u.account.Cash = 100
	p := wire.CashShopBuy{Header: wire.NewHeader(opCashShopBuy, 1), Item: s.Item, Price: 1, Quant: 1, Aba: 3}
	g.requestCashShopBuy(u, protocol.Packet{Raw: wire.MustMarshal(&p)})
	if u.account.Cash != 100 {
		t.Fatal("bought at a forged price")
	}
}

// A gift is free but comes once per account (the C++ gave it forever).
func TestCashShopGiftOnce(t *testing.T) {
	g, u, _ := cashGame(t)
	pl := &g.mobs[1].info.Player
	gift := &g.cfg.CashShop[0].Slots[3]
	buyCash(g, u, gift, 1)
	if u.account.Cash != 100 || firstSlotOf(pl, 413) < 0 || u.account.SingleGift&(1<<6) == 0 {
		t.Fatalf("gift: cash %d", u.account.Cash)
	}
	pl.Inventory[firstSlotOf(pl, 413)] = wire.Item{}
	buyCash(g, u, gift, 1)
	if firstSlotOf(pl, 413) >= 0 {
		t.Fatal("gift given twice")
	}
}

func TestCashShopFullInventoryCostsNothing(t *testing.T) {
	g, u, s := cashGame(t)
	pl := &g.mobs[1].info.Player
	for i := range invSlots {
		pl.Inventory[i] = wire.Item{Index: 1}
	}
	buyCash(g, u, s, 1)
	if u.account.Cash != 100 {
		t.Fatal("charged with no room")
	}
}

func TestHonorMarket(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	g.cfg.HonorMarket = []HonorItem{{Item: wire.Item{Index: 412, Effect: [3]wire.ItemEffect{{Index: EF_AMOUNT, Value: 1}}}, Price: 50}}
	npc := 1500
	g.mobs[npc].mode = modeNPCIdle
	g.mobs[npc].info.Player.Inventory[0] = wire.Item{Index: 412}
	g.mobs[npc].info.Player.Inventory[1] = wire.Item{Index: 413}
	pl := &g.mobs[1].info.Player
	u.account.Honra = 60
	g.buyHonorMarket(u, npc, 0, 5)
	if u.account.Honra != 10 || pl.Inventory[5].Index != 412 || pl.Inventory[5].Effect[0].Index != EF_AMOUNT {
		t.Fatalf("buy: honra %d item %+v", u.account.Honra, pl.Inventory[5])
	}
	g.buyHonorMarket(u, npc, 0, 6) // not enough honor
	g.buyHonorMarket(u, npc, 1, 6) // not for sale
	if u.account.Honra != 10 || pl.Inventory[6].Index != 0 {
		t.Fatal("refused purchases changed something")
	}
}

func TestEssenceFeedsMount(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	roll := rnd
	defer func() { rnd = roll }()
	rnd = func() int { return 0 }
	pl := &g.mobs[1].info.Player
	pl.Equip[14] = wire.Item{Index: 2361}
	pl.Equip[14].Effect[1] = wire.ItemEffect{Index: 30, Value: 10}
	pl.Inventory[2] = wire.Item{Index: 2391}
	pl.Inventory[2].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 5}
	send := func(slot, amount int32) {
		p := wire.EssencePacket{Header: wire.NewHeader(opEssenceUse, 1), Slot: slot, Amount: amount}
		g.requestEssenceUse(u, protocol.Packet{Raw: wire.MustMarshal(&p)})
	}
	send(2, 9) // more than held
	send(2, -1)
	if itemAmount(&pl.Inventory[2]) != 5 || pl.Equip[14].Effect[1].Index != 30 {
		t.Fatal("bad amounts changed something")
	}
	send(2, 3)
	if itemAmount(&pl.Inventory[2]) != 2 || pl.Equip[14].Effect[1].Index != 33 {
		t.Fatalf("3 essences: left %d, level %d", itemAmount(&pl.Inventory[2]), pl.Equip[14].Effect[1].Index)
	}
	// The wrong essence for the mount.
	pl.Inventory[3] = wire.Item{Index: 2395}
	send(3, 1)
	if pl.Inventory[3].Index != 2395 || pl.Equip[14].Effect[1].Index != 33 {
		t.Fatal("wrong essence used")
	}
}

func TestReconnectInsideEventRoomRecalls(t *testing.T) {
	inside := []struct{ x, y int }{{1050, 3550}, {3400, 1600}, {3650, 3650}, {480, 3900}}
	for _, p := range inside {
		found := false
		for _, a := range reconnectAreas {
			found = found || a.has(p.x, p.y)
		}
		if !found {
			t.Errorf("%v not an event room", p)
		}
	}
	for _, a := range reconnectAreas {
		if a.has(2100, 2100) { // Armia
			t.Fatalf("town inside %+v", a)
		}
	}
}
