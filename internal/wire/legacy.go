package wire

// The NPC and class template files (gamedata.mob_templates)
// were written by the C++ servers and keep the 32-bit layout of STRUCT_MOB.
// They are read through these structs and never written back.

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
