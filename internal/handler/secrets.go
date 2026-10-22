package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/config"
	"github.com/sarathsp06/testhooks/internal/crypto"
	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/signature"
)

// errNoEncryptionKeys is returned when a signature secret is saved but no
// key encryption key is configured to store it with.
var errNoEncryptionKeys = errors.New("signature secrets are stored encrypted: set ENCRYPTION_KEYS on the server to save one")

// secretBox keeps endpoint signature secrets write-only and encrypted at rest.
// Secrets arrive as signature.secret, are stored as signature.secret_enc (an
// envelope from internal/crypto) and leave the API only as signature.secret_set.
// A nil service still verifies configs saved before encryption existed.
type secretBox struct {
	svc *crypto.Service
}

// seal prepares an endpoint config from the API for storage. A signature
// without a "secret" key keeps the secret already stored in old; an empty or
// null secret removes it.
func (b secretBox) seal(in, old json.RawMessage) (json.RawMessage, error) {
	top, sig, ok := splitSignature(in)
	if !ok {
		return in, nil
	}
	delete(sig, "secret_set")
	delete(sig, "secret_enc")
	value, present := sig["secret"]
	delete(sig, "secret")
	secret, _ := value.(string)

	switch {
	case secret != "":
		if err := b.store(sig, secret); err != nil {
			return nil, err
		}
	case present:
		// Explicitly cleared.
	default:
		prev := config.ParseEndpointConfig(old).Signature
		switch {
		case prev == nil:
		case prev.SecretEnc != "":
			sig["secret_enc"] = prev.SecretEnc
		case prev.Secret != "":
			// Saved before encryption: encrypt it now if we can.
			if b.svc.Enabled() {
				if err := b.store(sig, prev.Secret); err != nil {
					return nil, err
				}
			} else {
				sig["secret"] = prev.Secret
			}
		}
	}
	return joinSignature(top, sig)
}

// store encrypts secret into sig["secret_enc"].
func (b secretBox) store(sig map[string]any, secret string) error {
	if !b.svc.Enabled() {
		return errNoEncryptionKeys
	}
	enc, err := b.svc.EncryptString(secret)
	if err != nil {
		return fmt.Errorf("encrypt signature secret: %w", err)
	}
	sig["secret_enc"] = base64.StdEncoding.EncodeToString(enc)
	return nil
}

// redact returns ep with the signature secret replaced by secret_set, for API
// responses.
func (b secretBox) redact(ep db.Endpoint) db.Endpoint {
	top, sig, ok := splitSignature(ep.Config)
	if !ok {
		return ep
	}
	if s, _ := sig["secret"].(string); s != "" {
		sig["secret_set"] = true
	}
	if s, _ := sig["secret_enc"].(string); s != "" {
		sig["secret_set"] = true
	}
	delete(sig, "secret")
	delete(sig, "secret_enc")
	if cfg, err := joinSignature(top, sig); err == nil {
		ep.Config = cfg
	}
	return ep
}

func (b secretBox) redactAll(eps []db.Endpoint) []db.Endpoint {
	out := make([]db.Endpoint, len(eps))
	for i, ep := range eps {
		out[i] = b.redact(ep)
	}
	return out
}

// check verifies a request's signature against cfg, decrypting the stored
// secret. A secret that can't be decrypted fails verification.
func (b secretBox) check(cfg config.EndpointConfig, h http.Header, body []byte, receivedAt time.Time) *signature.Result {
	sig := cfg.Signature
	if sig == nil {
		return nil
	}
	secret := sig.Secret
	if sig.SecretEnc != "" {
		var err error
		if secret, err = b.open(sig.SecretEnc); err != nil {
			return &signature.Result{
				Status:    signature.StatusInvalid,
				Error:     "stored signature secret can't be decrypted: " + err.Error(),
				WebhookID: h.Get("webhook-id"),
			}
		}
	}
	return signature.Check(secret, sig.PublicKey, h, body, receivedAt)
}

// annotate fills in Signature for stored requests, which don't persist the
// result. Headers are stored as the JSON-encoded http.Header.
func (b secretBox) annotate(cfg config.EndpointConfig, reqs ...*db.CapturedRequest) {
	if cfg.Signature == nil {
		return
	}
	for _, r := range reqs {
		var h http.Header
		_ = json.Unmarshal(r.Headers, &h)
		r.Signature = b.check(cfg, h, r.Body, r.CreatedAt)
	}
}

func (b secretBox) open(secretEnc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(secretEnc)
	if err != nil {
		return "", fmt.Errorf("invalid secret_enc: %w", err)
	}
	return b.svc.DecryptString(raw)
}

// reseal brings a stored config up to date with the keyring: it encrypts a
// plaintext secret and rewraps one encrypted with a key other than the
// primary. changed is false when nothing needed doing.
func (b secretBox) reseal(cfg json.RawMessage) (out json.RawMessage, changed bool, err error) {
	top, sig, ok := splitSignature(cfg)
	if !ok {
		return cfg, false, nil
	}
	if s, _ := sig["secret"].(string); s != "" {
		delete(sig, "secret")
		if err := b.store(sig, s); err != nil {
			return nil, false, err
		}
	} else if s, _ := sig["secret_enc"].(string); s != "" {
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, false, fmt.Errorf("invalid secret_enc: %w", err)
		}
		if keyID, err := crypto.KeyID(raw); err == nil && keyID == b.svc.PrimaryKeyID() {
			return cfg, false, nil
		}
		rewrapped, err := b.svc.Rewrap(raw)
		if err != nil {
			return nil, false, err
		}
		sig["secret_enc"] = base64.StdEncoding.EncodeToString(rewrapped)
	} else {
		return cfg, false, nil
	}
	out, err = joinSignature(top, sig)
	return out, err == nil, err
}

// ResealSecrets encrypts plaintext signature secrets and rewraps those
// encrypted with a non-primary key, so a rotated-out key can be removed once
// it has run. It does nothing without an encryption key.
func ResealSecrets(ctx context.Context, store Store, svc *crypto.Service, log zerolog.Logger) (resealed int, err error) {
	if !svc.Enabled() {
		return 0, nil
	}
	b := secretBox{svc: svc}
	const page = 200
	for offset := 0; ; offset += page {
		eps, err := store.ListEndpoints(ctx, "", page, offset)
		if err != nil {
			return resealed, err
		}
		for _, ep := range eps {
			cfg, changed, err := b.reseal(ep.Config)
			if err != nil {
				log.Error().Err(err).Str("endpoint", ep.ID).Msg("can't reseal signature secret")
				continue
			}
			if !changed {
				continue
			}
			if _, err := store.UpdateEndpoint(ctx, ep.ID, ep.Name, ep.Mode, cfg); err != nil {
				return resealed, err
			}
			resealed++
		}
		if len(eps) < page {
			return resealed, nil
		}
	}
}

// splitSignature decodes cfg into its top-level fields and its signature
// object. ok is false when cfg has no signature object.
func splitSignature(cfg json.RawMessage) (top map[string]json.RawMessage, sig map[string]any, ok bool) {
	if len(cfg) == 0 || json.Unmarshal(cfg, &top) != nil || top == nil {
		return nil, nil, false
	}
	raw, has := top["signature"]
	if !has || json.Unmarshal(raw, &sig) != nil || sig == nil {
		return nil, nil, false
	}
	return top, sig, true
}

func joinSignature(top map[string]json.RawMessage, sig map[string]any) (json.RawMessage, error) {
	raw, err := json.Marshal(sig)
	if err != nil {
		return nil, err
	}
	top["signature"] = raw
	return json.Marshal(top)
}
