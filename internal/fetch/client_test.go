package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientGet(t *testing.T) {
	tests := []struct {
		name     string
		statuses []int // status per request; the last repeats
		wantBody bool
		wantReqs int32
	}{
		{"ok", []int{200}, true, 1},
		{"retry 5xx then ok", []int{500, 500, 200}, true, 3},
		{"404 is not retried", []int{404}, false, 1},
		{"429 forever gives up after four", []int{429}, false, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var n atomic.Int32
			var ua string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				i := int(n.Add(1)) - 1
				ua = r.Header.Get("User-Agent")
				st := tc.statuses[min(i, len(tc.statuses)-1)]
				w.WriteHeader(st)
				if st == 200 {
					_, _ = w.Write([]byte("body"))
				}
			}))
			defer srv.Close()
			c := &Client{HTTP: srv.Client(), UserAgent: "test-agent/1", Ctx: context.Background(),
				Backoff: func(int) time.Duration { return 0 }}
			body, err := c.Get(srv.URL)
			if tc.wantBody {
				if err != nil || string(body) != "body" {
					t.Fatalf("Get = %q, %v", body, err)
				}
				if ua != "test-agent/1" {
					t.Errorf("User-Agent = %q", ua)
				}
			} else if err == nil {
				t.Fatalf("Get succeeded with %q", body)
			}
			if got := n.Load(); got != tc.wantReqs {
				t.Errorf("requests = %d, want %d", got, tc.wantReqs)
			}
		})
	}
}

func TestClientThrottle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Interval: 30 * time.Millisecond, Ctx: context.Background()}
	start := time.Now()
	for range 3 {
		if _, err := c.Get(srv.URL); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 60*time.Millisecond {
		t.Errorf("3 requests took %v, want at least 60ms", el)
	}
}
