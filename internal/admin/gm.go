package admin

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"dbsrv/internal/store"
	"golang.org/x/text/encoding/charmap"
)

// GM commands from the panel (the game no longer runs them) and the messages
// players write with /gm. A command is a "gm_command" delivery: DBSRV hands
// it to its channel and the channel's answer shows up in the list.

// gmCommand is one entry of the command list.
type gmCommand struct {
	Name, Args, Help string
	NeedsChar        bool
}

var gmCommands = []gmCommand{
	{"+item", "id ef1 val1 ef2 val2 ef3 val3", "Dá um item ao personagem", true},
	{"+equip", "slot id ef1 val1 ef2 val2 ef3 val3", "Coloca um item no slot de equipamento", true},
	{"+status", "str|dex|int|con valor, ou slot id ef1 val1 ...", "Muda um atributo ou o equipamento", true},
	{"+level", "nível", "Muda o nível", true},
	{"+exp", "valor", "Muda a experiência", true},
	{"+coin", "valor", "Muda o gold", true},
	{"+cash", "valor", "Muda o cash", true},
	{"+frag", "valor", "Muda os frags", true},
	{"+special", "índice(0-3) valor", "Muda a maestria", true},
	{"+learn", "skill", "Liga ou desliga uma skill", true},
	{"+buff", "índice", "Dá um buff", true},
	{"+nobuff", "", "Tira todos os buffs", true},
	{"+snoop", "", "Liga ou desliga o snoop", true},
	{"+move", "x y", "Move o personagem", true},
	{"+relo", "nome", "Leva o personagem até outro jogador", true},
	{"+summon", "nome", "Traz um jogador até o personagem", true},
	{"+summonserver", "", "Traz todos do canal até o personagem", true},
	{"+create", "npc quantidade", "Cria NPCs ao lado do personagem", true},
	{"+limparinv", "", "Limpa o inventário", true},
	{"+limparbau", "", "Limpa o baú", true},
	{"+set", "mission id", "Define a missão diária", true},
	{"+on", "", "Mostra quantos estão online", false},
	{"+not", "texto", "Aviso neste canal", false},
	{"+kick", "nome", "Desconecta um jogador", false},
	{"+teleport", "nome x y", "Move um jogador", false},
	{"+ban", "nome tipo dia mês ano", "Bane a conta de um jogador", false},
	{"+banmac", "nome", "Bane o computador de um jogador", false},
	{"+dcall", "", "Desconecta todos do canal", false},
	{"+generate", "gerador", "Nasce um gerador do NPCGener", false},
	{"+hpmob", "hp nome", "Muda o HP de um monstro", false},
	{"+remove", "nome", "Remove o primeiro monstro com esse nome", false},
	{"+removeall", "nome", "Remove todos os monstros com esse nome", false},
	{"+gate", "id [abrir]", "Abre ou fecha um portão", false},
	{"+fame", "guild valor", "Muda a fame da guild", false},
	{"+weekmode", "0-5", "Adianta a semana de guerra", false},
	{"+createkefra", "", "Faz o Kefra renascer", false},
	{"+kill", "kefra guild", "Marca o Kefra como derrotado", false},
	{"+weather", "0-2", "Muda o clima", false},
	{"+bonusexp", "porcentagem", "Bônus de experiência", false},
	{"+drop", "taxa (0 = tmsrv.yaml)", "Taxa de drop de todos os slots", false},
}

const gmMaxCommandLen = 120

func (s *Server) gmRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /gm", s.auth(s.gmPage))
	mux.HandleFunc("POST /gm", s.auth(s.sendGMCommand))
	mux.HandleFunc("GET /stores", s.auth(s.storesPage))
	mux.HandleFunc("GET /messages", s.auth(s.messagesPage))
	mux.HandleFunc("POST /messages/{id}/seen", s.auth(s.markMessageSeen))
}

func (s *Server) gmPage(w http.ResponseWriter, r *http.Request) {
	s.renderGM(w, r, http.StatusOK, "", gmForm{Channel: s.channels()[0]})
}

type gmForm struct {
	Channel             int
	Char, Command, Args string
}

func (s *Server) renderGM(w http.ResponseWriter, r *http.Request, code int, msg string, f gmForm) {
	list, err := s.st.GMCommands(r.Context(), 50)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, code, "gm", map[string]any{"Title": "Comandos GM", "Commands": gmCommands, "History": list,
		"ChannelList": s.channels(), "Form": f, "Error": msg})
}

// gmProblem checks a command before it is queued ("" when it is fine).
func gmProblem(cmd *gmCommand, f gmForm) string {
	switch {
	case cmd == nil:
		return "Escolha um comando da lista."
	case cmd.NeedsChar && f.Char == "":
		return "Este comando precisa do nome de um personagem online."
	case utf8.RuneCountInString(f.Char) > 15:
		return "Nome de personagem longo demais."
	case utf8.RuneCountInString(f.Args) > gmMaxCommandLen:
		return "Argumentos longos demais."
	}
	for _, c := range f.Char + f.Args {
		if _, ok := charmap.Windows1252.EncodeRune(c); !ok {
			return "Há um caractere que o jogo não aceita: " + string(c)
		}
	}
	return ""
}

func (s *Server) sendGMCommand(w http.ResponseWriter, r *http.Request) {
	f := gmForm{Channel: s.pickChannel(r), Char: strings.TrimSpace(r.PostFormValue("char")), Command: r.PostFormValue("command"),
		Args: strings.TrimSpace(strings.ReplaceAll(r.PostFormValue("args"), "\n", " "))}
	var cmd *gmCommand
	if i := slices.IndexFunc(gmCommands, func(c gmCommand) bool { return c.Name == f.Command }); i >= 0 {
		cmd = &gmCommands[i]
	}
	if msg := gmProblem(cmd, f); msg != "" {
		s.renderGM(w, r, http.StatusUnprocessableEntity, msg, f)
		return
	}
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		s.renderGM(w, r, http.StatusServiceUnavailable, msg, f)
		return
	}
	line := strings.TrimSpace(f.Command + " " + f.Args)
	by := sessionOf(r).user
	id, err := s.st.Enqueue(r.Context(), "gm_command", store.GMCommandUser, map[string]any{"channel": f.Channel, "char_name": f.Char, "command": line}, by)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("admin request", "admin", by, "kind", "gm_command", "channel", f.Channel, "char", f.Char, "command", line, "delivery", id)
	http.Redirect(w, r, "/gm", http.StatusSeeOther)
}

func (s *Server) messagesPage(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"
	list, err := s.st.GMMessages(r.Context(), !all, 300)
	if err != nil {
		s.fail(w, err)
		return
	}
	unseen, err := s.st.UnseenGMMessages(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, http.StatusOK, "messages", map[string]any{"Title": "Mensagens", "Messages": list, "Unseen": unseen, "All": all})
}

// markMessageSeen is POST /messages/{id}/seen (id 0 = all).
func (s *Server) markMessageSeen(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 0 {
		http.NotFound(w, r)
		return
	}
	if err := s.st.MarkGMMessagesSeen(r.Context(), id, sessionOf(r).user); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/messages", http.StatusSeeOther)
}

func (s *Server) storesPage(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.OpenStores(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, http.StatusOK, "stores", map[string]any{"Title": "Lojas abertas", "Stores": list})
}
