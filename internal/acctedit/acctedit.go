// Package acctedit is the staff edit of an account: a character's gold or
// level, an item of its equipment or inventory, the storage (cargo) items
// or gold, and the account's GM access level.
//
// The admin panel queues an Edit as an "edit" delivery; DBSRV applies it to
// the stored account when the player is offline (or playing another
// character), or forwards it to TMSRV as wire.AdminEdit (0xC11) when the
// player is in game, where TMSRV applies it to the live character.
package acctedit

import (
	"fmt"

	"dbsrv/internal/exptable"
	"dbsrv/internal/wire"
)

// Ops.
const (
	OpGold   = "gold"   // character's inventory gold = Value
	OpLevel  = "level"  // character's level = Value (as shown in game, 1-based)
	OpItem   = "item"   // Where/Slot = Item (Item id 0 removes it)
	OpAccess = "access" // account access level = Value (0 player, > 0 GM)
	OpCoin   = "coin"   // storage gold = Value
	OpMove   = "move"   // inventory Slot <-> equipment To (Expect, ExpectTo)
	// OpRemove empties every slot of Removes, or none of them.
	OpRemove = "remove"
	// OpLock tells TMSRV to reload the account lock (the in-game lock
	// password and switches, the account_locks row) after the
	// panel removed it or changed its password. Nothing is stored in the
	// account; offline it is a no-op (the file is read on entering the game).
	OpLock = "lock"
)

const (
	MaxGold      = wire.MaxLimit
	MaxEffect    = 65535 // an item effect value
	MaxRemove    = wire.AdminRemoveMax
	MaxLevel     = 400
	MaxAccess    = 255
	StorageSize  = 128 // MAX_CARGO
	EquipSize    = 16  // equipment slots the client shows (0 = face)
	MoveEquipEnd = 15  // the cape (15) never moves, as in the game
	InvSize      = 63  // 63 holds the PK points, not an item
)

var opCodes = map[string]int32{OpGold: 1, OpLevel: 2, OpItem: 3, OpAccess: 4, OpCoin: 5, OpLock: 6, OpMove: 7}

// Edit is one change. Item and Expect are [id, ef1, efv1, ef2, efv2, ef3,
// efv3]; Expect (optional) is the item the admin saw in that slot: when the
// slot holds something else by then, the edit is refused ("changed").
type Edit struct {
	Op     string `json:"op"`
	Char   int    `json:"char"`
	Value  int64  `json:"value"`
	Where  int    `json:"where"` // wire.SlotEquip, SlotInv or SlotStorage
	Slot   int    `json:"slot"`
	Item   []int  `json:"item"`
	Expect []int  `json:"expect"`
	// OpMove: the equipment slot and the item the admin saw in it.
	To       int   `json:"to,omitempty"`
	ExpectTo []int `json:"expect_to,omitempty"`
	// OpRemove.
	Removes []SlotRef `json:"removes,omitempty"`
}

// SlotRef is one slot and the item the admin saw in it.
type SlotRef struct {
	Where  int   `json:"where"`
	Slot   int   `json:"slot"`
	Expect []int `json:"expect"`
}

// Validate returns "" or what is wrong.
func (e *Edit) Validate() string {
	switch e.Op {
	case OpGold:
		if e.Value < 0 || e.Value > MaxGold {
			return "gold out of range"
		}
	case OpCoin:
		if e.Value < 0 || e.Value > MaxGold {
			return "storage gold out of range"
		}
	case OpLevel:
		if e.Value < 1 || e.Value > MaxLevel {
			return "level out of range"
		}
	case OpAccess:
		if e.Value < 0 || e.Value > MaxAccess {
			return "access out of range"
		}
		return ""
	case OpLock:
		return ""
	case OpMove:
		if e.Slot < 0 || e.Slot >= InvSize {
			return "inventory slot out of range"
		}
		if e.To < 1 || e.To >= MoveEquipEnd {
			return "equipment slot out of range"
		}
		if msg := checkItem(e.Expect, "expect"); msg != "" {
			return msg
		}
		if e.Expect[0] == 0 {
			return "nothing to move"
		}
		if msg := checkItem(e.ExpectTo, "expect_to"); msg != "" {
			return msg
		}
	case OpRemove:
		if msg := e.checkRemoves(); msg != "" {
			return msg
		}
		if e.Removes[0].Where == wire.SlotStorage {
			return ""
		}
	case OpItem:
		if msg := checkItem(e.Item, "item"); msg != "" {
			return msg
		}
		if e.Expect != nil {
			if msg := checkItem(e.Expect, "expect"); msg != "" {
				return msg
			}
		}
		switch e.Where {
		case wire.SlotEquip:
			if e.Slot < 1 || e.Slot >= EquipSize {
				return "equipment slot out of range"
			}
		case wire.SlotInv:
			if e.Slot < 0 || e.Slot >= InvSize {
				return "inventory slot out of range"
			}
		case wire.SlotStorage:
			if e.Slot < 0 || e.Slot >= StorageSize {
				return "storage slot out of range"
			}
			return ""
		default:
			return "unknown container"
		}
	default:
		return "unknown op"
	}
	if e.Op != OpCoin && (e.Char < 0 || e.Char > 3) {
		return "character slot out of range"
	}
	return ""
}

func checkItem(v []int, what string) string {
	if len(v) != 7 {
		return what + " needs 7 numbers"
	}
	if v[0] < 0 || v[0] > 6499 {
		return what + " id out of range"
	}
	for k := range 3 {
		if v[1+2*k] < 0 || v[1+2*k] > 255 || v[2+2*k] < 0 || v[2+2*k] > MaxEffect {
			return what + " effect out of range"
		}
	}
	return ""
}

// checkRemoves validates the slots of an OpRemove: all of the storage or all
// of the character, each once, each with the item the admin saw.
func (e *Edit) checkRemoves() string {
	if len(e.Removes) == 0 || len(e.Removes) > MaxRemove {
		return "removes out of range"
	}
	storage := e.Removes[0].Where == wire.SlotStorage
	seen := make(map[[2]int]bool, len(e.Removes))
	for _, r := range e.Removes {
		if (r.Where == wire.SlotStorage) != storage {
			return "storage and character slots in one removal"
		}
		var size, first int
		switch r.Where {
		case wire.SlotEquip:
			size, first = EquipSize, 1
		case wire.SlotInv:
			size = InvSize
		case wire.SlotStorage:
			size = StorageSize
		default:
			return "unknown container"
		}
		if r.Slot < first || r.Slot >= size {
			return "slot out of range"
		}
		key := [2]int{r.Where, r.Slot}
		if seen[key] {
			return "slot repeated"
		}
		seen[key] = true
		if msg := checkItem(r.Expect, "expect"); msg != "" {
			return msg
		}
		if r.Expect[0] == 0 {
			return "empty slot in removal"
		}
	}
	return ""
}

// CharScoped reports whether the edit touches one character only (gold,
// level, equipment or inventory) - TMSRV saves only the character in play,
// so the others can be edited in DBSRV's copy while the player is online.
func (e *Edit) CharScoped() bool {
	if e.Op == OpRemove {
		return len(e.Removes) > 0 && e.Removes[0].Where != wire.SlotStorage
	}
	return e.Op == OpGold || e.Op == OpLevel || e.Op == OpMove || (e.Op == OpItem && e.Where != wire.SlotStorage)
}

// ApplySlots does an OpMove or OpRemove on the item slots slot returns (nil
// for one that may not change). It checks every expected item first, so it
// changes everything or nothing.
func (e *Edit) ApplySlots(slot func(where, index int) *wire.Item) string {
	if e.Op == OpMove {
		inv, eq := slot(wire.SlotInv, e.Slot), slot(wire.SlotEquip, e.To)
		if inv == nil || eq == nil || *inv != ItemOf(e.Expect) || *eq != ItemOf(e.ExpectTo) {
			return ResChanged
		}
		*inv, *eq = *eq, *inv
		return ResOK
	}
	targets := make([]*wire.Item, len(e.Removes))
	for i, r := range e.Removes {
		targets[i] = slot(r.Where, r.Slot)
		if targets[i] == nil || *targets[i] != ItemOf(r.Expect) {
			return ResChanged
		}
	}
	for _, t := range targets {
		*t = wire.Item{}
	}
	return ResOK
}

// ItemOf converts [id, ef, efv, ...] to an item.
func ItemOf(v []int) wire.Item {
	var it wire.Item
	if len(v) != 7 {
		return it
	}
	it.Index = uint16(v[0])
	if it.Index == 0 {
		return wire.Item{}
	}
	for k := range 3 {
		it.Effect[k] = wire.ItemEffect{Index: uint8(v[1+2*k]), Value: uint16(v[2+2*k])}
	}
	return it
}

// Ints is the inverse of ItemOf.
func Ints(it wire.Item) []int {
	e := it.Effect
	return []int{int(it.Index), int(e[0].Index), int(e[0].Value), int(e[1].Index), int(e[1].Value), int(e[2].Index), int(e[2].Value)}
}

// LevelExp is the exp a character of that evolution has right at level
// (0-based, as stored): g_pNextLevel of the level below.
func LevelExp(evolution, level int) int64 {
	if level <= 0 || evolution < 0 || evolution >= len(exptable.Next) || level > len(exptable.Next[0]) {
		return 0
	}
	return exptable.Next[evolution][level-1]
}

// Results of Apply.
const (
	ResOK          = ""
	ResNoCharacter = "no_character"
	ResChanged     = "changed"
)

// Apply changes a stored account (the player not in game with that
// character). It returns ResOK or why nothing changed.
func (e *Edit) Apply(a *wire.Account) string {
	switch e.Op {
	case OpAccess:
		a.AccessLevel = uint32(e.Value)
		return ResOK
	case OpCoin:
		a.StorageCoin = e.Value
		return ResOK
	case OpLock:
		return ResOK
	}
	if e.Op == OpItem && e.Where == wire.SlotStorage {
		return e.putItem(&a.StorageItems[e.Slot])
	}
	if e.Op == OpRemove && !e.CharScoped() {
		return e.ApplySlots(func(_, i int) *wire.Item { return &a.StorageItems[i] })
	}
	c := &a.Mob[e.Char]
	if c.Player.Name[0] == 0 {
		return ResNoCharacter
	}
	pl := &c.Player
	switch e.Op {
	case OpMove, OpRemove:
		return e.ApplySlots(func(where, i int) *wire.Item {
			if where == wire.SlotEquip {
				return &pl.Equip[i]
			}
			return &pl.Inventory[i]
		})
	case OpGold:
		pl.Gold = e.Value
	case OpLevel:
		level := int16(e.Value - 1)
		pl.BStatus.Level, pl.Status.Level = level, level
		pl.Exp = LevelExp(int(pl.Equip[0].Effect[1].Value), int(level))
	case OpItem:
		if e.Where == wire.SlotEquip {
			return e.putItem(&pl.Equip[e.Slot])
		}
		return e.putItem(&pl.Inventory[e.Slot])
	}
	return ResOK
}

// putItem replaces *slot, checking Expect.
func (e *Edit) putItem(slot *wire.Item) string {
	if e.Expect != nil && *slot != ItemOf(e.Expect) {
		return ResChanged
	}
	*slot = ItemOf(e.Item)
	return ResOK
}

// Wire is the packet that makes TMSRV apply e to a player in game.
func (e *Edit) Wire(user string, client uint16) any {
	if e.Op == OpRemove {
		p := e.RemovePacket(user, client)
		return &p
	}
	p := e.Packet(user, client)
	return &p
}

// RemovePacket is the 0xC15 of an OpRemove.
func (e *Edit) RemovePacket(user string, client uint16) wire.AdminRemove {
	p := wire.AdminRemove{Header: wire.NewHeader(wire.OpAdminRemove, client), Char: int32(e.Char), Count: int32(len(e.Removes))}
	for i, r := range e.Removes {
		p.Slots[i] = wire.AdminRemoveSlot{Where: uint16(r.Where), Slot: uint16(r.Slot), Expect: ItemOf(r.Expect)}
	}
	wire.PutCString(p.Username[:], user)
	return p
}

// FromRemovePacket is the inverse of RemovePacket.
func FromRemovePacket(p *wire.AdminRemove) (Edit, error) {
	e := Edit{Op: OpRemove, Char: int(p.Char)}
	if p.Count < 1 || p.Count > MaxRemove {
		return e, fmt.Errorf("admin remove: %d slots", p.Count)
	}
	for _, s := range p.Slots[:p.Count] {
		e.Removes = append(e.Removes, SlotRef{Where: int(s.Where), Slot: int(s.Slot), Expect: Ints(s.Expect)})
	}
	if msg := e.Validate(); msg != "" {
		return e, fmt.Errorf("admin remove: %s", msg)
	}
	return e, nil
}

// Packet is the 0xC11 TMSRV applies to a player in game.
func (e *Edit) Packet(user string, client uint16) wire.AdminEdit {
	p := wire.AdminEdit{Header: wire.NewHeader(wire.OpAdminEdit, client), Op: opCodes[e.Op],
		Char: int32(e.Char), Where: int32(e.Where), Slot: int32(e.Slot), To: int32(e.To), Value: e.Value, Item: ItemOf(e.Item)}
	if e.Expect != nil {
		p.HasExpect, p.Expect, p.ExpectTo = 1, ItemOf(e.Expect), ItemOf(e.ExpectTo)
	}
	wire.PutCString(p.Username[:], user)
	return p
}

// FromPacket is the inverse of Packet.
func FromPacket(p *wire.AdminEdit) (Edit, error) {
	e := Edit{Char: int(p.Char), Where: int(p.Where), Slot: int(p.Slot), To: int(p.To), Value: p.Value, Item: Ints(p.Item)}
	for name, code := range opCodes {
		if code == p.Op {
			e.Op = name
		}
	}
	if p.HasExpect != 0 {
		e.Expect = Ints(p.Expect)
		if e.Op == OpMove {
			e.ExpectTo = Ints(p.ExpectTo)
		}
	}
	if msg := e.Validate(); msg != "" {
		return e, fmt.Errorf("admin edit: %s", msg)
	}
	return e, nil
}
