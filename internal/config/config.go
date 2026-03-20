package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds all application configuration, parsed from environment variables.
type Config struct {
	// Listen address for the HTTP server (e.g. ":8080", "0.0.0.0:9090").
	Listen string `env:"LISTEN" envDefault:":8080"`

	// PORT overrides Listen when set (standard PaaS convention).
	// If PORT is set, Listen becomes ":<PORT>".
	Port int `env:"PORT"`

	// PostgreSQL connection string.
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
	return cfg, nil
}
