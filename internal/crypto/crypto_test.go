package crypto

import (
	"bytes"
	"testing"
)

func testService(t *testing.T, primary string, ids ...string) *Service {
	t.Helper()
	var keys []Key
	for _, id := range ids {
		keys = append(keys, Key{ID: id, Material: bytes.Repeat([]byte(id[:1]), 32)})
	}
	kr, err := NewKeyring(keys, primary)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewServiceFromKeyring(kr)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestEnvelopeRoundTrip(t *testing.T) {
	svc := testService(t, "a", "a")
	ct, err := svc.EncryptString("whsec_secret")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("whsec_secret")) {
		t.Fatal("ciphertext contains the plaintext")
	}
	got, err := svc.DecryptString(ct)
	if err != nil || got != "whsec_secret" {
		t.Fatalf("DecryptString = %q, %v", got, err)
	}

	// Each value gets its own DEK and nonces.
	ct2, _ := svc.EncryptString("whsec_secret")
	if bytes.Equal(ct, ct2) {
		t.Error("two encryptions of the same value are identical")
	}

	// Tampering is detected.
	ct[len(ct)-1] ^= 1
	if _, err := svc.DecryptString(ct); err == nil {
		t.Error("tampered ciphertext decrypted")
	}
}

func TestRotation(t *testing.T) {
	old := testService(t, "a", "a")
	ct, err := old.EncryptString("s3cret")
	if err != nil {
		t.Fatal(err)
	}

	// New primary key; the old one stays for decryption.
	both := testService(t, "b", "a", "b")
	if got, err := both.DecryptString(ct); err != nil || got != "s3cret" {
		t.Fatalf("decrypt with old key in ring = %q, %v", got, err)
	}
	if id, _ := KeyID(ct); id != "a" {
		t.Fatalf("KeyID = %q, want a", id)
	}

	rewrapped, err := both.Rewrap(ct)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := KeyID(rewrapped); id != "b" {
		t.Fatalf("rewrapped KeyID = %q, want b", id)
	}

	// Once rewrapped, the old key can be removed.
	onlyNew := testService(t, "b", "b")
	if got, err := onlyNew.DecryptString(rewrapped); err != nil || got != "s3cret" {
		t.Fatalf("decrypt after rotation = %q, %v", got, err)
	}
	if _, err := onlyNew.DecryptString(ct); err == nil {
		t.Error("value still wrapped with the removed key decrypted")
	}
}

func TestDisabledService(t *testing.T) {
	var svc *Service
	if svc.Enabled() {
		t.Fatal("nil service reports enabled")
	}
	if _, err := svc.EncryptString("x"); err != ErrNoEncryptionKey {
		t.Errorf("EncryptString err = %v, want ErrNoEncryptionKey", err)
	}
}

func TestNewKeyringValidation(t *testing.T) {
	good := bytes.Repeat([]byte{1}, 32)
	tests := []struct {
		name    string
		keys    []Key
		primary string
	}{
		{"no keys", nil, ""},
		{"short key", []Key{{ID: "a", Material: []byte{1}}}, ""},
		{"bad id", []Key{{ID: "a b", Material: good}}, ""},
		{"duplicate", []Key{{ID: "a", Material: good}, {ID: "a", Material: good}}, "a"},
		{"no primary with two keys", []Key{{ID: "a", Material: good}, {ID: "b", Material: good}}, ""},
		{"unknown primary", []Key{{ID: "a", Material: good}}, "z"},
	}
	for _, tt := range tests {
		if _, err := NewKeyring(tt.keys, tt.primary); err == nil {
			t.Errorf("%s: want error", tt.name)
		}
	}
}
