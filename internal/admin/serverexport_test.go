package admin

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbsrv/internal/store"
)

func TestAllowedExportPath(t *testing.T) {
	for p, want := range map[string]bool{
		"tmsrv/npc/Kefra": true, "tmsrv/NPCGener.txt": true, "dbsrv/npc/TK": true, "tmsrv/Data/Missions.xml": true,
		"../tmsrv.yaml": false, "tmsrv/npc/../../x": false, "tmsrv/Cache/LockAccount/A.bin": false, "/etc/passwd": false,
		"tmsrv/HeightMap.dat": false, "tmsrv/npc/a/b": false, `tmsrv\npc\x`: false, "": false,
	} {
		if got := allowedExportPath(p); got != want {
			t.Errorf("%q: %v", p, got)
		}
	}
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
	data := filepath.Join(dir, "data")
	os.MkdirAll(filepath.Join(data, "tmsrv", "npc"), 0o755)
	os.MkdirAll(filepath.Join(dir, "state"), 0o755)
	os.WriteFile(filepath.Join(data, "tmsrv", "npc", "Lobo"), []byte{1, 2, 0xFF}, 0o644)
	os.WriteFile(filepath.Join(data, "tmsrv", "HeightMap.dat"), []byte("mapa"), 0o644)
	raw, err := os.ReadFile("../../tmsrv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "tmsrv.yaml")
	os.WriteFile(settings, raw, 0o644)
	h.panel.cfg.DataDir, h.panel.cfg.SettingsPath, h.panel.cfg.StateDir = data, settings, filepath.Join(dir, "state")

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
	if len(e.Files) != 1 || e.Files[0].Path != "tmsrv/npc/Lobo" || len(e.MacBans) != 1 || len(e.GameData.Items) != len(gd.Items) || !strings.Contains(e.TmsrvYAML, "wars:") {
		t.Fatalf("export: %d files %v, bans %d, items %d", len(e.Files), e.Files, len(e.MacBans), len(e.GameData.Items))
	}

	// Changing the file only shows up after the confirmation.
	e.GameData.Items[0].Price = 777
	e.Files[0].Content = []byte("novo")
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
	if got, _ := os.ReadFile(filepath.Join(data, "tmsrv", "npc", "Lobo")); string(got) != "novo" {
		t.Fatalf("file: %q", got)
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
	h.panel.cfg.DataDir, h.panel.cfg.SettingsPath, h.panel.cfg.StateDir = dir, settings, dir
	good := func() *ServerExport {
		return &ServerExport{Format: exportFormat, Version: exportVersion, TmsrvYAML: string(raw), GameData: testGameData()}
	}
	if code, _ := uploadRaw(t, h, csrf, []byte("{ nope")); code != http.StatusUnprocessableEntity {
		t.Fatalf("broken json: %d", code)
	}
	e := good()
	e.Format = "outra-coisa"
	if code, _ := uploadExport(t, h, csrf, e); code != http.StatusUnprocessableEntity {
		t.Fatalf("format: %d", code)
	}
	e = good()
	e.Files = []ExportFile{{Path: "../../etc/x", Content: []byte("x")}}
	if code, _ := uploadExport(t, h, csrf, e); code != http.StatusUnprocessableEntity {
		t.Fatalf("path: %d", code)
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
