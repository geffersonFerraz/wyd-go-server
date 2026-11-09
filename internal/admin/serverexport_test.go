package admin

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"dbsrv/internal/game"
	"dbsrv/internal/store"
)

// testWorld is the smallest world the import accepts.
func testWorld() *store.WorldAssets {
	w := &store.WorldAssets{Assets: []store.WorldAsset{
		{Name: store.AssetHeightMap, Content: make([]byte, 4096*4096)},
		{Name: store.AssetAttributeMap, Content: make([]byte, 1024*1024)},
		{Name: store.AssetItemList, Content: []byte("itens")},
	}, Templates: []store.MobTemplate{{Kind: store.TemplateNPC, Name: "Lobo", Content: make([]byte, 848)}}}
	for _, n := range game.ClassNames {
		w.Templates = append(w.Templates, store.MobTemplate{Kind: store.TemplateClass, Name: n, Content: make([]byte, 848)})
	}
	return w
}

// copyConfigFiles copies the real config folder and returns the files.
func copyConfigFiles(t *testing.T, dst string) []ExportFile {
	t.Helper()
	var out []ExportFile
	for _, n := range game.ConfigFiles {
		raw, err := os.ReadFile(filepath.Join("../../config", n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, n), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, ExportFile{Path: n, Content: raw})
	}
	return out
}

// uploadExport posts a file to the import step.
func uploadExport(t *testing.T, h *panelHarness, csrf string, e *ServerExport) (int, string) {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return uploadRaw(t, h, csrf, raw)
}

func uploadRaw(t *testing.T, h *panelHarness, csrf string, raw []byte) (int, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("csrf", csrf)
	fw, _ := mw.CreateFormFile("file", "export.json")
	_, _ = fw.Write(raw)
	mw.Close()
	code, body, _ := h.postRaw("/server/import", mw.FormDataContentType(), &b)
	return code, body
}

func TestServerExportImportRoundTrip(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	dir := t.TempDir()
	conf := filepath.Join(dir, "config")
	os.MkdirAll(conf, 0o755)
	os.MkdirAll(filepath.Join(dir, "state"), 0o755)
	copyConfigFiles(t, conf)
	raw, err := os.ReadFile("../../tmsrv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "tmsrv.yaml")
	os.WriteFile(settings, raw, 0o644)
	h.panel.cfg.ConfigDir, h.panel.cfg.SettingsPath, h.panel.cfg.StateDir = conf, settings, filepath.Join(dir, "state")
	if err := h.st.UpsertWorldAssets(h.ctx, testWorld()); err != nil {
		t.Fatal(err)
	}

	gd := testGameData()
	if err := h.st.ImportGameData(h.ctx, gd, "t"); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetMacBan(h.ctx, [8]byte{9, 8}, true); err != nil {
		t.Fatal(err)
	}
	code, body, _ := h.get("/server/export")
	if code != http.StatusOK {
		t.Fatalf("export: %d", code)
	}
	var e ServerExport
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatal(err)
	}
	if len(e.Files) != len(game.ConfigFiles) || e.World == nil || len(e.World.Templates) != 5 || len(e.MacBans) != 1 || len(e.GameData.Items) != len(gd.Items) || !strings.Contains(e.TmsrvYAML, "wars:") {
		t.Fatalf("export: %d files %v, bans %d, items %d", len(e.Files), e.Files, len(e.MacBans), len(e.GameData.Items))
	}

	// Changing the file only shows up after the confirmation.
	e.GameData.Items[0].Price = 777
	e.Files[0].Content = append(bytes.Clone(e.Files[0].Content), "\n# novo\n"...)
	e.World.Templates[0].Content = bytes.Repeat([]byte{7}, 848)
	e.TmsrvYAML += "\n# importado\n"
	e.MacBans = nil
	if code, body := uploadExport(t, h, csrf, &e); code != http.StatusOK || !strings.Contains(body, "Confirmar importação") {
		t.Fatalf("preview: %d %s", code, body)
	}
	back, _ := h.st.LoadGameData(h.ctx)
	if back.Items[0].Price == 777 {
		t.Fatal("the preview changed the database")
	}
	if code, _, _ := h.post("/server/import/apply", url.Values{"csrf": {csrf}, "token": {"errado"}}); code != http.StatusConflict {
		t.Fatalf("wrong token: %d", code)
	}
	if code, _, _ := h.post("/server/import/apply", url.Values{"csrf": {csrf}, "token": {h.panel.pending.token}}); code != http.StatusSeeOther {
		t.Fatalf("apply: %d", code)
	}
	back, _ = h.st.LoadGameData(h.ctx)
	if back.Items[0].Price != 777 {
		t.Fatal("database not imported")
	}
	if macs, _ := h.st.MacBans(h.ctx); len(macs) != 0 {
		t.Fatalf("bans: %v", macs)
	}
	if got, _ := os.ReadFile(filepath.Join(conf, e.Files[0].Path)); !strings.HasSuffix(string(got), "# novo\n") {
		t.Fatalf("file: %q", got[max(len(got)-20, 0):])
	}
	world, _ := h.st.LoadWorldAssets(h.ctx)
	if !bytes.Equal(world.Templates[0].Content, e.World.Templates[0].Content) {
		t.Fatal("templates not imported")
	}
	if got, _ := os.ReadFile(settings); !strings.Contains(string(got), "# importado") {
		t.Fatal("tmsrv.yaml not written")
	}
	if snaps, _ := filepath.Glob(filepath.Join(dir, "state", "antes-do-import-*.json")); len(snaps) != 1 {
		t.Fatalf("snapshots: %v", snaps)
	}
}

func TestServerImportRefusesBadFiles(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	dir := t.TempDir()
	raw, _ := os.ReadFile("../../tmsrv.yaml")
	settings := filepath.Join(dir, "tmsrv.yaml")
	os.WriteFile(settings, raw, 0o644)
	h.panel.cfg.ConfigDir, h.panel.cfg.SettingsPath, h.panel.cfg.StateDir = dir, settings, dir
	files := copyConfigFiles(t, dir)
	good := func() *ServerExport {
		return &ServerExport{Format: exportFormat, Version: exportVersion, TmsrvYAML: string(raw), GameData: testGameData(), World: testWorld(), Files: slices.Clone(files)}
	}
	if code, _ := uploadRaw(t, h, csrf, []byte("{ nope")); code != http.StatusUnprocessableEntity {
		t.Fatalf("broken json: %d", code)
	}
	e := good()
	e.Format = "outra-coisa"
	if code, _ := uploadExport(t, h, csrf, e); code != http.StatusUnprocessableEntity {
		t.Fatalf("format: %d", code)
	}
	for _, p := range []string{"../../etc/x", "../tmsrv.yaml", "/etc/passwd", "", `a\\b`, "HeightMap.dat"} {
		e = good()
		e.Files = append(e.Files, ExportFile{Path: p, Content: []byte("x")})
		if code, _ := uploadExport(t, h, csrf, e); code != http.StatusUnprocessableEntity {
			t.Fatalf("path %q: %d", p, code)
		}
	}
	e = good()
	e.World = nil
	if code, _ := uploadExport(t, h, csrf, e); code != http.StatusUnprocessableEntity {
		t.Fatalf("no world: %d", code)
	}
	e = good()
	e.World.Assets[0].Content = []byte("mapa")
	if code, body := uploadExport(t, h, csrf, e); code != http.StatusUnprocessableEntity || !strings.Contains(body, "HeightMap") {
		t.Fatalf("short map: %d", code)
	}
	e = good()
	e.TmsrvYAML = "limits: {}\n"
	if code, _ := uploadExport(t, h, csrf, e); code != http.StatusUnprocessableEntity {
		t.Fatalf("settings: %d", code)
	}
	e = good()
	e.GameData.Drops = append(e.GameData.Drops, store.Drop{Name: "Ruim", Items: []store.DropItem{{Kills: 0, Item: 412}}})
	if code, body := uploadExport(t, h, csrf, e); code != http.StatusUnprocessableEntity || !strings.Contains(body, "banco recusou") {
		t.Fatalf("database check: %d", code)
	}
	if h.panel.pending != nil {
		t.Fatal("a refused file is pending")
	}
}

// testGameData is the smallest configuration the import accepts.
func testGameData() *store.GameData {
	gd := &store.GameData{
		Items:  []store.Item{{ID: 412, Name: "Pedra", Price: 10}},
		Skills: []store.Skill{{ID: 1, Name: "Golpe"}},
		Drops: []store.Drop{{Name: "Lobo", MaxDrops: 1, Items: []store.DropItem{{Kills: 2, Item: 412}},
			Rate: []store.RateDrop{{Slot: 3, Item: 412, Chance: 9}}}},
		Teleports: []store.Teleport{{ID: 0, SrcX: 1, SrcY: 2, DstX: 3, DstY: 4}},
	}
	for i := range 64 {
		gd.DropSlots = append(gd.DropSlots, store.DropSlot{Slot: i, Base: 10, Rate: 100})
	}
	gd.BlockedDrops = []int{412}
	gd.CityOwners = []store.CityOwner{{Channel: 1, City: 0, GuildID: 0, ChangedBy: "t"}}
	return gd
}
