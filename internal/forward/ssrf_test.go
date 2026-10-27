package forward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestSSRFControl(t *testing.T) {
	cases := []struct {
		addr         string
		blocked      bool // with private addresses refused (the default)
		blockedAllow bool // with ALLOW_PRIVATE_FORWARD_TARGETS
	}{
		{"93.184.216.34:443", false, false},
		{"10.100.3.252:443", true, false},
		{"192.168.1.10:80", true, false},
		{"127.0.0.1:3000", true, false},
		{"[::1]:3000", true, false},
		{"100.64.0.1:80", true, false},
		{"169.254.169.254:80", true, true},
		{"100.100.100.200:80", true, true},
		{"[fd00:ec2::254]:80", true, true},
		{"0.0.0.0:80", true, true},
	}
	for _, tc := range cases {
		if got := ssrfSafeControl(false)("tcp", tc.addr, nil) != nil; got != tc.blocked {
			t.Errorf("default %s: blocked = %v, want %v", tc.addr, got, tc.blocked)
		}
		if got := ssrfSafeControl(true)("tcp", tc.addr, nil) != nil; got != tc.blockedAllow {
			t.Errorf("allow private %s: blocked = %v, want %v", tc.addr, got, tc.blockedAllow)
		}
	}
}

func TestBlockedAddressMessage(t *testing.T) {
	err := ssrfSafeControl(false)("tcp", "10.100.3.252:443", nil)
	var blocked *BlockedAddressError
	if !errors.As(err, &blocked) || !blocked.Private {
		t.Fatalf("err = %v, want a private BlockedAddressError", err)
	}
	for _, want := range []string{"10.100.3.252", "browser mode", "ALLOW_PRIVATE_FORWARD_TARGETS=true"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err, want)
		}
	}

	err = ssrfSafeControl(true)("tcp", "169.254.169.254:80", nil)
	if strings.Contains(err.Error(), "ALLOW_PRIVATE_FORWARD_TARGETS") {
		t.Errorf("metadata message %q offers a setting that would not help", err)
	}
}

// A forward to a refused address reports the plain message and is not retried.
func TestForward_BlockedAddressNotRetried(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer server.Close()

	fwd := New(Config{Timeout: 2 * time.Second, MaxRetries: 3, BaseDelay: time.Second, MaxDelay: time.Second}, zerolog.Nop())
	start := time.Now()
	res := fwd.ForwardOne(context.Background(), Request{Method: "POST"}, server.URL)

	if res.OK || hits.Load() != 0 {
		t.Fatalf("loopback target reached: ok=%v hits=%d", res.OK, hits.Load())
	}
	if res.Attempt != 1 || time.Since(start) > 500*time.Millisecond {
		t.Errorf("attempt = %d after %s, want one attempt and no backoff", res.Attempt, time.Since(start))
	}
	if !strings.HasPrefix(res.Error, "The target URL points to the private address 127.0.0.1") {
		t.Errorf("error = %q", res.Error)
	}

	fwd = New(Config{Timeout: 2 * time.Second, AllowPrivateAddresses: true}, zerolog.Nop())
	if res := fwd.ForwardOne(context.Background(), Request{Method: "POST"}, server.URL); !res.OK {
		t.Errorf("with private addresses allowed: %+v", res)
	}
}

