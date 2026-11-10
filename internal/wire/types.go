package wire

// Game structs from Source Servidor/Common/stBase.h, with gold, experience,
// HP, MP, attack and defense widened to 64 bits (see docs/x64-limites.md).
// Offsets in comments are MSVC x64 /Zp8 byte offsets; sizes are checked in
// sizes_test.go.

// Item mirrors STRUCT_ITEM (14 bytes, 2-aligned).
type Item struct {
	Index  uint16
	Effect [3]ItemEffect
}

// ItemEffect mirrors STRUCT_BONUSEFFECT (4 bytes): the value is 16-bit.
type ItemEffect struct {
	Index uint8
	_     uint8
	Value uint16
}

// Position mirrors STRUCT_POSITION (4 bytes).
type Position struct {
	X, Y uint16
}

// Affect mirrors STRUCT_AFFECT (8 bytes).
type Affect struct {
	Index  uint8
	Master uint8
	Value  uint16
	Time   uint32
}

// ItemDate mirrors STRUCT_ITEMDATE (8 bytes: 7 + 1 tail pad).
type ItemDate struct {
	Dia     uint8
	Mes     uint8
	Ano     uint16
	Hora    uint8
	Minuto  uint8
	Segundo uint8
	_       [1]byte
}

// Status mirrors STRUCT_STATUS (104 bytes).
type Status struct {
	Level    int16
	_        [6]byte
	Defense  int64
	Attack   int64
	Merchant uint8 // bitfield union: Merchant:4 | Direction:4
	Move     uint8 // bitfield union: Speed:4 | ChaosRate:4
	_        [6]byte
	MaxHP    int64
	MaxMP    int64
	CurHP    int64
	CurMP    int64
	STR      int64
	INT      int64
	DEX      int64
	CON      int64
	Mastery  [4]uint16
}

// Mob mirrors STRUCT_MOB (1464 bytes). The NPC files on disk keep the old
// 848-byte layout; see UnmarshalLegacyMob.
type Mob struct {
	Name            [16]byte
	CapeInfo        int8
	Info            uint8 // bitfield: Merchant:6 | CityID:2
	GuildIndex      uint16
	ClassInfo       uint8
	AffectInfo      uint8  // bitfield
	QuestInfo       uint16 // bitfield
	Gold            int64
	Exp             int64
	Last            Position
	_               [4]byte
	BStatus         Status
	Status          Status
	Equip           [20]Item
	Inventory       [64]Item
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
	Resist          [4]uint8 // Fogo, Gelo, Sagrado, Trovao
	_               [2]byte  // tail pad to 8
}

// SubInfo mirrors STRUCT_SUBINFO (448 bytes).
type SubInfo struct {
	Status    int32
	_         [4]byte
	SubStatus Status
	Equip     [2]Item
	SkillBar  [20]uint8
	Exp       int64
	Learn     uint32
	SecLearn  uint32
	Affect    [32]Affect
	Info      uint64 // UNION_QUESTINFO
	Soul      uint8
	_         [7]byte
}

type Counters struct {
	PvMMob   int32
	PvMDeath int32
	PvMBoss  int32
	PvPKill  int32
	PvPDeath int32
	Aux      [10]int32
}

type DailyQuest struct {
	QuestID    int32
	MobCount   [5]uint32
	ItemCount  [5]uint32
	IsAccepted bool
	_          [1]byte
	LastUpdate ItemDate
	BattlePass ItemDate
	_          [2]byte
}

type Contribution struct {
	DayOfYear int32
	Count     [10]int32
	Points    int32
	Limit     int32
}

type FilterDrop struct {
	IsEnabled bool
	_         [1]byte
	Items     [24]Item
	_         [2]byte
	Type      int32
}

type Roleta struct {
	BauSlot int8
	BauID   int8
	DropMsg int8
}

// CharInfo mirrors STRUCT_CHARINFO (2840 bytes).
type CharInfo struct {
	Player           Mob
	Affects          [32]Affect
	Info             uint64 // UNION_QUESTINFO
	HallEnter        uint32
	Fame             uint32
	PesaEnter        int8
	Citizen          uint8
	Soul             uint8
	_                [1]byte
	StatusBonus      int32 // Go only: points the admin panel added to the level's status points
	Hold             int64
	SkillBar         [16]uint8
	MagicIncrement   uint32
	MortalSlot       int32
	Divina           ItemDate
	Sephira          ItemDate
	Saude            ItemDate
	Escritura        ItemDate
	Revigorante      ItemDate
	Sub              SubInfo
	AffectInfo       int32
	Nightmare        [3]Position
	Counters         Counters
	SkillBonus       int32 // Go only: same, skill points
	Insignias01      uint64
	Insignias02      uint64
	DailyQuest       DailyQuest
	Contribution     Contribution
	FilterDrop       FilterDrop
	LastGuildKickOut ItemDate
	RvRPoints        int32
	Roleta           Roleta
	_                [1]byte
	MasterBonus      int32 // Go only: same, mastery points
}

type Daily struct {
	Received [7]bool
	_        [1]byte
	WeekYear int32
}

type Water struct {
	Day   int32
	Total int32
}

// Account mirrors STRUCT_ACCOUNT (14504 bytes). Only the admin protocol ever
// put it on the wire; DBSRV uses it as its in-memory/session model.
type Account struct {
	Username     [16]byte
	Password     [36]byte
	Cash         uint32
	Honra        uint32
	Novato       uint32
	BanType      uint32
	Ban          ItemDate
	SecondPass   [16]byte
	AccessLevel  uint32
	Year         int32
	YearDay      int32
	StorageCoin  int64
	StorageItems [168]Item
	BlockPass    [16]byte
	BlockDesc    [8]int8 // CanUnequip, CanTrade, CanBuy, CanSell, CanAutoTrade, CanUsePremium, CanCargo, Compound
	Mob          [4]CharInfo
	Friends      [30][16]byte
	Insignias    uint64
	TempKey      [52]byte
	CharSlot     int16
	Position     Position
	_            [6]byte
	Unique       uint64  // bitfields: PresenteFB_01, PresenteEmail_01, Novato
	UniqueNovato bool    // NovatoUsed (non-bitfield member after the int64 unit)
	_            [7]byte //
	Daily        Daily
	Water        Water
	Divina       ItemDate
	Sephira      ItemDate
	NovatoDate   ItemDate
	_            [4]byte
	SingleGift   uint64
	AlreadyReset bool
	_            [7]byte
	LastPenalty  int64 // time_t
	PassBlock    [16]byte
}

// SealInfo mirrors STRUCT_SEALINFO (52 bytes).
type SealInfo struct {
	Status    int32
	Face      int16
	Level     int16
	STR       int32
	INT       int32
	DEX       int32
	CON       int32
	Evolution int16
	CapeID    int16
	Unk3      int16
	Unk4      int16
	Skills    [9]int16
	QuestInfo int16
}
