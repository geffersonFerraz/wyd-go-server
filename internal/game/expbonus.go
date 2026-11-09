package game

import "dbsrv/internal/wire"

// The experience rules of GetExpApply_2 (GetFunc.cpp) that the first port
// left out: per zone and evolution bonuses ("Controlador EXP" files), the
// experience chest (affect 39 of the killer) and the fairy bonus, plus
// CMob::CheckQuarter (the 1/4, 2/4, 3/4 messages that refill HP and MP).

// ExpBonus holds the sServer.Bonus* values (exp_bonus of tmsrv.yaml).
type ExpBonus struct {
	DesertMortal int64 `yaml:"desert_mortal"`
	DesertArch   int64 `yaml:"desert_arch"`
	DesertCele   int64 `yaml:"desert_cele"`
	DesertSub    int64 `yaml:"desert_sub"`
	WaterMortal  int64 `yaml:"water_mortal"`
	WaterArch    int64 `yaml:"water_arch"`
	WaterCele    int64 `yaml:"water_cele"`
	WaterSub     int64 `yaml:"water_sub"`
	PesaCele     int64 `yaml:"pesa_cele"`
	PesaSub      int64 `yaml:"pesa_sub"`
	PesaMortal   int64 `yaml:"pesa_mortal"`
	PesaArch     int64 `yaml:"pesa_arch"`
}

// cut is "exp -= exp * f" on a long long (through a double).
func cut(exp int64, f float64) int64 { return int64(float64(exp) - float64(exp)*f) }

// zoneExp is the zone/evolution table of GetExpApply_2 for the receiver.
func (g *Game) zoneExp(exp int64, receiver int) int64 {
	m := &g.mobs[receiver]
	pl := &m.info.Player
	b := &g.data.ExpBonus
	ev, level, blevel := evolution(pl), int(pl.Status.Level), int(pl.BStatus.Level)
	zx, zy := int(m.target.X)/128, int(m.target.Y)/128
	at := func(x, y int) bool { return zx == x && zy == y }
	waterNM := at(8, 27) || at(9, 28)
	pesaA := at(9, 1)
	desert1, desert2 := at(9, 13), at(12, 13)

	switch ev {
	case evoMortal:
		switch {
		case waterNM:
			exp -= exp * b.WaterMortal / 50
		case pesaA:
			exp += exp * b.PesaMortal / 100
		case desert1 || desert2:
			exp += exp * b.DesertMortal / 100
		default:
			exp = 1000
		}
	case evoArch:
		switch {
		case waterNM:
			exp += exp * b.WaterArch / 50
		case pesaA:
			exp += exp * b.PesaArch / 100
		case desert1 || desert2:
			exp -= exp * b.DesertArch / 100
		default:
			exp = 20
		}
	case evoCelestial, evoSubCele:
		water, pesa, desert := b.WaterCele, b.PesaCele, b.DesertCele
		if ev == evoSubCele {
			water, pesa, desert = b.WaterSub, b.PesaSub, b.DesertSub
		}
		switch {
		case at(10, 27): // Água A
			if blevel > 180 {
				exp = cut(exp, 0.2)
			} else {
				exp += exp * water / 50
			}
		case pesaA:
			exp -= exp * pesa / 100
		case desert1:
			// The C++ "else" only covered a declaration: above 180 both cuts apply.
			if blevel > 180 {
				exp = cut(exp, 0.75)
			}
			exp -= exp * desert / 150
		case desert2:
			switch {
			case ev == evoCelestial && blevel > 180:
				exp = cut(exp, 0.96)
			case ev == evoCelestial:
				exp -= exp * desert / 150
			default: // Sub-Celestial: same dangling else as the first desert
				if blevel > 180 {
					exp = cut(exp, 0.22)
				}
				exp -= exp * desert / 150
			}
		default:
			exp = 20
		}
	}

	switch {
	case at(8, 27) && ev >= evoCelestial && level > 99:
		exp = 0
	case pesaA && ev >= evoCelestial && level > 180:
		exp = 0
	case at(29, 28) && ev == evoMortal:
		exp = 0
	}
	return exp
}

// fairyExpBonus is GetFairyExpBonus.
func fairyExpBonus(exp int64, fairy uint16) int64 {
	switch fairy {
	case 3900, 3906, 3914:
		return exp * 16 / 100
	case 3915:
		return exp * 18 / 100
	case 3902, 3908:
		return exp * 32 / 100
	}
	return 0
}

// checkQuarter is CMob::CheckQuarter: crossing 1/4, 2/4 or 3/4 of the
// level refills HP and MP. Fixed: at level 0 the C++ read g_pNextLevel[-1].
func (g *Game) checkQuarter(u *User, earned int64) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	ev, level := evolution(pl), int(pl.Status.Level)
	if ev < 0 || ev >= len(nextLevel) || level < 0 || level >= len(nextLevel[0]) {
		return
	}
	if (ev <= evoArch && level >= maxLevel) || (ev >= evoCelestial && level >= maxLevelCelestial) {
		return
	}
	var prev int64
	if level > 0 {
		prev = nextLevel[ev][level-1]
	}
	step := (nextLevel[ev][level] - prev) / 4
	for i := int64(3); i > 0; i-- {
		mark := prev + step*i
		if pl.Exp < mark && pl.Exp+earned >= mark {
			g.clientMessage(u, "%s", g.lang(57-int(i))) // 54 = 3/4, 55 = 2/4, 56 = 1/4
			pl.Status.CurHP, pl.Status.CurMP = pl.Status.MaxHP, pl.Status.MaxMP
			u.potionHP, u.potionMP = pl.Status.MaxHP, pl.Status.MaxMP
			g.sendSetHpMp(u)
			g.getCurrentScore(u.id)
			g.sendScore(u.id)
			g.sendEtc(u)
			return
		}
	}
}

func hasAffect(info *wire.CharInfo, index uint8) bool {
	for _, a := range info.Affects {
		if a.Index == index {
			return true
		}
	}
	return false
}
