package game

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/cluster"
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

// CityZone mirrors the STRUCT_GUILDZONE fields Phase A uses (guild_zone.txt).
type CityZone struct {
	OwnerIndex             int32
	TowerX, TowerY         int32
	AreaGuildX, AreaGuildY int32
	CityX, CityY           int32
	CityMinX, CityMinY     int32
	CityMaxX, CityMaxY     int32
	WarMinX, WarMinY       int32
	WarMaxX, WarMaxY       int32
	Impost                 int64 // taxes collected
	PercImpost             int32 // tax percentage on NPC trade
	WinCount               int32 // times the owner won the city (tower model)
}

// Data is the static game data loaded at startup (from the TMSRV folder).
type Data struct {
	Items     []ItemData // indexed by item id, len MaxItemList
	Cities    [5]CityZone
	Height    []byte // HeightMap.dat, [y*4096+x]; 127 = blocked
	Attribute []byte // AttributeMap.dat, 1024x1024 cells of 4x4 tiles
	Teleports []Teleport
	Geners    []Generator           // NPCGener.txt, index = generator id
	Lang      map[int]string        // Language.txt
	Skills    []SkillData           // skilldata.csv, 256 entries
	Drops     map[string]*MobDrop   // drop table by mob name (LoadDrops)
	NPCBase   [50]wire.Mob          // npc_base/ (mounts, summons)
	InitItems []InitItemFile        // InitItem.csv: gates, barriers, doors
	NPCEvents [maxNPCEvent]NPCEvent // Data/NPCEvento.txt
	StoreBase *wire.Mob             // npc/Base: the auction store NPC
	Thorn     *wire.Mob             // npc/Vinha: Muro de Espinhos (skill 98)

	DailyReward [7][4]wire.Item             // Data/DailyReward.txt
	Missions    []Mission                   // Data/Missions.xml
	Roulette    [maxRouletteBox]RouletteBox // ../Conf/Roleta.txt
	ExpBonus    ExpBonus                    // Controlador EXP/*.txt
}

// Config is what the game loop runs with: tmsrv.yaml (see ApplySettings)
// plus the TMSRV data folder. serverip.txt and dataserver.txt are no
// longer read (they stay for the C++ server).
type Config struct {
	NoCPAreas   []NoCPArea    // no_cp_areas of tmsrv.yaml
	LevelItems  []LevelItem   // level_items of tmsrv.yaml
	CashShop    []CashShopTab // cash_shop of tmsrv.yaml
	HonorMarket []HonorItem   // honor_market of tmsrv.yaml
	ListenIP    string
	ListenPort  int
	DBIP        string
	DBPort      int
	Channel     int            // 1-based
	Cluster     cluster.Config // every channel (in-game channel change)
	// ChangeChannelCooldown: minimum time between channel changes.
	ChangeChannelCooldown time.Duration
	NewbieZone            int
	StatSapphire          int // sapphires the Mestre das Habilidades charges
	MobAI                 MobAISettings
	Login                 LoginSettings
	Missions              MissionSettings
	Store                 StoreSettings
	CacheDir              string // per-account files shared with the C++ (locks, daily)
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
	c.Channel, c.Cluster = channel, cl
	c.ChangeChannelCooldown = time.Duration(s.ChangeChannel.CooldownSeconds) * time.Second
	c.DBIP, c.DBPort = s.Network.DBSRVIP, s.Network.DBSRVPort
	c.NewbieZone, c.MobAI = s.NewbieZone, s.MobAI
	c.StatSapphire = s.StatSapphire
	c.CashShop, c.HonorMarket = s.cashShopTabs(), s.honorMarketItems()
	c.NoCPAreas, c.LevelItems = s.NoCPAreas, s.levelItems()
	c.Login, c.Missions, c.Store = s.Login, s.Missions, s.Store
	if s.CacheDir != "" {
		c.CacheDir = s.CacheDir
	}
	return nil
}

// LoadConfig is the config of a TMSRV data folder with the C++ values (tests
// use it; the server applies tmsrv.yaml on top with ApplySettings).
func LoadConfig(dir string) (Config, error) {
	var c Config
	if err := c.ApplySettings(CppSettings(), 1); err != nil {
		return c, err
	}
	c.CacheDir = filepath.Join(dir, "Cache")
	return c, nil
}

// fillDefaults gives the zero values of a hand-made test Config the C++
// values (the server's Config always comes complete from tmsrv.yaml).
func (c *Config) fillDefaults() {
	d := CppSettings()
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

// LoadData loads the file-based data from the C++ TMSRV folder: guild_zone.txt,
// HeightMap.dat, AttributeMap.dat, Language.txt and NPCGener.txt (+ npc/).
// Items, skills, drops and teleports come from the database: ApplyGameData.
func LoadData(dir string) (*Data, error) {
	d := &Data{Items: make([]ItemData, MaxItemList)}
	if err := d.loadCityZones(filepath.Join(dir, "guild_zone.txt")); err != nil {
		return nil, err
	}
	h, err := os.ReadFile(filepath.Join(dir, "HeightMap.dat"))
	if err != nil {
		return nil, err
	}
	if len(h) != gridSize*gridSize {
		return nil, fmt.Errorf("HeightMap.dat: %d bytes, want %d", len(h), gridSize*gridSize)
	}
	d.Height = h
	if err := d.loadWorldData(dir); err != nil {
		return nil, err
	}
	if err := d.loadInitItems(filepath.Join(dir, "InitItem.csv")); err != nil {
		return nil, err
	}
	if err := d.loadNPCEvents(filepath.Join(dir, "Data", "NPCEvento.txt")); err != nil {
		return nil, err
	}
	d.loadStoreBase(dir)
	if err := loadOptional(d.loadDailyReward, filepath.Join(dir, "Data", "DailyReward.txt")); err != nil {
		return nil, err
	}
	if err := loadOptional(d.loadMissions, filepath.Join(dir, "Data", "Missions.xml")); err != nil {
		return nil, fmt.Errorf("Missions.xml: %w", err)
	}
	if err := loadOptional(d.loadRoulette, filepath.Join(dir, "Conf", "Roleta.txt")); err != nil {
		return nil, err
	}
	for _, f := range []string{"ControleExp.txt", "bonusmortal.txt"} {
		if err := loadOptional(func(p string) error { return d.ExpBonus.load(p, f) },
			filepath.Join(dir, "Controlador EXP", f)); err != nil {
			return nil, err
		}
	}
	return d, nil
}

var defineRe = regexp.MustCompile(`^\s*#define\s+(\w+)\s+(\d+)`)

// loadEffectNames mirrors ReadItemEffect: the runtime header decides how
// ItemList.csv effect names map to ids. Falls back to the compiled table.
func loadEffectNames(path string) map[string]int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return effectNames
	}
	out := map[string]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		if m := defineRe.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[2]); err == nil && v >= 0 && v < 256 {
				out[m[1]] = v
			}
		}
	}
	if len(out) == 0 {
		return effectNames
	}
	return out
}

// loadItemList mirrors ReadItemList: commas become spaces, then
// "id name mesh score unique price pos extreme grade (effect value)*12".
func (d *Data) loadItemList(path string, names map[string]int) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	n := 0
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Fields(strings.ReplaceAll(line, ",", " "))
		if len(f) < 9 {
			continue
		}
		id, err := strconv.Atoi(f[0])
		if err != nil || id <= 0 || id >= MaxItemList {
			continue
		}
		var it ItemData
		it.Name = f[1]
		mesh := strings.SplitN(f[2], ".", 2)
		it.Mesh1 = int16(atoi(mesh[0]))
		if len(mesh) > 1 {
			it.Mesh2 = int32(atoi(mesh[1]))
		}
		score := strings.Split(f[3], ".")
		dst := []*int16{&it.Level, &it.Str, &it.Int, &it.Dex, &it.Con}
		for i := 0; i < len(dst) && i < len(score); i++ {
			*dst[i] = int16(atoi(score[i]))
		}
		it.Unique = int16(atoi(f[4]))
		it.Price = int32(atoi(f[5]))
		it.FullPos = int32(atoi(f[6]))
		it.Pos = uint16(it.FullPos)
		it.Extreme = int16(atoi(f[7]))
		it.Grade = int16(atoi(f[8]))
		for e := 0; e < 12 && 9+2*e+1 < len(f); e++ {
			name := f[9+2*e]
			idx, ok := names[name]
			if !ok {
				idx = atoi(name) // get_effect_index falls back to atoi
			}
			it.Effect[e].Index = int16(idx)
			it.Effect[e].Value = int16(atoi(f[9+2*e+1]))
		}
		d.Items[id] = it
		n++
	}
	if n == 0 {
		return fmt.Errorf("%s: no items", path)
	}
	return sc.Err()
}

func (d *Data) loadCityZones(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < 10 {
			continue
		}
		idx := atoi(f[0])
		if idx < 0 || idx >= len(d.Cities) {
			continue
		}
		z := CityZone{
			OwnerIndex: int32(atoi(f[1])),
			TowerX:     int32(atoi(f[4])), TowerY: int32(atoi(f[5])),
			AreaGuildX: int32(atoi(f[6])), AreaGuildY: int32(atoi(f[7])),
			CityX: int32(atoi(f[8])), CityY: int32(atoi(f[9])),
		}
		if len(f) >= 14 {
			z.CityMinX, z.CityMinY = int32(atoi(f[10])), int32(atoi(f[11]))
			z.CityMaxX, z.CityMaxY = int32(atoi(f[12])), int32(atoi(f[13]))
		}
		if len(f) >= 22 {
			z.WarMinX, z.WarMinY = int32(atoi(f[18])), int32(atoi(f[19]))
			z.WarMaxX, z.WarMaxY = int32(atoi(f[20])), int32(atoi(f[21]))
		}
		if len(f) >= 28 {
			z.Impost = int64(atoi(f[26]))
			z.PercImpost = int32(atoi(f[27]))
		}
		if len(f) >= 29 {
			z.WinCount = int32(atoi(f[28]))
		}
		d.Cities[idx] = z
	}
	return sc.Err()
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
