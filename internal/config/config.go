package config

import (
	"fmt"
	"net"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/rs/zerolog/log"
)

// Config holds all application configuration, parsed from environment variables.
type Config struct {
	// Listen address for the HTTP server (e.g. ":8080", "0.0.0.0:9090").
	Listen string `env:"LISTEN" envDefault:":8080"`

	// PORT overrides Listen when set (standard PaaS convention).
	// If PORT is set, Listen becomes ":<PORT>".
	Port int `env:"PORT"`

	// PostgreSQL connection string. Required in production.
	// WARNING: The default value includes credentials and should NOT be used in production.
	DatabaseURL string `env:"DATABASE_URL" envDefault:"postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable"`

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

	// Max total body storage in bytes per endpoint — oldest requests are pruned
	// when an endpoint exceeds this budget (default 10MB).
	MaxEndpointStorageBytes int64 `env:"MAX_ENDPOINT_STORAGE_BYTES" envDefault:"10485760"`

	// Max requests per endpoint — oldest are pruned beyond this count.
	MaxRequestsPerEndpoint int `env:"MAX_REQUESTS_PER_ENDPOINT" envDefault:"500"`

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

	// API route timeout in seconds (for non-WebSocket HTTP routes).
	APITimeoutSeconds int `env:"API_TIMEOUT_SECONDS" envDefault:"30"`

	// Max WASM output size in bytes (default 1MB). QuickJS transform results
	// exceeding this limit are rejected.
	MaxWASMOutputSize int `env:"MAX_WASM_OUTPUT_SIZE" envDefault:"1048576"`
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

	// H-01: Fatal if using the default DATABASE_URL with embedded credentials in production.
	const defaultDBURL = "postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable"
	if cfg.DatabaseURL == defaultDBURL && !cfg.Dev {
		log.Fatal().Msg("DATABASE_URL is using default credentials — set DATABASE_URL env var for production")
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
