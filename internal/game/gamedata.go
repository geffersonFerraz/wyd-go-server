package game

import (
	"fmt"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Game data lives in Postgres (schema gamedata); ApplyGameData puts what the
// TMSRV loaded from the database into the in-memory tables.

// ApplyGameData installs database rows as the in-memory game tables.
func (d *Data) ApplyGameData(gd *store.GameData) error {
	if len(gd.Items) == 0 {
		return fmt.Errorf("gamedata.items is empty: import a server export in the admin panel")
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
		m := &MobDrop{Name: r.Name, Section: r.Section, MaxDrops: r.MaxDrops, Gold: r.Gold, RateGold: r.RateGold}
		for _, x := range r.Rate {
			if x.Slot < 0 || x.Slot >= 64 {
				return fmt.Errorf("drop %q: slot %d out of range", r.Name, x.Slot)
			}
			ri := RateItem{Slot: x.Slot, Chance: x.Chance, Item: wire.Item{Index: uint16(x.Item)}}
			for e, ef := range x.Effects {
				ri.Item.Effect[e] = wire.ItemEffect{Index: uint8(ef.Effect), Value: uint16(ef.Value)}
			}
			m.Rate = append(m.Rate, ri)
		}
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
	d.DropSlots = [64]store.DropSlot{}
	for _, s := range gd.DropSlots {
		if s.Slot >= 0 && s.Slot < len(d.DropSlots) {
			d.DropSlots[s.Slot] = s
		}
	}
	d.BlockedDrops = make(map[int]bool, len(gd.BlockedDrops))
	for _, id := range gd.BlockedDrops {
		d.BlockedDrops[id] = true
	}
	return nil
}

// ApplyCityOwners installs one channel's city owners.
func (d *Data) ApplyCityOwners(channel int, owners []store.CityOwner) {
	for _, c := range owners {
		if c.Channel == channel && c.City >= 0 && c.City < len(d.Cities) {
			d.Cities[c.City].OwnerIndex = int32(c.GuildID)
		}
	}
}

// ReadRateDrops reads the monsters' inventories from the templates of the
// generators (dir is the config folder): the first import of the drops by
// chance.
func ReadRateDrops(dir string, w *store.WorldAssets) ([]store.Drop, error) {
	tmp := &Data{npcFiles: w.TemplatesOf(store.TemplateNPC)}
	if err := tmp.loadGeners(dir); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []store.Drop
	add := func(m *wire.Mob) {
		if isMerchant(m) != 0 { // a merchant's inventory is its shop
			return
		}
		var raw [16]byte
		copy(raw[:], m.Name[:15])
		for i := range raw {
			if raw[i] == '_' || raw[i] == '@' {
				raw[i] = ' '
			}
		}
		name := wire.CString(raw[:])
		if name == "" || seen[name] {
			return
		}
		d := store.Drop{Name: wire.DecodeGame(name), RateGold: int64(m.Gold)}
		for slot, it := range m.Inventory {
			if it.Index == 0 || it.Index > 6499 {
				continue
			}
			r := store.RateDrop{Slot: slot, Item: int(it.Index)}
			for e, ef := range it.Effect {
				r.Effects[e] = store.ItemEffect{Effect: int(ef.Index), Value: int(ef.Value)}
			}
			d.Rate = append(d.Rate, r)
		}
		if len(d.Rate) == 0 && d.RateGold == 0 {
			return
		}
		seen[name] = true
		out = append(out, d)
	}
	for i := range tmp.Geners {
		ge := &tmp.Geners[i]
		if ge.HasLeader {
			add(&ge.Leader)
		}
		if ge.HasFollower {
			add(&ge.Follower)
		}
	}
	return out, nil
}
