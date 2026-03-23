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

	// Rate limit: sustained requests per second per IP on capture endpoints.
	// Set to 0 to disable rate limiting.
	RateLimitRPS float64 `env:"RATE_LIMIT_RPS" envDefault:"20"`

	// Rate limit: maximum burst size per IP.
	RateLimitBurst int `env:"RATE_LIMIT_BURST" envDefault:"40"`

	// Comma-separated list of allowed CORS origins (e.g. "https://hooks.example.com").
	// Defaults to "*" which allows all origins (suitable for dev).
	// In production, set this to your SPA's actual origin.
	AllowedOrigins string `env:"ALLOWED_ORIGINS" envDefault:"*"`

	// Comma-separated list of trusted proxy CIDRs (e.g. "10.0.0.0/8,172.16.0.0/12").
	// When set, X-Forwarded-For and X-Real-IP headers are only trusted if the
	// request comes from one of these networks. When empty, proxy headers are
	// NOT trusted and RemoteAddr is always used.
	TrustedProxies string `env:"TRUSTED_PROXIES" envDefault:""`
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

	// LOW-002: Warn if using the default DATABASE_URL with embedded credentials.
	const defaultDBURL = "postgres://testhooks:testhooks@localhost:5432/testhooks?sslmode=disable"
	if cfg.DatabaseURL == defaultDBURL && !cfg.Dev {
		log.Warn().Msg("DATABASE_URL is using default credentials — set DATABASE_URL env var for production")
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
