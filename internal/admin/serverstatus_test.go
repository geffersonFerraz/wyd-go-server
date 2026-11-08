package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

type fakeStatus struct {
	online []int
	age    time.Duration
	err    error
}

func (f fakeStatus) ChannelStatus(context.Context) ([]int, time.Duration, error) {
	return f.online, f.age, f.err
}

// parseLikeClient mimics the client's sscanf("%d\\n%d\\n..."): numbers
// separated by a literal backslash-n, stopping at the first mismatch.
func parseLikeClient(s string) []int {
	out := make([]int, 11)
	for i := range out {
		out[i] = -1
	}
	for i := 0; i < 11; i++ {
		w := 0
		if w < len(s) && (s[w] == '-' || s[w] == '+') {
			w++
		}
		for w < len(s) && s[w] >= '0' && s[w] <= '9' {
			w++
		}
		n, err := strconv.Atoi(s[:w])
		if err != nil {
			break
		}
		out[i] = n
		s = s[w:]
		if len(s) < 2 || s[:2] != `\n` {
			break
		}
		s = s[2:]
	}
	return out
}

func TestServerStatus(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cases := []struct {
		name string
		src  fakeStatus
		want []int
	}{
		{"up", fakeStatus{online: []int{5, 0, -1, -1, -1, -1, -1, -1, -1, -1}, age: time.Second},
			[]int{5, 5, 0, -1, -1, -1, -1, -1, -1, -1, -1}},
		{"stale", fakeStatus{online: []int{5, 0}, age: time.Minute},
			[]int{0, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}},
		{"never", fakeStatus{age: -1}, []int{0, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}},
		{"db error", fakeStatus{err: errors.New("down")}, []int{0, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := httptest.NewServer(serverStatusHandler(c.src, log))
			defer ts.Close()
			resp, err := http.Get(ts.URL + "/serv00.htm")
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			got := parseLikeClient(string(b))
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("body %q parsed as %v, want %v", b, got, c.want)
			}
		})
	}

	ts := httptest.NewServer(serverStatusHandler(fakeStatus{}, log))
	defer ts.Close()
	for _, p := range []string{"/", "/serv0.htm", "/x.htm", "/serv00.htm/x", "/accounts"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, resp.StatusCode)
		}
	}
}
