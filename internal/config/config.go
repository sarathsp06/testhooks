package config

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/rs/zerolog/log"
	"github.com/sarathsp06/testhooks/internal/crypto"
)

// Config holds all application configuration, parsed from environment variables.
type Config struct {
	// Listen address for the HTTP server (e.g. ":8080", "0.0.0.0:9090").
	Listen string `env:"LISTEN" envDefault:":8080"`

	// PORT overrides Listen when set (standard PaaS convention).
	// If PORT is set, Listen becomes ":<PORT>".
	Port int `env:"PORT"`

	// Storage: "memory" (the default) keeps data in memory, for local testing
	// with no database. Set a PostgreSQL connection string for anything shared
	// or long-running, e.g. postgres://user:pass@host:5432/testhooks.
	DatabaseURL string `env:"DATABASE_URL" envDefault:"memory"`

	// SnapshotPath, with DATABASE_URL=memory, is a file the in-memory store is
	// saved to every 30s and on shutdown, and loaded from on start. Empty keeps
	// data in memory only.
	SnapshotPath string `env:"SNAPSHOT_PATH"`

	// Dev mode: proxy SPA requests to the Vite dev server.
	Dev bool `env:"DEV" envDefault:"false"`

	// Vite dev server URL (used when Dev is true).
	ViteURL string `env:"VITE_URL" envDefault:"http://localhost:5173"`

	// Auth token for API access. When set, all API and WebSocket requests
	// must include this token via Authorization: Bearer <token> header.
	// When empty, auth is disabled (open access). For internal deployments.
	AuthToken string `env:"AUTH_TOKEN" envDefault:""`

	// Max body size in bytes for captured requests (default 512KB).
	MaxBodySize int64 `env:"MAX_BODY_SIZE" envDefault:"524288"`

	// Max bytes of a reply body stored with a request: the forward target's
	// reply and the response sent back. Longer bodies are cut and marked
	// truncated; live updates still carry the whole body. Not counted in
	// MaxEndpointStorageBytes. 0 stores status and headers only (default 64KB).
	MaxStoredResponseSize int `env:"MAX_STORED_RESPONSE_SIZE" envDefault:"65536"`

	// Max total body storage in bytes per endpoint — oldest requests are pruned
	// when an endpoint exceeds this budget (default 10MB).
	MaxEndpointStorageBytes int64 `env:"MAX_ENDPOINT_STORAGE_BYTES" envDefault:"10485760"`

	// Max requests per endpoint — oldest are pruned beyond this count.
	MaxRequestsPerEndpoint int `env:"MAX_REQUESTS_PER_ENDPOINT" envDefault:"500"`

	// A line shown under the storage limits in the UI saying what a paid
	// plan would lift, for example "A paid plan would keep 10,000 requests
	// for 30 days." It comes with an "I'd pay for that" button whose clicks
	// the server counts. Empty (the default) shows the limits only.
	UpgradeNote string `env:"UPGRADE_NOTE"`

	// Prune interval in seconds.
	PruneIntervalSeconds int `env:"PRUNE_INTERVAL_SECONDS" envDefault:"60"`

	// Ring buffer size for browser-mode endpoints.
	RingBufferSize int `env:"RING_BUFFER_SIZE" envDefault:"100"`

	// Ring buffer entry TTL in seconds (default 5 minutes). Entries older
	// than this are swept periodically.
	RingBufferTTLSeconds int `env:"RING_BUFFER_TTL_SECONDS" envDefault:"300"`

	// Rate limit: sustained requests per second per IP on capture endpoints.
	// Set to 0 to disable rate limiting.
	RateLimitRPS float64 `env:"RATE_LIMIT_RPS" envDefault:"20"`

	// Rate limit: maximum burst size per IP.
	RateLimitBurst int `env:"RATE_LIMIT_BURST" envDefault:"40"`

	// Max WebSocket subscribers per endpoint slug.
	MaxSubscribersPerSlug int `env:"MAX_SUBSCRIBERS_PER_SLUG" envDefault:"50"`

	// Comma-separated list of allowed CORS origins (e.g. "https://hooks.example.com").
	// Defaults to "*" which allows all origins (suitable for dev).
	// In production, set this to your SPA's actual origin.
	AllowedOrigins string `env:"ALLOWED_ORIGINS" envDefault:"*"`

	// Comma-separated list of allowed CORS headers.
	// Defaults to a safe explicit list instead of wildcard.
	AllowedHeaders string `env:"ALLOWED_HEADERS" envDefault:"Content-Type,Authorization,X-Requested-With"`

	// Comma-separated list of trusted proxy CIDRs (e.g. "10.0.0.0/8,172.16.0.0/12").
	// When set, X-Forwarded-For and X-Real-IP headers are only trusted if the
	// request comes from one of these networks. When empty, proxy headers are
	// NOT trusted and RemoteAddr is always used.
	TrustedProxies string `env:"TRUSTED_PROXIES" envDefault:""`

	// OnVercel is set by Vercel's runtime (VERCEL=1). Vercel's proxy reaches
	// the app over loopback and sets X-Forwarded-For / X-Real-Ip itself, so
	// loopback is trusted automatically when TRUSTED_PROXIES is unset.
	OnVercel bool `env:"VERCEL"`

	// TrustedProxiesAuto reports that TrustedProxies was filled in for Vercel.
	TrustedProxiesAuto bool

	// Comma-separated headers removed from captured requests before they are
	// stored, shown or forwarded. A trailing "*" matches a prefix
	// ("X-Internal-*"), "default" expands to DefaultStripHeaders, and an empty
	// value or "none" strips nothing. Unset means "default".
	StripHeaders string `env:"STRIP_HEADERS"`

	// Comma-separated key encryption keys (KEKs) for secrets stored at rest,
	// such as signature secrets, each "<key-id>=<64-char-hex-key>". Generate
	// one with: openssl rand -hex 32. To rotate, add a new key and make it
	// primary; values are rewrapped on startup, then the old key can go.
	EncryptionKeys string `env:"ENCRYPTION_KEYS"`

	// ID of the key in EncryptionKeys that encrypts new values. Required when
	// there is more than one key.
	EncryptionPrimaryKeyID string `env:"ENCRYPTION_PRIMARY_KEY_ID"`

	// API route timeout in seconds (for non-WebSocket HTTP routes).
	APITimeoutSeconds int `env:"API_TIMEOUT_SECONDS" envDefault:"30"`

	// Max WASM output size in bytes (default 1MB). QuickJS transform results
	// exceeding this limit are rejected.
	MaxWASMOutputSize int `env:"MAX_WASM_OUTPUT_SIZE" envDefault:"1048576"`
}

// DefaultStripHeaders are credentials hosting platforms add to requests on
// their way to the app. Vercel sends an OIDC token for the project and a proxy
// signature with every request; neither belongs to the webhook sender.
const DefaultStripHeaders = "X-Vercel-Oidc-Token,X-Vercel-Proxy-Signature,X-Vercel-Proxy-Signature-Ts,X-Vercel-Sc-*"

// vercelStripHeaders are stripped by default only on Vercel, where its proxy
// fills Forwarded with a signature. Elsewhere Forwarded is the sender's own.
const vercelStripHeaders = "Forwarded"

// ParsedStripHeaders returns the header patterns from StripHeaders, with
// "default" expanded.
func (c *Config) ParsedStripHeaders() []string {
	var patterns []string
	for _, p := range strings.Split(c.StripHeaders, ",") {
		p = strings.TrimSpace(p)
		switch strings.ToLower(p) {
		case "", "none":
		case "default":
			patterns = append(patterns, strings.Split(c.defaultStripHeaders(), ",")...)
		default:
			patterns = append(patterns, p)
		}
	}
	return patterns
}

func (c *Config) defaultStripHeaders() string {
	if c.OnVercel {
		return DefaultStripHeaders + "," + vercelStripHeaders
	}
	return DefaultStripHeaders
}

// EncryptionKeyring parses EncryptionKeys and EncryptionPrimaryKeyID. It
// returns nil when no keys are configured.
func (c *Config) EncryptionKeyring() (*crypto.Keyring, error) {
	var keys []crypto.Key
	for _, entry := range strings.Split(c.EncryptionKeys, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		id, raw, ok := strings.Cut(entry, "=")
		id, raw = strings.TrimSpace(id), strings.TrimSpace(raw)
		if !ok || id == "" {
			return nil, fmt.Errorf("ENCRYPTION_KEYS: invalid entry (want <key-id>=<64-char-hex-key>)")
		}
		key, err := crypto.ParseKey(raw)
		if err != nil || key == nil {
			return nil, fmt.Errorf("ENCRYPTION_KEYS: invalid key for %q: want 64 hex characters (openssl rand -hex 32)", id)
		}
		keys = append(keys, crypto.Key{ID: id, Material: key})
	}
	if len(keys) == 0 {
		if strings.TrimSpace(c.EncryptionPrimaryKeyID) != "" {
			return nil, fmt.Errorf("ENCRYPTION_PRIMARY_KEY_ID is set but ENCRYPTION_KEYS is empty")
		}
		return nil, nil
	}
	keyring, err := crypto.NewKeyring(keys, strings.TrimSpace(c.EncryptionPrimaryKeyID))
	if err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEYS / ENCRYPTION_PRIMARY_KEY_ID: %w", err)
	}
	return keyring, nil
}

// ParsedTrustedProxies returns the parsed CIDR networks from TrustedProxies.
func (c *Config) ParsedTrustedProxies() []*net.IPNet {
	return parseCIDRs(c.TrustedProxies)
}

// ParsedAllowedOrigins returns the list of allowed origins from AllowedOrigins.
func (c *Config) ParsedAllowedOrigins() []string {
	if c.AllowedOrigins == "" || c.AllowedOrigins == "*" {
		return []string{"*"}
	}
	parts := strings.Split(c.AllowedOrigins, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			origins = append(origins, p)
		}
	}
	if len(origins) == 0 {
		return []string{"*"}
	}
	return origins
}

// ParsedAllowedHeaders returns the list of allowed headers from AllowedHeaders.
func (c *Config) ParsedAllowedHeaders() []string {
	if c.AllowedHeaders == "" || c.AllowedHeaders == "*" {
		return []string{"Content-Type", "Authorization", "X-Requested-With"}
	}
	parts := strings.Split(c.AllowedHeaders, ",")
	headers := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			headers = append(headers, p)
		}
	}
	if len(headers) == 0 {
		return []string{"Content-Type", "Authorization", "X-Requested-With"}
	}
	return headers
}

// Load parses environment variables into a Config struct.
// If PORT is set, it overrides Listen to ":<PORT>".
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	if cfg.Port > 0 {
		cfg.Listen = fmt.Sprintf(":%d", cfg.Port)
	}

	// Secrets pasted into hosting dashboards often carry a trailing newline,
	// which makes the URL unparseable. Trim it; empty falls back to memory.
	cfg.DatabaseURL = strings.TrimSpace(cfg.DatabaseURL)
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = "memory"
	}
	cfg.SnapshotPath = strings.TrimSpace(cfg.SnapshotPath)

	// On Vercel every request arrives from loopback; without trusting it,
	// all clients look like 127.0.0.1 and share one rate-limit bucket.
	if cfg.OnVercel && strings.TrimSpace(cfg.TrustedProxies) == "" {
		cfg.TrustedProxies = "127.0.0.1/32,::1/128"
		cfg.TrustedProxiesAuto = true
	}

	if _, ok := os.LookupEnv("STRIP_HEADERS"); !ok {
		cfg.StripHeaders = "default"
	}

	// L-04: Validate configuration bounds.
	if cfg.MaxBodySize < 1024 || cfg.MaxBodySize > 10*1024*1024 {
		return nil, fmt.Errorf("MAX_BODY_SIZE must be between 1KB and 10MB, got %d", cfg.MaxBodySize)
	}
	if cfg.RingBufferSize < 1 || cfg.RingBufferSize > 10000 {
		return nil, fmt.Errorf("RING_BUFFER_SIZE must be between 1 and 10000, got %d", cfg.RingBufferSize)
	}
	if cfg.MaxRequestsPerEndpoint < 1 || cfg.MaxRequestsPerEndpoint > 100000 {
		return nil, fmt.Errorf("MAX_REQUESTS_PER_ENDPOINT must be between 1 and 100000, got %d", cfg.MaxRequestsPerEndpoint)
	}
	if cfg.MaxStoredResponseSize < 0 || cfg.MaxStoredResponseSize > 10*1024*1024 {
		return nil, fmt.Errorf("MAX_STORED_RESPONSE_SIZE must be between 0 and 10MB, got %d", cfg.MaxStoredResponseSize)
	}
	if cfg.MaxEndpointStorageBytes < 1024 || cfg.MaxEndpointStorageBytes > 1024*1024*1024 {
		return nil, fmt.Errorf("MAX_ENDPOINT_STORAGE_BYTES must be between 1KB and 1GB, got %d", cfg.MaxEndpointStorageBytes)
	}
	if cfg.RateLimitRPS < 0 {
		return nil, fmt.Errorf("RATE_LIMIT_RPS must be >= 0, got %f", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst < 0 {
		return nil, fmt.Errorf("RATE_LIMIT_BURST must be >= 0, got %d", cfg.RateLimitBurst)
	}
	if cfg.PruneIntervalSeconds < 1 {
		return nil, fmt.Errorf("PRUNE_INTERVAL_SECONDS must be >= 1, got %d", cfg.PruneIntervalSeconds)
	}
	if cfg.MaxSubscribersPerSlug < 1 {
		return nil, fmt.Errorf("MAX_SUBSCRIBERS_PER_SLUG must be >= 1, got %d", cfg.MaxSubscribersPerSlug)
	}
	if cfg.APITimeoutSeconds < 1 {
		return nil, fmt.Errorf("API_TIMEOUT_SECONDS must be >= 1, got %d", cfg.APITimeoutSeconds)
	}
	if cfg.MaxWASMOutputSize < 1024 {
		return nil, fmt.Errorf("MAX_WASM_OUTPUT_SIZE must be >= 1024, got %d", cfg.MaxWASMOutputSize)
	}
	if _, err := cfg.EncryptionKeyring(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// parseCIDRs splits a comma-separated CIDR string and returns parsed networks.
func parseCIDRs(s string) []*net.IPNet {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var nets []*net.IPNet
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(p)
		if err != nil {
			log.Warn().Str("cidr", p).Err(err).Msg("ignoring invalid trusted proxy CIDR")
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets
}
