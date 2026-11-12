package wire

import (
	"fmt"
	"math"
)

// The NPC and class template files (gamedata.mob_templates)
// were written by the C++ servers and keep the 32-bit layout of STRUCT_MOB.
// They are read through these structs; the admin panel writes them back with
// PatchLegacyMob.

type legacyItem struct {
	Index  uint16
	Effect [3]struct{ Index, Value uint8 }
}

func (l legacyItem) item() Item {
	it := Item{Index: l.Index}
	for i, e := range l.Effect {
		it.Effect[i] = ItemEffect{Index: e.Index, Value: uint16(e.Value)}
	}
	return it
}

func legacyItems(in []legacyItem, out []Item) {
	for i := range in {
		out[i] = in[i].item()
	}
}

// UnmarshalEquip decodes a stored [20]Item, including rows saved with the
// 8-byte item before the 16-bit effect values.
func UnmarshalEquip(b []byte, out *[20]Item) error {
	if len(b) == 20*8 {
		var l [20]legacyItem
		if err := Unmarshal(b, &l); err != nil {
			return err
		}
		legacyItems(l[:], out[:])
		return nil
	}
	return Unmarshal(b, out)
}

type legacyStatus struct {
	Level    int16
	_        [2]byte
	Defense  int32
	Attack   int32
	Merchant uint8
	Move     uint8
	_        [2]byte
	MaxHP    int32
	MaxMP    int32
	CurHP    int32
	CurMP    int32
	STR      uint16
	INT      uint16
	DEX      uint16
	CON      uint16
	Mastery  [4]uint16
}

func (s legacyStatus) status() Status {
	return Status{
		Level: s.Level, Defense: int64(s.Defense), Attack: int64(s.Attack),
		Merchant: s.Merchant, Move: s.Move,
		MaxHP: int64(s.MaxHP), MaxMP: int64(s.MaxMP), CurHP: int64(s.CurHP), CurMP: int64(s.CurMP),
		STR: int64(s.STR), INT: int64(s.INT), DEX: int64(s.DEX), CON: int64(s.CON), Mastery: s.Mastery,
	}
}

// legacyMob is the 848-byte STRUCT_MOB of the files.
type legacyMob struct {
	Name            [16]byte
	CapeInfo        int8
	Info            uint8
	GuildIndex      uint16
	ClassInfo       uint8
	AffectInfo      uint8
	QuestInfo       uint16
	Gold            int32
	_               [4]byte
	Exp             int64
	Last            Position
	BStatus         legacyStatus
	Status          legacyStatus
	Equip           [20]legacyItem
	Inventory       [64]legacyItem
	Learn           [2]uint32
	StatusPoint     uint16
	MasterPoint     uint16
	SkillPoint      uint16
	Critical        uint8
	SaveMana        uint8
	SkillBar1       [4]uint8
	GuildMemberType int8
	_               [1]byte
	MagicIncrement  uint16
	RegenHP         uint8
	RegenMP         uint8
	Resist          [4]uint8
	_               [6]byte
}

// UnmarshalLegacyMob decodes an NPC file. Like Unmarshal, a shorter b is
// zero-extended and a longer one is cut.
func UnmarshalLegacyMob(b []byte, m *Mob) error {
	var l legacyMob
	if err := Unmarshal(b, &l); err != nil {
		return err
	}
	*m = Mob{
		Name: l.Name, CapeInfo: l.CapeInfo, Info: l.Info, GuildIndex: l.GuildIndex,
		ClassInfo: l.ClassInfo, AffectInfo: l.AffectInfo, QuestInfo: l.QuestInfo,
		Gold: int64(l.Gold), Exp: l.Exp, Last: l.Last,
		BStatus: l.BStatus.status(), Status: l.Status.status(),
		Learn:       l.Learn,
		StatusPoint: l.StatusPoint, MasterPoint: l.MasterPoint, SkillPoint: l.SkillPoint,
		Critical: l.Critical, SaveMana: l.SaveMana, SkillBar1: l.SkillBar1,
		GuildMemberType: l.GuildMemberType, MagicIncrement: l.MagicIncrement,
		RegenHP: l.RegenHP, RegenMP: l.RegenMP, Resist: l.Resist,
	}
	legacyItems(l.Equip[:], m.Equip[:])
	legacyItems(l.Inventory[:], m.Inventory[:])
	return nil
}

// LegacyMobSize is sizeof(STRUCT_MOB) in the 32-bit layout.
const LegacyMobSize = 848

func legacyItemOf(it Item) (legacyItem, error) {
	l := legacyItem{Index: it.Index}
	for i, e := range it.Effect {
		if e.Value > math.MaxUint8 {
			return l, fmt.Errorf("item %d: effect value %d does not fit the template (0..255)", it.Index, e.Value)
		}
		l.Effect[i].Index, l.Effect[i].Value = e.Index, uint8(e.Value)
	}
	return l, nil
}

func legacyStatusOf(s Status) (legacyStatus, error) {
	for _, v := range []int64{s.Defense, s.Attack, s.MaxHP, s.MaxMP, s.CurHP, s.CurMP} {
		if v < math.MinInt32 || v > math.MaxInt32 {
			return legacyStatus{}, fmt.Errorf("status value %d does not fit the template (32 bits)", v)
		}
	}
	for _, v := range []int64{s.STR, s.INT, s.DEX, s.CON} {
		if v < 0 || v > math.MaxUint16 {
			return legacyStatus{}, fmt.Errorf("attribute %d does not fit the template (0..65535)", v)
		}
	}
	return legacyStatus{
		Level: s.Level, Defense: int32(s.Defense), Attack: int32(s.Attack), Merchant: s.Merchant, Move: s.Move,
		MaxHP: int32(s.MaxHP), MaxMP: int32(s.MaxMP), CurHP: int32(s.CurHP), CurMP: int32(s.CurMP),
		STR: uint16(s.STR), INT: uint16(s.INT), DEX: uint16(s.DEX), CON: uint16(s.CON), Mastery: s.Mastery,
	}, nil
}

// marshalLegacyMob encodes m in the 848-byte layout (padding zeroed).
func marshalLegacyMob(m *Mob) ([]byte, error) {
	if m.Gold < math.MinInt32 || m.Gold > math.MaxInt32 {
		return nil, fmt.Errorf("gold %d does not fit the template (32 bits)", m.Gold)
	}
	l := legacyMob{
		Name: m.Name, CapeInfo: m.CapeInfo, Info: m.Info, GuildIndex: m.GuildIndex,
		ClassInfo: m.ClassInfo, AffectInfo: m.AffectInfo, QuestInfo: m.QuestInfo,
		Gold: int32(m.Gold), Exp: m.Exp, Last: m.Last, Learn: m.Learn,
		StatusPoint: m.StatusPoint, MasterPoint: m.MasterPoint, SkillPoint: m.SkillPoint,
		Critical: m.Critical, SaveMana: m.SaveMana, SkillBar1: m.SkillBar1,
		GuildMemberType: m.GuildMemberType, MagicIncrement: m.MagicIncrement,
		RegenHP: m.RegenHP, RegenMP: m.RegenMP, Resist: m.Resist,
	}
	var err error
	if l.BStatus, err = legacyStatusOf(m.BStatus); err != nil {
		return nil, err
	}
	if l.Status, err = legacyStatusOf(m.Status); err != nil {
		return nil, err
	}
	for i, it := range m.Equip {
		if l.Equip[i], err = legacyItemOf(it); err != nil {
			return nil, err
		}
	}
	for i, it := range m.Inventory {
		if l.Inventory[i], err = legacyItemOf(it); err != nil {
			return nil, err
		}
	}
	return Marshal(&l)
}

// PatchLegacyMob returns the template image orig with the fields of m. Only
// the bytes of the fields that changed are touched, so padding and whatever
// else the image carries stay as they were. A short orig is zero-extended to
// the full struct. A value that does not fit the 32-bit layout is an error.
func PatchLegacyMob(orig []byte, m *Mob) ([]byte, error) {
	var was Mob
	if err := UnmarshalLegacyMob(orig, &was); err != nil {
		return nil, err
	}
	before, err := marshalLegacyMob(&was)
	if err != nil {
		return nil, err
	}
	after, err := marshalLegacyMob(m)
	if err != nil {
		return nil, err
	}
	out := make([]byte, max(len(orig), len(after)))
	copy(out, orig)
	for i := range after {
		if after[i] != before[i] {
			out[i] = after[i]
		}
	}
	return out, nil
}
