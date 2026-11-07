package game

import (
	"bufio"
	"bytes"
	"os"
	"strconv"
	"strings"

	"dbsrv/internal/wire"
)

// Language.txt indexes (Basedef.h) used so far.
const (
	nnNewbieZone       = 46
	nnOnlyGuildMembers = 47
)

// loadLanguage is ReadLanguageFile: "index name message" per line (tabs are
// spaces, '#' comments). The file is Windows-1252.
func (d *Data) loadLanguage(path string) {
	d.Lang = map[int]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.ReplaceAll(sc.Text(), "\t", " ")
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		idx, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		// %d %*s %[^\n]: the message starts after the second field.
		rest := strings.TrimLeft(line, " ")
		rest = strings.TrimLeft(rest[len(f[0]):], " ")
		rest = strings.TrimLeft(rest[len(f[1]):], " ")
		if len(rest) > 127 {
			rest = rest[:127]
		}
		d.Lang[idx] = wire.GameString([]byte(rest))
	}
}

// lang is g_pLanguageString[i].
func (g *Game) lang(i int) string { return g.data.Lang[i] }

// doRecall is DoRecall: back to the character's city, or to the guild
// area of a city its guild holds.
// TODO(phase E): RvR area and quest-time recalls.
func (g *Game) doRecall(id int) {
	m := &g.mobs[id]
	pl := &m.info.Player
	city := int(pl.Info>>6) & 3
	x := int(g.data.Cities[city].CityX) + rnd()%15
	y := int(g.data.Cities[city].CityY) + rnd()%15
	if gi := int32(pl.GuildIndex); gi > 0 {
		for i := range 5 {
			if g.chargedGuilds[g.cfg.Channel-1][i] == gi {
				x, y = int(g.data.Cities[i].AreaGuildX), int(g.data.Cities[i].AreaGuildY)
				break
			}
		}
	}
	g.teleportar(id, x, y)
}

// teleportar is Teleportar: jump to the nearest free cell of (x, y).
func (g *Game) teleportar(id, x, y int) {
	if !g.emptyMobGrid(id, &x, &y) {
		g.log.Info("no free cell to teleport", "id", id, "x", x, "y", y)
		return
	}
	p := g.action(id, x, y)
	p.MoveType = 1
	if u := g.player(id); u != nil {
		g.queue(u, &p)
	}
	g.multicastMove(id, x, y, &p)
}

// areaDenied applies RequestAction's map-attribute rules to a destination
// and recalls the player when the area is off limits.
func (g *Game) areaDenied(u *User, x, y int) bool {
	pl := &g.mobs[u.id].info.Player
	attr := g.data.attribute(x, y)
	level := int(pl.Status.Level)
	if attr&attrNewbie != 0 && ((level > g.cfg.NewbieZone && level < 1000) || pl.Equip[0].Effect[1].Value != evoMortal) {
		g.clientMessage(u, "%s", g.lang(nnNewbieZone))
		g.doRecall(u.id)
		return true
	}
	if attr&attrGuild != 0 && level < 400 {
		if v := g.data.village(x, y); v >= 0 && v < 5 && int32(pl.GuildIndex) != g.chargedGuilds[g.cfg.Channel-1][v] {
			g.clientMessage(u, "%s", g.lang(nnOnlyGuildMembers))
			g.doRecall(u.id)
			return true
		}
	}
	return false
}
