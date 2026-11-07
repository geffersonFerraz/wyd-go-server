package legacy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbsrv/internal/wire"
)

// Real data from the C++ server, used read-only.
const dbsrvDir = "../../../Release Servidor/DBSRV"

func needLegacyData(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(dbsrvDir); err != nil {
		t.Skipf("legacy DBSRV data not available: %v", err)
	}
}

func TestParseAnderson(t *testing.T) {
	needLegacyData(t)
	acc, err := ReadAccountFile(filepath.Join(dbsrvDir, "account/A/ANDERSON.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range acc.Warnings {
		t.Logf("warning: %s", w)
	}
	a := &acc.Wire
	check := func(name string, got, want any) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("username", wire.CString(a.Username[:]), "ANDERSON")
	check("password", acc.Password, "110389")
	check("numeric pass", acc.NumericPass, "1103")
	check("cash", a.Cash, uint32(13300))
	check("novato", a.Novato, uint32(1))
	check("charslot", a.CharSlot, int16(-1))
	check("position", a.Position, wire.Position{X: 1180, Y: 1742})
	check("water.day", a.Water.Day, int32(324))
	check("divina", a.Divina, wire.ItemDate{Dia: 16, Mes: 12, Ano: 2024, Hora: 3, Minuto: 23, Segundo: 28})
	check("storage[39]", a.StorageItems[39].Index, uint16(4201))

	m := &a.Mob[0]
	check("mob0.name", wire.CString(m.Player.Name[:]), "DragonBlue")
	check("mob0.guild", m.Player.GuildIndex, uint16(6))
	check("mob0.gold", m.Player.Gold, int32(1322968120))
	check("mob0.exp", m.Player.Exp, int64(1758425611))
	check("mob0.info", m.Player.Info, uint8(192))
	check("mob0.affectInfo", m.Player.AffectInfo, uint8(64))
	check("mob0.last", m.Player.Last, wire.Position{X: 2096, Y: 2096})
	check("mob0.level", m.Player.BStatus.Level, int16(351))
	check("mob0.int", m.Player.BStatus.INT, uint16(1945))
	check("mob0.move", m.Player.BStatus.Move, uint8(82))
	check("mob0.mastery3", m.Player.BStatus.Mastery[3], uint16(255))
	check("mob0.curDefense", m.Player.Status.Defense, int32(3837))
	ef := func(i, v uint8) wire.ItemEffect { return wire.ItemEffect{Index: i, Value: v} }
	check("mob0.equip13", m.Player.Equip[13], wire.Item{Index: 3908, Effect: [3]wire.ItemEffect{ef(106, 6), ef(107, 13), ef(108, 7)}})
	check("mob0.inv63", m.Player.Inventory[63], wire.Item{Index: 547, Effect: [3]wire.ItemEffect{ef(150, 0), ef(0, 0), ef(77, 0)}})
	check("mob0.inv62 (absent)", m.Player.Inventory[62], wire.Item{})
	check("mob0.learn", m.Player.Learn[0], uint32(419364904))
	check("mob0.skillPoint", m.Player.SkillPoint, uint16(339))
	check("mob0.critical", m.Player.Critical, uint8(97))
	check("mob0.saveMana", m.Player.SaveMana, uint8(38))
	check("mob0.skillbar1", m.Player.SkillBar1, [4]uint8{22, 248, 3, 5})
	check("mob0.guildMemberType", m.Player.GuildMemberType, int8(9))
	check("mob0.affect1", m.Affects[1], wire.Affect{Index: 14, Master: 10, Value: 230, Time: 29})
	check("mob0.hallEnter", m.HallEnter, uint32(19))
	check("mob0.skillbar2[1]", m.SkillBar[1], uint8(23))
}

// Every account file the C++ server wrote must parse.
func TestParseAllAccounts(t *testing.T) {
	needLegacyData(t)
	files, _ := filepath.Glob(filepath.Join(dbsrvDir, "account", "*", "*.xml"))
	if len(files) == 0 {
		t.Skip("no account files")
	}
	names := map[string]string{}
	for _, f := range files {
		if filepath.Base(f) == ".xml" {
			continue // empty account rewritten every second by ProcessSecTimer
		}
		acc, err := ReadAccountFile(f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		user := wire.CString(acc.Wire.Username[:])
		if want := strings.TrimSuffix(strings.ToUpper(filepath.Base(f)), ".XML"); user != want {
			t.Errorf("%s: username %q does not match file name", f, user)
		}
		for i := range acc.Wire.Mob {
			n := strings.ToLower(wire.GameString(acc.Wire.Mob[i].Player.Name[:]))
			if n == "" {
				continue
			}
			if other, dup := names[n]; dup && other != user {
				t.Logf("character name %q used by both %s and %s (import reports this)", n, other, user)
			}
			names[n] = user
		}
		for _, w := range acc.Warnings {
			t.Logf("%s: %s", user, w)
		}
	}
	t.Logf("parsed %d accounts, %d characters", len(files), len(names))
}

func TestReadTextFiles(t *testing.T) {
	needLegacyData(t)
	guilds, warns, err := ReadGuilds(filepath.Join(dbsrvDir, "guilds.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warns {
		t.Log(w)
	}
	byID := map[int]Guild{}
	for _, g := range guilds {
		byID[g.ID] = g
	}
	if g := byID[5]; g.Name != "TesteGuildBlue" || g.Fame != 100 || g.Kingdom != 7 || g.Citizen != 1 {
		t.Errorf("guild 5 = %+v", g)
	}
	if g := byID[1]; g.Name != "ANGELS" || g.SubGuild != [3]string{} {
		t.Errorf("guild 1 = %+v", g)
	}
	if _, ok := byID[0]; ok {
		t.Error("placeholder guild 0 should be skipped")
	}

	cfg, err := ReadConfig(filepath.Join(dbsrvDir, "Config.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSeal != 2 {
		t.Errorf("LastSeal = %d", cfg.LastSeal)
	}

	idx, err := ReadCharIndex(filepath.Join(dbsrvDir, "char"))
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) == 0 {
		t.Error("empty char index")
	}
}

func TestDecodeWindows1252(t *testing.T) {
	doc := []byte("<account><username>JO\xc3O</username><mob index=\"0\"><player><name>Ma\xe7\xe3</name></player></mob></account>")
	acc, err := ParseAccountXML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := wire.GameString(acc.Wire.Mob[0].Player.Name[:]); got != "Maçã" {
		t.Errorf("name = %q", got)
	}
	if acc.Wire.Mob[0].Player.Name[2] != 0xe7 {
		t.Error("wire bytes should stay Windows-1252")
	}
}
