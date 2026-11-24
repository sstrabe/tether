package tether

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// logRing keeps the last lines in memory (for the app's log view) and
// writes them to a file that is cut back to its newer half when it passes
// max bytes, so a log survives an extension restart for export.
type logRing struct {
	mu    sync.Mutex
	lines []string
	next  int
	full  bool
	f     *os.File
	path  string
	size  int64
	max   int64
}

const ringLines = 2000

func newLogRing(path string, max int64) (*logRing, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	fi, _ := f.Stat()
	r := &logRing{lines: make([]string, ringLines), f: f, path: path, max: max}
	if fi != nil {
		r.size = fi.Size()
	}
	return r, nil
}

func (r *logRing) logf(format string, args ...any) {
	line := time.Now().UTC().Format("2006-01-02T15:04:05.000Z ") + strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines[r.next] = line
	r.next = (r.next + 1) % len(r.lines)
	if r.next == 0 {
		r.full = true
	}
	if r.f == nil {
		return
	}
	n, _ := r.f.WriteString(line + "\n")
	r.size += int64(n)
	if r.size > r.max {
		r.truncateLocked()
	}
}

func (r *logRing) truncateLocked() {
	b, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	b = b[len(b)/2:]
	if i := strings.IndexByte(string(b), '\n'); i >= 0 {
		b = b[i+1:]
	}
	r.f.Close()
	if err := os.WriteFile(r.path, b, 0o600); err != nil {
		r.f = nil
		return
	}
	r.f, err = os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		r.f = nil
		return
	}
	r.size = int64(len(b))
}

func (r *logRing) tail(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []string
	if r.full {
		all = append(all, r.lines[r.next:]...)
	}
	all = append(all, r.lines[:r.next]...)
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

func (r *logRing) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
}
