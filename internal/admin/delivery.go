package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"dbsrv/internal/acctedit"
	"dbsrv/internal/store"
)

// Item delivery: queues "item" deliveries, which DBSRV puts into the
// account storage (cargo) — straight into the database when the player is
// offline, through TMSRV when in game, and waiting while the player is at
// character selection (TMSRV would overwrite the storage on its next save).

const (
	efSanc        = 43
	efAmount      = 61
	maxStack      = 120 // the client's stack limit
	maxItemCopies = 50
)

// sancValue is the EF_SANC value of a refine level (SetItemSanc): +0..+9
// as is, +10.. as 230 + 4 per level.
func sancValue(sanc int) int {
	if sanc > 9 {
		return 230 + (sanc-10)*4
	}
	return sanc
}

// itemRequest is a validated "entregar item" form.
type itemRequest struct {
	ItemID  int
	Effects [][2]int
	Copies  int
}

// readItemForm checks the form; the error is shown to the admin.
func readItemForm(r *http.Request) (itemRequest, string) {
	f := &formReader{r: r}
	req := itemRequest{
		ItemID: f.int("item_id", "O item", 1, 6499),
		Copies: f.int("copies", "Cópias", 1, maxItemCopies),
	}
	amount := f.int("amount", "A quantidade", 0, maxStack)
	sanc := f.int("sanc", "O refino", 0, 15)
	var extra [][2]int
	for i := range 3 {
		ef := f.int(fmt.Sprintf("ef%d", i), fmt.Sprintf("O efeito %d", i+1), 0, 255)
		ev := f.int(fmt.Sprintf("ev%d", i), fmt.Sprintf("O valor %d", i+1), 0, acctedit.MaxEffect)
		if ef != 0 {
			extra = append(extra, [2]int{ef, ev})
		}
	}
	if msg := f.err(); msg != "" {
		return req, msg
	}
	if amount > 1 {
		req.Effects = append(req.Effects, [2]int{efAmount, amount})
	}
	if sanc > 0 {
		req.Effects = append(req.Effects, [2]int{efSanc, sancValue(sanc)})
	}
	for _, e := range extra {
		if (e[0] == efAmount && amount > 1) || (e[0] == efSanc && sanc > 0) {
			return req, "Quantidade e refino já foram preenchidos nos campos próprios; não repita nos adicionais."
		}
	}
	req.Effects = append(req.Effects, extra...)
	if len(req.Effects) > 3 {
		return req, "Um item tem no máximo 3 efeitos (quantidade, refino e adicionais somados)."
	}
	return req, ""
}

func (s *Server) deliverItem(w http.ResponseWriter, r *http.Request) {
	user := strings.ToUpper(r.PathValue("user"))
	req, msg := readItemForm(r)
	if msg != "" {
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Item", msg)
		return
	}
	if _, err := s.st.GetItem(r.Context(), req.ItemID); errors.Is(err, store.ErrNotFound) {
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Item", fmt.Sprintf("O item %d não existe na lista de itens.", req.ItemID))
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	exists, err := s.st.AccountExists(r.Context(), user)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		s.renderAccount(w, r, http.StatusServiceUnavailable, "Item", msg)
		return
	}
	sess := sessionOf(r)
	payload := map[string]any{"item_id": req.ItemID, "effects": req.Effects}
	var last int64
	for range req.Copies {
		if last, err = s.st.Enqueue(r.Context(), "item", user, payload, sess.user); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.log.Info("admin request", "admin", sess.user, "kind", "item", "account", user, "item", req.ItemID,
		"effects", req.Effects, "copies", req.Copies, "delivery", last)
	http.Redirect(w, r, "/accounts/"+url.PathEscape(user)+"?req="+strconv.FormatInt(last, 10), http.StatusSeeOther)
}

// itemLabel describes an item delivery for the request lists.
func itemLabel(d store.DeliveryInfo) string {
	if d.Kind != "item" || d.ItemID == 0 {
		return ""
	}
	name := d.ItemName
	if name == "" {
		name = "item"
	}
	out := fmt.Sprintf("%s [%d]", name, d.ItemID)
	for _, e := range d.Effects {
		switch {
		case e[0] == efAmount && e[1] > 1:
			out += fmt.Sprintf(" ×%d", e[1])
		case e[0] == efSanc:
			out += fmt.Sprintf(" +%d", sancLevel(e[1]))
		case e[0] != 0:
			out += fmt.Sprintf(" (%d:%d)", e[0], e[1])
		}
	}
	return out
}

// sancLevel is the refine level of an EF_SANC value (failures dropped).
func sancLevel(v int) int {
	if v >= 230 {
		return min(10+(v-230)/4, 15)
	}
	return v % 10
}
