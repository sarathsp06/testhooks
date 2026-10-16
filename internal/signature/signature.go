// Package signature verifies Standard Webhooks signatures
// (https://www.standardwebhooks.com), the scheme Sparrow and other webhook
// senders use: headers webhook-id, webhook-timestamp and webhook-signature,
// over the message "{id}.{timestamp}.{raw body}".
//
//   - "v1,<base64>"  is HMAC-SHA256 keyed with a shared secret. A "whsec_"
//     secret is base64-decoded first; any other secret is used as raw bytes.
//   - "v1a,<base64>" is Ed25519, verified with a hex-encoded public key.
package signature

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultTolerance is the maximum clock skew accepted between the
// webhook-timestamp header and the verification time (replay protection).
const DefaultTolerance = 5 * time.Minute

// Result statuses.
const (
	StatusVerified = "verified" // a signature of a configured scheme matched
	StatusInvalid  = "invalid"  // headers present but verification failed
	StatusMissing  = "missing"  // one or more Standard Webhooks headers absent
)

// Result is the outcome of checking one request.
type Result struct {
	Status    string `json:"status"`
	Scheme    string `json:"scheme,omitempty"` // "v1" (HMAC) or "v1a" (Ed25519) when verified
	Error     string `json:"error,omitempty"`
	WebhookID string `json:"webhook_id,omitempty"` // stable across sender retries; use it to spot duplicates
}

// Verified reports whether the request carried a valid signature.
func (r *Result) Verified() bool { return r != nil && r.Status == StatusVerified }

var errMissingHeaders = errors.New("missing webhook-id, webhook-timestamp or webhook-signature header")

// Check verifies the request against whichever keys are configured (an HMAC
// secret, an Ed25519 public key, or both; matching either is enough). now is
// the time the request was received. Returns nil when no key is configured.
func Check(secret, publicKey string, h http.Header, body []byte, now time.Time) *Result {
	if secret == "" && publicKey == "" {
		return nil
	}
	res := &Result{WebhookID: h.Get("webhook-id")}

	var errs []string
	if secret != "" {
		err := VerifyHMAC(secret, h, body, now, DefaultTolerance)
		if err == nil {
			res.Status, res.Scheme = StatusVerified, "v1"
			return res
		}
		if errors.Is(err, errMissingHeaders) {
			res.Status, res.Error = StatusMissing, err.Error()
			return res
		}
		errs = append(errs, err.Error())
	}
	if publicKey != "" {
		err := VerifyEd25519(publicKey, h, body, now, DefaultTolerance)
		if err == nil {
			res.Status, res.Scheme = StatusVerified, "v1a"
			return res
		}
		if errors.Is(err, errMissingHeaders) {
			res.Status, res.Error = StatusMissing, err.Error()
			return res
		}
		errs = append(errs, err.Error())
	}
	res.Status, res.Error = StatusInvalid, strings.Join(errs, "; ")
	return res
}

// VerifyHMAC checks the "v1" HMAC-SHA256 signature.
func VerifyHMAC(secret string, h http.Header, body []byte, now time.Time, tolerance time.Duration) error {
	key, err := hmacKey(secret)
	if err != nil {
		return err
	}
	return verify(h, body, now, tolerance, "v1", func(msg, sig []byte) bool {
		mac := hmac.New(sha256.New, key)
		mac.Write(msg)
		return hmac.Equal(mac.Sum(nil), sig)
	})
}

// VerifyEd25519 checks the "v1a" Ed25519 signature with a hex public key.
func VerifyEd25519(publicKeyHex string, h http.Header, body []byte, now time.Time, tolerance time.Duration) error {
	pub, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil {
		return fmt.Errorf("invalid public key: not hex")
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key: want %d bytes, got %d", ed25519.PublicKeySize, len(pub))
	}
	return verify(h, body, now, tolerance, "v1a", func(msg, sig []byte) bool {
		return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
	})
}

func hmacKey(secret string) ([]byte, error) {
	if rest, ok := strings.CutPrefix(secret, "whsec_"); ok {
		key, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return nil, fmt.Errorf("invalid secret: whsec_ value is not base64")
		}
		if len(key) == 0 {
			return nil, fmt.Errorf("invalid secret: empty")
		}
		return key, nil
	}
	if secret == "" {
		return nil, fmt.Errorf("invalid secret: empty")
	}
	return []byte(secret), nil
}

// verify does the shared header/timestamp handling and tries every
// signature of the given version until match returns true.
func verify(h http.Header, body []byte, now time.Time, tolerance time.Duration, version string, match func(msg, sig []byte) bool) error {
	id, ts, sigHeader := h.Get("webhook-id"), h.Get("webhook-timestamp"), h.Get("webhook-signature")
	if id == "" || ts == "" || sigHeader == "" {
		return errMissingHeaders
	}

	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid webhook-timestamp %q", ts)
	}
	skew := now.Sub(time.Unix(sec, 0))
	if skew > tolerance || skew < -tolerance {
		return fmt.Errorf("webhook-timestamp outside %s tolerance (skew %s)", tolerance, skew.Round(time.Second))
	}

	msg := make([]byte, 0, len(id)+len(ts)+len(body)+2)
	msg = append(msg, id...)
	msg = append(msg, '.')
	msg = append(msg, ts...)
	msg = append(msg, '.')
	msg = append(msg, body...)

	found := false
	for _, entry := range strings.Fields(sigHeader) {
		v, sig64, ok := strings.Cut(entry, ",")
		if !ok || v != version {
			continue // other versions (e.g. v1a next to v1) are ignored
		}
		found = true
		sig, err := base64.StdEncoding.DecodeString(sig64)
		if err != nil {
			continue
		}
		if match(msg, sig) {
			return nil
		}
	}
	if !found {
		return fmt.Errorf("no %s signature in webhook-signature header", version)
	}
	return fmt.Errorf("%s signature does not match", version)
}
