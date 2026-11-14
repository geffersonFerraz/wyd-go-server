package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type stats struct {
	connecting, inWorld       atomic.Int64
	created, dropped, retries atomic.Int64
	moves, pktIn, pktOut      atomic.Uint64
	bytesIn                   atomic.Uint64
	handshake, login, enter   latencies

	mu       sync.Mutex
	failures map[string]int
}

func newStats() *stats { return &stats{failures: map[string]int{}} }

func (s *stats) fail(reason string) {
	s.mu.Lock()
	s.failures[reason]++
	s.mu.Unlock()
}

// latencies keeps every sample: a run has at most a few thousand logins.
type latencies struct {
	mu sync.Mutex
	d  []time.Duration
}

func (l *latencies) add(d time.Duration) {
	l.mu.Lock()
	l.d = append(l.d, d)
	l.mu.Unlock()
}

func (l *latencies) summary() string {
	l.mu.Lock()
	d := slices.Clone(l.d)
	l.mu.Unlock()
	if len(d) == 0 {
		return "-"
	}
	slices.Sort(d)
	pct := func(p float64) time.Duration { return d[min(len(d)-1, int(float64(len(d))*p))].Round(time.Millisecond) }
	return fmt.Sprintf("%d (p50 %v, p95 %v, máx %v)", len(d), pct(.5), pct(.95), d[len(d)-1].Round(time.Millisecond))
}

// reporter prints the counters every interval, with rates since the
// previous line.
type reporter struct {
	s                      *stats
	total                  int
	begin, last            time.Time
	lastIn, lastOut, lastB uint64
}

func (r *reporter) line() string {
	now := time.Now()
	in, out, b := r.s.pktIn.Load(), r.s.pktOut.Load(), r.s.bytesIn.Load()
	secs := now.Sub(r.last).Seconds()
	if secs <= 0 {
		secs = 1
	}
	line := fmt.Sprintf("[%s] no mundo %d/%d | conectando %d | quedas %d | falhas %d | rx %.0f pkt/s %.1f KB/s | tx %.0f pkt/s",
		now.Sub(r.begin).Round(time.Second), r.s.inWorld.Load(), r.total, r.s.connecting.Load(),
		r.s.dropped.Load(), r.failCount(),
		float64(in-r.lastIn)/secs, float64(b-r.lastB)/1024/secs, float64(out-r.lastOut)/secs)
	r.last, r.lastIn, r.lastOut, r.lastB = now, in, out, b
	if reason, n := r.topFailure(); n > 0 {
		line += fmt.Sprintf("\n    falha mais comum (%d): %s", n, reason)
	}
	return line
}

func (r *reporter) failCount() int {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	n := 0
	for _, c := range r.s.failures {
		n += c
	}
	return n
}

func (r *reporter) topFailure() (string, int) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	top, n := "", 0
	for k, c := range r.s.failures {
		if c > n || c == n && k < top {
			top, n = k, c
		}
	}
	return top, n
}

func (r *reporter) final() string {
	var sb strings.Builder
	s := r.s
	fmt.Fprintf(&sb, "\n=== resumo (%s) ===\n", time.Since(r.begin).Round(time.Second))
	fmt.Fprintf(&sb, "handshake:          %s\n", s.handshake.summary())
	fmt.Fprintf(&sb, "login → lista:      %s\n", s.login.summary())
	fmt.Fprintf(&sb, "conexão → mundo:    %s\n", s.enter.summary())
	fmt.Fprintf(&sb, "personagens criados %d | quedas no mundo %d | sessões repetidas %d | passos %d\n",
		s.created.Load(), s.dropped.Load(), s.retries.Load(), s.moves.Load())
	fmt.Fprintf(&sb, "pacotes recebidos %d (%.1f MB) | enviados %d\n",
		s.pktIn.Load(), float64(s.bytesIn.Load())/1024/1024, s.pktOut.Load())
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failures) > 0 {
		keys := make([]string, 0, len(s.failures))
		for k := range s.failures {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return s.failures[keys[i]] > s.failures[keys[j]] })
		sb.WriteString("falhas:\n")
		for _, k := range keys {
			fmt.Fprintf(&sb, "  %5d  %s\n", s.failures[k], k)
		}
	}
	return sb.String()
}
