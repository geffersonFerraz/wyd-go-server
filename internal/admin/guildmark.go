package admin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Guild marks (the 16x12 icon over a guild member's head).
//
// The client downloads them itself, over HTTP, from GameURLMark
// ("http://<host>/guild/img_guilds/", TMPaths.h) + "b0100000<guild id>.bmp"
// (TMFieldScene::Guildmark_MakeFileName). It only shows a file that passes
// Guildmark_IsCorrectBMP: "BM", bfSize 630 or 632, 16x12, 24 bits. It also
// memcpys bfOffBits-14 header bytes into a 44-byte BITMAPINFO on the stack,
// so a crafted header could overflow it: marks are always re-encoded here
// into the one canonical 630-byte layout.
//
// The panel writes the files to Config.GuildMarkDir; GuildMarkHandler
// serves that directory read-only at the URL the client uses (run it on a
// public listener, the panel itself stays private).

const (
	markW, markH   = 16, 12
	markHeaderSize = 54
	markRowSize    = markW * 3 // 48, already a multiple of 4
	markFileSize   = markHeaderSize + markRowSize*markH
	markMaxUpload  = 64 << 10
	// The client keeps only the low 12 bits of the guild index
	// (usGuild & 0xFFF), so higher ids show another guild's mark.
	markMaxClientGuild = 0xFFF
)

// GuildMarkPath is the URL path prefix the client downloads marks from.
const GuildMarkPath = "/guild/img_guilds/"

var markFileRe = regexp.MustCompile(`^b0100000([1-9][0-9]{0,4})\.bmp$`)

// guildMarkFile is the client's file name for a guild's mark.
func guildMarkFile(guild int) string { return "b0100000" + strconv.Itoa(guild) + ".bmp" }

// decodeGuildMark reads an uploaded mark (BMP of 8, 24 or 32 bits, or PNG)
// that must be exactly 16x12 and returns the canonical client BMP.
func decodeGuildMark(data []byte) ([]byte, error) {
	var img image.Image
	switch {
	case bytes.HasPrefix(data, []byte("BM")):
		var err error
		if img, err = decodeBMP(data); err != nil {
			return nil, err
		}
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		var err error
		if img, err = png.Decode(bytes.NewReader(data)); err != nil {
			return nil, errors.New("PNG inválido")
		}
	default:
		return nil, errors.New("envie um arquivo BMP ou PNG")
	}
	if b := img.Bounds(); b.Dx() != markW || b.Dy() != markH {
		return nil, fmt.Errorf("a imagem deve ter exatamente %dx%d pixels (esta tem %dx%d)", markW, markH, b.Dx(), b.Dy())
	}
	return encodeGuildMark(img), nil
}

// encodeGuildMark writes img (16x12) as the 630-byte 24-bit bottom-up BMP
// the client expects. Transparent pixels become black.
func encodeGuildMark(img image.Image) []byte {
	out := make([]byte, markFileSize)
	le := binary.LittleEndian
	copy(out, "BM")
	le.PutUint32(out[2:], markFileSize)
	le.PutUint32(out[10:], markHeaderSize)
	le.PutUint32(out[14:], 40)
	le.PutUint32(out[18:], markW)
	le.PutUint32(out[22:], markH)
	le.PutUint16(out[26:], 1)
	le.PutUint16(out[28:], 24)
	le.PutUint32(out[34:], markRowSize*markH)
	le.PutUint32(out[38:], 2835) // 72 dpi
	le.PutUint32(out[42:], 2835)
	b := img.Bounds()
	for y := range markH {
		row := out[markHeaderSize+(markH-1-y)*markRowSize:]
		for x := range markW {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA() // premultiplied
			row[x*3], row[x*3+1], row[x*3+2] = byte(bl>>8), byte(g>>8), byte(r>>8)
		}
	}
	return out
}

// decodeBMP handles the uncompressed BMPs image editors save: 8 bits with a
// palette, 24 bits, and 32 bits (BI_RGB or BI_BITFIELDS with BGRA masks),
// bottom-up or top-down.
func decodeBMP(data []byte) (image.Image, error) {
	bad := errors.New("BMP inválido ou corrompido")
	if len(data) < 54 {
		return nil, bad
	}
	le := binary.LittleEndian
	off := int(le.Uint32(data[10:]))
	hdr := int(le.Uint32(data[14:]))
	if hdr < 40 || 14+hdr > len(data) || off > len(data) || off < 14+hdr {
		return nil, bad
	}
	w := int(int32(le.Uint32(data[18:])))
	h := int(int32(le.Uint32(data[22:])))
	bpp := int(le.Uint16(data[28:]))
	comp := le.Uint32(data[30:])
	topDown := h < 0
	if topDown {
		h = -h
	}
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 {
		return nil, bad
	}
	if w != markW || h != markH {
		return nil, fmt.Errorf("a imagem deve ter exatamente %dx%d pixels (esta tem %dx%d)", markW, markH, w, h)
	}
	var palette [][3]byte
	switch {
	case bpp == 24 && comp == 0, bpp == 32 && comp == 0:
	case bpp == 32 && comp == 3:
		if len(data) < 66 || le.Uint32(data[54:]) != 0xFF0000 || le.Uint32(data[58:]) != 0xFF00 || le.Uint32(data[62:]) != 0xFF {
			return nil, errors.New("BMP de 32 bits com máscara de cores não suportada; salve em 24 bits")
		}
	case bpp == 8 && comp == 0:
		n := int(le.Uint32(data[46:]))
		if n == 0 {
			n = 256
		}
		if n > 256 || 14+hdr+n*4 > off {
			return nil, bad
		}
		for i := range n {
			p := data[14+hdr+i*4:]
			palette = append(palette, [3]byte{p[2], p[1], p[0]})
		}
	default:
		return nil, fmt.Errorf("BMP de %d bits (compressão %d) não suportado; salve em 24 bits", bpp, comp)
	}
	stride := (w*bpp/8 + 3) &^ 3
	if off+stride*h > len(data) {
		return nil, bad
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		src := y
		if !topDown {
			src = h - 1 - y
		}
		row := data[off+src*stride:]
		for x := range w {
			var r, g, b byte
			switch bpp {
			case 8:
				i := int(row[x])
				if i >= len(palette) {
					return nil, bad
				}
				r, g, b = palette[i][0], palette[i][1], palette[i][2]
			default:
				p := row[x*bpp/8:]
				b, g, r = p[0], p[1], p[2]
			}
			o := img.PixOffset(x, y)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = r, g, b, 0xFF
		}
	}
	return img, nil
}

// writeFileAtomic writes via a temp file + rename, so the public handler
// never serves half a file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mark-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// GuildMarkHandler serves dir read-only at GuildMarkPath for the game
// client. Only canonical mark names are served (no listing, no traversal).
func GuildMarkHandler(dir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+GuildMarkPath+"{file}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("file")
		if !markFileRe.MatchString(name) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/bmp")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.ServeContent(w, r, name, st.ModTime(), f)
	})
	return mux
}

// ---- panel pages ----

func (s *Server) guildRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /guilds", s.auth(s.guildsPage))
	mux.HandleFunc("GET /guilds/{id}/mark.bmp", s.auth(s.guildMarkImage))
	mux.HandleFunc("POST /guilds/{id}/mark", limitBody(markMaxUpload+8<<10, s.auth(s.uploadGuildMark)))
	mux.HandleFunc("POST /guilds/{id}/mark/delete", s.auth(s.deleteGuildMark))
	mux.HandleFunc("POST /cities/reset", s.auth(s.resetCities))
	mux.HandleFunc("POST /cities/{city}", s.auth(s.setCityOwner))
}

// limitBody rejects big uploads before the multipart form is parsed (the
// CSRF check in auth parses it).
func limitBody(max int64, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > max {
			http.Error(w, "arquivo grande demais", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next(w, r)
	}
}

type guildRow struct {
	ID      int
	Name    string
	Kingdom string
	Fame    int
	HasMark bool
	Version int64 // mark mtime, busts the preview cache
}

func kingdomLabel(k int) string {
	switch k {
	case 7:
		return "Azul"
	case 8:
		return "Vermelho"
	}
	return "Neutro"
}

func (s *Server) guildsPage(w http.ResponseWriter, r *http.Request) {
	s.renderGuilds(w, r, http.StatusOK, "", "")
}

func (s *Server) renderGuilds(w http.ResponseWriter, r *http.Request, code int, msg, errMsg string) {
	list, err := s.st.Guilds(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	marks := map[string]int64{}
	if dir := s.cfg.GuildMarkDir; dir != "" {
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			s.fail(w, err)
			return
		}
		for _, e := range entries {
			if info, err := e.Info(); err == nil && markFileRe.MatchString(e.Name()) {
				marks[e.Name()] = info.ModTime().Unix()
			}
		}
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	sel, _ := strconv.Atoi(r.URL.Query().Get("id"))
	if v := r.PathValue("id"); v != "" {
		sel, _ = strconv.Atoi(v)
	}
	var rows []guildRow
	var picked *guildRow
	withMark := 0
	for _, g := range list {
		v, has := marks[guildMarkFile(g.ID)]
		if has {
			withMark++
		}
		row := guildRow{ID: g.ID, Name: g.Name, Kingdom: kingdomLabel(g.Kingdom), Fame: g.Fame, HasMark: has, Version: v}
		if g.ID == sel {
			p := row
			picked = &p
		}
		if q != "" && !strings.Contains(strings.ToLower(g.Name), q) && strconv.Itoa(g.ID) != q {
			continue
		}
		rows = append(rows, row)
	}
	data := map[string]any{"Title": "Guildas", "Guilds": rows, "Total": len(list), "WithMark": withMark,
		"Q": r.URL.Query().Get("q"), "Picked": picked, "Enabled": s.cfg.GuildMarkDir != "",
		"MaxClientGuild": markMaxClientGuild, "Msg": msg, "Error": errMsg}
	cities, err := s.cityRows(r, list)
	if err != nil {
		s.fail(w, err)
		return
	}
	data["Cities"] = cities
	if picked != nil {
		data["FileName"] = guildMarkFile(picked.ID)
	}
	if r.URL.Query().Has("saved") {
		data["Msg"] = "Ícone salvo. Jogadores que já viram o ícone antigo só veem o novo depois de reabrir o jogo."
	} else if r.URL.Query().Has("removed") {
		data["Msg"] = "Ícone removido."
	}
	s.page(w, r, code, "guilds", data)
}

// guildID reads {id} and checks the guild exists.
func (s *Server) guildID(r *http.Request) (int, bool, error) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 || id >= 65535 {
		return 0, false, nil
	}
	list, err := s.st.Guilds(r.Context())
	if err != nil {
		return 0, false, err
	}
	for _, g := range list {
		if g.ID == id {
			return id, true, nil
		}
	}
	return id, false, nil
}

func (s *Server) guildMarkImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 || s.cfg.GuildMarkDir == "" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(s.cfg.GuildMarkDir, guildMarkFile(id)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/bmp")
	_, _ = w.Write(data)
}

func (s *Server) uploadGuildMark(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GuildMarkDir == "" {
		s.renderGuilds(w, r, http.StatusServiceUnavailable, "", "Upload desativado: inicie o painel com -guildmark-dir.")
		return
	}
	id, ok, err := s.guildID(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, _, err := r.FormFile("mark")
	if err != nil {
		s.renderGuilds(w, r, http.StatusUnprocessableEntity, "", "Escolha um arquivo.")
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, markMaxUpload+1))
	if err != nil || len(raw) > markMaxUpload {
		s.renderGuilds(w, r, http.StatusUnprocessableEntity, "", "Arquivo grande demais.")
		return
	}
	bmp, err := decodeGuildMark(raw)
	if err != nil {
		s.renderGuilds(w, r, http.StatusUnprocessableEntity, "", "Ícone recusado: "+err.Error()+".")
		return
	}
	if err := os.MkdirAll(s.cfg.GuildMarkDir, 0o755); err != nil {
		s.fail(w, err)
		return
	}
	if err := writeFileAtomic(filepath.Join(s.cfg.GuildMarkDir, guildMarkFile(id)), bmp); err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("guild mark saved", "admin", sessionOf(r).user, "guild", id, "file", guildMarkFile(id))
	http.Redirect(w, r, "/guilds?id="+strconv.Itoa(id)+"&saved=1&t="+strconv.FormatInt(time.Now().Unix(), 10), http.StatusSeeOther)
}

func (s *Server) deleteGuildMark(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 || s.cfg.GuildMarkDir == "" {
		http.NotFound(w, r)
		return
	}
	err = os.Remove(filepath.Join(s.cfg.GuildMarkDir, guildMarkFile(id)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.fail(w, err)
		return
	}
	s.log.Info("guild mark removed", "admin", sessionOf(r).user, "guild", id)
	http.Redirect(w, r, "/guilds?id="+strconv.Itoa(id)+"&removed=1", http.StatusSeeOther)
}
