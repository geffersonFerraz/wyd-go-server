// Package logfile writes the server logs to one file per day
// (<dir>/<name>-YYYY-MM-DD.log) besides the console, like the C++ servers
// kept Logs\Servidor_N\*.txt: closing the window or removing the container
// no longer loses the history of trades, sales, deliveries and GM commands.
package logfile

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Writer appends to the file of the current day, opening the next one at
// midnight and removing files older than Keep days (0 keeps them all).
type Writer struct {
	Dir, Name string
	Keep      int
	now       func() time.Time

	mu   sync.Mutex
	day  string
	file *os.File
}

// New creates the folder and opens today's file.
func New(dir, name string, keep int) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	w := &Writer{Dir: dir, Name: name, Keep: keep, now: time.Now}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotate(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) path(day string) string {
	return filepath.Join(w.Dir, w.Name+"-"+day+".log")
}

// rotate opens the file of the current day if it is not the open one.
func (w *Writer) rotate() error {
	day := w.now().Format("2006-01-02")
	if day == w.day && w.file != nil {
		return nil
	}
	f, err := os.OpenFile(w.path(day), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if w.file != nil {
		w.file.Close()
	}
	w.file, w.day = f, day
	w.prune()
	return nil
}

// prune removes this program's files older than Keep days.
func (w *Writer) prune() {
	if w.Keep <= 0 {
		return
	}
	limit := w.now().AddDate(0, 0, -w.Keep).Format("2006-01-02")
	files, _ := filepath.Glob(filepath.Join(w.Dir, w.Name+"-*.log"))
	for _, f := range files {
		day := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), w.Name+"-"), ".log")
		if _, err := time.Parse("2006-01-02", day); err == nil && day < limit {
			os.Remove(f)
		}
	}
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotate(); err != nil && w.file == nil {
		return 0, err
	}
	return w.file.Write(p)
}

// Close closes the open file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// Logger returns a text logger writing to the console and, when dir is not
// empty, to the daily files of name in dir. close flushes nothing (writes
// are unbuffered) but closes the file; call it before exiting.
func Logger(dir, name string, keep int, level slog.Level) (log *slog.Logger, close func(), err error) {
	var out io.Writer = os.Stdout
	close = func() {}
	if dir != "" {
		w, err := New(dir, name, keep)
		if err != nil {
			return nil, nil, fmt.Errorf("log folder: %w", err)
		}
		out = io.MultiWriter(os.Stdout, w)
		close = func() { w.Close() }
	}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})), close, nil
}
