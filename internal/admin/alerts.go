package admin

import (
	"net/http"
	"strconv"

	"dbsrv/internal/store"
)

// Alerts: what the TMSRVs reported for the staff (wire.SecurityAlert,
// stored by DBSRV in the alerts table). Today: WPE seen by the client
// protection (the player is disconnected).

func (s *Server) alertRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /alerts", s.auth(s.alertsPage))
	mux.HandleFunc("POST /alerts/{id}/seen", s.auth(s.markAlertSeen))
}

func alertKindLabel(k string) string {
	switch k {
	case store.AlertKindWPE:
		return "WPE"
	}
	return k
}

func (s *Server) alertsPage(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"
	list, err := s.st.Alerts(r.Context(), !all, 300)
	if err != nil {
		s.fail(w, err)
		return
	}
	unseen, err := s.st.UnseenAlerts(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, http.StatusOK, "alerts", map[string]any{"Title": "Alertas", "Alerts": list, "Unseen": unseen, "All": all})
}

// markAlertSeen is POST /alerts/{id}/seen (id 0 = all).
func (s *Server) markAlertSeen(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 0 {
		http.NotFound(w, r)
		return
	}
	if err := s.st.MarkAlertsSeen(r.Context(), id, sessionOf(r).user); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/alerts", http.StatusSeeOther)
}
