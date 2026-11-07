package admin

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbsrv/internal/store"
)

// testMark is a 16x12 image with a distinct color per pixel.
func testMark() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, markW, markH))
	for y := range markH {
		for x := range markW {
			img.Set(x, y, color.RGBA{uint8(x * 15), uint8(y * 20), uint8(x + y), 255})
		}
	}
	return img
}

// clientAccepts mirrors TMFieldScene::Guildmark_IsCorrectBMP plus the
// header length the client copies into its 44-byte BITMAPINFO.
func clientAccepts(b []byte) bool {
	le := binary.LittleEndian
	if len(b) < 54 || le.Uint16(b) != 19778 {
		return false
	}
	if size := le.Uint32(b[2:]); size != 630 && size != 632 {
		return false
	}
	if off := le.Uint32(b[10:]); off-14 > 44 {
		return false
	}
	return le.Uint32(b[18:]) == 16 && le.Uint32(b[22:]) == 12 && le.Uint16(b[28:]) == 24
}

func samePixels(t *testing.T, a, b image.Image) {
	t.Helper()
	for y := range markH {
		for x := range markW {
			r1, g1, b1, _ := a.At(x, y).RGBA()
			r2, g2, b2, _ := b.At(x, y).RGBA()
			if r1>>8 != r2>>8 || g1>>8 != g2>>8 || b1>>8 != b2>>8 {
				t.Fatalf("pixel %d,%d: %v != %v", x, y, a.At(x, y), b.At(x, y))
			}
		}
	}
}

func TestGuildMarkCanonical(t *testing.T) {
	src := testMark()
	out := encodeGuildMark(src)
	if len(out) != 630 || !clientAccepts(out) {
		t.Fatalf("client would reject the canonical mark (%d bytes)", len(out))
	}
	back, err := decodeBMP(out)
	if err != nil {
		t.Fatal(err)
	}
	samePixels(t, src, back)
	// Re-encoding a canonical mark is a no-op.
	again, err := decodeGuildMark(out)
	if err != nil || !bytes.Equal(again, out) {
		t.Fatalf("re-encode changed the file: %v", err)
	}
}

// bmp builds an uncompressed BMP for the decoder tests.
func bmp(img image.Image, bpp int, topDown bool) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	stride := (w*bpp/8 + 3) &^ 3
	pal := 0
	if bpp == 8 {
		pal = 256 * 4
	}
	off := 54 + pal
	b := make([]byte, off+stride*h)
	le := binary.LittleEndian
	copy(b, "BM")
	le.PutUint32(b[2:], uint32(len(b)))
	le.PutUint32(b[10:], uint32(off))
	le.PutUint32(b[14:], 40)
	le.PutUint32(b[18:], uint32(w))
	hh := int32(h)
	if topDown {
		hh = -hh
	}
	le.PutUint32(b[22:], uint32(hh))
	le.PutUint16(b[26:], 1)
	le.PutUint16(b[28:], uint16(bpp))
	index := map[[3]byte]byte{}
	for y := range h {
		row := y
		if !topDown {
			row = h - 1 - y
		}
		p := b[off+row*stride:]
		for x := range w {
			r, g, bl, _ := img.At(x, y).RGBA()
			c := [3]byte{byte(r >> 8), byte(g >> 8), byte(bl >> 8)}
			switch bpp {
			case 8:
				i, ok := index[c]
				if !ok {
					i = byte(len(index))
					index[c] = i
					q := b[54+int(i)*4:]
					q[0], q[1], q[2] = c[2], c[1], c[0]
				}
				p[x] = i
			default:
				q := p[x*bpp/8:]
				q[0], q[1], q[2] = c[2], c[1], c[0]
			}
		}
	}
	return b
}

func TestGuildMarkDecodeFormats(t *testing.T) {
	src := testMark()
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, src)
	cases := map[string][]byte{
		"bmp24":         bmp(src, 24, false),
		"bmp24 topdown": bmp(src, 24, true),
		"bmp32":         bmp(src, 32, false),
		"png":           pngBuf.Bytes(),
	}
	for name, data := range cases {
		out, err := decodeGuildMark(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !clientAccepts(out) {
			t.Fatalf("%s: client would reject the result", name)
		}
		back, _ := decodeBMP(out)
		samePixels(t, src, back)
	}

	pal := image.NewRGBA(image.Rect(0, 0, markW, markH))
	for y := range markH {
		for x := range markW {
			pal.Set(x, y, color.RGBA{uint8((x % 4) * 60), 0, uint8(y * 20), 255})
		}
	}
	out, err := decodeGuildMark(bmp(pal, 8, false))
	if err != nil {
		t.Fatalf("bmp8: %v", err)
	}
	back, _ := decodeBMP(out)
	samePixels(t, pal, back)
}

func TestGuildMarkRejects(t *testing.T) {
	big := image.NewRGBA(image.Rect(0, 0, 32, 24))
	trunc := bmp(testMark(), 24, false)[:300]
	hugeOff := bmp(testMark(), 24, false)
	binary.LittleEndian.PutUint32(hugeOff[10:], 1<<30)
	for name, data := range map[string][]byte{
		"wrong size": bmp(big, 24, false),
		"truncated":  trunc,
		"bad offset": hugeOff,
		"not image":  []byte("<?php echo 1; ?>"),
		"jpeg":       {0xFF, 0xD8, 0xFF, 0xE0},
		"empty":      nil,
		"bmp header": []byte("BM"),
	} {
		if _, err := decodeGuildMark(data); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestGuildMarkPublicHandler(t *testing.T) {
	dir := t.TempDir()
	mark := encodeGuildMark(testMark())
	if err := os.WriteFile(filepath.Join(dir, guildMarkFile(12)), mark, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("no"), 0o644)
	ts := httptest.NewServer(GuildMarkHandler(dir))
	defer ts.Close()

	// The client's name for guild 12 is b010000012.bmp (not b01000012.bmp,
	// the PHP site's name, which the client never asked for).
	if guildMarkFile(12) != "b010000012.bmp" {
		t.Fatalf("file name %s", guildMarkFile(12))
	}
	get := func(path string) (int, []byte) {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	if code, body := get(GuildMarkPath + "b010000012.bmp"); code != 200 || !bytes.Equal(body, mark) {
		t.Fatalf("mark: %d", code)
	}
	for _, p := range []string{GuildMarkPath + "secret.txt", GuildMarkPath + "b010000099.bmp", GuildMarkPath,
		GuildMarkPath + "..%2fsecret.txt", "/secret.txt"} {
		if code, _ := get(p); code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, code)
		}
	}
}

// ---- panel (needs DBSRV_TEST_DSN) ----

func (h *panelHarness) upload(path, csrf string, file []byte) (int, string, http.Header) {
	h.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf", csrf)
	fw, _ := mw.CreateFormFile("mark", "mark.bmp")
	_, _ = fw.Write(file)
	_ = mw.Close()
	resp, err := h.c.Post(h.ts.URL+path, mw.FormDataContentType(), &body)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func TestPanelGuildMarkUpload(t *testing.T) {
	h := newPanel(t)
	dir := t.TempDir()
	h.panel.cfg.GuildMarkDir = dir
	if err := h.st.UpsertGuild(h.ctx, store.Guild{ID: 7, Name: "Lobos", Kingdom: 7}); err != nil {
		t.Fatal(err)
	}
	csrf := h.login()

	if code, body, _ := h.get("/guilds"); code != 200 || !strings.Contains(body, "Lobos") {
		t.Fatalf("list: %d", code)
	}
	// Without the CSRF token nothing is saved.
	if code, _, _ := h.upload("/guilds/7/mark", "wrong", bmp(testMark(), 24, false)); code != http.StatusForbidden {
		t.Fatalf("no csrf: %d", code)
	}
	// Wrong size is refused with a message.
	big := image.NewRGBA(image.Rect(0, 0, 32, 32))
	if code, body, _ := h.upload("/guilds/7/mark", csrf, bmp(big, 24, false)); code != http.StatusUnprocessableEntity || !strings.Contains(body, "16x12") {
		t.Fatalf("big: %d", code)
	}
	// Unknown guild.
	if code, _, _ := h.upload("/guilds/8/mark", csrf, bmp(testMark(), 24, false)); code != http.StatusNotFound {
		t.Fatalf("unknown guild: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, guildMarkFile(7))); !os.IsNotExist(err) {
		t.Fatal("a refused upload wrote a file")
	}

	code, _, hdr := h.upload("/guilds/7/mark", csrf, bmp(testMark(), 32, false))
	if code != http.StatusSeeOther || !strings.HasPrefix(hdr.Get("Location"), "/guilds?id=7&saved=1") {
		t.Fatalf("upload: %d %s", code, hdr.Get("Location"))
	}
	saved, err := os.ReadFile(filepath.Join(dir, "b01000007.bmp"))
	if err != nil || !clientAccepts(saved) {
		t.Fatalf("saved mark: %v", err)
	}
	if code, body, _ := h.get("/guilds/7/mark.bmp"); code != 200 || body != string(saved) {
		t.Fatalf("preview: %d", code)
	}

	if code, _, _ := h.post("/guilds/7/mark/delete", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "b01000007.bmp")); !os.IsNotExist(err) {
		t.Fatal("mark not removed")
	}
}
