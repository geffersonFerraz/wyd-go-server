package wire

// Packet structs. Every one starts with Header; sizes and key offsets are
// checked in sizes_test.go. Where TMSRV's copy (Common/Struct.h) differs
// from DBSRV's (Dataserver/Basedef.h) we mirror DBSRV's, because that is the
// byte stream TMSRV receives today, and note the TMSRV size.

// Header mirrors PacketHeader (12 bytes). Size, Key, CheckSum and TimeStamp
// are filled in by protocol.Conn when sending.
type Header struct {
	Size      uint16
	Key       uint8
	CheckSum  uint8
	PacketID  uint16
	ClientID  uint16
	TimeStamp uint32
}

// NewHeader builds a header for an outgoing packet.
func NewHeader(packetID, clientID uint16) Header {
	return Header{PacketID: packetID, ClientID: clientID}
}

// Signal is a header-only packet (SendDBSignal).
type Signal struct {
	Header Header
}

// Parm1, Parm2, Parm3 mirror MSG_STANDARDPARM1/2/3.
type Parm1 struct {
	Header Header
	Parm1  int32
}

type Parm2 struct {
	Header Header
	Parm1  int32
	Parm2  int32
}

type Parm3 struct {
	Header Header
	Parm1  int32
	Parm2  int32
	Parm3  int32
}

// AccountLogin mirrors p20D (120 bytes), 0x803 in and 0x52A out.
type AccountLogin struct {
	Header   Header
	Password [12]byte
	Login    [16]byte
	Unknow   [52]byte // TempKey for server change: "server enc0..enc7 slot"
	CliVer   uint32
	Unknow84 uint32 // nonzero = kick previous session
	Mac      [8]byte
	Keys     [12]byte
}

// CharList mirrors st_CharList (1592 bytes).
type CharList struct {
	PositionX [4]int16
	PositionY [4]int16
	Name      [4][16]byte
	Status    [4]Status
	Equip     [4][20]Item
	GuildID   [4]uint16
	Gold      [4]int64
	Exp       [4]int64
}

// CNFAccountLogin mirrors p416 (4008 bytes), sent as 0x416.
type CNFAccountLogin struct {
	Header      Header
	Keys        [16]byte
	Unkw        int32
	CharList    CharList
	Storage     [168]Item
	GoldStorage int64
	UserName    [16]byte
	Unknow      [8]byte
}

// CharListPacket mirrors p418 (1608 bytes), sent as 0x418 / 0x419.
type CharListPacket struct {
	Header   Header
	_        [4]byte
	CharList CharList
}

// CharacterLogin mirrors p213 (32 bytes), 0x804.
type CharacterLogin struct {
	Header    Header
	CharIndex int32
	Zero      [16]byte
}

// CNFCharacterLogin mirrors p114 (1728 bytes), sent as 0x417.
type CNFCharacterLogin struct {
	Header      Header
	WorldPos    Position
	Mob         Mob
	CurrentKill uint16
	TotalKill   uint16
	SlotIndex   int16
	ClientIndex int16
	Weather     int16
	SkillBar2   [16]byte
	_           [2]byte
	Affect      [32]Affect
	_           [4]byte
	Hold        int64
}

// CNFCharacterLoginExtra mirrors p820 (2848 bytes), sent as 0x820.
type CNFCharacterLoginExtra struct {
	Header    Header
	_         [4]byte
	Mob       CharInfo
	Cash      int32
	Honra     int32
	Novato    int32
	Pass      [16]byte
	Blocked   int32
	PassBlock [16]byte
	TypeBlock [10]byte
	_         [6]byte
}

// AdditionalAccountInfo mirrors p415 (128 bytes; TMSRV's copy is 120, it has
// no TypeBlock), sent as 0x415.
type AdditionalAccountInfo struct {
	Header     Header
	Access     int32
	Unique     int64
	Daily      Daily
	Water      Water
	Divina     ItemDate
	Sephira    ItemDate
	_          [4]byte
	SingleGift uint64
	Ban        ItemDate
	BanType    uint32
	IsBanned   int32
	Cash       int32
	Honra      int32
	Novato     int32
	PassBlock  [16]byte
	TypeBlock  [10]byte
	_          [2]byte
}

// NewCharacter mirrors p20F (36 bytes), 0x802.
type NewCharacter struct {
	Header  Header
	SlotID  uint32
	Nick    [16]byte
	ClassID uint32
}

// NewArch mirrors MSG_DBNewArch (44 bytes), 0x80B.
type NewArch struct {
	Header     Header
	PosID      int32
	Name       [16]byte
	ClassID    int32
	ClassInfo  int32
	MortalSlot int32
}

// DeleteCharacter mirrors p211 (44 bytes), 0x809.
type DeleteCharacter struct {
	Header    Header
	SlotIndex int32
	Name      [16]byte
	Pwd       [12]byte
}

// SaveCharacter mirrors STRUCT_SAVECHARACTER (5816 bytes; TMSRV's copy is
// 5800, it has no TypeBlock), 0x806 / 0x807.
type SaveCharacter struct {
	Header     Header
	CharSlot   uint32
	Mob        CharInfo
	Storage    [168]Item
	Coin       int64
	SkillBar   [20]byte // [0:4] -> SkillBar1, [4:20] -> CharInfo.SkillBar
	User       [16]byte
	Arg2       uint32 // nonzero = also export
	Friends    [30][16]byte
	Pass       [16]byte
	Blocked    int32
	Cash       int32
	Honra      int32
	Novato     int32
	Insignia   int64
	BanType    uint32
	Ban        ItemDate
	Position   Position
	Slot       int16
	_          [6]byte
	Unique     int64
	Daily      Daily
	Water      Water
	Divina     ItemDate
	Sephira    ItemDate
	_          [4]byte
	SingleGift uint64
	PassBlock  [16]byte
	TypeBlock  [10]byte
	_          [6]byte
}

// SavingQuit mirrors MSG_DBSavingQuit (32 bytes), sent as 0x40A.
type SavingQuit struct {
	Header      Header
	AccountName [16]byte
	Mode        int32
}

// CNFRemoveServer mirrors the client's MSG_CNFRemoveServer (80 bytes),
// 0x52A to the client: reconnect to channel N of TID "*N key" and log in
// with the key.
type CNFRemoveServer struct {
	Header      Header
	AccountName [16]byte
	TID         [52]byte
}

// ServerChange mirrors MSG_DBServerChange (20 bytes), 0x814.
type ServerChange struct {
	Header      Header
	NewServerID int32
	Slot        int32
}

// NumericPassword mirrors pFDE (32 bytes), 0xFDE both ways.
type NumericPassword struct {
	Header        Header
	Num           [16]byte
	RequestChange int32
}

// CreateSubCele mirrors p830 (52 bytes), 0x830.
type CreateSubCele struct {
	Header    Header
	Level     uint32
	ClassInfo uint32
	Learn     uint32
	Mantle    uint32
	Face      uint32
	CharPos   uint32
	Name      [16]byte
}

// GuildZoneReport mirrors MSG_GuildZoneReport (32 bytes), 0x80D.
type GuildZoneReport struct {
	Header Header
	Guild  [5]int32
}

// ChargedGuildList mirrors MSG_ChargedGuildList (212 bytes), sent as 0x428.
// The C++ server sent 0x708 (1800) bytes here, reading past the struct.
type ChargedGuildList struct {
	Header           Header
	ChargedGuildList [10][5]int32
}

// CreateGuild mirrors MSG_CREATEGUILD (40 bytes), 0x310.
type CreateGuild struct {
	Header    Header
	GuildName [16]byte
	Kingdom   int32
	Citizen   int32
	GuildID   int32
}

// AddGuild mirrors MSG_ADDGUILD (24 bytes), 0x313. Type 0 fame, 1 citizen, 2 wins.
type AddGuild struct {
	Header     Header
	Type       int32
	Value      int32
	GuildIndex int32
}

// AddSub mirrors MSG_ADDSUB (36 bytes), 0x315.
type AddSub struct {
	Header     Header
	GuildIndex int16
	SubIndex   int16
	Name       [16]byte
	Status     int16
	_          [2]byte
}

// GuildRelation mirrors pE12 (20 bytes), 0xE12 / 0xE0E.
type GuildRelation struct {
	Header      Header
	GuildIndex1 uint32
	GuildIndex2 uint32
}

// SecurityAlert (Go only, 0xC13, 132 bytes): TMSRV reports something the
// staff should look at (e.g. a WPE warning from the client protection);
// DBSRV stores it for the admin panel.
type SecurityAlert struct {
	Header   Header
	Username [16]byte
	CharName [16]byte
	IP       [16]byte
	Kind     int32 // AlertWPE, ...
	Channel  int32
	Detail   [64]byte // Windows-1252, like the rest of the game text
}

// RankUpdate (Go only, 0xC14): one character's ranking row and profile.
// DBSRV stores it, so every channel shows the same ranking.
type RankUpdate struct {
	Header    Header
	Name      [16]byte
	Level     int32 // as shown: Status.Level + 1
	Class     int32
	Evolution int32
	Guild     int32
	Patent    int32
	Kingdom   int32
	Frags     int32
	Power     int32
	Fame      int32
	Citizen   int32
	PvPKill   int32
	PvMKill   int32
	Mantle    int32
	Exp       int64
	Equip     [20]Item
}

// HpMode is 0x292 (p292), corrects the client's HP and state.
type HpMode struct {
	Header Header
	_      [4]byte
	CurHP  int64
	Status int16
	_      [6]byte
}

// RankRow is SRankTable, one line of the ranking window.
type RankRow struct {
	Player      [16]byte
	Level       int32
	Class       int32
	Evolution   int32
	Guild       int32
	Patent      int32
	Kingdom     int32
	Frags       int32
	GuildName   [20]byte
	Exp         int64
	Power       int32
	CurrentPage int32
	TotalPage   int32
	_           [4]byte
}

// RankList is p528 (0x528 level, 0x529 PvP, 0x530 power).
type RankList struct {
	Header     Header
	SceneIndex int32
	Update     uint8
	_          [7]byte
	Rank       [20]RankRow
}

// CharProfile is p487, the character profile window.
type CharProfile struct {
	Header     Header
	Name       [16]byte
	Evolution  int32
	Level      int32
	GuildName  [20]byte
	GuildIndex int32
	Citizen    int32
	Fame       int32
	PvPKill    int32
	PvMKill    int32
	Mantle     int32
	Equip      [20]Item
}

// DropMob is one monster of the drop window.
type DropMob struct {
	Name  [64]byte
	X, Y  int16
	_     [4]byte
	Exp   int64
	Gold  int64
	Carry [64]Item
}

// DropList is p2568 (0xA08): the drop window.
type DropList struct {
	Header Header
	Amount int32
	Drop   [50]DropMob
}

// FilterDropMsg is MSG_pFilterDrop as the client has it (24 items; the
// C++ server declared 64, which only moved Type).
type FilterDropMsg struct {
	Header    Header
	Code      int32
	IsEnabled uint8
	_         uint8
	Items     [24]Item
	_         [2]byte
	Type      int32
}

// RollItem is MSG_RollItem (0xF06): the 20 prizes the wheel shows.
type RollItem struct {
	Header Header
	Level  int16
	Slot   int16
	Evol   int16
	Item   [20]Item
	Rare   [20]int16
	_      [2]byte
}

// Recaptcha is MSG_pRecaptcha (5269), the same both ways.
type Recaptcha struct {
	Header Header
	Parm   int32
	Code   int32
	Pass   [5]byte
	_      [3]byte
}

// EssencePacket is MSG_ESSENCEPACKET (0x473).
type EssencePacket struct {
	Header Header
	Slot   int32
	Amount int32
}

// CashShopRequest is MSG_ReqPremiumStore (0xAF3).
type CashShopRequest struct {
	Header Header
	Aba    int16
	Page   int16
}

// CashShopInfo is MSG_CASHSHOPINFO (0xAF1 to the client).
type CashShopInfo struct {
	Header     Header
	Name       [32]byte
	Item       [65]Item
	Price      [65]int16
	Amount     [65]int16
	Promo      [65]int16
	SlotFree   [65]int16
	Aba        int16
	Page       int16
	LastUpdate ItemDate
	_          [2]byte
}

// CashShopBuy is MSG_BuyPremiumStore (0xF03).
type CashShopBuy struct {
	Header    Header
	Slot      int16
	Price     int16
	Item      Item
	Quant     int16
	Aba       int32
	Page      int32
	Categoria [128]byte
	Nickname  [16]byte
}

// HonorShopList is p17CF: the Honor Market window.
type HonorShopList struct {
	Header Header
	NPC    int32
	Item   [27]Item
	_      [2]byte
	Taxes  int32
}

// SignalNick is pMsgSignalChar: a request naming a character.
type SignalNick struct {
	Header Header
	Nick   [16]byte
}

// Security alert kinds.
const (
	AlertWPE = 1 // the client's protection saw WPE (packet editor)
)

// ItemDelivery mirrors pCOF (48 bytes), sent as 0xC0F.
type ItemDelivery struct {
	Header   Header
	Val      int32
	Username [16]byte
	Item     Item
	_        [2]byte
}

// CashDelivery mirrors pCOE (72 bytes), sent as 0xC0E.
type CashDelivery struct {
	Header    Header
	Val       int32
	Username  [16]byte
	Cash      int32
	Honra     int32
	Novato    int32
	PassBlock [16]byte
	TypeBlock [10]byte
	_         [2]byte
}

// BanNotify mirrors pC10 (40 bytes), sent as 0xC10.
// AdminEdit is a staff edit of a player in game (Go only, 0xC11, 112 bytes;
// see package acctedit).
type AdminEdit struct {
	Header    Header
	Username  [16]byte
	Op        int32
	Char      int32
	Where     int32
	Slot      int32
	_         [4]byte
	Value     int64
	Item      Item
	Expect    Item
	ExpectTo  Item
	_         [2]byte
	HasExpect int32
	To        int32 // equipment slot of a move
	_         [4]byte
}

// AdminRemoveMax is the most slots one AdminRemove can clear (a whole storage).
const AdminRemoveMax = 128

// AdminRemoveSlot is one slot of an AdminRemove.
type AdminRemoveSlot struct {
	Where  uint16
	Slot   uint16
	Expect Item
}

// AdminRemove (Go only, 0xC15): a staff removal of several items of a player
// in game, applied all together or not at all.
type AdminRemove struct {
	Header   Header
	Username [16]byte
	Char     int32
	Count    int32
	Slots    [AdminRemoveMax]AdminRemoveSlot
}

type BanNotify struct {
	Header   Header
	Username [16]byte
	BanType  int32
	Ban      ItemDate
}

// Notice96 covers p101 and _MSG_SEND_SERVER_NOTICE (108 bytes).
type Notice96 struct {
	Header Header
	Text   [96]byte
}

// MessagePanel mirrors MSG_MessagePanel (108 bytes), sent as 0x101.
type MessagePanel struct {
	Header Header
	ID     int32
	Text   [92]byte
}

// KefraDeath mirrors _MSG_NOTIFY_KEFRA_DEATH (28 bytes), 0x909.
type KefraDeath struct {
	Header Header
	Name   [16]byte
}

// FirstKefra mirrors _MSG_FIRST_KEFRA_NOTIFY (16 bytes), sent as 0x90A.
type FirstKefra struct {
	Header  Header
	Channel int32
}

// StoreSlot mirrors pMsgSignal2 / pMsgSignal3 used by the donate store.
type StoreSlot struct {
	Header Header
	Store  int32
	Slot   int32
}

type StoreSetAvaible struct {
	Header  Header
	Store   int32
	Slot    int32
	Avaible int32
}

// SignalValue mirrors pMsgSignal (16 bytes).
type SignalValue struct {
	Header Header
	Value  int32
}

// SignalValue64 mirrors MSG_STANDARDPARM64 (24 bytes): the signals that carry
// gold (0x3AF, 0x387, 0x388).
type SignalValue64 struct {
	Header Header
	_      [4]byte
	Value  int64
}

// SealInfoReply mirrors MSG_SEALINFO (56 bytes).
type SealInfoReply struct {
	Header Header
	Info   SealInfo
}

// PutOutSeal mirrors MSG_PUTOUTSEAL (52 bytes), 3132.
type PutOutSeal struct {
	Header  Header
	SrcType int32
	SrcSlot int32
	DstType int32
	DstSlot int32
	GridX   uint16
	GridY   uint16
	WarpID  uint16
	MobName [16]byte
	_       [2]byte
}

// MSVCString is the in-memory image of an x86 Release std::string
// (24 bytes: 16-byte SSO buffer/pointer, size, capacity). DBSRV memcpy'd
// STRUCT_GUILDINFO (which holds std::string) onto the wire for 0x766, so
// TMSRV only ever sees valid text for names that fit the 15-byte SSO buffer.
type MSVCString struct {
	Buf      [16]byte
	Size     uint32
	Capacity uint32
}

// NewMSVCString builds the SSO image. Longer strings are truncated to 15
// bytes; the C++ server would have sent a dangling heap pointer instead.
func NewMSVCString(s string) MSVCString {
	var m MSVCString
	if len(s) > 15 {
		s = s[:15]
	}
	copy(m.Buf[:], s)
	m.Size = uint32(len(s))
	m.Capacity = 15
	return m
}

func (m MSVCString) String() string { return string(m.Buf[:min(int(m.Size), 15)]) }

// GuildInfo mirrors STRUCT_GUILDINFO as laid out in memory (112 bytes).
type GuildInfo struct {
	Name     MSVCString
	SubGuild [3]MSVCString
	Kingdom  uint8
	_        [3]byte
	Fame     int32
	Citizen  int32
	Wins     int32
}

// PanelGuildList mirrors _MSG_PANELGUILD_GETLIST (4492 bytes), 0x766.
type PanelGuildList struct {
	Header Header
	Guild  [40]GuildInfo
}

// CityBid is a guild's bet on a city (STRUCT_CHALLENGEINFO).
type CityBid struct {
	Guild int32
	Value int32
}

// CityState is the part of STRUCT_GUILDZONE that changes while the server
// runs. Only the eight highest bets of a city are kept.
type CityState struct {
	Impost int64
	Tax    int32
	Wins   int32
	Chall1 int32
	Chall2 int32
	Bids   [8]CityBid
}

// ChannelState (Go only, 0xC16): what the C++ kept in guild_zone.txt and
// gameconfig_v2.xml. The channel is Header.ClientID.
type ChannelState struct {
	Header        Header
	WeekMode      int32
	CastleState   int32
	CapeWin       int32
	TowerGuild    int32
	KefraDead     int32
	KefraKiller   int32
	KingdomWinner int32
	RvRBonus      int32
	Cities        [5]CityState
}

// CityOwnerUpdate (Go only, 0xC17): the war channel's new owner of a city.
type CityOwnerUpdate struct {
	Header Header
	City   int32
	Guild  int32
}

// MacBan (Go only, 0xC18): +banmac. Type 1 bans the computer.
type MacBan struct {
	Header Header
	Mac    [8]byte
	Type   int32
}

// GMMessage (Go only, 0xC19): what a player wrote with /gm.
type GMMessage struct {
	Header   Header
	Channel  int32
	Username [16]byte
	CharName [16]byte
	Text     [100]byte // Windows-1252
}

// PanelCommand (Go only, 0xC1A): a GM command from the admin panel. CharName
// is the character it acts on ("" for the commands of the whole channel).
type PanelCommand struct {
	Header   Header
	Delivery int64
	CharName [16]byte
	Command  [128]byte // Windows-1252
}

// PanelCommandResult (Go only, 0xC1B): what the command answered.
type PanelCommandResult struct {
	Header   Header
	Delivery int64
	Text     [256]byte // Windows-1252
}

// OpenStore (Go only, 0xC1C): one store open on a channel, for the panel.
type OpenStore struct {
	Header  Header
	Channel int32
	Items   int32
	Owner   [16]byte
	Name    [24]byte
}

// StoreSale (Go only, 0xC1D): one sale in the auction stores.
type StoreSale struct {
	Header   Header
	Channel  int32
	Category int32 // 1 donate, 2 gold, 3 bars
	Price    int64
	Tax      int64
	Buyer    [16]byte
	Seller   [16]byte
	Item     Item
}
