// Package crypto provides envelope encryption (AES-256-GCM, per-record DEK)
// for sensitive data such as webhook signing secrets. It follows Sparrow's
// format (github.com/sarathsp06/sparrow internal/crypto).
//
// The Key Encryption Key (KEK) is loaded from the ENCRYPTION_KEYS
// environment variable as a small keyring. One key is primary for new writes
// and all configured keys are valid for decryption.
//
// # Envelope Encryption
//
// Each Encrypt call generates a random 256-bit Data Encryption Key (DEK),
// encrypts the plaintext with the DEK, then wraps (encrypts) the DEK with
// the KEK. This enables efficient key rotation: [Service.Rewrap] re-wraps a
// DEK with the new primary KEK without touching the data.
//
// When no encryption key is configured, any attempt to encrypt or decrypt
// returns [ErrNoEncryptionKey].
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// ErrNoEncryptionKey is returned when encryption/decryption is attempted
// without a configured encryption key.
var ErrNoEncryptionKey = errors.New("crypto: encryption key not configured (set ENCRYPTION_KEYS)")

// Key is one KEK in a keyring.
type Key struct {
	ID       string
	Material []byte
}

// Keyring holds a small set of KEKs. One key is primary for new encryption;
// all configured keys remain available for decryption during rotation.
type Keyring struct {
	primaryID string
	keys      map[string]Key
	order     []string
}

// NewKeyring validates a set of KEKs and selects the primary key by ID.
func NewKeyring(keys []Key, primaryID string) (*Keyring, error) {
	if len(keys) == 0 {
		return nil, errors.New("crypto: keyring requires at least one key")
	}

	kr := &Keyring{keys: make(map[string]Key, len(keys)), order: make([]string, 0, len(keys))}
	for _, key := range keys {
		if key.ID == "" {
			return nil, errors.New("crypto: key id must not be empty")
		}
		if len(key.ID) > 255 {
			return nil, fmt.Errorf("crypto: key id %q is too long", key.ID)
		}
		if !isSafeKeyID(key.ID) {
			return nil, fmt.Errorf("crypto: key id %q must use only [A-Za-z0-9_-]", key.ID)
		}
		if len(key.Material) != 32 {
			return nil, fmt.Errorf("crypto: key %q must be exactly 32 bytes, got %d", key.ID, len(key.Material))
		}
		if _, exists := kr.keys[key.ID]; exists {
			return nil, fmt.Errorf("crypto: duplicate key id %q", key.ID)
		}
		material := append([]byte(nil), key.Material...)
		kr.keys[key.ID] = Key{ID: key.ID, Material: material}
		kr.order = append(kr.order, key.ID)
	}

	if primaryID == "" {
		if len(keys) != 1 {
			return nil, errors.New("crypto: primary key id is required when keyring has multiple keys")
		}
		primaryID = keys[0].ID
	}
	if _, ok := kr.keys[primaryID]; !ok {
		return nil, fmt.Errorf("crypto: primary key id %q not found in keyring", primaryID)
	}
	kr.primaryID = primaryID
	return kr, nil
}

type kek struct {
	id   string
	aead cipher.AEAD
}

// Service provides encrypt/decrypt operations using envelope encryption
// (per-record DEK wrapped with a KEK). A nil *Service or one created
// without a key is valid — calls return ErrNoEncryptionKey.
type Service struct {
	primary *kek
	byID    map[string]*kek
}

// NewServiceFromKeyring creates a crypto service from a validated keyring.
// The primary key encrypts new values and all configured keys can decrypt.
func NewServiceFromKeyring(keyring *Keyring) (*Service, error) {
	if keyring == nil {
		return &Service{}, nil
	}

	byID := make(map[string]*kek, len(keyring.order))
	for _, id := range keyring.order {
		k, err := buildKEK(id, keyring.keys[id].Material)
		if err != nil {
			return nil, fmt.Errorf("crypto: new KEK cipher for %q: %w", id, err)
		}
		byID[id] = k
	}

	return &Service{
		primary: byID[keyring.primaryID],
		byID:    byID,
	}, nil
}

func buildKEK(id string, material []byte) (*kek, error) {
	aead, err := newAEAD(material)
	if err != nil {
		return nil, err
	}
	return &kek{id: id, aead: aead}, nil
}

func isSafeKeyID(id string) bool {
	for i := 0; i < len(id); i++ {
		b := id[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
			continue
		}
		return false
	}
	return true
}

// ParseKey decodes a 64-character hex string into a 32-byte key suitable for
// a keyring entry. Returns nil if raw is empty.
func ParseKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, nil
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("crypto: invalid hex key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	return key, nil
}

// GenerateKey generates a cryptographically random 32-byte key and returns
// it as a 64-character hex string suitable for an ENCRYPTION_KEYS entry.
func GenerateKey() (string, []byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", nil, fmt.Errorf("crypto: generate key: %w", err)
	}
	return hex.EncodeToString(key), key, nil
}

// Envelope encryption constants.
const (
	envelopeVersionWithKeyID byte = 0x02
	dekSize                       = 32
	wrappedDEKSize                = 12 + dekSize + 16
	envelopeMinSize               = 1 + 1 + 1 + 2 + wrappedDEKSize + 12 + 16
)

// EnvelopeEncrypt encrypts plaintext using envelope encryption:
//  1. Generates a random 256-bit DEK
//  2. Encrypts the plaintext with the DEK using AES-256-GCM
//  3. Encrypts (wraps) the DEK with the KEK
func (s *Service) EnvelopeEncrypt(plaintext []byte) ([]byte, error) {
	if !s.Enabled() {
		return nil, ErrNoEncryptionKey
	}

	dek := make([]byte, dekSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("crypto: generate DEK: %w", err)
	}
	defer clear(dek)

	dataAEAD, err := newAEAD(dek)
	if err != nil {
		return nil, fmt.Errorf("crypto: create data cipher: %w", err)
	}

	dataNonce := make([]byte, dataAEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, dataNonce); err != nil {
		return nil, fmt.Errorf("crypto: generate data nonce: %w", err)
	}
	encryptedData := dataAEAD.Seal(nil, dataNonce, plaintext, nil)

	rest := make([]byte, 0, len(dataNonce)+len(encryptedData))
	rest = append(rest, dataNonce...)
	rest = append(rest, encryptedData...)
	return s.sealEnvelope(dek, rest)
}

// sealEnvelope wraps dek with the primary KEK and assembles the envelope
// around rest (the data nonce and ciphertext).
func (s *Service) sealEnvelope(dek, rest []byte) ([]byte, error) {
	dekNonce := make([]byte, s.primary.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, dekNonce); err != nil {
		return nil, fmt.Errorf("crypto: generate DEK nonce: %w", err)
	}
	wrappedDEK := s.primary.aead.Seal(dekNonce, dekNonce, dek, nil)

	kid := []byte(s.primary.id)
	out := make([]byte, 0, 1+1+len(kid)+2+len(wrappedDEK)+len(rest))
	out = append(out, envelopeVersionWithKeyID, byte(len(kid))) //nolint:gosec // key IDs are at most 255 bytes, checked when the keyring is loaded
	out = append(out, kid...)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(wrappedDEK))) //nolint:gosec // a sealed 32-byte DEK plus nonce and tag, far below 64 KiB
	out = append(out, wrappedDEK...)
	out = append(out, rest...)
	return out, nil
}

// EnvelopeDecrypt decrypts data produced by EnvelopeEncrypt.
func (s *Service) EnvelopeDecrypt(ciphertext []byte) ([]byte, error) {
	if !s.Enabled() {
		return nil, ErrNoEncryptionKey
	}
	return s.decryptKeyedEnvelope(ciphertext)
}

func (s *Service) decryptKeyedEnvelope(ciphertext []byte) ([]byte, error) {
	keyID, wrappedDEK, rest, err := splitKeyedEnvelope(ciphertext)
	if err != nil {
		return nil, err
	}
	key, ok := s.byID[keyID]
	if !ok {
		return nil, fmt.Errorf("crypto: unknown key id %q", keyID)
	}
	dek, err := unwrapDEK(key.aead, wrappedDEK)
	if err != nil {
		return nil, err
	}
	defer clear(dek)
	return decryptDataWithDEK(dek, rest)
}

// splitKeyedEnvelope parses a keyed envelope into its key ID, wrapped DEK and
// the data nonce plus ciphertext.
func splitKeyedEnvelope(ciphertext []byte) (keyID string, wrappedDEK, rest []byte, err error) {
	if len(ciphertext) == 0 {
		return "", nil, nil, errors.New("crypto: envelope ciphertext too short")
	}
	if ciphertext[0] != envelopeVersionWithKeyID {
		return "", nil, nil, fmt.Errorf("crypto: unknown envelope version: 0x%02x", ciphertext[0])
	}
	if len(ciphertext) < envelopeMinSize {
		return "", nil, nil, errors.New("crypto: envelope ciphertext too short")
	}
	kidLen := int(ciphertext[1])
	if kidLen <= 0 {
		return "", nil, nil, errors.New("crypto: invalid envelope: missing key id")
	}
	if len(ciphertext) < 1+1+kidLen+2 {
		return "", nil, nil, errors.New("crypto: invalid envelope: truncated key id")
	}

	kidStart := 2
	kidEnd := kidStart + kidLen
	keyID = string(ciphertext[kidStart:kidEnd])

	edekLen := int(binary.LittleEndian.Uint16(ciphertext[kidEnd : kidEnd+2]))
	headerSize := kidEnd + 2
	if edekLen <= 0 || headerSize+edekLen > len(ciphertext) {
		return "", nil, nil, errors.New("crypto: invalid envelope: bad edek_len")
	}
	return keyID, ciphertext[headerSize : headerSize+edekLen], ciphertext[headerSize+edekLen:], nil
}

func unwrapDEK(aead cipher.AEAD, wrappedDEK []byte) ([]byte, error) {
	nonceSize := aead.NonceSize()
	if len(wrappedDEK) < nonceSize {
		return nil, errors.New("crypto: wrapped DEK too short")
	}
	dekNonce, dekCipher := wrappedDEK[:nonceSize], wrappedDEK[nonceSize:]
	dek, err := aead.Open(nil, dekNonce, dekCipher, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: unwrap DEK: %w", err)
	}
	return dek, nil
}

func decryptDataWithDEK(dek, rest []byte) ([]byte, error) {
	dataAEAD, err := newAEAD(dek)
	if err != nil {
		return nil, fmt.Errorf("crypto: create data cipher from DEK: %w", err)
	}

	dataNonceSize := dataAEAD.NonceSize()
	if len(rest) < dataNonceSize+dataAEAD.Overhead() {
		return nil, errors.New("crypto: envelope data too short")
	}
	dataNonce, dataEncrypted := rest[:dataNonceSize], rest[dataNonceSize:]

	plaintext, err := dataAEAD.Open(nil, dataNonce, dataEncrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt data: %w", err)
	}
	return plaintext, nil
}

// newAEAD creates an AES-256-GCM AEAD from a 32-byte key.
func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Enabled reports whether the service has an encryption key configured.
func (s *Service) Enabled() bool {
	return s != nil && s.primary != nil
}

// EncryptString encrypts a plaintext string and returns the ciphertext bytes.
// An empty string returns nil rather than an envelope, so callers can store
// NULL for "no secret configured" instead of an encrypted empty payload.
func (s *Service) EncryptString(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	return s.EnvelopeEncrypt([]byte(plaintext))
}

// DecryptString decrypts ciphertext back to a plaintext string. Nil/empty
// ciphertext (the EncryptString("") case) decrypts to "" with no error.
func (s *Service) DecryptString(ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	plain, err := s.EnvelopeDecrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// PrimaryKeyID returns the ID of the key that encrypts new values, or "" when
// no key is configured.
func (s *Service) PrimaryKeyID() string {
	if !s.Enabled() {
		return ""
	}
	return s.primary.id
}

// KeyID returns the ID of the KEK that wrapped ciphertext's DEK.
func KeyID(ciphertext []byte) (string, error) {
	keyID, _, _, err := splitKeyedEnvelope(ciphertext)
	return keyID, err
}

// Rewrap re-wraps ciphertext's DEK with the primary KEK, leaving the data
// ciphertext as is. Use it to rotate KEKs: once every value is rewrapped, the
// old key can be removed from the keyring.
func (s *Service) Rewrap(ciphertext []byte) ([]byte, error) {
	if !s.Enabled() {
		return nil, ErrNoEncryptionKey
	}
	keyID, wrappedDEK, rest, err := splitKeyedEnvelope(ciphertext)
	if err != nil {
		return nil, err
	}
	key, ok := s.byID[keyID]
	if !ok {
		return nil, fmt.Errorf("crypto: unknown key id %q", keyID)
	}
	dek, err := unwrapDEK(key.aead, wrappedDEK)
	if err != nil {
		return nil, err
	}
	defer clear(dek)
	return s.sealEnvelope(dek, rest)
}
