// Package legacy reads the C++ DBSRV's on-disk formats (account/*.xml,
// char/*, seal/*.xml, guilds.txt, Donate.txt, Config.txt) for the one-time
// import into Postgres.
//
// The XML shape comes from AppendStructure/XMLToStructure in
// Dataserver/Basedef.cpp and CSealSoul.cpp.
package legacy

// Raw XML node shapes. Numbers stay strings so a bad value becomes a warning
// instead of failing the whole account (the C++ std::stoi threw and the
// account became unloadable).

type xDate struct {
	Day    string `xml:"day"`
	Month  string `xml:"month"`
	Year   string `xml:"year"`
	Hour   string `xml:"hour"`
	Minute string `xml:"minute"`
	Second string `xml:"second"`
}

type xPos struct {
	X string `xml:"x,attr"`
	Y string `xml:"y,attr"`
}

type xItem struct {
	Slot   *string `xml:"slot,attr"`
	Index  *string `xml:"index,attr"`
	ItemID string  `xml:"itemId,attr"`
	Ef0    string  `xml:"ef0,attr"`
	Efv0   string  `xml:"efv0,attr"`
	Ef1    string  `xml:"ef1,attr"`
	Efv1   string  `xml:"efv1,attr"`
	Ef2    string  `xml:"ef2,attr"`
	Efv2   string  `xml:"efv2,attr"`
}

type xSk struct {
	Index string `xml:"index,attr"`
	Value string `xml:",chardata"`
}

type xAffect struct {
	SkillIndex *string `xml:"skillIndex,attr"`
	Inex       *string `xml:"inex,attr"` // misspelling some old files used; C++ accepted it for sub affects
	Index      string  `xml:"index,attr"`
	Master     string  `xml:"master,attr"`
	Value      string  `xml:"value,attr"`
	Time       string  `xml:"time,attr"`
}

type xMastery struct {
	M0 string `xml:"mastery0,attr"`
	M1 string `xml:"mastery1,attr"`
	M2 string `xml:"mastery2,attr"`
	M3 string `xml:"mastery3,attr"`
}

type xStatus struct {
	Level    string   `xml:"level"`
	Defense  string   `xml:"defense"`
	Attack   string   `xml:"attack"`
	Merchant string   `xml:"merchant"`
	Move     string   `xml:"move"`
	MaxHP    string   `xml:"maxHp"`
	MaxMP    string   `xml:"maxMp"`
	CurHP    string   `xml:"curHp"`
	CurMP    string   `xml:"curMp"`
	STR      string   `xml:"str"`
	INT      string   `xml:"int"`
	DEX      string   `xml:"des"`
	CON      string   `xml:"con"`
	Mastery  xMastery `xml:"mastery"`
}

type xPlayer struct {
	Name            string   `xml:"name"`
	CapeInfo        string   `xml:"capeInfo"`
	Info            string   `xml:"info"`
	QuestInfo       string   `xml:"questInfo"`
	GuildIndex      string   `xml:"guildIndex"`
	ClassInfo       string   `xml:"classInfo"`
	AffectInfo      string   `xml:"affectInfo"`
	Gold            string   `xml:"gold"`
	Experience      string   `xml:"experience"`
	LastPosition    xPos     `xml:"lastPosition"`
	BaseStatus      *xStatus `xml:"baseStatus"`
	CurrentStatus   *xStatus `xml:"currentStatus"`
	Equips          []xItem  `xml:"equips>item"`
	Inventory       []xItem  `xml:"inventory>item"`
	Learn           string   `xml:"learn"`
	SecLearn        string   `xml:"secLearn"`
	StatusPoint     string   `xml:"statusPoint"`
	MasterPoint     string   `xml:"masterPoint"`
	SkillPoint      string   `xml:"skillPoint"`
	Critical        string   `xml:"critical"`
	SaveMana        string   `xml:"saveMana"`
	SkillBar1       []xSk    `xml:"skillbar1>sk"`
	GuildMemberType string   `xml:"guildMemberType"`
}

type xSub struct {
	Status    string    `xml:"status,attr"`
	SubStatus *xStatus  `xml:"status"`
	Equips    []xItem   `xml:"equips>equip"`
	Exp       string    `xml:"experience"`
	Learn     string    `xml:"learn"`
	SecLearn  string    `xml:"secLearn"`
	SkillBar  []xSk     `xml:"skillbar>sk"`
	Affects   []xAffect `xml:"affects>affect"`
	QuestInfo string    `xml:"questInfo"`
	Soul      string    `xml:"soul"`
}

type xNightmare struct {
	Index string `xml:"index,attr"`
	X     string `xml:"x,attr"`
	Y     string `xml:"y,attr"`
}

// xCharInfo is a <mob> node (account) or the <mob> child of <seal>.
type xCharInfo struct {
	Index       string       `xml:"index,attr"`
	Player      xPlayer      `xml:"player"`
	Affects     []xAffect    `xml:"affects>affect"`
	Info        string       `xml:"info"`
	HallEnter   string       `xml:"hallEnter"`
	Fame        string       `xml:"fame"`
	PesaEnter   string       `xml:"pesaEnter"`
	Citizen     string       `xml:"citizen"`
	Soul        string       `xml:"soul"`
	Hold        string       `xml:"hold"`
	SkillBar2   []xSk        `xml:"skillbar2>sk"`
	MortalSlot  string       `xml:"mortalSlot"`
	RvRPoints   string       `xml:"rvrPoints"`
	Saude       *xDate       `xml:"saude"`
	Escritura   *xDate       `xml:"escritura"`
	Revigorante *xDate       `xml:"revigorante"`
	LastKickOut *xDate       `xml:"lastKickOut"`
	Sub         *xSub        `xml:"sub"`
	Nightmares  []xNightmare `xml:"nightmares>nightmare"`
	Filter      []xItem      `xml:"filter>items"`
}

type xAccount struct {
	Username       string      `xml:"username"`
	Password       string      `xml:"password"`
	Cash           string      `xml:"cash"`
	Honra          string      `xml:"honra"`
	Novato         string      `xml:"novato"`
	AlreadyReseted string      `xml:"alreadyReseted"`
	BanType        string      `xml:"ban>type"`
	BanDate        *xDate      `xml:"ban>date"`
	TempKey        string      `xml:"tempkey"`
	Token          string      `xml:"token"`
	TokenBlock     string      `xml:"tokenblock"`
	Access         string      `xml:"access"`
	Mobs           []xCharInfo `xml:"mob"`
	StorageGold    string      `xml:"storage>gold"`
	StorageItems   []xItem     `xml:"storage>items>item"`
	Insignia       string      `xml:"insignia"`
	CharSlot       string      `xml:"charslot"`
	Position       xPos        `xml:"position"`
	WaterDay       string      `xml:"water>day"`
	WaterTotal     string      `xml:"water>total"`
	Divina         *xDate      `xml:"divina"`
	Sephira        *xDate      `xml:"sephira"`
}

type xSkill struct {
	Index string `xml:"index,attr"`
	Value string `xml:"value,attr"`
}

type xSeal struct {
	CON       string    `xml:"CON"`
	STR       string    `xml:"STR"`
	DEX       string    `xml:"DEX"`
	INT       string    `xml:"INT"`
	Face      string    `xml:"face"`
	Level     string    `xml:"level"`
	Quest     string    `xml:"quest"`
	Evolution string    `xml:"evolution"`
	Cape      string    `xml:"cape"`
	Unk3      string    `xml:"unk3"`
	Skills    []xSkill  `xml:"skills>skill"`
	Mob       xCharInfo `xml:"mob"`
}
