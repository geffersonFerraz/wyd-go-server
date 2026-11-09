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
	// OpLock tells TMSRV to reload the account lock (the in-game lock
	// password and switches, TMSRV's Cache/LockAccount/<LOGIN>.bin) after the
	// panel removed it or changed its password. Nothing is stored in the
	// account; offline it is a no-op (the file is read on entering the game).
	OpLock = "lock"
)

const (
	MaxGold     = wire.MaxLimit
	MaxLevel    = 400
	MaxAccess   = 255
	StorageSize = 128 // MAX_CARGO
	EquipSize   = 16  // equipment slots the client shows (0 = face)
	InvSize     = 63  // 63 holds the PK points, not an item
)

var opCodes = map[string]int32{OpGold: 1, OpLevel: 2, OpItem: 3, OpAccess: 4, OpCoin: 5, OpLock: 6}

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
	for _, x := range v[1:] {
		if x < 0 || x > 255 {
			return what + " effect out of range"
		}
	}
	return ""
}

// CharScoped reports whether the edit touches one character only (gold,
// level, equipment or inventory) - TMSRV saves only the character in play,
// so the others can be edited in DBSRV's copy while the player is online.
func (e *Edit) CharScoped() bool {
	return e.Op == OpGold || e.Op == OpLevel || (e.Op == OpItem && e.Where != wire.SlotStorage)
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
		it.Effect[k] = wire.ItemEffect{Index: uint8(v[1+2*k]), Value: uint8(v[2+2*k])}
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
	c := &a.Mob[e.Char]
	if c.Player.Name[0] == 0 {
		return ResNoCharacter
	}
	pl := &c.Player
	switch e.Op {
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

// Packet is the 0xC11 TMSRV applies to a player in game.
func (e *Edit) Packet(user string, client uint16) wire.AdminEdit {
	p := wire.AdminEdit{Header: wire.NewHeader(wire.OpAdminEdit, client), Op: opCodes[e.Op],
		Char: int32(e.Char), Where: int32(e.Where), Slot: int32(e.Slot), Value: e.Value, Item: ItemOf(e.Item)}
	if e.Expect != nil {
		p.HasExpect, p.Expect = 1, ItemOf(e.Expect)
	}
	wire.PutCString(p.Username[:], user)
	return p
}

// FromPacket is the inverse of Packet.
func FromPacket(p *wire.AdminEdit) (Edit, error) {
	e := Edit{Char: int(p.Char), Where: int(p.Where), Slot: int(p.Slot), Value: p.Value, Item: Ints(p.Item)}
	for name, code := range opCodes {
		if code == p.Op {
			e.Op = name
		}
	}
	if p.HasExpect != 0 {
		e.Expect = Ints(p.Expect)
	}
	if msg := e.Validate(); msg != "" {
		return e, fmt.Errorf("admin edit: %s", msg)
	}
	return e, nil
}
