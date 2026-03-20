package forward

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Request contains the data needed to forward a captured webhook.
type Request struct {
	Method      string
	Path        string
	Headers     map[string][]string
	Body        []byte
	ContentType string
}

// Result is the outcome of forwarding to a single target URL.
type Result struct {
	URL             string        `json:"url"`
	StatusCode      int           `json:"status_code"`
	OK              bool          `json:"ok"`
	Latency         time.Duration `json:"latency"`
	Error           string        `json:"error,omitempty"`
	Attempt         int           `json:"attempt"`
	ResponseBody    []byte        `json:"-"`                      // populated only by ForwardOne (sync mode)
	ResponseHeaders http.Header   `json:"-"`                      // populated only by ForwardOne (sync mode)
	ContentType     string        `json:"content_type,omitempty"` // response Content-Type, sync mode only
}

// Forwarder sends captured webhook requests to configured target URLs.
type Forwarder struct {
	client     *http.Client
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
	log        zerolog.Logger
}

// Config configures the Forwarder.
type Config struct {
	// Timeout per individual HTTP request.
	Timeout time.Duration
	// MaxRetries is the number of retries after initial failure (0 = no retry).
	MaxRetries int
	// BaseDelay is the initial backoff delay.
	BaseDelay time.Duration
	// MaxDelay caps the exponential backoff.
	MaxDelay time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Timeout:    10 * time.Second,
		MaxRetries: 3,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   10 * time.Second,
	}
}

// New creates a new Forwarder.
func New(cfg Config, log zerolog.Logger) *Forwarder {
	return &Forwarder{
		client: &http.Client{
			Timeout: cfg.Timeout,
			// Don't follow redirects — just report the first response.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxRetries: cfg.MaxRetries,
		baseDelay:  cfg.BaseDelay,
		maxDelay:   cfg.MaxDelay,
		log:        log.With().Str("component", "forwarder").Logger(),
	}
}

// Forward sends the request to all target URLs concurrently.
// It retries failed requests with exponential backoff.
// The context can be used to cancel all in-flight forwarding.
func (f *Forwarder) Forward(ctx context.Context, req Request, targets []string) []Result {
	if len(targets) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	results := make([]Result, len(targets))

	for i, target := range targets {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			results[idx] = f.forwardWithRetry(ctx, req, url)
		}(i, target)
	}

	wg.Wait()
	return results
}

// ForwardOne sends the request to a single target URL and returns the full response
// (status, headers, body). This is used for sync-mode forwarding where the forward
// target's response is needed by the pipeline (e.g., passed to the custom response handler).
// It retries with exponential backoff, same as Forward().
func (f *Forwarder) ForwardOne(ctx context.Context, req Request, target string) Result {
	return f.forwardWithRetryCapture(ctx, req, target)
}

// forwardWithRetryCapture is like forwardWithRetry but captures the response body.
func (f *Forwarder) forwardWithRetryCapture(ctx context.Context, req Request, target string) Result {
	var lastResult Result

	for attempt := 0; attempt <= f.maxRetries; attempt++ {
		if attempt > 0 {
			delay := f.backoff(attempt)
			f.log.Debug().
				Str("url", target).
				Int("attempt", attempt+1).
				Dur("delay", delay).
				Msg("retrying sync forward")

			select {
			case <-ctx.Done():
				lastResult.Error = ctx.Err().Error()
				return lastResult
			case <-time.After(delay):
			}
		}

		result := f.doForwardCapture(ctx, req, target)
		result.Attempt = attempt + 1

		if result.OK || !isRetryable(result.StatusCode, result.Error) {
			return result
		}

		lastResult = result
	}

	return lastResult
}

// doForwardCapture performs a single forwarding attempt and captures the full response.
// Unlike doForward, it reads the response body instead of discarding it.
func (f *Forwarder) doForwardCapture(ctx context.Context, req Request, target string) Result {
	start := time.Now()
	result := Result{URL: target}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, target, bytes.NewReader(req.Body))
	if err != nil {
		result.Error = fmt.Sprintf("invalid request: %v", err)
		result.Latency = time.Since(start)
		return result
	}

	for key, values := range req.Headers {
		if isHopByHop(key) {
			continue
		}
		for _, v := range values {
			httpReq.Header.Add(key, v)
		}
	}

	if req.ContentType != "" {
		httpReq.Header.Set("Content-Type", req.ContentType)
	}

	httpReq.Header.Set("X-Forwarded-By", "testhooks")

	resp, err := f.client.Do(httpReq)
	result.Latency = time.Since(start)
	if err != nil {
		result.Error = err.Error()
		f.log.Warn().Str("url", target).Err(err).Msg("sync forward failed")
		return result
	}
	defer resp.Body.Close()

	// Read response body (capped at 1MB to prevent OOM).
	const maxResponseBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		result.Error = fmt.Sprintf("read response body: %v", err)
		result.StatusCode = resp.StatusCode
		result.Latency = time.Since(start)
		return result
	}

	result.StatusCode = resp.StatusCode
	result.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	result.ResponseBody = body
	result.ResponseHeaders = resp.Header.Clone()
	result.ContentType = resp.Header.Get("Content-Type")

	f.log.Debug().
		Str("url", target).
		Int("status", resp.StatusCode).
		Int("response_size", len(body)).
		Dur("latency", result.Latency).
		Msg("sync forwarded")

	return result
}

// forwardWithRetry attempts to forward to a single URL with retries.
func (f *Forwarder) forwardWithRetry(ctx context.Context, req Request, target string) Result {
	var lastResult Result

	for attempt := 0; attempt <= f.maxRetries; attempt++ {
		if attempt > 0 {
			delay := f.backoff(attempt)
			f.log.Debug().
				Str("url", target).
				Int("attempt", attempt+1).
				Dur("delay", delay).
				Msg("retrying forward")

			select {
			case <-ctx.Done():
				lastResult.Error = ctx.Err().Error()
				return lastResult
			case <-time.After(delay):
			}
		}

		result := f.doForward(ctx, req, target)
		result.Attempt = attempt + 1

		if result.OK || !isRetryable(result.StatusCode, result.Error) {
			return result
		}

		lastResult = result
	}

	return lastResult
}

// doForward performs a single forwarding attempt.
func (f *Forwarder) doForward(ctx context.Context, req Request, target string) Result {
	start := time.Now()
	result := Result{URL: target}

	// Build the outbound HTTP request.
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, target, bytes.NewReader(req.Body))
	if err != nil {
		result.Error = fmt.Sprintf("invalid request: %v", err)
		result.Latency = time.Since(start)
		return result
	}

	// Copy original headers, skipping hop-by-hop headers.
	for key, values := range req.Headers {
		if isHopByHop(key) {
			continue
		}
		for _, v := range values {
			httpReq.Header.Add(key, v)
		}
	}

	// Override content-type if set.
	if req.ContentType != "" {
		httpReq.Header.Set("Content-Type", req.ContentType)
	}

	// Add forwarding header so targets know this is a forward.
	httpReq.Header.Set("X-Forwarded-By", "testhooks")

	resp, err := f.client.Do(httpReq)
	result.Latency = time.Since(start)
	if err != nil {
		result.Error = err.Error()
		f.log.Warn().Str("url", target).Err(err).Msg("forward failed")
		return result
	}
	defer resp.Body.Close()
	// Drain body to allow connection reuse.
	io.Copy(io.Discard, resp.Body)

	result.StatusCode = resp.StatusCode
	result.OK = resp.StatusCode >= 200 && resp.StatusCode < 300

	f.log.Debug().
		Str("url", target).
		Int("status", resp.StatusCode).
		Dur("latency", result.Latency).
		Msg("forwarded")

	return result
}

// backoff computes the delay for a given retry attempt using exponential backoff.
func (f *Forwarder) backoff(attempt int) time.Duration {
	delay := time.Duration(float64(f.baseDelay) * math.Pow(2, float64(attempt-1)))
	if delay > f.maxDelay {
		delay = f.maxDelay
	}
	return delay
}

// isRetryable returns true if the failure is worth retrying.
func isRetryable(statusCode int, errMsg string) bool {
	// Network errors are retryable.
	if errMsg != "" && statusCode == 0 {
		return true
	}
	// 5xx server errors are retryable.
	if statusCode >= 500 {
		return true
	}
	// 429 Too Many Requests is retryable.
	if statusCode == 429 {
		return true
	}
	return false
}

// isHopByHop returns true for HTTP hop-by-hop headers that shouldn't be forwarded.
func isHopByHop(header string) bool {
	switch http.CanonicalHeaderKey(header) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailers", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}
