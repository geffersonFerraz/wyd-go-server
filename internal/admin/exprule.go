package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"dbsrv/internal/store"
)

// The experience rule screen (gamedata.exp_rule): base experience, level
// difference, evolution and the global multiplier, with a table of what the
// values on screen give.

func (s *Server) expRuleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /gamedata/exp", s.auth(s.expRulePage))
	mux.HandleFunc("POST /gamedata/exp", s.auth(s.saveExpRule))
}

var expEvolutions = []struct {
	Ev    int
	Label string
}{{store.EvoMortal, "Mortal"}, {store.EvoArch, "Arch"}, {store.EvoCelestial, "Celestial"}, {store.EvoSubCele, "Sub-Celestial"}}

// expPreviewDiffs are the level differences of the table (monster - character).
var expPreviewDiffs = []int{-50, -20, -10, -5, 0, 5, 10, 20, 50}

type expEvolutionRow struct {
	Ev          int
	Label       string
	Percent     int
	LevelOffset int
}

type expPreviewRow struct {
	Label string
	Cells []int64
}

// expPreview is the experience of a monster of level 200 and 10.000 of base
// experience for a character of each evolution, by level difference.
func expPreview(r *store.ExpRule) (base int64, rows []expPreviewRow) {
	const mobLevel, templateExp = 200, 10000
	base = r.Base(templateExp, mobLevel)
	for _, e := range expEvolutions {
		row := expPreviewRow{Label: e.Label}
		for _, d := range expPreviewDiffs {
			// The character's level as the rule compares it, offset included.
			row.Cells = append(row.Cells, r.Apply(templateExp, mobLevel-d-r.Evolutions[e.Ev].LevelOffset, mobLevel, e.Ev))
		}
		rows = append(rows, row)
	}
	return base, rows
}

func (s *Server) expRulePage(w http.ResponseWriter, r *http.Request) {
	rule, err := s.st.ExpRule(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	msg := ""
	if r.URL.Query().Has("saved") {
		msg = reloadNote
	}
	s.renderExpRule(w, r, http.StatusOK, &rule, msg, "")
}

func (s *Server) renderExpRule(w http.ResponseWriter, r *http.Request, code int, rule *store.ExpRule, msg, errMsg string) {
	evos := make([]expEvolutionRow, len(expEvolutions))
	for i, e := range expEvolutions {
		evos[i] = expEvolutionRow{Ev: e.Ev, Label: e.Label, Percent: rule.Evolutions[e.Ev].Percent, LevelOffset: rule.Evolutions[e.Ev].LevelOffset}
	}
	base, preview := expPreview(rule)
	changes, err := s.st.RecentChanges(r.Context(), "exp", "", 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.page(w, r, code, "exprule", map[string]any{"Title": "Experiência", "Rule": rule, "Evolutions": evos,
		"ByLevel": rule.BaseMode == store.ExpBaseLevel, "PreviewBase": base, "Preview": preview, "Diffs": expPreviewDiffs,
		"Msg": msg, "Error": errMsg, "Changes": changes})
}

func (s *Server) saveExpRule(w http.ResponseWriter, r *http.Request) {
	old, err := s.st.ExpRule(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	f := &formReader{r: r}
	rule := store.ExpRule{
		Enabled:          r.PostFormValue("enabled") != "",
		ZoneTable:        r.PostFormValue("zone_table") != "",
		GlobalPercent:    f.int("global", "Multiplicador global", 0, 100000),
		BaseMode:         r.PostFormValue("base_mode"),
		BaseFixed:        int64(f.int("base_fixed", "Base fixa", 0, 2000000000)),
		BasePerLevel:     int64(f.int("base_per_level", "Base por nível", 0, 2000000000)),
		EqualRange:       f.int("equal_range", "Faixa de nível igual", 0, 400),
		EqualPercent:     f.int("equal", "Nível igual", 0, 10000),
		LowerPerLevel:    f.int("lower_per_level", "Bônus por nível abaixo", 0, 10000),
		LowerMaxPercent:  f.int("lower_max", "Teto abaixo do monstro", 0, 100000),
		HigherPerLevel:   f.int("higher_per_level", "Redução por nível acima", 0, 10000),
		HigherMinPercent: f.int("higher_min", "Piso acima do monstro", 0, 10000),
	}
	if rule.BaseMode != store.ExpBaseLevel {
		rule.BaseMode = store.ExpBaseTemplate
	}
	for _, e := range expEvolutions {
		rule.Evolutions[e.Ev] = store.ExpEvolution{
			Percent:     f.int(fmt.Sprintf("evo%d", e.Ev), e.Label, 0, 10000),
			LevelOffset: f.int(fmt.Sprintf("offset%d", e.Ev), "Ajuste de nível ("+e.Label+")", 0, 1000),
		}
	}
	rule.Evolutions[0] = store.ExpEvolution{Percent: 100}
	if rule.LowerMaxPercent < rule.EqualPercent {
		f.errs = append(f.errs, "O teto para quem está abaixo do monstro não pode ser menor que o valor de nível igual.")
	}
	if rule.HigherMinPercent > rule.EqualPercent {
		f.errs = append(f.errs, "O piso para quem está acima do monstro não pode ser maior que o valor de nível igual.")
	}
	if rule.BaseMode == store.ExpBaseLevel && rule.BaseFixed == 0 && rule.BasePerLevel == 0 {
		f.errs = append(f.errs, "Com a base calculada pelo nível, informe a base fixa ou o valor por nível.")
	}
	if e := f.err(); e != "" {
		s.renderExpRule(w, r, http.StatusUnprocessableEntity, &rule, "", e)
		return
	}
	if err := s.st.SaveExpRule(r.Context(), &rule, sessionOf(r).user, expRuleSummary(&old, &rule)); err != nil {
		s.renderExpRule(w, r, http.StatusUnprocessableEntity, &rule, "", "O banco recusou: "+err.Error())
		return
	}
	http.Redirect(w, r, "/gamedata/exp?saved=1", http.StatusSeeOther)
}

// expRuleSummary says what a save changed.
func expRuleSummary(old, now *store.ExpRule) string {
	var parts []string
	if old.Enabled != now.Enabled {
		parts = append(parts, map[bool]string{true: "regra ativada", false: "regra desativada"}[now.Enabled])
	}
	if old.GlobalPercent != now.GlobalPercent {
		parts = append(parts, fmt.Sprintf("global %d%% → %d%%", old.GlobalPercent, now.GlobalPercent))
	}
	if old.BaseMode != now.BaseMode || old.BaseFixed != now.BaseFixed || old.BasePerLevel != now.BasePerLevel {
		parts = append(parts, "experiência base")
	}
	if old.EqualRange != now.EqualRange || old.EqualPercent != now.EqualPercent || old.LowerPerLevel != now.LowerPerLevel ||
		old.LowerMaxPercent != now.LowerMaxPercent || old.HigherPerLevel != now.HigherPerLevel || old.HigherMinPercent != now.HigherMinPercent {
		parts = append(parts, "diferença de nível")
	}
	for _, e := range expEvolutions {
		if a, b := old.Evolutions[e.Ev], now.Evolutions[e.Ev]; a != b {
			parts = append(parts, e.Label+" "+strconv.Itoa(b.Percent)+"%")
		}
	}
	if old.ZoneTable != now.ZoneTable {
		parts = append(parts, map[bool]string{true: "tabela de zonas ligada", false: "tabela de zonas desligada"}[now.ZoneTable])
	}
	if len(parts) == 0 {
		return "sem mudanças"
	}
	return strings.Join(parts, ", ")
}
