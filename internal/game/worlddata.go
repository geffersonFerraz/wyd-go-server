package game

import "dbsrv/internal/wire"

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

// Teleport is one gamedata.teleports row (STRUCT_TELEPORT).
type Teleport struct {
	Price        int32
	SrcX, SrcY   int
	DestX, DestY int
}

// Generator is STRUCT_NPCGENERATOR (one entry of config/npc_generators.yaml).
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

// loadNPCBase is ReadNPCBase: the npc_base templates (a STRUCT_MOB, shorter
// images are zero-filled like fread into a zeroed table). Missing ones leave
// the entry empty (the C++ showed a message box and went on).
func (d *Data) loadNPCBase(files map[string][]byte) {
	for i, name := range npcBaseNames {
		if b, ok := files[name]; ok && name != "" {
			_ = wire.UnmarshalLegacyMob(b, &d.NPCBase[i]) // zero-extends short images
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
