package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"dbsrv/internal/game"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// NPC screens: the templates (gamedata.mob_templates, the STRUCT_MOB images
// of monsters, merchants, mounts and summons) and the generators that put
// them in the world (gamedata.npc_generators). Saves reach the running TMSRVs
// through the notify triggers; NPCs already alive keep what they were born
// with.

const npcReloadNote = "Salvo. Os canais recarregam em cerca de 1 segundo; os NPCs já nascidos continuam como estão até renascerem."

const npcListLimit = 300

func (s *Server) npcRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /gamedata/npcs", s.auth(s.npcsPage))
	mux.HandleFunc("POST /gamedata/npcs", s.auth(s.createNPC))
	mux.HandleFunc("GET /gamedata/npc", s.auth(s.npcPage))
	mux.HandleFunc("POST /gamedata/npc", s.auth(s.saveNPC))
	mux.HandleFunc("POST /gamedata/npc/delete", s.auth(s.deleteNPC))
	mux.HandleFunc("GET /gamedata/generators", s.auth(s.generatorsPage))
	mux.HandleFunc("GET /gamedata/generators/{id}", s.auth(s.generatorPage))
	mux.HandleFunc("POST /gamedata/generators/{id}", s.auth(s.saveGenerator))
	mux.HandleFunc("POST /gamedata/generators/{id}/delete", s.auth(s.deleteGenerator))
}

// ---- templates ----

// npcKinds are the template kinds the panel edits (the class templates
// belong to character creation).
var npcKinds = []struct{ Kind, Label string }{
	{store.TemplateNPC, "Monstros e NPCs"},
	{store.TemplateNPCBase, "Montarias e evocações"},
}

func npcKind(v string) string {
	if v == store.TemplateNPCBase {
		return v
	}
	return store.TemplateNPC
}

func npcURL(kind, name, extra string) string {
	u := "/gamedata/npc?kind=" + url.QueryEscape(kind) + "&name=" + url.QueryEscape(name)
	if extra != "" {
		u += "&" + extra
	}
	return u
}

// npcNameOK: a template name is a file name of the C++ server and the key
// the generators use.
var npcNameOK = regexp.MustCompile(`^[A-Za-z0-9_@.\-]{1,15}$`)

type npcSummary struct {
	Kind, Name, GameName string
	Level                int
	HP, Exp              int64
	Merchant             int
	Bad                  bool // the image does not decode
}

func summarizeNPC(t store.MobTemplate) npcSummary {
	sum := npcSummary{Kind: t.Kind, Name: t.Name}
	var m wire.Mob
	if wire.UnmarshalLegacyMob(t.Content, &m) != nil {
		sum.Bad = true
		return sum
	}
	sum.GameName = wire.DecodeGame(wire.CString(m.Name[:]))
	sum.Level, sum.HP, sum.Exp = int(m.BStatus.Level), m.BStatus.MaxHP, m.Exp
	sum.Merchant = int(m.BStatus.Merchant & 0x0F)
	return sum
}

func (s *Server) npcsPage(w http.ResponseWriter, r *http.Request) {
	q, kind := r.URL.Query().Get("q"), npcKind(r.URL.Query().Get("kind"))
	list, total, err := s.st.SearchMobTemplates(r.Context(), kind, q, npcListLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	rows := make([]npcSummary, len(list))
	for i, t := range list {
		rows[i] = summarizeNPC(t)
	}
	changes, err := s.st.RecentChanges(r.Context(), "npc", "", 15)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, http.StatusOK, "npcs", map[string]any{"Title": "NPCs", "Q": q, "Kind": kind, "Kinds": npcKinds, "NPCs": rows,
		"Total": total, "Limit": npcListLimit, "Error": r.URL.Query().Get("error"), "Changes": changes})
}

// createNPC copies a template under a new name.
func (s *Server) createNPC(w http.ResponseWriter, r *http.Request) {
	kind := npcKind(r.PostFormValue("kind"))
	name, from := strings.TrimSpace(r.PostFormValue("name")), strings.TrimSpace(r.PostFormValue("from"))
	back := func(msg string) {
		http.Redirect(w, r, "/gamedata/npcs?kind="+url.QueryEscape(kind)+"&error="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if !npcNameOK.MatchString(name) {
		back("O nome do modelo deve ter de 1 a 15 letras, números, _ ou @, sem espaços nem acentos.")
		return
	}
	if kind == store.TemplateNPCBase && !game.IsNPCBaseName(name) {
		back("O servidor só lê as montarias e evocações que conhece pelo nome; edite uma das que existem.")
		return
	}
	content := make([]byte, wire.LegacyMobSize)
	summary := "criado"
	if from != "" {
		src, err := s.st.GetMobTemplate(r.Context(), kind, from)
		if errors.Is(err, store.ErrNotFound) {
			back("O modelo de origem " + from + " não existe.")
			return
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		content, summary = src, "copiado de "+from
	}
	// The name the game shows starts as the template's name.
	var m wire.Mob
	if err := wire.UnmarshalLegacyMob(content, &m); err != nil {
		back("O modelo de origem não pôde ser lido.")
		return
	}
	m.Name = [16]byte{}
	wire.PutCString(m.Name[:], name)
	content, err := wire.PatchLegacyMob(content, &m)
	if err != nil {
		back("Não foi possível criar: " + err.Error())
		return
	}
	if err := s.st.SaveMobTemplate(r.Context(), store.MobTemplate{Kind: kind, Name: name, Content: content}, true, sessionOf(r).user, summary); err != nil {
		back("Não foi possível criar (o nome já existe?): " + err.Error())
		return
	}
	http.Redirect(w, r, npcURL(kind, name, ""), http.StatusSeeOther)
}

// npcField is one number of the template shown in the form.
type npcField struct {
	Key, Label, Help string
	Lo, Hi           int64
	get              func(*wire.Mob) int64
	set              func(*wire.Mob, int64)
	Value            int64
}

type npcGroup struct {
	Title, Help string
	Fields      []npcField
}

// num builds a field over any integer of the mob.
func num[T int8 | uint8 | int16 | uint16 | uint32 | int64](key, label, help string, lo, hi int64, at func(*wire.Mob) *T) npcField {
	return npcField{Key: key, Label: label, Help: help, Lo: lo, Hi: hi,
		get: func(m *wire.Mob) int64 { return int64(*at(m)) },
		set: func(m *wire.Mob, v int64) { *at(m) = T(v) }}
}

// bitField builds a field over some bits of a byte.
func bitField(key, label, help string, shift, width uint, at func(*wire.Mob) *uint8) npcField {
	mask := uint8(1<<width-1) << shift
	return npcField{Key: key, Label: label, Help: help, Lo: 0, Hi: int64(1<<width - 1),
		get: func(m *wire.Mob) int64 { return int64(*at(m) & mask >> shift) },
		set: func(m *wire.Mob, v int64) { *at(m) = *at(m)&^mask | uint8(v)<<shift&mask }}
}

const (
	max32 = 1<<31 - 1
	min32 = -1 << 31
)

func statusFields(prefix string, at func(*wire.Mob) *wire.Status) []npcField {
	f := []npcField{
		num(prefix+"level", "Nível", "o jogo mostra nível + 1", -32768, 32767, func(m *wire.Mob) *int16 { return &at(m).Level }),
		num(prefix+"max_hp", "HP máximo", "", 0, max32, func(m *wire.Mob) *int64 { return &at(m).MaxHP }),
		num(prefix+"cur_hp", "HP atual", "nasce com o HP máximo", 0, max32, func(m *wire.Mob) *int64 { return &at(m).CurHP }),
		num(prefix+"max_mp", "MP máximo", "no modelo base de um gerador: skill com que o NPC nasce", 0, max32, func(m *wire.Mob) *int64 { return &at(m).MaxMP }),
		num(prefix+"cur_mp", "MP atual", "", 0, max32, func(m *wire.Mob) *int64 { return &at(m).CurMP }),
		num(prefix+"attack", "Ataque", "", min32, max32, func(m *wire.Mob) *int64 { return &at(m).Attack }),
		num(prefix+"defense", "Defesa", "", min32, max32, func(m *wire.Mob) *int64 { return &at(m).Defense }),
		num(prefix+"str", "FOR", "", 0, 65535, func(m *wire.Mob) *int64 { return &at(m).STR }),
		num(prefix+"int", "INT", "", 0, 65535, func(m *wire.Mob) *int64 { return &at(m).INT }),
		num(prefix+"dex", "DES", "", 0, 65535, func(m *wire.Mob) *int64 { return &at(m).DEX }),
		num(prefix+"con", "CON", "", 0, 65535, func(m *wire.Mob) *int64 { return &at(m).CON }),
		bitField(prefix+"merchant", "Tipo de NPC", "0 monstro, 1 loja, 3 loja de skills, demais: NPCs de função", 0, 4, func(m *wire.Mob) *uint8 { return &at(m).Merchant }),
		bitField(prefix+"direction", "Direção", "zerada ao nascer", 4, 4, func(m *wire.Mob) *uint8 { return &at(m).Merchant }),
		bitField(prefix+"speed", "Velocidade", "", 0, 4, func(m *wire.Mob) *uint8 { return &at(m).Move }),
		bitField(prefix+"chaos", "Chaos rate", "", 4, 4, func(m *wire.Mob) *uint8 { return &at(m).Move }),
	}
	for i := range 4 {
		f = append(f, num(fmt.Sprintf("%smastery%d", prefix, i), fmt.Sprintf("Maestria %d", i+1), "", 0, 65535,
			func(m *wire.Mob) *uint16 { return &at(m).Mastery[i] }))
	}
	return f
}

// npcGroups is the form: every number of the template the server reads.
func npcGroups() []npcGroup {
	groups := []npcGroup{
		{Title: "Recompensa e comportamento", Fields: []npcField{
			num("exp", "Experiência", "base por morte, antes do ajuste por nível e zona", 0, 1<<62, func(m *wire.Mob) *int64 { return &m.Exp }),
			num("gold", "Gold", "", 0, max32, func(m *wire.Mob) *int64 { return &m.Gold }),
			num("cape", "Clã (CapeInfo)", "4 = não dá experiência nem drop; 7/8 reinos", -128, 127, func(m *wire.Mob) *int8 { return &m.CapeInfo }),
			bitField("info_merchant", "Info: mercador", "", 0, 6, func(m *wire.Mob) *uint8 { return &m.Info }),
			bitField("info_city", "Info: cidade", "", 6, 2, func(m *wire.Mob) *uint8 { return &m.Info }),
			num("guild", "Guilda", "", 0, 65535, func(m *wire.Mob) *uint16 { return &m.GuildIndex }),
			num("guild_member", "Cargo na guilda", "", -128, 127, func(m *wire.Mob) *int8 { return &m.GuildMemberType }),
			num("class", "Classe", "", 0, 255, func(m *wire.Mob) *uint8 { return &m.ClassInfo }),
			num("affect_info", "AffectInfo", "", 0, 255, func(m *wire.Mob) *uint8 { return &m.AffectInfo }),
			num("quest_info", "QuestInfo", "", 0, 65535, func(m *wire.Mob) *uint16 { return &m.QuestInfo }),
		}},
		{Title: "Status base", Help: "O que o servidor usa para calcular o NPC ao nascer.",
			Fields: statusFields("b_", func(m *wire.Mob) *wire.Status { return &m.BStatus })},
		{Title: "Status atual", Help: "A cópia calculada guardada no modelo. O nível daqui entra na conta da experiência; os NPCs de troca usam os níveis 500 a 509.",
			Fields: statusFields("s_", func(m *wire.Mob) *wire.Status { return &m.Status })},
		{Title: "Combate e skills", Fields: []npcField{
			num("critical", "Crítico", "", 0, 255, func(m *wire.Mob) *uint8 { return &m.Critical }),
			num("save_mana", "Save mana", "", 0, 255, func(m *wire.Mob) *uint8 { return &m.SaveMana }),
			num("magic", "Incremento mágico", "", 0, 65535, func(m *wire.Mob) *uint16 { return &m.MagicIncrement }),
			num("regen_hp", "Regen. HP", "", 0, 255, func(m *wire.Mob) *uint8 { return &m.RegenHP }),
			num("regen_mp", "Regen. MP", "", 0, 255, func(m *wire.Mob) *uint8 { return &m.RegenMP }),
			num("learn0", "Skills aprendidas", "bits", 0, 1<<32-1, func(m *wire.Mob) *uint32 { return &m.Learn[0] }),
			num("learn1", "Skills aprendidas (2)", "bits", 0, 1<<32-1, func(m *wire.Mob) *uint32 { return &m.Learn[1] }),
			num("status_point", "Pontos de status", "", 0, 65535, func(m *wire.Mob) *uint16 { return &m.StatusPoint }),
			num("master_point", "Pontos de maestria", "", 0, 65535, func(m *wire.Mob) *uint16 { return &m.MasterPoint }),
			num("skill_point", "Pontos de skill", "", 0, 65535, func(m *wire.Mob) *uint16 { return &m.SkillPoint }),
		}},
	}
	last := &groups[len(groups)-1]
	for i, name := range []string{"Fogo", "Gelo", "Sagrado", "Trovão"} {
		last.Fields = append(last.Fields, num(fmt.Sprintf("resist%d", i), "Resist. "+name, "", 0, 255, func(m *wire.Mob) *uint8 { return &m.Resist[i] }))
	}
	for i := range 4 {
		last.Fields = append(last.Fields, num(fmt.Sprintf("skillbar%d", i), fmt.Sprintf("Barra de skill %d", i+1), "", 0, 255, func(m *wire.Mob) *uint8 { return &m.SkillBar1[i] }))
	}
	return groups
}

// npcItemRow is one equipment or inventory slot of the form.
type npcItemRow struct {
	Slot     int
	Item     wire.Item
	ItemName string
}

func npcItemRows(items []wire.Item, names map[int]string) []npcItemRow {
	rows := make([]npcItemRow, len(items))
	for i, it := range items {
		rows[i] = npcItemRow{Slot: i, Item: it, ItemName: names[int(it.Index)]}
	}
	return rows
}

func (s *Server) npcPage(w http.ResponseWriter, r *http.Request) {
	kind, name := npcKind(r.URL.Query().Get("kind")), r.URL.Query().Get("name")
	content, err := s.st.GetMobTemplate(r.Context(), kind, name)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	var m wire.Mob
	if err := wire.UnmarshalLegacyMob(content, &m); err != nil {
		s.fail(w, err)
		return
	}
	msg := ""
	if r.URL.Query().Has("saved") {
		msg = npcReloadNote
	}
	s.renderNPC(w, r, http.StatusOK, kind, name, &m, msg, "")
}

func (s *Server) renderNPC(w http.ResponseWriter, r *http.Request, code int, kind, name string, m *wire.Mob, msg, errMsg string) {
	ctx := r.Context()
	var ids []int
	for _, it := range m.Equip {
		ids = append(ids, int(it.Index))
	}
	for _, it := range m.Inventory {
		ids = append(ids, int(it.Index))
	}
	names, err := s.st.ItemNames(ctx, ids)
	if err != nil {
		s.fail(w, err)
		return
	}
	groups := npcGroups()
	for g := range groups {
		for f := range groups[g].Fields {
			groups[g].Fields[f].Value = groups[g].Fields[f].get(m)
		}
	}
	var used []int
	if kind == store.TemplateNPC {
		if used, err = s.st.GeneratorsUsing(ctx, name); err != nil {
			s.fail(w, err)
			return
		}
	}
	changes, err := s.st.RecentChanges(ctx, "npc", kind+"/"+name, 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	gameName := wire.DecodeGame(wire.CString(m.Name[:]))
	s.page(w, r, code, "npc", map[string]any{"Title": "NPC · " + name, "Kind": kind, "Name": name, "GameName": gameName,
		"DropName": strings.NewReplacer("_", " ", "@", " ").Replace(gameName), "Groups": groups,
		"Equip": npcItemRows(m.Equip[:], names), "Inventory": npcItemRows(m.Inventory[:], names),
		"Merchant": m.BStatus.Merchant&0x0F != 0, "Used": used, "Msg": msg, "Error": errMsg, "Changes": changes})
}

func (s *Server) saveNPC(w http.ResponseWriter, r *http.Request) {
	kind, name := npcKind(r.PostFormValue("kind")), r.PostFormValue("name")
	orig, err := s.st.GetMobTemplate(r.Context(), kind, name)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	var m wire.Mob
	if err := wire.UnmarshalLegacyMob(orig, &m); err != nil {
		s.fail(w, err)
		return
	}
	was := m
	var errs []string
	gameName := wire.EncodeGame(strings.TrimSpace(r.PostFormValue("game_name")))
	if gameName == "" || len(gameName) > 15 {
		errs = append(errs, "O nome no jogo deve ter de 1 a 15 caracteres.")
	} else {
		m.Name = [16]byte{}
		wire.PutCString(m.Name[:], gameName)
	}
	for _, g := range npcGroups() {
		for _, f := range g.Fields {
			raw := strings.TrimSpace(r.PostFormValue(f.Key))
			if raw == "" {
				raw = "0"
			}
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || v < f.Lo || v > f.Hi {
				errs = append(errs, fmt.Sprintf("%s (%s) deve ser um número de %d a %d.", f.Label, g.Title, f.Lo, f.Hi))
				continue
			}
			f.set(&m, v)
		}
	}
	readItems := func(prefix, label string, items []wire.Item) {
		for i := range items {
			var vals [7]int
			// Some images of the C++ carry ids above the item list (65535 in the
			// mounts): the field takes whatever the template can hold.
			for k, lim := range [7]int{65535, 255, 255, 255, 255, 255, 255} {
				raw := strings.TrimSpace(r.PostFormValue(fmt.Sprintf("%s%d_%d", prefix, i, k)))
				if raw == "" {
					raw = "0"
				}
				v, err := strconv.Atoi(raw)
				if err != nil || v < 0 || v > lim {
					errs = append(errs, fmt.Sprintf("%s, slot %d: valor inválido %q (0 a %d).", label, i, raw, lim))
					continue
				}
				vals[k] = v
			}
			it := wire.Item{Index: uint16(vals[0])}
			if it.Index != 0 {
				for e := range 3 {
					it.Effect[e] = wire.ItemEffect{Index: uint8(vals[1+e*2]), Value: uint16(vals[2+e*2])}
				}
			}
			items[i] = it
		}
	}
	readItems("eq", "Equipamento", m.Equip[:])
	readItems("inv", "Inventário", m.Inventory[:])
	if len(errs) > 0 {
		s.renderNPC(w, r, http.StatusUnprocessableEntity, kind, name, &m, "", strings.Join(errs, " "))
		return
	}
	content, err := wire.PatchLegacyMob(orig, &m)
	if err != nil {
		s.renderNPC(w, r, http.StatusUnprocessableEntity, kind, name, &m, "", "Valor fora do que o modelo guarda: "+err.Error())
		return
	}
	summary := npcChangeSummary(&was, &m)
	if summary == "" {
		http.Redirect(w, r, npcURL(kind, name, "saved=1"), http.StatusSeeOther)
		return
	}
	if err := s.st.SaveMobTemplate(r.Context(), store.MobTemplate{Kind: kind, Name: name, Content: content}, false, sessionOf(r).user, summary); err != nil {
		s.renderNPC(w, r, http.StatusUnprocessableEntity, kind, name, &m, "", "O banco recusou: "+err.Error())
		return
	}
	http.Redirect(w, r, npcURL(kind, name, "saved=1"), http.StatusSeeOther)
}

// npcChangeSummary says what a save changed ("" = nothing).
func npcChangeSummary(was, now *wire.Mob) string {
	var parts []string
	if was.Name != now.Name {
		parts = append(parts, fmt.Sprintf("nome %q", wire.DecodeGame(wire.CString(now.Name[:]))))
	}
	for _, g := range npcGroups() {
		for _, f := range g.Fields {
			if a, b := f.get(was), f.get(now); a != b {
				label := f.Label
				if strings.HasPrefix(f.Key, "s_") {
					label += " (atual)"
				}
				parts = append(parts, fmt.Sprintf("%s %d → %d", label, a, b))
			}
		}
	}
	if was.Equip != now.Equip {
		parts = append(parts, "equipamento")
	}
	if was.Inventory != now.Inventory {
		parts = append(parts, "inventário")
	}
	if len(parts) > 8 {
		parts = append(parts[:8], fmt.Sprintf("e mais %d", len(parts)-8))
	}
	return strings.Join(parts, ", ")
}

func (s *Server) deleteNPC(w http.ResponseWriter, r *http.Request) {
	kind, name := npcKind(r.PostFormValue("kind")), r.PostFormValue("name")
	fail := func(msg string) {
		http.Redirect(w, r, "/gamedata/npcs?kind="+url.QueryEscape(kind)+"&error="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	if kind == store.TemplateNPC {
		used, err := s.st.GeneratorsUsing(r.Context(), name)
		if err != nil {
			s.fail(w, err)
			return
		}
		if len(used) > 0 {
			fail(fmt.Sprintf("%s não foi removido: %d gerador(es) nascem dele (o primeiro é o %d). Troque o modelo nesses geradores antes.", name, len(used), used[0]))
			return
		}
		if name == "Base" || name == "Vinha" {
			fail(name + " não pode ser removido: o servidor usa este modelo (loja de jogador e Muro de Espinhos).")
			return
		}
	}
	if err := s.st.DeleteMobTemplate(r.Context(), kind, name, sessionOf(r).user); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/gamedata/npcs?kind="+url.QueryEscape(kind), http.StatusSeeOther)
}

// ---- generators ----

const generatorListLimit = 300

type generatorRow struct {
	store.Generator
	X, Y int // where it is born
}

func (s *Server) generatorsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	list, total, err := s.st.SearchGenerators(r.Context(), store.GeneratorFilter{Q: q, Limit: generatorListLimit})
	if err != nil {
		s.fail(w, err)
		return
	}
	rows := make([]generatorRow, len(list))
	for i, g := range list {
		rows[i].Generator = g
		for _, seg := range g.Segments {
			if seg.Segment == 0 {
				rows[i].X, rows[i].Y = seg.X, seg.Y
			}
		}
	}
	next, err := s.st.NextGeneratorID(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	changes, err := s.st.RecentChanges(r.Context(), "generator", "", 15)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, http.StatusOK, "generators", map[string]any{"Title": "Geradores", "Q": q, "Generators": rows, "Total": total,
		"Limit": generatorListLimit, "Next": next, "CanCreate": next < store.MaxGenerators, "Changes": changes})
}

func generatorID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil && id >= 0 && id < store.MaxGenerators
}

func (s *Server) generatorPage(w http.ResponseWriter, r *http.Request) {
	id, ok := generatorID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	g, err := s.st.GetGenerator(r.Context(), id)
	isNew := errors.Is(err, store.ErrNotFound)
	if isNew {
		g = &store.Generator{ID: id, Minute: 4, MaxMobs: 1}
	} else if err != nil {
		s.fail(w, err)
		return
	}
	msg := ""
	if r.URL.Query().Has("saved") {
		msg = npcReloadNote
	}
	s.renderGenerator(w, r, http.StatusOK, g, isNew, msg, "")
}

// generatorNote explains the ids the code treats specially.
func generatorNote(id int) string {
	switch {
	case id == 0:
		return "O gerador 0 nunca é usado pelo servidor."
	case id == 1 || id == 2 || id == 5 || id == 6 || id == 7:
		return "Este gerador não nasce pelo tempo: é disparado pelo servidor (reis, torres e eventos)."
	}
	return ""
}

func (s *Server) renderGenerator(w http.ResponseWriter, r *http.Request, code int, g *store.Generator, isNew bool, msg, errMsg string) {
	segs := make([]store.GeneratorSegment, 5)
	for i := range segs {
		segs[i].Segment = i
	}
	for _, seg := range g.Segments {
		if seg.Segment >= 0 && seg.Segment < len(segs) {
			segs[seg.Segment] = seg
		}
	}
	pad := func(in []string) []string {
		out := make([]string, 4)
		copy(out, in)
		return out
	}
	var warn []string
	for _, name := range []string{g.Leader, g.Follower} {
		if name == "" {
			continue
		}
		if _, err := s.st.GetMobTemplate(r.Context(), store.TemplateNPC, name); errors.Is(err, store.ErrNotFound) {
			warn = append(warn, "O modelo "+name+" não existe: o gerador fica sem ele até o modelo ser criado.")
		} else if err != nil {
			s.fail(w, err)
			return
		}
	}
	if g.Leader == "" && !isNew {
		warn = append(warn, "Sem líder o gerador não cria nada.")
	}
	changes, err := s.st.RecentChanges(r.Context(), "generator", strconv.Itoa(g.ID), 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, code, "generator", map[string]any{"Title": fmt.Sprintf("Gerador %d", g.ID), "Gen": g, "New": isNew,
		"Segments": segs, "Fight": pad(g.FightActions), "Die": pad(g.DieActions), "Note": generatorNote(g.ID),
		"Random": g.Minute >= 500, "Warnings": warn, "Msg": msg, "Error": errMsg, "Changes": changes})
}

// trimActions drops the empty lines at the end (the position of a line is
// its number).
func trimActions(in []string) []string {
	n := len(in)
	for n > 0 && in[n-1] == "" {
		n--
	}
	return in[:n]
}

func (s *Server) saveGenerator(w http.ResponseWriter, r *http.Request) {
	id, ok := generatorID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, err := s.st.GetGenerator(r.Context(), id)
	isNew := errors.Is(err, store.ErrNotFound)
	if err != nil && !isNew {
		s.fail(w, err)
		return
	}
	f := &formReader{r: r}
	g := &store.Generator{ID: id,
		Leader: strings.TrimSpace(r.PostFormValue("leader")), Follower: strings.TrimSpace(r.PostFormValue("follower"))}
	g.Minute = f.int("minute", "Intervalo", -1, 100000)
	g.MaxMobs = f.int("max_mobs", "Máximo de NPCs", 0, 30000)
	g.MinGroup = f.int("min_group", "Grupo mínimo", 0, 12)
	g.MaxGroup = f.int("max_group", "Grupo máximo", 0, 12)
	g.Formation = f.int("formation", "Formação", 0, 255)
	g.Route = f.int("route", "Rota", 0, 255)
	for i := range 5 {
		label := fmt.Sprintf("(ponto %d)", i)
		seg := store.GeneratorSegment{Segment: i,
			X: f.int(fmt.Sprintf("seg_x%d", i), "X "+label, 0, 8191), Y: f.int(fmt.Sprintf("seg_y%d", i), "Y "+label, 0, 8191),
			Range: f.int(fmt.Sprintf("seg_range%d", i), "Raio "+label, 0, 1000), Wait: f.int(fmt.Sprintf("seg_wait%d", i), "Espera "+label, 0, 100000),
			Action: strings.TrimSpace(r.PostFormValue(fmt.Sprintf("seg_action%d", i)))}
		if len(wire.EncodeGame(seg.Action)) > 79 {
			f.errs = append(f.errs, "A fala "+label+" passa de 79 caracteres.")
		}
		if seg != (store.GeneratorSegment{Segment: i}) {
			g.Segments = append(g.Segments, seg)
		}
	}
	for i := range 4 {
		g.FightActions = append(g.FightActions, strings.TrimSpace(r.PostFormValue(fmt.Sprintf("fight%d", i))))
		g.DieActions = append(g.DieActions, strings.TrimSpace(r.PostFormValue(fmt.Sprintf("die%d", i))))
	}
	g.FightActions, g.DieActions = trimActions(g.FightActions), trimActions(g.DieActions)
	if g.MinGroup > g.MaxGroup {
		f.errs = append(f.errs, "O grupo mínimo não pode passar do máximo.")
	}
	if g.Minute >= 500 {
		// A "random" generator: point 0 holds a range of generator ids.
		if len(g.Segments) == 0 || g.Segments[0].Segment != 0 || g.Segments[0].Y <= g.Segments[0].X {
			f.errs = append(f.errs, "Com intervalo de 500 ou mais o ponto 0 é uma faixa de geradores: X é o primeiro id e Y o último + 1 (Y maior que X).")
		}
	} else if g.Leader != "" && (len(g.Segments) == 0 || g.Segments[0].Segment != 0 || g.Segments[0].X == 0 || g.Segments[0].Y == 0) {
		f.errs = append(f.errs, "Informe a posição do ponto 0 (onde o NPC nasce).")
	}
	if e := f.err(); e != "" {
		s.renderGenerator(w, r, http.StatusUnprocessableEntity, g, isNew, "", e)
		return
	}
	if err := s.st.SaveGenerator(r.Context(), g, sessionOf(r).user); err != nil {
		s.renderGenerator(w, r, http.StatusUnprocessableEntity, g, isNew, "", "O banco recusou: "+err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/gamedata/generators/%d?saved=1", id), http.StatusSeeOther)
}

func (s *Server) deleteGenerator(w http.ResponseWriter, r *http.Request) {
	id, ok := generatorID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.st.DeleteGenerator(r.Context(), id, sessionOf(r).user); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/gamedata/generators", http.StatusSeeOther)
}

// SeedGenerators copies config/npc_generators.yaml into an empty
// gamedata.npc_generators: the first start after the generators moved to the
// database. The file is not read again.
func SeedGenerators(ctx context.Context, st *store.Store, configDir string) (int, error) {
	gens, err := game.ReadGeneratorsFile(filepath.Join(configDir, game.GeneratorsFile))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return st.SeedGenerators(ctx, gens, "npc_generators.yaml (import)")
}
