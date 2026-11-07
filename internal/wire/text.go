package wire

import "golang.org/x/text/encoding/charmap"

// The game client and servers treat char[] as Windows-1252 bytes. These
// helpers convert between that and the UTF-8 strings stored in Postgres.

// GameString decodes a NUL-terminated Windows-1252 char array to UTF-8.
func GameString(b []byte) string {
	s := CString(b)
	out, err := charmap.Windows1252.NewDecoder().String(s)
	if err != nil {
		return s
	}
	return out
}

// PutGameString encodes s as Windows-1252 into dst (truncated, NUL
// terminated). Runes with no Windows-1252 mapping become '?'.
func PutGameString(dst []byte, s string) {
	buf := make([]byte, 0, len(s))
	for _, r := range s {
		b, ok := charmap.Windows1252.EncodeRune(r)
		if !ok {
			b = '?'
		}
		buf = append(buf, b)
	}
	PutCString(dst, string(buf))
}

// EncodeGame converts a UTF-8 string to Windows-1252 bytes (as a Go string),
// the form names have in game memory. Unmappable runes become '?'.
func EncodeGame(s string) string {
	buf := make([]byte, 0, len(s))
	for _, r := range s {
		b, ok := charmap.Windows1252.EncodeRune(r)
		if !ok {
			b = '?'
		}
		buf = append(buf, b)
	}
	return string(buf)
}

// DecodeGame converts Windows-1252 bytes (as a Go string) to UTF-8.
func DecodeGame(s string) string {
	out, err := charmap.Windows1252.NewDecoder().String(s)
	if err != nil {
		return s
	}
	return out
}
