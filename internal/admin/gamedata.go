package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"dbsrv/internal/acctedit"
	"dbsrv/internal/game"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Game data screens (gamedata schema). Saves go straight to Postgres; the
// notify triggers make running TMSRVs reload within about a second.

const reloadNote = "Salvo. Os TMSRV em execução recarregam em cerca de 1 segundo."

// formReader collects form values with range checks.
type formReader struct {
	r    *http.Request
	errs []string
}

func (f *formReader) int(name, label string, lo, hi int) int {
	raw := strings.TrimSpace(f.r.PostFormValue(name))
	if raw == "" {
		raw = "0"
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < lo || v > hi {
		f.errs = append(f.errs, fmt.Sprintf("%s deve ser um número de %d a %d.", label, lo, hi))
		return 0
	}
	return v
}

func (f *formReader) err() string { return strings.Join(f.errs, " ") }

// visibleSpaces shows trailing spaces of a drop name.
func visibleSpaces(s string) string {
	t := strings.TrimRight(s, " ")
	return t + strings.Repeat("␣", len(s)-len(t))
}

func (s *Server) gamedataRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /gamedata/items", s.auth(s.itemsPage))
	mux.HandleFunc("GET /gamedata/items/{id}", s.auth(s.itemPage))
	mux.HandleFunc("POST /gamedata/items/{id}", s.auth(s.saveItem))
	mux.HandleFunc("GET /gamedata/skills", s.auth(s.skillsPage))
	mux.HandleFunc("GET /gamedata/skills/{id}", s.auth(s.skillPage))
	mux.HandleFunc("POST /gamedata/skills/{id}", s.auth(s.saveSkill))
	mux.HandleFunc("GET /gamedata/drops", s.auth(s.dropsPage))
	mux.HandleFunc("POST /gamedata/drops", s.auth(s.createDrop))
	mux.HandleFunc("GET /gamedata/drop", s.auth(s.dropPage))
	mux.HandleFunc("POST /gamedata/drop", s.auth(s.saveDrop))
	mux.HandleFunc("POST /gamedata/drop/delete", s.auth(s.deleteDrop))
	mux.HandleFunc("GET /gamedata/droprates", s.auth(s.dropRatesPage))
	mux.HandleFunc("POST /gamedata/droprates", s.auth(s.saveDropRates))
	mux.HandleFunc("GET /gamedata/teleports", s.auth(s.teleportsPage))
	mux.HandleFunc("POST /gamedata/teleports", s.auth(s.saveTeleport))
	mux.HandleFunc("POST /gamedata/teleports/delete", s.auth(s.deleteTeleport))
}

// ---- items ----

func (s *Server) itemsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	items, err := s.st.SearchItems(r.Context(), q, 200)
	if err != nil {
		s.fail(w, err)
		return
	}
	changes, err := s.st.RecentChanges(r.Context(), "", "", 15)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, http.StatusOK, "items", map[string]any{"Title": "Itens", "Q": q, "Items": items, "Changes": changes})
}

type effectRow struct {
	Slot, Effect, Value int
}

func (s *Server) itemPage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 || id > 6499 {
		http.NotFound(w, r)
		return
	}
	it, err := s.st.GetItem(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		it = &store.Item{ID: id} // new item
	} else if err != nil {
		s.fail(w, err)
		return
	}
	msg := ""
	if r.URL.Query().Has("saved") {
		msg = reloadNote
	}
	s.renderItem(w, r, http.StatusOK, it, msg, "")
}

func (s *Server) renderItem(w http.ResponseWriter, r *http.Request, code int, it *store.Item, msg, errMsg string) {
	names, err := s.effectNames(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	changes, err := s.st.RecentChanges(r.Context(), "item", strconv.Itoa(it.ID), 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	rows := make([]effectRow, 12)
	for i, e := range it.Effects {
		rows[i] = effectRow{Slot: i, Effect: e.Effect, Value: e.Value}
	}
	s.page(w, r, code, "item", map[string]any{"Title": fmt.Sprintf("Item %d", it.ID), "Item": it, "Effects": rows,
		"EffectNames": names, "Msg": msg, "Error": errMsg, "Changes": changes, "New": it.Name == ""})
}

func (s *Server) saveItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 1 || id > 6499 {
		http.NotFound(w, r)
		return
	}
	f := &formReader{r: r}
	it := &store.Item{ID: id, Name: strings.TrimSpace(r.PostFormValue("name"))}
	it.Level = f.int("level", "Nível", -32768, 32767)
	it.ReqStr = f.int("req_str", "FOR", -32768, 32767)
	it.ReqInt = f.int("req_int", "INT", -32768, 32767)
	it.ReqDex = f.int("req_dex", "DES", -32768, 32767)
	it.ReqCon = f.int("req_con", "CON", -32768, 32767)
	it.Price = f.int("price", "Preço", 0, 2000000000)
	it.Unique = f.int("unique_type", "Unique", -32768, 32767)
	it.Pos = f.int("pos", "Posição", 0, 1<<31-1)
	it.Extreme = f.int("extreme", "Extreme", -32768, 32767)
	it.Grade = f.int("grade", "Grade", -32768, 32767)
	it.Mesh1 = f.int("mesh1", "Mesh 1", -32768, 32767)
	it.Mesh2 = f.int("mesh2", "Mesh 2", -2147483648, 2147483647)
	for i := range it.Effects {
		it.Effects[i] = store.ItemEffect{
			Effect: f.int(fmt.Sprintf("ef%d", i), fmt.Sprintf("Efeito %d", i+1), 0, 255),
			Value:  f.int(fmt.Sprintf("ev%d", i), fmt.Sprintf("Valor %d", i+1), -32768, 32767),
		}
	}
	if it.Name == "" {
		f.errs = append(f.errs, "O nome é obrigatório.")
	}
	if e := f.err(); e != "" {
		s.renderItem(w, r, http.StatusUnprocessableEntity, it, "", e)
		return
	}
	if err := s.st.SaveItem(r.Context(), it, sessionOf(r).user); err != nil {
		s.renderItem(w, r, http.StatusUnprocessableEntity, it, "", "O banco recusou: "+err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/gamedata/items/%d?saved=1", id), http.StatusSeeOther)
}

// ---- skills ----

type skillField struct {
	Key, Label, Help string
	Value            int
}

func skillFields(k *store.Skill) []skillField {
	return []skillField{
		{"points", "Pontos para aprender", "", k.Points},
		{"target", "Alvo", "", k.Target},
		{"mana", "Mana", "custo base", k.Mana},
		{"delay", "Recarga (s)", "", k.Delay},
		{"range", "Alcance", "+3 de tolerância", k.Range},
		{"instance_type", "Tipo", "1 sagrado, 2-5 elemental, 6 cura, 7 acalmar, 8 limpar, 10 provocar, 12 Chamas", k.InstanceType},
		{"instance_value", "Valor base", "dano/cura", k.InstanceValue},
		{"tick_type", "Efeito (tick)", "aplicado também em monstros", k.TickType},
		{"tick_value", "Valor do tick", "", k.TickValue},
		{"affect_type", "Buff (affect)", "só jogadores", k.AffectType},
		{"affect_value", "Valor do buff", "", k.AffectValue},
		{"time", "Duração", "x (100 + maestria) / 100", k.Time},
		{"instance_attribute", "Atributo da instância", "", k.InstanceAttribute},
		{"tick_attribute", "Atributo do tick", "", k.TickAttribute},
		{"aggressive", "Hostil", "1 = não afeta grupo/guild", k.Aggressive},
		{"max_target", "Máx. alvos", "", k.MaxTarget},
		{"party_check", "Só grupo/guild", "", k.PartyCheck},
		{"affect_resist", "Resistência", "1-3: pode anular o efeito", k.AffectResist},
		{"passive", "Passiva", "1 = não pode ser usada", k.Passive},
		{"force_damage", "Force damage", "", k.ForceDamage},
	}
}

func (s *Server) skillsPage(w http.ResponseWriter, r *http.Request) {
	skills, err := s.st.ListSkills(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, http.StatusOK, "skills", map[string]any{"Title": "Skills", "Skills": skills})
}

func (s *Server) skillPage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 || id > 255 {
		http.NotFound(w, r)
		return
	}
	k, err := s.st.GetSkill(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		k = &store.Skill{ID: id}
	} else if err != nil {
		s.fail(w, err)
		return
	}
	msg := ""
	if r.URL.Query().Has("saved") {
		msg = reloadNote
	}
	s.renderSkill(w, r, http.StatusOK, k, msg, "")
}

func (s *Server) renderSkill(w http.ResponseWriter, r *http.Request, code int, k *store.Skill, msg, errMsg string) {
	changes, err := s.st.RecentChanges(r.Context(), "skill", strconv.Itoa(k.ID), 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, code, "skill", map[string]any{"Title": fmt.Sprintf("Skill %d", k.ID), "Skill": k,
		"Fields": skillFields(k), "Msg": msg, "Error": errMsg, "Changes": changes})
}

func (s *Server) saveSkill(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id < 0 || id > 255 {
		http.NotFound(w, r)
		return
	}
	f := &formReader{r: r}
	k := &store.Skill{ID: id, Name: strings.TrimSpace(r.PostFormValue("name")),
		Act1: strings.TrimSpace(r.PostFormValue("act1")), Act2: strings.TrimSpace(r.PostFormValue("act2"))}
	const big = 2000000000
	ptrs := map[string]*int{"points": &k.Points, "target": &k.Target, "mana": &k.Mana, "delay": &k.Delay,
		"range": &k.Range, "instance_type": &k.InstanceType, "instance_value": &k.InstanceValue,
		"tick_type": &k.TickType, "tick_value": &k.TickValue, "affect_type": &k.AffectType,
		"affect_value": &k.AffectValue, "time": &k.Time, "instance_attribute": &k.InstanceAttribute,
		"tick_attribute": &k.TickAttribute, "aggressive": &k.Aggressive, "max_target": &k.MaxTarget,
		"party_check": &k.PartyCheck, "affect_resist": &k.AffectResist, "passive": &k.Passive,
		"force_damage": &k.ForceDamage}
	for _, fd := range skillFields(k) {
		*ptrs[fd.Key] = f.int(fd.Key, fd.Label, -big, big)
	}
	if e := f.err(); e != "" {
		s.renderSkill(w, r, http.StatusUnprocessableEntity, k, "", e)
		return
	}
	if err := s.st.SaveSkill(r.Context(), k, sessionOf(r).user); err != nil {
		s.renderSkill(w, r, http.StatusUnprocessableEntity, k, "", "O banco recusou: "+err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/gamedata/skills/%d?saved=1", id), http.StatusSeeOther)
}

// ---- drops ----

func (s *Server) dropsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	drops, err := s.st.SearchDrops(r.Context(), q)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, http.StatusOK, "drops", map[string]any{"Title": "Drops", "Q": q, "Drops": drops,
		"Error": r.URL.Query().Get("error")})
}

func dropURL(name string, extra string) string {
	u := "/gamedata/drop?name=" + url.QueryEscape(name)
	if extra != "" {
		u += "&" + extra
	}
	return u
}

func (s *Server) createDrop(w http.ResponseWriter, r *http.Request) {
	name := r.PostFormValue("name") // not trimmed: trailing spaces matter
	if strings.TrimSpace(name) == "" {
		http.Redirect(w, r, "/gamedata/drops?error="+url.QueryEscape("Informe o nome do monstro."), http.StatusSeeOther)
		return
	}
	d := &store.Drop{Name: name, MaxDrops: 1}
	if err := s.st.SaveDrop(r.Context(), "", d, sessionOf(r).user); err != nil {
		http.Redirect(w, r, "/gamedata/drops?error="+url.QueryEscape("Não foi possível criar: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dropURL(name, ""), http.StatusSeeOther)
}

type dropRow struct {
	Kills, Item, Effect, Value int
	ItemName                   string
}

func (s *Server) dropPage(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	d, err := s.st.GetDrop(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	msg := ""
	if r.URL.Query().Has("saved") {
		msg = reloadNote
	}
	s.renderDrop(w, r, http.StatusOK, name, d, msg, "")
}

func (s *Server) renderDrop(w http.ResponseWriter, r *http.Request, code int, oldName string, d *store.Drop, msg, errMsg string) {
	ids := make([]int, 0, len(d.Items)+len(d.Rate))
	for _, it := range d.Items {
		ids = append(ids, it.Item)
	}
	for _, it := range d.Rate {
		ids = append(ids, it.Item)
	}
	names, err := s.st.ItemNames(r.Context(), ids)
	if err != nil {
		s.fail(w, err)
		return
	}
	rows := make([]dropRow, 0, len(d.Items)+5)
	for _, it := range d.Items {
		rows = append(rows, dropRow{Kills: it.Kills, Item: it.Item, Effect: it.Effect, Value: it.Value, ItemName: names[it.Item]})
	}
	for range 5 {
		rows = append(rows, dropRow{})
	}
	rateRows, used := make([]rateDropRow, 0, len(d.Rate)+5), map[int]bool{}
	for _, it := range d.Rate {
		used[it.Slot] = true
		rateRows = append(rateRows, rateDropRow{RateDrop: it, ItemName: names[it.Item]})
	}
	for slot, added := 0, 0; slot < 64 && added < 5; slot++ {
		if !used[slot] {
			rateRows = append(rateRows, rateDropRow{RateDrop: store.RateDrop{Slot: slot}})
			added++
		}
	}
	changes, err := s.st.RecentChanges(r.Context(), "drop", d.Name, 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, code, "drop", map[string]any{"Title": "Drops · " + strings.TrimSpace(d.Name), "Drop": d,
		"OldName": oldName, "Rows": rows, "RateRows": rateRows, "Msg": msg, "Error": errMsg, "Changes": changes})
}

func (s *Server) saveDrop(w http.ResponseWriter, r *http.Request) {
	oldName := r.PostFormValue("old_name")
	f := &formReader{r: r}
	d := &store.Drop{Name: r.PostFormValue("name"), Section: strings.TrimSpace(r.PostFormValue("section"))}
	d.MaxDrops = f.int("max_drops", "Máx. por morte", 0, 1000)
	d.Gold = f.int("gold", "Gold", 0, wire.MaxLimit)
	d.RateGold = int64(f.int("rate_gold", "Gold por chance", 0, wire.MaxLimit))
	n, _ := strconv.Atoi(r.PostFormValue("rows"))
	for i := 0; i < n && i < 500; i++ {
		item := strings.TrimSpace(r.PostFormValue(fmt.Sprintf("item%d", i)))
		if item == "" || item == "0" {
			continue // empty line = removed
		}
		row := store.DropItem{
			Kills:  f.int(fmt.Sprintf("kills%d", i), fmt.Sprintf("Kills (linha %d)", i+1), 1, 2000000000),
			Item:   f.int(fmt.Sprintf("item%d", i), fmt.Sprintf("Item (linha %d)", i+1), 1, 6499),
			Effect: f.int(fmt.Sprintf("effect%d", i), fmt.Sprintf("Efeito (linha %d)", i+1), 0, 255),
			Value:  f.int(fmt.Sprintf("value%d", i), fmt.Sprintf("Valor (linha %d)", i+1), 0, acctedit.MaxEffect),
		}
		d.Items = append(d.Items, row)
	}
	nr, _ := strconv.Atoi(r.PostFormValue("rate_rows"))
	slots := map[int]bool{}
	for i := 0; i < nr && i < 100; i++ {
		if v := strings.TrimSpace(r.PostFormValue(fmt.Sprintf("ritem%d", i))); v == "" || v == "0" {
			continue
		}
		line := fmt.Sprintf("(chance, linha %d)", i+1)
		row := store.RateDrop{
			Slot:   f.int(fmt.Sprintf("rslot%d", i), "Slot "+line, 0, 63),
			Item:   f.int(fmt.Sprintf("ritem%d", i), "Item "+line, 1, 6499),
			Chance: f.int(fmt.Sprintf("rchance%d", i), "Chance "+line, 0, 32000),
		}
		for e := range row.Effects {
			row.Effects[e] = store.ItemEffect{
				Effect: f.int(fmt.Sprintf("ref%d_%d", i, e), "Efeito "+line, 0, 255),
				Value:  f.int(fmt.Sprintf("rval%d_%d", i, e), "Valor "+line, 0, acctedit.MaxEffect),
			}
		}
		if slots[row.Slot] {
			f.errs = append(f.errs, fmt.Sprintf("O slot %d aparece duas vezes.", row.Slot))
		}
		slots[row.Slot] = true
		d.Rate = append(d.Rate, row)
	}
	if strings.TrimSpace(d.Name) == "" {
		f.errs = append(f.errs, "O nome é obrigatório.")
	}
	if e := f.err(); e != "" {
		s.renderDrop(w, r, http.StatusUnprocessableEntity, oldName, d, "", e)
		return
	}
	if err := s.st.SaveDrop(r.Context(), oldName, d, sessionOf(r).user); err != nil {
		s.renderDrop(w, r, http.StatusUnprocessableEntity, oldName, d, "", "O banco recusou: "+err.Error())
		return
	}
	http.Redirect(w, r, dropURL(d.Name, "saved=1"), http.StatusSeeOther)
}

func (s *Server) deleteDrop(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteDrop(r.Context(), r.PostFormValue("name"), sessionOf(r).user); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/gamedata/drops", http.StatusSeeOther)
}

// ---- teleports ----

func (s *Server) teleportsPage(w http.ResponseWriter, r *http.Request) {
	tps, err := s.st.ListTeleports(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	edit := store.Teleport{ID: len(tps)}
	if v, err := strconv.Atoi(r.URL.Query().Get("id")); err == nil {
		edit.ID = v
		for _, t := range tps {
			if t.ID == v {
				edit = t
			}
		}
	}
	changes, err := s.st.RecentChanges(r.Context(), "teleport", "", 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	msg := ""
	if r.URL.Query().Has("saved") {
		msg = reloadNote
	}
	s.page(w, r, http.StatusOK, "teleports", map[string]any{"Title": "Teleportes", "Teleports": tps, "Edit": edit,
		"Msg": msg, "Error": r.URL.Query().Get("error"), "Changes": changes})
}

func (s *Server) saveTeleport(w http.ResponseWriter, r *http.Request) {
	f := &formReader{r: r}
	t := &store.Teleport{
		ID:   f.int("id", "Id", 0, 127),
		SrcX: f.int("src_x", "Origem X", 0, 4095), SrcY: f.int("src_y", "Origem Y", 0, 4095),
		DstX: f.int("dst_x", "Destino X", 0, 4095), DstY: f.int("dst_y", "Destino Y", 0, 4095),
		Price: f.int("price", "Preço", 0, 2000000000),
	}
	if e := f.err(); e != "" {
		http.Redirect(w, r, "/gamedata/teleports?error="+url.QueryEscape(e), http.StatusSeeOther)
		return
	}
	if err := s.st.SaveTeleport(r.Context(), t, sessionOf(r).user); err != nil {
		http.Redirect(w, r, "/gamedata/teleports?error="+url.QueryEscape("O banco recusou: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/gamedata/teleports?id=%d&saved=1", t.ID), http.StatusSeeOther)
}

func (s *Server) deleteTeleport(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PostFormValue("id"))
	if err := s.st.DeleteTeleport(r.Context(), id, sessionOf(r).user); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/gamedata/teleports", http.StatusSeeOther)
}

type rateDropRow struct {
	store.RateDrop
	ItemName string
}

func (s *Server) dropRatesPage(w http.ResponseWriter, r *http.Request) {
	msg := ""
	if r.URL.Query().Has("saved") {
		msg = "Salvo. Vale nos canais em até alguns segundos."
	}
	s.renderDropRates(w, r, http.StatusOK, msg, "")
}

func (s *Server) renderDropRates(w http.ResponseWriter, r *http.Request, code int, msg, errMsg string) {
	slots, err := s.st.DropSlots(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	blocked, err := s.st.BlockedDropItems(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	names, err := s.st.ItemNames(r.Context(), blocked)
	if err != nil {
		s.fail(w, err)
		return
	}
	text := make([]string, len(blocked))
	type blockedRow struct {
		ID   int
		Name string
	}
	rows := make([]blockedRow, len(blocked))
	for i, id := range blocked {
		text[i] = strconv.Itoa(id)
		rows[i] = blockedRow{id, names[id]}
	}
	s.page(w, r, code, "droprates", map[string]any{"Title": "Taxas de drop", "Slots": slots, "BlockedText": strings.Join(text, " "),
		"Blocked": rows, "Msg": msg, "Error": errMsg})
}

func (s *Server) saveDropRates(w http.ResponseWriter, r *http.Request) {
	f := &formReader{r: r}
	var slots []store.DropSlot
	for i := range 64 {
		slots = append(slots, store.DropSlot{Slot: i,
			Base: f.int(fmt.Sprintf("base%d", i), fmt.Sprintf("Base (slot %d)", i), 0, 2000000000),
			Rate: f.int(fmt.Sprintf("rate%d", i), fmt.Sprintf("Taxa (slot %d)", i), 0, 10000)})
	}
	var blocked []int
	for _, v := range strings.FieldsFunc(r.PostFormValue("blocked"), func(c rune) bool { return c == ',' || c == ' ' || c == '\n' || c == '\r' || c == '\t' }) {
		id, err := strconv.Atoi(v)
		if err != nil || id < 1 || id > 6499 {
			f.errs = append(f.errs, "Item bloqueado inválido: "+v+".")
			continue
		}
		blocked = append(blocked, id)
	}
	if e := f.err(); e != "" {
		s.renderDropRates(w, r, http.StatusUnprocessableEntity, "", e)
		return
	}
	if err := s.st.SaveDropRates(r.Context(), slots, blocked, sessionOf(r).user); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/gamedata/droprates?saved=1", http.StatusSeeOther)
}

// SeedRateDrops copies the monsters' inventories to the chance list the
// first time the panel runs after the migration.
func SeedRateDrops(ctx context.Context, st *store.Store) (int, error) {
	done, err := st.RateDropsImported(ctx)
	if err != nil || done {
		return 0, err
	}
	world, err := st.LoadWorldAssets(ctx)
	if err != nil || len(world.TemplatesOf(store.TemplateNPC)) == 0 {
		return 0, err // a new server: the templates come with the first import
	}
	gens, err := st.Generators(ctx)
	if err != nil || len(gens) == 0 {
		return 0, err
	}
	drops, err := game.ReadRateDrops(gens, world)
	if err != nil {
		return 0, err
	}
	return st.SeedRateDrops(ctx, drops, "npc (import)")
}
