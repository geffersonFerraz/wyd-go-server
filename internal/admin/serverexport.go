package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"dbsrv/internal/game"
	"dbsrv/internal/store"
)

// Server export: all the configuration in one JSON file, and the import that
// puts it back. Players, accounts, guilds and the state of the wars are not
// configuration and stay out.
//
// An import runs in two steps. The upload is checked in full (the database
// included, in a transaction that is rolled back) and shown as a summary;
// nothing changes until the staff confirms. Before applying, the current
// configuration is saved in the panel's state folder.

const (
	exportFormat    = "wyd-server-export"
	exportVersion   = 1
	exportMaxUpload = 128 << 20
	importTTL       = 15 * time.Minute
	snapshotsKept   = 10
)

// exportDirs are the files of the data folder that hold configuration. The
// maps, the client files and Cache/ (player state) are left out.
var (
	exportDirs  = []string{"tmsrv/Data", "tmsrv/Conf", "tmsrv/Controlador EXP", "tmsrv/npc", "tmsrv/npc_base", "dbsrv/npc"}
	exportFiles = []string{"tmsrv/guild_zone.txt", "tmsrv/InitItem.csv", "tmsrv/NPCGener.txt", "tmsrv/Language.txt"}
)

// ExportFile is one configuration file; Content is base64 in the JSON
// because many of them are binary or Windows-1252.
type ExportFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}

// ServerExport is the whole file.
type ServerExport struct {
	Format     string          `json:"format"`
	Version    int             `json:"version"`
	ExportedAt time.Time       `json:"exported_at"`
	ExportedBy string          `json:"exported_by"`
	TmsrvYAML  string          `json:"tmsrv_yaml"`
	GameData   *store.GameData `json:"gamedata"`
	MacBans    []string        `json:"mac_bans"`
	Files      []ExportFile    `json:"files"`
}

// importCount is one line of the summary.
type importCount struct {
	Name         string
	Current, New int
}

type pendingImport struct {
	token    string
	user     string
	export   *ServerExport
	macs     [][8]byte
	expires  time.Time
	changed  []string // files that differ
	settings bool     // tmsrv.yaml differs
	counts   []importCount
	warnings []string
}

func (s *Server) serverRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /server", s.auth(s.serverPage))
	mux.HandleFunc("GET /server/export", s.auth(s.exportServer))
	mux.HandleFunc("POST /server/import", limitBody(exportMaxUpload, s.auth(s.previewImport)))
	mux.HandleFunc("POST /server/import/apply", s.auth(s.applyImport))
	mux.HandleFunc("POST /server/import/cancel", s.auth(s.cancelImport))
}

func (s *Server) serverPage(w http.ResponseWriter, r *http.Request) {
	s.renderServer(w, r, http.StatusOK, r.URL.Query().Get("msg"), "", s.currentImport(sessionOf(r).user))
}

func (s *Server) renderServer(w http.ResponseWriter, r *http.Request, code int, msg, errMsg string, p *pendingImport) {
	data := map[string]any{"Title": "Exportar servidor", "Msg": msg, "Error": errMsg, "Enabled": s.cfg.DataDir != "" && s.cfg.SettingsPath != ""}
	if p != nil {
		data["Pending"] = map[string]any{"Token": p.token, "By": p.export.ExportedBy, "At": p.export.ExportedAt, "Counts": p.counts,
			"Changed": p.changed, "ChangedTotal": len(p.changed), "Settings": p.settings, "Warnings": p.warnings}
	}
	s.page(w, r, code, "server", data)
}

func (s *Server) currentImport(user string) *pendingImport {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.pending; p != nil && p.user == user && time.Now().Before(p.expires) {
		return p
	}
	return nil
}

// buildExport reads the current configuration.
func (s *Server) buildExport(ctx context.Context, by string) (*ServerExport, error) {
	gd, err := s.st.LoadGameData(ctx)
	if err != nil {
		return nil, err
	}
	macs, err := s.st.MacBans(ctx)
	if err != nil {
		return nil, err
	}
	yaml, err := os.ReadFile(s.cfg.SettingsPath)
	if err != nil {
		return nil, err
	}
	files, err := readExportFiles(s.cfg.DataDir)
	if err != nil {
		return nil, err
	}
	e := &ServerExport{Format: exportFormat, Version: exportVersion, ExportedAt: time.Now(), ExportedBy: by,
		TmsrvYAML: string(yaml), GameData: gd, MacBans: []string{}, Files: files}
	for _, m := range macs {
		e.MacBans = append(e.MacBans, hex.EncodeToString(m[:]))
	}
	return e, nil
}

func readExportFiles(dataDir string) ([]ExportFile, error) {
	var out []ExportFile
	add := func(rel string) error {
		raw, err := os.ReadFile(filepath.Join(dataDir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		out = append(out, ExportFile{Path: rel, Content: raw})
		return nil
	}
	for _, rel := range exportFiles {
		if err := add(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	for _, dir := range exportDirs {
		entries, err := os.ReadDir(filepath.Join(dataDir, filepath.FromSlash(dir)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Type().IsRegular() {
				if err := add(dir + "/" + e.Name()); err != nil {
					return nil, err
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (s *Server) exportServer(w http.ResponseWriter, r *http.Request) {
	by := sessionOf(r).user
	e, err := s.buildExport(r.Context(), by)
	if err != nil {
		s.fail(w, err)
		return
	}
	name := "server-export-" + e.ExportedAt.Format("20060102-150405") + ".json"
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	if err := enc.Encode(e); err != nil {
		s.log.Error("server export", "err", err)
		return
	}
	s.log.Info("server exported", "admin", by, "files", len(e.Files), "items", len(e.GameData.Items))
}

// allowedExportPath reports whether an import may write this file. The list
// is the same that the export reads, so a crafted file cannot reach anything
// else in the data folder.
func allowedExportPath(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\\x00") || filepath.IsAbs(p) || p != filepath.ToSlash(filepath.Clean(p)) || strings.HasPrefix(p, "../") {
		return false
	}
	if slices.Contains(exportFiles, p) {
		return true
	}
	dir, name, ok := cutLast(p)
	return ok && name != "" && name != ".." && slices.Contains(exportDirs, dir)
}

func cutLast(p string) (string, string, bool) {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "", "", false
	}
	return p[:i], p[i+1:], true
}

// checkExport validates a parsed file (everything but the database).
func (s *Server) checkExport(e *ServerExport) ([][8]byte, error) {
	if e.Format != exportFormat {
		return nil, errors.New("não é um arquivo de exportação do servidor")
	}
	if e.Version != exportVersion {
		return nil, fmt.Errorf("versão %d do arquivo; este painel lê a versão %d", e.Version, exportVersion)
	}
	if e.GameData == nil || len(e.GameData.Items) == 0 {
		return nil, errors.New("o arquivo não tem a lista de itens")
	}
	if err := (&game.Data{}).ApplyGameData(e.GameData); err != nil {
		return nil, fmt.Errorf("dados do jogo: %w", err)
	}
	tmp, err := os.CreateTemp("", "tmsrv-import-*.yaml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(e.TmsrvYAML)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if _, err := game.LoadSettings(tmp.Name()); err != nil {
		return nil, fmt.Errorf("tmsrv.yaml: %s", strings.ReplaceAll(err.Error(), tmp.Name(), "tmsrv.yaml"))
	}
	seen := map[string]bool{}
	for _, f := range e.Files {
		if !allowedExportPath(f.Path) {
			return nil, fmt.Errorf("arquivo não permitido: %q", f.Path)
		}
		if seen[f.Path] {
			return nil, fmt.Errorf("arquivo repetido: %q", f.Path)
		}
		seen[f.Path] = true
	}
	macs := make([][8]byte, 0, len(e.MacBans))
	for _, h := range e.MacBans {
		raw, err := hex.DecodeString(h)
		if err != nil || len(raw) != 8 {
			return nil, fmt.Errorf("banimento inválido: %q", h)
		}
		macs = append(macs, [8]byte(raw))
	}
	return macs, nil
}

func (s *Server) previewImport(w http.ResponseWriter, r *http.Request) {
	user := sessionOf(r).user
	fail := func(code int, msg string) { s.renderServer(w, r, code, "", msg, nil) }
	if s.cfg.DataDir == "" || s.cfg.SettingsPath == "" {
		fail(http.StatusServiceUnavailable, "Importação indisponível: o painel não conhece a pasta de dados.")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		fail(http.StatusUnprocessableEntity, "Escolha o arquivo de exportação.")
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		fail(http.StatusUnprocessableEntity, "Não foi possível ler o arquivo.")
		return
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	e := &ServerExport{}
	if err := dec.Decode(e); err != nil {
		fail(http.StatusUnprocessableEntity, "O arquivo não é um JSON válido: "+err.Error())
		return
	}
	macs, err := s.checkExport(e)
	if err != nil {
		fail(http.StatusUnprocessableEntity, "Arquivo recusado: "+err.Error())
		return
	}
	if err := s.st.ImportServerConfig(r.Context(), e.GameData, macs, user, true); err != nil {
		fail(http.StatusUnprocessableEntity, "O banco recusou os dados: "+err.Error())
		return
	}
	p, err := s.summarizeImport(r.Context(), e, macs, user)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.mu.Lock()
	s.pending = p
	s.mu.Unlock()
	s.log.Info("server import checked", "admin", user, "items", len(e.GameData.Items), "files_changed", len(p.changed))
	s.renderServer(w, r, http.StatusOK, "", "", p)
}

func (s *Server) summarizeImport(ctx context.Context, e *ServerExport, macs [][8]byte, user string) (*pendingImport, error) {
	cur, err := s.buildExport(ctx, user)
	if err != nil {
		return nil, err
	}
	lines := func(gd *store.GameData) (fixed, chance int) {
		for _, d := range gd.Drops {
			fixed += len(d.Items)
			chance += len(d.Rate)
		}
		return
	}
	cf, cc := lines(cur.GameData)
	nf, nc := lines(e.GameData)
	p := &pendingImport{token: randomToken(), user: user, export: e, macs: macs, expires: time.Now().Add(importTTL),
		settings: cur.TmsrvYAML != e.TmsrvYAML}
	p.counts = []importCount{
		{"Itens", len(cur.GameData.Items), len(e.GameData.Items)},
		{"Skills", len(cur.GameData.Skills), len(e.GameData.Skills)},
		{"Monstros na tabela de drops", len(cur.GameData.Drops), len(e.GameData.Drops)},
		{"Drops fixos", cf, nf},
		{"Drops por chance", cc, nc},
		{"Itens bloqueados", len(cur.GameData.BlockedDrops), len(e.GameData.BlockedDrops)},
		{"Teleportes", len(cur.GameData.Teleports), len(e.GameData.Teleports)},
		{"Donos de cidade", len(cur.GameData.CityOwners), len(e.GameData.CityOwners)},
		{"Computadores banidos", len(cur.MacBans), len(e.MacBans)},
		{"Arquivos de configuração", len(cur.Files), len(e.Files)},
	}
	old := make(map[string][32]byte, len(cur.Files))
	for _, f := range cur.Files {
		old[f.Path] = sha256.Sum256(f.Content)
	}
	for _, f := range e.Files {
		if h, ok := old[f.Path]; !ok || h != sha256.Sum256(f.Content) {
			p.changed = append(p.changed, f.Path)
		}
	}
	guilds, err := s.st.Guilds(ctx)
	if err != nil {
		return nil, err
	}
	known := map[int]bool{0: true}
	for _, g := range guilds {
		known[g.ID] = true
	}
	for _, c := range e.GameData.CityOwners {
		if !known[c.GuildID] {
			p.warnings = append(p.warnings, fmt.Sprintf("A cidade %d do canal %d aponta para a guilda %d, que não existe neste servidor.", c.City, c.Channel, c.GuildID))
		}
	}
	if p.settings {
		p.warnings = append(p.warnings, "O tmsrv.yaml muda. Confira se os canais continuam iguais aos do docker-compose.")
	}
	return p, nil
}

func (s *Server) cancelImport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.pending = nil
	s.mu.Unlock()
	http.Redirect(w, r, "/server", http.StatusSeeOther)
}

func (s *Server) applyImport(w http.ResponseWriter, r *http.Request) {
	user := sessionOf(r).user
	p := s.currentImport(user)
	if p == nil || r.PostFormValue("token") != p.token {
		s.renderServer(w, r, http.StatusConflict, "", "Esta importação expirou. Envie o arquivo de novo.", nil)
		return
	}
	snapshot, err := s.saveSnapshot(r.Context(), user)
	if err != nil {
		s.renderServer(w, r, http.StatusInternalServerError, "", "Importação cancelada: não foi possível guardar a configuração atual ("+err.Error()+").", p)
		return
	}
	if err := s.st.ImportServerConfig(r.Context(), p.export.GameData, p.macs, user, false); err != nil {
		s.renderServer(w, r, http.StatusUnprocessableEntity, "", "O banco recusou os dados, e nada mudou: "+err.Error(), p)
		return
	}
	if err := s.writeImportedFiles(p); err != nil {
		s.log.Error("server import: files", "admin", user, "err", err)
		s.renderServer(w, r, http.StatusInternalServerError, "", "O banco foi importado, mas falhou a gravação dos arquivos: "+err.Error()+". A configuração anterior está em "+snapshot+".", nil)
		return
	}
	s.mu.Lock()
	s.pending = nil
	s.mu.Unlock()
	s.log.Warn("server configuration imported", "admin", user, "snapshot", snapshot, "files_changed", len(p.changed), "settings_changed", p.settings)
	msg := "Importação concluída. A configuração anterior está em " + snapshot + "."
	if p.settings || len(p.changed) > 0 {
		msg += " Reinicie o DBSRV e os canais para valerem o tmsrv.yaml e os arquivos."
	}
	http.Redirect(w, r, "/server?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *Server) writeImportedFiles(p *pendingImport) error {
	for _, rel := range p.changed {
		i := slices.IndexFunc(p.export.Files, func(f ExportFile) bool { return f.Path == rel })
		path := filepath.Join(s.cfg.DataDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		tmp := path + ".import"
		if err := os.WriteFile(tmp, p.export.Files[i].Content, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	if p.settings {
		// In place: in Docker the file is a bind mount and cannot be replaced.
		return os.WriteFile(s.cfg.SettingsPath, []byte(p.export.TmsrvYAML), 0o644)
	}
	return nil
}

// saveSnapshot keeps the current configuration before an import.
func (s *Server) saveSnapshot(ctx context.Context, user string) (string, error) {
	e, err := s.buildExport(ctx, user)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.cfg.StateDir, 0o700); err != nil {
		return "", err
	}
	name := filepath.Join(s.cfg.StateDir, "antes-do-import-"+e.ExportedAt.Format("20060102-150405")+".json")
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		return "", err
	}
	old, _ := filepath.Glob(filepath.Join(s.cfg.StateDir, "antes-do-import-*.json"))
	sort.Strings(old)
	for len(old) > snapshotsKept {
		_ = os.Remove(old[0])
		old = old[1:]
	}
	return name, nil
}
