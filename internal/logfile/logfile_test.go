package logfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDailyFilesAndPrune(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "tmsrv-2026-01-01.log")
	other := filepath.Join(dir, "dbsrv-2026-01-01.log")
	for _, f := range []string{old, other} {
		if err := os.WriteFile(f, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 8, 23, 59, 0, 0, time.Local)
	w := &Writer{Dir: dir, Name: "tmsrv", Keep: 30, now: func() time.Time { return now }}
	if _, err := w.Write([]byte("a\n")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := w.Write([]byte("b\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	for file, want := range map[string]string{"tmsrv-2026-11-08.log": "a\n", "tmsrv-2026-11-09.log": "b\n"} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || string(b) != want {
			t.Fatalf("%s = %q, %v", file, b, err)
		}
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old file of the program not removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("file of another program removed")
	}
}
