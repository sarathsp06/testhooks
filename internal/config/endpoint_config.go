package config

import "encoding/json"

// EndpointConfig represents the parsed JSON config stored in the endpoints.config column.
// The Go server parses this to drive server-side forwarding and WASM transforms.
type EndpointConfig struct {
	// ForwardURL is the single URL that the server will forward captured requests to.
	// Only used in server mode. Must be a public URL (no localhost).
	ForwardURL string `json:"forward_url,omitempty"`

	// ForwardMode controls whether forwarding blocks the webhook response.
	// "async" (default): fire-and-forget, don't block the response.
	// "sync": wait for the forward target's response; if no handler, return
	//   the target's response directly to the webhook sender; if handler exists,
	//   pass the forward response to the handler script.
	ForwardMode string `json:"forward_mode,omitempty"`

	// WASMScript is the user-provided JavaScript source code that will be compiled
	// and executed via Wazero/QuickJS on every captured request (server mode only).
	WASMScript string `json:"wasm_script,omitempty"`

	// TransformLanguage specifies the language of the transform script. Only
	// JavaScript runs; "lua" and "jsonnet" are kept from older versions so the
	// UI can show the script and ask for a rewrite.
	TransformLanguage string `json:"transform_language,omitempty"`

	// TransformDisabled keeps the script saved but stops it running.
	TransformDisabled bool `json:"transform_disabled,omitempty"`

	// CustomResponse configures a programmable response handler that determines
	// what the capture endpoint returns to the webhook sender. The user writes a
	// script (JS/Lua/Jsonnet) that receives the request and returns {status, headers, body}.
	// In the new pipeline, the handler receives the post-transform request plus an
	// optional forward_response object (when sync forwarding is configured).
	CustomResponse *CustomResponseConfig `json:"custom_response,omitempty"`

	// PersistRequests enables server-side storage of captured requests for browser-mode
	// endpoints. When true and mode=browser, the server writes requests to Postgres
	// in addition to relaying them over WebSocket. Processing (transforms, forwarding)
	// still happens in the browser only. Has no effect in server mode (always persisted).
	PersistRequests bool `json:"persist_requests,omitempty"`

	// Signature enables Standard Webhooks signature verification (the scheme
	// Sparrow, Svix and others use) on captured requests.
	Signature *SignatureConfig `json:"signature,omitempty"`
}

// SignatureConfig holds the keys used to verify Standard Webhooks signatures.
// Set Secret, PublicKey, or both; a request verifies if either matches.
//
// The HMAC secret is write-only: the API accepts it as Secret, stores it
// envelope-encrypted as SecretEnc, and only ever reports SecretSet.
type SignatureConfig struct {
	Secret    string `json:"secret,omitempty"`     // HMAC secret ("whsec_..." or raw) for v1 signatures; plaintext only on input or in configs saved before encryption
	SecretEnc string `json:"secret_enc,omitempty"` // base64 envelope (see internal/crypto) holding the HMAC secret
	SecretSet bool   `json:"secret_set,omitempty"` // set in API responses when a secret is stored
	PublicKey string `json:"public_key,omitempty"` // hex Ed25519 public key for v1a signatures
	// Enforce rejects requests that fail verification with 401, as a real
	// receiver should. Senders like Sparrow treat 401 as permanent (no retry).
	Enforce bool `json:"enforce,omitempty"`
	// Resign signs forwarded requests again with the HMAC secret, after the
	// transform, so a target that checks the sender's signature accepts the
	// changed body. Only requests whose incoming signature verified are
	// re-signed; the rest are forwarded with their original headers. Needs Secret.
	Resign bool `json:"resign,omitempty"`
}

// CustomResponseConfig allows users to define a programmable response handler.
// The script receives the request object and must return {status, headers, body}.
// Only JavaScript runs (see IsJavaScript).
type CustomResponseConfig struct {
	Enabled  bool   `json:"enabled,omitempty"`  // must be true to apply custom response
	Script   string `json:"script,omitempty"`   // user-provided response handler script
	Language string `json:"language,omitempty"` // "javascript"; legacy "lua", "jsonnet" don't run
}

// IsJavaScript reports whether a stored script language can run. Scripts
// saved before Lua and Jsonnet were removed keep their language and are skipped.
func IsJavaScript(lang string) bool {
	return lang == "" || lang == "javascript"
}

// ParseEndpointConfig unmarshals the raw JSONB config into a typed struct.
// Returns a zero-value config (no-op) if raw is nil or empty.
func ParseEndpointConfig(raw json.RawMessage) EndpointConfig {
	var cfg EndpointConfig
	if len(raw) == 0 {
		return cfg
	}
	_ = json.Unmarshal(raw, &cfg)
	return cfg
}
