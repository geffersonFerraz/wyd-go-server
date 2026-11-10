package game

import (
	"fmt"
	"slices"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Compounder NPCs (RequestCompounder/Lindy/Shany/Odin/Ehre/Agatha/Aylin/
// Tiny/Alchemy/CompoNova/Extraction.cpp). They all take the same pCompor
// window: up to 8 inventory items with their slots.
//
// Differences from the C++ (holes that could corrupt memory or duplicate
// items there):
//   - every item a recipe consumes is checked against the inventory (Shany
//     never checked its 4 Lactolerium, Ehre accepted empty places in recipes
//     that then wiped "slot -1");
//   - Ehre's earring recipe accepted any item ("< 591 && > 595" is never
//     true) and its soul recipe printed "incorrect" after succeeding with
//     the last combination;
//   - Extraction destroyed only the first 4 bytes of the item (sizeof of a
//     pointer);
//   - Odin's ReturnChance read past its table for items with a failure
//     count; the refine level (0..9) is used.

// Language.txt indexes (Basedef.h).
const (
	nnBadNetworkPackets = 23
	nnCombineFailed     = 269
	nnIncorrectComp     = 348
	nnSuccessComp       = 349
	nnMinAlqOdinRef10   = 390
	nnCantRefineWithMe  = 391
	nnOdinCombineOK     = 392
)

const (
	opCliCompounder = 0x3A6 // Anct
	opCliLindy      = 0x2C3
	opCliShany      = 0x2C4
	opCliOdin       = 0x2D2
	opCliEhre       = 0x2D3
	opCliAlchemy    = 0x2E1
	opCliAylin      = 0x3B5
	opCliAgatha     = 0x3BA
	opCliTiny       = 0x3C0
	opCliCompoNova  = 0x2FD1
	opCliExtraction = 0x2D4
	opSrvCloseInv   = 0x3A7
	opSrvChatColor  = 0xD1D
)

// compor is one decoded compounder window.
type compor struct {
	wire.Compor
	u *User
}

func (c *compor) slot(i int) int { return int(c.Slot[i]) }

// readCompor decodes the window and checks places [0,n). empty(c, i) says
// a place counts as empty (it is then zeroed, slot -1); nil means no place
// may be empty. A used place must point to an inventory slot below 60
// holding exactly that item, and no slot may be used twice. Anything else
// resyncs the client.
func (g *Game) readCompor(u *User, pkt protocol.Packet, n int, empty func(c *wire.Compor, i int) bool) (*compor, bool) {
	c := &compor{u: u}
	if wire.Unmarshal(pkt.Raw, &c.Compor) != nil || u.status != userPlay {
		return nil, false
	}
	pl := &g.mobs[u.id].info.Player
	bad := func(why string, i int) (*compor, bool) {
		g.log.Warn("compounder: bad window", "slot", u.id, "op", fmt.Sprintf("0x%X", pkt.Header.PacketID), "why", why, "place", i)
		g.sendCarry(u)
		return nil, false
	}
	for i := range n {
		if empty != nil && empty(&c.Compor, i) {
			c.Slot[i], c.Items[i] = -1, wire.Item{}
		}
	}
	for i := range n {
		s := c.slot(i)
		if s == -1 && empty != nil && empty(&c.Compor, i) {
			continue
		}
		if s < 0 || s >= 60 {
			return bad("slot", i)
		}
		if c.Items[i] != pl.Inventory[s] {
			return bad("item", i)
		}
		for y := range n {
			if y != i && c.slot(y) == s {
				return bad("same slot", i)
			}
		}
	}
	for i := n; i < len(c.Slot); i++ {
		c.Slot[i], c.Items[i] = -1, wire.Item{}
	}
	if u.trade.ClientID != 0 {
		g.removeTrade(u.id)
		return nil, false
	}
	return c, true
}

// noSlot: a place with slot -1 is empty.
func noSlot(c *wire.Compor, i int) bool { return c.Slot[i] == -1 }

// take empties the inventory slots of places idx.
func (g *Game) take(c *compor, idx ...int) {
	pl := &g.mobs[c.u.id].info.Player
	for _, i := range idx {
		s := c.slot(i)
		if s < 0 || s >= 60 {
			continue
		}
		pl.Inventory[s] = wire.Item{}
		g.sendItem(c.u, wire.SlotInv, s, &pl.Inventory[s])
	}
}

// give puts it into the inventory slot of place i.
func (g *Game) give(c *compor, i int, it wire.Item) {
	g.replaceCarry(c.u, c.slot(i), it)
}

func (g *Game) closeCompor(u *User) { g.signalParm(u, wire.ClientIDSystem, opSrvCloseInv, 2) }

func (g *Game) charName(id int) string {
	return wire.DecodeGame(wire.CString(g.mobs[id].info.Player.Name[:]))
}

func (g *Game) itemName(id uint16) string { return wire.DecodeGame(g.data.Item(id).Name) }

// serverNotice is SendServerNotice: through the DB to every channel.
func (g *Game) serverNotice(msg string) {
	p := wire.Notice96{Header: wire.NewHeader(wire.OpServerNotice, 0)}
	wire.PutGameString(p.Text[:], msg)
	g.sendDB(&p)
}

// chatAll is SendChatMessage(color, ...): a colored line for everyone here.
func (g *Game) chatAll(color uint32, msg string) {
	p := wire.ChatColor{Header: wire.NewHeader(opSrvChatColor, 0), Color: color}
	wire.PutGameString(p.Message[:96], msg)
	for _, u := range g.users[1:] {
		if u.status == userPlay {
			g.queue(u, &p)
		}
	}
}

func (g *Game) payGold(u *User, amount int64) {
	pl := &g.mobs[u.id].info.Player
	pl.Gold -= amount
	g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
}

// compounderDeal is GetCompounderDeal: an Escritura de Composição (4529)
// in the inventory is used up for +15..19% chance.
func (g *Game) compounderDeal(u *User) int {
	pl := &g.mobs[u.id].info.Player
	s := firstSlotOf(pl, 4529)
	if s == -1 {
		return 0
	}
	amountMinus(&pl.Inventory[s])
	g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
	return rnd()%5 + 15
}

// inventoryAmount is GetInventoryAmount (bags only while they last).
func inventoryAmount(pl *wire.Mob, id uint16) int {
	total := 0
	for i := range invSlots {
		if pl.Inventory[i].Index == id {
			total += itemAmount(&pl.Inventory[i])
		}
	}
	return total
}

// removeAmount is RemoveAmount: take amount units of id from the inventory.
func (g *Game) removeAmount(u *User, id uint16, amount int) {
	pl := &g.mobs[u.id].info.Player
	for i := 0; i < 60 && amount > 0; i++ {
		if pl.Inventory[i].Index != id {
			continue
		}
		for pl.Inventory[i].Index == id && amount > 0 {
			amountMinus(&pl.Inventory[i])
			amount--
		}
		g.sendItem(u, wire.SlotInv, i, &pl.Inventory[i])
	}
}

// sancEffect returns the index of the effect holding the refine level.
func sancEffect(it *wire.Item) int {
	for i := range 3 {
		if e := it.Effect[i].Index; e == EF_SANC || (e >= 116 && e <= 125) {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------- Anct

// anctBlocked are the Sephira weapons (cannot go into an Anct).
var anctBlocked = []uint16{1104, 1116, 1128, 1140, 1152, 1416, 1419, 1422, 1425, 1428, 1254, 1266, 1278, 1290,
	1302, 1553, 1569, 1572, 1575, 1578, 938, 939, 940, 941, 942, 943}

// matchCombine is GetMatchCombine: chance (1..100) of an Anct from the
// weapon (place 0), the gem (1) and up to six refined items (2..7).
func (g *Game) matchCombine(items *[8]wire.Item) int {
	if items[0].Index == 0 || int(items[0].Index) >= MaxItemList || items[1].Index == 0 || int(items[1].Index) >= MaxItemList {
		return 0
	}
	weapon := g.data.Item(items[0].Index)
	if weapon.Unique < 41 || weapon.Unique > 49 || weapon.Extreme == 0 {
		return 0
	}
	typ := g.data.effectValueByIndex(items[0].Index, EF_UNKNOW1)
	chance := 1
	for i := 2; i < 8; i++ {
		it := &items[i]
		if it.Index == 0 || int(it.Index) >= MaxItemList {
			continue
		}
		if g.data.itemAbility(it, EF_POS) == 0 || slices.Contains(anctBlocked, it.Index) {
			return 0
		}
		switch s := itemSanc(it); s {
		case 7:
			chance += 2
		case 8:
			chance += 4
		case 9:
			chance += 10
		default:
			chance += s + 2
		}
		t := g.data.effectValueByIndex(it.Index, EF_UNKNOW1)
		switch {
		case t == typ:
		case t <= typ-2:
			return 0
		case t < typ:
			chance -= 2
		default:
			chance += 2
		}
	}
	return min(chance, 100)
}

// requestCompounder is RequestCompounder (0x3A6): a weapon becomes Ancient
// (ItemList.Extreme + gem) at +7.
func (g *Game) requestCompounder(u *User, pkt protocol.Packet) {
	// Places with no item are empty whatever slot the client sent.
	noItem := func(c *wire.Compor, i int) bool { return c.Items[i].Index == 0 || int(c.Items[i].Index) >= MaxItemList }
	c, ok := g.readCompor(u, pkt, 8, noItem)
	if !ok {
		g.closeCompor(u)
		return
	}
	chance := g.matchCombine(&c.Items)
	if chance <= 0 {
		g.clientMessage(u, "Composição incorreta")
		return
	}
	stone := int(c.Items[1].Index) - 2441
	if stone < 0 || stone > 3 {
		g.clientMessage(u, "Jóia incorreta na composição do item.")
		g.closeCompor(u)
		return
	}
	for i := range 8 {
		if c.slot(i) >= 0 {
			g.take(c, i)
		}
	}
	chance += g.compounderDeal(u)
	name, weapon := g.charName(u.id), g.itemName(c.Items[0].Index)
	if roll := rnd() % 50; roll > chance {
		g.serverNotice(name + " falhou na composição do item " + weapon + " para Ancient")
		g.clientMessage(u, "Falha na composição do item.")
		g.chatAll(0xFFFF0027, name+" falhou na composição do item "+weapon+" para Ancient")
		g.closeCompor(u)
		g.log.Info("anct failed", "slot", u.id, "item", c.Items[0].Index, "roll", roll, "chance", chance)
		return
	}
	for range 3 {
		g.sendEmotion(u.id, 100, rnd()%8)
	}
	it := c.Items[0]
	it.Index = uint16(int(g.data.Item(it.Index).Extreme) + stone)
	setItemSanc(&it, 7, 0)
	g.give(c, 0, it)
	anct := g.itemName(it.Index)
	g.serverNotice("Jogador " + name + " obteve sucesso na composição de " + anct)
	g.clientMessage(u, "Jogador %s obteve sucesso na composição de %s", name, anct)
	g.chatAll(0xFFFF0027, name+" obteve sucesso na composição do item "+weapon+" para Ancient")
	g.log.Info("anct", "slot", u.id, "item", it.Index, "chance", chance)
	g.closeCompor(u)
}

// ---------------------------------------------------------------- Lindy

// requestLindy is RequestLindy (0x2C3): unlocks the Arch level blocks at
// 354/369 (Lac x10, Lac x10, 4127, Lac x4) and gives the Arch cape.
func (g *Game) requestLindy(u *User, pkt protocol.Packet) {
	c, ok := g.readCompor(u, pkt, 7, nil)
	if !ok {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	level := int(pl.BStatus.Level)
	q := m.info.Info
	waiting := ((level == 354 && q&questUnlock354 == 0) || (level == 369 && q&questUnlock369 == 0)) && q&questLvBlockedArch != 0
	ids := [7]uint16{413, 413, 4127, 413, 413, 413, 413}
	match := true
	for i, id := range ids {
		match = match && c.Items[i].Index == id
	}
	if !waiting || !match {
		g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
		g.saveUser(u, 0)
		return
	}
	g.closeCompor(u)
	if itemAmount(&c.Items[0]) != 10 || itemAmount(&c.Items[1]) != 10 || (level == 369 && m.info.Fame < 1) {
		g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
		return
	}
	g.take(c, 0, 1, 2, 3, 4, 5, 6)
	if level == 369 {
		m.info.Fame--
	}
	m.info.Info &^= questLvBlockedArch
	if level == 354 {
		m.info.Info |= questUnlock354
	} else {
		m.info.Info |= questUnlock369
	}
	cape := &pl.Equip[15]
	if cape.Index != 3191 && cape.Index != 3192 && cape.Index != 3193 {
		newCape := uint16(0)
		switch {
		case pl.CapeInfo == 7:
			newCape = 3191
		case pl.CapeInfo == 8:
			newCape = 3192
		case pl.CapeInfo == 9 || cape.Index == 0:
			newCape = 3193
		}
		// The C++ also wrote an item 0 carrying the resist when the
		// character's kingdom was none of 7/8/9; the cape is kept here.
		if newCape != 0 {
			*cape = wire.Item{Index: newCape}
			cape.Effect[1] = wire.ItemEffect{Index: EF_RESISTALL, Value: 16}
		}
	}
	if level == 369 {
		cape.Effect[1] = wire.ItemEffect{Index: EF_RESISTALL, Value: 16}
	}
	g.clientMessage(u, "%s", g.lang(nnSuccessComp))
	g.sendItem(u, wire.SlotEquip, 15, cape)
	g.log.Info("lindy unlock", "slot", u.id, "level", level)
	g.saveUser(u, 0)
}

// ---------------------------------------------------------------- Shany

// requestShany is RequestShany (0x2C4): two +9 Pedras Espirituais [E]
// (540/541), a third one and 4 Lactolerium give a Pedra Espiritual [F]
// (631..633) for 2,000,000 exp; 40% success.
func (g *Game) requestShany(u *User, pkt protocol.Packet) {
	c, ok := g.readCompor(u, pkt, 7, nil)
	if !ok {
		return
	}
	pl := &g.mobs[u.id].info.Player
	g.closeCompor(u)
	for i := range 3 {
		if id := c.Items[i].Index; id != 540 && id != 541 {
			g.clientMessage(u, "Insira a Pedra Espiritual [E] nos dois primeiros slots")
			return
		}
		if i != 2 && itemSanc(&c.Items[i]) != 9 {
			g.clientMessage(u, "Pedra Espiritual [E] deve estar +9")
			return
		}
	}
	for i := 3; i < 7; i++ {
		if c.Items[i].Index != 413 {
			g.clientMessage(u, "Insira [04] Poeiras de Lactolerium")
			return
		}
	}
	ev := evolution(pl)
	if ev == evoMortal || (ev == evoArch && pl.BStatus.Level < 255) {
		g.clientMessage(u, "Necessario ser no mínimo arch nivel 256")
		return
	}
	name := g.charName(u.id)
	if rnd()%100 <= 60 {
		g.take(c, 2, 3, 4, 5, 6)
		g.clientMessage(u, "Houve uma falha na composição")
		g.serverNotice(name + " falhou na composição(Shany) do item " + g.itemName(c.Items[0].Index))
		g.saveUser(u, 0)
		return
	}
	g.serverNotice(name + " Sucesso na composição(Shany) do item " + g.itemName(c.Items[0].Index))
	g.clientMessage(u, "Sucesso na composição")
	pl.Exp = max(pl.Exp-2000000, 0)
	table := 0
	if ev >= evoCelestial {
		table = 2 // as in the C++
	}
	for pl.BStatus.Level > 0 && pl.Exp < nextLevel[table][pl.BStatus.Level-1] {
		pl.BStatus.Level--
	}
	g.take(c, 3, 4, 5, 6, 0, 1)
	stone := uint16(633)
	switch r := rnd() % 100; {
	case r <= 33:
		stone = 631
	case r <= 66:
		stone = 632
	}
	g.give(c, 2, wire.Item{Index: stone})
	g.log.Info("shany", "slot", u.id, "stone", stone)
	g.getCurrentScore(u.id)
	g.sendEtc(u)
	g.sendScore(u.id)
	g.saveUser(u, 0)
}

// ---------------------------------------------------------------- Ehre

var ehreSoul = [11][3]uint16{
	{2441, 2441, 2441}, {2442, 2442, 2442}, {2443, 2443, 2443}, {2444, 2444, 2444},
	{2441, 2442, 2443}, {2441, 2443, 2444}, {2442, 2443, 2444}, {2442, 2441, 2443},
	{2443, 2442, 2444}, {2444, 2441, 2443}, {2441, 2443, 2442},
}

// ehreCostume maps a mount costume item to its Effect[2].Value.
func ehreCostume(id uint16) int {
	switch {
	case id == 4899:
		return 0
	case id == 4200:
		return 22
	case id == 4201:
		return 24
	case id == 4202:
		return 25
	case id >= 4204 && id <= 4219:
		return int(id) - 4204 + 26
	case id >= 5706 && id <= 5725:
		return int(id) - 5706 + 42
	}
	return 11 + int(id) - 4190
}

// requestEhre is RequestEhre (0x2D3): Pedra Espiritual [F] bonuses,
// blessed refining, Bênção de Ímago, Pedra Misteriosa, mount costumes, 4860,
// earring dust, 4683 and the soul limit.
func (g *Game) requestEhre(u *User, pkt protocol.Packet) {
	c, ok := g.readCompor(u, pkt, 7, noSlot)
	if !ok {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	items := &c.Items
	g.closeCompor(u)
	incorrect := func() { g.clientMessage(u, "%s", g.lang(nnIncorrectComp)) }
	in := func(id, lo, hi uint16) bool { return id >= lo && id <= hi }

	switch {
	case in(items[2].Index, 631, 633):
		if !in(items[0].Index, 661, 663) || !in(items[1].Index, 661, 663) {
			incorrect()
			return
		}
		if !g.ehreStone(c) {
			return
		}

	case ((items[0].Index == 697 && items[1].Index == 697) || (items[0].Index == 4717 && items[1].Index == 4717)) &&
		(items[2].Index == 3338 || items[2].Index == 4864):
		// Blessed refining (Refinação Abençoada), Celestials only.
		if evolution(pl) <= evoArch {
			g.clientMessage(u, "Disponivel apenas para celestiais ou superior")
			return
		}
		level := int(pl.BStatus.Level)
		if level < 39 {
			g.clientMessage(u, "O level mínimo é o 40")
			return
		}
		target := &pl.Inventory[c.slot(2)]
		sanc := itemSanc(target)
		if sanc >= 9 {
			g.clientMessage(u, "O item esta em sua refinação maxima")
			return
		}
		chance := 50
		switch {
		case level >= 180 && level <= 190:
			chance = 70
		case level > 190:
			chance = 80
		}
		if pl.Exp < 10000000 {
			g.clientMessage(u, "Erro - Contate a administração #3000")
			return
		}
		if rnd()%101 <= chance {
			setItemSanc(target, sanc+1, 0)
			g.clientMessage(u, "%s", g.lang(nnSuccessComp))
		} else {
			g.clientMessage(u, "%s", g.lang(nnCombineFailed))
		}
		if items[0].Index == 697 { // 4717 does not cost exp
			pl.Exp -= 10000000
			for pl.BStatus.Level > 0 && pl.Exp < nextLevel[3][pl.BStatus.Level-1] {
				pl.BStatus.Level--
				pl.Status.Level--
			}
		}
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.sendEtc(u)
		g.take(c, 0, 1)
		g.sendItem(u, wire.SlotInv, c.slot(2), target)

	case items[0].Index == 3252 && itemAmount(&items[0]) == 10 && items[1].Index == 3253 && itemAmount(&items[1]) == 5:
		if pl.Gold < 250000000 {
			g.clientMessage(u, "São necessarios 250 milhões de gold para a composição")
			return
		}
		g.take(c, 0, 1)
		g.payGold(u, 250000000)
		g.give(c, 0, wire.Item{Index: 3250})
		g.clientMessage(u, "%s", g.lang(nnSuccessComp))

	case in(items[0].Index, 5110, 5133) && in(items[1].Index, 5110, 5133) && items[2].Index == 413:
		if itemAmount(&items[2]) != 10 {
			incorrect()
			return
		}
		g.take(c, 0, 1, 2)
		it := wire.Item{Index: 4148}
		it.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 10}
		g.give(c, 2, it)
		g.clientMessage(u, "%s", g.lang(nnSuccessComp))

	// 5706..5725 are belts on this server and in the client's item list,
	// not costumes as in the C++ table.
	case in(items[0].Index, 2360, 2389) && (in(items[1].Index, 4190, 4219) || items[1].Index == 4899):
		if !g.ehreCostume(c) {
			return
		}

	case items[0].Index == 4862:
		for i := 1; i < 5; i++ {
			if !in(items[i].Index, 2441, 2444) {
				incorrect()
				return
			}
		}
		if pl.Gold < 100000000 {
			g.clientMessage(u, "Gold insuficiente")
			return
		}
		g.take(c, 0, 1, 2, 3, 4)
		if rnd()%101 <= 75 {
			g.give(c, 0, wire.Item{Index: 4860})
			g.clientMessage(u, "Sucesso na composição")
		} else {
			g.clientMessage(u, "Falha na composição")
		}
		g.payGold(u, 100000000)

	case in(items[0].Index, 591, 595):
		for i := range 3 {
			if !in(items[i].Index, 591, 595) {
				g.clientMessage(u, "Composição incorreta")
				return
			}
		}
		g.take(c, 0, 1, 2)
		if rnd()%101 <= 75 {
			total := 0
			for range 3 {
				total += rnd()%3 + 1
			}
			it := wire.Item{Index: 4719}
			it.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint16(total)}
			g.give(c, 0, it)
			g.clientMessage(u, "%s", g.lang(nnSuccessComp))
		} else {
			g.clientMessage(u, "Falha na composição")
		}

	case items[0].Index == 4684:
		if items[1].Index != 5121 || items[2].Index != 5119 || items[3].Index != 5131 || items[4].Index != 5120 ||
			items[5].Index != 5133 || items[6].Index != 5128 {
			g.clientMessage(u, "Composição incorreta")
			return
		}
		g.take(c, 0, 1, 2, 3, 4, 5, 6)
		g.give(c, 0, wire.Item{Index: 4683})
		g.clientMessage(u, "%s", g.lang(nnSuccessComp))

	default:
		found := -1
		for i, s := range ehreSoul {
			if items[0].Index == s[0] && items[1].Index == s[1] && items[2].Index == s[2] {
				found = i
				break
			}
		}
		if found == -1 {
			g.clientMessage(u, "Composição incorreta")
			return
		}
		m.info.Soul = uint8(found)
		g.clientMessage(u, "Limite da Alma configurada com sucesso.")
		g.take(c, 0, 1, 2)
	}
	g.saveUser(u, 0)
}

// ehreStone moves the bonus of the stones 661 (MP), 662 (HP) and 663
// (critical) in places 0 and 1 onto the +9 Pedra Espiritual [F] in place
// 2; 50% for both, and on failure the stone is lost. It reports whether
// the composition went ahead.
func (g *Game) ehreStone(c *compor) bool {
	u := c.u
	pl := &g.mobs[u.id].info.Player
	items := &c.Items
	if itemSanc(&pl.Inventory[c.slot(2)]) < 9 {
		g.clientMessage(u, "Pedra [F] precisa estar +9")
		return false
	}
	valueAdd := [3]uint8{EF_MPADD2, EF_HPADD2, EF_CRITICAL}
	initialAdd := [3]uint8{2, 2, 10}
	hp := g.data.itemAbilityNoSanc(&items[2], EF_HPADD2)
	mp := g.data.itemAbilityNoSanc(&items[2], EF_MPADD2)
	crit := g.data.itemAbilityNoSanc(&items[2], EF_CRITICAL)
	for i := range 2 {
		switch items[i].Index {
		case 661:
			mp += 2
		case 662:
			hp += 2
		case 663:
			crit += 10
		}
	}
	if hp > 20 || mp > 20 || crit > 100 || (hp > 10 && mp > 10) || (hp > 10 && crit != 0) ||
		(mp > 10 && (crit != 0 || hp != 0)) || (crit > 50 && (mp != 0 || hp != 0)) || (mp != 0 && hp != 0 && crit != 0) {
		g.clientMessage(u, "Composição incorreta")
		return false
	}
	roll := rnd() % 101
	success := false
	// apply adds one stone's bonus; checkMax is only done for place 1, as
	// in the C++. It returns false when the composition must stop.
	apply := func(place int, checkMax bool) bool {
		add := int(items[place].Index) - 661
		eff, start := valueAdd[add], initialAdd[add]
		value := int(start)
		for x := range 3 {
			if items[2].Effect[x].Index == eff {
				value += int(items[2].Effect[x].Value)
			}
		}
		if value == int(start) {
			free := false
			for x := range 3 {
				free = free || items[2].Effect[x].Index == 0
			}
			if !free {
				g.clientMessage(u, "Item impossível de refinar...")
				return false
			}
		}
		if checkMax && ((eff != EF_CRITICAL && value > 20) || (eff == EF_CRITICAL && value > 100)) {
			g.clientMessage(u, "Item com seu adicional maximo...")
			return false
		}
		if roll > 50 {
			return true
		}
		success = true
		for x := range 3 {
			if value == int(start) && items[2].Effect[x].Index == 0 {
				items[2].Effect[x] = wire.ItemEffect{Index: eff, Value: uint16(start)}
				break
			}
			if value != int(start) && items[2].Effect[x].Index == eff {
				items[2].Effect[x].Value += uint16(start)
				break
			}
		}
		return true
	}
	if !apply(0, false) || !apply(1, true) {
		return false
	}
	if success {
		g.clientMessage(u, "Composição concluída com sucesso")
	} else {
		g.clientMessage(u, "Houve uma falha na composição %d/50", roll)
	}
	result := "falha"
	if success {
		result = "sucesso"
	}
	g.notice(g.charName(u.id) + " obteve " + result + " na transferência de adicional para Pedra Espiritual [F]")
	g.take(c, 0, 1)
	if success {
		g.give(c, 2, items[2])
	} else {
		g.take(c, 2)
	}
	return true
}

// ehreCostume puts a costume (place 1) on a mount (place 0) for 1,000,000
// gold; 4899 takes it off.
func (g *Game) ehreCostume(c *compor) bool {
	u := c.u
	pl := &g.mobs[u.id].info.Player
	items := &c.Items
	current := int(items[0].Effect[2].Value)
	remove := items[1].Index == 4899
	if (current >= 11 && !remove) || (current < 11 && remove) {
		g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
		return false
	}
	if pl.Gold < 1000000 {
		g.clientMessage(u, "Gold insuficiente")
		return false
	}
	costume := ehreCostume(items[1].Index)
	mount := items[0].Index
	allowed := func(ids ...uint16) bool { return slices.Contains(ids, mount) }
	ok := true
	switch {
	case (costume >= 29 && costume <= 34) || (costume >= 46 && costume <= 51):
		ok = allowed(2384, 2385, 2386)
	case costume >= 35 && costume <= 37:
		ok = allowed(2381, 2382, 2383)
	case costume == 40 || costume == 41:
		ok = allowed(2376, 2378)
	case costume == 38 || costume == 39 || (costume >= 56 && costume <= 58):
		ok = allowed(2377)
	case costume >= 42 && costume <= 45:
		ok = allowed(2366, 2367, 2368, 2369, 2370, 2371, 2372, 2373, 2374, 2375)
	}
	if !ok {
		g.clientMessage(u, "Não disponível para esta montaria")
		return false
	}
	it := pl.Inventory[c.slot(0)]
	it.Effect[2].Value = uint16(costume)
	g.give(c, 0, it)
	g.take(c, 1)
	g.payGold(u, 1000000)
	g.clientMessage(u, "%s", g.lang(nnSuccessComp))
	return true
}

// ---------------------------------------------------------------- Odin

// itemExtractionType is GetItemType: the extraction item for a position.
func itemExtractionType(pos int) uint16 {
	switch pos {
	case 2:
		return 3021
	case 4:
		return 3022
	case 8:
		return 3023
	case 16:
		return 3024
	case 32:
		return 3025
	case 64, 192:
		return 3026
	}
	return uint16(pos)
}

// returnChance is ReturnChance for a Refinação Abençoada (3338).
func returnChance(it *wire.Item) int {
	refs := [10]int{2, 3, 4, 5, 6, 7, 8, 10, 12, 15}
	for i := range 3 {
		if it.Effect[i].Index == EF_SANC {
			return refs[int(it.Effect[i].Value)%10]
		}
	}
	return 2
}

var odinSecret = [4][7]uint16{
	{5126, 5127, 5121, 5114, 5125, 5111, 5118},
	{5131, 5113, 5115, 5116, 5125, 5112, 5114},
	{5110, 5124, 5117, 5129, 5114, 5125, 5128},
	{5122, 5119, 5132, 5120, 5130, 5133, 5123},
}

func comporIs(items *[8]wire.Item, ids ...uint16) bool {
	for i, id := range ids {
		if items[i].Index != id {
			return false
		}
	}
	return true
}

// requestOdin is RequestOdin (0x2D2): +11..+15 refining, the Celestial 40
// unlock, Pedra da Fúria, cape refining, Pista de Runas, Pedra de Kersef
// and the secret stones.
func (g *Game) requestOdin(u *User, pkt protocol.Packet) {
	c, ok := g.readCompor(u, pkt, 7, nil)
	if !ok {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	items := &c.Items
	g.closeCompor(u)
	all := []int{0, 1, 2, 3, 4, 5, 6}

	switch {
	case (items[0].Index == 4043 && items[1].Index == 4043) ||
		(items[0].Index == 413 && itemAmount(&items[0]) == 10 && items[1].Index == 413 && itemAmount(&items[1]) == 10):
		g.odinRefine(c)
		return

	case comporIs(items, 4127, 4127, 5135, 5113, 5129, 5112, 5110):
		q := m.info.Info
		if evolution(pl) != evoCelestial || pl.BStatus.Level != 39 || q&questLvBlocked == 0 || q&questUnlock39 != 0 {
			g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
			return
		}
		g.take(c, all...)
		if rnd()%100 <= 95 {
			m.info.Info = m.info.Info&^questLvBlocked | questUnlock39
			g.clientMessage(u, "%s", g.lang(nnSuccessComp))
		} else {
			g.clientMessage(u, "%s", g.lang(nnCombineFailed))
		}

	case comporIs(items, 5125, 5115, 5111, 5112, 5120, 5128, 5119):
		g.take(c, all...)
		g.clientMessage(u, "%s", g.lang(nnSuccessComp))
		g.give(c, 0, wire.Item{Index: 3020})

	case comporIs(items, 4127, 4127, 5135, 413, 413, 413, 413):
		if evolution(pl) < evoCelestial {
			g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
			return
		}
		sanc := itemSanc(&pl.Equip[15])
		if sanc >= 9 {
			g.clientMessage(u, "%s", g.lang(nnCantRefineMore))
			return
		}
		g.take(c, all...)
		setItemSanc(&pl.Equip[15], sanc+1, 0)
		g.clientMessage(u, "%s", g.lang(nnSuccessComp))
		g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
		g.getCurrentScore(u.id)
		g.sendScore(u.id)

	case comporIs(items, 413, 413, 413, 413, 413, 413, 413):
		g.take(c, all...)
		g.give(c, 0, wire.Item{Index: 5134})
		g.clientMessage(u, "%s", g.lang(nnSuccessComp))

	case items[0].Index == 674:
		g.odinKersef(u)
		return

	default:
		for y, recipe := range odinSecret {
			if !comporIs(items, recipe[:]...) {
				continue
			}
			if pl.Gold < 2000000 {
				g.clientMessage(u, "São necessarios 2 milhões de gold")
				return
			}
			g.take(c, all...)
			g.payGold(u, 2000000)
			secret := uint16(5334 + y)
			if rnd()%101 > 95 {
				g.clientMessage(u, "Falha na composição")
				g.notice(g.charName(u.id) + " falhou na composição de " + g.itemName(secret))
			} else {
				g.give(c, 0, wire.Item{Index: secret})
				g.clientMessage(u, "Composição concluída")
				g.notice(g.charName(u.id) + " compôs com sucesso a " + g.itemName(secret))
			}
			g.saveUser(u, 0)
			return
		}
		runes := true
		for i := range 7 {
			runes = runes && items[i].Index >= 5110 && items[i].Index <= 5133
		}
		if !runes {
			g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
			return
		}
		// Seven random runes: 5% for a random secret stone.
		if pl.Gold < 2000000 {
			g.clientMessage(u, "São necessarios 2 milhões de gold")
			return
		}
		g.take(c, all...)
		g.payGold(u, 2000000)
		if rnd()%101 <= 5 {
			secret := uint16(5334 + rnd()%4)
			g.give(c, 0, wire.Item{Index: secret})
			g.clientMessage(u, "Composição concluída")
			g.notice(g.charName(u.id) + " compôs com sucesso a " + g.itemName(secret))
		} else {
			g.clientMessage(u, "Houve uma falha na composição do item")
			g.notice(g.charName(u.id) + " falhou na composição da Pedra Secreta")
		}
	}
	g.saveUser(u, 0)
}

// odinRefine is the +11..+15 refining of RequestOdin: two Pedras da Luz
// (4043) or 2x10 Lactolerium, the item in place 2 and Refinações
// Abençoadas / secret stones in 3..6. A failure takes one level off, and
// one time in five breaks the item (or turns it into an extraction when
// the 4043 were used).
func (g *Game) odinRefine(c *compor) {
	u := c.u
	pl := &g.mobs[u.id].info.Player
	items := &c.Items
	sanc := itemSanc(&items[2])
	if sanc >= 15 {
		g.clientMessage(u, "%s", g.lang(nnCantRefineMore))
		return
	}
	if sanc <= 10 {
		g.clientMessage(u, "%s", g.lang(nnMinAlqOdinRef10))
		return
	}
	if g.data.effectValueByIndex(items[2].Index, EF_MOBTYPE) == 3 {
		g.clientMessage(u, "%s", g.lang(nnCantRefineWithMe))
		return
	}
	rate := 1
	switch sanc {
	case 11:
		rate = 4
	case 12:
		rate = 3
	case 13:
		rate = 2
	}
	canBreak, extraction := true, false
	if items[0].Index == 4043 && items[1].Index == 4043 {
		extraction = true
		rate += 2
	}
	pos := int(g.data.Item(items[2].Index).Pos)
	if (items[2].Index >= 3500 && items[2].Index <= 3507) || pos == 128 {
		canBreak, extraction = false, false
	}
	secrets := false
	for i := 3; i < 7; i++ {
		id := items[i].Index
		if id >= 5334 && id <= 5337 {
			rate++
			secrets = true
			continue
		}
		if id != 3338 {
			continue
		}
		if secrets {
			g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
			return
		}
		rate += returnChance(&items[i])
		if itemSanc(&items[i]) != 0 {
			canBreak, extraction = false, false
		}
	}
	if pos > 192 {
		g.clientMessage(u, "%s", g.lang(nnCantRefine))
		return
	}
	if rate > 100 {
		g.clientMessage(u, "%s", g.lang(nnBadNetworkPackets))
		return
	}
	target := &pl.Inventory[c.slot(2)]
	name := g.charName(u.id)
	roll := rnd() % 50
	switch e := sancEffect(target); {
	case roll <= rate:
		if e >= 0 {
			target.Effect[e].Value += 4
		}
		g.serverNotice(fmt.Sprintf(g.lang(nnOdinCombineOK), name, sanc+1))
	case canBreak && roll%5 == 0:
		g.serverNotice(fmt.Sprintf("%s falhou a refinação de %s para %d", name, g.itemName(target.Index), sanc+1))
		if extraction {
			g.makeOdinExtraction(target, sanc)
		} else {
			*target = wire.Item{}
		}
	default:
		g.serverNotice(fmt.Sprintf("%s falhou a refinação de %s para %d", name, g.itemName(target.Index), sanc+1))
		if e >= 0 {
			target.Effect[e].Value -= 4
		}
	}
	g.log.Info("odin refine", "slot", u.id, "item", target.Index, "to", sanc+1, "roll", roll, "rate", rate)
	g.take(c, 0, 1, 3, 4, 5, 6)
	g.sendItem(u, wire.SlotInv, c.slot(2), target)
	g.saveUser(u, 0)
}

// makeOdinExtraction turns a broken item into its extraction (3021..3026)
// carrying the item class in EF 87 and the base values in its effects.
func (g *Game) makeOdinExtraction(it *wire.Item, sanc int) {
	e := sancEffect(it)
	if e < 0 {
		return
	}
	value := g.data.effectValueByIndex(it.Index, EF_UNKNOW1)
	mobType := g.data.effectValueByIndex(it.Index, EF_MOBTYPE)
	if !(value <= 5 && mobType == 0) {
		switch {
		case value == 6 && mobType == 1 && sanc <= 9:
			value = 10
		case value == 6 && mobType == 1:
			value = 11
		case value == 6:
		case mobType == 1 && sanc >= 10:
			value = 9
		case mobType == 1:
			value = 8
		}
	}
	it.Effect[e] = wire.ItemEffect{Index: 87, Value: uint16(value)}
	data := g.data.Item(it.Index)
	for i := range 3 {
		idx := it.Effect[i].Index
		if idx == EF_SANC || (idx >= 116 && idx <= 125) || idx == 0 || idx == 87 || data.Pos > 32 {
			continue
		}
		it.Effect[i].Value += uint16(g.data.effectValueByIndex(it.Index, int(idx)))
	}
	it.Index = itemExtractionType(int(data.Pos))
}

// odinKersef is the Pedra de Kersef (4552): 5 674, 2 675, 3 676, 1 677,
// 2 4127 and 5 770 from anywhere in the inventory and 100,000,000 gold;
// 80% success, a failure loses 5 random units.
func (g *Game) odinKersef(u *User) {
	pl := &g.mobs[u.id].info.Player
	need := []struct {
		id uint16
		n  int
	}{{674, 5}, {675, 2}, {676, 3}, {677, 1}, {4127, 2}, {770, 5}}
	for _, r := range need {
		if inventoryAmount(pl, r.id) < r.n {
			g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
			return
		}
	}
	if pl.Gold < 100000000 {
		g.clientMessage(u, "Gold insuficiente!")
		return
	}
	if firstEmptySlot(pl) == -1 {
		return
	}
	g.payGold(u, 100000000) // the C++ did not tell the client
	if rnd()%100 >= 80 {
		for range 5 {
			g.removeAmount(u, need[rnd()%6].id, 1)
		}
		g.clientMessage(u, "%s", g.lang(nnCombineFailed))
		return
	}
	for _, r := range need {
		g.removeAmount(u, r.id, r.n)
	}
	g.putItem(u, &wire.Item{Index: 4552})
	g.clientMessage(u, "%s", g.lang(nnSuccessComp))
}

// ---------------------------------------------------------------- Agatha

// requestAgatha is RequestAgatha (0x3BA): the bonuses of a [D]+ mortal
// item (place 1) move to an Arch item of the same kind (place 0) at +7,
// with 4 Pedras da Luz (3140). A failure destroys the Arch item.
func (g *Game) requestAgatha(u *User, pkt protocol.Packet) {
	c, ok := g.readCompor(u, pkt, 6, noSlot)
	if !ok {
		return
	}
	items := &c.Items
	if g.data.effectValueByIndex(items[0].Index, EF_MOBTYPE) != 1 {
		g.clientMessage(u, "Utilize apenas itens archs na composição")
		return
	}
	class := g.data.effectValueByIndex(items[1].Index, EF_UNKNOW1)
	if class < 4 {
		g.clientMessage(u, "Utilize apenas itens [D] ou superior")
		return
	}
	arch, mortal := g.data.Item(items[0].Index), g.data.Item(items[1].Index)
	if itemSanc(&items[0]) >= 10 {
		g.clientMessage(u, "Não é possível transferir para itens acima de +9")
		return
	}
	if arch.Pos != mortal.Pos {
		g.clientMessage(u, "Os itens devem ter o mesmo tipo")
		return
	}
	if arch.Pos > 32 {
		g.clientMessage(u, "Posição do item invalido")
		return
	}
	if g.data.effectValueByIndex(items[0].Index, EF_CLASS) != g.data.effectValueByIndex(items[1].Index, EF_CLASS) {
		g.clientMessage(u, "Necessario ser da mesma classe")
		return
	}
	for i := 2; i < 6; i++ {
		if items[i].Index != 3140 {
			g.clientMessage(u, "Utilize 4 Pedras da Luz")
			return
		}
	}
	chance := 35
	if grade := int(mortal.Grade); class == 4 && grade >= 1 && grade <= 4 {
		chance = [4]int{20, 25, 35, 45}[grade-1]
	}
	roll := rnd() % 50
	g.closeCompor(u)
	name, archName := g.charName(u.id), g.itemName(items[0].Index)
	g.take(c, 2, 3, 4, 5)
	if roll <= chance {
		it := items[1]
		if e := sancEffect(&it); e >= 0 {
			it.Effect[e].Value = 7
		}
		it.Index = items[0].Index
		g.notice(name + " transferiu com sucesso adicional para " + archName)
		g.clientMessage(u, "Jogador %s obteve sucesso na composição de %s ", name, archName)
		g.take(c, 1)
		g.give(c, 0, it)
	} else {
		g.take(c, 0)
		g.notice(name + " falhou na transferência de adicional para " + archName)
		g.clientMessage(u, "Falha na composição do item.")
	}
	g.log.Info("agatha", "slot", u.id, "item", items[0].Index, "ok", roll <= chance)
	g.saveUser(u, 0)
}

// ---------------------------------------------------------------- Aylin

// requestAylin is RequestAylin (0x3B5): two identical +9 Ancients or sets
// (places 0 and 1), a third item (2) and four identical gems (3..6) make
// a +10 with the gem's bonus, for 50,000,000 gold.
func (g *Game) requestAylin(u *User, pkt protocol.Packet) {
	c, ok := g.readCompor(u, pkt, 7, func(c *wire.Compor, i int) bool { return i >= 3 && c.Slot[i] == -1 })
	if !ok {
		return
	}
	pl := &g.mobs[u.id].info.Player
	items := &c.Items
	if g.data.Item(items[0].Index).Pos >= 256 || g.data.Item(items[1].Index).Pos == 0 {
		g.clientMessage(u, "Só é possível com armas ou sets")
		return
	}
	if itemSanc(&items[0]) < 9 || itemSanc(&items[1]) < 9 {
		g.clientMessage(u, "Só é possível combinar com itens +9")
		return
	}
	for i := range 2 {
		d := g.data.Item(items[i].Index)
		if d.Unique >= 41 && d.Unique <= 49 && (d.Grade < 5 || d.Grade > 8) {
			g.clientMessage(u, "Utilize itens ancts na composição")
			return
		}
	}
	if items[0].Index != items[1].Index {
		g.clientMessage(u, "Combinação incorreta.")
		return
	}
	if pl.Gold < 50000000 {
		g.clientMessage(u, "São necessarios 50.000.000 de gold para continuar")
		return
	}
	for i := 3; i < 7; i++ {
		if items[i].Index < 2441 || items[i].Index > 2444 {
			g.clientMessage(u, "Incorreto")
			return
		}
	}
	if items[3].Index != items[4].Index || items[3].Index != items[5].Index || items[3].Index != items[6].Index {
		g.clientMessage(u, "As jóias devem ser iguais!")
		return
	}
	sanc := int(items[3].Index) - 2211 // 230..233: +10 with the gem
	chance := 50 + g.compounderDeal(u)
	roll := rnd() % 101
	g.closeCompor(u)
	g.payGold(u, 50000000)
	name := g.charName(u.id)
	if roll > chance {
		g.take(c, 2, 3, 4, 5, 6)
		g.clientMessage(u, "Houve uma falha na composição")
		g.notice("Jogador " + name + " falhou a composição do item " + g.itemName(items[0].Index) + " +10")
		return
	}
	keep, lose := 0, 1
	if rnd()%50 <= 25 {
		pl.Inventory[c.slot(0)] = pl.Inventory[c.slot(1)]
	} else {
		keep, lose = 1, 0
		pl.Inventory[c.slot(1)] = pl.Inventory[c.slot(0)]
	}
	pl.Inventory[c.slot(lose)] = wire.Item{}
	it := &pl.Inventory[c.slot(keep)]
	d := g.data.Item(it.Index)
	if d.Pos >= 64 && d.Pos != 128 {
		it.Index = uint16(int(it.Index) - (int(d.Grade) - 5) + (sanc - 230))
	}
	if e := sancEffect(it); e >= 0 {
		it.Effect[e].Value = uint16(sanc)
	}
	g.take(c, 2, 3, 4, 5, 6)
	g.sendItem(u, wire.SlotInv, c.slot(0), &pl.Inventory[c.slot(0)])
	g.sendItem(u, wire.SlotInv, c.slot(1), &pl.Inventory[c.slot(1)])
	g.notice("Jogador " + name + " concluiu com sucesso a composição do item " + g.itemName(it.Index) + " +10")
	g.log.Info("aylin", "slot", u.id, "item", it.Index, "roll", roll, "chance", chance)
	g.saveUser(u, 0)
}

// ---------------------------------------------------------------- Tiny

// requestTiny is RequestTiny (0x3C0): the bonuses of a +9 Ancient (place
// 1) move to another +9 Ancient (place 0) at +7 with a [D]+ item (2), for
// 100,000,000 gold; 25% + 4% per class above [C].
func (g *Game) requestTiny(u *User, pkt protocol.Packet) {
	var raw wire.Compor
	if wire.Unmarshal(pkt.Raw, &raw) == nil {
		for i := range 3 {
			if raw.Slot[i] == -1 {
				g.clientMessage(u, "Coloque todos os itens")
				return
			}
		}
	}
	c, ok := g.readCompor(u, pkt, 3, nil)
	if !ok {
		return
	}
	pl := &g.mobs[u.id].info.Player
	items := &c.Items
	g.closeCompor(u)
	typ := g.data.effectValueByIndex(items[2].Index, EF_UNKNOW1)
	if typ < 4 {
		g.clientMessage(u, "Utilize um item [D] ou superior na composição!")
		return
	}
	chance := 25 + (typ-3)*4
	for i := range 2 {
		if d := g.data.Item(items[i].Index); d.Grade < 5 || d.Grade > 8 {
			g.clientMessage(u, "Utilize itens ancts na composição")
			return
		}
		if itemSanc(&items[i]) < 9 {
			g.clientMessage(u, "Utilize itens +9 na composição")
			return
		}
	}
	if pl.Gold < 100000000 {
		g.clientMessage(u, "São necessarios 100 milhões de gold para a composição")
		return
	}
	roll := rnd() % 100
	g.payGold(u, 100000000)
	g.take(c, 2)
	name, target := g.charName(u.id), g.itemName(items[0].Index)
	if roll <= chance {
		it := items[1]
		g.notice(name + " transferiu com sucesso o adicional para " + target)
		g.clientMessage(u, "Sucesso na transferência de adicional de %s para %s ", g.itemName(it.Index), target)
		it.Index = items[0].Index
		setItemSanc(&it, 7, 0)
		g.take(c, 0)
		g.give(c, 1, it)
	} else {
		g.notice(name + " falhou na transferência o adicional para " + target)
		g.clientMessage(u, "Falha na transferência de adicional ")
	}
	g.saveUser(u, 0)
}

// ---------------------------------------------------------------- Alchemy

type alchemyRecipe struct {
	need   [7]struct{ id, sanc, amount int }
	gold   int64
	result uint16
}

func recipe(gold int64, result uint16, need ...[3]int) alchemyRecipe {
	r := alchemyRecipe{gold: gold, result: result}
	for i, n := range need {
		r.need[i].id, r.need[i].sanc, r.need[i].amount = n[0], n[1], n[2]
	}
	return r
}

// alchemyRecipes is compositionItems of RequestAlchemy.
var alchemyRecipes = []alchemyRecipe{
	recipe(20000000, 3200, [3]int{413, 0, 1}, [3]int{2441, 0, 1}, [3]int{2442, 0, 1}),
	recipe(20000000, 3201, [3]int{413, 0, 1}, [3]int{2443, 0, 1}, [3]int{2442, 0, 1}),
	recipe(20000000, 3202, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}),
	recipe(20000000, 3203, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{697, 0, 1}),
	recipe(20000000, 3204, [3]int{412, 0, 1}, [3]int{2441, 0, 1}, [3]int{2444, 0, 1}),
	recipe(20000000, 3205, [3]int{412, 0, 1}, [3]int{2444, 0, 1}, [3]int{2443, 0, 1}),
	recipe(20000000, 3214, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{413, 0, 10}, [3]int{3221, 0, 1}),
	recipe(100000000, 3206, [3]int{413, 0, 60}, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{614, 9, 1}, [3]int{3221, 0, 1}),
	recipe(100000000, 3208, [3]int{413, 0, 60}, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{612, 9, 1}, [3]int{3221, 0, 1}),
	recipe(100000000, 3209, [3]int{413, 0, 60}, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{4127, 0, 1}, [3]int{613, 9, 1}, [3]int{3221, 0, 1}),
}

// requestAlchemy is RequestAlchemy (0x2E1): fixed recipes; the chance is
// 3% + mastery 3 / 5, +20% with skill bit 15. Everything in the window is
// used up, even items the recipe did not ask for (as in the C++).
func (g *Game) requestAlchemy(u *User, pkt protocol.Packet) {
	c, ok := g.readCompor(u, pkt, 7, noSlot)
	if !ok {
		return
	}
	pl := &g.mobs[u.id].info.Player
	var found *alchemyRecipe
	for i := range alchemyRecipes {
		r := &alchemyRecipes[i]
		match := true
		for k, n := range r.need {
			if n.id == 0 {
				continue
			}
			it := &c.Items[k]
			if int(it.Index) != n.id || itemSanc(it) != n.sanc || itemAmount(it) != n.amount {
				match = false
				break
			}
		}
		if match {
			found = r
			break
		}
	}
	if found == nil || (found.gold != 0 && pl.Gold < found.gold) {
		g.clientMessage(u, "%s", g.lang(nnIncorrectComp))
		return
	}
	chance := 3 + int(pl.Status.Mastery[2])*2/10
	if pl.Learn[0]&0x8000 != 0 {
		chance += 20
	}
	roll := rnd() % 100
	for i := range 7 {
		if c.slot(i) >= 0 {
			g.take(c, i)
		}
	}
	if found.gold != 0 {
		g.payGold(u, found.gold)
	}
	g.closeCompor(u)
	if roll > chance {
		g.clientMessage(u, "%s", g.lang(nnCombineFailed))
	} else {
		g.give(c, 0, wire.Item{Index: found.result})
		g.clientMessage(u, "%s", g.lang(nnSuccessComp))
	}
	g.log.Info("alchemy", "slot", u.id, "result", found.result, "roll", roll, "chance", chance)
	g.saveUser(u, 0)
}

// ---------------------------------------------------------------- CompoNova

// requestCompoNova is RequestCompoNova (0x2FD1): one Lactolerium becomes
// item 3314 (EF 61 = 1). Only place 0 is looked at.
func (g *Game) requestCompoNova(u *User, pkt protocol.Packet) {
	c, ok := g.readCompor(u, pkt, 6, noSlot)
	if !ok {
		return
	}
	pl := &g.mobs[u.id].info.Player
	g.closeCompor(u)
	for i := range 7 {
		if c.Items[i].Index == 747 {
			g.clientMessage(u, "Composição incorreta")
			return
		}
	}
	if c.Items[0].Index != 413 {
		g.clientMessage(u, "Composição incorreta")
		return
	}
	s := -1
	for i := range 30 {
		if pl.Inventory[i].Index == 413 && itemAmount(&pl.Inventory[i]) >= 1 {
			s = i
			break
		}
	}
	if s == -1 {
		g.clientMessage(u, "Traga os itens necessarios")
		return
	}
	amountMinus(&pl.Inventory[s])
	g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
	it := wire.Item{Index: 3314}
	it.Effect[0] = wire.ItemEffect{Index: 61, Value: 1}
	g.putItem(u, &it)
	g.clientMessage(u, "Sucesso!")
	g.saveUser(u, 0)
}

// ---------------------------------------------------------------- Extraction

// requestExtraction is RequestExtraction (0x2D4): with a Pedra do Sábio
// (1774) a piece of armor up to +9 becomes its extraction (3021..3025).
// The chance is 3% + mastery 3 * 2 / 11, +10% with skill bit 15; a failure
// destroys the item and the stone.
func (g *Game) requestExtraction(u *User, pkt protocol.Packet) {
	var p wire.Extraction
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || p.SlotID >= 60 {
		return
	}
	pl := &g.mobs[u.id].info.Player
	it := &pl.Inventory[p.SlotID]
	if it.Index == 0 || int(it.Index) >= MaxItemList {
		return
	}
	data := g.data.Item(it.Index)
	result := itemExtractionType(int(data.Pos))
	if result < 3021 || result >= 3026 {
		g.clientMessage(u, "Só é possível utilizar em equipamentos.")
		return
	}
	stone := firstSlotOf(pl, 1774)
	if stone == -1 {
		g.clientMessage(u, "Para realizar a composição é necessario [01] Pedra do Sabio")
		return
	}
	if data.Unique >= 41 && data.Unique <= 49 && data.Grade >= 5 && data.Grade <= 8 {
		g.clientMessage(u, "Não é possível usar em itens ancts")
		return
	}
	if itemSanc(it) > 9 {
		g.clientMessage(u, "Não é possível tirar extração de itens acima de +9")
		return
	}
	// IsImpossibleToRefine: three effects and none of them a refine.
	if it.Effect[0].Index != 0 && it.Effect[1].Index != 0 && it.Effect[2].Index != 0 && sancEffect(it) == -1 {
		g.clientMessage(u, "Item impossível de refinar")
		return
	}
	chance := 3 + int(pl.Status.Mastery[2])*2/11
	if pl.Learn[0]&0x8000 != 0 {
		chance += 10
	}
	clearStone := func() {
		pl.Inventory[stone] = wire.Item{}
		g.sendItem(u, wire.SlotInv, stone, &pl.Inventory[stone])
	}
	if roll := rnd() % 100; roll > chance {
		g.log.Info("extraction failed", "slot", u.id, "item", it.Index, "roll", roll, "chance", chance)
		*it = wire.Item{}
		clearStone()
		g.sendItem(u, wire.SlotInv, int(p.SlotID), it)
		g.clientMessage(u, "Item destruído")
		return
	}
	class := g.data.effectValueByIndex(it.Index, EF_UNKNOW1)
	e := sancEffect(it)
	if e == -1 {
		for i := range 3 {
			if it.Effect[i].Index == 0 {
				e = i
				break
			}
		}
	}
	if e == -1 {
		g.clientMessage(u, "Item impossível de refinar")
		return
	}
	if class == 0 {
		g.clientMessage(u, "Necessario que o item possua alguma classe ([A],[B],[C] ....)")
		return
	}
	it.Effect[e] = wire.ItemEffect{Index: 87, Value: uint16(class)}
	// GetStaticItemAbility(EF_DAMAGE): the ItemList damage (no refine left).
	base := 0
	for _, ef := range data.Effect {
		if int(ef.Index) == EF_DAMAGE {
			base += int(ef.Value)
		}
	}
	for i := 1; i < 3; i++ {
		if int(it.Effect[i].Index) == EF_DAMAGE {
			it.Effect[i].Value += uint16(base)
		}
	}
	it.Index = result
	clearStone()
	g.sendItem(u, wire.SlotInv, int(p.SlotID), it)
	g.clientMessage(u, "Extração criada")
	g.log.Info("extraction", "slot", u.id, "item", it.Index)
}
