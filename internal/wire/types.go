package wire

// Game structs from Source Servidor/Common/stBase.h. Offsets in comments are
// MSVC x86 /Zp8 byte offsets; sizes are checked in sizes_test.go.

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

// Status mirrors STRUCT_STATUS (48 bytes).
type Status struct {
	Level    int16 // 0
	_        [2]byte
	Defense  int32 // 4
	Attack   int32 // 8
	Merchant uint8 // 12 bitfield union: Merchant:4 | Direction:4
	Move     uint8 // 13 bitfield union: Speed:4 | ChaosRate:4
	_        [2]byte
	MaxHP    int32 // 16
	MaxMP    int32
	CurHP    int32
	CurMP    int32
	STR      uint16 // 32
	INT      uint16
	DEX      uint16
	CON      uint16
	Mastery  [4]uint16 // 40
}

// Mob mirrors STRUCT_MOB (848 bytes; matches the size of DBSRV/npc/{TK,FM,BM,HT}).
type Mob struct {
	Name            [16]byte // 0
	CapeInfo        int8     // 16
	Info            uint8    // 17 bitfield: Merchant:6 | CityID:2
	GuildIndex      uint16   // 18
	ClassInfo       uint8    // 20
	AffectInfo      uint8    // 21 bitfield
	QuestInfo       uint16   // 22 bitfield
	Gold            int32    // 24
	_               [4]byte
	Exp             int64     // 32
	Last            Position  // 40
	BStatus         Status    // 44
	Status          Status    // 92
	Equip           [20]Item  // 140
	Inventory       [64]Item  // 300
	Learn           [2]uint32 // 812
	StatusPoint     uint16    // 820
	MasterPoint     uint16
	SkillPoint      uint16
	Critical        uint8 // 826
	SaveMana        uint8
	SkillBar1       [4]uint8 // 828
	GuildMemberType int8     // 832
	_               [1]byte
	MagicIncrement  uint16 // 834
	RegenHP         uint8
	RegenMP         uint8
	Resist          [4]uint8 // 838: Fogo, Gelo, Sagrado, Trovao
	_               [6]byte  // tail pad to 8 (Exp is 8-aligned)
}

// SubInfo mirrors STRUCT_SUBINFO (376 bytes).
type SubInfo struct {
	Status    int32     // 0
	SubStatus Status    // 4
	Equip     [2]Item   // 52
	SkillBar  [20]uint8 // 68
	Exp       int64     // 88
	Learn     uint32    // 96
	SecLearn  uint32
	Affect    [32]Affect // 104
	Info      uint64     // 360 UNION_QUESTINFO
	Soul      uint8      // 368
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

// CharInfo mirrors STRUCT_CHARINFO (2008 bytes).
type CharInfo struct {
	Player           Mob        // 0
	Affects          [32]Affect // 848
	Info             uint64     // 1104 UNION_QUESTINFO
	HallEnter        uint32     // 1112
	Fame             uint32     // 1116
	PesaEnter        int8       // 1120
	Citizen          uint8
	Soul             uint8
	_                [5]byte
	Hold             int64     // 1128
	SkillBar         [16]uint8 // 1136
	MagicIncrement   uint32    // 1152
	MortalSlot       int32     // 1156
	Divina           ItemDate  // 1160
	Sephira          ItemDate
	Saude            ItemDate
	Escritura        ItemDate
	Revigorante      ItemDate
	Sub              SubInfo     // 1200
	AffectInfo       int32       // 1576
	Nightmare        [3]Position // 1580
	Counters         Counters    // 1592
	_                [4]byte
	Insignias01      uint64       // 1656
	Insignias02      uint64       // 1664
	DailyQuest       DailyQuest   // 1672
	Contribution     Contribution // 1736
	FilterDrop       FilterDrop   // 1788
	LastGuildKickOut ItemDate     // 1988
	RvRPoints        int32        // 1996
	Roleta           Roleta       // 2000
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

// Account mirrors STRUCT_ACCOUNT (10168 bytes). Only the admin protocol ever
// put it on the wire; DBSRV uses it as its in-memory/session model.
type Account struct {
	Username     [16]byte  // 0
	Password     [36]byte  // 16
	Cash         uint32    // 52
	Honra        uint32    // 56
	Novato       uint32    // 60
	BanType      uint32    // 64
	Ban          ItemDate  // 68
	SecondPass   [16]byte  // 76
	AccessLevel  uint32    // 92
	Year         int32     // 96
	YearDay      int32     // 100
	StorageCoin  uint32    // 104
	StorageItems [168]Item // 108
	BlockPass    [16]byte  // 1452
	BlockDesc    [8]int8   // 1468: CanUnequip, CanTrade, CanBuy, CanSell, CanAutoTrade, CanUsePremium, CanCargo, Compound
	_            [4]byte
	Mob          [4]CharInfo  // 1480
	Friends      [30][16]byte // 9512
	Insignias    uint64       // 9992
	TempKey      [52]byte     // 10000
	CharSlot     int16        // 10052
	Position     Position     // 10054
	_            [6]byte
	Unique       uint64  // 10064 bitfields: PresenteFB_01, PresenteEmail_01, Novato
	UniqueNovato bool    // 10072 NovatoUsed (non-bitfield member after the int64 unit)
	_            [7]byte //
	Daily        Daily   // 10080
	Water        Water   // 10092
	Divina       ItemDate
	Sephira      ItemDate
	NovatoDate   ItemDate // 10116
	_            [4]byte
	SingleGift   uint64 // 10128
	AlreadyReset bool   // 10136
	_            [7]byte
	LastPenalty  int64    // 10144 time_t
	PassBlock    [16]byte // 10152
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
