// Package wire holds byte-exact Go mirrors of the C++ structs that DBSRV and
// TMSRV exchange. They were built with MSVC for Win32 (x86) with the default
// /Zp8 packing, so every implicit padding byte is spelled out here as a blank
// `_` field. encoding/binary skips blank fields on read and zeroes them on
// write, and binary.Size(T) must equal the C++ sizeof(T); sizes_test.go
// enforces that.
package wire

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

var le = binary.LittleEndian

// Marshal encodes v (a pointer to or value of a wire struct) to its exact
// C++ byte image.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(binary.Size(v))
	if err := binary.Write(&buf, le, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MustMarshal is Marshal for static wire structs, where an error is a bug.
func MustMarshal(v any) []byte {
	b, err := Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Unmarshal decodes b into v (a pointer). A shorter b is zero-extended:
// TMSRV's copies of some structs (Common/Struct.h) omit trailing fields that
// DBSRV's copies have (e.g. TypeBlock[10] on STRUCT_SAVECHARACTER), and the
// C++ server simply read past them. A longer b is fine; extra bytes are ignored.
func Unmarshal(b []byte, v any) error {
	need := binary.Size(v)
	if need < 0 {
		return fmt.Errorf("wire: %T is not a fixed-size struct", v)
	}
	if len(b) < need {
		padded := make([]byte, need)
		copy(padded, b)
		b = padded
	}
	return binary.Read(bytes.NewReader(b[:need]), le, v)
}

// CString returns the NUL-terminated prefix of a fixed char array.
func CString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// PutCString copies s into dst, truncating so the result always ends in NUL
// (strncpy_s semantics), and zeroes the rest.
func PutCString(dst []byte, s string) {
	clear(dst)
	if len(dst) == 0 {
		return
	}
	n := copy(dst[:len(dst)-1], s)
	dst[n] = 0
}
