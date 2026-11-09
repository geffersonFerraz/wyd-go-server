package wire

// Client <-> TMSRV packets (Common/Struct.h). Packets that TMSRV relays
// between the client and DBSRV reuse the DB structs: p20D = AccountLogin,
// p10A = CNFAccountLogin, p110/p112 = CharListPacket, p114 =
// CNFCharacterLogin, p20F = NewCharacter, p211 = DeleteCharacter,
// p213 = CharacterLogin, pFDE = NumericPassword.

// Client packet ids.
const (
	OpCliLogin       = 0x20D
	OpCliCreateChar  = 0x20F
	OpCliDeleteChar  = 0x211
	OpCliEnterWorld  = 0x213
	OpCliLogout      = 0x215
	OpCliChat        = 0x333
	OpCliCommand     = 0x334
	OpCliMoveStop    = 0x366
	OpCliMoveIllus   = 0x368
	OpCliRequestMob  = 0x369
	OpCliMotion      = 0x36A
	OpCliMove        = 0x36C
	OpCliMoveItem    = 0x376
	OpCliKeepAlive   = 0x3AE
	OpCliIgnored2CB  = 0x2CB
	OpCliNumericPass = 0xFDE

	OpSrvMessage     = 0x101
	OpSrvCharList    = 0x10A // first char list after login (with keys)
	OpSrvCharListNew = 0x110 // after create
	OpSrvCharListDel = 0x112 // after delete
	OpSrvEnterWorld  = 0x114
	OpSrvBackToList  = 0x116
	OpSrvCreateFail  = 0x11A
	OpSrvDeleteFail  = 0x11B
	OpSrvKickedOther = 0x11D
	OpSrvRemoveMob   = 0x165
	OpSrvItem        = 0x182
	OpSrvHpMode      = 0x292
	OpSrvScore       = 0x336
	OpSrvEtc         = 0x337
	OpSrvCreateMob   = 0x364
	OpSrvEquip       = 0x36B
	OpSrvLoginFail   = 0xF22
	OpSrvNumericBad  = 0xF23

	// System client ids used in server -> client headers.
	ClientIDSystem   = 0x7530
	ClientIDCharList = 0x7531
	ClientIDLogin    = 0x7532
)

// Phase B client packet ids.
const (
	OpCliOpenShop      = 0x27B
	OpCliTeleport      = 0x290
	OpCliBuyShop       = 0x379
	OpCliSellShop      = 0x37A
	OpCliGoldToInv     = 0x387
	OpCliGoldToStorage = 0x388
	OpCliRepurchase    = 0x3E8 // also the server reply
	OpCliTrade         = 0x383 // both ways
	OpCliTradeCancel   = 0x384 // both ways (signal)
	OpSrvTradeConfirm  = 0x386 // own confirm acknowledged
	OpSrvCarry         = 0x185 // whole inventory + gold
	OpCliAttack        = 0x367 // also relayed to viewers
	OpCliAttackOne     = 0x39D
	OpCliAttackTwo     = 0x39E
	OpSrvKilled        = 0x338
	OpCliResurrect     = 0x289
	OpCliUseItem       = 0x373
	OpCliPartyInvite   = 0x37F // also forwarded to the invited player
	OpCliPartyAccept   = 0x3AB
	OpCliPartyExit     = 0x37E // also the server's "left the party"
	OpSrvPartyAdd      = 0x37D
	OpSrvSetHpMp       = 0x181
	OpSrvHpChange      = 0x18A

	OpSrvShopList  = 0x17C
	OpSrvCargoCoin = 0x339
	OpSrvGold      = 0x3AF // pMsgSignal: inventory gold
)

// OpenShop mirrors p27B (40 bytes).
type OpenShop struct {
	Header   Header
	NpcID    uint16
	Warp     uint16
	FindName [24]byte
}

// ShopList mirrors p17C (236 bytes): the 27 items an NPC sells.
type ShopList struct {
	Header Header
	Unknow int32 // the NPC's merchant type
	Item   [27]Item
	Taxes  int32
}

// BuyShop mirrors p379 (24 bytes).
type BuyShop struct {
	Header   Header
	MobID    int16
	SellSlot int16 // index into the NPC's Inventory
	InvSlot  int16
	Unknown1 int16
	Unknown2 int32
}

// SellShop mirrors p37A (20 bytes).
type SellShop struct {
	Header   Header
	NpcID    int16
	Type     int16
	SellSlot int32
}

// Repurchase mirrors p3E8 (172 bytes): the last 10 items sold.
type Repurchase struct {
	Header Header
	Item   [10]RepurchaseItem
}

type RepurchaseItem struct {
	Index     int32
	Item      Item
	SellPrice int32
}

// AttackTarget is st_Target (16 bytes).
type AttackTarget struct {
	Index  int32
	_      [4]byte
	Damage int64 // client: -2 physical, -1 skill; server: damage dealt
}

// Attack mirrors p367 (288 bytes, 13 targets). p39D is the same header with
// one target (96 bytes); 0x39E carries two (112 bytes).
type Attack struct {
	Header         Header
	_              [4]byte
	Hold           int64    // 16
	ReqMP          int64    // 24
	CurrentExp     int64    // 32
	Unknow         int16    // 40
	AttackerPos    Position // 42
	TargetPos      Position // 46
	AttackerID     int16    // 50
	AttackCount    int16    // 52
	Motion         uint8    // 54
	SkillParm      uint8
	DoubleCritical uint8 // 56
	FlagLocal      uint8
	Rsv            int16
	_              [4]byte
	CurrentMP      int64 // 64
	SkillID        int16 // 72
	ReqMp          int16
	_              [4]byte
	Target         [13]AttackTarget // 80
}

// Killed mirrors p338 (40 bytes): a mob died.
type Killed struct {
	Header Header
	_      [4]byte
	Hold   int64 // 16
	Killed int16 // 24
	Killer int16
	Unknow int32
	Exp    int64 // 32
}

// SetHpMp mirrors p181 (48 bytes).
type SetHpMp struct {
	Header                     Header
	_                          [4]byte
	CurHP, CurMP, MaxHP, MaxMP int64 // 16
}

// HpChange mirrors p18A (32 bytes).
type HpChange struct {
	Header  Header
	_       [4]byte
	CurHP   int64 // 16
	Incress int64 // 24
}

// PartyInvite mirrors p37F (44 bytes).
type PartyInvite struct {
	Header   Header
	PartyID  uint16
	Level    uint16
	MaxHP    uint16
	CurHP    uint16
	LeaderID uint16
	Name     [16]byte
	Unk      uint8
	_        [1]byte
	TargetID uint16
	_        [2]byte // sizeof(p37F) = 44: PacketHeader aligns to 4
}

// PartyAccept mirrors p3AB (32 bytes).
type PartyAccept struct {
	Header   Header
	LeaderID uint16
	Name     [16]byte
	_        [2]byte // sizeof(p3AB) = 32
}

// PartyMember mirrors p37D (40 bytes): a line of the party window. HP is a
// percentage.
type PartyMember struct {
	Header   Header
	LeaderID uint16 // 0 = this line is the leader
	Level    uint16
	MaxHP    uint16
	CurHP    uint16
	PartyID  uint16 // the member's id
	Name     [16]byte
	ID       uint16
}

// PartyExit mirrors p37E (16 bytes).
type PartyExit struct {
	Header Header
	MobID  int16
	_      [2]byte // sizeof(p37E) = 16; the client reads an int (MSG_STANDARDPARM)
}

// Compor mirrors pCompor (84 bytes): the item window of the compounder
// NPCs (Anct 0x3A6, Lindy 0x2C3, Shany 0x2C4, Odin 0x2D2, Ehre 0x2D3,
// Alchemy 0x2E1, Aylin 0x3B5, Agatha 0x3BA, Tiny 0x3C0, 0x2FD1). Slot -1
// is an empty place.
type Compor struct {
	Header Header
	Items  [8]Item
	Slot   [8]int8
}

// Extraction mirrors p2D4 (20 bytes).
type Extraction struct {
	Header  Header
	Unknown uint16
	Confirm uint16
	SlotID  uint16
	Unknow2 uint16
}

// ChatColor mirrors MSG_CHATMESSAGE (144 bytes), sent as 0xD1D to everyone.
type ChatColor struct {
	Header  Header
	Message [128]byte
	Color   uint32
}

// Motion mirrors p36A (20 bytes): an emote, also SendEmotion.
type Motion struct {
	Header  Header
	Motion  int16
	Parm    int16
	NotUsed int32
}

// SkillBar mirrors p378 (32 bytes): the whole skill bar.
type SkillBar struct {
	Header    Header
	SkillBar1 [4]uint8
	SkillBar2 [16]uint8
}

// UseItem mirrors p373 (36 bytes).
type UseItem struct {
	Header  Header
	SrcType uint32
	SrcSlot uint32
	DstType uint32
	DstSlot uint32
	PosX    uint16
	PosY    uint16
	Warp    int32
}

// AffectList mirrors p3B9 (268 bytes): a player's affects.
type AffectList struct {
	Header Header
	Affect [32]Affect
}

// Trade mirrors p383 (168 bytes).
type Trade struct {
	Header   Header
	Item     [15]Item // 12
	Slot     [15]int8 // 132: inventory slot of each item, -1 = none
	Unknow   uint8
	_        [4]byte
	Gold     int64 // 152
	Confirm  uint8 // 160 (bool)
	_        [1]byte
	ClientID uint16 // 162: the other player
	_        [4]byte
}

// Carry mirrors p185 (536 bytes).
type Carry struct {
	Header Header
	Item   [64]Item
	_      [4]byte
	Gold   int64 // 528
}

// CargoCoin mirrors p339 (24 bytes): storage gold.
type CargoCoin struct {
	Header Header
	_      [4]byte
	Gold   int64 // 16
}

// Message128 mirrors TMSRV's p101 (140 bytes): a system message.
type Message128 struct {
	Header Header
	Msg    [128]byte
}

// Action mirrors p36C (52 bytes): movement (0x366 stop, 0x368 illusion,
// 0x36C walk) both ways.
type Action struct {
	Header    Header
	LastPos   Position // 12
	MoveType  uint32   // 16
	MoveSpeed uint32   // 20
	Command   [24]byte // 24: route, one direction digit per step
	Destiny   Position // 48
}

// Chat mirrors p333 (144 bytes).
type Chat struct {
	Header Header
	Type   int32
	Text   [128]byte
}

// Command mirrors p334 (284 bytes): slash commands.
type Command struct {
	Header   Header
	Command  [16]byte
	Value    [128]byte
	ValueNew [128]byte
}

// MoveItem mirrors p376 (20 bytes).
type MoveItem struct {
	Header  Header
	DstType uint8
	DstSlot uint8
	SrcType uint8
	SrcSlot uint8
	PosX    uint16
	PosY    uint16
}

// Slot types (SlotType in Struct.h).
const (
	SlotEquip   = 0
	SlotInv     = 1
	SlotStorage = 2
)

// ItemUpdate mirrors p182 (24 bytes): one item slot changed.
type ItemUpdate struct {
	Header  Header
	InvType int16
	InvSlot int16
	Item    Item
}

// Equipment mirrors p36B (72 bytes): what others see a character wearing.
type Equipment struct {
	Header   Header
	ItemEff  [20]uint16
	AnctCode [20]byte
}

// CreateMob mirrors p364 (280 bytes): a mob or player appears.
// Players use Name[12] followed by kill counters; mobs (index >= 1000)
// use the same 16 bytes as Name[16] (p364_Mob).
type CreateMob struct {
	Header          Header
	CurrentX        int16 // 12
	CurrentY        int16
	Index           int16           // 16
	Name            [12]byte        // 18
	ChaosPoints     uint8           // 30
	CurrentKill     uint8           // 31
	TotalKill       uint16          // 32
	ItemRefine      [20]uint16      // 34
	Affect          [32]AffectBrief // 74
	GuildIndex      uint16          // 138
	GuildMemberType int8            // 140
	_               [1]byte
	Unknow          uint16   // 142
	Status          Status   // 144
	SpawnType       uint16   // 224
	AnctCode        [20]byte // 226
	Tab             [26]byte // 246
	Unknow02        [4]byte  // 272
	Life            int32    // 276
}

// AffectBrief is the 2-byte affect entry in p364 (time first).
type AffectBrief struct {
	Time  uint8
	Index uint8
}

// UpdateScore mirrors MSG_UpdateScore (200 bytes), sent as 0x336.
type UpdateScore struct {
	Header       Header
	_            [4]byte
	Score        Status     // 16
	Critical     int8       // 96
	SaveMana     int8       // 97
	Affect       [32]uint16 // 98: index<<8 | time
	Guild        uint16     // 162
	GuildLevel   uint16     // 164
	Resist       [4]int8    // 166
	_            [6]byte
	ReqHP        int64  // 176
	ReqMP        int64  // 184
	Magician     uint16 // 192
	Rsv          uint16 // 194
	LearnedSkill int8   // 196
	_            [3]byte
}

// Etc mirrors p337 (56 bytes), sent as 0x337: gold, exp and points.
type Etc struct {
	Header       Header
	_            [4]byte
	Hold         int64     // 16
	Exp          int64     // 24
	LearnedSkill [2]uint32 // 32
	ScoreBonus   int16     // 40
	SpecialBonus int16
	SkillBonus   int16
	_            [2]byte
	Coin         int64 // 48
}
