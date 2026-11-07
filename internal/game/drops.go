package game

import (
	"bufio"
	"bytes"
	"strings"

	"dbsrv/internal/wire"
)

// Mob drops (GetFunc.cpp "SISTEMA DROP POR TXT": loadMobDropData,
// HandleMobDrop; Basedef.cpp PutItem, AgroupItem, AgroupItemFada).
//
// The C++ MobKilled still walks the mob's inventory with the gameconfig.txt
// drop rates but its drop call was removed; drops come only from the
// "controle drop/Controle.txt" table, straight into the killer's inventory:
// every N kills of a mob (per player and item) gives that item.

// MobDrop is one monster's drops. Name must match the monster's name
// exactly, trailing spaces included: "Guer Caveira " and "Guer Caveira" are
// different monsters (different npc files), as in the C++ table.
type MobDrop struct {
	Name     string     `yaml:"name"`
	MaxDrops int        `yaml:"max_drops"`
	Gold     int        `yaml:"gold"`
	Items    []DropItem `yaml:"items"`
	Section  string     `yaml:"-"` // "////X////" heading in Controle.txt
}

// DropItem: every Kills kills of the monster give Item (with one effect).
type DropItem struct {
	Kills  int    `yaml:"kills"`
	Index  uint16 `yaml:"item"`
	Effect uint8  `yaml:"effect,omitempty"`
	Value  uint8  `yaml:"value,omitempty"`
}

// ParseDropsTxt reads the C++ Controle.txt (loadMobDropData). It returns
// the blocks in order and the names of blocks dropped because an earlier
// block had the same name (the C++ getMobDropData used the first one).
// Names are converted from Windows-1252 to UTF-8.
func ParseDropsTxt(raw []byte) (mobs []*MobDrop, skipped []string) {
	seen := map[string]bool{}
	section := ""
	sc := bufio.NewScanner(bytes.NewReader(raw))
	var cur *MobDrop
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		if cur == nil {
			if name, ok := strings.CutPrefix(line, "MobName:"); ok {
				cur = &MobDrop{Name: wire.DecodeGame(name), Section: section}
			} else if strings.HasPrefix(line, "/") {
				section = wire.DecodeGame(strings.TrimSpace(strings.Trim(line, "/ ")))
			}
			continue
		}
		switch {
		case line == "END":
			if seen[cur.Name] {
				skipped = append(skipped, cur.Name)
			} else {
				seen[cur.Name] = true
				mobs = append(mobs, cur)
			}
			cur = nil
		case strings.HasPrefix(line, "MaxDrops:"):
			cur.MaxDrops = atoi(line[len("MaxDrops:"):])
		case strings.HasPrefix(line, "GoldDrop:"):
			cur.Gold = atoi(line[len("GoldDrop:"):])
		default:
			// istringstream >> : missing fields are 0.
			f := strings.Fields(line)
			var v [4]int
			for i := 0; i < 4 && i < len(f); i++ {
				v[i] = atoi(f[i])
			}
			cur.Items = append(cur.Items, DropItem{Kills: v[0], Index: uint16(v[1]), Effect: uint8(v[2]), Value: uint8(v[3])})
		}
	}
	return mobs, skipped
}

// mobName is the mob's name as the C++ std::string saw it (up to NUL).
func mobName(pl *wire.Mob) string {
	n := 0
	for n < len(pl.Name) && pl.Name[n] != 0 {
		n++
	}
	return string(pl.Name[:n])
}

// handleMobDrop is HandleMobDrop(player, mobName).
func (g *Game) handleMobDrop(u *User, name string) {
	drop := g.data.Drops[name]
	if drop == nil || len(drop.Items) == 0 {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if drop.Gold > 0 {
		// The C++ added without a cap; keep the 2 billion limit.
		pl.Gold = int32(min(int64(pl.Gold)+int64(drop.Gold), maxGold))
		g.sendEtc(u)
		g.sendScore(u.id)
	}
	if drop.MaxDrops <= 0 {
		return
	}
	if u.dropKills == nil {
		u.dropKills = map[string]map[int]int{}
	}
	counts := u.dropKills[name]
	if counts == nil {
		counts = map[int]int{}
		u.dropKills[name] = counts
	}
	dropped := 0
	for _, it := range drop.Items {
		key := int(it.Index)*1000000 + int(it.Effect)*1000 + int(it.Value)
		counts[key]++
		item := wire.Item{Index: it.Index}
		item.Effect[0] = wire.ItemEffect{Index: it.Effect, Value: it.Value}

		rate := it.Kills
		fairy := pl.Equip[13].Index
		switch fairy {
		case 3901, 3904, 3905, 3907, 3908, 3914:
			rate -= rate * 32 / 100
		case 3915:
			rate -= rate * 40 / 100
		case 3902:
			rate -= rate * 16 / 100
		}
		if b := m.combat.dropBonus; b > 0 && b < 100 {
			rate -= rate * b / 100
		}
		if rate == 0 {
			rate = 1
		}
		if counts[key] < rate {
			continue
		}
		counts[key] = 0
		dropped++
		if dropped > drop.MaxDrops {
			continue
		}
		if m.info.FilterDrop.IsEnabled && !g.filterAllows(u, &item) {
			continue
		}
		g.giveDrop(u, &item, fairy == 3915)
	}
}

// filterAllows is PanelCheckFilterDrop: the item is in the drop filter.
func (g *Game) filterAllows(u *User, it *wire.Item) bool {
	m := &g.mobs[u.id]
	if m.info.Player.Status.CurHP <= 0 {
		return false
	}
	for _, f := range m.info.FilterDrop.Items {
		if f.Index != 0 && f.Index == it.Index {
			return true
		}
	}
	return false
}

// giveDrop puts a dropped item in the inventory, stacking where the C++
// did: with the gold fairy (3915) any stackable item, otherwise only
// 4117..4121. Items 1..40 (faces) are never given without the fairy.
func (g *Game) giveDrop(u *User, it *wire.Item, goldFairy bool) {
	pl := &g.mobs[u.id].info.Player
	if !goldFairy && it.Index <= 40 {
		return
	}
	if it.Index == 0 {
		return
	}
	slot := firstSlotOf(pl, it.Index)
	if slot == -1 {
		g.putItem(u, it)
		return
	}
	if !goldFairy && (it.Index < 4117 || it.Index > 4121) {
		g.putItem(u, it)
		return
	}
	if g.agroupItemFada(u, it) {
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		return
	}
	g.putItem(u, it)
}

// putItem is PutItem: first free inventory slot (lost when full, as in the C++).
func (g *Game) putItem(u *User, it *wire.Item) bool {
	if u.status != userPlay {
		return false
	}
	pl := &g.mobs[u.id].info.Player
	slot := firstEmptySlot(pl)
	if slot == -1 {
		return false
	}
	pl.Inventory[slot] = *it
	g.sendItem(u, wire.SlotInv, slot, it)
	return true
}

// firstSlotOf is GetFirstSlot(clientId, itemId) for itemId != 0.
func firstSlotOf(pl *wire.Mob, id uint16) int {
	for i := range invSlots {
		if pl.Inventory[i].Index == id {
			return i
		}
	}
	return -1
}

var fairyStackable = map[uint16]bool{}

func init() {
	for _, id := range []uint16{475, 476, 480, 482, 483, 485, 412, 413, 419, 420,
		2390, 2391, 2392, 2393, 2394, 2395, 2396, 2397, 2398, 2399, 2400, 2401, 2402,
		2403, 2404, 2405, 2406, 2407, 2408, 2409, 2410, 2411, 2412, 2413, 2414, 2415,
		2416, 2417, 2418, 2419, 4016, 4017, 4018, 4019, 4020, 4850, 4117, 4118, 4119,
		4120, 4121, 3140, 2443, 2441, 2444, 2442, 1774, 4026, 4027, 4028, 4029} {
		fairyStackable[id] = true
	}
	for _, id := range []uint16{475, 476, 480, 482, 483, 484, 412, 413, 3200, 3201, 3202, 3203, 3204, 3314,
		3205, 3206, 3207, 3208, 3214, 3209, 2390, 2391, 2392, 2393, 2394, 2395, 2396, 2397, 2398, 2399,
		2400, 2401, 2402, 2403, 2404, 2405, 2406, 2407, 2408, 2409, 3027, 3028, 3029, 3030, 2410, 2411,
		2412, 2413, 4117, 4118, 4119, 4120, 4121, 2414, 2415, 2416, 2417, 2418, 2419, 415, 419, 420,
		4599, 4601, 4603, 4547, 4528, 1739, 4641, 4719, 4140, 4548, 4549, 4016, 4017, 4018, 4019, 2441,
		2442, 2443, 2444, 3140, 4132, 4133, 3288, 3289, 3290, 4134, 4135, 4136, 4137, 4026, 4027, 4028,
		4029, 4010, 4011, 1774, 4020, 777, 3173, 3182, 3252, 3253, 5110, 5111, 5112, 5113, 5114, 5115,
		5116, 5117, 5118, 5119, 5120, 5121, 5122, 5123, 5124, 5125, 5126, 5127, 600, 5128, 5129, 5130,
		5131, 5132, 5133, 489} {
		maxAmount[id] = 255
	}
}

// maxAmount is GetMaxAmountItem: stack size (0 = not stackable).
var maxAmount = map[uint16]int{}

// agroupItemFada is AgroupItemFada: if the item is in the fairy list, stack
// it onto the first matching slot. Reports whether it was in the list (the
// C++ returned that, even when the stack was full and nothing moved).
func (g *Game) agroupItemFada(u *User, it *wire.Item) bool {
	if !fairyStackable[it.Index] {
		return false
	}
	pl := &g.mobs[u.id].info.Player
	if slot := firstSlotOf(pl, it.Index); slot != -1 {
		tmp := *it
		if agroupItem(&tmp, &pl.Inventory[slot]) {
			g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		}
	}
	return true
}

// agroupItem is AgroupItem: merge src into dst up to the stack size.
func agroupItem(src, dst *wire.Item) bool {
	if src == dst || src.Index != dst.Index {
		return false
	}
	if src.Index == 4685 {
		dst.Index = 4641
		*src = wire.Item{}
		return true
	}
	maxN := maxAmount[src.Index]
	if maxN <= 0 {
		return false
	}
	for i := range 3 {
		if src.Index >= 2390 && src.Index <= 2419 && (src.Effect[i].Index == 210 || dst.Effect[i].Index == 210) {
			return false
		}
		if src.Effect[i].Index == 200 || dst.Effect[i].Index == 200 {
			return false
		}
	}
	amount := func(it *wire.Item) int {
		n := 0
		for i := range 3 {
			if it.Effect[i].Index == EF_AMOUNT {
				n += int(it.Effect[i].Value)
			}
		}
		return n
	}
	as, ad := amount(src), amount(dst)
	if as >= maxN || ad >= maxN {
		return false
	}
	as, ad = max(as, 1), max(ad, 1)
	if as+ad <= maxN {
		*src = wire.Item{}
		out := wire.Item{Index: dst.Index}
		for i := range 3 {
			if e := dst.Effect[i].Index; e != EF_AMOUNT && e != EF_UNIQUE {
				out.Effect[i] = dst.Effect[i]
			}
		}
		i := 0
		for ; i < 3; i++ {
			if out.Effect[i].Index == 0 {
				out.Effect[i] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint8(as + ad)}
				break
			}
		}
		if i == 3 {
			return false
		}
		*dst = out
		return true
	}
	as -= maxN - ad
	for i := range 3 {
		if dst.Effect[i].Index == EF_AMOUNT {
			dst.Effect[i].Value = uint8(maxN)
			break
		}
	}
	i := 0
	for ; i < 3; i++ {
		if src.Effect[i].Index == EF_AMOUNT {
			src.Effect[i].Value = uint8(as)
			break
		}
	}
	if i == 3 {
		for i = 0; i < 3; i++ {
			if src.Effect[i].Index == 59 {
				src.Effect[i] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint8(as)}
				break
			}
		}
	}
	return true
}
