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
	Password [12]byte // 12
	Login    [16]byte // 24
	Unknow   [52]byte // 40: TempKey for server change: "server enc0..enc7 slot"
	CliVer   uint32   // 92
	Unknow84 uint32   // 96: nonzero = kick previous session
	Mac      [8]byte  // 100
	Keys     [12]byte // 108
}

// CharList mirrors st_CharList (968 bytes).
type CharList struct {
	PositionX [4]int16
	PositionY [4]int16
	Name      [4][16]byte
	Status    [4]Status   // 80
	Equip     [4][20]Item // 272
	GuildID   [4]uint16   // 912
	Gold      [4]int32    // 920
	Exp       [4]int64    // 936
}

// CNFAccountLogin mirrors p416 (2376 bytes), sent as 0x416.
type CNFAccountLogin struct {
	Header      Header
	Keys        [16]byte  // 12
	Unkw        int32     // 28
	CharList    CharList  // 32
	Storage     [168]Item // 1000
	GoldStorage uint32    // 2344
	UserName    [16]byte  // 2348
	Unknow      [12]byte  // 2364
}

// CharListPacket mirrors p418 (984 bytes), sent as 0x418 / 0x419.
type CharListPacket struct {
	Header   Header
	_        [4]byte
	CharList CharList // 16
}

// CharacterLogin mirrors p213 (32 bytes), 0x804.
type CharacterLogin struct {
	Header    Header
	CharIndex int32
	Zero      [16]byte
}

// CNFCharacterLogin mirrors p114 (1152 bytes), sent as 0x417.
type CNFCharacterLogin struct {
	Header      Header
	WorldPos    Position // 12
	Mob         Mob      // 16
	CurrentKill uint16   // 864
	TotalKill   uint16
	SlotIndex   int16 // 868
	ClientIndex int16
	Weather     int16
	SkillBar2   [16]byte // 874
	_           [2]byte
	Affect      [32]Affect // 892
	Hold        int32      // 1148
}

// CNFCharacterLoginExtra mirrors p820 (2088 bytes), sent as 0x820.
type CNFCharacterLoginExtra struct {
	Header    Header
	_         [4]byte
	Mob       CharInfo // 16
	Cash      int32    // 2024
	Honra     int32
	Novato    int32
	Pass      [16]byte // 2036
	Blocked   int32    // 2052
	PassBlock [16]byte // 2056
	TypeBlock [10]byte // 2072
	_         [6]byte
}

// AdditionalAccountInfo mirrors p415 (128 bytes; TMSRV's copy is 120, it has
// no TypeBlock), sent as 0x415.
type AdditionalAccountInfo struct {
	Header     Header
	Access     int32 // 12
	Unique     int64 // 16
	Daily      Daily // 24
	Water      Water // 36
	Divina     ItemDate
	Sephira    ItemDate // 52
	_          [4]byte
	SingleGift uint64   // 64
	Ban        ItemDate // 72
	BanType    uint32   // 80
	IsBanned   int32
	Cash       int32
	Honra      int32
	Novato     int32
	PassBlock  [16]byte // 100
	TypeBlock  [10]byte // 116
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

// SaveCharacter mirrors STRUCT_SAVECHARACTER (4048 bytes; TMSRV's copy is
// 4032, it has no TypeBlock), 0x806 / 0x807.
type SaveCharacter struct {
	Header     Header
	CharSlot   uint32       // 12
	Mob        CharInfo     // 16
	Storage    [168]Item    // 2024
	Coin       int32        // 3368
	SkillBar   [20]byte     // 3372: [0:4] -> SkillBar1, [4:20] -> CharInfo.SkillBar
	User       [16]byte     // 3392
	Arg2       uint32       // 3408: nonzero = also export
	Friends    [30][16]byte // 3412
	Pass       [16]byte     // 3892
	Blocked    int32        // 3908
	Cash       int32
	Honra      int32
	Novato     int32 // 3920
	_          [4]byte
	Insignia   int64    // 3928
	BanType    uint32   // 3936
	Ban        ItemDate // 3940
	Position   Position // 3948
	Slot       int16    // 3952
	_          [6]byte
	Unique     int64 // 3960
	Daily      Daily // 3968
	Water      Water // 3980
	Divina     ItemDate
	Sephira    ItemDate // 3996
	_          [4]byte
	SingleGift uint64   // 4008
	PassBlock  [16]byte // 4016
	TypeBlock  [10]byte // 4032
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

// Security alert kinds.
const (
	AlertWPE = 1 // the client's protection saw WPE (packet editor)
)

// ItemDelivery mirrors pCOF (40 bytes), sent as 0xC0F.
type ItemDelivery struct {
	Header   Header
	Val      int32
	Username [16]byte
	Item     Item
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
// AdminEdit is a staff edit of a player in game (Go only, 0xC11, 80 bytes;
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
	HasExpect int32
	_         [4]byte
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
	SubGuild [3]MSVCString // 24
	Kingdom  uint8         // 96
	_        [3]byte
	Fame     int32 // 100
	Citizen  int32
	Wins     int32
}

// PanelGuildList mirrors _MSG_PANELGUILD_GETLIST (4492 bytes), 0x766.
type PanelGuildList struct {
	Header Header
	Guild  [40]GuildInfo
}
