package admin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/bits"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"dbsrv/internal/acctedit"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Account editing: a character's gold, level, attributes, points, exp,
// skills, equipment and inventory, the storage (cargo) items and gold, and the GM access level. Every change is
// an "edit" delivery (package acctedit): DBSRV writes it to the database
// when the player is offline, or has TMSRV apply it live when the player is
// in game with that character.

var equipNames = [acctedit.EquipSize]string{"Face", "Elmo", "Peitoral", "Calça", "Luva", "Bota",
	"Arma (esq.)", "Arma (dir.)", "Anel", "Colar", "Orbe", "Pedra", "Medalha", "Fada", "Montaria", "Capa"}

// itemCell is one slot as shown in the editor.
type itemCell struct {
	Where   int
	Slot    int
	Label   string
	Item    wire.Item
	Name    string
	Effects []string
	Expect  string // the item as "id,ef,efv,..." for the edit's Expect
	Picked  bool   // open in the editor
	EquipTo []equipOption
}

// equipOption is an equipment slot an inventory item can go to.
type equipOption struct {
	Value string // "slot|expected item in it"
	Label string
}

func (c itemCell) Empty() bool { return c.Item.Index == 0 }

// editRoutes adds the editor pages and the edit action.
func (s *Server) editRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /accounts/{user}/chars/{slot}", s.auth(s.characterPage))
	mux.HandleFunc("GET /accounts/{user}/storage", s.auth(s.storagePage))
	mux.HandleFunc("POST /accounts/{user}/edit", s.auth(s.submitEdit))
}

func expectString(it wire.Item) string {
	parts := make([]string, 0, 7)
	for _, v := range acctedit.Ints(it) {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ",")
}

func parseExpect(raw string) ([]int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	f := strings.Split(raw, ",")
	if len(f) != 7 {
		return nil, false
	}
	out := make([]int, 7)
	for i, s := range f {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// parseSlotRef reads a selection: "where:slot:id,ef,efv,...".
func parseSlotRef(raw string) (acctedit.SlotRef, bool) {
	f := strings.SplitN(raw, ":", 3)
	if len(f) != 3 {
		return acctedit.SlotRef{}, false
	}
	where, err1 := strconv.Atoi(f[0])
	slot, err2 := strconv.Atoi(f[1])
	exp, ok := parseExpect(f[2])
	if err1 != nil || err2 != nil || !ok || exp == nil {
		return acctedit.SlotRef{}, false
	}
	return acctedit.SlotRef{Where: where, Slot: slot, Expect: exp}, true
}

// cells builds the editor rows for a container.
func (s *Server) cells(r *http.Request, where int, items []wire.Item, labels []string, effectNames map[int]string) ([]itemCell, error) {
	var ids []int
	for _, it := range items {
		if it.Index != 0 {
			ids = append(ids, int(it.Index))
		}
	}
	names, err := s.st.ItemNames(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	pickWhere, _ := strconv.Atoi(r.URL.Query().Get("where"))
	pickSlot, pickErr := strconv.Atoi(r.URL.Query().Get("slot"))
	out := make([]itemCell, len(items))
	for i, it := range items {
		c := itemCell{Where: where, Slot: i, Item: it, Name: names[int(it.Index)], Expect: expectString(it),
			Picked: pickErr == nil && pickWhere == where && pickSlot == i}
		if i < len(labels) {
			c.Label = labels[i]
		} else {
			c.Label = strconv.Itoa(i)
		}
		for _, e := range it.Effect {
			if e.Index == 0 {
				continue
			}
			n := effectNames[int(e.Index)]
			if n == "" {
				n = strconv.Itoa(int(e.Index))
			}
			c.Effects = append(c.Effects, fmt.Sprintf("%s %d", n, e.Value))
		}
		out[i] = c
	}
	return out, nil
}

// editorData loads what both editor pages need.
func (s *Server) editorData(w http.ResponseWriter, r *http.Request) (string, *store.AccountRecord, map[string]any, map[int]string, bool) {
	user := strings.ToUpper(r.PathValue("user"))
	rec, err := s.st.LoadAccount(r.Context(), user)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return "", nil, nil, nil, false
	}
	if err != nil {
		s.fail(w, err)
		return "", nil, nil, nil, false
	}
	online, err := s.st.Online(r.Context(), user)
	if err != nil {
		s.fail(w, err)
		return "", nil, nil, nil, false
	}
	list, err := s.effectNames(r.Context())
	if err != nil {
		s.fail(w, err)
		return "", nil, nil, nil, false
	}
	names := map[int]string{}
	for _, e := range list {
		names[e.ID] = e.Name
	}
	deliveries, err := s.st.RecentDeliveries(r.Context(), user, 15)
	if err != nil {
		s.fail(w, err)
		return "", nil, nil, nil, false
	}
	data := map[string]any{"Username": user, "Online": online, "EffectNames": list, "Deliveries": deliveries,
		"Error": r.URL.Query().Get("err"), "Requested": r.URL.Query().Get("req")}
	return user, rec, data, names, true
}

// picked returns the cell open in the editor (or an empty one).
func picked(groups ...[]itemCell) *itemCell {
	for _, g := range groups {
		for i := range g {
			if g[i].Picked {
				return &g[i]
			}
		}
	}
	return nil
}

func (s *Server) characterPage(w http.ResponseWriter, r *http.Request) {
	slot, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil || slot < 0 || slot > 3 {
		http.NotFound(w, r)
		return
	}
	user, rec, data, names, ok := s.editorData(w, r)
	if !ok {
		return
	}
	c := &rec.Account.Mob[slot]
	if c.Player.Name[0] == 0 {
		http.NotFound(w, r)
		return
	}
	pl := &c.Player
	equip, err := s.cells(r, wire.SlotEquip, pl.Equip[:acctedit.EquipSize], equipNames[:], names)
	if err != nil {
		s.fail(w, err)
		return
	}
	inv, err := s.cells(r, wire.SlotInv, pl.Inventory[:acctedit.InvSize], nil, names)
	if err != nil {
		s.fail(w, err)
		return
	}
	for i := range inv {
		switch {
		case i >= 30 && i < 45:
			inv[i].Label = fmt.Sprintf("%d (bolsa 1)", i)
		case i >= 45 && i < 60:
			inv[i].Label = fmt.Sprintf("%d (bolsa 2)", i)
		case i == 60 || i == 61:
			inv[i].Label = fmt.Sprintf("%d (bolsa)", i)
		}
	}
	name := wire.DecodeGame(wire.CString(pl.Name[:]))
	data["Title"] = name + " · " + user
	data["Slot"] = slot
	data["Name"] = name
	data["Level"] = int(pl.BStatus.Level) + 1
	data["Evolution"] = int16(pl.Equip[0].Effect[1].Value)
	data["Gold"] = pl.Gold
	data["Exp"] = pl.Exp
	data["Class"] = int(pl.ClassInfo)
	data["LearnOrig"] = pl.Learn[0] & acctedit.ClassSkills
	stats := make([]statRow, len(acctedit.StatFields))
	for i, f := range acctedit.StatFields {
		stats[i] = statRow{Key: f.Key, Label: f.Label, Max: f.Max, Value: f.Get(pl)}
	}
	data["Attributes"], data["Masteries"], data["Points"], data["ExpStat"] = stats[:4], stats[4:8], stats[8:11], stats[11]
	skills, err := s.skillRows(r.Context(), pl)
	if err != nil {
		s.fail(w, err)
		return
	}
	data["Skills"] = skills
	if err := s.addEquipOptions(r, inv, equip); err != nil {
		s.fail(w, err)
		return
	}
	data["Equip"] = equip[1:] // the face (0) is not editable
	data["Inv"] = inv
	data["Pick"] = picked(equip, inv)
	data["Back"] = fmt.Sprintf("/accounts/%s/chars/%d", url.PathEscape(user), slot)
	s.page(w, r, http.StatusOK, "character", data)
}

// statRow is one number of the character's attribute form.
type statRow struct {
	Key, Label string
	Max, Value int64
}

// skillRow is one class skill of the character's attribute form.
type skillRow struct {
	Bit     int
	Name    string
	Learned bool
}

// skillRows lists the 24 skills of the character's class. A skill's name is
// the name of its book in the item list (5000 + class*24 + position).
func (s *Server) skillRows(ctx context.Context, pl *wire.Mob) ([]skillRow, error) {
	if pl.ClassInfo >= 4 {
		return nil, nil
	}
	first := 5000 + int(pl.ClassInfo)*24
	ids := make([]int, 24)
	for i := range ids {
		ids[i] = first + i
	}
	names, err := s.st.ItemNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	rows := make([]skillRow, 24)
	for i := range rows {
		rows[i] = skillRow{Bit: i, Name: cmp.Or(names[first+i], fmt.Sprintf("Skill %d", i+1)), Learned: pl.Learn[0]&(1<<i) != 0}
	}
	return rows, nil
}

// readStats fills an OpStats with what differs from the values the form was
// drawn with, so the rest keeps its live value when the player is in game.
func (s *Server) readStats(r *http.Request, f *formReader, e *acctedit.Edit) error {
	e.Stats = map[string]int64{}
	for _, st := range acctedit.StatFields {
		if r.PostFormValue(st.Key) != r.PostFormValue("orig_"+st.Key) {
			e.Stats[st.Key] = int64(f.int(st.Key, st.Label, 0, int(st.Max)))
		}
	}
	orig := uint32(f.int("learn_orig", "As skills originais", 0, acctedit.ClassSkills))
	var now uint32
	for _, raw := range r.PostForm["learn"] {
		bit, err := strconv.Atoi(raw)
		if err != nil || bit < 0 || bit > 23 {
			f.errs = append(f.errs, "Skill inválida; recarregue a página.")
			return nil
		}
		now |= 1 << bit
	}
	e.LearnAdd, e.LearnDel = now&^orig, orig&^now
	if e.LearnAdd == 0 && e.LearnDel == 0 {
		return nil
	}
	class := f.int("class", "A classe", 0, 3)
	list, err := s.st.ListSkills(r.Context())
	if err != nil {
		return err
	}
	points := make(map[int]int, len(list))
	for _, k := range list {
		points[k.ID] = k.Points
	}
	for bit := range 24 {
		switch id := class*24 + bit; {
		case e.LearnAdd&(1<<bit) != 0:
			e.LearnCost += points[id]
		case e.LearnDel&(1<<bit) != 0:
			e.LearnCost -= points[id]
		}
	}
	return nil
}

// addEquipOptions offers each inventory item the equipment slots its
// position allows. An occupied slot is a swap.
func (s *Server) addEquipOptions(r *http.Request, inv, equip []itemCell) error {
	var ids []int
	for _, c := range inv {
		if !c.Empty() {
			ids = append(ids, int(c.Item.Index))
		}
	}
	pos, err := s.st.ItemPositions(r.Context(), ids)
	if err != nil {
		return err
	}
	for i := range inv {
		if inv[i].Empty() {
			continue
		}
		for _, to := range equip[1:acctedit.MoveEquipEnd] {
			if pos[int(inv[i].Item.Index)]>>to.Slot&1 == 0 {
				continue
			}
			label := to.Label
			if !to.Empty() {
				label += " (troca com " + cmp.Or(to.Name, "item") + ")"
			}
			inv[i].EquipTo = append(inv[i].EquipTo, equipOption{Value: fmt.Sprintf("%d|%s", to.Slot, to.Expect), Label: label})
		}
	}
	return nil
}

func (s *Server) storagePage(w http.ResponseWriter, r *http.Request) {
	user, rec, data, names, ok := s.editorData(w, r)
	if !ok {
		return
	}
	cells, err := s.cells(r, wire.SlotStorage, rec.Account.StorageItems[:acctedit.StorageSize], nil, names)
	if err != nil {
		s.fail(w, err)
		return
	}
	data["Title"] = "Baú · " + user
	data["Coin"] = rec.Account.StorageCoin
	data["Storage"] = cells
	data["Pick"] = picked(cells)
	data["Back"] = "/accounts/" + url.PathEscape(user) + "/storage"
	s.page(w, r, http.StatusOK, "storage", data)
}

// submitEdit queues one edit and goes back to the page it came from.
func (s *Server) submitEdit(w http.ResponseWriter, r *http.Request) {
	user := strings.ToUpper(r.PathValue("user"))
	base := "/accounts/" + url.PathEscape(user)
	back := r.PostFormValue("back")
	if (back != base && !strings.HasPrefix(back, base+"/")) || strings.ContainsAny(back, "?#\\") {
		back = base
	}
	fail := func(msg string) {
		http.Redirect(w, r, back+"?err="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	f := &formReader{r: r}
	e := acctedit.Edit{Op: r.PostFormValue("op")}
	switch e.Op {
	case acctedit.OpGold:
		e.Char = f.int("char", "O personagem", 0, 3)
		e.Value = int64(f.int("value", "O gold", 0, acctedit.MaxGold))
	case acctedit.OpCoin:
		e.Value = int64(f.int("value", "O gold do baú", 0, acctedit.MaxGold))
	case acctedit.OpLevel:
		e.Char = f.int("char", "O personagem", 0, 3)
		e.Value = int64(f.int("value", "O nível", 1, acctedit.MaxLevel))
	case acctedit.OpAccess:
		e.Value = int64(f.int("value", "O nível de acesso", 0, acctedit.MaxAccess))
	case acctedit.OpItem:
		e.Char = f.int("char", "O personagem", 0, 3)
		e.Where = f.int("where", "O local", 0, 2)
		e.Slot = f.int("slot", "O slot", 0, acctedit.StorageSize-1)
		id := f.int("item_id", "O item", 0, 6499)
		e.Item = []int{id, 0, 0, 0, 0, 0, 0}
		if id != 0 {
			for k := range 3 {
				e.Item[1+2*k] = f.int(fmt.Sprintf("ef%d", k), fmt.Sprintf("O efeito %d", k+1), 0, 255)
				e.Item[2+2*k] = f.int(fmt.Sprintf("ev%d", k), fmt.Sprintf("O valor %d", k+1), 0, acctedit.MaxEffect)
			}
		}
		exp, ok := parseExpect(r.PostFormValue("expect"))
		if !ok {
			fail("Item esperado inválido; recarregue a página.")
			return
		}
		e.Expect = exp
		if id != 0 {
			if _, err := s.st.GetItem(r.Context(), id); errors.Is(err, store.ErrNotFound) {
				fail(fmt.Sprintf("O item %d não existe na lista de itens.", id))
				return
			} else if err != nil {
				s.fail(w, err)
				return
			}
		}
	case acctedit.OpMove:
		e.Char = f.int("char", "O personagem", 0, 3)
		e.Slot = f.int("slot", "O slot", 0, acctedit.InvSize-1)
		to, expectTo, ok := strings.Cut(r.PostFormValue("to"), "|")
		e.To, _ = strconv.Atoi(to)
		var ok1, ok2 bool
		e.Expect, ok1 = parseExpect(r.PostFormValue("expect"))
		e.ExpectTo, ok2 = parseExpect(expectTo)
		if !ok || !ok1 || !ok2 || e.Expect == nil || e.ExpectTo == nil {
			fail("Item esperado inválido; recarregue a página.")
			return
		}
		if msg := f.err(); msg != "" {
			fail(msg)
			return
		}
		it, err := s.st.GetItem(r.Context(), e.Expect[0])
		if errors.Is(err, store.ErrNotFound) || (err == nil && it.Pos>>e.To&1 == 0) {
			fail("Este item não pode ser equipado nesse slot.")
			return
		} else if err != nil {
			s.fail(w, err)
			return
		}
	case acctedit.OpStats:
		e.Char = f.int("char", "O personagem", 0, 3)
		if err := s.readStats(r, f, &e); err != nil {
			s.fail(w, err)
			return
		}
		if f.err() == "" && len(e.Stats) == 0 && e.LearnAdd == 0 && e.LearnDel == 0 {
			fail("Nenhum valor foi alterado.")
			return
		}
	case acctedit.OpRemove:
		e.Char = f.int("char", "O personagem", 0, 3)
		sel := r.PostForm["sel"]
		if len(sel) == 0 {
			fail("Selecione ao menos um item para remover.")
			return
		}
		for _, v := range sel {
			ref, ok := parseSlotRef(v)
			if !ok {
				fail("Seleção inválida; recarregue a página.")
				return
			}
			e.Removes = append(e.Removes, ref)
		}
	default:
		fail("Operação desconhecida.")
		return
	}
	if msg := f.err(); msg != "" {
		fail(msg)
		return
	}
	if msg := e.Validate(); msg != "" {
		fail("Pedido inválido: " + msg)
		return
	}
	if exists, err := s.st.AccountExists(r.Context(), user); err != nil {
		s.fail(w, err)
		return
	} else if !exists {
		http.NotFound(w, r)
		return
	}
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		fail(msg)
		return
	}
	sess := sessionOf(r)
	id, err := s.st.Enqueue(r.Context(), "edit", user, e, sess.user)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("admin edit", "admin", sess.user, "account", user, "op", e.Op, "char", e.Char,
		"where", e.Where, "slot", e.Slot, "value", e.Value, "item", e.Item, "stats", e.Stats,
		"learn_add", e.LearnAdd, "learn_del", e.LearnDel, "delivery", id)
	http.Redirect(w, r, back+"?req="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

var whereNames = map[int]string{wire.SlotEquip: "equipamento", wire.SlotInv: "inventário", wire.SlotStorage: "baú"}

// editLabel describes an edit delivery in the request lists.
func editLabel(d store.DeliveryInfo) string {
	e := d.Edit
	if d.Kind != "edit" || e == nil {
		return ""
	}
	char := fmt.Sprintf("personagem %d", e.Char+1)
	switch e.Op {
	case acctedit.OpGold:
		return fmt.Sprintf("gold de %s = %d", char, e.Value)
	case acctedit.OpCoin:
		return fmt.Sprintf("gold do baú = %d", e.Value)
	case acctedit.OpLevel:
		return fmt.Sprintf("nível de %s = %d", char, e.Value)
	case acctedit.OpAccess:
		return fmt.Sprintf("acesso = %d", e.Value)
	case acctedit.OpLock:
		return "bloqueio da conta (avisar o jogo)"
	case acctedit.OpStats:
		var parts []string
		for _, f := range acctedit.StatFields {
			if v, ok := e.Stats[f.Key]; ok {
				parts = append(parts, fmt.Sprintf("%s = %d", f.Label, v))
			}
		}
		if n := bits.OnesCount32(e.LearnAdd); n > 0 {
			parts = append(parts, fmt.Sprintf("%d skills adicionadas", n))
		}
		if n := bits.OnesCount32(e.LearnDel); n > 0 {
			parts = append(parts, fmt.Sprintf("%d skills removidas", n))
		}
		return char + ": " + strings.Join(parts, ", ")
	case acctedit.OpMove:
		return fmt.Sprintf("%s: item [%d] do inventário slot %d para %s", char, e.Expect[0], e.Slot, equipNames[e.To])
	case acctedit.OpRemove:
		if e.CharScoped() {
			return fmt.Sprintf("remover %d itens de %s", len(e.Removes), char)
		}
		return fmt.Sprintf("remover %d itens do baú", len(e.Removes))
	case acctedit.OpItem:
		place := fmt.Sprintf("%s slot %d", whereNames[e.Where], e.Slot)
		if e.Where != wire.SlotStorage {
			place = char + ", " + place
		}
		if len(e.Item) == 0 || e.Item[0] == 0 {
			return "remover item de " + place
		}
		return fmt.Sprintf("item [%d] em %s", e.Item[0], place)
	}
	return e.Op
}
