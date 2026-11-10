package game

import (
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Player trade: RequestTrade.cpp (0x383), RemoveTrade, SendCarry.
//
// Flow: A sends 0x383 naming B -> stored as A's offer and relayed to B.
// B answers with its own 0x383 naming A; from then on every update is
// relayed to the other side, and a change to an item already offered
// cancels the trade. Each side confirms (Confirm = 1); when both have, the
// items and gold are swapped atomically and both players are saved.

const (
	nnWrongTradePacket1     = 80
	nnOnlyWithGuildMaster   = 81
	nnAlreadyTrading        = 82
	nnHaventMoneySoMuch     = 83
	nnOpponentHaventMoney   = 84
	nnEachOfYouHaventSpace  = 85
	nnYouHaveNoSpaceToTrade = 86
	nnOpponentHaveNoSpaceTo = 87
	tradeSlots              = 15
)

func emptyTrade() wire.Trade {
	var t wire.Trade
	for i := range t.Slot {
		t.Slot[i] = -1
	}
	return t
}

// removeTrade is RemoveTrade. Unlike the C++ (which only cleared the state
// and ignored the client's 0x384), the client is told the trade is closed.
func (g *Game) removeTrade(id int) {
	u := g.player(id)
	if u == nil || u.status != userPlay || u.trade.ClientID == 0 {
		return
	}
	u.trade = emptyTrade()
	g.signal(u, uint16(u.id), wire.OpCliTradeCancel)
}

// cancelTrade closes the trade of a user and of the partner trading with them.
func (g *Game) cancelTrade(u *User) {
	if other := int(u.trade.ClientID); other > 0 && other < MaxPlayer {
		if o := g.users[other]; o.status == userPlay && int(o.trade.ClientID) == u.id {
			g.removeTrade(other)
		}
	}
	g.removeTrade(u.id)
}

// sendCarry is SendCarry (0x185): the whole inventory and gold.
func (g *Game) sendCarry(u *User) {
	if u.status != userPlay {
		return
	}
	pl := &g.mobs[u.id].info.Player
	g.queue(u, &wire.Carry{Header: wire.NewHeader(wire.OpSrvCarry, uint16(u.id)), Item: pl.Inventory, Gold: pl.Gold})
}

func (g *Game) requestTrade(u *User, pkt protocol.Packet) {
	var msg wire.Trade
	if wire.Unmarshal(pkt.Raw, &msg) != nil {
		return
	}
	me := &g.mobs[u.id].info.Player
	if u.status != userPlay || me.Status.CurHP <= 0 {
		g.sendHpMode(u)
		g.removeTrade(u.id)
		return
	}
	oppID := int(msg.ClientID)
	if oppID <= 0 || oppID >= MaxPlayer || oppID == u.id {
		g.removeTrade(u.id)
		return
	}
	opp := g.users[oppID]
	if opp.status != userPlay {
		g.removeTrade(u.id)
		return
	}
	them := &g.mobs[oppID].info.Player
	if g.locked(u, lockTrade, "Bloqueio de troca ativo!") {
		return
	}
	if msg.Gold < 0 || msg.Gold > me.Gold {
		g.removeTrade(u.id)
		g.removeTrade(oppID)
		return
	}
	for i := range tradeSlots {
		if msg.Item[i].Index != 0 {
			slot := int(msg.Slot[i])
			if slot < 0 || slot >= 60 {
				return
			}
			if me.Inventory[slot] != msg.Item[i] {
				g.removeTrade(u.id)
				g.removeTrade(oppID)
				return
			}
		}
		if opp.trade.Item[i].Index != 0 {
			slot := int(opp.trade.Slot[i])
			if slot < 0 || slot >= 60 {
				return
			}
			if them.Inventory[slot] != opp.trade.Item[i] {
				g.removeTrade(u.id)
				g.removeTrade(oppID)
				return
			}
		}
	}
	if int(u.trade.ClientID) == oppID {
		// An item already offered, or the offered gold, must not change.
		for i := range tradeSlots {
			changed := u.trade.Item[i].Index != 0 && u.trade.Item[i] != msg.Item[i]
			if changed || (u.trade.Gold != 0 && msg.Gold != u.trade.Gold) {
				g.removeTrade(u.id)
				g.removeTrade(oppID)
				return
			}
		}
	} else if u.trade.ClientID != 0 {
		g.clientMessage(u, "%s", g.lang(nnAlreadyTrading))
		g.removeTrade(u.id)
		return
	}
	if opp.pkMode || u.pkMode {
		if opp.pkMode {
			g.clientMessage(u, "O outro jogador esta com o modo PvP ativo")
		} else {
			g.clientMessage(u, "Não é possível trocar com o modo PvP ativo")
		}
		g.removeTrade(oppID)
		g.removeTrade(u.id)
		return
	}
	if opp.whisperOff || u.whisperOff {
		if opp.whisperOff {
			g.clientMessage(u, "O outro jogador esta com o chat desativado")
		} else {
			g.clientMessage(u, "Não é possível trocar com o chat desativado")
		}
		g.removeTrade(oppID)
		g.removeTrade(u.id)
		return
	}
	for i := range tradeSlots {
		it := &msg.Item[i]
		if it.Index == 747 || it.Index == 3993 || it.Index == 3994 {
			g.clientMessage(u, "%s", g.lang(nnOnlyWithGuildMaster))
			g.clientMessage(opp, "%s", g.lang(nnOnlyWithGuildMaster))
			g.removeTrade(u.id)
			if int(opp.trade.ClientID) == u.id {
				g.removeTrade(oppID)
			}
			return
		}
		if it.Index != 0 && g.data.itemAbility(it, EF_NOTRADE) != 0 {
			g.clientMessage(u, "Este item não pode ser trocado.")
			g.clientMessage(opp, "Este item não pode ser trocado.")
			g.removeTrade(u.id)
			if int(opp.trade.ClientID) == u.id {
				g.removeTrade(oppID)
			}
			return
		}
	}

	if opp.trade.ClientID == 0 {
		// Opening a trade: one request per second.
		now := time.Now()
		if now.Sub(u.lastTrade) <= time.Second {
			g.clientMessage(u, "Aguarde para enviar novamente")
			u.lastTrade = now
			return
		}
		u.trade = msg
		u.trade.Confirm, u.trade.Gold = 0, 0
		g.relayTrade(u, opp, msg)
		u.lastTrade = now
		return
	}
	if int(opp.trade.ClientID) != u.id {
		g.clientMessage(u, "%s", g.lang(nnAlreadyTrading))
		g.removeTrade(u.id)
		return
	}

	// The opponent is trading with us: rebuild the items from our inventory.
	for i := range tradeSlots {
		slot := int(msg.Slot[i])
		if slot == -1 {
			msg.Item[i] = wire.Item{}
			continue
		}
		if slot < -1 || slot >= 60 {
			g.clientMessage(u, "%s", g.lang(nnWrongTradePacket1))
			g.removeTrade(u.id)
			return
		}
		for j := range tradeSlots {
			if j != i && msg.Slot[j] == msg.Slot[i] {
				g.clientMessage(u, "%s", g.lang(nnWrongTradePacket1))
				g.removeTrade(u.id)
				return
			}
		}
		msg.Item[i] = me.Inventory[slot]
	}

	if msg.Confirm != 1 {
		u.trade = msg
		u.trade.Confirm = 0
		opp.trade.Confirm = 0
		g.relayTrade(u, opp, msg)
		return
	}

	// Confirming: the offer must be exactly what was last shown.
	if u.trade.Item != msg.Item || u.trade.Slot != msg.Slot || u.trade.Gold != msg.Gold || u.trade.ClientID != msg.ClientID {
		g.removeTrade(u.id)
		g.removeTrade(oppID)
		return
	}
	u.trade.Confirm = 1
	if opp.trade.Confirm != 1 {
		g.signal(u, uint16(u.id), wire.OpSrvTradeConfirm)
		g.relayTrade(u, opp, msg)
		return
	}
	g.completeTrade(u, opp)
}

// relayTrade sends our offer to the other player, addressed from us.
func (g *Game) relayTrade(u, opp *User, msg wire.Trade) {
	msg.Header = wire.NewHeader(wire.OpCliTrade, uint16(opp.id))
	msg.ClientID = uint16(u.id)
	g.queue(opp, &msg)
}

// completeTrade swaps items and gold once both sides confirmed.
func (g *Game) completeTrade(u, opp *User) {
	me, them := &g.mobs[u.id].info.Player, &g.mobs[opp.id].info.Player
	if u.trade.Gold < 0 || u.trade.Gold > g.cfg.Limits.MaxGold || opp.trade.Gold < 0 || opp.trade.Gold > g.cfg.Limits.MaxGold {
		g.removeTrade(u.id)
		g.removeTrade(opp.id)
		return // the C++ cancelled here but carried on with the trade
	}
	myGold := int64(me.Gold) - int64(u.trade.Gold) + int64(opp.trade.Gold)
	theirGold := int64(them.Gold) - int64(opp.trade.Gold) + int64(u.trade.Gold)
	if myGold > g.cfg.Limits.MaxGold || myGold < 0 || theirGold > g.cfg.Limits.MaxGold || theirGold < 0 {
		g.clientMessage(u, "%s", g.lang(nnCantGetMoreThan2G))
		g.removeTrade(u.id)
		g.removeTrade(opp.id)
		return
	}
	if u.trade.Gold > me.Gold {
		g.clientMessage(u, "%s", g.lang(nnHaventMoneySoMuch))
		g.clientMessage(opp, "%s", g.lang(nnOpponentHaventMoney))
		return
	}
	if opp.trade.Gold > them.Gold {
		g.clientMessage(opp, "%s", g.lang(nnHaventMoneySoMuch))
		g.clientMessage(u, "%s", g.lang(nnOpponentHaventMoney))
		return
	}
	myInv, myOK := canTrade(g.data, &me.Inventory, &u.trade.Slot, opp.trade.Item)
	theirInv, theirOK := canTrade(g.data, &them.Inventory, &opp.trade.Slot, u.trade.Item)
	switch {
	case !myOK && !theirOK:
		g.clientMessage(u, "%s", g.lang(nnEachOfYouHaventSpace))
		g.clientMessage(opp, "%s", g.lang(nnEachOfYouHaventSpace))
		return
	case !myOK:
		g.clientMessage(u, "%s", g.lang(nnYouHaveNoSpaceToTrade))
		g.clientMessage(opp, "%s", g.lang(nnOpponentHaveNoSpaceTo))
		return
	case !theirOK:
		g.clientMessage(opp, "%s", g.lang(nnYouHaveNoSpaceToTrade))
		g.clientMessage(u, "%s", g.lang(nnOpponentHaveNoSpaceTo))
		return
	}
	g.log.Info("trade", "a", u.id, "a_account", wire.CString(u.account.Username[:]), "a_gold", u.trade.Gold,
		"b", opp.id, "b_account", wire.CString(opp.account.Username[:]), "b_gold", opp.trade.Gold)
	me.Inventory, them.Inventory = myInv, theirInv
	me.Gold, them.Gold = myGold, theirGold
	g.sendCarry(u)
	g.sendCarry(opp)
	g.removeTrade(u.id)
	g.removeTrade(opp.id)
	g.saveUser(u, 0)
	g.saveUser(opp, 0)
}

// canTrade is CanTrade: the inventory after giving away mySlots and
// receiving incoming (placed largest-grid first into free slots).
func canTrade(d *Data, inv *[64]wire.Item, mySlots *[15]int8, incoming [15]wire.Item) ([64]wire.Item, bool) {
	dest := *inv
	for _, s := range mySlots {
		if s >= 0 && int(s) < len(dest) {
			dest[s] = wire.Item{}
		}
	}
	sortTradeItems(d, &incoming)
	for _, it := range incoming {
		if it.Index == 0 {
			continue
		}
		free := firstCarrySlot(&dest, it.Index)
		if free == -1 {
			return dest, false
		}
		dest[free] = it
	}
	return dest, true
}

// sortTradeItems is SortTradeItem(item, EF_GRID): largest grid first.
func sortTradeItems(d *Data, items *[15]wire.Item) {
	var key [15]int
	for i := range items {
		key[i] = -1
		if items[i].Index != 0 {
			key[i] = d.itemAbility(&items[i], EF_GRID)
		}
	}
	var out [15]wire.Item
	for n := range out {
		best, bestKey := 0, -1
		for i, k := range key {
			if k > bestKey {
				best, bestKey = i, k
			}
		}
		if bestKey == -1 {
			break
		}
		key[best] = -1
		out[n] = items[best]
	}
	*items = out
}
