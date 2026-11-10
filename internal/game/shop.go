package game

import (
	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// NPC shops: RequestOpenShop.cpp, RequestBuyShop.cpp, RequestSellShop.cpp,
// SellItem and SendRepurchase.

const (
	nnCantDepositThatMuch  = 44
	nnCantWithdrawThatMuch = 45
	nnCantGetMoreThan2G    = 273
	nnNotEnoughGoldBuyShop = 298
)

// isMerchant reports a shop NPC (bStatus.Merchant.Merchant 1, 3 or 19).
func isMerchant(pl *wire.Mob) int {
	merch := int(pl.BStatus.Merchant & 0x0F)
	if merch == 1 || merch == 3 || merch == 19 {
		return merch
	}
	return 0
}

// shopCity is the city whose tax applies to an NPC (GetVillage, 5 -> 4).
func (g *Game) shopCity(id int) int {
	t := g.mobs[id].target
	c := g.data.village(int(t.X), int(t.Y))
	if c == 5 {
		c = 4
	}
	return c
}

func (g *Game) requestOpenShop(u *User, pkt protocol.Packet) {
	if u.status != userPlay {
		return
	}
	var p wire.OpenShop
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	if wire.CString(p.FindName[:]) == honorShopName {
		g.openHonorMarket(u)
		return
	}
	id := int(p.NpcID)
	if !isNPC(id) || g.mobs[id].mode == 0 {
		return
	}
	npc := &g.mobs[id].info.Player
	merch := isMerchant(npc)
	if merch == 0 {
		return
	}
	out := wire.ShopList{Header: wire.NewHeader(wire.OpSrvShopList, wire.ClientIDSystem), Unknow: int32(merch)}
	for i := range 27 {
		out.Item[i] = npc.Inventory[i%9+(i/9)*27]
	}
	if npc.BStatus.INT != 4001 {
		out.Taxes = g.data.Cities[g.shopCity(id)].PercImpost
	}
	g.queue(u, &out)
}

// The inventory has one slot per cell of the 9x7 grid (slot 63 is the PK
// counter). The Bolsa do Andarilho, which unlocked slots 30..59, no longer
// exists; characters saved with one had it on slot 60 / 61.
const (
	invSlots = 63
	itemBag  = 3467
	// invUsable: the client draws items only on slots 0..59; the last 3
	// cells of the grid hold the inventory buttons.
	invUsable = 60
)

// clearBagSlots removes the bags of the old system from slots 60 and 61.
func clearBagSlots(pl *wire.Mob) {
	for _, s := range [2]int{60, 61} {
		if pl.Inventory[s].Index == itemBag {
			pl.Inventory[s] = wire.Item{}
		}
	}
}

// invSlotUsable reports whether an inventory slot exists.
func invSlotUsable(_ *wire.Mob, slot int) bool { return slot >= 0 && slot < invSlots }

// firstEmptySlot is GetFirstSlot(clientId, 0).
func firstEmptySlot(pl *wire.Mob) int {
	for i := range invSlots {
		if pl.Inventory[i].Index == 0 {
			return i
		}
	}
	return -1
}

// itemAmount is GetItemAmount.
func itemAmount(it *wire.Item) int {
	for i := range 3 {
		if it.Effect[i].Index == EF_AMOUNT {
			if it.Effect[i].Value == 0 {
				return 1
			}
			return int(it.Effect[i].Value)
		}
	}
	return 1
}

func soldByAmount(id uint16) bool { return id == 419 || id == 420 || id == 412 || id == 413 }

func (g *Game) requestBuyShop(u *User, pkt protocol.Packet) {
	if u.status != userPlay {
		return
	}
	var p wire.BuyShop
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	mobID, sellSlot, invSlot := int(p.MobID), int(p.SellSlot), int(p.InvSlot)
	if mobID < 0 || mobID >= MaxMob || sellSlot < 0 || sellSlot > 63 || invSlot < 0 || invSlot >= invSlots {
		return
	}
	pl := &g.mobs[u.id].info.Player
	if mobID >= firstNPC { // not a buyback, which keeps the item's own track
		defer g.npcWatch(u, mobID, wire.TrackNPCBuy)()
	}
	if u.honorMarket || (mobID >= firstNPC && wire.CString(g.mobs[mobID].info.Player.Name[:]) == honorShopName) {
		g.buyHonorMarket(u, mobID, sellSlot, invSlot)
		return
	}
	if mobID < firstNPC {
		g.buyBack(u, sellSlot)
		return
	}
	if g.mobs[mobID].mode == 0 || isMerchant(&g.mobs[mobID].info.Player) == 0 {
		return // the C++ sold from any mob id here
	}
	item := g.mobs[mobID].info.Player.Inventory[sellSlot]
	if item.Index == 0 || int(item.Index) >= MaxItemList {
		return
	}
	// The slot the client asks for comes from the inventory page the mouse
	// last passed over, not from the 9x7 grid: the item goes to the first
	// cell where it fits, in reading order.
	if invSlot = firstCarrySlot(&pl.Inventory, item.Index); invSlot == -1 {
		g.clientMessage(u, "Não há espaço no inventário para este item.")
		return
	}
	if g.locked(u, lockBuy, "Bloqueio de compra ativo!") {
		return
	}
	if g.shopSpecial(u, mobID, &item) {
		return
	}
	city := g.shopCity(mobID)
	price := int64(g.data.Item(item.Index).Price)
	impost := int64(0)
	if perc := int64(g.data.Cities[city].PercImpost); perc != 0 {
		impost = price * perc / 100
		price += impost
	}
	if price > g.cfg.Limits.MaxGold || pl.Gold < price {
		g.clientMessage(u, "%s", g.lang(nnNotEnoughGoldBuyShop))
		return
	}
	g.data.Cities[city].Impost += impost
	pl.Gold -= price
	g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
	pl.Inventory[invSlot] = item
	g.sendItem(u, wire.SlotInv, invSlot, &item)
}

// buyBack is the repurchase branch of RequestBuyShop (mobID < 1000).
func (g *Game) buyBack(u *User, slot int) {
	if slot == 27 {
		slot = 9
	}
	if slot >= len(u.repurchase) {
		return
	}
	item := &u.repurchase[slot]
	if item.Index == 0 || int(item.Index) >= MaxItemList {
		return
	}
	pl := &g.mobs[u.id].info.Player
	price := int64(g.data.Item(item.Index).Price)
	if soldByAmount(item.Index) {
		price *= int64(itemAmount(item))
	}
	if price > g.cfg.Limits.MaxGold {
		g.clientMessage(u, "Não é possível recomprar este item.")
		return
	}
	if pl.Gold < price {
		g.clientMessage(u, "%s", g.lang(nnNotEnoughGoldBuyShop))
		return
	}
	slotID := firstCarrySlot(&pl.Inventory, item.Index)
	if slotID == -1 {
		g.clientMessage(u, "Não ha espaço no inventario")
		return
	}
	pl.Gold -= price
	g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
	pl.Inventory[slotID] = *item
	g.sendItem(u, wire.SlotInv, slotID, item)
	if r := u.repurchaseTrack[slot]; r != nil {
		g.giveTrack(u, r, wire.SlotInv, slotID)
		g.trackEvent(wire.TrackBuyback, r.id, *item, trackParty{}, g.partyOf(u), price)
	}
	u.repurchaseTrack[slot] = nil
	*item = wire.Item{}
	g.sendRepurchase(u)
}

func (g *Game) requestSellShop(u *User, pkt protocol.Packet) {
	if u.status != userPlay {
		return
	}
	var p wire.SellShop
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	pl := &g.mobs[u.id].info.Player
	goldBefore := pl.Gold
	fairy := pl.Equip[13].Index
	remote := fairy == 3914 || fairy == 3915 // fairies sell from anywhere
	mobID, slot := int(p.NpcID), int(p.SellSlot)
	if mobID < 0 || mobID >= MaxMob || (!isNPC(mobID) && !remote) || slot < 0 || slot >= invSlots {
		return
	}
	if mobID != 0 && !remote {
		nt, mt := g.mobs[mobID].target, g.mobs[u.id].target
		if distance(int(nt.X), int(nt.Y), int(mt.X), int(mt.Y)) > viewGrid/2 {
			return
		}
	}
	item := &pl.Inventory[slot]
	if item.Index == 0 || int(item.Index) >= MaxItemList {
		return
	}
	if g.locked(u, lockSell, "Bloqueio de venda ativo!") {
		return
	}
	sold := *item
	ref := g.takeTrack(u, wire.SlotInv, slot)
	switch g.sellItem(u, item) {
	case sellGoldLimit:
		g.giveTrack(u, ref, wire.SlotInv, slot)
		g.clientMessage(u, "%s", g.lang(nnCantGetMoreThan2G))
		g.sendItem(u, int(p.Type), slot, item)
		return
	case sellBlocked:
		g.giveTrack(u, ref, wire.SlotInv, slot)
		g.clientMessage(u, "Não é possível vender este item")
		g.sendItem(u, int(p.Type), slot, item)
		return
	}
	if ref != nil {
		g.trackEvent(wire.TrackNPCSell, ref.id, sold, g.partyOf(u), trackParty{}, pl.Gold-goldBefore)
	}
	free := -1
	for i, it := range u.repurchase {
		if it.Index == 0 || int(it.Index) >= MaxItemList {
			free = i
			break
		}
	}
	if free == -1 {
		u.repurchaseLast++
		if u.repurchaseLast >= len(u.repurchase) {
			u.repurchaseLast = 0
		}
		free = u.repurchaseLast
	}
	u.repurchase[free] = sold
	u.repurchaseTrack[free] = ref
	g.sendItem(u, wire.SlotInv, slot, item)
}

type sellResult int

const (
	sellOK sellResult = iota
	sellBlocked
	sellGoldLimit
)

// sellItem is SellItem: price from ItemList with the city tax taken out.
func (g *Game) sellItem(u *User, item *wire.Item) sellResult {
	switch item.Index {
	case 509, 3993, 3994, 747:
		return sellBlocked
	}
	if v := g.data.itemAbility(item, EF_VOLATILE); v == 1 || v == 191 {
		return sellBlocked
	}
	city := g.data.village(int(g.mobs[u.id].target.X), int(g.mobs[u.id].target.Y))
	if city == 5 {
		city = 4
	}
	perc := g.data.Cities[city].PercImpost
	price := g.data.Item(item.Index).Price / 4
	switch {
	case price >= 5001 && price <= 10000:
		price = price * 2 / 3
	case price > 10000:
		price /= 2
	}
	amount := soldByAmount(item.Index)
	if amount {
		price *= int32(itemAmount(item))
	}
	impost := int32(0)
	if perc != 0 {
		if amount {
			perc /= 2
		}
		impost = price * perc / 100
		price -= impost
	}
	pl := &g.mobs[u.id].info.Player
	if total := pl.Gold + int64(price); total > g.cfg.Limits.MaxGold || total < 0 {
		return sellGoldLimit
	}
	g.data.Cities[city].Impost += int64(impost)
	pl.Gold += int64(price)
	*item = wire.Item{}
	g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
	return sellOK
}

// sendRepurchase is SendRepurchase (0x3E8): the items sold, at list price.
func (g *Game) sendRepurchase(u *User) {
	if u.status != userPlay {
		return
	}
	p := wire.Repurchase{Header: wire.NewHeader(wire.OpCliRepurchase, 0)}
	for i := range p.Item {
		p.Item[i].Index = int32(i + 1)
		it := &u.repurchase[i]
		if it.Index == 0 || int(it.Index) >= MaxItemList {
			continue
		}
		p.Item[i].Item = *it
		p.Item[i].SellPrice = g.data.Item(it.Index).Price
	}
	g.queue(u, &p)
}

// requestGoldToInv is RequestTransferGoldToInv (0x387): storage -> bag.
func (g *Game) requestGoldToInv(u *User, pkt protocol.Packet) {
	var p wire.SignalValue64
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	pl := &g.mobs[u.id].info.Player
	if pl.Status.CurHP <= 0 {
		return
	}
	g.removeTrade(u.id)
	gold := p.Value
	if gold < 0 || u.account.StorageCoin < gold || gold > g.cfg.Limits.MaxGold {
		g.clientMessage(u, "%s", g.lang(nnCantWithdrawThatMuch))
		return
	}
	total := pl.Gold + gold
	if total > g.cfg.Limits.MaxGold || total < 0 {
		g.clientMessage(u, "%s", g.lang(nnCantGetMoreThan2G))
		return
	}
	pl.Gold = total
	u.account.StorageCoin -= gold
	p.Header.ClientID = wire.ClientIDSystem
	g.queue(u, &p)
	g.sendCargoCoin(u)
	g.saveUser(u, 0)
}

// requestGoldToStorage is RequestTransferGoldToBank (0x388): bag -> storage.
func (g *Game) requestGoldToStorage(u *User, pkt protocol.Packet) {
	var p wire.SignalValue64
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	pl := &g.mobs[u.id].info.Player
	if pl.Status.CurHP <= 0 {
		return
	}
	g.removeTrade(u.id)
	gold := p.Value
	if pl.Gold < gold || gold < 0 || gold > g.cfg.Limits.MaxGold {
		g.clientMessage(u, "%s", g.lang(nnCantDepositThatMuch))
		return
	}
	total := u.account.StorageCoin + gold
	if total > g.cfg.Limits.MaxGold {
		g.clientMessage(u, "%s", g.lang(nnCantGetMoreThan2G))
		return
	}
	pl.Gold -= gold
	u.account.StorageCoin = total
	// The C++ echoed the packet before setting the client id; keep that.
	g.queue(u, &p)
	g.sendCargoCoin(u)
	g.saveUser(u, 0)
}

// sendCargoCoin is SendCargoCoin (0x339).
func (g *Game) sendCargoCoin(u *User) {
	if u.status != userPlay {
		return
	}
	g.queue(u, &wire.CargoCoin{Header: wire.NewHeader(wire.OpSrvCargoCoin, uint16(u.id)), Gold: u.account.StorageCoin})
}
