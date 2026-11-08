package admin

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"dbsrv/internal/store"
)

// Ban and cash: the "ban" and "cash" deliveries DBSRV already applied (the
// C++ took them from the site), now with forms on the account page.

// Ban types, as the login checks them: 2 answers "account disabled", any
// other "account blocked". 0 lifts the ban.
const (
	banBlocked  = 1
	banDisabled = 2
	// maxCash is the largest cash delivery DBSRV accepts (int32).
	maxCash = 1<<31 - 1
)

// permanentBan is the end of a ban without end: the login only honours a
// ban with a date.
var permanentBan = time.Date(2099, 12, 31, 23, 59, 0, 0, time.Local)

func (s *Server) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /accounts/{user}/ban", s.auth(s.banAccount))
	mux.HandleFunc("POST /accounts/{user}/unban", s.auth(s.unbanAccount))
	mux.HandleFunc("POST /accounts/{user}/cash", s.auth(s.deliverCash))
}

// readBanForm returns the ban type and its end; the message is shown to
// the admin.
func readBanForm(r *http.Request, now time.Time) (int, time.Time, string) {
	f := &formReader{r: r}
	typ := f.int("type", "O tipo", banBlocked, banDisabled)
	days := f.int("days", "Os dias", 0, 3650)
	if msg := f.err(); msg != "" {
		return 0, time.Time{}, msg
	}
	if r.PostFormValue("permanent") != "" {
		return typ, permanentBan, ""
	}
	if days == 0 {
		return 0, time.Time{}, "Informe os dias do banimento ou marque permanente."
	}
	return typ, now.AddDate(0, 0, days), ""
}

func (s *Server) banAccount(w http.ResponseWriter, r *http.Request) {
	typ, until, msg := readBanForm(r, time.Now())
	if msg != "" {
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Ban", msg)
		return
	}
	if !s.accountRequestOK(w, r, "Ban") {
		return
	}
	s.enqueue(w, r, "ban", r.PathValue("user"), map[string]any{"type": typ, "until": until})
}

func (s *Server) unbanAccount(w http.ResponseWriter, r *http.Request) {
	if !s.accountRequestOK(w, r, "Ban") {
		return
	}
	s.enqueue(w, r, "ban", r.PathValue("user"), map[string]any{"type": 0})
}

func (s *Server) deliverCash(w http.ResponseWriter, r *http.Request) {
	f := &formReader{r: r}
	amount := f.int("amount", "O cash", 1, maxCash)
	if msg := f.err(); msg != "" {
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Cash", msg)
		return
	}
	if !s.accountRequestOK(w, r, "Cash") {
		return
	}
	s.enqueue(w, r, "cash", r.PathValue("user"), map[string]any{"amount": amount})
}

// accountRequestOK checks that the account exists and DBSRV is up; when not
// it answers and returns false.
func (s *Server) accountRequestOK(w http.ResponseWriter, r *http.Request, which string) bool {
	exists, err := s.st.AccountExists(r.Context(), strings.ToUpper(r.PathValue("user")))
	if err != nil {
		s.fail(w, err)
		return false
	}
	if !exists {
		http.NotFound(w, r)
		return false
	}
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		s.renderAccount(w, r, http.StatusServiceUnavailable, which, msg)
		return false
	}
	return true
}

// banLabel and cashLabel describe those deliveries in the request lists.
func banLabel(d store.DeliveryInfo) string {
	if d.Kind != "ban" {
		return ""
	}
	switch {
	case d.BanType == 0:
		return "retirar banimento"
	case d.BanUntil == nil:
		return banTypeLabel(d.BanType)
	case !d.BanUntil.Before(permanentBan):
		return banTypeLabel(d.BanType) + " permanente"
	}
	return banTypeLabel(d.BanType) + " até " + d.BanUntil.Local().Format("02/01/2006 15:04")
}

func banTypeLabel(t int) string {
	if t == banDisabled {
		return "desativada"
	}
	return "bloqueada"
}

func cashLabel(d store.DeliveryInfo) string {
	if d.Kind != "cash" || d.Amount == 0 {
		return ""
	}
	return fmt.Sprintf("%d", d.Amount)
}
