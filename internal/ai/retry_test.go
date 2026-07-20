package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeRT is a scripted http.RoundTripper: it serves one function per attempt,
// so a test can dictate the exact (response, error) each retry sees without any
// network. Extra calls beyond the script are a test failure.
type fakeRT struct {
	responses []func(*http.Request) (*http.Response, error)
	calls     int
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.calls >= len(f.responses) {
		return nil, fmt.Errorf("fakeRT: unexpected call %d (only %d scripted)", f.calls+1, len(f.responses))
	}
	fn := f.responses[f.calls]
	f.calls++
	return fn(req)
}

// respFn returns an attempt function yielding a response with the given status
// and optional headers.
func respFn(status int, headers map[string]string) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) {
		h := http.Header{}
		for k, v := range headers {
			h.Set(k, v)
		}
		return &http.Response{
			StatusCode: status,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader("body")),
		}, nil
	}
}

// errFn returns an attempt function yielding a transport error.
func errFn(err error) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) { return nil, err }
}

// newTestTransport builds a retryTransport over base with a deterministic RNG
// and a recording sleep (no real waiting). The returned slice accumulates every
// delay the transport chose to sleep for.
func newTestTransport(base http.RoundTripper, cfg RetryConfig) (*retryTransport, *[]time.Duration) {
	t := newRetryTransport(base, cfg)
	t.rng = rand.New(rand.NewSource(1)) // fixed seed → stable jitter
	slept := &[]time.Duration{}
	t.sleep = func(ctx context.Context, d time.Duration) error {
		*slept = append(*slept, d)
		return ctx.Err() // honour an already-cancelled context
	}
	return t, slept
}

// postReq builds a rewindable POST request (GetBody set, as
// http.NewRequestWithContext does for a *bytes.Reader body).
func postReq(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://example.test/v1", bytes.NewReader([]byte(`{"q":1}`)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return req
}

func TestRetry_429RetryAfterDeltaSeconds(t *testing.T) {
	fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
		respFn(http.StatusTooManyRequests, map[string]string{"Retry-After": "1"}),
		respFn(http.StatusOK, nil),
	}}
	tr, slept := newTestTransport(fake, RetryConfig{})

	resp, err := tr.RoundTrip(postReq(t, context.Background()))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if fake.calls != 2 {
		t.Errorf("attempts = %d, want 2", fake.calls)
	}
	if len(*slept) != 1 || (*slept)[0] != time.Second {
		t.Errorf("slept = %v, want [1s] (Retry-After overrides computed backoff)", *slept)
	}
}

func TestRetry_429RetryAfterHTTPDate(t *testing.T) {
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	when := base.Add(2 * time.Second).Format(http.TimeFormat)
	fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
		respFn(http.StatusTooManyRequests, map[string]string{"Retry-After": when}),
		respFn(http.StatusOK, nil),
	}}
	tr, slept := newTestTransport(fake, RetryConfig{})
	tr.now = func() time.Time { return base } // fixed clock → deterministic delta

	resp, err := tr.RoundTrip(postReq(t, context.Background()))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if len(*slept) != 1 || (*slept)[0] != 2*time.Second {
		t.Errorf("slept = %v, want [2s] from HTTP-date Retry-After", *slept)
	}
}

func TestRetry_5xxComputedBackoff(t *testing.T) {
	fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
		respFn(http.StatusServiceUnavailable, nil),
		respFn(http.StatusOK, nil),
	}}
	cfg := RetryConfig{BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}
	tr, slept := newTestTransport(fake, cfg)

	resp, err := tr.RoundTrip(postReq(t, context.Background()))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if fake.calls != 2 {
		t.Errorf("attempts = %d, want 2", fake.calls)
	}
	// No Retry-After → full jitter over the first backoff step: [0, BaseDelay).
	if len(*slept) != 1 {
		t.Fatalf("slept = %v, want one delay", *slept)
	}
	if d := (*slept)[0]; d < 0 || d >= 500*time.Millisecond {
		t.Errorf("computed backoff = %v, want within [0, 500ms)", d)
	}
}

func TestRetry_SuccessAfterRetries(t *testing.T) {
	fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
		respFn(http.StatusInternalServerError, nil),
		respFn(http.StatusInternalServerError, nil),
		respFn(http.StatusOK, nil),
	}}
	tr, slept := newTestTransport(fake, RetryConfig{MaxAttempts: 4})

	resp, err := tr.RoundTrip(postReq(t, context.Background()))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if fake.calls != 3 {
		t.Errorf("attempts = %d, want 3", fake.calls)
	}
	if len(*slept) != 2 {
		t.Errorf("sleeps = %d, want 2 (one before each retry)", len(*slept))
	}
}

func TestRetry_TransientTransportError(t *testing.T) {
	fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
		errFn(errors.New("connection reset by peer")),
		respFn(http.StatusOK, nil),
	}}
	tr, _ := newTestTransport(fake, RetryConfig{})

	resp, err := tr.RoundTrip(postReq(t, context.Background()))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if fake.calls != 2 {
		t.Errorf("attempts = %d, want 2", fake.calls)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
		respFn(http.StatusServiceUnavailable, nil),
		respFn(http.StatusOK, nil), // must never be reached
	}}
	tr, _ := newTestTransport(fake, RetryConfig{})
	// Simulate the context being cancelled during the backoff sleep.
	tr.sleep = func(ctx context.Context, d time.Duration) error { return context.Canceled }

	_, err := tr.RoundTrip(postReq(t, context.Background()))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if fake.calls != 1 {
		t.Errorf("attempts = %d, want 1 (no further attempt after cancellation)", fake.calls)
	}
}

func TestInterruptibleSleep_Cancelled(t *testing.T) {
	tr := newRetryTransport(nil, RetryConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled → the select must pick ctx.Done, not the timer
	if err := tr.interruptibleSleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("interruptibleSleep err = %v, want context.Canceled", err)
	}
}

func TestRetry_NonRetryable4xx(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
				respFn(status, nil),
			}}
			tr, slept := newTestTransport(fake, RetryConfig{})

			resp, err := tr.RoundTrip(postReq(t, context.Background()))
			if err != nil {
				t.Fatalf("RoundTrip: %v", err)
			}
			if resp.StatusCode != status {
				t.Errorf("status = %d, want %d verbatim", resp.StatusCode, status)
			}
			if fake.calls != 1 {
				t.Errorf("attempts = %d, want 1 (4xx is not retried)", fake.calls)
			}
			if len(*slept) != 0 {
				t.Errorf("slept = %v, want none", *slept)
			}
		})
	}
}

func TestRetry_MaxAttemptsExhausted(t *testing.T) {
	fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
		respFn(http.StatusTooManyRequests, nil),
		respFn(http.StatusTooManyRequests, nil),
		respFn(http.StatusTooManyRequests, nil),
		respFn(http.StatusTooManyRequests, nil),
	}}
	tr, slept := newTestTransport(fake, RetryConfig{MaxAttempts: 4})

	resp, err := tr.RoundTrip(postReq(t, context.Background()))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want the real 429 surfaced (not a synthetic error)", resp.StatusCode)
	}
	if fake.calls != 4 {
		t.Errorf("attempts = %d, want 4", fake.calls)
	}
	if len(*slept) != 3 {
		t.Errorf("sleeps = %d, want 3 (no sleep after the final attempt)", len(*slept))
	}
}

func TestRetry_NonRewindableBodySingleAttempt(t *testing.T) {
	fake := &fakeRT{responses: []func(*http.Request) (*http.Response, error){
		respFn(http.StatusServiceUnavailable, nil),
		respFn(http.StatusOK, nil), // must never be reached
	}}
	tr, _ := newTestTransport(fake, RetryConfig{})

	req, err := http.NewRequestWithContext(context.Background(), "POST", "http://example.test", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Body = io.NopCloser(strings.NewReader("cannot rewind"))
	req.GetBody = nil // not replayable

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (single attempt, returned as-is)", resp.StatusCode)
	}
	if fake.calls != 1 {
		t.Errorf("attempts = %d, want 1 (non-rewindable body cannot retry)", fake.calls)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		header  string
		wantDur time.Duration
		wantOK  bool
	}{
		{"integer seconds", "5", 5 * time.Second, true},
		{"zero seconds", "0", 0, true},
		{"negative seconds", "-3", 0, false},
		{"http date future", now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second, true},
		{"http date past floors to zero", now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
		{"empty", "", 0, false},
		{"garbage", "soon", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDur, gotOK := parseRetryAfter(tt.header, now)
			if gotOK != tt.wantOK {
				t.Errorf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotDur != tt.wantDur {
				t.Errorf("dur = %v, want %v", gotDur, tt.wantDur)
			}
		})
	}
}

func TestRetryConfig_WithDefaults(t *testing.T) {
	got := RetryConfig{}.withDefaults()
	want := RetryConfig{MaxAttempts: defaultMaxAttempts, BaseDelay: defaultBaseDelay, MaxDelay: defaultMaxDelay}
	if got != want {
		t.Errorf("withDefaults() = %+v, want %+v", got, want)
	}
	// Explicit values are preserved.
	custom := RetryConfig{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: time.Minute}
	if custom.withDefaults() != custom {
		t.Errorf("withDefaults() overrode explicit values: %+v", custom.withDefaults())
	}
}

func TestBackoffStep_CapsAtMaxDelay(t *testing.T) {
	tr := newRetryTransport(nil, RetryConfig{BaseDelay: time.Second, MaxDelay: 5 * time.Second})
	// 2^3 = 8s exceeds the 5s cap.
	if got := tr.backoffStep(3); got != 5*time.Second {
		t.Errorf("backoffStep(3) = %v, want 5s (capped)", got)
	}
	if got := tr.backoffStep(0); got != time.Second {
		t.Errorf("backoffStep(0) = %v, want 1s", got)
	}
}
