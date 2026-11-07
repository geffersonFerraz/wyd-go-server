package game

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Settings is tmsrv.yaml, the only source of the Go server's options: the
// network addresses (formerly serverip.txt / dataserver.txt), the channel
// and the gameplay values that used to be fixed in code or in the C++
// gameconfig.txt.
//
// Every key must be present in the file: nothing silently falls back to a
// value in the code (LoadSettings names the missing keys).
type Settings struct {
	Network NetworkSettings `yaml:"network"`
	Channel int             `yaml:"channel"`
	// CacheDir: per-account files shared with the C++ server (account
	// locks, daily reward); "" = <TMSRV folder>/Cache.
	CacheDir   string `yaml:"cache_dir"`
	NewbieZone int    `yaml:"newbie_zone"`
	// StatSapphire: sapphires the Mestre das Habilidades (stat reset NPC)
	// charges (gameconfig.txt StatSapphire, 30 in the shipped file).
	StatSapphire int             `yaml:"stat_sapphire"`
	Login        LoginSettings   `yaml:"login"`
	Missions     MissionSettings `yaml:"missions"`
	Store        StoreSettings   `yaml:"store"`
	MobAI        MobAISettings   `yaml:"mob_ai"`
}

// NetworkSettings is where the TMSRV listens and where DBSRV is.
type NetworkSettings struct {
	// ListenIP / ListenPort: the address the game client connects to. The
	// IP is also the source address of the DBSRV link (DBSRV tells the
	// channels apart by it); 0.0.0.0 listens on every interface.
	ListenIP   string `yaml:"listen_ip"`
	ListenPort int    `yaml:"listen_port"`
	DBSRVIP    string `yaml:"dbsrv_ip"`
	DBSRVPort  int    `yaml:"dbsrv_port"`
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
		Network:      NetworkSettings{ListenIP: "127.0.0.1", ListenPort: 8281, DBSRVIP: "127.0.0.1", DBSRVPort: 7514},
		Channel:      1,
		NewbieZone:   35,
		StatSapphire: 30,
		Login:        LoginSettings{MaxAccountsPerComputer: 3},
		Missions:     MissionSettings{OfferEverySeconds: 1152, TimeToCompleteSeconds: 1152},
		Store:        StoreSettings{HonorEverySeconds: 600},
		MobAI: MobAISettings{
			IdleThinkSeconds:   3,
			BattleThinkSeconds: 2,
		},
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
		key := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if key == "" || key == "-" {
			continue
		}
		v, ok := tree[key]
		if !ok {
			out = append(out, prefix+key)
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
	n := &s.Network
	if net.ParseIP(n.ListenIP) == nil {
		return fmt.Errorf("network.listen_ip must be an IP address, got %q", n.ListenIP)
	}
	if n.DBSRVIP == "" {
		return errors.New("network.dbsrv_ip is empty")
	}
	for name, p := range map[string]int{"network.listen_port": n.ListenPort, "network.dbsrv_port": n.DBSRVPort} {
		if p < 1 || p > 65535 {
			return fmt.Errorf("%s must be 1..65535, got %d", name, p)
		}
	}
	if s.Channel < 1 || s.Channel > 10 {
		return fmt.Errorf("channel must be 1..10, got %d", s.Channel)
	}
	if s.NewbieZone < 0 {
		return fmt.Errorf("newbie_zone must be >= 0, got %d", s.NewbieZone)
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
	return nil
}

// ticks converts seconds to 500 ms server ticks.
func ticks(sec float64) int { return max(int(sec*2), 1) }
