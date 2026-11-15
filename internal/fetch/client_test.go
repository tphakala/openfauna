package fetch

import (
	"context"
	"errors"
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

func TestClientGetRetriesBrokenResponses(t *testing.T) {
	tests := []struct {
		name string
		fail func(w http.ResponseWriter)
	}{
		{"dropped connection", func(w http.ResponseWriter) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}},
		{"truncated body", func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("short"))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var n atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if n.Add(1) == 1 {
					tc.fail(w)
					return
				}
				_, _ = w.Write([]byte("body"))
			}))
			defer srv.Close()
			c := &Client{HTTP: srv.Client(), Ctx: context.Background(), Backoff: func(int) time.Duration { return 0 }}
			body, err := c.Get(srv.URL)
			if err != nil || string(body) != "body" {
				t.Fatalf("Get = %q, %v", body, err)
			}
			if got := n.Load(); got != 2 {
				t.Errorf("requests = %d, want 2", got)
			}
		})
	}
}

func TestClientGetCancelEndsBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var waits atomic.Int32
	c := &Client{HTTP: srv.Client(), Ctx: ctx, Backoff: func(int) time.Duration {
		// The first backoff is long; cancelling must end it without waiting it out.
		if waits.Add(1) == 1 {
			time.AfterFunc(10*time.Millisecond, cancel)
		}
		return time.Hour
	}}
	if _, err := c.Get(srv.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestClientGetTruncatedTerminalStatusIsFinal(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), UserAgent: "test-agent/1", Ctx: context.Background(),
		Backoff: func(int) time.Duration { return 0 }}
	_, err := c.Get(srv.URL)
	if err == nil || err.Error() != "status 404" {
		t.Fatalf("Get error = %v, want status 404", err)
	}
	if got := n.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}
}
