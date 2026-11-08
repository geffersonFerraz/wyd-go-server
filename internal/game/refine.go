package game

import (
	"strconv"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Refining with dusts (RequestUseItem "POEIRAS": EF_VOLATILE 4 = Poeira de
// Oriharucon, 5 = Poeira de Lactolerium) and the emote packet 0x36A
// (RequestMotion / SendEmotion).

// Language.txt indexes (Basedef.h).
const (
	nnCantRefineMore = 75
	nnFailToRefine   = 76
	nnRefineSuccess  = 176
	nnIncubated      = 253
	nnMountNotMatch  = 256
	nnIncuWaitMore   = 261
	nnCantRefine     = 303
	nnCantRefineWar  = 304
)

const nucleoLac = 4521 // NUCLEO_LAC_ITEMID

// refineRate is Rate[dust][item type][sanc]: chance (0..100) to go up one
// level. Rows A..F come from the item's EF_UNKNOW1, the last row is for
// Arch items (EF_MOBTYPE 1).
var refineRate = [2][7][9]int{
	{ // Oriharucon (+0..+5)
		{100, 100, 100, 100, 90, 60, 0, 0, 0},
		{100, 100, 100, 100, 90, 50, 0, 0, 0},
		{100, 100, 100, 80, 75, 50, 0, 0, 0},
		{100, 100, 95, 85, 80, 80, 0, 0, 0},
		{100, 95, 95, 85, 80, 80, 0, 0, 0},
		{100, 90, 80, 80, 80, 80, 0, 0, 0},
		{100, 90, 80, 80, 80, 80, 0, 0, 0},
	},
	{ // Lactolerium (+0..+8)
		{100, 100, 100, 100, 100, 100, 75, 55, 40},
		{100, 100, 100, 100, 100, 100, 75, 45, 35},
		{100, 100, 100, 100, 100, 100, 75, 40, 30},
		{100, 100, 100, 100, 100, 100, 75, 50, 45},
		{100, 100, 100, 100, 100, 100, 65, 50, 45},
		{100, 100, 100, 100, 100, 100, 55, 45, 45},
		{100, 100, 100, 100, 100, 100, 55, 45, 45},
	},
}

// refineFailBonus is Fail[sanc]: extra chance per recorded failure.
var refineFailBonus = [9]int{0, 5, 5, 4, 4, 3, 3, 2, 1}

// refineRateCelestial is RateCele[sanc] for Celestial items (EF_MOBTYPE 3).
var refineRateCelestial = [15]int{90, 85, 85, 80, 70, 60, 50, 45, 40, 35, 25, 15, 10, 7, 5}

// cytheraRate is g_pCytheraRate for the "Cythera Desencantada" (3508).
var cytheraRate = [10]int{80, 70, 60, 50, 50, 50, 50, 40, 30, 30}

// setItemSanc is SetItemSanc: write the refine level (and the failure count
// in the tens) into the item's sanc effect, adding EF_SANC if needed.
func setItemSanc(it *wire.Item, sanc, fails int) bool {
	if sanc > 15 || sanc < 0 {
		return false
	}
	fails = min(max(fails, 0), 20)
	slot, actual := -1, 0
	for i := range 3 {
		if e := it.Effect[i].Index; e == EF_SANC || (e >= 116 && e <= 125) {
			slot, actual = i, int(it.Effect[i].Value)
			break
		}
	}
	if sanc > 9 {
		// Keep the gem bonus a +11 or higher item already has.
		extra := 0
		if actual > 230 {
			extra = (actual - 230) % 4
		}
		sanc = min(230+(sanc-10)*4+extra, 250)
	}
	sc := uint8(sanc + 10*fails)
	if slot >= 0 {
		it.Effect[slot].Value = sc
		return true
	}
	for i := range 3 {
		if it.Effect[i].Index == 0 {
			it.Effect[i] = wire.ItemEffect{Index: EF_SANC, Value: sc}
			return true
		}
	}
	return false
}

// sendEmotion is SendEmotion: an emote by the player, to its viewers.
func (g *Game) sendEmotion(id, motion, parm int) {
	m := &g.mobs[id]
	g.multicast2(int(m.target.X), int(m.target.Y),
		&wire.Motion{Header: wire.NewHeader(wire.OpCliMotion, uint16(id)), Motion: int16(motion), Parm: int16(parm)}, 0)
}

// requestMotion is CUser::RequestMotion: relay an emote (15 sits, 13 ...).
func (g *Game) requestMotion(u *User, pkt protocol.Packet) {
	var p wire.Motion
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	m := &g.mobs[u.id]
	if u.status != userPlay || m.info.Player.Status.CurHP == 0 {
		g.sendHpMode(u)
		return
	}
	switch p.Motion {
	case 15:
		m.motion = 1
	case 13:
		m.motion = 2
	default:
		m.motion = 0
	}
	p.Header.ClientID = uint16(u.id)
	g.multicast2(int(m.target.X), int(m.target.Y), &p, 0)
}

// useDust is the "POEIRAS" branch of RequestUseItem (volatile 4 or 5 on
// anything but item 4141). src is consumed except when refused.
func (g *Game) useDust(u *User, p *wire.UseItem, src *wire.Item, vol int) {
	pl := &g.mobs[u.id].info.Player
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	refuse := func(msg int) {
		if msg != 0 {
			g.clientMessage(u, "%s", g.lang(msg))
		}
		resend()
	}
	dstType, dstSlot := int(p.DstType), int(p.DstSlot)
	dst := g.slotItemAny(u, dstType, dstSlot)
	if dst == nil {
		resend()
		return
	}
	sendDst := func() { g.sendItem(u, dstType, dstSlot, dst) }
	equip := dstType == wire.SlotEquip

	if equip && dstSlot == 15 && evolution(pl) >= evoCelestial {
		refuse(nnCantRefine)
		return
	}
	// The face cannot be refined (the C++ returned without giving the dust
	// back to the client's view).
	if equip && dstSlot == 0 {
		resend()
		return
	}
	if dst.Index == 769 && g.data.warArea(int(g.mobs[u.id].target.X), int(g.mobs[u.id].target.Y)) != -1 {
		refuse(nnCantRefineWar)
		return
	}
	if equip && dstSlot == 12 {
		resend()
		return
	}
	// The C++ only printed the message and then refined these anyway.
	if dst.Index == 3993 || dst.Index == 3994 {
		refuse(nnCantRefine)
		return
	}

	// Lactolerium core: each Lac dust charges it by 2, up to 100.
	if dst.Index == nucleoLac {
		if vol == 4 {
			g.clientMessage(u, "Somente com poeiras de lac.")
			resend()
			sendDst()
			return
		}
		if dst.Effect[0].Index != EF_AMOUNT {
			dst.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT}
		}
		if dst.Effect[0].Value >= 100 {
			g.clientMessage(u, "O núcleo está totalmente carregado!")
			sendDst()
			resend()
			return
		}
		dst.Effect[0].Value += 2
		g.clientMessage(u, "Núcleo refinado com sucesso!")
		sendDst()
		g.sendEmotion(u.id, 14, 3)
		amountMinus(src)
		return
	}

	if dstSlot == 13 && dst.Index >= 3397 && dst.Index <= 3406 {
		dst.Index += 10
		sendDst()
		amountMinus(src)
		return
	}

	// Mount egg in the mount slot: hatch it, or bring the hatching closer.
	if equip && dstSlot == 14 {
		if dst.Index < 2300 || dst.Index >= 2330 {
			refuse(nnMountNotMatch)
			return
		}
		if dst.Effect[2].Value != 0 {
			refuse(nnIncuWaitMore)
			return
		}
		if dst.Effect[0].Value <= 1 {
			dst.Index += 30
			dst.Effect[0] = wire.ItemEffect{Index: 100, Value: 100}
			dst.Effect[1] = wire.ItemEffect{Index: 1, Value: uint8(rnd()%20 + 10)}
			dst.Effect[2] = wire.ItemEffect{Index: 30, Value: 1}
			g.mountProcess(u.id)
			g.clientMessage(u, "%s", g.lang(nnIncubated))
		} else {
			dst.Effect[0].Value--
			dst.Effect[2] = wire.ItemEffect{Index: 84, Value: uint8(rnd()%8 + 4)}
		}
		sendDst()
		g.sendEmotion(u.id, 14, 3)
		amountMinus(src)
		return
	}

	// Sealed stones in the stone slot: 80% to open.
	if equip && dstSlot == 11 && dst.Index >= 1752 && dst.Index < 1760 {
		if rnd()%101 <= 80 {
			dst.Index -= 8
			g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
		} else {
			g.clientMessage(u, "%s", g.lang(nnFailToRefine))
		}
		sendDst()
		g.sendEmotion(u.id, 14, 3)
		amountMinus(src)
		return
	}

	sanc := itemSanc(dst)

	// Earrings 591..595 from +9: Lac only, 10% (5% from +11); the item is
	// destroyed on failure unless an Emblema da Proteção (4611) is carried,
	// which is used up either way.
	if equip && dstSlot == 8 && vol == 5 && dst.Index >= 591 && dst.Index <= 595 && sanc >= 9 && sanc <= 14 {
		roll := rnd() % 101
		g.sendEmotion(u.id, 14, 3)
		protect := firstSlotOf(pl, 4611)
		rate := 90
		if sanc >= 11 {
			rate = 95
		}
		if roll > rate {
			g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
			setItemSanc(dst, sanc+1, 0)
			g.log.Info("refine", "slot", u.id, "item", dst.Index, "to", sanc+1, "ok", true)
		} else {
			g.clientMessage(u, "%s", g.lang(nnFailToRefine))
			if protect == -1 {
				g.log.Info("refine destroyed", "slot", u.id, "item", dst.Index, "to", sanc+1)
				*dst = wire.Item{}
			}
		}
		if protect != -1 {
			amountMinus(&pl.Inventory[protect])
			g.sendItem(u, wire.SlotInv, protect, &pl.Inventory[protect])
		}
		sendDst()
		amountMinus(src)
		return
	}

	// Cythera Desencantada: Lac only, up to +9; failing resets it to +0,
	// or destroys it when going for +7.
	if dst.Index == 3508 {
		if vol != 5 {
			resend()
			return
		}
		if sanc >= 9 {
			refuse(nnCantRefine)
			return
		}
		if rnd()%100 <= cytheraRate[sanc] {
			setItemSanc(dst, sanc+1, 0)
			sendDst()
			g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
			if sanc == 8 {
				g.notice(wire.DecodeGame(wire.CString(pl.Name[:])) + " refinou Cythera Desencantada para +9")
			}
		} else if sanc != 6 {
			g.clientMessage(u, "%s", g.lang(nnFailToRefine))
			setItemSanc(dst, 0, 0)
			sendDst()
		} else {
			g.log.Info("refine destroyed", "slot", u.id, "item", dst.Index, "to", sanc+1)
			*dst = wire.Item{}
			sendDst()
			g.clientMessage(u, "Item destruído")
		}
		amountMinus(src)
		return
	}

	// Items 762..768 / 788..791 from +9: Lac, 30%/15% (15%/6% for
	// 788..791); failing resets them to +0. Announced to everyone.
	if ((dst.Index >= 762 && dst.Index <= 768) || (dst.Index >= 788 && dst.Index <= 791)) && (sanc == 9 || sanc == 10) && vol == 5 {
		rate := 30
		if sanc == 10 {
			rate = 15
		}
		if dst.Index >= 788 {
			rate = 15
			if sanc == 10 {
				rate = 6
			}
		}
		name := wire.DecodeGame(wire.CString(pl.Name[:]))
		item := wire.DecodeGame(g.data.Item(dst.Index).Name)
		if rnd()%100 < rate {
			setItemSanc(dst, sanc+1, 0)
			g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
			g.notice(name + " refinou " + item + " para +" + strconv.Itoa(sanc+1))
		} else {
			setItemSanc(dst, 0, 0)
			g.clientMessage(u, "%s", g.lang(nnFailToRefine))
			g.notice(name + " falhou " + item + " para +" + strconv.Itoa(sanc+1))
		}
		sendDst()
		amountMinus(src)
		return
	}

	// Celestial items: Lac only, up to +15; a failure takes one level off.
	// The C++ read the type of Equip[DstSlot] even for inventory items; the
	// refined item's own type is used here.
	mobType := g.data.effectValueByIndex(dst.Index, EF_MOBTYPE)
	if vol == 5 && mobType == 3 && dst.Index != 0 {
		if sanc >= 15 {
			refuse(nnCantRefine)
			return
		}
		g.sendEmotion(u.id, 14, 3)
		if rnd()%101 <= refineRateCelestial[sanc] {
			setItemSanc(dst, sanc+1, 0)
			g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
		} else {
			if sanc != 0 {
				setItemSanc(dst, sanc-1, 0)
			}
			g.clientMessage(u, "%s", g.lang(nnFailToRefine))
		}
		amountMinus(src)
		sendDst()
		return
	}

	// Equipped +10 gear: Lac gives a 4% chance at +11 (nothing is lost).
	if equip && dstSlot <= 7 && vol == 5 && sanc == 10 {
		amountMinus(src)
		if rnd()%101 > 96 {
			for i := range 3 {
				if e := dst.Effect[i].Index; e == EF_SANC || (e >= 116 && e <= 125) {
					dst.Effect[i].Value += 4
					g.sendEmotion(u.id, 14, 3)
					g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
					break
				}
			}
		} else {
			g.clientMessage(u, "%s", g.lang(nnFailToRefine))
		}
		sendDst()
		return
	}

	if (vol == 4 && sanc >= 6) || (vol == 5 && sanc >= 9) {
		refuse(nnCantRefineMore)
		return
	}
	free := false
	for i := range 3 {
		if e := dst.Effect[i].Index; e == 0 || e == EF_SANC || (e >= 116 && e <= 125) {
			free = true
			break
		}
	}
	if !free || g.data.effectValueByIndex(dst.Index, 125) == 1 || g.data.Item(dst.Index).Pos == 0 {
		refuse(nnCantRefine)
		return
	}

	fails := 0
	for i := range 3 {
		if e := dst.Effect[i].Index; (e == EF_SANC || (e >= 116 && e <= 125)) && dst.Effect[i].Value != 0 {
			fails = int(dst.Effect[i].Value) / 10
			break
		}
	}
	typ := g.data.effectValueByIndex(dst.Index, EF_UNKNOW1)
	if typ < 0 || typ >= 6 {
		typ = 5
	}
	// As in the C++, the failure bonus is also what gets stored as the new
	// failure count, so an item that never failed keeps a count of 0.
	bonus := refineFailBonus[sanc] * fails
	rate := refineRate[vol-4][typ][sanc] + bonus
	if mobType == 1 {
		rate = refineRate[vol-4][6][sanc]
	}
	ok := rnd()%101 <= rate
	if ok {
		g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
		setItemSanc(dst, sanc+1, bonus)
	} else {
		g.clientMessage(u, "%s", g.lang(nnFailToRefine))
		setItemSanc(dst, sanc, bonus)
	}
	g.log.Info("refine", "slot", u.id, "item", dst.Index, "to", sanc+1, "ok", ok, "rate", rate)
	sendDst()
	if ok {
		g.sendEmotion(u.id, 14, 3)
	} else {
		g.sendEmotion(u.id, 15, 1)
	}
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	amountMinus(src)
	g.sendEquip(u)
}
