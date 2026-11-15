// Package fetch holds the throttled HTTP client shared by the manual
// maintenance tools (cmd/fetch-gbif, cmd/fetch-wikidata). The build itself never
// uses it.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client wraps an HTTP client with a descriptive User-Agent and a politeness
// throttle. Public APIs require the User-Agent and rate-limit aggressive
// clients. A Client is for sequential use: it is not safe for concurrent calls.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	Interval  time.Duration                   // minimum spacing between requests; 0 disables the throttle
	Ctx       context.Context                 // cancels requests and waits
	Backoff   func(attempt int) time.Duration // nil means attempt seconds
	last      time.Time
}

// RateInterval converts a requests-per-second limit to a request spacing.
// A non-positive rate disables the throttle.
func RateInterval(qps float64) time.Duration {
	if qps <= 0 {
		return 0
	}
	return time.Duration(float64(time.Second) / qps)
}

// Get fetches a URL, throttling to the configured rate and retrying transient
// failures (a connection error, a body-read error on an otherwise-200 response,
// or a 429/5xx status) with a short backoff. A GET is idempotent, so a read
// error is safe to retry.
func (c *Client) Get(rawURL string) ([]byte, error) {
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if c.Interval > 0 {
			if wait := c.Interval - time.Since(c.last); wait > 0 {
				if err := c.sleep(wait); err != nil {
					return nil, err
				}
			}
		}
		c.last = time.Now()

		req, err := http.NewRequestWithContext(c.Ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if serr := c.sleep(c.backoff(attempt)); serr != nil {
				return nil, serr
			}
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if serr := c.sleep(c.backoff(attempt)); serr != nil {
				return nil, serr
			}
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			if serr := c.sleep(c.backoff(attempt)); serr != nil {
				return nil, serr
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("status %d", resp.StatusCode)
		}
		return body, nil
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxAttempts, lastErr)
}

// sleep waits for d or until the context is cancelled, so an interrupt (Ctrl+C)
// returns promptly instead of blocking for the full throttle or backoff delay.
func (c *Client) sleep(d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-c.Ctx.Done():
		return c.Ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func (c *Client) backoff(attempt int) time.Duration {
	if c.Backoff != nil {
		return c.Backoff(attempt)
	}
	return time.Duration(attempt) * time.Second
}
