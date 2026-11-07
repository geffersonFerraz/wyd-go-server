package game

import (
	"encoding/binary"
	"testing"

	"dbsrv/internal/wire"
)

func TestAuctionPacketSizes(t *testing.T) {
	for _, c := range []struct {
		v    any
		size int
	}{
		{Listing{}, 52}, {AuctionPage{}, 2516}, {AuctionQuery{}, 24}, {AuctionSearch{}, 48},
		{AuctionAdd{}, 56}, {AuctionBuy{}, 68}, {AuctionFunds{}, 24}, {StoreMob{}, 268},
	} {
		if got := binary.Size(c.v); got != c.size {
			t.Errorf("%T: %d bytes, want %d", c.v, got, c.size)
		}
	}
}

func TestAuctionStore(t *testing.T) {
	g := worldGame(t)
	if g.data.StoreBase == nil {
		t.Skip("no npc/Base")
	}
	c := g.data.Cities[0]
	seller := placePlayer(g, int(c.CityMinX+c.CityMaxX)/2, int(c.CityMinY+c.CityMaxY)/2, 0)
	if v := g.data.village(int(g.mobs[1].target.X), int(g.mobs[1].target.Y)); v < 0 || v >= 5 {
		t.Skip("test spot is not in town")
	}
	buyer := secondPlayer(g)
	listen(seller, buyer)
	buyer.mac[0] = 1 // another computer
	sp, bp := &g.mobs[1].info.Player, &g.mobs[2].info.Player

	open := AuctionPage{Header: wire.NewHeader(opCliAuctionCreate, 1)}
	copy(open.List[0].Name[:], "Loja do Teste")
	g.requestAuctionCreate(seller, packet(&open))
	if !seller.store.open || seller.store.mob == 0 || g.mobs[seller.store.mob].storeOwner != 1 {
		t.Fatalf("store not opened: %+v", seller.store.open)
	}

	add := func(slot int, it wire.Item, cat, price int32) {
		g.requestAuctionAdd(seller, packet(&AuctionAdd{Header: wire.NewHeader(opCliAuctionAdd, 1),
			ItemType: cat, SlotBank: int32(slot), Price: price, Item: it}))
	}
	sword := wire.Item{Index: 412}
	seller.account.StorageItems[5] = sword
	add(5, sword, 7, 5000) // unknown currency
	if seller.store.list[0].Item.Index != 0 {
		t.Fatal("listed with category 7")
	}
	add(5, sword, auctionGold, 5000)
	add(5, sword, auctionGold, 5000) // same slot twice
	if seller.store.list[0].Item != sword || seller.store.list[1].Item.Index != 0 {
		t.Fatalf("listing: %+v / %+v", seller.store.list[0], seller.store.list[1])
	}
	// A listed storage item cannot be moved.
	g.requestMoveItem(seller, packet(&wire.MoveItem{Header: wire.NewHeader(wire.OpCliMoveItem, 1),
		SrcType: wire.SlotStorage, SrcSlot: 5, DstType: wire.SlotInv, DstSlot: 10}))
	if seller.account.StorageItems[5] != sword {
		t.Fatal("listed item moved out of the storage")
	}

	// Gold sale.
	bp.Gold = 10000
	buy := func(slot int, l Listing) {
		g.requestAuctionBuy(buyer, packet(&AuctionBuy{Header: wire.NewHeader(opCliAuctionBuy, 2),
			ItemType: l.Category, SlotBank: l.SlotBank, ItemSlot: int32(slot), Price: l.Price, Tax: l.TaxRate, Seller: 1, Item: l.Item}))
	}
	coin := seller.account.StorageCoin
	buy(0, seller.store.list[0])
	if firstSlotOf(bp, 412) < 0 || bp.Gold != 5000 || seller.account.StorageCoin != coin+5000 {
		t.Fatalf("gold sale: buyer gold %d, seller storage coin %d", bp.Gold, seller.account.StorageCoin)
	}
	if seller.account.StorageItems[5].Index != 0 || seller.store.list[0].Item.Index != 0 {
		t.Fatal("sold item still in the seller's storage or list")
	}

	// Gold bar sale.
	gem := wire.Item{Index: 413}
	seller.account.StorageItems[6] = gem
	add(6, gem, auctionBars, 2)
	bars := wire.Item{Index: 4011}
	bars.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 5}
	bp.Inventory[20] = bars
	buy(0, seller.store.list[0])
	if itemAmount(&bp.Inventory[20]) != 3 || firstSlotOf(bp, 413) < 0 {
		t.Fatalf("bar sale: buyer bars %+v", bp.Inventory[20])
	}
	got := 0
	for _, it := range seller.account.StorageItems {
		if it.Index == 4011 {
			got += itemAmount(&it)
		}
	}
	if got != 2 {
		t.Fatalf("seller got %d bars", got)
	}

	// A price of 0 bars (or donate) is refused: it minted a bar per sale.
	seller.account.StorageItems[10] = sword
	add(10, sword, auctionBars, 0)
	add(10, sword, auctionDonate, 0)
	if seller.store.listingOf(10) >= 0 {
		t.Fatal("listed for 0 bars / 0 donate")
	}
	seller.account.StorageItems[10] = wire.Item{}

	// Bought from the "all stores" page: the client sends the page
	// position, not the seller's list slot.
	seller.account.StorageItems[8] = sword
	seller.account.StorageItems[9] = gem
	add(8, sword, auctionGold, 3000)
	add(9, gem, auctionGold, 4000)
	bp.Gold = 10000
	l9 := seller.store.list[1]
	g.requestAuctionBuy(buyer, packet(&AuctionBuy{Header: wire.NewHeader(opCliAuctionBuy, 2),
		ItemType: l9.Category, SlotBank: l9.SlotBank, ItemSlot: 7, Price: l9.Price, Tax: l9.TaxRate, Seller: 1, Item: l9.Item}))
	if seller.account.StorageItems[9].Index != 0 || bp.Gold != 6000 || seller.store.list[0].SlotBank != 8 {
		t.Fatalf("page-position buy: storage %+v gold %d", seller.account.StorageItems[9], bp.Gold)
	}
	seller.store.removeListing(0)
	seller.account.StorageItems[8] = wire.Item{}

	// The seller cannot buy from their own store.
	seller.account.StorageItems[7] = sword
	add(7, sword, auctionGold, 2000)
	sp.Gold = 1_000_000
	l := seller.store.list[0]
	g.requestAuctionBuy(seller, packet(&AuctionBuy{Header: wire.NewHeader(opCliAuctionBuy, 1),
		ItemType: l.Category, SlotBank: l.SlotBank, Price: l.Price, Tax: l.TaxRate, Seller: 1, Item: l.Item}))
	if seller.account.StorageItems[7] != sword {
		t.Fatal("seller bought from their own store")
	}

	// Honor while open; /fecharloja closes the store.
	seller.store.timer = g.cfg.Store.HonorEverySeconds - 1
	honra := seller.account.Honra
	g.storeTimer()
	if seller.account.Honra != honra+1 {
		t.Fatal("no honor for the open store")
	}
	mob := seller.store.mob
	g.storeCommand(seller, "fecharloja")
	if seller.store.open || g.mobs[mob].mode != 0 {
		t.Fatal("store not closed")
	}
}
