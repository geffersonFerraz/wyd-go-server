package game

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Game data lives in Postgres (schema gamedata). ReadLegacyGameData parses
// the C++ files once for cmd/gamedata-import; ApplyGameData puts what the
// TMSRV loaded from the database into the in-memory tables.

// ReadLegacyGameData reads ItemList.csv (+ ItemEffect.h), skilldata.csv,
// "controle drop/Controle.txt" and Teleport.txt from the C++ TMSRV folder.
// skipped lists drop blocks ignored because an earlier block had the same
// name (the C++ used the first one).
func ReadLegacyGameData(dir string) (gd *store.GameData, skipped []string, err error) {
	gd = &store.GameData{}
	names := loadEffectNames(filepath.Join(dir, "ItemEffect.h"))
	for name, id := range names {
		gd.EffectNames = append(gd.EffectNames, store.EffectName{ID: id, Name: name})
	}
	sort.Slice(gd.EffectNames, func(i, j int) bool { return gd.EffectNames[i].ID < gd.EffectNames[j].ID })
	gd.EffectNames = dedupeEffectIDs(gd.EffectNames)

	tmp := &Data{Items: make([]ItemData, MaxItemList)}
	if err := tmp.loadItemList(filepath.Join(dir, "ItemList.csv"), names); err != nil {
		return nil, nil, err
	}
	for id := 1; id < MaxItemList; id++ {
		it := &tmp.Items[id]
		if it.Name == "" {
			continue
		}
		row := store.Item{
			ID: id, Name: wire.DecodeGame(it.Name), Mesh1: int(it.Mesh1), Mesh2: int(it.Mesh2),
			Level: int(it.Level), ReqStr: int(it.Str), ReqInt: int(it.Int), ReqDex: int(it.Dex), ReqCon: int(it.Con),
			Price: int(it.Price), Unique: int(it.Unique), Pos: int(it.FullPos), Extreme: int(it.Extreme), Grade: int(it.Grade),
		}
		for e := range it.Effect {
			row.Effects[e] = store.ItemEffect{Effect: int(it.Effect[e].Index), Value: int(it.Effect[e].Value)}
		}
		gd.Items = append(gd.Items, row)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "skilldata.csv"))
	if err != nil {
		return nil, nil, err
	}
	for _, s := range ParseSkillsCSV(raw) {
		k := s.SkillData
		gd.Skills = append(gd.Skills, store.Skill{
			ID: s.ID, Name: s.Name, Points: k.Points, Target: k.Target, Mana: k.Mana, Delay: k.Delay, Range: k.Range,
			InstanceType: k.InstanceType, InstanceValue: k.InstanceValue, TickType: k.TickType, TickValue: k.TickValue,
			AffectType: k.AffectType, AffectValue: k.AffectValue, Time: k.Time, InstanceAttribute: k.InstanceAttribute,
			TickAttribute: k.TickAttribute, Aggressive: k.Aggressive, MaxTarget: k.Maxtarget, PartyCheck: k.PartyCheck,
			AffectResist: k.AffectResist, Passive: k.PassiveCheck, ForceDamage: k.ForceDamage, Act1: s.Act1, Act2: s.Act2,
		})
	}

	raw, err = os.ReadFile(filepath.Join(dir, "controle drop", "Controle.txt"))
	if err != nil {
		return nil, nil, err
	}
	mobs, skipped := ParseDropsTxt(raw)
	for _, m := range mobs {
		d := store.Drop{Name: m.Name, Section: m.Section, MaxDrops: m.MaxDrops, Gold: m.Gold}
		for _, it := range m.Items {
			d.Items = append(d.Items, store.DropItem{Kills: it.Kills, Item: int(it.Index), Effect: int(it.Effect), Value: int(it.Value)})
		}
		gd.Drops = append(gd.Drops, d)
	}

	if err := tmp.loadTeleports(filepath.Join(dir, "Teleport.txt")); err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	for i, t := range tmp.Teleports {
		gd.Teleports = append(gd.Teleports, store.Teleport{ID: i, SrcX: t.SrcX, SrcY: t.SrcY, DstX: t.DestX, DstY: t.DestY, Price: int(t.Price)})
	}
	return gd, skipped, nil
}

// dedupeEffectIDs keeps one name per id (ItemEffect.h has aliases; the
// first name in id order wins, which is what effect_names stores).
func dedupeEffectIDs(in []store.EffectName) []store.EffectName {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].ID != in[j].ID {
			return in[i].ID < in[j].ID
		}
		return in[i].Name < in[j].Name
	})
	out := in[:0]
	for i, e := range in {
		if i > 0 && e.ID == in[i-1].ID {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ApplyGameData installs database rows as the in-memory game tables.
func (d *Data) ApplyGameData(gd *store.GameData) error {
	if len(gd.Items) == 0 {
		return fmt.Errorf("gamedata.items is empty: run gamedata-import")
	}
	items := make([]ItemData, MaxItemList)
	for _, r := range gd.Items {
		if r.ID <= 0 || r.ID >= MaxItemList {
			return fmt.Errorf("item %d out of range", r.ID)
		}
		it := ItemData{
			Name: wire.EncodeGame(r.Name), Mesh1: int16(r.Mesh1), Mesh2: int32(r.Mesh2), Level: int16(r.Level),
			Str: int16(r.ReqStr), Int: int16(r.ReqInt), Dex: int16(r.ReqDex), Con: int16(r.ReqCon),
			Price: int32(r.Price), Unique: int16(r.Unique), Pos: uint16(r.Pos), FullPos: int32(r.Pos), Extreme: int16(r.Extreme), Grade: int16(r.Grade),
		}
		for e, ef := range r.Effects {
			it.Effect[e].Index, it.Effect[e].Value = int16(ef.Effect), int16(ef.Value)
		}
		items[r.ID] = it
	}

	skills := make([]SkillData, maxSkill)
	for _, k := range gd.Skills {
		if k.ID < 0 || k.ID >= maxSkill {
			return fmt.Errorf("skill %d out of range", k.ID)
		}
		skills[k.ID] = SkillData{
			Points: k.Points, Target: k.Target, Mana: k.Mana, Delay: k.Delay, Range: k.Range,
			InstanceType: k.InstanceType, InstanceValue: k.InstanceValue, TickType: k.TickType, TickValue: k.TickValue,
			AffectType: k.AffectType, AffectValue: k.AffectValue, Time: k.Time, InstanceAttribute: k.InstanceAttribute,
			TickAttribute: k.TickAttribute, Aggressive: k.Aggressive, Maxtarget: k.MaxTarget, PartyCheck: k.PartyCheck,
			AffectResist: k.AffectResist, PassiveCheck: k.Passive, ForceDamage: k.ForceDamage,
		}
	}

	drops := make(map[string]*MobDrop, len(gd.Drops))
	for _, r := range gd.Drops {
		m := &MobDrop{Name: r.Name, Section: r.Section, MaxDrops: r.MaxDrops, Gold: r.Gold}
		for _, it := range r.Items {
			m.Items = append(m.Items, DropItem{Kills: it.Kills, Index: uint16(it.Item), Effect: uint8(it.Effect), Value: uint16(it.Value)})
		}
		drops[wire.EncodeGame(r.Name)] = m
	}

	var teleports []Teleport
	for _, t := range gd.Teleports {
		if t.ID < 0 || t.ID >= maxTeleport {
			return fmt.Errorf("teleport %d out of range", t.ID)
		}
		for len(teleports) <= t.ID {
			teleports = append(teleports, Teleport{})
		}
		teleports[t.ID] = Teleport{SrcX: t.SrcX, SrcY: t.SrcY, DestX: t.DstX, DestY: t.DstY, Price: int32(t.Price)}
	}

	d.Items, d.Skills, d.Drops, d.Teleports = items, skills, drops, teleports
	for _, c := range gd.CityOwners {
		if c.City >= 0 && c.City < len(d.Cities) {
			d.Cities[c.City].OwnerIndex = int32(c.GuildID)
		}
	}
	return nil
}
