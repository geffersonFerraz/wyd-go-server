package game

import "dbsrv/internal/wire"

// Score computation: CMob::GetCurrentScore, GetCurScore, GetStatusBaseScore,
// GetScorePoint/GetSkillPoint/GetMasterPoint, GetMobAbility (CMob.cpp,
// Basedef.cpp, GetFunc.cpp).

const (
	EF_SPECIAL1         = 11
	EF_UNIQUE           = 59
	EF_HITRATE          = 41
	EF_SAVEMANA         = 44
	EF_HPADD            = 45
	EF_MPADD            = 46
	EF_REGENHP          = 47
	EF_REGENMP          = 48
	EF_HPADD2           = 69
	EF_MPADD2           = 70
	EF_SPECIALALL       = 74
	EF_LIFESTEAL        = 150
	EF_BONUSEXP         = 151
	EF_POTIONHEAL       = 152
	EF_BONUSEXPIND      = 153
	EF_VAMPIRISM        = 154
	EF_IGNORERESISTANCE = 155
	EF_SLOW             = 156
	EF_RESISTANCE       = 157

	maxPoints         = 64000
	maxResist         = 100
	maxMagicIncrement = 1200
	maxLevel          = 399
	maxLevelCelestial = 199

	capeBlue = 7
)

// UNION_QUESTINFO bits (CharInfo.Info).
const (
	questElime   = 1 << 5
	questSylphed = 1 << 6
	questThelion = 1 << 7
	questNoas    = 1 << 8
	questPilula  = 1 << 24

	questLvBlocked     = 1 << 32 // Celestial level block (39/89/199/209)
	questLvBlockedArch = 1 << 33 // Arch level block (354/369)
	questUnlock354     = 1 << 37
	questUnlock369     = 1 << 38
	questUnlock39      = 1 << 39
	questUnlock89      = 1 << 40
	questBloqueia354   = 1 << 41 // never set by the C++
	questBloqueia369   = 1 << 42
	questUnlock200     = 1 << 46
)

// Player.AffectInfo bits (stBase.h).
const (
	affSlowMov    = 0x01
	affDrainHP    = 0x02
	affVisionDrop = 0x04
	affEvasion    = 0x08
	affSnoop      = 0x10
	affSpeedMov   = 0x20
	affSkillDelay = 0x40
	affResist     = 0x80
)

// baseSIDCHM is BaseSIDCHM: base STR, INT, DEX, CON, HP, MP per class.
var baseSIDCHM = [4][6]int{
	{8, 4, 7, 6, 80, 45},
	{5, 8, 5, 5, 60, 65},
	{6, 6, 9, 5, 70, 55},
	{8, 9, 13, 6, 75, 60},
}

// transBonus is pTransBonus (BeastMaster transformations).
type transBonus struct {
	MinAttack, MaxAttack, MinDefense, MaxDefense, MinHp, MaxHp int
	SpeedMove, SpeedAttack, Sanc                               int
}

var pTransBonus = [5]transBonus{
	{110, 130, 90, 105, 95, 105, 1, 0, 20},
	{90, 105, 100, 110, 110, 165, 0, 0, 0},
	{108, 120, 105, 115, 100, 140, 1, 0, 20},
	{115, 125, 110, 135, 105, 130, 0, 0, 20},
	{105, 122, 110, 120, 105, 145, 1, 0, 20},
}

// capesID is g_pCapesID: capes of the blue, red and neutral kingdoms.
var capesID = [3][10]uint16{
	{736, 1766, 1767, 1768, 543, 545, 734, 3191, 3194, 3197},
	{737, 1769, 1770, 1771, 544, 546, 735, 3192, 3195, 3198},
	{550, 572, 573, 574, 549, 548, 550, 3193, 3196, 3199},
}

// combatState holds the CMob fields GetCurrentScore derives.
type combatState struct {
	parry, hitRate                        int
	expBonus, dropBonus                   int
	forceDamage, reflectDamage            int
	potionBonus, lifeSteal, vampirism     int
	ignoreResistance, slowChance, resistC int
	individualExpBonus                    int
	weaponDamage, attackSpeed             int
	regenHP, regenMP                      int
	magicIncrement                        int
	attackCount                           int16 // pUser.AttackCount
	currentTarget                         int
	enemies                               [4]int // EnemyList
}

func learned(pl *wire.Mob, bit int) bool { return pl.Learn[0]&(1<<uint(bit%24)) != 0 }

func evolution(pl *wire.Mob) int { return int(pl.Equip[0].Effect[1].Value) }

// mobAbility is GetMobAbility(STRUCT_MOB*, eff).
func (d *Data) mobAbility(pl *wire.Mob, eff int) int {
	if eff == EF_RANGE {
		v := d.maxAbility(pl, eff)
		if v < 2 && pl.Equip[0].Index/10 == 3 && pl.Learn[0]&0x40 != 0 {
			v = 2
		}
		return v
	}
	total := 0
	var unique [20]int
	for i := range 20 {
		idx := pl.Equip[i].Index
		if idx == 0 && i != 7 {
			continue
		}
		if i >= 1 && i <= 5 {
			unique[i] = int(d.Item(idx).Unique)
		}
		if eff == EF_DAMAGE && i == 6 {
			continue
		}
		if eff == EF_MAGIC && i == 7 {
			continue
		}
		if i == 7 && eff == EF_DAMAGE {
			dam1 := d.itemAbility(&pl.Equip[6], EF_DAMAGE) + d.itemAbility(&pl.Equip[6], EF_DAMAGE2)
			dam2 := d.itemAbility(&pl.Equip[7], EF_DAMAGE) + d.itemAbility(&pl.Equip[7], EF_DAMAGE2)
			u1, u2 := 0, 0
			if a := pl.Equip[6].Index; a > 0 && int(a) < MaxItemList {
				u1 = int(d.Item(a).Unique)
			}
			if a := pl.Equip[7].Index; a > 0 && int(a) < MaxItemList {
				u2 = int(d.Item(a).Unique)
			}
			if u1 != 0 && u2 != 0 {
				porc := 50
				if u1 == u2 {
					porc = 70
				}
				if dam1 > dam2 {
					total += dam1 + dam2*porc/100
				} else {
					total += dam2 + dam1*porc/100
				}
				continue
			}
			total += max(dam1, dam2)
			continue
		}
		v := d.itemAbility(&pl.Equip[i], eff)
		if eff == EF_ATTSPEED && v == 1 {
			v = 10
		}
		total += v
	}
	if eff == EF_AC && unique[1] != 0 && unique[1] == unique[2] && unique[2] == unique[3] && unique[3] == unique[4] && unique[4] == unique[5] {
		total = total * 105 / 100
	}
	return total
}

// maxAbility is GetMaxAbility.
func (d *Data) maxAbility(pl *wire.Mob, eff int) int {
	best := 0
	for i := range 20 {
		if pl.Equip[i].Index == 0 {
			continue
		}
		if v := int(int16(d.itemAbility(&pl.Equip[i], eff))); v > best {
			best = v
		}
	}
	return best
}

// bonusSet is GetBonusSet: +5% defense for a full matching armor set.
func (d *Data) bonusSet(pl *wire.Mob, defense int) int {
	start, first := 2, &pl.Equip[1]
	if evolution(pl) >= 2 {
		start, first = 3, &pl.Equip[2]
	}
	if first.Index == 0 || int(first.Index) >= MaxItemList {
		return 0
	}
	mobType := d.effectValueByIndex(first.Index, EF_MOBTYPE)
	class := d.effectValueByIndex(first.Index, EF_CLASS)
	letter := d.effectValueByIndex(first.Index, EF_UNKNOW1)
	i := start
	for ; i < 6; i++ {
		it := pl.Equip[i].Index
		if it == 0 || it >= 6500 {
			break
		}
		if mobType != d.effectValueByIndex(it, EF_MOBTYPE) || class != d.effectValueByIndex(it, EF_CLASS) || letter != d.effectValueByIndex(it, EF_UNKNOW1) {
			break
		}
	}
	if i == 6 {
		return defense * 5 / 100
	}
	return 0
}

// getCurrentScore is CMob::GetCurrentScore(id).
func (g *Game) getCurrentScore(id int) {
	m := &g.mobs[id]
	pl := &m.info.Player
	d := g.data
	if id < MaxPlayer {
		pl.CapeInfo = 0
	}
	if id < MaxPlayer && pl.BStatus.Level < 1011 {
		for i := range 4 {
			pl.Resist[i] = uint8(d.mobAbility(pl, EF_RESIST1+i))
		}
		pl.Equip[0].Effect[0] = wire.ItemEffect{}
		pl.AffectInfo = 0
		m.info.AffectInfo = 0
		g.statusBaseScore(id)
	} else if id >= MaxPlayer {
		if gid := m.npc.generateID; gid > 0 && gid < len(d.Geners) {
			pl.Resist = d.Geners[gid].Leader.Resist
		}
	}
	c := &m.combat
	c.parry = d.mobAbility(pl, EF_PARRY)
	c.hitRate = d.mobAbility(pl, EF_HITRATE)
	if id < MaxPlayer {
		r := d.mobAbility(pl, EF_RANGE)
		if pl.Learn[0]&0x20000000 != 0 {
			r++
		}
		if pl.Learn[0]&0x80000 != 0 {
			r++
		}
		g.users[id].attackRange = r
	}
	c.expBonus, c.dropBonus, c.forceDamage, c.reflectDamage = 0, 0, 0, 0
	c.potionBonus, c.lifeSteal, c.vampirism, c.individualExpBonus = 0, 0, 0, 0
	c.ignoreResistance, c.resistC = 0, 0
	g.curScore(id)

	if pl.Status.CurHP > pl.Status.MaxHP {
		pl.Status.CurHP = pl.Status.MaxHP
	}
	if pl.Status.CurMP > pl.Status.MaxMP {
		pl.Status.CurMP = pl.Status.MaxMP
	}
	wd := d.itemAbility(&pl.Equip[6], EF_DAMAGE)
	sd := d.itemAbility(&pl.Equip[7], EF_DAMAGE)
	if wd > sd {
		c.weaponDamage = wd + sd/3
	} else {
		c.weaponDamage = sd + wd/3
	}
	for _, slot := range []int{6, 7} {
		if p := d.Item(pl.Equip[slot].Index).Pos; (p == 64 || p == 192) && itemSanc(&pl.Equip[slot]) == 9 {
			c.weaponDamage += 40
		}
	}
	for i := 1; i < 8; i++ {
		it := &pl.Equip[i]
		if it.Index == 0 {
			continue
		}
		sanc := itemSanc(it)
		grade := d.Item(it.Index).Grade
		if !((grade >= 5 && grade <= 8) || sanc >= 10) {
			continue
		}
		switch grade {
		case 5:
			if sanc <= 9 {
				c.dropBonus += 2
			} else if sanc <= 15 {
				c.dropBonus += 4
			}
		case 6:
			if sanc <= 9 {
				c.forceDamage += 40
			} else if sanc <= 15 {
				c.forceDamage += 80 + 80*(sanc-10)
			}
		case 7:
			if sanc <= 9 {
				c.expBonus += 2
			} else if sanc <= 15 {
				c.expBonus += 4
			}
		case 8:
			if sanc <= 9 {
				c.reflectDamage += 40
			} else if sanc <= 15 {
				c.reflectDamage += 80 + 80*(sanc-10)
			}
		default: // sanc >= 10
			value := -1
			for p := range 3 {
				if e := it.Effect[p].Index; e == 43 || (e >= 116 && e <= 125) {
					value = int(it.Effect[p].Value)
					break
				}
			}
			if value == -1 {
				continue
			}
			switch (value - 230) % 4 {
			case 0:
				c.dropBonus += 2
			case 1:
				if sanc <= 15 {
					c.forceDamage += 40 * (sanc - 9)
				}
			case 2:
				c.expBonus += 2
			case 3:
				if sanc <= 15 {
					c.reflectDamage += 40 * (sanc - 9)
				}
			}
		}
	}
	for i := range 4 {
		it := &pl.Equip[8+i]
		if it.Index == 0 {
			continue
		}
		abs, perf := 0, 0
		switch it.Index {
		case 540:
			perf = 10
		case 541:
			abs = 10
		case 631:
			perf = 15
		case 632:
			abs = 15
		case 633, 567, 568, 569, 570:
			abs, perf = 15, 15
		}
		sanc := itemSanc(it)
		if sanc == 0 {
			sanc = 1
		}
		c.forceDamage += perf + sanc*2
		c.reflectDamage += abs + sanc*2
	}
	c.expBonus += d.mobAbility(pl, EF_BONUSEXP)
	c.individualExpBonus += d.mobAbility(pl, EF_BONUSEXPIND)
}

// curScore is GetCurScore: Status from bStatus, items, affects and passives.
func (g *Game) curScore(id int) {
	m := &g.mobs[id]
	mob := &m.info.Player
	d := g.data
	c := &m.combat
	mob.AffectInfo = 0
	m.info.AffectInfo = 0
	curHP, curMP := mob.Status.CurHP, mob.Status.CurMP
	mob.Status = mob.BStatus
	mob.Status.CurHP, mob.Status.CurMP = curHP, curMP

	ab := func(eff int) int { return d.mobAbility(mob, eff) }
	critical := int(float32(ab(EF_CRITICAL)) / 10.0 * 2.5)
	attack := int(mob.BStatus.Attack) + ab(EF_DAMAGE)
	defense := int(mob.BStatus.Defense) + ab(EF_AC) + ab(EF_ACADD)
	maxHP := int(mob.BStatus.MaxHP) + ab(EF_HP)
	maxMP := int(mob.BStatus.MaxMP) + ab(EF_MP)
	str := int(mob.Status.STR) + ab(EF_STR)
	dex := int(mob.Status.DEX) + ab(EF_DEX)
	intel := int(mob.Status.INT) + ab(EF_INT)
	con := int(mob.Status.CON) + ab(EF_CON)
	var resist [5]int
	for i := range 4 {
		resist[i] = ab(EF_RESIST1 + i)
	}
	var mastery [4]int
	all := ab(EF_SPECIALALL)
	for i := range 4 {
		mastery[i] = min(int(mob.Status.Mastery[i])+ab(EF_SPECIAL1+i)+all, 255)
	}
	// The C++ wrote bStatus.Move.Speed >> 4 on a 4-bit field: always 0.
	speed := [2]int{ab(EF_RUNSPEED) + 2, ab(EF_ATTSPEED)}
	speed[0] = min(speed[0], 6)
	saveMana := ab(EF_SAVEMANA)
	magic := ab(EF_MAGIC) / 3
	regenHP := ab(EF_REGENHP) / 2
	regenMP := ab(EF_REGENMP) / 2
	lifeSteal, vampirism := ab(EF_LIFESTEAL), ab(EF_VAMPIRISM)
	potionBonus, ignoreRes := ab(EF_POTIONHEAL), ab(EF_IGNORERESISTANCE)
	slowChance, resistChance := ab(EF_SLOW), ab(EF_RESISTANCE)

	body := int(mob.Equip[0].Index) / 10
	if body < 4 {
		mob.Equip[0].Effect[0] = wire.ItemEffect{Index: EF_SANC}
		if mob.CapeInfo >= 7 && mob.CapeInfo <= 9 {
			mob.CapeInfo = 0
		}
	}
	if mob.CapeInfo != 4 {
	capes:
		for k := range 2 {
			for _, id := range capesID[k] {
				if id == mob.Equip[15].Index {
					mob.CapeInfo = int8(capeBlue + k)
					break capes
				}
			}
		}
	}

	incHp := ab(EF_HPADD) + 100 + ab(EF_HPADD2)
	incMp := ab(EF_MPADD) + 100 + ab(EF_MPADD2)
	incDamage, incAc := 100, 100
	local20, local21 := 0, 0
	magicJewel := false
	learn := mob.Learn[0]

	for i := range m.info.Affects {
		a := &m.info.Affects[i]
		if a.Index == 0 {
			continue
		}
		master, value := int(a.Master), int(a.Value)
		switch a.Index {
		case 1: // slow
			switch {
			case master == 255:
				speed[0] -= 3
			case master >= 200:
				speed[0] -= 2
			default:
				speed[0]--
			}
			speed[1] -= 30
		case 2: // speed
			speed[0] += master
			m.info.AffectInfo |= 0x80
		case 3: // resistance down
			dec := master
			if mob.Equip[0].Index <= 50 {
				dec >>= 1
			} else {
				dec -= 10
			}
			for k := range 4 {
				resist[k] = min(max(resist[k]-dec, 0), 100)
			}
		case 55:
			incHp += 10
			incMp += 10
		case 4: // potions
			switch value {
			case 4:
				incDamage += 13
				incHp += 15
				incMp += 10
				magic += 5
			case 5:
				incDamage += 7
				incHp += 10
				magic += 3
			case 6:
				attack += 75
				magic++
			case 7:
				attack += 120
				magic += 2
			case 8:
				attack += 140
				magic += 3
			default:
				incDamage += 3
				attack += 20
				magic += 5
			}
		case 5: // evasion down: the C++ truncates the factor to 0 or 1
			dex *= int((100.0 - float32(master)) / 100.0)
		case 6:
			dex = int(float32(dex) * ((100.0 + float32(master)) / 100.0))
		case 7:
			dec := value/10 + 10
			speed[1] -= dec
			speed[0]--
			if mob.Equip[0].Index <= 50 {
				continue
			}
			intel -= dec + 10
		case 8:
			if value&2 != 0 {
				resist[4] += 25
			}
			if value&8 != 0 {
				c.reflectDamage += 300
			}
			if value&16 != 0 {
				incAc += 10
				incHp += 10
			}
			if value&32 != 0 {
				incHp += 10
				incDamage += 7
			}
			if value&128 != 0 {
				magicJewel = true
			}
		case 9: // attack buff
			bonus := ((value*5/20 + master) * 3) >> 1
			incDamage += 5
			if mob.ClassInfo == 1 && learn&100000 != 0 {
				bonus *= 4
				incDamage += 10
			}
			attack += bonus
		case 10:
			attack -= value/3 + master
		case 11:
			defense += value/3 + master
		case 12:
			defense = int(float64(mob.Status.Defense) * ((100.0 - float64(master)) / 100.0))
		case 13:
			incDamage += value/10 + master
			incAc -= 5
		case 14:
			if learn&(1<<7) != 0 {
				con += value*4 + master*2
			} else {
				con += value*3 + master
			}
		case 15:
			inc := value/10 + master
			for k := range 4 {
				mastery[k] += inc
			}
		case 16: // BeastMaster transformation
			t := master - 1
			if t < 0 || t >= 5 || mob.ClassInfo != 2 {
				continue
			}
			if t == 4 {
				mob.Equip[0].Index = 32
			} else {
				mob.Equip[0].Index = uint16(22 + t)
			}
			atkBonus, hpBonus, acInc, speedInc, resistTrans := 0, 0, 0, 0, 0
			switch mob.Equip[0].Index {
			case 22:
				atkBonus -= 12
				resistTrans += 15
			case 23:
				hpBonus += 150
				resistTrans += 32
				speedInc += 20
			case 24:
				atkBonus -= 10
				acInc += 8
			case 25:
				resistTrans += 20
				atkBonus -= 15
			case 32:
				atkBonus -= 9
				hpBonus += 10
				acInc += 4
				speedInc += 20
				resistTrans += 25
			}
			if learn&(1<<(69%24)) != 0 {
				atkBonus += 3
				hpBonus += 25
			}
			tb := pTransBonus[t]
			sanc := min(max((int(mob.Status.Level)*2+mastery[3])/3-tb.Sanc, 0)/12, 9)
			if evolution(mob) >= evoCelestial {
				sanc = 9
			}
			mob.Equip[0].Effect[0] = wire.ItemEffect{Index: 43, Value: uint16(sanc)}
			lo, hi := tb.MinAttack+atkBonus, tb.MaxAttack+atkBonus
			v := (hi-lo)*value/300 + lo
			if mob.Equip[0].Index == 22 {
				attack += 25
			}
			incDamage += v - 100
			lo, hi = tb.MinDefense+acInc, tb.MaxDefense+acInc
			v = (hi-lo)*value/300 + lo
			defense = defense * v / 100
			if mob.Equip[0].Index == 22 {
				defense += 5
			}
			lo, hi = tb.MinHp+hpBonus, tb.MaxHp+hpBonus
			v = ((hi-lo)+25)*value/200 + lo
			maxHP = maxHP * v / 100
			for k := range 4 {
				resist[k] = min(max(resist[k]+resistTrans, -50), maxResist)
			}
			if t == 0 {
				critical += 50
			}
			local20 = tb.SpeedAttack + speedInc
			local21 = tb.SpeedMove
		case 38:
			mp := (int(mob.Status.Level)+value)>>1 + maxMP
			maxHP += mp
			maxMP -= mp / 3
		case 19:
			m.info.AffectInfo |= 0x02
		case 21:
			defense -= value/3 + 10
			incDamage += value/10 + master
		case 24:
			inc := ((value*5/15 + master) * 3) >> 1
			if it := mob.Equip[7].Index; it > 0 && int(it) < MaxItemList && d.Item(it).Unique == 51 {
				inc += inc / 3
				for k := range 4 {
					resist[k] += 15
				}
			}
			defense += inc
		case 25:
			sum := value>>2 + master
			if learn&128 != 0 {
				sum = value>>1 + master + 75
			}
			defense += sum
			for k := range 4 {
				resist[k] += 22
			}
		case 36:
			mob.AffectInfo |= 0x10
		case 26:
			mob.AffectInfo |= 0x20
		case 28:
			mob.AffectInfo |= 0x40
		case 27:
			mob.AffectInfo |= 0x04
		case 29: // soul
			evo := evolution(mob)
			if value == 99 && evo == evoMortal {
				maxHP, maxMP, str, intel = 3000, 3000, 1000, 1000
				continue
			}
			val, mid, mid2 := 120, 80, 40
			if evo == evoMortal {
				val, mid, mid2 = 70, 40, 30
			}
			switch m.info.Soul {
			case 0:
				str += str * mid / 100
				con += con * mid2 / 100
			case 1:
				intel += intel * mid / 100
				con += con * mid2 / 100
			case 2:
				dex += dex * mid / 100
				con += con * mid2 / 100
			case 3:
				str += str * mid / 100
				dex += dex * mid2 / 100
			case 4:
				intel += intel * mid / 100
				dex += dex * mid2 / 100
			case 5:
				con += con * mid / 100
				dex += dex * mid2 / 100
			case 6:
				str += str * val / 100
			case 7:
				intel += intel * val / 100
			case 8:
				con += con * val / 100
			case 9:
				dex += dex * val / 100
			}
		case 31:
			defense += value>>1 + master
		case 34: // divine
			incDamage += 13
			magic += 15
			incHp += 30
			incMp += 20
		case 35: // vigor potion (falls through to 56 in the C++)
			if value == 2 {
				incHp += 15
				incMp += 15
			} else {
				incHp += 20
				incMp += 20
			}
			incAc -= 5 + value
		case 56:
			incAc -= 5 + value
		}
	}

	for i := 1; i <= 7; i++ {
		idx := mob.Equip[i].Index
		if idx == 0 || int(idx) >= MaxItemList || itemSanc(&mob.Equip[i]) < 9 {
			continue
		}
		switch d.Item(idx).Pos {
		case 4, 8, 128:
			defense += 25
		case 16:
			mob.AffectInfo |= affSkillDelay
		case 64, 192:
			if d.Item(idx).Unique == 47 {
				magic += 4
			} else {
				attack += 40
			}
		}
	}
	if e := mob.Equip[0].Index; (e >= 22 && e <= 25) || e == 32 {
		transformed := false
		for _, a := range m.info.Affects {
			if a.Index == 16 {
				transformed = true
				break
			}
		}
		if !transformed {
			mob.Equip[0].Index = uint16(mob.Equip[0].Effect[1].Index)
			if u := g.player(id); u != nil {
				g.sendEquip(u)
			}
		}
	}

	// Class passives (learned skills).
	switch mob.ClassInfo {
	case 0: // TransKnight
		incAc += 10
		if learned(mob, 9) && mob.Equip[6].Index != 0 && mob.Equip[7].Index != 0 && d.Item(mob.Equip[7].Index).Pos != 128 {
			incDamage += 2
		}
		switch {
		case learned(mob, 7):
			magic += 10
		case learned(mob, 15):
			incAc += 7
			critical += 25
		case learned(mob, 23):
			magic += 10
		}
	case 1: // Foema
		switch {
		case learned(mob, 31):
			maxHP += maxHP * 10 / 100
		case learned(mob, 39):
			magic += 5
		case learned(mob, 47):
			attack += 250
			incDamage += 10
			if dex+str > intel {
				incHp += 10
			}
		}
	case 2: // BeastMaster
		shield := d.itemAbility(&mob.Equip[7], EF_POS) == 128
		if shield && learned(mob, 67) {
			incAc += 2
			defense += int(mob.Status.Mastery[3]) / 2
		}
		if learned(mob, 65) {
			c.reflectDamage += 100
			if shield || learned(mob, 71) {
				c.reflectDamage += 75
			}
		}
		switch {
		case learned(mob, 55):
			incAc += 3
			magic += 8
		case learned(mob, 63):
			incAc += 5
			defense += 200
		case learned(mob, 71):
			incHp += 8
			maxHP += 700
			attack += 200
		}
	case 3: // Hunter
		unique := d.Item(mob.Equip[6].Index).Unique
		if learned(mob, 74) && (unique == 42 || unique == 43) {
			attack += int(mob.Status.Mastery[1]) + 10
		}
		if learned(mob, 82) && mob.Equip[6].Index != 0 && mob.Equip[7].Index != 0 && d.itemAbility(&mob.Equip[7], EF_POS) != 128 {
			attack += int(mob.Status.Mastery[2]) * 2
		}
		if learned(mob, 90) {
			critical += dex / 80
		}
		if learned(mob, 94) && unique == 43 {
			defense += int(mob.Status.Mastery[3]) * 3 / 5
		}
		switch {
		case learned(mob, 79):
			if d.effectValueByIndex(mob.Equip[6].Index, EF_WTYPE) == 101 {
				attack += 60
				incDamage += 3
			}
		case learned(mob, 87):
			incHp += 7
		case learned(mob, 95):
			incAc += 10
		}
	}

	if evolution(mob) >= evoCelestial && id < MaxPlayer {
		if mob.ClassInfo != 1 || intel < str+dex {
			maxHP += 1000
		}
	}
	t := m.target
	if t.X >= 3449 && t.X <= 3979 && t.Y >= 2673 && t.Y <= 3221 && !petAlive(mob) {
		local21 -= 2
	}
	speed[1] += local20 + dex/5
	speed[0] = min(max(speed[0]+local21, 0), 6)
	mob.Status.Move = uint8(min(max(speed[1], 0), 150)<<4 + speed[0])

	if body < 4 {
		attack += str/3 + mastery[0] + dex/4
		maxHP += con * 2
		maxMP += intel * 2
	}
	for i := range 5 {
		if gi := g.chargedGuilds[g.cfg.Channel-1][i]; gi != 0 && int32(mob.GuildIndex) == gi {
			maxHP += 50
			maxMP += 50
		}
	}
	if id < MaxPlayer && evolution(mob) == evoArch {
		q := m.info.Info
		if q&questElime != 0 {
			maxMP += 80
		}
		if q&questSylphed != 0 {
			defense += 30
		}
		if q&questThelion != 0 {
			maxHP += 80
		}
		if q&questNoas != 0 {
			maxHP += 60
			maxMP += 60
			defense += 20
		}
	}
	if id < MaxPlayer {
		bonus := g.kingdomRegen(int(mob.CapeInfo))
		regenHP += bonus
		regenMP += bonus
	}

	if pk := int(mob.Inventory[63].Effect[0].Index) - 75; pk < 0 && id < MaxPlayer {
		pk = -pk
		maxHP -= maxHP * (pk + 15) / 100
		maxMP -= maxMP * (pk + 15) / 100
		attack -= attack * pk / 100
		defense -= defense * pk / 100
		magic -= magic * pk / 100
	}
	defense += d.bonusSet(mob, defense)
	if incDamage != 100 {
		attack = attack * incDamage / 100
	}
	if incHp != 100 {
		maxHP = maxHP * incHp / 100
	}
	if incMp != 100 {
		maxMP = maxMP * incMp / 100
	}
	if incAc != 100 {
		defense = defense * incAc / 100
	}
	c.regenHP = max(regenHP+5, 5)
	c.regenMP = max(regenMP+5, 5)
	for i := range 4 {
		mob.Status.Mastery[i] = uint16(min(mastery[i], 255))
		resist[i] = min(resist[i]+resist[4], maxResist)
		mob.Resist[i] = uint8(resist[i])
	}
	if magicJewel {
		removed := maxMP / 2
		maxMP -= removed
		maxHP += removed
	}
	if id < MaxPlayer {
		attack = min(attack, int(g.cfg.Limits.MaxAttackDefense))
		defense = min(defense, int(g.cfg.Limits.MaxAttackDefense))
	}
	magic = min(magic, maxMagicIncrement)
	mob.Status.Attack = int64(attack)
	mob.Status.Defense = int64(defense)
	mob.Status.STR, mob.Status.INT = int64(str), int64(intel)
	mob.Status.CON, mob.Status.DEX = int64(con), int64(dex)
	c.magicIncrement = magic
	m.info.MagicIncrement = uint32(magic)
	mob.MagicIncrement = uint16(magic)
	mob.SaveMana = uint8(saveMana)
	mob.Status.CurHP = min(mob.Status.CurHP, int64(maxHP))
	mob.Status.MaxHP = int64(maxHP)
	mob.Status.CurMP = min(mob.Status.CurMP, int64(maxMP))
	mob.Status.MaxMP = int64(maxMP)
	mob.Critical = uint8(min(critical, 255))
	c.attackSpeed = speed[1] + 100
	c.potionBonus, c.lifeSteal, c.vampirism = potionBonus, lifeSteal, vampirism
	c.ignoreResistance, c.slowChance, c.resistC = ignoreRes, slowChance, resistChance
}

// petAlive is CMob::isPetAlive.
func petAlive(pl *wire.Mob) bool {
	it := &pl.Equip[14]
	if it.Index == 0 {
		return false
	}
	if it.Index >= 2360 && it.Index <= 2389 && int16(uint16(it.Effect[0].Index)|uint16(it.Effect[0].Value)<<8) > 0 {
		return true
	}
	return it.Index >= 3980 && it.Index <= 3999
}

// statusBaseScore is GetStatusBaseScore: bStatus attack/defense/HP/MP from
// level and class, then the point totals.
func (g *Game) statusBaseScore(id int) {
	pl := &g.mobs[id].info.Player
	if id <= 0 || id >= MaxPlayer || pl.ClassInfo >= 4 {
		return
	}
	addHPMP := [4][2]int{{3, 1}, {1, 3}, {1, 2}, {2, 1}}
	mult := 1
	switch evolution(pl) {
	case evoArch:
		mult = 2
	case evoCelestial, evoSubCele:
		mult = 3
	}
	level := int(pl.BStatus.Level)
	if evolution(pl) >= evoCelestial {
		level += 400
	}
	pl.BStatus.Attack = int64(5 + mult*level)
	pl.BStatus.Defense = int64(4 + mult*level)
	cls := pl.ClassInfo
	pl.BStatus.MaxHP = int64(addHPMP[cls][0]*level + baseSIDCHM[cls][4])
	pl.BStatus.MaxMP = int64(addHPMP[cls][1]*level + baseSIDCHM[cls][5])
	g.scorePoint(id)
	g.skillPoint(id)
	g.masterPoint(id)
}

// mortalLevel is CharList.Status[MortalSlot].Level (399 if unknown).
func (g *Game) mortalLevel(id int) int {
	slot := int(g.mobs[id].info.MortalSlot)
	if slot >= 0 && slot <= 3 {
		return int(g.users[id].charList.Status[slot].Level)
	}
	return 399
}

// scorePoint is GetScorePoint.
func (g *Game) scorePoint(id int) {
	pl := &g.mobs[id].info.Player
	level := int(pl.BStatus.Level)
	steps := func(pnts, last int) int {
		switch {
		case level < 255:
			return pnts + level*5
		case level < 301:
			return pnts + 1270 + (level-254)*10
		case level < 354:
			return pnts + 1270 + 450 + (level-300)*20
		}
		return pnts + 1270 + 450 + 1080 + (level-354)*last
	}
	var pnts int
	switch evolution(pl) {
	case evoMortal:
		pnts = steps(0, 12)
	case evoArch:
		pnts = steps((g.mortalLevel(id)-300+1)*6, 6)
		if level >= 369 && pl.Equip[0].Effect[2].Value >= 4 {
			pnts += 300
		}
	case evoCelestial, evoSubCele:
		pnts = g.celestialScorePoints(id)
	default:
		return
	}
	pnts += int(g.mobs[id].info.StatusBonus)
	cls := pl.ClassInfo
	b := &pl.BStatus
	total := int(b.STR) - baseSIDCHM[cls][0] + int(b.INT) - baseSIDCHM[cls][1] + int(b.DEX) - baseSIDCHM[cls][2] + int(b.CON) - baseSIDCHM[cls][3]
	excess := max(total-pnts, 0)
	for i, stat := range [4]*int64{&b.STR, &b.INT, &b.DEX, &b.CON} {
		take := min(excess, max(int(*stat)-baseSIDCHM[cls][i], 0))
		*stat -= int64(take)
		excess -= take
	}
	pl.StatusPoint = uint16(max(pnts-total, 0))
}

// skillPoint is GetSkillPoint.
func (g *Game) skillPoint(id int) {
	pl := &g.mobs[id].info.Player
	level := int(pl.BStatus.Level)
	var pnts int
	switch evolution(pl) {
	case evoMortal:
		pnts = level * 3
	case evoArch:
		pnts = (g.mortalLevel(id) - 300 + 1) * 6
		switch {
		case level < 255:
			pnts += level * 4
		case level < 301:
			pnts += 1016 + (level-254)*3
		case level < 354:
			pnts += 1016 + 137 + (level-300)*4
		default:
			pnts += 1016 + 137 + 212 + (level-353)*4
		}
	case evoCelestial, evoSubCele:
		pnts = celestialSkillPoints(g.mobs[id].info.Info, level)
	default:
		return
	}
	if g.mobs[id].info.Info&questPilula != 0 {
		pnts += 9
	}
	pnts += int(g.mobs[id].info.SkillBonus)
	initial := int(pl.ClassInfo) * 24
	for i := initial; i < initial+24; i++ {
		if pl.Learn[0]&(1<<uint(i%24)) != 0 {
			pnts -= g.data.Skills[i].Points
		}
	}
	pl.SkillPoint = uint16(max(pnts, 0))
}

// masterPoint is GetMasterPoint.
func (g *Game) masterPoint(id int) {
	pl := &g.mobs[id].info.Player
	level := int(pl.BStatus.Level)
	var pnts int
	switch evolution(pl) {
	case evoMortal:
		pnts = level * 2
	case evoArch:
		pnts = (level + 50) * 2
	case evoCelestial, evoSubCele:
		pnts = 855
	default:
		return
	}
	pnts += int(g.mobs[id].info.MasterBonus)
	for i := range 4 {
		pnts -= int(pl.BStatus.Mastery[i])
	}
	pl.MasterPoint = uint16(max(pnts, 0))
}
