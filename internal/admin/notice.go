package admin

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"dbsrv/internal/server"
	"golang.org/x/text/encoding/charmap"
)

// Global notice: a "notice" delivery that DBSRV sends to every channel,
// shown to all players in game (the C++ staff used the NP program).

func (s *Server) noticeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /notice", s.auth(s.noticePage))
	mux.HandleFunc("POST /notice", s.auth(s.sendNotice))
}

func (s *Server) noticePage(w http.ResponseWriter, r *http.Request) {
	s.renderNotice(w, r, http.StatusOK, "", "")
}

func (s *Server) renderNotice(w http.ResponseWriter, r *http.Request, code int, msg, text string) {
	list, err := s.st.RecentDeliveries(r.Context(), server.NoticeUser, 30)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, code, "notice", map[string]any{"Title": "Aviso global", "Notices": list, "Error": msg,
		"Text": text, "Max": server.MaxNoticeLen, "Sent": r.URL.Query().Get("req")})
}

// noticeProblem checks the text: it must fit the game packet (one byte
// per character in Windows-1252).
func noticeProblem(text string) string {
	switch {
	case text == "":
		return "Escreva o aviso."
	case utf8.RuneCountInString(text) > server.MaxNoticeLen:
		return "O aviso passa do limite de 95 caracteres."
	}
	for _, c := range text {
		if _, ok := charmap.Windows1252.EncodeRune(c); !ok {
			return "O aviso tem um caractere que o jogo não mostra: " + string(c)
		}
	}
	return ""
}

func (s *Server) sendNotice(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(strings.ReplaceAll(r.PostFormValue("text"), "\n", " "))
	if msg := noticeProblem(text); msg != "" {
		s.renderNotice(w, r, http.StatusUnprocessableEntity, msg, text)
		return
	}
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		s.renderNotice(w, r, http.StatusServiceUnavailable, msg, text)
		return
	}
	sess := sessionOf(r)
	id, err := s.st.Enqueue(r.Context(), "notice", server.NoticeUser, map[string]any{"text": text}, sess.user)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("admin request", "admin", sess.user, "kind", "notice", "text", text, "delivery", id)
	http.Redirect(w, r, "/notice?req="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}
