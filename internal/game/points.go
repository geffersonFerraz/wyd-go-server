package game

import (
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Character upkeep from the client's windows: status/mastery points and
// skill learning (RequestAddPoint.cpp), the skill bar
// (RequestChangeSkillBar.cpp), deleting an item in the bin
// (RequestDeleteItem.cpp), splitting a stack (RequestUngroupItem.cpp) and
// the home city (RequestChangeCity.cpp).

const (
	opCliAddPoint   = 0x277
	opCliSkillBar   = 0x378
	opCliDeleteItem = 0x2E4
	opCliUngroup    = 0x2E5
	opCliChangeCity = 0x291

	nnMaximumPointNow     = 105
	nnMaximumPoint200Now  = 106
	nnCantLearnOtherClass = 107
	nnNotEnoughSkillPoint = 108
	nnAlreadyLearnedIt    = 109
	nnNeedMoreLevel       = 110
	nnNeedMoreMastery     = 111
	nnOnlyA8thPerChar     = 387
	nnNeedAllSkillLineage = 388
	nnNeedXGoldToBuySkill = 389
	nnCantDeleteThisItem  = 393
)

// AddPoint mirrors p277 (20 bytes).
type AddPoint struct {
	Header wire.Header
	Mode   int16 // 0 status, 1 mastery, 2 learn a skill
	Info   int16 // stat 0..3, mastery 0..3, or the skill book item 5000..5095
	Unk    int32 // mode 2: the NPC (only range-checked)
}

// DeleteItem mirrors p2E4 (20 bytes).
type DeleteItem struct {
	Header wire.Header
	SlotID int16
	Unk    int16
	ItemID int16
	Unk2   int16
}

// UngroupItem mirrors p2E5 (24 bytes).
type UngroupItem struct {
	Header wire.Header
	SlotID int32
	ItemID int32
	Amount int32
}

// eighthSkillGold is the price of a class's 8th skill by evolution
// (Mortal, Arch, Celestial, Sub-Celestial).
var eighthSkillGold = [4]int32{5_000_000, 10_000_000, 50_000_000, 50_000_000}

// x86shl is a 32-bit `1 << n` as the C++ compiled it: x86 masks the count
// to 5 bits, so negative or >= 32 counts wrap instead of giving 0.
func x86shl(n int) uint32 { return 1 << (uint(n) & 31) }

// requestAddPoint is CUser::RequestAddPoint (0x277).
func (g *Game) requestAddPoint(u *User, pkt protocol.Packet) {
	var p AddPoint
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if u.status != userPlay || pl.Status.CurHP <= 0 {
		g.sendHpMode(u)
		return
	}
	info := int(p.Info)
	switch p.Mode {
	case 0: // status points: STR, INT, DEX, CON (100 at a time above 200)
		if pl.StatusPoint == 0 {
			g.sendEtc(u)
			return
		}
		if info < 0 || info > 3 {
			g.log.Warn("add point: bad stat", "slot", u.id, "info", info)
			return
		}
		add := uint16(1)
		if pl.StatusPoint > 200 {
			add = 100
		}
		stat := [4]*uint16{&pl.BStatus.STR, &pl.BStatus.INT, &pl.BStatus.DEX, &pl.BStatus.CON}[info]
		// The C++ fields are shorts: stop before they would wrap negative.
		if int(*stat)+int(add) > 32767 {
			g.sendEtc(u)
			return
		}
		pl.StatusPoint -= add
		*stat += add

	case 1: // mastery points
		if pl.MasterPoint == 0 {
			g.sendEtc(u)
			return
		}
		if info < 0 || info > 3 {
			g.log.Warn("add point: bad mastery", "slot", u.id, "info", info)
			return
		}
		cur := int(pl.BStatus.Mastery[info])
		if cur >= (int(pl.BStatus.Level)+1)*3>>1 && evolution(pl) < evoCelestial {
			g.clientMessage(u, "%s", g.lang(nnMaximumPointNow))
			return
		}
		// 255 with the class's 8th skill of that tree, else 200. Info 0
		// tests bit 31 (the C++ shifted by -1).
		limit := 200
		if pl.Learn[0]&x86shl(8*info-1) != 0 {
			limit = 255
		}
		if cur >= limit {
			g.clientMessage(u, "%s", g.lang(nnMaximumPoint200Now))
			return
		}
		pl.MasterPoint--
		pl.BStatus.Mastery[info]++

	case 2: // learn a skill from its book
		if !g.learnSkill(u, info, int(p.Unk)) {
			return
		}

	default:
		g.log.Warn("add point: bad mode", "slot", u.id, "mode", p.Mode)
		return
	}
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	g.sendEtc(u)
}

// learnSkill is RequestAddPoint mode 2. book is the skill's ItemList id.
func (g *Game) learnSkill(u *User, book, npc int) bool {
	pl := &g.mobs[u.id].info.Player
	if book < 5000 || book > 5095 || npc < 0 || npc >= 30000 {
		g.log.Warn("learn skill out of range", "slot", u.id, "book", book, "npc", npc)
		return false
	}
	class, pos := (book-5000)/24, (book-5000)%24
	if int(pl.ClassInfo) != class {
		g.clientMessage(u, "%s", g.lang(nnCantLearnOtherClass))
		return false
	}
	points := 0
	if i := class*24 + pos; i < len(g.data.Skills) {
		points = g.data.Skills[i].Points
	}
	if points > int(pl.SkillPoint) {
		g.clientMessage(u, "%s", g.lang(nnNotEnoughSkillPoint))
		return false
	}
	bit := uint32(1) << pos
	if pl.Learn[0]&bit != 0 {
		g.clientMessage(u, "%s", g.lang(nnAlreadyLearnedIt))
		return false
	}
	it := g.data.Item(uint16(book))
	evo := evolution(pl)
	if int(it.Level) > int(pl.Status.Level) && evo < evoCelestial {
		g.clientMessage(u, "%s", g.lang(nnNeedMoreLevel))
		return false
	}
	if int(pl.Status.Mastery[1]) < int(it.Int) || int(pl.Status.Mastery[2]) < int(it.Dex) || int(pl.Status.Mastery[3]) < int(it.Con) {
		g.clientMessage(u, "%s", g.lang(nnNeedMoreMastery))
		return false
	}
	var gold int32
	eighth := pos%8 == 7
	if eighth {
		for i := 1; i < 8; i++ {
			if pl.Learn[0]&(1<<(pos-i)) == 0 {
				g.clientMessage(u, "%s", g.lang(nnNeedAllSkillLineage))
				return false
			}
		}
		tree := pos/8 + 1
		for i := 1; i < 4; i++ {
			if i != tree && pl.Learn[0]&(1<<(i*8-1)) != 0 {
				g.clientMessage(u, "%s", g.lang(nnOnlyA8thPerChar))
				return false
			}
		}
		// The C++ read gold[EFV2 - 1] unchecked; an unset evolution
		// counts as Mortal here.
		gold = eighthSkillGold[min(max(evo, evoMortal), evoSubCele)-1]
		if pl.Gold < gold {
			g.clientMessage(u, "%s", g.lang(nnNeedXGoldToBuySkill))
			return false
		}
	}
	g.log.Info("learned skill", "slot", u.id, "book", book, "learn_before", pl.Learn[0], "gold", gold)
	pl.Learn[0] |= bit
	pl.SkillPoint -= uint16(points)
	pl.Gold -= gold
	return true
}

// skillBarEmpty is what the C++ wrote over a slot it refused.
const skillBarEmpty = 248

// requestChangeSkillBar is CUser::RequestChangeSkillbar (0x378): keep only
// learned skills, store the bar and echo it.
func (g *Game) requestChangeSkillBar(u *User, pkt protocol.Packet) {
	var p wire.SkillBar
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	m := &g.mobs[u.id]
	learn := m.info.Player.Learn[0]
	check := func(s *uint8) {
		id := int(*s)
		switch {
		case id > skillBarEmpty:
			*s = skillBarEmpty
		case id < 96:
			if learn&(1<<(id%24)) == 0 {
				*s = skillBarEmpty
			}
		case id < skillBarEmpty:
			// 96.. are the special skills at bit 24 + n; past bit 31 the
			// C++ shift wrapped, so keep that.
			if learn&x86shl(24+id-96) == 0 {
				*s = skillBarEmpty
			}
		}
	}
	for i := range p.SkillBar1 {
		check(&p.SkillBar1[i])
	}
	for i := range p.SkillBar2 {
		check(&p.SkillBar2[i])
	}
	m.info.Player.SkillBar1 = p.SkillBar1
	m.info.SkillBar = p.SkillBar2
	g.queue(u, &p)
}

// requestDeleteItem is CUser::RequestDeleteItem (0x2E4): the inventory bin.
func (g *Game) requestDeleteItem(u *User, pkt protocol.Packet) {
	var p DeleteItem
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	slot, id := int(p.SlotID), int(p.ItemID)
	if id <= 0 || id >= MaxItemList || slot < 0 || slot >= 60 {
		g.log.Warn("delete item: bad packet", "slot", u.id, "inv", slot, "item", id)
		return
	}
	if u.status != userPlay {
		g.sendHpMode(u)
		return
	}
	pl := &g.mobs[u.id].info.Player
	it := &pl.Inventory[slot]
	if int(it.Index) != id {
		g.sendItem(u, wire.SlotInv, slot, it)
		return
	}
	if other := int(u.trade.ClientID); other != 0 {
		g.removeTrade(other)
		g.removeTrade(u.id)
		return
	}
	if it.Index == 509 {
		g.clientMessage(u, "%s", g.lang(nnCantDeleteThisItem))
		g.sendItem(u, wire.SlotInv, slot, it)
		return
	}
	if since := time.Since(u.lastDelete); since <= 400*time.Millisecond {
		g.log.Warn("items deleted very fast", "slot", u.id, "ms", since.Milliseconds())
	}
	u.lastDelete = time.Now()
	g.log.Info("item deleted", "slot", u.id, "inv", slot, "item", it.Index, "effects", it.Effect)
	*it = wire.Item{}
	g.sendItem(u, wire.SlotInv, slot, it)
}

// ungroupable lists the stacks RequestUngroupItem may split.
var ungroupable = func() map[uint16]bool {
	ids := []uint16{
		475, 476, 480, 482, 483, 484, 412, 413,
		3200, 3201, 3202, 3203, 3204, 3205, 3206, 3207, 3208, 3209, 3214,
		2390, 2391, 2392, 2393, 2394, 2395, 2396, 2397, 2398, 2399, 2400,
		4027, 4028, 4029, 4010, 4140, 4144, 3288, 3289, 3290, 4011,
		2401, 2402, 2403, 2404, 2405, 2406, 2407, 2408, 2409, 2410, 2411, 2412, 2413,
		4132, 4133, 4134, 4135, 2414, 2415, 2416, 2417, 2418, 2419,
		415, 419, 420, 4117, 4118, 4119, 4120, 4121, 4550, 4158, 4159, 3314,
		4599, 4601, 4603, 4016, 4017, 4018, 4019, 4020, 777, 3173, 3182, 4850,
		1774, 2441, 2442, 2443, 2444, 3140, 4547, 4528, 3252, 3253, 1739, 4641, 4719,
	}
	m := make(map[uint16]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}()

// requestUngroupItem is CUser::RequestUngroupItem (0x2E5): split Amount
// off an inventory stack into the first free slot.
//
// Fixed: the C++ split items without an EF_AMOUNT effect (a single item,
// "amount" 1) by creating the new stack without taking anything from the
// old one, which copied the item; and splitting a whole stack left an
// empty stack behind. Now the stack must keep at least one.
func (g *Game) requestUngroupItem(u *User, pkt protocol.Packet) {
	var p UngroupItem
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	if u.trade.ClientID != 0 {
		g.removeTrade(u.id)
		return
	}
	slot, amount := int(p.SlotID), int(p.Amount)
	if slot < 0 || slot >= 60 || amount <= 0 {
		return
	}
	pl := &g.mobs[u.id].info.Player
	it := &pl.Inventory[slot]
	if int32(it.Index) != p.ItemID || itemAmount(it) < amount {
		g.sendItem(u, wire.SlotInv, slot, it)
		return
	}
	free := firstEmptySlot(pl)
	if free < 0 {
		g.clientMessage(u, "Não ha espaço para separar!")
		return
	}
	if time.Since(u.lastMoveItem) < 700*time.Millisecond {
		return
	}
	u.lastMoveItem = time.Now()
	if !ungroupable[it.Index] {
		g.clientMessage(u, "Não foi possível desagrupar")
		return
	}
	stack := -1
	for i := range 3 {
		switch it.Effect[i].Index {
		case EF_NOTRADE:
			g.clientMessage(u, "Item imóvel!")
			return
		case EF_AMOUNT:
			if stack < 0 {
				stack = i
			}
		}
	}
	if stack < 0 || int(it.Effect[stack].Value) <= amount {
		g.clientMessage(u, "Não pode separar mais do que se tem.")
		g.sendItem(u, wire.SlotInv, slot, it)
		return
	}
	it.Effect[stack].Value -= uint8(amount)
	pl.Inventory[free] = wire.Item{Index: it.Index}
	pl.Inventory[free].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint8(amount)}
	g.log.Info("ungrouped", "slot", u.id, "item", it.Index, "amount", amount, "from", slot, "to", free)
	g.sendItem(u, wire.SlotInv, slot, it)
	g.sendItem(u, wire.SlotInv, free, &pl.Inventory[free])
}

// requestChangeCity is CUser::RequestChangeCity (0x291): the home city
// (Info.CityID, 2 bits; city 4 wraps to 0 as in the C++ bitfield).
func (g *Game) requestChangeCity(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || p.Value < 0 || p.Value >= 5 {
		return
	}
	pl := &g.mobs[u.id].info.Player
	pl.Info = pl.Info&0x3F | uint8(p.Value&3)<<6
}
