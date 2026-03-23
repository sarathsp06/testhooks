package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newTestForwarder(cfg Config) *Forwarder {
	cfg.DisableSSRFProtection = true
	log := zerolog.Nop()
	return New(cfg, log)
}

func TestForward_SingleTargetSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.Header.Get("X-Forwarded-By") != "testhooks" {
			t.Error("missing X-Forwarded-By header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fwd := newTestForwarder(Config{
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	})

	results := fwd.Forward(context.Background(), Request{
		Method:      "POST",
		Path:        "/webhook",
		Headers:     map[string][]string{"Content-Type": {"application/json"}},
		Body:        []byte(`{"test":true}`),
		ContentType: "application/json",
	}, []string{server.URL})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if !results[0].OK {
		t.Errorf("OK = false, want true. Error: %s", results[0].Error)
	}
	if results[0].StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", results[0].StatusCode)
	}
	if results[0].Latency <= 0 {
		t.Error("Latency should be > 0")
	}
}

func TestForward_MultipleTargets(t *testing.T) {
	var callCount atomic.Int32
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server2.Close()

	fwd := newTestForwarder(Config{Timeout: 5 * time.Second, MaxRetries: 0})
	results := fwd.Forward(context.Background(), Request{Method: "POST"}, []string{server1.URL, server2.URL})

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if callCount.Load() != 2 {
		t.Errorf("call count = %d, want 2", callCount.Load())
	}
	if !results[0].OK {
		t.Errorf("results[0].OK = false")
	}
	if !results[1].OK {
		t.Errorf("results[1].OK = false")
	}
}

func TestForward_EmptyTargets(t *testing.T) {
	fwd := newTestForwarder(Config{Timeout: 5 * time.Second})
	results := fwd.Forward(context.Background(), Request{Method: "GET"}, nil)
	if results != nil {
		t.Errorf("results = %v, want nil", results)
	}
}

func TestForward_TargetReturns500_WithRetry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fwd := newTestForwarder(Config{
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   50 * time.Millisecond,
	})

	results := fwd.Forward(context.Background(), Request{Method: "POST"}, []string{server.URL})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if !results[0].OK {
		t.Errorf("OK = false after retries, want true. Error: %s", results[0].Error)
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3", attempts.Load())
	}
}

func TestForward_TargetReturns400_NoRetry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	fwd := newTestForwarder(Config{
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   50 * time.Millisecond,
	})

	results := fwd.Forward(context.Background(), Request{Method: "POST"}, []string{server.URL})

	if results[0].OK {
		t.Error("OK = true for 400 response")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (4xx should not retry)", attempts.Load())
	}
}

func TestForward_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	fwd := newTestForwarder(Config{Timeout: 10 * time.Second, MaxRetries: 0})
	results := fwd.Forward(ctx, Request{Method: "POST"}, []string{server.URL})

	if results[0].OK {
		t.Error("OK = true for cancelled request")
	}
	if results[0].Error == "" {
		t.Error("Error should be set for cancelled request")
	}
}

func TestForward_HopByHopHeadersFiltered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Connection") != "" {
			t.Error("Connection header should be filtered")
		}
		if r.Header.Get("Keep-Alive") != "" {
			t.Error("Keep-Alive header should be filtered")
		}
		if r.Header.Get("X-Custom") != "value" {
			t.Error("custom headers should be preserved")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fwd := newTestForwarder(Config{Timeout: 5 * time.Second, MaxRetries: 0})
	fwd.Forward(context.Background(), Request{
		Method: "POST",
		Headers: map[string][]string{
			"Connection": {"keep-alive"},
			"Keep-Alive": {"timeout=5"},
			"X-Custom":   {"value"},
		},
	}, []string{server.URL})
}

func TestForward_ContentTypeOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "application/xml" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/xml")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fwd := newTestForwarder(Config{Timeout: 5 * time.Second, MaxRetries: 0})
	fwd.Forward(context.Background(), Request{
		Method:      "POST",
		Headers:     map[string][]string{"Content-Type": {"text/plain"}},
		Body:        []byte("<xml/>"),
		ContentType: "application/xml",
	}, []string{server.URL})
}

func TestForward_InvalidURL(t *testing.T) {
	fwd := newTestForwarder(Config{Timeout: 5 * time.Second, MaxRetries: 0})
	results := fwd.Forward(context.Background(), Request{Method: "POST"}, []string{"://invalid"})

	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].OK {
		t.Error("OK = true for invalid URL")
	}
	if results[0].Error == "" {
		t.Error("Error should be set for invalid URL")
	}
}

func TestBackoff(t *testing.T) {
	fwd := newTestForwarder(Config{
		BaseDelay: 100 * time.Millisecond,
		MaxDelay:  5 * time.Second,
	})

	d1 := fwd.backoff(1)
	if d1 != 100*time.Millisecond {
		t.Errorf("backoff(1) = %v, want 100ms", d1)
	}

	d2 := fwd.backoff(2)
	if d2 != 200*time.Millisecond {
		t.Errorf("backoff(2) = %v, want 200ms", d2)
	}

	d3 := fwd.backoff(3)
	if d3 != 400*time.Millisecond {
		t.Errorf("backoff(3) = %v, want 400ms", d3)
	}
}

func TestBackoff_CappedAtMax(t *testing.T) {
	fwd := newTestForwarder(Config{
		BaseDelay: 1 * time.Second,
		MaxDelay:  3 * time.Second,
	})

	d := fwd.backoff(10)
	if d > 3*time.Second {
		t.Errorf("backoff(10) = %v, exceeds max 3s", d)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		status int
		err    string
		want   bool
	}{
		{0, "connection refused", true}, // network error
		{500, "", true},                 // 5xx
		{502, "", true},                 // 5xx
		{503, "", true},                 // 5xx
		{429, "", true},                 // rate limited
		{400, "", false},                // 4xx (not retryable)
		{404, "", false},                // 4xx
		{200, "", false},                // success
		{301, "", false},                // redirect
	}

	for _, tt := range tests {
		got := isRetryable(tt.status, tt.err)
		if got != tt.want {
			t.Errorf("isRetryable(%d, %q) = %v, want %v", tt.status, tt.err, got, tt.want)
		}
	}
}

func TestIsHopByHop(t *testing.T) {
	hopHeaders := []string{"Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailers", "Transfer-Encoding", "Upgrade"}
	for _, h := range hopHeaders {
		if !isHopByHop(h) {
			t.Errorf("isHopByHop(%q) = false, want true", h)
		}
	}

	nonHop := []string{"Content-Type", "Authorization", "X-Custom", "Accept"}
	for _, h := range nonHop {
		if isHopByHop(h) {
			t.Errorf("isHopByHop(%q) = true, want false", h)
		}
	}
}

func TestForward_BodyForwarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		body := string(buf[:n])
		if !strings.Contains(body, "hello world") {
			t.Errorf("body = %q, want to contain %q", body, "hello world")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fwd := newTestForwarder(Config{Timeout: 5 * time.Second, MaxRetries: 0})
	fwd.Forward(context.Background(), Request{
		Method: "POST",
		Body:   []byte("hello world"),
	}, []string{server.URL})
}
