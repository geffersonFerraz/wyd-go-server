package cluster

import (
	"bytes"
	"strings"
	"testing"
)

func TestServerNames(t *testing.T) {
	// Group 3 only: lines 1 and 2 stay "0" so line g-1 is group g.
	yaml := strings.Replace(sample, "group: 1", "group: 3\n    name: Hércules", 1) +
		strings.Replace(second, "group: 2", "group: 1\n    name: Atlas", 1)
	c, err := Load(write(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.ServerNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != ServerNamesSize {
		t.Fatalf("%d bytes, want %d", len(b), ServerNamesSize)
	}
	lines := bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n"))
	want := []string{"1 Atlas", "0", "3 H\xe9rcules", "0", "0", "0", "0", "0", "0", "0"}
	if len(lines) != len(want) {
		t.Fatalf("%d lines", len(lines))
	}
	for i, line := range lines {
		if len(line) != snLineSize-1 {
			t.Errorf("line %d: %d bytes", i, len(line))
		}
		// What the client's sscanf("%d %s") reads.
		if got := strings.Join(strings.Fields(string(line)), " "); got != want[i] {
			t.Errorf("line %d: %q, want %q", i, got, want[i])
		}
	}
}

func TestServerNameRejects(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, want string }{
		"space":     {"name: 'Dois Mundos'", "without spaces"},
		"too long":  {"name: Abcdefghijklmnop", "at most 15"},
		"not latin": {"name: Mundo世", "without spaces"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, strings.Replace(sample, "group: 1", "group: 1\n    "+tc.yaml, 1)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want %q", err, tc.want)
			}
		})
	}
	dup := strings.Replace(sample, "group: 1", "group: 1\n    name: Atlas", 1) +
		strings.Replace(second, "group: 2", "group: 2\n    name: atlas", 1)
	if _, err := Load(write(t, dup)); err == nil || !strings.Contains(err.Error(), "already the one of server 1") {
		t.Errorf("same name: %v", err)
	}
}
