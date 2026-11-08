package game

import (
	"dbsrv/internal/acctedit"
	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// DBSRV deliveries that change a player in game:
//   - 0xC11 AdminEdit (Go only): a staff edit from the admin panel
//     (package acctedit) applied to the live character and saved at once;
//   - 0xC0E CashDelivery: cash added to the account (PacketControl.cpp);
//   - 0xC10 BanNotify: the account is banned and the player disconnected.

func (g *Game) deliveryFor(u *User, username []byte, what string) bool {
	if wire.CString(username) != wire.CString(u.account.Username[:]) {
		g.log.Error(what+" for another account", "slot", u.id, "packet_user", wire.CString(username))
		return false
	}
	return true
}

// adminEdit is 0xC11.
func (g *Game) adminEdit(u *User, pkt protocol.Packet) {
	var p wire.AdminEdit
	if wire.Unmarshal(pkt.Raw, &p) != nil || !g.deliveryFor(u, p.Username[:], "admin edit") {
		return
	}
	e, err := acctedit.FromPacket(&p)
	if err != nil {
		g.log.Error("admin edit refused", "slot", u.id, "err", err)
		return
	}
	log := g.log.With("slot", u.id, "account", wire.CString(u.account.Username[:]), "op", e.Op)
	if e.Op == acctedit.OpAccess {
		u.accessLevel = int32(e.Value)
		u.account.AccessLevel = uint32(e.Value)
		log.Info("admin edit applied", "access", e.Value)
		return
	}
	if u.status != userPlay {
		if e.Op == acctedit.OpLock {
			return // read again on entering the world
		}
		log.Warn("admin edit lost: player not in game")
		return
	}
	if e.Op == acctedit.OpLock {
		had := u.lock.set()
		u.lock = accountLock{}
		g.loadLock(u)
		g.sendLockStatus(u)
		switch {
		case !u.lock.set() && had:
			g.clientMessage(u, "O bloqueio da conta foi removido pela administração.")
		case u.lock.set():
			g.clientMessage(u, "A senha de bloqueio da conta foi redefinida pela administração.")
		}
		log.Info("account lock reloaded", "locked", u.lock.set())
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if e.CharScoped() && e.Char != u.charSlot {
		log.Warn("admin edit lost: another character in play", "char", e.Char, "playing", u.charSlot)
		return
	}
	switch e.Op {
	case acctedit.OpGold:
		pl.Gold = int32(e.Value)
		g.signalParm(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
	case acctedit.OpCoin:
		u.account.StorageCoin = uint32(e.Value)
		g.sendCargoCoin(u)
	case acctedit.OpLevel:
		level := int16(e.Value - 1)
		pl.BStatus.Level, pl.Status.Level = level, level
		pl.Exp = acctedit.LevelExp(evolution(pl), int(level))
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.sendEtc(u)
		g.refreshMob(u.id)
	case acctedit.OpItem:
		var slot *wire.Item
		switch e.Where {
		case wire.SlotEquip:
			slot = &pl.Equip[e.Slot]
		case wire.SlotInv:
			slot = &pl.Inventory[e.Slot]
		default:
			if u.listedSlot(e.Slot) {
				log.Warn("admin edit refused: storage slot listed in the auction", "slot", e.Slot)
				return
			}
			slot = &u.account.StorageItems[e.Slot]
		}
		if e.Expect != nil && *slot != acctedit.ItemOf(e.Expect) {
			log.Warn("admin edit refused: the slot changed", "where", e.Where, "slot", e.Slot)
			return
		}
		*slot = acctedit.ItemOf(e.Item)
		g.sendItem(u, e.Where, e.Slot, slot)
		if e.Where == wire.SlotEquip {
			g.getCurrentScore(u.id)
			g.sendScore(u.id)
			g.sendEquip(u)
			g.refreshMob(u.id)
		}
	}
	g.saveUser(u, 0)
	log.Info("admin edit applied", "char", e.Char, "where", e.Where, "slot", e.Slot, "value", e.Value, "item", e.Item)
}

// cashDelivery is 0xC0E: cash bought on the site.
func (g *Game) cashDelivery(u *User, pkt protocol.Packet) {
	var p wire.CashDelivery
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status < userSelChar || !g.deliveryFor(u, p.Username[:], "cash delivery") {
		return
	}
	u.account.Cash += uint32(p.Cash)
	if u.status == userSelChar {
		g.saveUser(u, 0)
	}
	g.sendClientInfo(u)
	g.clientMessage(u, "Chegou [ %d ] Cash", p.Cash)
	g.log.Info("cash delivered", "slot", u.id, "cash", p.Cash, "total", u.account.Cash)
}

// banNotify is 0xC10.
func (g *Game) banNotify(u *User, pkt protocol.Packet) {
	var p wire.BanNotify
	if wire.Unmarshal(pkt.Raw, &p) != nil || !g.deliveryFor(u, p.Username[:], "ban") {
		return
	}
	u.account.BanType = uint32(p.BanType)
	u.account.Ban = p.Ban
	if p.BanType == 0 { // ban lifted: nothing to apply in game
		g.log.Info("ban lifted", "slot", u.id)
		return
	}
	g.clientMessage(u, "Conta banida. Contate o suporte para maiores informações.")
	g.flush(u)
	if u.status == userPlay {
		g.saveUser(u, 0)
	}
	g.closeUser(u)
}
