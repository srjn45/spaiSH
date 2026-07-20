package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Retry policy defaults, applied by (RetryConfig).withDefaults() to any zero
// field so the config section (and callers) may omit values.
const (
	defaultMaxAttempts = 4
	defaultBaseDelay   = 500 * time.Millisecond
	defaultMaxDelay    = 30 * time.Second
)

// RetryConfig bounds the shared backoff policy used by every provider. Zero
// values fall back to the package defaults via (RetryConfig).withDefaults(), so
// a RetryConfig{} resolves to the defaults.
type RetryConfig struct {
	MaxAttempts int           // total attempts including the first; default 4
	BaseDelay   time.Duration // first backoff step; default 500ms
	MaxDelay    time.Duration // cap on any single backoff; default 30s
}

// withDefaults returns a copy of c with any non-positive field replaced by its
// package default.
func (c RetryConfig) withDefaults() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = defaultMaxAttempts
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = defaultBaseDelay
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = defaultMaxDelay
	}
	return c
}

// retryTransport wraps a base RoundTripper with bounded, jittered exponential
// backoff on HTTP 429 / 5xx responses and transient transport errors. The retry
// decision is made from response headers before any streamed body is read, so a
// request whose stream has already begun is never restarted.
type retryTransport struct {
	base http.RoundTripper // nil → http.DefaultTransport
	cfg  RetryConfig

	// now supplies the current time for Retry-After HTTP-date parsing; a field
	// so tests can inject a fixed clock. Defaults to time.Now.
	now func() time.Time
	// sleep waits d (interruptibly on ctx). A field so tests can record the
	// chosen delays without real waiting. Defaults to interruptibleSleep.
	sleep func(ctx context.Context, d time.Duration) error

	// rng backs full jitter; it is per-transport (never the global source) and
	// guarded by mu so backoff is deterministic under an injected seed in tests.
	mu  sync.Mutex
	rng *rand.Rand
}

// NewRetryClient returns an *http.Client whose Transport applies cfg's retry
// policy. Providers use this in place of &http.Client{}. base may be nil for
// http.DefaultTransport or a fake RoundTripper in tests.
func NewRetryClient(base http.RoundTripper, cfg RetryConfig) *http.Client {
	return &http.Client{Transport: newRetryTransport(base, cfg)}
}

// newRetryTransport builds a retryTransport with production defaults wired in.
func newRetryTransport(base http.RoundTripper, cfg RetryConfig) *retryTransport {
	t := &retryTransport{
		base: base,
		cfg:  cfg.withDefaults(),
		now:  time.Now,
		rng:  rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	t.sleep = t.interruptibleSleep
	return t
}

// RoundTrip issues req, retrying on transient failures per the configured
// policy. It returns the last real response/error rather than a synthetic one
// once attempts are exhausted, and abandons promptly on context cancellation.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	// A body we cannot rewind cannot be safely replayed, so make a single
	// attempt. http.NewRequestWithContext sets GetBody for the *bytes.Reader
	// bodies the providers use, so real completion POSTs are always rewindable.
	if req.Body != nil && req.GetBody == nil {
		return base.RoundTrip(req)
	}

	var (
		resp *http.Response
		err  error
	)
	for attempt := 0; attempt < t.cfg.MaxAttempts; attempt++ {
		if attempt > 0 && req.Body != nil {
			body, berr := req.GetBody()
			if berr != nil {
				return nil, fmt.Errorf("retry: rewind request body: %w", berr)
			}
			req.Body = body
		}

		resp, err = base.RoundTrip(req)
		if !t.shouldRetry(resp, err) {
			return resp, err
		}
		if attempt == t.cfg.MaxAttempts-1 {
			// Exhausted: surface the real 429/5xx (or transport error).
			return resp, err
		}

		delay := t.retryDelay(attempt, resp)
		if resp != nil {
			drainAndClose(resp.Body)
		}
		if serr := t.sleep(req.Context(), delay); serr != nil {
			return nil, serr
		}
	}
	return resp, err
}

// shouldRetry reports whether the outcome of an attempt warrants another one.
// Context cancellation/deadline is always terminal; other transport errors and
// 429 / 5xx responses are retried; every other status (including all other 4xx)
// is returned as-is.
func (t *retryTransport) shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return true
	}
	if resp == nil {
		return false
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode >= 500 && resp.StatusCode <= 599
}

// retryDelay computes the wait before the next attempt. A parseable Retry-After
// header overrides the computed backoff (capped at MaxDelay, no jitter);
// otherwise full jitter is applied to the exponential step.
func (t *retryTransport) retryDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if d, ok := parseRetryAfter(ra, t.now()); ok {
				if d > t.cfg.MaxDelay {
					d = t.cfg.MaxDelay
				}
				return d
			}
		}
	}
	return t.jitter(t.backoffStep(attempt))
}

// backoffStep returns min(MaxDelay, BaseDelay * 2^attempt), the pre-jitter
// backoff for a given zero-based attempt index.
func (t *retryTransport) backoffStep(attempt int) time.Duration {
	step := float64(t.cfg.BaseDelay) * math.Pow(2, float64(attempt))
	if step >= float64(t.cfg.MaxDelay) {
		return t.cfg.MaxDelay
	}
	return time.Duration(step)
}

// jitter returns a random duration in [0, step) — full jitter — spreading
// retries to avoid a synchronised thundering herd.
func (t *retryTransport) jitter(step time.Duration) time.Duration {
	if step <= 0 {
		return 0
	}
	t.mu.Lock()
	n := t.rng.Int63n(int64(step))
	t.mu.Unlock()
	return time.Duration(n)
}

// interruptibleSleep waits for d, returning early with the context error if ctx
// is cancelled first. A non-positive d returns immediately.
func (t *retryTransport) interruptibleSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// parseRetryAfter interprets a Retry-After header value per RFC 9110: either
// delta-seconds (an integer) or an HTTP-date. It returns the delay from now and
// true on success, or (0, false) when absent or unparseable. now is a parameter
// so the HTTP-date branch is deterministically testable.
func parseRetryAfter(h string, now time.Time) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(h); err == nil {
		d := when.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// drainAndClose reads a bounded amount of the response body and closes it so the
// underlying connection can be reused for the retry. Bodies larger than the cap
// simply aren't reused; error responses are small enough that this is rare.
func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4096))
	body.Close()
}
