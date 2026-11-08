package game

import (
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Using items (RequestUseItem.cpp, 0x373) and potions (ApplyHp/ApplyMp and
// the "pot" loop of ProcessSecTimer).
//
// Potions do not heal at once: they raise the player's pending HP/MP
// (Potion.CountHp/CountMp) and set bQuaff; on the next tick HP/MP move
// toward the pending value by level + 100.

// Language.txt indexes used by items (Basedef.h).
const (
	nnGetSkillPoint   = 1
	nnDoneAlready     = 71
	dnGetExp          = 183
	nnNoReturnPoint   = 78
	nnCantUseThatHere = 96
	nnSetWarp         = 186
)

// setReqHp / setReqMp are SetReqHp / SetReqMp: clamp HP/MP and keep the
// pending potion values at least the current ones.
func (g *Game) setReqHp(u *User) {
	s := &g.mobs[u.id].info.Player.Status
	s.CurHP = min(s.CurHP, s.MaxHP)
	u.potionHP = max(u.potionHP, s.CurHP)
}

func (g *Game) setReqMp(u *User) {
	s := &g.mobs[u.id].info.Player.Status
	s.CurMP = min(s.CurMP, s.MaxMP)
	u.potionMP = max(u.potionMP, s.CurMP)
}

// applyPotion is ApplyHp/ApplyMp for one value: move cur toward pending.
func applyPotion(cur *int32, pending *int32, maxV int32, step int32) bool {
	*pending = min(*pending, maxV)
	if *pending <= *cur {
		return false
	}
	*cur = min(*cur+step, *pending)
	return true
}

// potionTimer is the potion loop of ProcessSecTimer (every tick).
func (g *Game) potionTimer() {
	for id := 1; id < MaxPlayer; id++ {
		u := g.users[id]
		if u.status != userPlay || !u.quaff {
			continue
		}
		pl := &g.mobs[id].info.Player
		if pl.Status.CurHP == 0 {
			continue
		}
		u.quaff = false
		step := int32(pl.Status.Level) + 100
		if evolution(pl) >= evoCelestial {
			step += 400
		}
		hp := applyPotion(&pl.Status.CurHP, &u.potionHP, pl.Status.MaxHP, step)
		mp := applyPotion(&pl.Status.CurMP, &u.potionMP, pl.Status.MaxMP, step)
		if hp {
			g.sendScore(id)
		} else if mp {
			g.sendSetHpMp(u)
		}
	}
}

// amountMinus is AmountMinus: one less in a stack, or the item is gone.
func amountMinus(it *wire.Item) {
	for i := range 3 {
		if it.Effect[i].Index == EF_AMOUNT {
			if it.Effect[i].Value > 1 {
				it.Effect[i].Value--
				return
			}
			break
		}
	}
	*it = wire.Item{}
}

func (g *Game) requestUseItem(u *User, pkt protocol.Packet) {
	var p wire.UseItem
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	if p.SrcSlot > 60 || p.DstSlot > 60 || p.SrcType > 2 || p.DstType > 2 {
		return
	}
	if u.status != userPlay {
		return
	}
	src := g.slotItemAny(u, int(p.SrcType), int(p.SrcSlot))
	if src == nil {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	if pl.Status.CurHP <= 0 {
		resend()
		return
	}
	if p.PosX >= gridSize || p.PosY >= gridSize {
		return
	}
	g.removeTrade(u.id)
	if src.Index == 0 || int(src.Index) >= MaxItemList {
		resend()
		return
	}
	vol := g.data.itemAbility(src, EF_VOLATILE)
	if vol != 1 && vol != 15 && vol != 191 { // only logged, as in the C++
		now := time.Now()
		if now.Sub(u.lastUsedItem) < 169*time.Millisecond {
			g.log.Warn("items used too fast", "slot", u.id, "account", wire.CString(u.account.Username[:]),
				"ms", now.Sub(u.lastUsedItem).Milliseconds(), "item", src.Index)
		}
		u.lastUsedItem = now
	}
	if vol != 2 && vol != 15 && g.locked(u, lockUnequip, "Bloqueio de uso de itens ativo!") {
		resend()
		return
	}

	if src.Index == sealItem {
		g.useSoulSeal(u, &p, src)
		return
	}
	if (vol == 4 || vol == 5) && src.Index != 4141 { // refining dusts
		g.useDust(u, &p, src, vol)
		return
	}

	switch vol {
	case 1: // HP/MP potion
		for _, a := range m.info.Affects {
			if a.Index == 32 {
				resend()
				return
			}
		}
		bonus := m.combat.potionBonus
		if hp := g.data.itemAbility(src, EF_HP); hp != 0 {
			hp += hp * bonus / 100
			u.potionHP = min(u.potionHP+int32(hp), pl.Status.MaxHP)
		}
		if mp := g.data.itemAbility(src, EF_MP); mp != 0 {
			mp += mp * bonus / 100
			u.potionMP = min(u.potionMP+int32(mp), pl.Status.MaxMP)
		}
		u.quaff = true
		g.sendSetHpMp(u)
		amountMinus(src) // the client updates its own stack
		return

	case 243: // healing herb: cleanse + 25 HP
		changed := false
		for i, a := range m.info.Affects {
			switch a.Index {
			case 1, 3, 5, 7, 10, 12, 20, 56:
				m.info.Affects[i] = wire.Affect{}
				changed = true
			}
		}
		u.potionHP = min(u.potionHP+25, pl.Status.MaxHP)
		u.quaff = true
		if changed {
			g.getCurrentScore(u.id)
			g.sendScore(u.id)
		}
		g.sendSetHpMp(u)
		amountMinus(src)
		return

	case 10: // antidote / Kappa potion (affect 4)
		aff := &m.info.Affects
		slot := -1
		for i := range aff {
			if aff[i].Index == 4 && (aff[i].Value == 4 || aff[i].Value == 5) {
				continue
			}
			if aff[i].Index == 4 {
				slot = i
				break
			}
		}
		if slot == -1 {
			slot = g.emptyAffect(u.id, 0)
		}
		if slot == -1 {
			resend()
			return
		}
		value, dur := uint16(6), uint32(15)
		switch src.Index {
		case 1764:
			value = 7
		case 600:
			value, dur = 8, 15*60/8
		case 3321:
			value, dur = 8, 8*1150
		}
		if aff[slot].Index != 0 && aff[slot].Value != value {
			g.clientMessage(u, "Você possui uma poção de outro tipo ativa")
			resend()
			return
		}
		aff[slot].Index, aff[slot].Value = 4, value
		if aff[slot].Time > 0 {
			aff[slot].Time += dur
		} else {
			aff[slot].Time = dur
		}
		g.getCurrentScore(u.id)
		g.sendAffect(u)
		g.sendScore(u.id)
		cm := g.createMobPacket(u.id)
		g.multicast2(int(m.target.X), int(m.target.Y), &cm, 0)
		amountMinus(src)
		return

	case 11: // return scroll: town, or back to where it was used
		x, y := int(m.target.X), int(m.target.Y)
		hallKefra := x >= 3199 && x <= 3324 && y >= 1663 && y <= 1714
		if g.data.attribute(x, y)&attrVillage != 0 && !hallKefra {
			if u.returnX == 0 && u.returnY == 0 {
				g.clientMessage(u, "%s", g.lang(nnNoReturnPoint))
				resend()
				return
			}
			if g.data.attribute(u.returnX, u.returnY)&attrCantSummon != 0 && pl.BStatus.Level < 1000 {
				g.clientMessage(u, "%s", g.lang(nnCantUseThatHere))
				resend()
				return
			}
			g.teleportar(u.id, u.returnX, u.returnY)
		} else {
			u.returnX, u.returnY = x, y
			g.doRecall(u.id)
		}
		amountMinus(src)
		return

	case 12: // star gem: save the warp point
		x, y := int(m.target.X), int(m.target.Y)
		// TODO(phase E): Nightmare areas (g_pPesaArea) and arenas.
		if (g.data.attribute(x, y)&attrCantSummon != 0 && pl.BStatus.Level < 1000) || g.data.village(x, y) < 5 {
			g.clientMessage(u, "%s", g.lang(nnCantUseThatHere))
			resend()
			return
		}
		pl.Last = m.target
		amountMinus(src)
		g.clientMessage(u, "%s", g.lang(nnSetWarp))
		return

	case 13: // teleport scroll: to the star gem point
		g.teleportar(u.id, int(pl.Last.X), int(pl.Last.Y))
		amountMinus(src)
		return

	case 6: // Pílula do Poder Mágico: once per character; used up even if not
		amountMinus(src)
		if m.info.Info&questPilula != 0 {
			g.clientMessage(u, "%s", g.lang(nnDoneAlready))
			return
		}
		m.info.Info |= questPilula
		g.getCurrentScore(u.id)
		g.clientMessage(u, "%s", g.lang(nnGetSkillPoint))
		g.sendEtc(u)
		g.sendScore(u.id)
		return

	case 7: // Poeira da Fada: 300,000,000 exp
		pl.Exp += 300000000
		g.sendEmotion(u.id, 14, 3)
		g.getCurrentScore(u.id)
		amountMinus(src)
		g.checkGetLevel(u)
		g.sendScore(u.id)
		g.sendEtc(u)
		return

	case 8: // Olho Crescente: 3000 exp
		pl.Exp += 3000
		g.clientMessage(u, g.lang(dnGetExp), 3000)
		g.sendEmotion(u.id, 14, 3)
		amountMinus(src)
		g.checkGetLevel(u)
		g.sendEtc(u) // the C++ did not refresh the exp bar
		return

	case 9: // Pedras LE (575..579): turn equipment into its "extreme" version
		g.useExtremeStone(u, &p, src)
		return

	case 15: // mount food
		g.feedMount(u, &p, src)
		return

	case itemPedraIdeal: // 211: Celestial / Sub-Celestial, level 200 unlock
		g.usePedraIdeal(u, &p, src)
		return

	case 14, 17: // empty in the C++ (Neses summon, guild scroll)
		resend()
		return
	}

	if g.useItemByID(u, &p, src, vol) {
		return
	}

	if src.Index == itemRouletteBox && p.SrcType == wire.SlotInv && p.SrcSlot >= 0 && p.SrcSlot < 60 {
		g.openRoulette(u, int(p.SrcSlot))
		return
	}

	// Everything else (quests, boxes, events...) is not ported yet: give
	// the item back so nothing is lost.
	// TODO(phase D): the rest of RequestUseItem.
	g.log.Info("use item not ported", "slot", u.id, "item", src.Index, "volatile", vol)
	g.clientMessage(u, "Este item ainda não pode ser usado neste servidor.")
	resend()
}

// slotItemAny is GetItemPointer (equipment, inventory or storage).
func (g *Game) slotItemAny(u *User, typ, slot int) *wire.Item {
	pl := &g.mobs[u.id].info.Player
	switch typ {
	case wire.SlotEquip:
		if slot >= 0 && slot < len(pl.Equip) {
			return &pl.Equip[slot]
		}
	case wire.SlotInv:
		if slot >= 0 && slot < len(pl.Inventory) {
			return &pl.Inventory[slot]
		}
	case wire.SlotStorage:
		if slot >= 0 && slot < len(u.account.StorageItems) {
			return &u.account.StorageItems[slot]
		}
	}
	return nil
}
