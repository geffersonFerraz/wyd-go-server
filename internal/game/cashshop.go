package game

import (
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The premium (cash) store and the Honor Market: RequestPremiumStore.cpp,
// BuyMarketHonra in RequestBuyShop.cpp, SendMarketHonror in RequestOpenShop.cpp.

const (
	opCashShopInfo = 0xAF1 // to the client
	opCashShopOpen = 0xAF3
	opCashShopBuy  = 0xF03
	opHonorMarket  = 0xAAF1
	opHonorShop    = 0x17CF

	cashShopSlots = 65
	honorShopName = "Honra"
	efGiftID      = 201 // EF_GIFTID: one per account (SingleGift bit)
)

// CashShopItem is one slot of a tab. SlotFree is how many can be bought at
// once; -1 is not for sale.
type CashShopItem struct {
	Item                 wire.Item
	Price, Amount, Promo int
	SlotFree             int
}

// HonorItem is what the Honor Market hands over, and its price in honor.
type HonorItem struct {
	Item  wire.Item
	Price int
}

// CashShopTab is TOD_CashShopInfo.
type CashShopTab struct {
	ID    int
	Name  string
	Slots [cashShopSlots]CashShopItem
}

func (g *Game) cashShopTab(id int) *CashShopTab {
	for i := range g.cfg.CashShop {
		if g.cfg.CashShop[i].ID == id {
			return &g.cfg.CashShop[i]
		}
	}
	return nil
}

// requestCashShop is RequestPremiunStore (0xAF3): shows a tab.
func (g *Game) requestCashShop(u *User, pkt protocol.Packet) {
	if g.locked(u, lockPremium, "Bloqueio de loja donate ativo!") {
		return
	}
	var req wire.CashShopRequest
	if wire.Unmarshal(pkt.Raw, &req) != nil {
		return
	}
	if u.status != userPlay || g.mobs[u.id].info.Player.Status.CurHP == 0 {
		g.sendHpMode(u)
		return
	}
	now := time.Now()
	if now.Sub(u.cashShopOpens) < 200*time.Millisecond {
		return
	}
	u.cashShopOpens = now
	p := wire.CashShopInfo{Header: wire.NewHeader(opCashShopInfo, 0)}
	tab := g.cashShopTab(int(req.Aba))
	if tab == nil {
		g.queue(u, &p)
		return
	}
	wire.PutGameString(p.Name[:], tab.Name)
	for i := range tab.Slots {
		s := &tab.Slots[i]
		p.Item[i], p.Price[i], p.Amount[i], p.Promo[i], p.SlotFree[i] = s.Item, int16(s.Price), int16(s.Amount), int16(s.Promo), int16(s.SlotFree)
	}
	p.Aba = req.Aba
	g.signalParm(u, uint16(u.id), opSrvCash, int32(u.account.Cash))
	g.queue(u, &p)
}

// giftBit returns the SingleGift bit of a one-per-account item.
func giftBit(it *wire.Item) (uint64, bool) {
	for _, e := range it.Effect {
		if e.Index == efGiftID && e.Value < 64 {
			return uint64(1) << e.Value, true
		}
	}
	return 0, false
}

// requestCashShopBuy is RequestBuyDonateShop (0xF03). Checked here and not
// in the C++: the quantity (it could be negative or 0), the empty slots,
// and that a gift is taken once.
func (g *Game) requestCashShopBuy(u *User, pkt protocol.Packet) {
	if g.locked(u, lockPremium, "Bloqueio de loja donate ativo!") {
		return
	}
	var p wire.CashShopBuy
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	m := &g.mobs[u.id]
	if u.status != userPlay || m.info.Player.Status.CurHP == 0 {
		g.sendHpMode(u)
		return
	}
	tab := g.cashShopTab(int(p.Aba))
	if tab == nil || p.Item.Index == 0 {
		return
	}
	var slot *CashShopItem
	for i := range tab.Slots {
		if s := &tab.Slots[i]; s.Item == p.Item && s.Price == int(p.Price) {
			slot = s
			break
		}
	}
	if slot == nil {
		return
	}
	quant := int(p.Quant)
	switch {
	case slot.SlotFree == -1:
		g.clientMessage(u, "Não esta disponível")
		return
	case slot.SlotFree <= 0:
		g.clientMessage(u, "Item esgotado no momento. Aguarde a reposição.")
		return
	case quant < 1:
		return
	case quant > slot.SlotFree:
		g.clientMessage(u, "Limite de compra de %d", slot.SlotFree)
		return
	}
	bit, isGift := giftBit(&slot.Item)
	cost := int64(slot.Price) * int64(quant)
	if isGift {
		if u.account.SingleGift&bit != 0 {
			g.clientMessage(u, "Você já recebeu este presente.")
			return
		}
		if quant != 1 {
			g.clientMessage(u, "Limite de compra de %d", 1)
			return
		}
		cost = 0
	}
	if slot.Price < 0 || int64(u.account.Cash) < cost {
		g.clientMessage(u, "Você não possui Cash suficientes.")
		return
	}
	pl := &m.info.Player
	free := 0
	for i := range invSlots {
		if pl.Inventory[i].Index == 0 {
			free++
		}
	}
	if free < quant {
		g.clientMessage(u, "Não possui espaço suficiente no inventário")
		return
	}
	u.account.Cash -= uint32(cost)
	if isGift {
		u.account.SingleGift |= bit
	}
	for range quant {
		g.putItem(u, &slot.Item)
	}
	g.log.Info("cash shop", "slot", u.id, "account", wire.CString(u.account.Username[:]), "char", g.charName(u.id),
		"item", slot.Item.Index, "effects", slot.Item.Effect, "quant", quant, "cost", cost, "cash", u.account.Cash)
	name := g.data.Item(slot.Item.Index).Name
	g.clientMessage(u, "Você comprou %d %s por um total de %d. Restam %d Cash.", quant, name, cost, u.account.Cash)
	g.signalParm(u, uint16(u.id), opSrvCash, int32(u.account.Cash))
	g.saveUser(u, 0) // a purchase must survive a crash
}

// ---- Honor Market ----

// requestHonorMarketFlag is 0xAAF1: the window closed (or opened).
func (g *Game) requestHonorMarketFlag(u *User, pkt protocol.Packet) {
	var p wire.Parm1
	if wire.Unmarshal(pkt.Raw, &p) == nil {
		u.honorMarket = p.Parm1 != 0
	}
}

// openHonorMarket is the "Honra" branch of RequestOpenShop: the items of
// the NPC called Honra.
func (g *Game) openHonorMarket(u *User) {
	u.honorMarket = true
	g.signalParm(u, uint16(u.id), opHonorMarket, 1)
	p := wire.HonorShopList{Header: wire.NewHeader(opHonorShop, wire.ClientIDSystem)}
	for id := firstNPC; id < MaxMob; id++ {
		npc := &g.mobs[id].info.Player
		if g.mobs[id].mode == 0 || isMerchant(npc) == 0 || wire.CString(npc.Name[:]) != honorShopName {
			continue
		}
		p.NPC = int32(id)
		for i := range p.Item {
			p.Item[i] = npc.Inventory[i%9+(i/9)*27]
		}
		break
	}
	g.queue(u, &p)
}

// buyHonorMarket is BuyMarketHonra: price and item come from honor_market.
func (g *Game) buyHonorMarket(u *User, mobID, sellSlot, invSlot int) {
	pl := &g.mobs[u.id].info.Player
	if mobID < firstNPC || mobID >= MaxMob || g.mobs[mobID].mode == 0 {
		return
	}
	wanted := g.mobs[mobID].info.Player.Inventory[sellSlot].Index
	var offer *HonorItem
	for i := range g.cfg.HonorMarket {
		if g.cfg.HonorMarket[i].Item.Index == wanted && wanted != 0 {
			offer = &g.cfg.HonorMarket[i]
			break
		}
	}
	if offer == nil {
		g.clientMessage(u, "Item de demonstração.")
		return
	}
	if int64(u.account.Honra) < int64(offer.Price) {
		g.clientMessage(u, "Você não possui honras suficientes.")
		return
	}
	if pl.Inventory[invSlot].Index != 0 {
		g.sendItem(u, wire.SlotInv, invSlot, &pl.Inventory[invSlot])
		return
	}
	if !invSlotUsable(pl, invSlot) {
		return
	}
	u.account.Honra -= uint32(offer.Price)
	pl.Inventory[invSlot] = offer.Item
	g.sendItem(u, wire.SlotInv, invSlot, &pl.Inventory[invSlot])
	g.sendClientInfo(u)
	g.log.Info("honor market", "slot", u.id, "char", g.charName(u.id), "item", offer.Item.Index, "price", offer.Price, "honor", u.account.Honra)
}
