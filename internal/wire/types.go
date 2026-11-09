package wire

// Game structs from Source Servidor/Common/stBase.h, with gold, experience,
// HP, MP, attack and defense widened to 64 bits (see docs/x64-limites.md).
// Offsets in comments are MSVC x64 /Zp8 byte offsets; sizes are checked in
// sizes_test.go.

// Item mirrors STRUCT_ITEM (8 bytes).
type Item struct {
	Index  uint16
	Effect [3]ItemEffect
}

type ItemEffect struct {
	Index uint8
	Value uint8
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

// Status mirrors STRUCT_STATUS (80 bytes).
type Status struct {
	Level    int16 // 0
	_        [6]byte
	Defense  int64 // 8
	Attack   int64 // 16
	Merchant uint8 // 24 bitfield union: Merchant:4 | Direction:4
	Move     uint8 // 25 bitfield union: Speed:4 | ChaosRate:4
	_        [6]byte
	MaxHP    int64 // 32
	MaxMP    int64
	CurHP    int64
	CurMP    int64
	STR      uint16 // 64
	INT      uint16
	DEX      uint16
	CON      uint16
	Mastery  [4]uint16 // 72
}

// Mob mirrors STRUCT_MOB (912 bytes). The NPC files on disk keep the old
// 848-byte layout; see UnmarshalLegacyMob.
type Mob struct {
	Name            [16]byte // 0
	CapeInfo        int8     // 16
	Info            uint8    // 17 bitfield: Merchant:6 | CityID:2
	GuildIndex      uint16   // 18
	ClassInfo       uint8    // 20
	AffectInfo      uint8    // 21 bitfield
	QuestInfo       uint16   // 22 bitfield
	Gold            int64    // 24
	Exp             int64    // 32
	Last            Position // 40
	_               [4]byte
	BStatus         Status    // 48
	Status          Status    // 128
	Equip           [20]Item  // 208
	Inventory       [64]Item  // 368
	Learn           [2]uint32 // 880
	StatusPoint     uint16    // 888
	MasterPoint     uint16
	SkillPoint      uint16
	Critical        uint8 // 894
	SaveMana        uint8
	SkillBar1       [4]uint8 // 896
	GuildMemberType int8     // 900
	_               [1]byte
	MagicIncrement  uint16 // 902
	RegenHP         uint8
	RegenMP         uint8
	Resist          [4]uint8 // 906: Fogo, Gelo, Sagrado, Trovao
	_               [2]byte  // tail pad to 8
}

// SubInfo mirrors STRUCT_SUBINFO (416 bytes).
type SubInfo struct {
	Status    int32 // 0
	_         [4]byte
	SubStatus Status    // 8
	Equip     [2]Item   // 88
	SkillBar  [20]uint8 // 104
	_         [4]byte
	Exp       int64  // 128
	Learn     uint32 // 136
	SecLearn  uint32
	Affect    [32]Affect // 144
	Info      uint64     // 400 UNION_QUESTINFO
	Soul      uint8      // 408
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
	IsAccepted bool // 44
	_          [1]byte
	LastUpdate ItemDate // 46
	BattlePass ItemDate // 54
	_          [2]byte  // 64
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
	Items     [24]Item // 2
	_         [2]byte
	Type      int32 // 196
}

type Roleta struct {
	BauSlot int8
	BauID   int8
	DropMsg int8
}

// CharInfo mirrors STRUCT_CHARINFO (2112 bytes).
type CharInfo struct {
	Player           Mob        // 0
	Affects          [32]Affect // 912
	Info             uint64     // 1168 UNION_QUESTINFO
	HallEnter        uint32     // 1176
	Fame             uint32     // 1180
	PesaEnter        int8       // 1184
	Citizen          uint8
	Soul             uint8
	_                [5]byte
	Hold             int64     // 1192
	SkillBar         [16]uint8 // 1200
	MagicIncrement   uint32    // 1216
	MortalSlot       int32     // 1220
	Divina           ItemDate  // 1224
	Sephira          ItemDate
	Saude            ItemDate
	Escritura        ItemDate
	Revigorante      ItemDate
	Sub              SubInfo     // 1264
	AffectInfo       int32       // 1680
	Nightmare        [3]Position // 1684
	Counters         Counters    // 1696
	_                [4]byte
	Insignias01      uint64       // 1760
	Insignias02      uint64       // 1768
	DailyQuest       DailyQuest   // 1776
	Contribution     Contribution // 1840
	FilterDrop       FilterDrop   // 1892
	LastGuildKickOut ItemDate     // 2092
	RvRPoints        int32        // 2100
	Roleta           Roleta       // 2104
	_                [5]byte
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

// Account mirrors STRUCT_ACCOUNT (10584 bytes). Only the admin protocol ever
// put it on the wire; DBSRV uses it as its in-memory/session model.
type Account struct {
	Username     [16]byte     // 0
	Password     [36]byte     // 16
	Cash         uint32       // 52
	Honra        uint32       // 56
	Novato       uint32       // 60
	BanType      uint32       // 64
	Ban          ItemDate     // 68
	SecondPass   [16]byte     // 76
	AccessLevel  uint32       // 92
	Year         int32        // 96
	YearDay      int32        // 100
	StorageCoin  int64        // 104
	StorageItems [168]Item    // 112
	BlockPass    [16]byte     // 1456
	BlockDesc    [8]int8      // 1472: CanUnequip, CanTrade, CanBuy, CanSell, CanAutoTrade, CanUsePremium, CanCargo, Compound
	Mob          [4]CharInfo  // 1480
	Friends      [30][16]byte // 9928
	Insignias    uint64       // 10408
	TempKey      [52]byte     // 10416
	CharSlot     int16        // 10468
	Position     Position     // 10470
	_            [6]byte
	Unique       uint64  // 10480 bitfields: PresenteFB_01, PresenteEmail_01, Novato
	UniqueNovato bool    // 10488 NovatoUsed (non-bitfield member after the int64 unit)
	_            [7]byte //
	Daily        Daily   // 10496
	Water        Water   // 10508
	Divina       ItemDate
	Sephira      ItemDate
	NovatoDate   ItemDate // 10532
	_            [4]byte
	SingleGift   uint64 // 10544
	AlreadyReset bool   // 10552
	_            [7]byte
	LastPenalty  int64    // 10560 time_t
	PassBlock    [16]byte // 10568
}

// SealInfo mirrors STRUCT_SEALINFO (44 bytes).
type SealInfo struct {
	Status    int32
	Face      int16
	Level     int16
	STR       int16
	INT       int16
	DEX       int16
	CON       int16
	Evolution int16
	CapeID    int16
	Unk3      int16
	Unk4      int16
	Skills    [9]int16
	QuestInfo int16
}
