package game

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"

	"dbsrv/internal/wire"
)

// SkillData is STRUCT_SKILLDATA (one row of gamedata.skills).
type SkillData struct {
	Points            int `yaml:"points"`
	Target            int `yaml:"target"`
	Mana              int `yaml:"mana"`
	Delay             int `yaml:"delay"` // cooldown, seconds
	Range             int `yaml:"range"`
	InstanceType      int `yaml:"instance_type"` // 1 holy, 2-5 elemental, 6 heal, 7 calm, 8 cleanse, ...
	InstanceValue     int `yaml:"instance_value"`
	TickType          int `yaml:"tick_type"` // affect applied by SetTick
	TickValue         int `yaml:"tick_value"`
	AffectType        int `yaml:"affect_type"` // affect applied by SetAffect
	AffectValue       int `yaml:"affect_value"`
	Time              int `yaml:"time"` // affect duration (x delay / 100)
	InstanceAttribute int `yaml:"instance_attribute"`
	TickAttribute     int `yaml:"tick_attribute"`
	Aggressive        int `yaml:"aggressive"`
	Maxtarget         int `yaml:"max_target"`
	PartyCheck        int `yaml:"party_check"`
	AffectResist      int `yaml:"affect_resist"`
	PassiveCheck      int `yaml:"passive"`
	ForceDamage       int `yaml:"force_damage"`
}

// SkillEntry is one skilldata.csv row: the id, the name and the raw "Act"
// columns (ignored by the server, kept so nothing is lost on import).
type SkillEntry struct {
	ID        int    `yaml:"id"`
	Name      string `yaml:"name,omitempty"`
	Act1      string `yaml:"act1,omitempty"`
	Act2      string `yaml:"act2,omitempty"`
	SkillData `yaml:",inline"`
}

const maxSkill = 256

// ParseSkillsCSV reads the C++ skilldata.csv (ReadSkillData): commas become
// spaces, then "index points target mana delay range itype ivalue ttype
// tvalue atype avalue time act1 act2 iattr tattr aggressive maxtarget party
// aresist passive force [name]". Rows with fewer than 19 numbers are
// skipped, as sscanf_s returned < 19; a later row for the same id wins.
func ParseSkillsCSV(raw []byte) []SkillEntry {
	byID := map[int]int{}
	var out []SkillEntry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Fields(strings.ReplaceAll(line, ",", " "))
		if len(f) < 21 {
			continue
		}
		idx := atoi(f[0])
		if idx < 0 || idx >= maxSkill {
			continue
		}
		n := func(i int) int {
			if i < len(f) {
				return atoi(f[i])
			}
			return 0
		}
		e := SkillEntry{ID: idx, Act1: f[13], Act2: f[14], SkillData: SkillData{
			Points: n(1), Target: n(2), Mana: n(3), Delay: n(4), Range: n(5),
			InstanceType: n(6), InstanceValue: n(7), TickType: n(8), TickValue: n(9),
			AffectType: n(10), AffectValue: n(11), Time: n(12),
			InstanceAttribute: n(15), TickAttribute: n(16), Aggressive: n(17),
			Maxtarget: n(18), PartyCheck: n(19), AffectResist: n(20),
			PassiveCheck: n(21), ForceDamage: n(22),
		}}
		if last := f[len(f)-1]; len(f) > 22 {
			if _, err := strconv.Atoi(last); err != nil {
				e.Name = wire.DecodeGame(last)
			}
		}
		if at, dup := byID[idx]; dup {
			out[at] = e
		} else {
			byID[idx] = len(out)
			out = append(out, e)
		}
	}
	return out
}

// hitRate is g_pHitRate (BASE_InitializeHitRate).
var hitRate = func() [1024]int {
	var t [1024]int
	v, phase := 0, 0
	for step := 512; step != 0; step /= 2 {
		for i := range t {
			if t[i] != 0 {
				continue
			}
			switch phase {
			case 0:
				t[i] = v
			case 1:
				t[i] = 512 - v
			case 2:
				t[i] = v + 512
			default:
				t[i] = 1024 - v
			}
			if t[i] > 999 {
				t[i] = 999
			}
			phase++
			if phase >= 4 {
				phase = 0
			}
			if phase == 0 {
				v++
			}
		}
	}
	t[0] = 512
	return t
}()
