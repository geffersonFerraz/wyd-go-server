// Package wiretest checks wire struct layouts in tests.
package wiretest

import "reflect"

func alignOf(t reflect.Type) int {
	switch t.Kind() {
	case reflect.Array:
		return alignOf(t.Elem())
	case reflect.Struct:
		a := 1
		for i := range t.NumField() {
			if f := t.Field(i); f.Name != "_" {
				a = max(a, alignOf(f.Type))
			}
		}
		return a
	default:
		return int(t.Size())
	}
}

func sizeOf(t reflect.Type) int {
	switch t.Kind() {
	case reflect.Array:
		return t.Len() * sizeOf(t.Elem())
	case reflect.Struct:
		n := 0
		for i := range t.NumField() {
			n += sizeOf(t.Field(i).Type)
		}
		return n
	default:
		return int(t.Size())
	}
}

// Misaligned lists the fields MSVC would place elsewhere: every padding byte
// has to be spelled out as a blank field.
func Misaligned(t reflect.Type) []string {
	var out []string
	off := 0
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Name != "_" && off%alignOf(f.Type) != 0 {
			out = append(out, f.Name)
		}
		if f.Type.Kind() == reflect.Struct {
			for _, m := range Misaligned(f.Type) {
				out = append(out, f.Name+"."+m)
			}
		}
		off += sizeOf(f.Type)
	}
	if off%alignOf(t) != 0 {
		out = append(out, "(tail)")
	}
	return out
}
