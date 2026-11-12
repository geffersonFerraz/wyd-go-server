package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"dbsrv/internal/store"
)

func TestExpRuleScreen(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	code, body, _ := h.get("/gamedata/exp")
	if code != http.StatusOK || !strings.Contains(body, "cálculo original") || !strings.Contains(body, "Simulação") {
		t.Fatalf("page: %d", code)
	}
	form := url.Values{"csrf": {csrf}, "enabled": {"1"}, "global": {"200"}, "base_mode": {"template"}, "base_per_level": {"100"},
		"equal_range": {"3"}, "equal": {"100"}, "lower_per_level": {"4"}, "lower_max": {"180"}, "higher_per_level": {"6"}, "higher_min": {"5"},
		"evo1": {"100"}, "evo2": {"60"}, "evo3": {"35"}, "evo4": {"25"}, "offset3": {"50"}}
	if code, _, hdr := h.post("/gamedata/exp", form); code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "saved=1") {
		t.Fatalf("save: %d", code)
	}
	r, err := h.st.ExpRule(h.ctx)
	if err != nil || !r.Enabled || r.ZoneTable || r.GlobalPercent != 200 || r.EqualRange != 3 || r.LowerMaxPercent != 180 || r.HigherMinPercent != 5 ||
		r.Evolutions[store.EvoArch].Percent != 60 || r.Evolutions[store.EvoCelestial] != (store.ExpEvolution{Percent: 35, LevelOffset: 50}) {
		t.Fatalf("saved rule: %+v %v", r, err)
	}
	// The simulation uses the saved values: mortal, same level, double.
	if code, body, _ := h.get("/gamedata/exp"); code != http.StatusOK || !strings.Contains(body, "regra ativa") || !strings.Contains(body, `<td class="num">20000</td>`) {
		t.Fatalf("page after save: %d", code)
	}
	if ch, _ := h.st.RecentChanges(h.ctx, "exp", "", 5); len(ch) != 1 || !strings.Contains(ch[0].Summary, "regra ativada") || !strings.Contains(ch[0].Summary, "global 100% → 200%") {
		t.Fatalf("change log: %+v", ch)
	}
	// Inconsistent limits are refused and nothing changes.
	form.Set("lower_max", "50")
	if code, body, _ := h.post("/gamedata/exp", form); code != http.StatusUnprocessableEntity || !strings.Contains(body, "teto") {
		t.Fatalf("cap below equal: %d", code)
	}
	form.Set("lower_max", "180")
	form.Set("global", "-5")
	if code, _, _ := h.post("/gamedata/exp", form); code != http.StatusUnprocessableEntity {
		t.Fatalf("negative global: %d", code)
	}
	if r, _ := h.st.ExpRule(h.ctx); r.GlobalPercent != 200 || r.LowerMaxPercent != 180 {
		t.Fatalf("a refused save changed the rule: %+v", r)
	}
}
