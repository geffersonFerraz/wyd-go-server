package game

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"dbsrv/internal/cluster"
	"dbsrv/internal/wire"
)

// Settings is tmsrv.yaml, the only source of the Go server's options: the
// servers with their DBSRV address and channels (formerly serverip.txt,
// dataserver.txt and DBSRV's serverlist.txt; also the source of the client's
// serverlist.bin) and the gameplay values that used to be fixed in code or
// in the C++ gameconfig.txt. One file describes every server and channel;
// each TMSRV picks its own with -server and -channel.
//
// Every key must be present in the file: nothing silently falls back to a
// value in the code (LoadSettings names the missing keys).
type Settings struct {
	Limits LimitSettings `yaml:"limits"`
	// Servers: every server (group, DBSRV, channels) the client can reach.
	Servers []cluster.Server `yaml:"servers"`
	// ChangeChannel: in-game channel change (client "srv507").
	ChangeChannel ChangeChannelSettings `yaml:"change_channel"`
	NewbieZone    int                   `yaml:"newbie_zone"`
	// StatSapphire: sapphires the Mestre das Habilidades (stat reset NPC)
	// charges (gameconfig.txt StatSapphire, 30 in the shipped file).
	StatSapphire int `yaml:"stat_sapphire"`
	// DropToInventory: monster drops that go straight to the killer's
	// inventory instead of falling on the ground (quest items).
	DropToInventory []uint16        `yaml:"drop_to_inventory"`
	Login           LoginSettings   `yaml:"login"`
	Missions        MissionSettings `yaml:"missions"`
	Store           StoreSettings   `yaml:"store"`
	MobAI           MobAISettings   `yaml:"mob_ai"`
	// CashShop and HonorMarket replace Data/CashShop.xml and MarketHonra.txt.
	CashShop    []CashShopTabSettings `yaml:"cash_shop"`
	HonorMarket []HonorItemSettings   `yaml:"honor_market"`
	// NoCPAreas and LevelItems replace Data/NonCP.txt and Data/LevelItem.txt.
	NoCPAreas  []NoCPArea          `yaml:"no_cp_areas"`
	LevelItems []LevelItemSettings `yaml:"level_items"`
	Arena      ArenaSettings       `yaml:"arena"`
	Wars       WarSettings         `yaml:"wars"`
	// Bosses replaces Data/Boss.xml.
	Bosses []BossSettings `yaml:"bosses"`
	// The world tables of the C++ text files (see worldconfig.go).
	Cities      []CitySettings        `yaml:"cities"`
	InitItems   []InitItemSettings    `yaml:"init_items"`
	NPCEvents   []NPCEventSettings    `yaml:"npc_events"`
	DailyReward []DailyRewardSettings `yaml:"daily_reward"`
	Roulette    []RouletteSettings    `yaml:"roulette"`
	ExpBonus    ExpBonus              `yaml:"exp_bonus"`
}

// BossSettings is a boss that comes back by itself at one of its generators.
type BossSettings struct {
	Fame          int   `yaml:"fame"`
	RebornSeconds int   `yaml:"reborn_seconds"`
	MaxSeconds    int   `yaml:"max_seconds"`
	Geners        []int `yaml:"geners"`
}

// WarSettings: replaces the hours of gameconfig_v2.xml.
type WarSettings struct {
	CityHour   int `yaml:"city_hour"`
	CastleHour int `yaml:"castle_hour"`
	TowerHour  int `yaml:"tower_hour"`
	RvRHour    int `yaml:"rvr_hour"`
}

func (w *WarSettings) validate() error {
	for _, h := range []int{w.CityHour, w.CastleHour, w.TowerHour, w.RvRHour} {
		if h < 1 || h > 22 {
			return fmt.Errorf("wars: hour %d not 1..22", h)
		}
	}
	return nil
}

// ArenaSettings: when the Arena Real opens and what it pays.
type ArenaSettings struct {
	Hours            []int               `yaml:"hours"`
	WinnerItems      []ArenaItemSettings `yaml:"winner_items"`
	TopKillItems     []ArenaItemSettings `yaml:"top_kill_items"`
	ParticipantItems []ArenaItemSettings `yaml:"participant_items"`
}

// ArenaItemSettings is a prize; Amount 0 is a single item without a count.
type ArenaItemSettings struct {
	Item   int `yaml:"item"`
	Amount int `yaml:"amount"`
}

func (a *ArenaSettings) validate() error {
	seen := map[int]bool{}
	for _, h := range a.Hours {
		if h < 0 || h > 23 || seen[h] {
			return fmt.Errorf("arena.hours: %d repeated or not 0..23", h)
		}
		seen[h] = true
	}
	for _, list := range [][]ArenaItemSettings{a.WinnerItems, a.TopKillItems, a.ParticipantItems} {
		for _, it := range list {
			if it.Item < 1 || it.Item >= MaxItemList || it.Amount < 0 || it.Amount > 65535 {
				return fmt.Errorf("arena: invalid prize item %d amount %d", it.Item, it.Amount)
			}
		}
	}
	return nil
}

// NoCPArea is a map rectangle where a kill costs the killer no CP.
type NoCPArea struct {
	MinX int `yaml:"min_x"`
	MinY int `yaml:"min_y"`
	MaxX int `yaml:"max_x"`
	MaxY int `yaml:"max_y"`
}

// LevelItemSettings is an item given on reaching a level.
type LevelItemSettings struct {
	Evolution int      `yaml:"evolution"`
	Level     int      `yaml:"level"`
	Class     int      `yaml:"class"`
	Type      int      `yaml:"type"`
	Item      int      `yaml:"item"`
	Effects   [][2]int `yaml:"effects"`
}

// CashShopTabSettings is one tab of the premium store.
type CashShopTabSettings struct {
	ID    int                    `yaml:"id"`
	Name  string                 `yaml:"name"`
	Items []CashShopItemSettings `yaml:"items"`
}

// CashShopItemSettings is one slot of a tab. Limit is the most that can be
// bought at once (-1 not for sale, 0 sold out).
type CashShopItemSettings struct {
	Slot    int      `yaml:"slot"`
	Item    int      `yaml:"item"`
	Effects [][2]int `yaml:"effects"`
	Price   int      `yaml:"price"`
	Amount  int      `yaml:"amount"`
	Promo   int      `yaml:"promo"`
	Limit   int      `yaml:"limit"`
}

// HonorItemSettings is what the Honor Market sells, and for how much.
type HonorItemSettings struct {
	Item    int      `yaml:"item"`
	Effects [][2]int `yaml:"effects"`
	Price   int      `yaml:"price"`
}

// ChangeChannelSettings tunes the in-game channel change.
type ChangeChannelSettings struct {
	// CooldownSeconds: minimum time between two requests of a player.
	CooldownSeconds int `yaml:"cooldown_seconds"`
}

// LimitSettings are the caps of the 64-bit values. Each must be 1 to
// wire.MaxLimit.
type LimitSettings struct {
	MaxGold          int64 `yaml:"max_gold"`           // inventory, storage, trade and auction gold
	MaxDamage        int64 `yaml:"max_damage"`         // one hit of a player against a mob, skills included
	MaxAttackDefense int64 `yaml:"max_attack_defense"` // a player's attack and defense
}

func (l LimitSettings) validate() error {
	for name, v := range map[string]int64{"max_gold": l.MaxGold, "max_damage": l.MaxDamage,
		"max_attack_defense": l.MaxAttackDefense} {
		if v < 1 || v > wire.MaxLimit {
			return fmt.Errorf("limits.%s must be 1..%d, got %d", name, int64(wire.MaxLimit), v)
		}
	}
	return nil
}

// LoginSettings limits logins.
type LoginSettings struct {
	// MaxAccountsPerComputer: accounts playing at once from one MAC address
	// (C++: 3).
	MaxAccountsPerComputer int `yaml:"max_accounts_per_computer"`
}

// MissionSettings times the daily missions (C++: 0.32 h = 1152 s each).
type MissionSettings struct {
	OfferEverySeconds     int `yaml:"offer_every_seconds"`
	TimeToCompleteSeconds int `yaml:"time_to_complete_seconds"`
}

// StoreSettings is the auction store ("lojinha").
type StoreSettings struct {
	// HonorEverySeconds: an open store earns 1 honor point this often
	// (C++ TEMPO_LOJINHA: 600).
	HonorEverySeconds int `yaml:"honor_every_seconds"`
}

// MobAISettings tunes the mob AI.
type MobAISettings struct {
	// IdleThinkSeconds: how often an idle mob walks / looks for players
	// (C++: 3). Multiples of 0.5 s, the server tick.
	IdleThinkSeconds float64 `yaml:"idle_think_seconds"`
	// BattleThinkSeconds: how often a fighting mob acts (C++: 2).
	BattleThinkSeconds float64 `yaml:"battle_think_seconds"`
	// AggroWhileWalking: look for players even when more than 12 cells
	// from the route point being walked to (C++: false).
	AggroWhileWalking bool `yaml:"aggro_while_walking"`
	// FollowersLookForPlayers: group members also look for players, not
	// only the leader (C++: false).
	FollowersLookForPlayers bool `yaml:"followers_look_for_players"`
	// MinAttackChance: minimum % chance a fighting mob acts on its turn.
	// The C++ uses only the mob's INT (0 = keep that).
	MinAttackChance int `yaml:"min_attack_chance"`
}

// CppSettings is what the C++ server did. It is not a fallback for
// tmsrv.yaml (every key is required there); tests and New use it for the
// values a test Config leaves at zero.
func CppSettings() Settings {
	return Settings{
		Cities:    []CitySettings{{City: 0}, {City: 1}, {City: 2}, {City: 3}, {City: 4}},
		InitItems: []InitItemSettings{}, NPCEvents: []NPCEventSettings{}, DailyReward: []DailyRewardSettings{}, Roulette: []RouletteSettings{},
		Servers: []cluster.Server{{Group: 1, PublicIP: "127.0.0.1", StatusURL: "http://127.0.0.1/serv00.htm",
			DBSRV:    cluster.DBSRV{IP: "127.0.0.1", Port: 7514},
			Channels: []cluster.Channel{{Number: 1, IP: "127.0.0.1", Port: 8281}}}},
		ChangeChannel:   ChangeChannelSettings{CooldownSeconds: 10},
		NewbieZone:      35,
		StatSapphire:    30,
		DropToInventory: []uint16{4117, 4118, 4119, 4120, 4121},
		Limits:          LimitSettings{MaxGold: 2_000_000_000, MaxDamage: 64_000, MaxAttackDefense: 100_000},
		Login:           LoginSettings{MaxAccountsPerComputer: 3},
		Missions:        MissionSettings{OfferEverySeconds: 1152, TimeToCompleteSeconds: 1152},
		Store:           StoreSettings{HonorEverySeconds: 600},
		MobAI: MobAISettings{
			IdleThinkSeconds:   3,
			BattleThinkSeconds: 2,
		},
		CashShop:    []CashShopTabSettings{},
		HonorMarket: []HonorItemSettings{},
		NoCPAreas:   []NoCPArea{},
		LevelItems:  []LevelItemSettings{},
		Wars:        WarSettings{CityHour: 20, CastleHour: 21, TowerHour: 22, RvRHour: 20},
		Bosses:      []BossSettings{},
		Arena: ArenaSettings{Hours: []int{11, 14, 16, 18, 20, 23}, WinnerItems: []ArenaItemSettings{},
			TopKillItems: []ArenaItemSettings{{Item: 3251, Amount: 3}}, ParticipantItems: []ArenaItemSettings{{Item: 3251}}},
	}
}

// LoadSettings reads tmsrv.yaml. The file is required, every key in it is
// required, and unknown keys are an error (so typos do not go unnoticed).
func LoadSettings(path string) (Settings, error) {
	var s Settings
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("settings file: %w", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	if missing := missingKeys(reflect.TypeOf(s), tree, ""); len(missing) > 0 {
		return s, fmt.Errorf("%s: missing keys: %s", path, strings.Join(missing, ", "))
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil && !errors.Is(err, io.EOF) {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.validate(); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// missingKeys lists the yaml keys of t (nested structs as a.b) absent from
// the decoded tree.
func missingKeys(t reflect.Type, tree map[string]any, prefix string) []string {
	var out []string
	for i := range t.NumField() {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("yaml"), ",")
		key := tag[0]
		if key == "" || key == "-" {
			continue
		}
		v, ok := tree[key]
		if !ok {
			if !slices.Contains(tag[1:], "omitempty") { // omitempty marks an optional key.
				out = append(out, prefix+key)
			}
			continue
		}
		if f.Type.Kind() == reflect.Struct {
			sub, _ := v.(map[string]any)
			out = append(out, missingKeys(f.Type, sub, prefix+key+".")...)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Settings) validate() error {
	if err := s.Limits.validate(); err != nil {
		return err
	}
	cl := s.Cluster()
	if err := cl.Validate(); err != nil {
		return err
	}
	if c := s.ChangeChannel.CooldownSeconds; c < 0 || c > 3600 {
		return fmt.Errorf("change_channel.cooldown_seconds must be 0..3600, got %d", c)
	}
	if s.NewbieZone < 0 {
		return fmt.Errorf("newbie_zone must be >= 0, got %d", s.NewbieZone)
	}
	for _, id := range s.DropToInventory {
		if id == 0 || id >= MaxItemList {
			return fmt.Errorf("drop_to_inventory: item %d must be 1..%d", id, MaxItemList-1)
		}
	}
	if s.StatSapphire < 0 || s.StatSapphire > 1000 {
		return fmt.Errorf("stat_sapphire must be 0..1000, got %d", s.StatSapphire)
	}
	if s.Login.MaxAccountsPerComputer < 1 {
		return fmt.Errorf("login.max_accounts_per_computer must be >= 1, got %d", s.Login.MaxAccountsPerComputer)
	}
	for name, v := range map[string]int{"missions.offer_every_seconds": s.Missions.OfferEverySeconds,
		"missions.time_to_complete_seconds": s.Missions.TimeToCompleteSeconds, "store.honor_every_seconds": s.Store.HonorEverySeconds} {
		if v < 1 || v > 86400 {
			return fmt.Errorf("%s must be 1..86400, got %d", name, v)
		}
	}
	ai := &s.MobAI
	for name, v := range map[string]float64{"idle_think_seconds": ai.IdleThinkSeconds, "battle_think_seconds": ai.BattleThinkSeconds} {
		if v < 0.5 || v > 60 || math.Mod(v*2, 1) != 0 {
			return fmt.Errorf("mob_ai.%s must be 0.5..60 in steps of 0.5, got %v", name, v)
		}
	}
	if ai.MinAttackChance < 0 || ai.MinAttackChance > 100 {
		return fmt.Errorf("mob_ai.min_attack_chance must be 0..100, got %d", ai.MinAttackChance)
	}
	if err := s.validateLevels(); err != nil {
		return err
	}
	if err := s.Wars.validate(); err != nil {
		return err
	}
	for i, b := range s.Bosses {
		if len(b.Geners) == 0 || b.RebornSeconds < 1 || b.MaxSeconds < 1 || b.Fame < 0 {
			return fmt.Errorf("bosses[%d]: needs generators, reborn_seconds and max_seconds", i)
		}
	}
	if err := s.Arena.validate(); err != nil {
		return err
	}
	if err := s.validateWorldTables(); err != nil {
		return err
	}
	return s.validateShops()
}

func (s *Settings) validateLevels() error {
	for i, a := range s.NoCPAreas {
		if a.MinX < 0 || a.MinY < 0 || a.MaxX >= gridSize || a.MaxY >= gridSize || a.MinX > a.MaxX || a.MinY > a.MaxY {
			return fmt.Errorf("no_cp_areas[%d]: invalid rectangle %+v", i, a)
		}
	}
	for i, it := range s.LevelItems {
		where := fmt.Sprintf("level_items[%d]", i)
		if err := validateItem(where, it.Item, it.Effects); err != nil {
			return err
		}
		if it.Evolution < evoMortal || it.Evolution > evoSubCele {
			return fmt.Errorf("%s: evolution must be %d..%d, got %d", where, evoMortal, evoSubCele, it.Evolution)
		}
		if it.Level < 0 || it.Level > 399 || it.Class < 0 || it.Class > 3 || it.Type < -1 || it.Type > 3 {
			return fmt.Errorf("%s: level 0..399, class 0..3 and type -1..3", where)
		}
	}
	return nil
}

func (s *Settings) levelItems() []LevelItem {
	out := make([]LevelItem, 0, len(s.LevelItems))
	for _, it := range s.LevelItems {
		out = append(out, LevelItem{Evolution: it.Evolution, Level: it.Level, Class: it.Class, Type: it.Type, Item: makeItem(it.Item, it.Effects)})
	}
	return out
}

// validateItem checks an item id and its effects.
func validateItem(where string, item int, effects [][2]int) error {
	if item <= 0 || item >= MaxItemList {
		return fmt.Errorf("%s: item must be 1..%d, got %d", where, MaxItemList-1, item)
	}
	if len(effects) > 3 {
		return fmt.Errorf("%s: an item has at most 3 effects, got %d", where, len(effects))
	}
	for _, e := range effects {
		if e[0] < 0 || e[0] > 255 || e[1] < 0 || e[1] > 65535 {
			return fmt.Errorf("%s: effect must be 0..255, got [%d, %d]", where, e[0], e[1])
		}
	}
	return nil
}

func (s *Settings) validateShops() error {
	tabs := map[int]bool{}
	for t, tab := range s.CashShop {
		if tabs[tab.ID] || tab.ID < 0 || tab.ID > 32767 {
			return fmt.Errorf("cash_shop[%d]: id %d repeated or out of range", t, tab.ID)
		}
		tabs[tab.ID] = true
		if len(wire.EncodeGame(tab.Name)) > 31 {
			return fmt.Errorf("cash_shop[%d]: name longer than 31 characters", t)
		}
		slots := map[int]bool{}
		for i, it := range tab.Items {
			where := fmt.Sprintf("cash_shop[%d].items[%d]", t, i)
			if it.Slot < 0 || it.Slot >= cashShopSlots || slots[it.Slot] {
				return fmt.Errorf("%s: slot %d repeated or out of 0..%d", where, it.Slot, cashShopSlots-1)
			}
			slots[it.Slot] = true
			if err := validateItem(where, it.Item, it.Effects); err != nil {
				return err
			}
			// A missing price must not become a free item.
			gift := slices.ContainsFunc(it.Effects, func(e [2]int) bool { return e[0] == efGiftID })
			if it.Price < 0 || it.Price > math.MaxInt16 || (it.Price == 0 && !gift) {
				return fmt.Errorf("%s: price must be 1..%d (0 only with effect %d), got %d", where, math.MaxInt16, efGiftID, it.Price)
			}
			if it.Amount < 1 || it.Amount > math.MaxInt16 || it.Promo < 0 || it.Promo > math.MaxInt16 {
				return fmt.Errorf("%s: amount must be 1..%d and promo 0..%d", where, math.MaxInt16, math.MaxInt16)
			}
			if it.Limit < -1 || it.Limit > math.MaxInt16 {
				return fmt.Errorf("%s: limit must be -1..%d, got %d", where, math.MaxInt16, it.Limit)
			}
		}
	}
	items := map[int]bool{}
	for i, it := range s.HonorMarket {
		where := fmt.Sprintf("honor_market[%d]", i)
		if err := validateItem(where, it.Item, it.Effects); err != nil {
			return err
		}
		if items[it.Item] {
			return fmt.Errorf("%s: item %d repeated", where, it.Item)
		}
		items[it.Item] = true
		if it.Price < 1 || it.Price > 1_000_000 {
			return fmt.Errorf("%s: price must be 1..1000000, got %d", where, it.Price)
		}
	}
	return nil
}

func makeItem(id int, effects [][2]int) wire.Item {
	it := wire.Item{Index: uint16(id)}
	for i, e := range effects {
		it.Effect[i] = wire.ItemEffect{Index: uint8(e[0]), Value: uint16(e[1])}
	}
	return it
}

// cashShopTabs is the premium store as the game uses it: empty slots are
// not for sale.
func (s *Settings) cashShopTabs() []CashShopTab {
	out := make([]CashShopTab, 0, len(s.CashShop))
	for _, t := range s.CashShop {
		tab := CashShopTab{ID: t.ID, Name: t.Name}
		for i := range tab.Slots {
			tab.Slots[i].SlotFree = -1
		}
		for _, it := range t.Items {
			tab.Slots[it.Slot] = CashShopItem{Item: makeItem(it.Item, it.Effects), Price: it.Price, Amount: it.Amount, Promo: it.Promo, SlotFree: it.Limit}
		}
		out = append(out, tab)
	}
	return out
}

func (s *Settings) honorMarketItems() []HonorItem {
	out := make([]HonorItem, 0, len(s.HonorMarket))
	for _, it := range s.HonorMarket {
		out = append(out, HonorItem{Item: makeItem(it.Item, it.Effects), Price: it.Price})
	}
	return out
}

// Cluster is the server layout (shared with DBSRV and the admin panel).
func (s *Settings) Cluster() cluster.Config {
	return cluster.Config{Servers: s.Servers}
}

// ticks converts seconds to 500 ms server ticks.
func ticks(sec float64) int { return max(int(sec*2), 1) }
