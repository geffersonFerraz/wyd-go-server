package game

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// The world tables that used to be text files of the C++ TMSRV. The small
// ones are sections of tmsrv.yaml; the two big ones live in the config
// folder (ConfigFiles). The NPC generators are in the database. Text is UTF-8 in the YAML; the fields the game sends
// as they are keep the client's Windows-1252 in memory.

// ConfigFiles are the files of the config folder (-config-dir).
var ConfigFiles = []string{"language.yaml", "missions.yaml"}

// CitySettings is one city (formerly a row of guild_zone.txt).
type CitySettings struct {
	City           int      `yaml:"city"`
	Owner          int32    `yaml:"owner"` // first start only: the owners live in the database
	Tower          [2]int32 `yaml:"tower"`
	GuildArea      [2]int32 `yaml:"guild_area"`
	Center         [2]int32 `yaml:"center"`
	AreaMin        [2]int32 `yaml:"area_min"`
	AreaMax        [2]int32 `yaml:"area_max"`
	WarMin         [2]int32 `yaml:"war_min"`
	WarMax         [2]int32 `yaml:"war_max"`
	WarDefenders   [2]int32 `yaml:"war_defenders"`
	WarChallengers [2]int32 `yaml:"war_challengers"`
	TaxesCollected int64    `yaml:"taxes_collected"`
	TaxPercent     int32    `yaml:"tax_percent"`
	Wins           int32    `yaml:"wins"`
}

// InitItemSettings is a gate, barrier or door (formerly InitItem.csv).
type InitItemSettings struct {
	Item   int `yaml:"item"`
	X      int `yaml:"x"`
	Y      int `yaml:"y"`
	Rotate int `yaml:"rotate"`
}

// NPCEventSettings is an exchange NPC (formerly Data/NPCEvento.txt).
type NPCEventSettings struct {
	Event    int                   `yaml:"event"`
	Name     string                `yaml:"name"`
	Gold     int                   `yaml:"gold_required,omitempty"`
	Required []NPCEventRequirement `yaml:"required,omitempty"`
	Tiers    []NPCEventTier        `yaml:"tiers,omitempty"`
}

// NPCEventRequirement is an item the player must hand in.
type NPCEventRequirement struct {
	Slot    int      `yaml:"slot"`
	Item    int      `yaml:"item"`
	Amount  int      `yaml:"amount,omitempty"`
	Effects [][2]int `yaml:"effects,omitempty"`
}

// NPCEventTier is one prize tier: given when Rand()%300 <= Rate.
type NPCEventTier struct {
	Tier     int             `yaml:"tier"`
	Rate     int             `yaml:"rate,omitempty"`
	Teleport *[2]int         `yaml:"teleport,omitempty"`
	Message  string          `yaml:"message,omitempty"`
	Prizes   []NPCEventPrize `yaml:"prizes,omitempty"`
}

// NPCEventPrize is one item of a tier.
type NPCEventPrize struct {
	Slot    int      `yaml:"slot"`
	Item    int      `yaml:"item"`
	Effects [][2]int `yaml:"effects,omitempty"`
}

// DailyRewardSettings is one item of a weekday (formerly Data/DailyReward.txt).
type DailyRewardSettings struct {
	Day     int      `yaml:"day"`
	Slot    int      `yaml:"slot"`
	Item    int      `yaml:"item"`
	Effects [][2]int `yaml:"effects,omitempty"`
}

// RouletteSettings is a box of /roleta (formerly Conf/Roleta.txt).
type RouletteSettings struct {
	Box       int                    `yaml:"box"`
	Name      string                 `yaml:"name,omitempty"`
	Enabled   bool                   `yaml:"enabled"`
	SuperDrop bool                   `yaml:"super_drop"`
	KeyID     int                    `yaml:"key_id"`
	ShowSlots [11]int                `yaml:"show_slots"`
	Rolls     []RouletteRollSettings `yaml:"rolls"`
}

// RouletteRollSettings is one prize of a box.
type RouletteRollSettings struct {
	Item     int      `yaml:"item"`
	Effects  [][2]int `yaml:"effects,omitempty"`
	Chance   int      `yaml:"chance"`
	Announce int      `yaml:"announce"`
	Rare     int      `yaml:"rare"`
	Super    int      `yaml:"super"`
	EvolBar  int      `yaml:"evol_bar"`
}

// LanguageEntry is one message (formerly a line of Language.txt).
type LanguageEntry struct {
	ID   int    `yaml:"id"`
	Key  string `yaml:"key,omitempty"`
	Text string `yaml:"text"`
}

type languageFile struct {
	Messages []LanguageEntry `yaml:"messages"`
}

// MissionDef is one mission (formerly Data/Missions.xml).
type MissionDef struct {
	ID      int             `yaml:"id"`
	Name    string          `yaml:"name"`
	Classes []int           `yaml:"classes,omitempty"`
	Level   *[2]int         `yaml:"level,omitempty"`
	Mobs    []MissionMob    `yaml:"mobs,omitempty"`
	Drops   []MissionDrop   `yaml:"drops,omitempty"`
	Gold    int             `yaml:"gold"`
	Exp     int             `yaml:"exp"`
	Free    []MissionReward `yaml:"free,omitempty"`
	Pass    []MissionReward `yaml:"pass,omitempty"`
}

// MissionMob is a monster to kill.
type MissionMob struct {
	Slot  int    `yaml:"slot"`
	Name  string `yaml:"name"`
	Total int    `yaml:"total"`
}

// MissionDrop is an item to collect.
type MissionDrop struct {
	Slot  int `yaml:"slot"`
	Item  int `yaml:"item"`
	Total int `yaml:"total"`
}

// MissionReward is one reward item.
type MissionReward struct {
	Slot    int      `yaml:"slot"`
	Item    int      `yaml:"item"`
	Amount  int      `yaml:"amount"`
	Effects [][2]int `yaml:"effects,omitempty"`
}

type missionsFile struct {
	Missions []MissionDef `yaml:"missions"`
}

// GeneratorSettings is one NPC generator in the old YAML file (formerly a
// block of NPCGener.txt); its position in the list is its id.
type GeneratorSettings struct {
	ID           int                `yaml:"id"`
	Minute       int                `yaml:"minute"`
	MaxMobs      int                `yaml:"max_mobs"`
	MinGroup     int                `yaml:"min_group"`
	MaxGroup     int                `yaml:"max_group"`
	Formation    int                `yaml:"formation"`
	Route        int                `yaml:"route"`
	Leader       string             `yaml:"leader,omitempty"`
	Follower     string             `yaml:"follower,omitempty"`
	Segments     []GeneratorSegment `yaml:"segments,omitempty"`
	FightActions []string           `yaml:"fight_actions,omitempty"`
	DieActions   []string           `yaml:"die_actions,omitempty"`
}

// GeneratorSegment is a point of the route: 0 the start, 4 the destination.
type GeneratorSegment struct {
	Segment int    `yaml:"segment"`
	X       int    `yaml:"x"`
	Y       int    `yaml:"y"`
	Range   int    `yaml:"range,omitempty"`
	Wait    int    `yaml:"wait,omitempty"`
	Action  string `yaml:"action,omitempty"`
}

type generatorsFile struct {
	Generators []GeneratorSettings `yaml:"generators"`
}

// validateWorldTables checks the world sections of tmsrv.yaml.
func (s *Settings) validateWorldTables() error {
	if len(s.Cities) != len(Data{}.Cities) {
		return fmt.Errorf("cities: %d rows, want %d", len(s.Cities), len(Data{}.Cities))
	}
	seen := map[int]bool{}
	for i, c := range s.Cities {
		if c.City < 0 || c.City >= len(Data{}.Cities) || seen[c.City] {
			return fmt.Errorf("cities[%d]: bad or repeated city %d", i, c.City)
		}
		seen[c.City] = true
		if c.TaxPercent < 0 || c.TaxPercent > 100 {
			return fmt.Errorf("cities[%d]: tax_percent must be 0..100, got %d", i, c.TaxPercent)
		}
	}
	if len(s.InitItems) >= maxGroundItem {
		return fmt.Errorf("init_items: %d rows, at most %d", len(s.InitItems), maxGroundItem-1)
	}
	for i, it := range s.InitItems {
		if err := validateItem(fmt.Sprintf("init_items[%d]", i), it.Item, nil); err != nil {
			return err
		}
	}
	for i, ev := range s.NPCEvents {
		if ev.Event < 0 || ev.Event >= maxNPCEvent {
			return fmt.Errorf("npc_events[%d]: event must be 0..%d, got %d", i, maxNPCEvent-1, ev.Event)
		}
		for _, r := range ev.Required {
			if r.Slot < 0 || r.Slot >= 10 || r.Amount < 0 {
				return fmt.Errorf("npc_events[%d]: bad requirement slot %d amount %d", i, r.Slot, r.Amount)
			}
			if err := validateOptionalItem(fmt.Sprintf("npc_events[%d].required", i), r.Item, r.Effects); err != nil {
				return err
			}
		}
		for _, t := range ev.Tiers {
			if t.Tier < 0 || t.Tier >= 10 {
				return fmt.Errorf("npc_events[%d]: tier must be 0..9, got %d", i, t.Tier)
			}
			for _, p := range t.Prizes {
				if p.Slot < 0 || p.Slot >= 10 {
					return fmt.Errorf("npc_events[%d]: prize slot must be 0..9, got %d", i, p.Slot)
				}
				if err := validateOptionalItem(fmt.Sprintf("npc_events[%d].prizes", i), p.Item, p.Effects); err != nil {
					return err
				}
			}
		}
	}
	for i, r := range s.DailyReward {
		if r.Day < 0 || r.Day >= 7 || r.Slot < 0 || r.Slot >= 4 {
			return fmt.Errorf("daily_reward[%d]: day must be 0..6 and slot 0..3, got %d/%d", i, r.Day, r.Slot)
		}
		if err := validateOptionalItem(fmt.Sprintf("daily_reward[%d]", i), r.Item, r.Effects); err != nil {
			return err
		}
	}
	for i, b := range s.Roulette {
		if b.Box < 0 || b.Box >= 20 || len(b.Rolls) > maxRouletteItem {
			return fmt.Errorf("roulette[%d]: box must be 0..19 with at most %d rolls", i, maxRouletteItem)
		}
		for j, r := range b.Rolls {
			if err := validateOptionalItem(fmt.Sprintf("roulette[%d].rolls[%d]", i, j), r.Item, r.Effects); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateOptionalItem is validateItem for tables where 0 is an empty slot.
func validateOptionalItem(where string, item int, effects [][2]int) error {
	if item == 0 {
		item = 1 // only the effects are left to check
	}
	return validateItem(where, item, effects)
}

// applyWorldTables copies the world sections of tmsrv.yaml into the data.
func (d *Data) applyWorldTables(s *Settings) {
	for _, c := range s.Cities {
		d.Cities[c.City] = CityZone{OwnerIndex: c.Owner,
			TowerX: c.Tower[0], TowerY: c.Tower[1], AreaGuildX: c.GuildArea[0], AreaGuildY: c.GuildArea[1],
			CityX: c.Center[0], CityY: c.Center[1], CityMinX: c.AreaMin[0], CityMinY: c.AreaMin[1],
			CityMaxX: c.AreaMax[0], CityMaxY: c.AreaMax[1], WarMinX: c.WarMin[0], WarMinY: c.WarMin[1],
			WarMaxX: c.WarMax[0], WarMaxY: c.WarMax[1], GuildAWarX: c.WarDefenders[0], GuildAWarY: c.WarDefenders[1],
			GuildBWarX: c.WarChallengers[0], GuildBWarY: c.WarChallengers[1],
			Impost: c.TaxesCollected, PercImpost: c.TaxPercent, WinCount: c.Wins}
	}
	d.InitItems = nil
	for _, it := range s.InitItems {
		d.InitItems = append(d.InitItems, InitItemFile{Index: it.Item, X: it.X, Y: it.Y, Rotate: it.Rotate})
	}
	for _, e := range s.NPCEvents {
		ev := &d.NPCEvents[e.Event]
		ev.Name, ev.GoldRequired = wire.EncodeGame(e.Name), e.Gold
		for _, r := range e.Required {
			ev.ItemRequired[r.Slot], ev.AmountRequired[r.Slot] = makeItem(r.Item, r.Effects), r.Amount
		}
		for _, t := range e.Tiers {
			ev.Rates[t.Tier], ev.Msg[t.Tier] = t.Rate, wire.EncodeGame(t.Message)
			if t.Teleport != nil {
				ev.Teleport[t.Tier] = *t.Teleport
			}
			for _, p := range t.Prizes {
				ev.ItemEarned[t.Tier][p.Slot] = makeItem(p.Item, p.Effects)
			}
		}
	}
	for _, r := range s.DailyReward {
		d.DailyReward[r.Day][r.Slot] = makeItem(r.Item, r.Effects)
	}
	for _, b := range s.Roulette {
		box := &d.Roulette[b.Box]
		box.Name, box.Status, box.HasSuperDrop, box.KeyID = wire.EncodeGame(b.Name), b.Enabled, b.SuperDrop, int8(b.KeyID)
		for i, v := range b.ShowSlots {
			box.ShowSlots[i] = int8(v)
		}
		for i, r := range b.Rolls {
			box.Roll[i] = makeItem(r.Item, r.Effects)
			box.Meta[i] = RollMeta{Chance: r.Chance, SendMsg: r.Announce, Rare: r.Rare, Super: r.Super, EvolBar: r.EvolBar}
		}
	}
	d.ExpBonus = s.ExpBonus
}

// loadConfigFile reads one file of the config folder; unknown keys are an
// error, as in tmsrv.yaml.
func loadConfigFile(dir, name string, out any) error {
	path := filepath.Join(dir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// loadLanguage reads config/language.yaml.
func (d *Data) loadLanguage(dir string) error {
	var f languageFile
	if err := loadConfigFile(dir, "language.yaml", &f); err != nil {
		return err
	}
	d.Lang = make(map[int]string, len(f.Messages))
	for _, m := range f.Messages {
		d.Lang[m.ID] = m.Text
	}
	return nil
}

// loadMissions reads config/missions.yaml.
func (d *Data) loadMissions(dir string) error {
	var f missionsFile
	if err := loadConfigFile(dir, "missions.yaml", &f); err != nil {
		return err
	}
	d.Missions = make([]Mission, 0, len(f.Missions))
	for i, xm := range f.Missions {
		where := fmt.Sprintf("missions.yaml: missions[%d]", i)
		m := Mission{ID: xm.ID, Name: xm.Name, Classes: xm.Classes, Gold: xm.Gold, Exp: xm.Exp}
		if xm.Level != nil {
			m.MinLevel, m.MaxLevel, m.HasLevel = xm.Level[0], xm.Level[1], true
		}
		for _, mob := range xm.Mobs {
			if mob.Slot < 0 || mob.Slot >= len(m.MobName) {
				return fmt.Errorf("%s: mob slot must be 0..4, got %d", where, mob.Slot)
			}
			m.MobName[mob.Slot], m.MobAmount[mob.Slot] = mob.Name, mob.Total
		}
		for _, dr := range xm.Drops {
			if dr.Slot < 0 || dr.Slot >= len(m.DropItem) {
				return fmt.Errorf("%s: drop slot must be 0..4, got %d", where, dr.Slot)
			}
			m.DropItem[dr.Slot], m.DropAmount[dr.Slot] = dr.Item, dr.Total
		}
		read := func(src []MissionReward, dst *[6]wire.Item, amount *[6]int) error {
			for _, r := range src {
				if r.Slot < 0 || r.Slot >= len(dst) || r.Amount < 0 || r.Amount > 120 {
					return fmt.Errorf("%s: reward slot must be 0..5 and amount 0..120, got %d/%d", where, r.Slot, r.Amount)
				}
				if err := validateItem(where, r.Item, r.Effects); err != nil {
					return err
				}
				dst[r.Slot], amount[r.Slot] = makeItem(r.Item, r.Effects), r.Amount
			}
			return nil
		}
		if err := read(xm.Free, &m.Free, &m.FreeAmount); err != nil {
			return err
		}
		if err := read(xm.Pass, &m.Pass, &m.PassAmount); err != nil {
			return err
		}
		d.Missions = append(d.Missions, m)
	}
	return nil
}

// GeneratorsFile is the file the generators lived in before they went to the
// database (gamedata.npc_generators). It is only read to fill an empty table.
const GeneratorsFile = "npc_generators.yaml"

// ReadGeneratorsFile reads a generators YAML (the old config file, or the one
// inside a version 2 server export).
func ReadGeneratorsFile(path string) ([]store.Generator, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseGenerators(raw)
}

// ParseGenerators decodes the generators YAML.
func ParseGenerators(raw []byte) ([]store.Generator, error) {
	var f generatorsFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", GeneratorsFile, err)
	}
	out := make([]store.Generator, len(f.Generators))
	for i, s := range f.Generators {
		if s.ID != i {
			return nil, fmt.Errorf("%s: generators[%d] has id %d", GeneratorsFile, i, s.ID)
		}
		g := store.Generator{ID: s.ID, Minute: s.Minute, MaxMobs: s.MaxMobs, MinGroup: s.MinGroup, MaxGroup: s.MaxGroup,
			Formation: s.Formation, Route: s.Route, Leader: s.Leader, Follower: s.Follower,
			FightActions: s.FightActions, DieActions: s.DieActions}
		for _, seg := range s.Segments {
			g.Segments = append(g.Segments, store.GeneratorSegment{Segment: seg.Segment, X: seg.X, Y: seg.Y, Range: seg.Range, Wait: seg.Wait, Action: seg.Action})
		}
		if err := g.Check(); err != nil {
			return nil, fmt.Errorf("%s: %w", GeneratorsFile, err)
		}
		out[i] = g
	}
	return out, nil
}

// ApplyGenerators builds the generator list from the database rows: the id
// is the position, and an id without a row is an empty generator. A leader
// or follower with no template leaves the generator without it, as in the
// C++.
func (d *Data) ApplyGenerators(rows []store.Generator) error {
	geners, err := d.buildGenerators(rows)
	if err != nil {
		return err
	}
	d.Geners = geners
	return nil
}

func (d *Data) buildGenerators(rows []store.Generator) ([]Generator, error) {
	size := 0
	for i := range rows {
		if err := rows[i].Check(); err != nil {
			return nil, err
		}
		size = max(size, rows[i].ID+1)
	}
	mobs := map[string]*wire.Mob{}
	load := func(name string) (wire.Mob, bool) {
		if name == "" {
			return wire.Mob{}, false
		}
		m, ok := mobs[name]
		if !ok {
			m = d.npcTemplate(wire.EncodeGame(name))
			mobs[name] = m
		}
		if m == nil {
			return wire.Mob{}, false
		}
		return *m, true
	}
	geners := make([]Generator, size)
	for _, s := range rows {
		g := &geners[s.ID]
		g.MinuteGenerate, g.MaxNumMob, g.MinGroup, g.MaxGroup, g.Formation, g.RouteType = s.Minute, s.MaxMobs, s.MinGroup, s.MaxGroup, s.Formation, s.Route
		g.Leader, g.HasLeader = load(s.Leader)
		g.Follower, g.HasFollower = load(s.Follower)
		for _, seg := range s.Segments {
			n := seg.Segment
			g.SegmentX[n], g.SegmentY[n], g.SegmentRange[n], g.SegmentWait[n], g.SegmentAction[n] = seg.X, seg.Y, seg.Range, seg.Wait, wire.EncodeGame(seg.Action)
		}
		for a, v := range s.FightActions {
			g.FightAction[a] = wire.EncodeGame(v)
		}
		for a, v := range s.DieActions {
			g.DieAction[a] = wire.EncodeGame(v)
		}
	}
	return geners, nil
}

// setWorldAssets keeps the binary world files that came from the database.
func (d *Data) setWorldAssets(w *store.WorldAssets) error {
	height, err := w.Require(store.AssetHeightMap, gridSize*gridSize)
	if err != nil {
		return err
	}
	attribute, err := w.Require(store.AssetAttributeMap, 1024*1024)
	if err != nil {
		return err
	}
	// Gates and doors write into the height map: each Data has its own copy.
	d.Height, d.Attribute = slices.Clone(height), slices.Clone(attribute)
	if len(w.TemplatesOf(store.TemplateNPC)) == 0 {
		return errors.New("the database has no NPC templates: import a server export in the panel, or run: admin world-import")
	}
	d.setNPCTemplates(w.TemplatesOf(store.TemplateNPC), w.TemplatesOf(store.TemplateNPCBase))
	return nil
}

// setNPCTemplates installs the template images: the generators' monsters and
// merchants (npc) and the mounts and summons (npc_base).
func (d *Data) setNPCTemplates(npc, base map[string][]byte) {
	d.npcFiles = npc
	d.NPCBase = [50]wire.Mob{}
	d.loadNPCBase(base)
	d.StoreBase, d.Thorn = d.npcTemplate("Base"), d.npcTemplate("Vinha")
}

// CheckClassTemplates checks the images DBSRV starts new characters from.
func CheckClassTemplates(files map[string][]byte) error {
	_, err := ClassTemplates(files)
	return err
}

// ClassNames are the class templates, in class order.
var ClassNames = [4]string{"TK", "FM", "BM", "HT"}

// ClassTemplates decodes the four class templates (C++ ReadCharBase).
func ClassTemplates(files map[string][]byte) ([4]wire.Mob, error) {
	var base [4]wire.Mob
	for i, name := range ClassNames {
		raw, ok := files[name]
		if !ok {
			return base, fmt.Errorf("the database has no class template %s: import a server export in the panel, or run: admin world-import", name)
		}
		if len(raw) != 848 { // the template keeps the 32-bit layout
			return base, fmt.Errorf("class template %s: %d bytes, want 848 (sizeof STRUCT_MOB)", name, len(raw))
		}
		if err := wire.UnmarshalLegacyMob(raw, &base[i]); err != nil {
			return base, err
		}
	}
	return base, nil
}

// IsNPCBaseName reports whether a template name is read as npc_base.
func IsNPCBaseName(name string) bool {
	return name != "" && slices.Contains(npcBaseNames[:], name)
}
