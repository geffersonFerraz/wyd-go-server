package legacy

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"dbsrv/internal/wire"
)

// Seal is one seal/<id>.xml (CSealSoul.cpp).
type Seal struct {
	ID       int
	Info     wire.SealInfo // Skills by their XML index; -1 kept as written
	Char     wire.CharInfo
	Warnings []string
}

func ReadSealFile(path string, id int) (*Seal, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var x xSeal
	if err := decodeXML(raw, &x); err != nil {
		return nil, fmt.Errorf("xml: %w", err)
	}
	p := &parser{}
	s := &Seal{ID: id}
	s.Info = wire.SealInfo{
		Status:    int32(id),
		CON:       int32(p.num("CON", x.CON)),
		STR:       int32(p.num("STR", x.STR)),
		DEX:       int32(p.num("DEX", x.DEX)),
		INT:       int32(p.num("INT", x.INT)),
		Face:      int16(p.num("face", x.Face)),
		Level:     int16(p.num("level", x.Level)),
		QuestInfo: int16(p.num("quest", x.Quest)),
		Evolution: int16(p.num("evolution", x.Evolution)),
		CapeID:    int16(p.num("cape", x.Cape)),
		Unk3:      int16(p.num("unk3", x.Unk3)),
	}
	for i := range s.Info.Skills {
		s.Info.Skills[i] = -1
	}
	for _, sk := range x.Skills {
		i := int(p.num("skill@index", sk.Index))
		if i < 0 || i >= len(s.Info.Skills) {
			p.warn("skill index %d out of range", i)
			continue
		}
		s.Info.Skills[i] = int16(p.num("skill@value", sk.Value))
	}
	p.charInfo("mob", &x.Mob, &s.Char)
	s.Warnings = p.warnings
	return s, nil
}

// Guild is one guilds.txt line.
type Guild struct {
	ID       int
	Fame     int
	Kingdom  int
	Citizen  int
	Wins     int
	Name     string
	SubGuild [3]string
}

// ReadGuilds parses guilds.txt: "id, fame, kingdom, citizen, wins, name, sub1, sub2, sub3"
// (the column comment in the file omits wins). Guild 0 is a placeholder row
// written by WriteGuilds and is skipped.
func ReadGuilds(path string) ([]Guild, []string, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, nil, err
	}
	var out []Guild
	var warns []string
	for n, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := splitCSV(line)
		if len(f) < 6 {
			warns = append(warns, fmt.Sprintf("guilds.txt:%d: expected at least 6 fields: %q", n+1, line))
			continue
		}
		nums := make([]int, 5)
		bad := false
		for i := range nums {
			v, err := strconv.Atoi(f[i])
			if err != nil {
				bad = true
			}
			nums[i] = v
		}
		if bad {
			warns = append(warns, fmt.Sprintf("guilds.txt:%d: bad number: %q", n+1, line))
			continue
		}
		g := Guild{ID: nums[0], Fame: max(nums[1], 0), Kingdom: nums[2], Citizen: nums[3], Wins: nums[4], Name: f[5]}
		if g.ID <= 0 || g.ID >= 65535 || g.Name == "" {
			continue
		}
		for i := 0; i < 3 && 6+i < len(f); i++ {
			g.SubGuild[i] = f[6+i]
		}
		out = append(out, g)
	}
	return out, warns, nil
}

// DonateSlot is one Donate.txt line.
type DonateSlot struct {
	Store, Slot int
	Available   int
	Price       int64
	Loop        int
	Item        wire.Item
}

// ReadDonate parses Donate.txt: "store, slot, avaible, price, loop, itemId, ef1, efv1, ef2, efv2, ef3, efv3".
// A missing file is not an error (the server just starts with an empty store).
func ReadDonate(path string) ([]DonateSlot, []string, error) {
	lines, err := readLines(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var out []DonateSlot
	var warns []string
	for n, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := splitCSV(line)
		v := make([]int64, 12)
		ok := 0
		for i := 0; i < len(f) && i < 12; i++ {
			x, err := strconv.ParseInt(f[i], 10, 64)
			if err != nil {
				break
			}
			v[i] = x
			ok++
		}
		if ok < 6 {
			warns = append(warns, fmt.Sprintf("Donate.txt:%d: unparseable: %q", n+1, line))
			continue
		}
		// C++ accepted only stores 0..4 even though the array has 10.
		if v[0] < 0 || v[0] >= 10 || v[1] < 0 || v[1] >= 27 {
			warns = append(warns, fmt.Sprintf("Donate.txt:%d: store/slot out of range: %q", n+1, line))
			continue
		}
		out = append(out, DonateSlot{
			Store: int(v[0]), Slot: int(v[1]), Available: int(v[2]), Price: v[3], Loop: int(v[4]),
			Item: wire.Item{Index: uint16(v[5]), Effect: [3]wire.ItemEffect{
				{Index: uint8(v[6]), Value: uint16(v[7])},
				{Index: uint8(v[8]), Value: uint16(v[9])},
				{Index: uint8(v[10]), Value: uint16(v[11])},
			}},
		})
	}
	return out, warns, nil
}

// Config is Config.txt ("Sapphire N", "LastSeal N").
type Config struct {
	Sapphire int
	LastSeal int
	// SapphireLoaded reports whether the C++ ReadConfig would actually have
	// read Sapphire: it alternated fgets/fscanf, so it only saw every other
	// line and silently skipped the first one.
	SapphireLoaded bool
}

func ReadConfig(path string) (Config, error) {
	lines, err := readLines(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		v, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		switch f[0] {
		case "Sapphire":
			c.Sapphire = v
			c.SapphireLoaded = true
		case "LastSeal":
			c.LastSeal = v
		}
	}
	return c, nil
}

// ReadCharIndex reads char/<L>/<NAME> files: 16 bytes holding the owning
// account name. Returns NAME (upper-cased) -> ACCOUNT.
func ReadCharIndex(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != "" {
			return nil // skips GeradorDePastas.exe and the like
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[strings.ToUpper(d.Name())] = strings.ToUpper(wire.GameString(raw))
		return nil
	})
	return out, err
}

func readLines(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		lines = append(lines, strings.TrimSpace(wire.GameString(sc.Bytes())))
	}
	return lines, sc.Err()
}

func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
