package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/config"
	"github.com/sarathsp06/testhooks/internal/crypto"
)

func testCrypto(t *testing.T, primary string, ids ...string) *crypto.Service {
	t.Helper()
	var keys []crypto.Key
	for _, id := range ids {
		keys = append(keys, crypto.Key{ID: id, Material: bytes.Repeat([]byte(id[:1]), 32)})
	}
	kr, err := crypto.NewKeyring(keys, primary)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := crypto.NewServiceFromKeyring(kr)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func doJSON(t *testing.T, mux http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func signatureOf(v map[string]any) map[string]any {
	cfg, _ := v["config"].(map[string]any)
	sig, _ := cfg["signature"].(map[string]any)
	return sig
}

func TestAPI_SignatureSecretWriteOnlyAndEncrypted(t *testing.T) {
	store := newMockStore()
	api := newTestAPI(store)
	api.SetEncryption(testCrypto(t, "a", "a"))
	mux := newMux(api)

	w, ep := doJSON(t, mux, "POST", "/api/endpoints",
		`{"config":{"forward_url":"https://x.test","signature":{"secret":"whsec_c2VjcmV0","enforce":true}}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "whsec_") || strings.Contains(w.Body.String(), "secret_enc") {
		t.Fatalf("response leaks the secret: %s", w.Body)
	}
	if sig := signatureOf(ep); sig["secret_set"] != true || sig["enforce"] != true {
		t.Errorf("response signature = %v, want secret_set and enforce", sig)
	}

	id := ep["id"].(string)
	stored := func() config.EndpointConfig {
		got, _ := store.GetEndpointByID(context.Background(), id)
		if strings.Contains(string(got.Config), "whsec_") {
			t.Fatalf("secret stored in plaintext: %s", got.Config)
		}
		return config.ParseEndpointConfig(got.Config)
	}
	cfg := stored()
	if cfg.Signature.SecretEnc == "" || cfg.ForwardURL != "https://x.test" {
		t.Fatalf("stored config = %+v", cfg)
	}
	enc := cfg.Signature.SecretEnc

	// Reads never return it.
	for _, path := range []string{"/api/endpoints/" + id, "/api/endpoints"} {
		w, _ := doJSON(t, mux, "GET", path, "")
		if strings.Contains(w.Body.String(), "whsec_") || strings.Contains(w.Body.String(), enc) {
			t.Errorf("GET %s leaks the secret: %s", path, w.Body)
		}
	}

	// Saving the config as returned (secret_set, no secret) keeps the secret.
	_, got := doJSON(t, mux, "GET", "/api/endpoints/"+id, "")
	patch, _ := json.Marshal(map[string]any{"config": got["config"]})
	if w, _ := doJSON(t, mux, "PATCH", "/api/endpoints/"+id, string(patch)); w.Code != http.StatusOK {
		t.Fatalf("patch status = %d", w.Code)
	}
	if stored().Signature.SecretEnc != enc {
		t.Error("PATCH without secret changed the stored secret")
	}

	// A client can't plant its own ciphertext.
	doJSON(t, mux, "PATCH", "/api/endpoints/"+id, `{"config":{"signature":{"secret_enc":"AAAA"}}}`)
	if stored().Signature.SecretEnc != enc {
		t.Error("client-supplied secret_enc was stored")
	}

	// An empty secret removes it.
	doJSON(t, mux, "PATCH", "/api/endpoints/"+id, `{"config":{"signature":{"secret":"","public_key":"ab"}}}`)
	if sig := stored().Signature; sig.SecretEnc != "" || sig.Secret != "" || sig.PublicKey != "ab" {
		t.Errorf("after clearing: %+v", sig)
	}
}

func TestAPI_SignatureSecretNeedsKeys(t *testing.T) {
	store := newMockStore()
	mux := newMux(newTestAPI(store))
	w, body := doJSON(t, mux, "POST", "/api/endpoints", `{"config":{"signature":{"secret":"s"}}}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(body["error"].(string), "ENCRYPTION_KEYS") {
		t.Fatalf("status = %d, body = %v; want 400 naming ENCRYPTION_KEYS", w.Code, body)
	}
	// Public keys aren't secret and need no encryption.
	if w, _ := doJSON(t, mux, "POST", "/api/endpoints", `{"config":{"signature":{"public_key":"ab"}}}`); w.Code != http.StatusCreated {
		t.Errorf("public key only: status = %d", w.Code)
	}
}

func TestCapture_VerifiesEncryptedSecret(t *testing.T) {
	svc := testCrypto(t, "a", "a")
	store := newMockStore()
	api := newTestAPI(store)
	api.SetEncryption(svc)
	_, ep := doJSON(t, newMux(api), "POST", "/api/endpoints",
		`{"config":{"signature":{"secret":"shared-secret","enforce":true}}}`)
	slug := ep["slug"].(string)

	capture, _ := newTestCapture(store)
	mux := captureMux(capture)

	send := func() int {
		req := httptest.NewRequest("POST", "/h/"+slug, strings.NewReader("{}"))
		signStandardWebhook(req, "shared-secret", "msg_1", "{}", time.Now())
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w.Code
	}

	// Without the key the secret can't be read, so enforcement fails closed.
	if code := send(); code != http.StatusUnauthorized {
		t.Errorf("without key: status = %d, want 401", code)
	}
	capture.SetEncryption(svc)
	if code := send(); code != http.StatusOK {
		t.Errorf("with key: status = %d, want 200", code)
	}
}

func TestResealSecrets(t *testing.T) {
	store := newMockStore()
	old := testCrypto(t, "a", "a")
	enc, _ := old.EncryptString("rotated")

	legacy, _ := json.Marshal(map[string]any{"signature": map[string]any{"secret": "plain"}})
	store.seedEndpoint("ep-1", "s1", "", "server", legacy)
	wrapped, _ := json.Marshal(map[string]any{"signature": map[string]any{"secret_enc": base64.StdEncoding.EncodeToString(enc)}})
	store.seedEndpoint("ep-2", "s2", "", "server", wrapped)
	store.seedEndpoint("ep-3", "s3", "", "server", nil)

	svc := testCrypto(t, "b", "a", "b")
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	n, err := ResealSecrets(context.Background(), store, svc, log)
	if err != nil || n != 2 {
		t.Fatalf("ResealSecrets = %d, %v; want 2", n, err)
	}

	// Everything now opens with the new key alone.
	b := secretBox{svc: testCrypto(t, "b", "b")}
	for id, want := range map[string]string{"ep-1": "plain", "ep-2": "rotated"} {
		ep, _ := store.GetEndpointByID(context.Background(), id)
		sig := config.ParseEndpointConfig(ep.Config).Signature
		if sig.Secret != "" {
			t.Errorf("%s: plaintext secret left: %s", id, ep.Config)
		}
		if got, err := b.open(sig.SecretEnc); err != nil || got != want {
			t.Errorf("%s: open = %q, %v; want %q", id, got, err, want)
		}
	}

	// A second run has nothing to do.
	if n, _ := ResealSecrets(context.Background(), store, svc, log); n != 0 {
		t.Errorf("second run resealed %d", n)
	}
}
