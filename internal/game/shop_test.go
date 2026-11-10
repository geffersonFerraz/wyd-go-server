package game

import (
	"log/slog"
	"testing"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// worldGame loads the real TMSRV data and spawns NPCs (no network).
func worldGame(t *testing.T) *Game {
	t.Helper()
	d := LoadRealData(t)
	applyLegacyGameData(t, d)
	g := New(Config{Channel: 1, NewbieZone: 35}, d, slog.New(slog.DiscardHandler))
	g.minTimer()
	return g
}

// placePlayer puts a playing user (no connection) at (x, y).
func placePlayer(g *Game, x, y int, gold int64) *User {
	u := g.users[1]
	u.status = userPlay
	m := &g.mobs[1]
	m.mode = 1
	m.info.Player.Gold = gold
	m.info.Player.Status.CurHP = 100
	m.info.Player.Status.Level = 10
	m.info.Player.Equip[0].Effect[1].Value = evoMortal
	m.info.Player.Inventory[63].Effect[0].Index = 75 // neutral PK points
	g.emptyMobGrid(1, &x, &y)
	m.target = wire.Position{X: uint16(x), Y: uint16(y)}
	g.setCell(x, y, 1)
	return u
}

func packet(v any) protocol.Packet {
	return protocol.Packet{Raw: wire.MustMarshal(v)}
}

func TestShopBuySellRepurchase(t *testing.T) {
	g := worldGame(t)
	// Find a merchant selling something priced below 1000.
	npc, slot := 0, 0
	for id := firstNPC; id < MaxMob && npc == 0; id++ {
		m := &g.mobs[id]
		if m.mode == 0 || isMerchant(&m.info.Player) == 0 {
			continue
		}
		for s := range 27 {
			it := m.info.Player.Inventory[s]
			if p := g.data.Item(it.Index).Price; it.Index != 0 && p > 0 && p < 1000 && !soldByAmount(it.Index) {
				npc, slot = id, s
				break
			}
		}
	}
	if npc == 0 {
		t.Fatal("no merchant found")
	}
	nt := g.mobs[npc].target
	u := placePlayer(g, int(nt.X)+1, int(nt.Y), 5000)
	pl := &g.mobs[1].info.Player
	item := g.mobs[npc].info.Player.Inventory[slot]
	price := int64(g.data.Item(item.Index).Price)
	perc := int64(g.data.Cities[g.shopCity(npc)].PercImpost)
	cost := price + price*perc/100

	pl.Inventory = [64]wire.Item{}
	pl.Inventory[0] = wire.Item{Index: 412}
	pl.Inventory[1] = wire.Item{Index: 412}
	pl.Inventory[2] = wire.Item{Index: 412}
	// The slot the client asks for is ignored: the first free cell wins.
	g.requestBuyShop(u, packet(&wire.BuyShop{MobID: int16(npc), SellSlot: int16(slot), InvSlot: 45}))
	if pl.Inventory[3].Index != item.Index || pl.Inventory[45].Index != 0 || int64(pl.Gold) != 5000-cost {
		t.Fatalf("buy: slot3=%d slot45=%d gold=%d (cost %d)", pl.Inventory[3].Index, pl.Inventory[45].Index, pl.Gold, cost)
	}

	before := pl.Gold
	g.requestSellShop(u, packet(&wire.SellShop{NpcID: int16(npc), Type: wire.SlotInv, SellSlot: 3}))
	if pl.Inventory[3].Index != 0 || pl.Gold <= before && price >= 4 {
		t.Fatalf("sell: slot3=%d gold %d -> %d", pl.Inventory[3].Index, before, pl.Gold)
	}
	if u.repurchase[0].Index != item.Index {
		t.Fatalf("repurchase list: %+v", u.repurchase[0])
	}

	// Buy it back (mobID < 1000): list price, first free slot.
	afterSell := pl.Gold
	pl.Inventory[0] = wire.Item{}
	g.requestBuyShop(u, packet(&wire.BuyShop{MobID: 0, SellSlot: 0, InvSlot: 0}))
	if pl.Inventory[0].Index != item.Index || int64(pl.Gold) != int64(afterSell)-price || u.repurchase[0].Index != 0 {
		t.Fatalf("buy back: slot0=%d gold %d -> %d", pl.Inventory[0].Index, afterSell, pl.Gold)
	}

	// Blocked items cannot be sold.
	pl.Inventory[4] = wire.Item{Index: 509}
	g.requestSellShop(u, packet(&wire.SellShop{NpcID: int16(npc), Type: wire.SlotInv, SellSlot: 4}))
	if pl.Inventory[4].Index != 509 {
		t.Fatal("sold a blocked item")
	}
}

func TestTeleport(t *testing.T) {
	g := worldGame(t)
	idx := -1
	for i, tp := range g.data.Teleports {
		if tp.Price > 0 && i != 6 && i != 14 && i != 43 && (i < 44 || i > 47) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("no paid teleport")
	}
	tp := g.data.Teleports[idx]
	u := placePlayer(g, tp.SrcX, tp.SrcY, int64(tp.Price)-1)
	g.requestTeleport(u)
	if pos := g.mobs[1].target; int(pos.X) != tp.SrcX && int(pos.X) > tp.SrcX+5 {
		t.Fatal("teleported without enough gold")
	}
	g.mobs[1].info.Player.Gold = int64(tp.Price) + 10
	g.requestTeleport(u)
	pos := g.mobs[1].target
	if distance(int(pos.X), int(pos.Y), tp.DestX, tp.DestY) > 6 {
		t.Fatalf("at %v, want near %dx%d", pos, tp.DestX, tp.DestY)
	}
	if g.mobs[1].info.Player.Gold != 10 {
		t.Fatalf("gold %d, want 10", g.mobs[1].info.Player.Gold)
	}
	if g.cell(int(pos.X), int(pos.Y)) != 1 {
		t.Fatal("grid not updated")
	}
}

func packetRaw(raw []byte) protocol.Packet {
	var h wire.Header
	_ = wire.Unmarshal(raw, &h)
	return protocol.Packet{Header: protocol.Header{Size: h.Size, PacketID: h.PacketID, ClientID: h.ClientID, TimeStamp: h.TimeStamp}, Raw: raw}
}

// applyLegacyGameData loads the test copy of the game configuration.
func applyLegacyGameData(t *testing.T, d *Data) {
	t.Helper()
	if err := d.ApplyGameData(GameDataFixture(t)); err != nil {
		t.Fatal(err)
	}
}
