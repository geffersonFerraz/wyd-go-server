package game

import "dbsrv/internal/wire"

// Mount and equipment-upgrade items of RequestUseItem (volatile 9 and 15).

// mountHP is the mount's HP, a short over Effect[0].Index/Value.
func mountHP(it *wire.Item) int {
	return int(int16(uint16(it.Effect[0].Index) | uint16(it.Effect[0].Value)<<8))
}

func setMountHP(it *wire.Item, hp int) {
	v := uint16(int16(hp))
	it.Effect[0].Index, it.Effect[0].Value = uint8(v), uint8(v>>8)
}

// feedMount is volatile 15 ("rações"): food 2420+n feeds the mount whose
// family is n: +5000 HP (up to the npc_base max) and +2 feed (up to 100).
func (g *Game) feedMount(u *User, p *wire.UseItem, src *wire.Item) {
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	if p.DstType != wire.SlotEquip || p.DstSlot != 14 {
		resend()
		return
	}
	mont := &g.mobs[u.id].info.Player.Equip[14]
	base := 0
	switch {
	case mont.Index >= 2330 && mont.Index < 2360:
		base = 2330
	case mont.Index >= 2360 && mont.Index < 2390:
		base = 2360
	default:
		resend()
		return
	}
	family := int(mont.Index) - base
	switch {
	case (family >= 6 && family <= 15) || family == 27:
		family = 6
	case family == 19:
		family = 7
	case family == 20:
		family = 8
	case family == 21 || family == 22 || family == 23 || family == 28:
		family = 9
	case family == 24 || family == 25 || family == 26:
		family = 10
	case family == 29:
		family = 19
	}
	if family != int(src.Index)-2420 {
		g.clientMessage(u, "%s", g.lang(nnMountNotMatch))
		resend()
		return
	}
	if mountHP(mont) <= 0 { // a dead mount cannot eat
		resend()
		return
	}
	idx := int(mont.Index) - base + 10
	hp := mountHP(mont) + 5000
	if idx < len(g.data.NPCBase) {
		hp = min(hp, int(g.data.NPCBase[idx].BStatus.MaxHP))
	}
	setMountHP(mont, hp)
	mont.Effect[2].Index = uint8(min(int(mont.Effect[2].Index)+2, 100))
	if base == 2330 {
		g.mountProcess(u.id) // the baby walks with the new HP
	} else {
		g.processAdultMount(u.id, 0)
	}
	amountMinus(src)
	g.signalParm(u, wire.ClientIDSystem, 0x3A3, 0x10E)
	g.sendItem(u, wire.SlotEquip, 14, mont)
}

// extremeUnique maps ItemList.Unique to the stone (575 + n) that upgrades it.
func extremeUnique(unique int) int {
	switch unique {
	case 5, 14, 24, 34:
		return 0
	case 6, 15, 25, 35:
		return 1
	case 7, 16, 26, 36:
		return 2
	case 8, 17, 27, 37:
		return 3
	case 10, 20, 30, 40:
		return 4 // the C++ said 3 here, so stone 579 never matched anything
	}
	return -1
}

// useExtremeStone is volatile 9 ("pedras LE", items 575..579): 50% to turn
// a piece of armor up to +9 into its ItemList.Extreme version; the stone is
// used up either way.
func (g *Game) useExtremeStone(u *User, p *wire.UseItem, src *wire.Item) {
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	stone := int(src.Index) - 575
	dst := g.slotItemAny(u, int(p.DstType), int(p.DstSlot))
	if stone < 0 || stone > 4 || dst == nil {
		resend()
		return
	}
	data := g.data.Item(dst.Index)
	if data.Pos < 2 || data.Pos > 32 {
		g.clientMessage(u, "Utilize em equipamentos")
		resend()
		return
	}
	if g.data.itemAbility(dst, EF_UNKNOW1) == 5 {
		g.clientMessage(u, "Não é possível utilizar nesse equipamento.")
		resend()
		return
	}
	if itemSanc(dst) > 9 {
		g.clientMessage(u, "Não é possível utilizar em itens superiores a +9")
		resend()
		return
	}
	if extremeUnique(int(data.Unique)) != stone {
		g.clientMessage(u, "Item inadequado")
		resend()
		return
	}
	if data.Extreme <= 0 || int(data.Extreme) >= MaxItemList {
		resend()
		return
	}
	amountMinus(src)
	if rnd()%100 < 50 {
		g.clientMessage(u, "%s", g.lang(nnFailToRefine))
		return
	}
	g.log.Info("extreme stone", "slot", u.id, "from", dst.Index, "to", data.Extreme)
	dst.Index = uint16(data.Extreme)
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	g.sendEquip(u)
	// The C++ always sent it as an equipment slot.
	g.sendItem(u, int(p.DstType), int(p.DstSlot), dst)
}
