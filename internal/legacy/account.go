package legacy

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"dbsrv/internal/wire"
)

// Account is one parsed account/<L>/<NAME>.xml.
type Account struct {
	Wire        wire.Account // Password and SecondPass left empty
	Password    string       // as stored, including any '@' '_' '#' status prefix
	NumericPass string       // <token>, the numeric second password
	Warnings    []string
}

// ReadAccountFile parses one account XML file.
func ReadAccountFile(path string) (*Account, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseAccountXML(raw)
}

// decodeXML unmarshals pugixml output. pugixml writes char[] bytes verbatim
// and the game uses Windows-1252, so files with accented names are not valid
// UTF-8; transcode those first.
func decodeXML(raw []byte, v any) error {
	if !utf8.Valid(raw) {
		dec, err := charmap.Windows1252.NewDecoder().Bytes(raw)
		if err != nil {
			return err
		}
		raw = dec
	}
	d := xml.NewDecoder(bytes.NewReader(raw))
	return d.Decode(v)
}

// ParseAccountXML converts an account document to wire.Account, mirroring
// XMLToStructure(pugi::xml_node, STRUCT_ACCOUNT*) but reading every field
// AppendStructure writes (the C++ reader skipped critical, saveMana,
// charslot, position and sub equip[1]).
func ParseAccountXML(raw []byte) (*Account, error) {
	var x xAccount
	if err := decodeXML(raw, &x); err != nil {
		return nil, fmt.Errorf("xml: %w", err)
	}
	p := &parser{}
	out := &Account{}
	a := &out.Wire

	wire.PutGameString(a.Username[:], strings.ToUpper(strings.TrimSpace(x.Username)))
	out.Password = x.Password
	out.NumericPass = x.Token

	a.Cash = uint32(p.num("cash", x.Cash))
	a.Honra = uint32(p.num("honra", x.Honra))
	a.Novato = uint32(p.num("novato", x.Novato))
	a.AlreadyReset = strings.TrimSpace(x.AlreadyReseted) != "" && strings.TrimSpace(x.AlreadyReseted) != "0"
	a.BanType = uint32(p.num("ban.type", x.BanType))
	a.Ban = p.date("ban.date", x.BanDate)
	wire.PutGameString(a.TempKey[:], x.TempKey)
	wire.PutGameString(a.PassBlock[:], x.TokenBlock)
	a.AccessLevel = uint32(p.num("access", x.Access))

	for i := range x.Mobs {
		m := &x.Mobs[i]
		idx := int(p.num("mob@index", m.Index))
		if idx < 0 || idx >= 4 {
			p.warn("mob index %d out of range, skipped", idx)
			continue
		}
		p.charInfo(fmt.Sprintf("mob[%d]", idx), m, &a.Mob[idx])
	}

	a.StorageCoin = uint32(p.num("storage.gold", x.StorageGold))
	p.items("storage", x.StorageItems, a.StorageItems[:])

	a.Insignias = uint64(p.num("insignia", x.Insignia))
	if strings.TrimSpace(x.CharSlot) != "" {
		a.CharSlot = int16(p.num("charslot", x.CharSlot))
	} else {
		a.CharSlot = -1
	}
	a.Position = p.pos("position", x.Position)
	a.Water.Day = int32(p.num("water.day", x.WaterDay))
	a.Water.Total = int32(p.num("water.total", x.WaterTotal))
	a.Divina = p.date("divina", x.Divina)
	a.Sephira = p.date("sephira", x.Sephira)

	if wire.CString(a.Username[:]) == "" {
		return nil, fmt.Errorf("account has no <username>")
	}
	out.Warnings = p.warnings
	return out, nil
}

// parser converts strings to numbers, collecting problems as warnings.
// Values are truncated to the destination type by the caller, as the C++
// casts did.
type parser struct {
	warnings []string
}

func (p *parser) warn(format string, args ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf(format, args...))
}

// num parses a decimal integer. Empty means 0 (with a warning only when the
// C++ reader would have thrown, which is every caller; missing optional
// nodes are passed as "" deliberately and accepted silently).
func (p *parser) num(field, s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseUint(s, 10, 64); err == nil {
		return int64(v)
	}
	p.warn("%s: %q is not a number, using 0", field, s)
	return 0
}

func (p *parser) pos(field string, x xPos) wire.Position {
	return wire.Position{X: uint16(p.num(field+".x", x.X)), Y: uint16(p.num(field+".y", x.Y))}
}

func (p *parser) date(field string, x *xDate) wire.ItemDate {
	if x == nil {
		return wire.ItemDate{}
	}
	return wire.ItemDate{
		Dia:     uint8(p.num(field+".day", x.Day)),
		Mes:     uint8(p.num(field+".month", x.Month)),
		Ano:     uint16(p.num(field+".year", x.Year)),
		Hora:    uint8(p.num(field+".hour", x.Hour)),
		Minuto:  uint8(p.num(field+".minute", x.Minute)),
		Segundo: uint8(p.num(field+".second", x.Second)),
	}
}

func (p *parser) item(field string, x xItem) wire.Item {
	return wire.Item{
		Index: uint16(p.num(field+".itemId", x.ItemID)),
		Effect: [3]wire.ItemEffect{
			{Index: uint8(p.num(field+".ef0", x.Ef0)), Value: uint8(p.num(field+".efv0", x.Efv0))},
			{Index: uint8(p.num(field+".ef1", x.Ef1)), Value: uint8(p.num(field+".efv1", x.Efv1))},
			{Index: uint8(p.num(field+".ef2", x.Ef2)), Value: uint8(p.num(field+".efv2", x.Efv2))},
		},
	}
}

// items places <item slot=".."> / <equip index=".."> nodes into dst. The C++
// reader did not bounds-check the slot; out-of-range slots are dropped here.
func (p *parser) items(field string, xs []xItem, dst []wire.Item) {
	for _, x := range xs {
		var slotStr string
		switch {
		case x.Slot != nil:
			slotStr = *x.Slot
		case x.Index != nil:
			slotStr = *x.Index
		}
		slot := int(p.num(field+"@slot", slotStr))
		if slot < 0 || slot >= len(dst) {
			p.warn("%s: slot %d out of range (0..%d), item %s dropped", field, slot, len(dst)-1, x.ItemID)
			continue
		}
		dst[slot] = p.item(fmt.Sprintf("%s[%d]", field, slot), x)
	}
}

func (p *parser) skillbar(field string, xs []xSk, dst []uint8) {
	for _, x := range xs {
		i := int(p.num(field+"@index", x.Index))
		if i < 0 || i >= len(dst) {
			p.warn("%s: index %d out of range", field, i)
			continue
		}
		dst[i] = uint8(p.num(fmt.Sprintf("%s[%d]", field, i), x.Value))
	}
}

func (p *parser) affects(field string, xs []xAffect, dst []wire.Affect) {
	for _, x := range xs {
		var idxStr string
		switch {
		case x.SkillIndex != nil:
			idxStr = *x.SkillIndex
		case x.Inex != nil:
			idxStr = *x.Inex
		default:
			continue
		}
		i := int(p.num(field+"@skillIndex", idxStr))
		// C++ checked "> 32", letting index 32 write past the array.
		if i < 0 || i >= len(dst) {
			p.warn("%s: skillIndex %d out of range", field, i)
			continue
		}
		dst[i] = wire.Affect{
			Index:  uint8(p.num(field+".index", x.Index)),
			Master: uint8(p.num(field+".master", x.Master)),
			Value:  uint16(p.num(field+".value", x.Value)),
			Time:   uint32(p.num(field+".time", x.Time)),
		}
	}
}

func (p *parser) status(field string, x *xStatus) wire.Status {
	if x == nil {
		return wire.Status{}
	}
	return wire.Status{
		Level:    int16(p.num(field+".level", x.Level)),
		Defense:  int32(p.num(field+".defense", x.Defense)),
		Attack:   int32(p.num(field+".attack", x.Attack)),
		Merchant: uint8(p.num(field+".merchant", x.Merchant)),
		Move:     uint8(p.num(field+".move", x.Move)),
		MaxHP:    int32(p.num(field+".maxHp", x.MaxHP)),
		MaxMP:    int32(p.num(field+".maxMp", x.MaxMP)),
		CurHP:    int32(p.num(field+".curHp", x.CurHP)),
		CurMP:    int32(p.num(field+".curMp", x.CurMP)),
		STR:      uint16(p.num(field+".str", x.STR)),
		INT:      uint16(p.num(field+".int", x.INT)),
		DEX:      uint16(p.num(field+".des", x.DEX)),
		CON:      uint16(p.num(field+".con", x.CON)),
		Mastery: [4]uint16{
			uint16(p.num(field+".mastery0", x.Mastery.M0)),
			uint16(p.num(field+".mastery1", x.Mastery.M1)),
			uint16(p.num(field+".mastery2", x.Mastery.M2)),
			uint16(p.num(field+".mastery3", x.Mastery.M3)),
		},
	}
}

// charInfo mirrors XMLToStructure(pugi::xml_node, STRUCT_CHARINFO&).
func (p *parser) charInfo(f string, x *xCharInfo, c *wire.CharInfo) {
	pl := &x.Player
	m := &c.Player

	wire.PutGameString(m.Name[:], strings.TrimSpace(pl.Name))
	m.CapeInfo = int8(p.num(f+".capeInfo", pl.CapeInfo))
	m.Info = uint8(p.num(f+".info", pl.Info))
	m.GuildIndex = uint16(p.num(f+".guildIndex", pl.GuildIndex))
	m.ClassInfo = uint8(p.num(f+".classInfo", pl.ClassInfo))
	m.AffectInfo = uint8(p.num(f+".affectInfo", pl.AffectInfo))
	m.QuestInfo = uint16(p.num(f+".questInfo", pl.QuestInfo))
	m.Exp = p.num(f+".experience", pl.Experience)
	m.Gold = int32(p.num(f+".gold", pl.Gold))
	m.Last = p.pos(f+".lastPosition", pl.LastPosition)
	m.BStatus = p.status(f+".baseStatus", pl.BaseStatus)
	m.Status = p.status(f+".currentStatus", pl.CurrentStatus)
	p.items(f+".equips", pl.Equips, m.Equip[:])
	p.items(f+".inventory", pl.Inventory, m.Inventory[:])
	m.Learn[0] = uint32(p.num(f+".learn", pl.Learn))
	m.Learn[1] = uint32(p.num(f+".secLearn", pl.SecLearn))
	m.StatusPoint = uint16(p.num(f+".statusPoint", pl.StatusPoint))
	m.MasterPoint = uint16(p.num(f+".masterPoint", pl.MasterPoint))
	m.SkillPoint = uint16(p.num(f+".skillPoint", pl.SkillPoint))
	m.Critical = uint8(p.num(f+".critical", pl.Critical))
	m.SaveMana = uint8(p.num(f+".saveMana", pl.SaveMana))
	p.skillbar(f+".skillbar1", pl.SkillBar1, m.SkillBar1[:])
	m.GuildMemberType = int8(p.num(f+".guildMemberType", pl.GuildMemberType))

	p.affects(f+".affects", x.Affects, c.Affects[:])
	c.Info = uint64(p.num(f+".info", x.Info))
	c.HallEnter = uint32(p.num(f+".hallEnter", x.HallEnter))
	c.Fame = uint32(p.num(f+".fame", x.Fame))
	c.PesaEnter = int8(p.num(f+".pesaEnter", x.PesaEnter))
	c.Citizen = uint8(p.num(f+".citizen", x.Citizen))
	c.Soul = uint8(p.num(f+".soul", x.Soul))
	c.Hold = p.num(f+".hold", x.Hold)
	c.MortalSlot = int32(p.num(f+".mortalSlot", x.MortalSlot))
	c.RvRPoints = int32(p.num(f+".rvrPoints", x.RvRPoints))
	p.skillbar(f+".skillbar2", x.SkillBar2, c.SkillBar[:])

	c.Saude = p.date(f+".saude", x.Saude)
	c.Escritura = p.date(f+".escritura", x.Escritura)
	c.Revigorante = p.date(f+".revigorante", x.Revigorante)
	c.LastGuildKickOut = p.date(f+".lastKickOut", x.LastKickOut)

	if s := x.Sub; s != nil {
		sub := &c.Sub
		sub.Status = int32(p.num(f+".sub@status", s.Status))
		sub.SubStatus = p.status(f+".sub.status", s.SubStatus)
		// C++ only accepted equip index 0 ("itemSlot >= 1"), dropping index 1.
		p.items(f+".sub.equips", s.Equips, sub.Equip[:])
		sub.Exp = p.num(f+".sub.experience", s.Exp)
		sub.Learn = uint32(p.num(f+".sub.learn", s.Learn))
		sub.SecLearn = uint32(p.num(f+".sub.secLearn", s.SecLearn))
		p.skillbar(f+".sub.skillbar", s.SkillBar, sub.SkillBar[:])
		p.affects(f+".sub.affects", s.Affects, sub.Affect[:])
		sub.Info = uint64(p.num(f+".sub.questInfo", s.QuestInfo))
		sub.Soul = uint8(p.num(f+".sub.soul", s.Soul))
	}

	for _, n := range x.Nightmares {
		i := int(p.num(f+".nightmare@index", n.Index))
		if i < 0 || i >= len(c.Nightmare) {
			p.warn("%s.nightmares: index %d out of range", f, i)
			continue
		}
		c.Nightmare[i] = wire.Position{X: uint16(p.num(f+".nightmare.x", n.X)), Y: uint16(p.num(f+".nightmare.y", n.Y))}
	}
	p.items(f+".filter", x.Filter, c.FilterDrop.Items[:])
}
