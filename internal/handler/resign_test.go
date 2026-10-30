package handler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/forward"
	"github.com/sarathsp06/testhooks/internal/signature"
	"github.com/sarathsp06/testhooks/internal/wasm"
)

const resignSecret = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"

// received is what a forward target got.
type received struct {
	mu     sync.Mutex
	header http.Header
	body   []byte
}

func recordingTarget(t *testing.T) (*httptest.Server, *received) {
	t.Helper()
	got := &received{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		got.header, got.body = r.Header.Clone(), body
		got.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// resigningCapture returns a server-mode capture handler at /h/rs whose
// endpoint transforms the body, re-signs and forwards it to target.
func resigningCapture(t *testing.T, target string, sig map[string]any) (*Capture, *mockStore) {
	t.Helper()
	store := newMockStore()
	cfg, _ := json.Marshal(map[string]any{
		"forward_url":  target,
		"forward_mode": "sync",
		"wasm_script":  `function transform(req) { req.json.transformed = true; return req; }`,
		"signature":    sig,
	})
	store.seedEndpoint("ep-1", "rs", "Resign", "server", cfg)
	capture, _ := newTestCapture(store)
	capture.forwarder = forward.New(forward.Config{DisableSSRFProtection: true}, zerolog.Nop())
	runner, err := wasm.New(context.Background(), wasm.Config{Timeout: 5 * time.Second, PoolSize: 1, MemoryLimit: 32 << 20, MaxStackSize: 512 << 10}, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.Close(context.Background()) })
	capture.wasmRunner = runner
	return capture, store
}

func TestCapture_ResignsAfterTransform(t *testing.T) {
	const body = `{"event":"order.created"}`
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	cases := []struct {
		name         string
		sign         func(*http.Request)
		wantResigned bool
	}{
		{"signed with the secret", func(r *http.Request) { signWith(r, resignSecret, "msg_1", body) }, true},
		{"unsigned", func(*http.Request) {}, false},
		{"signed with another secret", func(r *http.Request) { signWith(r, "whsec_b3RoZXI=", "msg_1", body) }, false},
		// Verifies, but against a public key anyone with API access could set:
		// it must not get the body signed with the HMAC secret.
		{"signed only with an Ed25519 key", func(r *http.Request) {
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			s := ed25519.Sign(priv, []byte("msg_1."+ts+"."+body))
			r.Header.Set("webhook-id", "msg_1")
			r.Header.Set("webhook-timestamp", ts)
			r.Header.Set("webhook-signature", "v1a,"+base64.StdEncoding.EncodeToString(s))
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, got := recordingTarget(t)
			capture, store := resigningCapture(t, target.URL, map[string]any{
				"secret": resignSecret, "public_key": hex.EncodeToString(pub), "resign": true,
			})
			req := httptest.NewRequest("POST", "/h/rs", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			tc.sign(req)
			captureMux(capture).ServeHTTP(httptest.NewRecorder(), req)

			got.mu.Lock()
			defer got.mu.Unlock()
			if !strings.Contains(string(got.body), `"transformed":true`) {
				t.Fatalf("target got %s, want the transformed body", got.body)
			}
			err := signature.VerifyHMAC(resignSecret, got.header, got.body, time.Now(), signature.DefaultTolerance)
			if tc.wantResigned != (err == nil) {
				t.Errorf("target's verify error = %v, want re-signed %v", err, tc.wantResigned)
			}
			if tc.wantResigned && got.header.Get("webhook-id") != "msg_1" {
				t.Errorf("webhook-id = %q, want the sender's msg_1", got.header.Get("webhook-id"))
			}

			var fwd forwardResultPayload
			if err := json.Unmarshal(onlyRequest(t, store).ForwardResult, &fwd); err != nil {
				t.Fatal(err)
			}
			if fwd.Resigned != tc.wantResigned || fwd.Resigned == (fwd.ResignError != "") {
				t.Errorf("forward_result resigned=%v resign_error=%q, want resigned %v", fwd.Resigned, fwd.ResignError, tc.wantResigned)
			}
		})
	}
}

func TestCapture_NoResignWhenOff(t *testing.T) {
	const body = `{"a":1}`
	target, got := recordingTarget(t)
	capture, store := resigningCapture(t, target.URL, map[string]any{"secret": resignSecret})
	req := httptest.NewRequest("POST", "/h/rs", strings.NewReader(body))
	signWith(req, resignSecret, "msg_1", body)
	sent := req.Header.Get("webhook-signature")
	captureMux(capture).ServeHTTP(httptest.NewRecorder(), req)

	if got.header.Get("webhook-signature") != sent {
		t.Errorf("signature changed with resign off: %q", got.header.Get("webhook-signature"))
	}
	var fwd forwardResultPayload
	_ = json.Unmarshal(onlyRequest(t, store).ForwardResult, &fwd)
	if fwd.Resigned || fwd.ResignError != "" {
		t.Errorf("forward_result = %+v, want no re-signing fields", fwd)
	}
}

func signWith(r *http.Request, secret, id, body string) {
	h := http.Header{}
	_ = signature.Resign(h, secret, id, []byte(body), time.Now())
	for k, v := range h {
		r.Header[k] = v
	}
}

func TestAPI_Resign(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	api.SetEncryption(testCrypto(t, "a", "a"))
	api.SetLimits(Limits{MaxBodySize: 1 << 20})
	mux := newMux(api)
	mux.HandleFunc("POST /api/endpoints/{id}/resign", api.ResignRequest)

	// Re-signing needs a secret.
	if w, _ := doJSON(t, mux, "POST", "/api/endpoints", `{"mode":"browser","config":{"signature":{"resign":true}}}`); w.Code != http.StatusBadRequest {
		t.Errorf("resign without secret: status = %d, want 400", w.Code)
	}

	_, ep := doJSON(t, mux, "POST", "/api/endpoints",
		`{"mode":"browser","config":{"signature":{"secret":"`+resignSecret+`","resign":true}}}`)
	id := ep["id"].(string)
	if sig := signatureOf(ep); sig["resign"] != true || sig["secret_set"] != true {
		t.Fatalf("signature = %v", sig)
	}

	const original = `{"event":"x"}`
	const forwarded = `{"event":"x","transformed":true}`
	signed := httptest.NewRequest("POST", "/", nil)
	signWith(signed, resignSecret, "msg_9", original)
	call := func(headers http.Header, origBody, encoding string) (*httptest.ResponseRecorder, map[string]any) {
		in, _ := json.Marshal(map[string]any{
			"request": map[string]any{"id": "r1", "headers": headers, "body": origBody, "body_encoding": encoding},
			"body":    forwarded,
		})
		return doJSON(t, mux, "POST", "/api/endpoints/"+id+"/resign", string(in))
	}

	t.Run("signed original", func(t *testing.T) {
		// Lower-case names, as the tab may send them.
		lower := http.Header{}
		for k, v := range signed.Header {
			lower[strings.ToLower(k)] = v
		}
		w, out := call(lower, original, "text")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", w.Code, w.Body)
		}
		h := http.Header{}
		for k, v := range out["headers"].(map[string]any) {
			h.Set(k, v.(string))
		}
		if err := signature.VerifyHMAC(resignSecret, h, []byte(forwarded), time.Now(), signature.DefaultTolerance); err != nil {
			t.Errorf("returned headers don't verify the forwarded body: %v", err)
		}
		if h.Get("webhook-id") != "msg_9" {
			t.Errorf("webhook-id = %q, want msg_9", h.Get("webhook-id"))
		}
	})

	t.Run("base64 original", func(t *testing.T) {
		w, _ := call(signed.Header, base64.StdEncoding.EncodeToString([]byte(original)), "base64")
		if w.Code != http.StatusOK {
			t.Errorf("status = %d: %s", w.Code, w.Body)
		}
	})

	t.Run("unsigned original is refused", func(t *testing.T) {
		w, out := call(http.Header{}, original, "text")
		if w.Code != http.StatusUnprocessableEntity || out["error"] == "" {
			t.Errorf("status = %d, body %v; want 422 with a reason", w.Code, out)
		}
	})

	t.Run("tampered original is refused", func(t *testing.T) {
		if w, _ := call(signed.Header, `{"event":"y"}`, "text"); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", w.Code)
		}
	})

	t.Run("resign off", func(t *testing.T) {
		_, other := doJSON(t, mux, "POST", "/api/endpoints", `{"config":{"signature":{"secret":"`+resignSecret+`"}}}`)
		in := `{"request":{"headers":{},"body":""},"body":""}`
		if w, _ := doJSON(t, mux, "POST", "/api/endpoints/"+other["id"].(string)+"/resign", in); w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}
