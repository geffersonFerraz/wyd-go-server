package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/store"
)

// The item tracking screens ("Rastreio"): the tracked items that went
// through an account, the accounts an item went through, and the whole
// history of one tracked item. The data is what the channels report to
// DBSRV (store/itemtracks.go); nothing is changed here.

const trackSearchLimit = 300

func (s *Server) trackRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /tracks", s.auth(s.tracksPage))
	mux.HandleFunc("GET /tracks/{id}", s.auth(s.trackPage))
}

// trackStep is one account in the path of a tracked item.
type trackStep struct{ Account, Char string }

// trackRow is a tracked item as the lists show it.
type trackRow struct {
	store.ItemTrack
	Label, Was string // what it is now, and what it was created as when that changed
	Origin     string
	Status     string
	Tone       string // pill class of the status
	Chain      []trackStep
}

// trackSide is how a tracked item came into an account or left it.
type trackSide struct {
	At    time.Time
	Text  string
	Other trackStep // the other account, when there is one
}

// trackPassage is one stay of a tracked item in the searched account.
type trackPassage struct {
	Track   *trackRow
	In, Out *trackSide
	Holding bool // still there
}

// trackEventRow is one line of a track's history.
type trackEventRow struct {
	store.ItemTrackEvent
	Label, ItemLabel string
	Before           string // what the item was before, when this event changed it
}

var trackKindLabels = map[string]string{
	store.TrackPickup:      "Pegou o drop",
	store.TrackNPCBuy:      "Comprou do NPC",
	store.TrackNPCReward:   "Recebeu do NPC",
	store.TrackEventReward: "Prêmio de evento",
	store.TrackTrade:       "Troca",
	store.TrackStore:       "Venda em loja de jogador",
	store.TrackGround:      "Largado no chão e pego",
	store.TrackNPCSell:     "Vendido ao NPC",
	store.TrackBuyback:     "Recomprado do NPC",
	store.TrackDestroyed:   "Largado no chão e sumiu",
	store.TrackGone:        "Saiu da conta (usado, combinado ou apagado)",
	store.TrackChanged:     "Efeitos mudaram (refino, tempo, etc.)",
}

func trackStatus(t *store.ItemTrack) (label, tone string) {
	switch t.Status {
	case "active":
		if t.HolderAccount == "" {
			return "No jogo", "ok"
		}
		return "Com " + t.HolderAccount, "ok"
	case "sold":
		return "Vendido ao NPC", "wait"
	case "destroyed":
		return "Sumiu do chão", "bad"
	}
	return "Saiu do jogo", "bad"
}

func trackOrigin(t *store.ItemTrack) string {
	switch t.Origin {
	case "npc_buy":
		return fmt.Sprintf("Comprado do NPC %s por %d", t.Mob, t.Price)
	case "npc_reward":
		return "Dado pelo NPC " + t.Mob
	case "event_reward":
		return "Prêmio do evento " + t.Mob
	}
	if t.KillerChar != "" {
		return fmt.Sprintf("Drop de %s (morto por %s)", t.Mob, t.KillerChar)
	}
	return "Drop de " + t.Mob
}

// trackItemLabel is "Nome [id] +9 (Aumento de Dano 12)".
func trackItemLabel(name string, id int, effects [3]store.ItemEffect) string {
	if name == "" {
		name = "item"
	}
	out := fmt.Sprintf("%s [%d]", name, id)
	var extra []string
	for _, e := range effects {
		switch {
		case e.Effect == 0:
		case e.Effect == efSanc:
			out += fmt.Sprintf(" +%d", sancLevel(e.Value))
		case effectLabels[e.Effect] != "":
			extra = append(extra, fmt.Sprintf("%s %d", effectLabels[e.Effect], e.Value))
		default:
			extra = append(extra, fmt.Sprintf("efeito %d: %d", e.Effect, e.Value))
		}
	}
	if len(extra) > 0 {
		out += " (" + strings.Join(extra, ", ") + ")"
	}
	return out
}

// sameAccount reports an event that moved the item inside one account (a
// character dropped it and another of the same account picked it up).
func sameAccount(e *store.ItemTrackEvent) bool {
	return e.FromAccount != "" && e.FromAccount == e.ToAccount
}

// trackChain is every account the item was in, in order.
func trackChain(events []store.ItemTrackEvent) []trackStep {
	var chain []trackStep
	for i := range events {
		e := &events[i]
		if e.ToAccount == "" || (len(chain) > 0 && chain[len(chain)-1].Account == e.ToAccount) {
			continue
		}
		chain = append(chain, trackStep{e.ToAccount, e.ToChar})
	}
	return chain
}

func newTrackRow(t store.ItemTrack) *trackRow {
	r := &trackRow{ItemTrack: t, Origin: trackOrigin(&t), Chain: trackChain(t.Events)}
	r.Label = trackItemLabel(t.CurrentItemName, t.CurrentItem, t.CurrentEffects)
	if was := trackItemLabel(t.ItemName, t.Item, t.Effects); was != r.Label {
		r.Was = was
	}
	r.Status, r.Tone = trackStatus(&t)
	return r
}

// trackIn says how the event brought the item to its ToAccount.
func trackIn(t *trackRow, e *store.ItemTrackEvent) *trackSide {
	side := &trackSide{At: e.At, Other: trackStep{e.FromAccount, e.FromChar}}
	switch e.Kind {
	case store.TrackPickup, store.TrackNPCBuy, store.TrackNPCReward, store.TrackEventReward:
		side.Text = t.Origin
	case store.TrackTrade:
		side.Text = "Troca com"
	case store.TrackStore:
		side.Text = fmt.Sprintf("Comprou por %d na loja de", e.Price)
	case store.TrackGround:
		side.Text = "Pegou do chão, largado por"
	case store.TrackBuyback:
		side.Text = fmt.Sprintf("Recomprou do NPC por %d", e.Price)
	default:
		side.Text = trackKindLabels[e.Kind]
	}
	return side
}

// trackOut says how the event took the item from its FromAccount.
func trackOut(e *store.ItemTrackEvent) *trackSide {
	side := &trackSide{At: e.At, Other: trackStep{e.ToAccount, e.ToChar}}
	switch e.Kind {
	case store.TrackTrade:
		side.Text = "Troca com"
	case store.TrackStore:
		side.Text = fmt.Sprintf("Vendeu na loja por %d para", e.Price)
	case store.TrackGround:
		side.Text = "Largou no chão, pego por"
	case store.TrackNPCSell:
		side.Text = fmt.Sprintf("Vendeu ao NPC por %d", e.Price)
	case store.TrackDestroyed:
		side.Text = "Largou no chão e sumiu"
	default:
		side.Text = trackKindLabels[e.Kind]
	}
	return side
}

// trackPassages are the stays of the item in the account (upper case): one
// for each time it came in, with how it left.
func trackPassages(t *trackRow, account string) []trackPassage {
	var out []trackPassage
	open := false
	for i := range t.Events {
		e := &t.Events[i]
		if sameAccount(e) {
			continue
		}
		switch {
		case e.ToAccount == account:
			out = append(out, trackPassage{Track: t, In: trackIn(t, e)})
			open = true
		case e.FromAccount == account:
			if !open { // the arrival was not recorded
				out = append(out, trackPassage{Track: t})
			}
			out[len(out)-1].Out = trackOut(e)
			open = false
		}
	}
	holds := t.ItemTrack.Status == "active" && t.HolderAccount == account
	if holds && !open { // held without a recorded arrival
		out = append(out, trackPassage{Track: t})
	}
	if holds {
		out[len(out)-1].Holding = true
	}
	return out
}

func (s *Server) tracksPage(w http.ResponseWriter, r *http.Request) {
	account := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("account")))
	item := strings.TrimSpace(r.URL.Query().Get("item"))
	if len(account) > 32 || len(item) > 64 {
		http.Error(w, "busca longa demais", http.StatusBadRequest)
		return
	}
	limit := trackSearchLimit
	if account == "" && item == "" {
		limit = 50 // the latest movements
	}
	tracks, err := s.st.FindItemTracks(r.Context(), store.ItemTrackFilter{Account: account, Item: item, Limit: limit})
	if err != nil {
		s.fail(w, err)
		return
	}
	rows := make([]*trackRow, len(tracks))
	var passages []trackPassage
	holding := 0
	for i, t := range tracks {
		rows[i] = newTrackRow(t)
		if account == "" {
			continue
		}
		ps := trackPassages(rows[i], account)
		passages = append(passages, ps...)
		if len(ps) > 0 && ps[len(ps)-1].Holding {
			holding++
		}
	}
	s.page(w, r, http.StatusOK, "tracks", map[string]any{"Title": "Rastreio de itens", "Account": account, "Item": item,
		"Tracks": rows, "Passages": passages, "Holding": holding, "Searched": account != "" || item != "",
		"Truncated": len(tracks) == limit, "Limit": limit})
}

func (s *Server) trackPage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	tracks, err := s.st.FindItemTracks(r.Context(), store.ItemTrackFilter{ID: id, Limit: 1})
	if err != nil {
		s.fail(w, err)
		return
	}
	if len(tracks) == 0 {
		http.NotFound(w, r)
		return
	}
	row := newTrackRow(tracks[0])
	ids := make([]int, 0, len(row.Events))
	for _, e := range row.Events {
		ids = append(ids, e.Item)
	}
	names, err := s.st.ItemNames(r.Context(), ids)
	if err != nil {
		s.fail(w, err)
		return
	}
	events := make([]trackEventRow, len(row.Events))
	for i, e := range row.Events {
		events[i] = trackEventRow{ItemTrackEvent: e, Label: trackKindLabels[e.Kind], ItemLabel: trackItemLabel(names[e.Item], e.Item, e.Effects)}
		if events[i].Label == "" {
			events[i].Label = e.Kind
		}
		if i > 0 && events[i-1].ItemLabel != events[i].ItemLabel {
			events[i].Before = events[i-1].ItemLabel
		}
	}
	s.page(w, r, http.StatusOK, "track", map[string]any{"Title": "Rastreio " + row.Label, "Track": row, "Events": events})
}
