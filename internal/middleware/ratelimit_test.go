package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func newTestLimiter(rps float64, burst int) *RateLimiter {
	log := zerolog.Nop()
	rl := &RateLimiter{
		rate:    rps,
		burst:   float64(burst),
		clients: make(map[string]*bucket),
		log:     log,
		done:    make(chan struct{}),
	}
	// Don't start the cleanup goroutine in tests.
	return rl
}

func TestAllow_FirstRequest(t *testing.T) {
	rl := newTestLimiter(10, 10)
	defer rl.Close()

	if !rl.allow("1.2.3.4") {
		t.Error("first request should be allowed")
	}
}

func TestAllow_BurstExhaustion(t *testing.T) {
	rl := newTestLimiter(10, 5)
	defer rl.Close()

	// 5 requests should succeed (burst = 5).
	for i := 0; i < 5; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 6th should fail.
	if rl.allow("1.2.3.4") {
		t.Error("request beyond burst should be denied")
	}
}

func TestAllow_DifferentIPs(t *testing.T) {
	rl := newTestLimiter(10, 2)
	defer rl.Close()

	// Exhaust IP A.
	rl.allow("a")
	rl.allow("a")
	if rl.allow("a") {
		t.Error("IP a should be rate limited")
	}

	// IP B should still be fine.
	if !rl.allow("b") {
		t.Error("IP b should not be rate limited")
	}
}

func TestWrap_Returns429(t *testing.T) {
	rl := newTestLimiter(10, 1)
	defer rl.Close()

	handler := rl.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should pass.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "5.5.5.5:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("first request status = %d, want 200", w.Code)
	}

	// Second request should be rate limited.
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want 429", w.Code)
	}

	if w.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q, want %q", w.Header().Get("Retry-After"), "1")
	}
}

// parseCIDR is a test helper.
func parseCIDR(cidr string) *net.IPNet {
	_, n, _ := net.ParseCIDR(cidr)
	return n
}

func TestClientIP_XForwardedFor_Trusted(t *testing.T) {
	// Peer is a trusted proxy — X-Forwarded-For should be used.
	trusted := []*net.IPNet{parseCIDR("192.0.2.0/24")}
	ip := ClientIP("192.0.2.1:1234", "10.0.0.1, 10.0.0.2, 10.0.0.3", "", trusted)
	if ip != "10.0.0.3" {
		t.Errorf("ClientIP = %q, want %q", ip, "10.0.0.3")
	}
}

func TestClientIP_XForwardedFor_Single_Trusted(t *testing.T) {
	trusted := []*net.IPNet{parseCIDR("192.0.2.0/24")}
	ip := ClientIP("192.0.2.1:1234", "10.0.0.1", "", trusted)
	if ip != "10.0.0.1" {
		t.Errorf("ClientIP = %q, want %q", ip, "10.0.0.1")
	}
}

func TestClientIP_XRealIP_Trusted(t *testing.T) {
	trusted := []*net.IPNet{parseCIDR("192.0.2.0/24")}
	ip := ClientIP("192.0.2.1:1234", "", "192.168.1.1", trusted)
	if ip != "192.168.1.1" {
		t.Errorf("ClientIP = %q, want %q", ip, "192.168.1.1")
	}
}

func TestClientIP_RemoteAddr(t *testing.T) {
	// No trusted proxies — always use RemoteAddr.
	ip := ClientIP("172.16.0.1:54321", "", "", nil)
	if ip != "172.16.0.1" {
		t.Errorf("ClientIP = %q, want %q", ip, "172.16.0.1")
	}
}

func TestClientIP_RemoteAddr_NoPort(t *testing.T) {
	ip := ClientIP("172.16.0.1", "", "", nil)
	if ip != "172.16.0.1" {
		t.Errorf("ClientIP = %q, want %q", ip, "172.16.0.1")
	}
}

func TestClientIP_UntrustedProxy_IgnoresHeaders(t *testing.T) {
	// Peer is NOT in trusted proxies — headers should be ignored.
	trusted := []*net.IPNet{parseCIDR("10.0.0.0/8")}
	ip := ClientIP("192.168.1.1:1234", "1.2.3.4", "5.6.7.8", trusted)
	if ip != "192.168.1.1" {
		t.Errorf("ClientIP = %q, want %q (should ignore headers from untrusted peer)", ip, "192.168.1.1")
	}
}

func TestClientIP_NoTrustedProxies_IgnoresHeaders(t *testing.T) {
	// No trusted proxies configured — headers should always be ignored.
	ip := ClientIP("192.168.1.1:1234", "1.2.3.4", "5.6.7.8", nil)
	if ip != "192.168.1.1" {
		t.Errorf("ClientIP = %q, want %q (should ignore headers when no proxies configured)", ip, "192.168.1.1")
	}
}
