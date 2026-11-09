package game

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dbsrv/internal/wire"
)

// Map attributes (eMapAttribute bits).
const (
	attrVillage    = 1
	attrCantGo     = 2
	attrCantSummon = 4
	attrHouse      = 8
	attrTeleport   = 16
	attrGuild      = 32
	attrPvP        = 64
	attrNewbie     = 128
)

// Teleport is one Teleport.txt row (STRUCT_TELEPORT).
type Teleport struct {
	Price        int32
	SrcX, SrcY   int
	DestX, DestY int
}

// Generator is STRUCT_NPCGENERATOR (one "#" block of NPCGener.txt).
type Generator struct {
	MinuteGenerate int
	MaxNumMob      int
	MinGroup       int
	MaxGroup       int
	SegmentX       [5]int
	SegmentY       [5]int
	SegmentRange   [5]int
	SegmentWait    [5]int
	SegmentAction  [5]string
	FightAction    [4]string
	DieAction      [4]string
	Formation      int
	RouteType      int
	Leader         wire.Mob
	Follower       wire.Mob
	HasLeader      bool
	HasFollower    bool
}

func (d *Data) loadWorldData(dir string) error {
	attr, err := os.ReadFile(filepath.Join(dir, "AttributeMap.dat"))
	if err != nil {
		return err
	}
	if len(attr) < 1024*1024 {
		return fmt.Errorf("AttributeMap.dat: %d bytes, want %d", len(attr), 1024*1024)
	}
	d.Attribute = attr[:1024*1024]
	d.loadLanguage(filepath.Join(dir, "Language.txt"))
	d.Skills = make([]SkillData, maxSkill) // filled by ApplyGameData
	d.loadNPCBase(dir)
	return d.loadGeners(dir)
}

// npcBaseNames is the npc_name table of ReadNPCBase (mounts and summons).
var npcBaseNames = [39]string{
	"Condor", "Javali", "Lobo", "Urso", "Tigre",
	"Gorila", "Dragao_Negro", "Succubus", "", "", "Porco",
	"Javali_", "Lobo_", "Dragao_Menor", "Urso_",
	"Dente_de_Sabre", "Sem_Sela", "Fantasma", "Leve",
	"Equipado", "Andaluz", "Sem_Sela_", "Fantasma_",
	"Leve_", "Equipado_", "Andaluz_", "Fenrir", "Dragao",
	"Grande_Fenrir", "Tigre_de_Fogo", "Dragao_Vermelho",
	"Unicornio", "Pegasus", "Unisus", "Grifo", "Hippo_Grifo",
	"Grifo_Sangrento", "Sleipnir", "Svadilfari",
}

// loadNPCBase is ReadNPCBase: npc_base/<name> (a STRUCT_MOB, shorter files
// are zero-filled like fread into a zeroed table). Missing files leave the
// entry empty (the C++ showed a message box and went on).
func (d *Data) loadNPCBase(dir string) {
	for i, name := range npcBaseNames {
		if name == "" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, "npc_base", name)); err == nil {
			_ = wire.UnmarshalLegacyMob(b, &d.NPCBase[i]) // zero-extends short files
		}
	}
}

// attribute is GetAttribute.
func (d *Data) attribute(x, y int) byte {
	if x < 0 || x > gridSize || y < 0 || y > gridSize {
		return d.Attribute[0]
	}
	return d.Attribute[((y>>2)&0x3FF)*1024+((x>>2)&0x3FF)]
}

// village is GetVillage: the city zone containing (x, y), 5 = none.
func (d *Data) village(x, y int) int {
	for i, c := range d.Cities {
		if int32(x) >= c.CityMinX && int32(x) <= c.CityMaxX && int32(y) >= c.CityMinY && int32(y) <= c.CityMaxY {
			return i
		}
	}
	return 5
}

const maxTeleport = 128 // MAX_TELEPORT

const maxNPCGenerator = 8192 // MAX_NPCGENERATOR

// loadGeners mirrors CNPCGener::ReadNPCGener: "#" starts a generator,
// then "Key: value" lines; Leader/Follower name a file in npc/ holding a
// STRUCT_MOB (only the first 848 bytes are read, as fread did).
func (d *Data) loadGeners(dir string) error {
	raw, err := os.ReadFile(filepath.Join(dir, "NPCGener.txt"))
	if err != nil {
		return err
	}
	mobCache := map[string]*wire.Mob{}
	loadMob := func(name string) (wire.Mob, bool) {
		if m, ok := mobCache[name]; ok {
			if m == nil {
				return wire.Mob{}, false
			}
			return *m, true
		}
		b, err := os.ReadFile(filepath.Join(dir, "npc", name))
		if err != nil {
			mobCache[name] = nil
			return wire.Mob{}, false
		}
		var m wire.Mob
		if wire.UnmarshalLegacyMob(b, &m) != nil {
			mobCache[name] = nil
			return wire.Mob{}, false
		}
		mobCache[name] = &m
		return m, true
	}

	var cur *Generator
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			if len(d.Geners) == maxNPCGenerator {
				break
			}
			d.Geners = append(d.Geners, Generator{})
			cur = &d.Geners[len(d.Geners)-1]
			continue
		}
		if cur == nil || line == "" || line[0] == '/' {
			continue
		}
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		n := atoi(val)
		switch key {
		case "MinuteGenerate":
			cur.MinuteGenerate = n
		case "MaxNumMob":
			cur.MaxNumMob = n
		case "Formation":
			cur.Formation = n
		case "MinGroup":
			cur.MinGroup = n
		case "MaxGroup":
			cur.MaxGroup = n
		case "RouteType":
			cur.RouteType = n
		case "Leader":
			cur.Leader, cur.HasLeader = loadMob(val)
		case "Follower":
			cur.Follower, cur.HasFollower = loadMob(val)
		case "StartX":
			cur.SegmentX[0] = n
		case "StartY":
			cur.SegmentY[0] = n
		case "StartRange":
			cur.SegmentRange[0] = n
		case "StartWait":
			cur.SegmentWait[0] = n
		case "StartAction":
			cur.SegmentAction[0] = val
		case "DestX":
			cur.SegmentX[4] = n
		case "DestY":
			cur.SegmentY[4] = n
		case "DestRange":
			cur.SegmentRange[4] = n
		case "DestWait":
			cur.SegmentWait[4] = n
		case "DestAction":
			cur.SegmentAction[4] = val
		default:
			var seg int
			var field string
			if _, err := fmt.Sscanf(key, "Segment%d%s", &seg, &field); err == nil && seg >= 1 && seg <= 3 {
				switch field {
				case "X":
					cur.SegmentX[seg] = n
				case "Y":
					cur.SegmentY[seg] = n
				case "Wait":
					cur.SegmentWait[seg] = n
				case "Range":
					cur.SegmentRange[seg] = n
				case "Action":
					cur.SegmentAction[seg] = val
				}
			}
			var a int
			if _, err := fmt.Sscanf(key, "FightAction%d", &a); err == nil && a >= 1 && a <= 4 {
				cur.FightAction[a-1] = val
			}
			if _, err := fmt.Sscanf(key, "DieAction%d", &a); err == nil && a >= 1 && a <= 4 {
				cur.DieAction[a-1] = val
			}
		}
	}
	return sc.Err()
}

// distanceTable is g_pDistanceTable.
var distanceTable = [8][8]int{
	{0, 1, 2, 3, 4, 5, 6, 7},
	{1, 1, 2, 3, 4, 5, 6, 7},
	{2, 2, 3, 4, 4, 5, 6, 7},
	{3, 3, 4, 4, 5, 5, 6, 7},
	{4, 4, 4, 5, 5, 5, 6, 7},
	{5, 5, 5, 5, 5, 6, 6, 7},
	{6, 6, 6, 6, 6, 6, 7, 7},
	{7, 7, 7, 7, 7, 7, 7, 7},
}

// distance is GetDistance.
func distance(x1, y1, x2, y2 int) int {
	x, y := x1-x2, y1-y2
	if x < 0 {
		x = -x
	}
	if y < 0 {
		y = -y
	}
	if x <= 7 && y <= 7 {
		return distanceTable[y][x]
	}
	if x > y {
		return x + 1
	}
	return y + 1
}
