package game

import (
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Soul seals (Selo da Alma, item 3443): RequestUseItem 3443 seals the
// character (PutInSealPacket 3133), RequestPutOutSeal (0x3CC) makes a new
// character from a filled seal (OutSealPacket 3132), RequestSealInfo (0x2CD)
// shows what is inside (SealInfoPacket 3134 -> client 0xDC3). DBSRV does the
// storage work (internal/server/seals.go).

const (
	sealItem          = 3443
	opCliSealInfo     = 0x2CD
	opCliPutOutSeal   = 0x3CC
	opSrvSealInfo     = 0xDC3
	opSrvOutSealSlot  = 0x3B4
	sealCacheLifetime = 5 * time.Minute
)

type sealCacheEntry struct {
	info wire.SealInfo
	at   time.Time
}

// capesByKingdom is g_pCapesID: blue, red and neutral capes, by tier.
var capesByKingdom = [3][10]uint16{
	{736, 1766, 1767, 1768, 543, 545, 734, 3191, 3194, 3197},
	{737, 1769, 1770, 1771, 544, 546, 735, 3192, 3195, 3198},
	{550, 572, 573, 574, 549, 548, 550, 3193, 3196, 3199},
}

// neutralCape turns a blue or red cape into the neutral one of its tier.
func neutralCape(it *wire.Item, kingdom int) {
	for i, id := range capesByKingdom[kingdom] {
		if it.Index == id {
			it.Index = capesByKingdom[2][i]
			return
		}
	}
}

// requestSealInfo is RequestSealInfo (0x2CD): answered from a 5-minute
// cache, otherwise asked to DBSRV.
func (g *Game) requestSealInfo(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	if c, ok := g.sealCache[p.Value]; ok && time.Since(c.at) < sealCacheLifetime {
		g.queue(u, &wire.SealInfoReply{Header: wire.NewHeader(opSrvSealInfo, 0), Info: c.info})
		return
	}
	p.Header = wire.NewHeader(wire.OpSealInfo, uint16(u.id))
	g.sendDB(&p)
}

// sealInfoReply is the SealInfoPacket answer from DBSRV. The C++ closed the
// connection when the seal did not exist; the client just gets no answer.
func (g *Game) sealInfoReply(u *User, pkt protocol.Packet) {
	var p wire.SealInfoReply
	if wire.Unmarshal(pkt.Raw, &p) != nil || p.Info.Status == -1 {
		return
	}
	g.sealCache[p.Info.Status] = sealCacheEntry{info: p.Info, at: time.Now()}
	p.Header = wire.NewHeader(opSrvSealInfo, wire.ClientIDSystem)
	g.queue(u, &p)
}

// useSoulSeal is RequestUseItem for an empty Selo da Alma: the character
// must be an Arch or above, with nothing equipped (but face and cape),
// an empty inventory, no gold, a free storage slot and no guild leadership.
func (g *Game) useSoulSeal(u *User, p *wire.UseItem, src *wire.Item) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	refuse := func(msg string) {
		g.clientMessage(u, "%s", msg)
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
	}
	if src.Effect[0].Index != 0 {
		// A filled seal is opened with 0x3CC; the C++ ignored this silently.
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
		return
	}
	if p.SrcType != wire.SlotInv {
		refuse("Use o selo pelo inventário")
		return
	}
	for i := 1; i < 15; i++ {
		if pl.Equip[i].Index > 0 {
			refuse("Desequipe todos os itens do seu personagem para selá-lo")
			return
		}
	}
	for i := range 60 {
		if i != int(p.SrcSlot) && pl.Inventory[i].Index > 0 {
			refuse("Deixe o inventário vazio para selar seu personagem")
			return
		}
	}
	free := false
	for _, it := range u.account.StorageItems {
		free = free || it.Index == 0
	}
	if !free {
		refuse("Necessário ter um slot vago no seu Guarda Carga")
		return
	}
	if pl.GuildMemberType != 0 && pl.GuildMemberType != 1 {
		refuse("Não é possível selar personagens líderes/sublíderes de guild")
		return
	}
	if pl.Gold != 0 {
		refuse("Não é possível selar personagem com gold no inventário")
		return
	}
	if evolution(pl) == evoMortal {
		refuse("Não é possível selar personagem mortal")
		return
	}
	if k := int(pl.CapeInfo) - 7; k == 0 || k == 1 {
		neutralCape(&pl.Equip[15], k)
		ev := evolution(pl)
		if (ev == evoCelestial && m.info.Sub.Status == 1) || ev == evoSubCele {
			neutralCape(&m.info.Sub.Equip[1], k)
			setItemSanc(&m.info.Sub.Equip[1], 0, 0)
		}
	}
	setItemSanc(&pl.Equip[15], 0, 0)
	amountMinus(src)
	slot := u.charSlot
	g.log.Info("character sealed", "slot", u.id, "charSlot", slot, "name", g.charName(u.id))
	g.charLogout(u) // saves the character as it is now
	g.sendDB(&wire.SignalValue{Header: wire.NewHeader(wire.OpPutInSeal, uint16(u.id)), Value: int32(slot)})
}

// requestPutOutSeal is RequestPutOutSeal (0x3CC): from level 280, a filled
// seal in the inventory becomes a new character in a free list slot.
func (g *Game) requestPutOutSeal(u *User, pkt protocol.Packet) {
	var p wire.PutOutSeal
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	pl := &g.mobs[u.id].info.Player
	if u.status != userPlay || pl.Status.CurHP == 0 {
		g.sendHpMode(u)
		return
	}
	if u.trade.ClientID != 0 {
		g.removeTrade(u.id)
		return
	}
	if p.SrcType != wire.SlotInv || p.SrcSlot < 0 || p.SrcSlot >= 60 {
		return
	}
	src := &pl.Inventory[p.SrcSlot]
	resend := func() { g.sendItem(u, wire.SlotInv, int(p.SrcSlot), src) }
	if src.Index != sealItem || src.Effect[0].Index == 0 {
		resend()
		return
	}
	if pl.BStatus.Level < 279 {
		g.clientMessage(u, "Level mínimo para tirar o selo do personagem é 280")
		resend()
		return
	}
	newSlot := -1
	for i := range 4 {
		if u.charList.Name[i][0] == 0 {
			newSlot = i
			break
		}
	}
	if newSlot == -1 {
		g.clientMessage(u, "Não possui espaço na seleção de personagem")
		resend()
		return
	}
	g.charLogout(u) // DBSRV takes the seal from the saved inventory
	p.MobName[15] = 0
	p.Header = wire.NewHeader(wire.OpOutSeal, uint16(u.id))
	g.sendDB(&p)
	g.signalParm(u, uint16(u.id), opSrvOutSealSlot, int32(newSlot))
	g.log.Info("seal opened", "slot", u.id, "item slot", p.SrcSlot)
}

// putInSealDone is PutInSealSuccess: the sealed character is gone, so the
// in-memory copy must not be saved again.
func (g *Game) putInSealDone(u *User) {
	g.mobs[u.id].info = wire.CharInfo{}
}

// itemDelivery is 0xC0F (ImportItem / the new seal): into the first free
// storage slot, else the inventory.
//
// Fixed: with a full storage (or the player already gone) the item was
// only logged and lost, while DBSRV had already marked the delivery done.
// Now it goes back to DBSRV (0xC12), which queues it again for the storage
// once the player is offline.
func (g *Game) itemDelivery(u *User, pkt protocol.Packet) {
	var p wire.ItemDelivery
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	if u == nil || u.status < userSelChar || wire.CString(p.Username[:]) != wire.CString(u.account.Username[:]) {
		g.log.Warn("item delivery: player not here, returned to DBSRV", "account", wire.CString(p.Username[:]), "item", p.Item.Index)
		g.returnDelivery(&p)
		return
	}
	for i := range u.account.StorageItems {
		if u.account.StorageItems[i].Index != 0 {
			continue
		}
		u.account.StorageItems[i] = p.Item
		g.sendItem(u, wire.SlotStorage, i, &p.Item)
		g.clientMessage(u, "Chegou um item [ %s ]", g.itemName(p.Item.Index))
		g.log.Info("item delivered", "slot", u.id, "item", p.Item.Index, "storage", i)
		if u.status == userSelChar {
			g.saveUser(u, 0)
		}
		return
	}
	if u.status == userPlay && g.putItem(u, &p.Item) {
		g.clientMessage(u, "Chegou um item [ %s ] (baú cheio: foi para o inventário)", g.itemName(p.Item.Index))
		g.log.Info("item delivered to the inventory (storage full)", "slot", u.id, "item", p.Item.Index)
		g.saveUser(u, 0)
		return
	}
	g.clientMessage(u, "Seu baú e inventário estão cheios: o item [ %s ] será colocado no baú quando você sair do jogo.", g.itemName(p.Item.Index))
	g.log.Warn("item delivery: storage and inventory full, returned to DBSRV", "slot", u.id, "item", p.Item.Index)
	g.returnDelivery(&p)
}

// returnDelivery sends an undelivered item back to DBSRV (Go only, 0xC12).
func (g *Game) returnDelivery(p *wire.ItemDelivery) {
	out := *p
	out.Header = wire.NewHeader(wire.OpItemDeliveryReturn, 0)
	g.sendDB(&out)
}
