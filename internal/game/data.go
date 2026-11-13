package game

import (
	"crypto/ecdh"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/cluster"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

const (
	MaxItemList = 6500
	gridSize    = 4096
)

// ItemData mirrors STRUCT_ITEMLIST (one ItemList.csv row).
type ItemData struct {
	Name    string
	Mesh1   int16
	Mesh2   int32
	Level   int16
	Str     int16
	Int     int16
	Dex     int16
	Con     int16
	Effect  [12]struct{ Index, Value int16 }
	Price   int32
	Unique  int16
	Pos     uint16 // what the game rules see: 16 bits, as in the C++ server
	FullPos int32  // the client's value (necklaces 65536, belts 131072), kept in the database
	Extreme int16
	Grade   int16
}

// CityZone mirrors the STRUCT_GUILDZONE fields Phase A uses (cities of tmsrv.yaml).
type CityZone struct {
	OwnerIndex             int32
	TowerX, TowerY         int32
	AreaGuildX, AreaGuildY int32
	CityX, CityY           int32
	CityMinX, CityMinY     int32
	CityMaxX, CityMaxY     int32
	WarMinX, WarMinY       int32
	WarMaxX, WarMaxY       int32
	GuildAWarX, GuildAWarY int32 // where the defenders start a city war
	GuildBWarX, GuildBWarY int32 // where the challengers start
	Impost                 int64 // taxes collected
	PercImpost             int32 // tax percentage on NPC trade
	WinCount               int32 // times the owner won the city (tower model)
}

// Data is the static game data loaded at startup.
type Data struct {
	Items        []ItemData // indexed by item id, len MaxItemList
	Cities       [5]CityZone
	Height       []byte // gamedata.assets HeightMap.dat, [y*4096+x]; 127 = blocked
	Attribute    []byte // gamedata.assets AttributeMap.dat, 1024x1024 cells of 4x4 tiles
	Teleports    []Teleport
	Geners       []Generator           // gamedata.npc_generators, index = generator id
	Lang         map[int]string        // config/language.yaml
	Skills       []SkillData           // skilldata.csv, 256 entries
	Drops        map[string]*MobDrop   // drop table by mob name (LoadDrops)
	DropSlots    [64]store.DropSlot    // chance settings of each slot
	BlockedDrops map[int]bool          // never dropped by chance
	NPCBase      [50]wire.Mob          // templates npc_base (mounts, summons)
	npcFiles     map[string][]byte     // gamedata.mob_templates, kind npc
	InitItems    []InitItemFile        // init_items of tmsrv.yaml: gates, barriers, doors
	NPCEvents    [maxNPCEvent]NPCEvent // npc_events of tmsrv.yaml
	StoreBase    *wire.Mob             // template Base: the auction store NPC
	Thorn        *wire.Mob             // template Vinha: Muro de Espinhos (skill 98)

	DailyReward [7][4]wire.Item             // daily_reward of tmsrv.yaml
	Missions    []Mission                   // config/missions.yaml
	Roulette    [maxRouletteBox]RouletteBox // roulette of tmsrv.yaml
	ExpBonus    ExpBonus                    // exp_bonus of tmsrv.yaml
	ExpRule     store.ExpRule               // gamedata.exp_rule (the panel); disabled = the C++ calculation
}

// Config is what the game loop runs with: tmsrv.yaml (see ApplySettings).
type Config struct {
	Limits      LimitSettings  // limits of tmsrv.yaml
	NoCPAreas   []NoCPArea     // no_cp_areas of tmsrv.yaml
	LevelItems  []LevelItem    // level_items of tmsrv.yaml
	CashShop    []CashShopTab  // cash_shop of tmsrv.yaml
	HonorMarket []HonorItem    // honor_market of tmsrv.yaml
	Arena       ArenaSettings  // arena of tmsrv.yaml
	Wars        WarSettings    // wars of tmsrv.yaml
	Bosses      []BossSettings // bosses of tmsrv.yaml
	ListenIP    string
	ListenPort  int
	DBIP        string
	DBPort      int
	Channel     int            // 1-based
	Cluster     cluster.Config // every channel (in-game channel change)
	// ChangeChannelCooldown: minimum time between channel changes.
	ChangeChannelCooldown time.Duration
	NewbieZone            int
	StatSapphire          int      // sapphires the Mestre das Habilidades charges
	DropToInventory       []uint16 // monster drops that skip the ground
	MobAI                 MobAISettings
	Login                 LoginSettings
	Missions              MissionSettings
	Store                 StoreSettings
	Locks                 LockStore // account locks; nil keeps them only in memory
	// TransportKey is the static key of the secure client transport
	// (protocol/secure.go). The server always has one (TMSRV_TRANSPORT_KEY);
	// nil keeps the old cipher, for tests that speak it.
	TransportKey *ecdh.PrivateKey
}

// ApplySettings copies tmsrv.yaml values into the config, for channel
// (1-based; 0 = the only channel of the file).
func (c *Config) ApplySettings(s Settings, channel int) error {
	cl := s.Cluster()
	if channel == 0 {
		if len(cl.Channels) != 1 {
			return fmt.Errorf("%d channels configured: choose one with -channel (or TMSRV_CHANNEL)", len(cl.Channels))
		}
		channel = 1
	}
	ch, ok := cl.Channel(channel)
	if !ok {
		return fmt.Errorf("channel %d is not in the settings (channels 1..%d)", channel, len(cl.Channels))
	}
	c.ListenIP, c.ListenPort = ch.IP, ch.Port
	c.Limits = s.Limits
	c.Channel, c.Cluster = channel, cl
	c.ChangeChannelCooldown = time.Duration(s.ChangeChannel.CooldownSeconds) * time.Second
	c.DBIP, c.DBPort = s.Network.DBSRVIP, s.Network.DBSRVPort
	c.NewbieZone, c.MobAI = s.NewbieZone, s.MobAI
	c.StatSapphire = s.StatSapphire
	c.DropToInventory = s.DropToInventory
	c.CashShop, c.HonorMarket = s.cashShopTabs(), s.honorMarketItems()
	c.NoCPAreas, c.LevelItems = s.NoCPAreas, s.levelItems()
	c.Arena = s.Arena
	c.Wars, c.Bosses = s.Wars, s.Bosses
	c.Login, c.Missions, c.Store = s.Login, s.Missions, s.Store
	return nil
}

// LoadConfig is the config with the C++ values (tests use it; the server
// applies tmsrv.yaml on top with ApplySettings).
func LoadConfig() (Config, error) {
	var c Config
	err := c.ApplySettings(CppSettings(), 1)
	return c, err
}

// fillDefaults gives the zero values of a hand-made test Config the C++
// values (the server's Config always comes complete from tmsrv.yaml).
func (c *Config) fillDefaults() {
	d := CppSettings()
	if c.Limits == (LimitSettings{}) {
		c.Limits = d.Limits
	}
	if c.MobAI.IdleThinkSeconds == 0 || c.MobAI.BattleThinkSeconds == 0 {
		c.MobAI = d.MobAI
	}
	if c.Login.MaxAccountsPerComputer == 0 {
		c.Login = d.Login
	}
	if c.Missions.OfferEverySeconds == 0 || c.Missions.TimeToCompleteSeconds == 0 {
		c.Missions = d.Missions
	}
	if c.Store.HonorEverySeconds == 0 {
		c.Store = d.Store
	}
}

// LoadData builds the static data: the world tables of tmsrv.yaml (s), the
// big tables of the config folder (dir) and the binary files of the database
// (w). Items, skills, drops, teleports and NPC generators come from the
// database too: ApplyGameData.
func LoadData(dir string, s Settings, w *store.WorldAssets) (*Data, error) {
	d := &Data{Items: make([]ItemData, MaxItemList), Skills: make([]SkillData, maxSkill)} // Skills: filled by ApplyGameData
	if err := d.setWorldAssets(w); err != nil {
		return nil, err
	}
	d.applyWorldTables(&s)
	if err := d.loadLanguage(dir); err != nil {
		return nil, err
	}
	if err := d.loadMissions(dir); err != nil {
		return nil, err
	}
	return d, nil
}

func atoi(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		// sscanf %d stops at the first non-digit
		s = strings.TrimSpace(s)
		end := 0
		for end < len(s) && (s[end] >= '0' && s[end] <= '9' || (end == 0 && s[end] == '-')) {
			end++
		}
		v, _ = strconv.Atoi(s[:end])
	}
	return v
}

// Item returns the ItemList row for id (a zero row when out of range, as the
// C++ read ItemList[0]).
func (d *Data) Item(id uint16) *ItemData {
	if int(id) >= len(d.Items) {
		return &d.Items[0]
	}
	return &d.Items[id]
}

// Blocked reports HeightMap value 127 (or out of map).
func (d *Data) Blocked(x, y int) bool {
	if x < 0 || y < 0 || x >= gridSize || y >= gridSize {
		return true
	}
	return d.Height[y*gridSize+x] == 127
}

// npcTemplate decodes the NPC template name (nil if there is none).
func (d *Data) npcTemplate(name string) *wire.Mob {
	b, ok := d.npcFiles[name]
	if !ok {
		return nil
	}
	var m wire.Mob
	if wire.UnmarshalLegacyMob(b, &m) != nil {
		return nil
	}
	return &m
}
