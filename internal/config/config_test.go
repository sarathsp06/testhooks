package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Unset all env vars that could interfere.
	envVars := []string{"LISTEN", "PORT", "DATABASE_URL", "DEV", "VITE_URL",
		"MAX_BODY_SIZE", "MAX_ENDPOINT_STORAGE_BYTES", "MAX_REQUESTS_PER_ENDPOINT",
		"PRUNE_INTERVAL_SECONDS", "RING_BUFFER_SIZE", "RATE_LIMIT_RPS", "RATE_LIMIT_BURST"}
	for _, e := range envVars {
		t.Setenv(e, "")
		os.Unsetenv(e)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// With nothing set, testhooks runs in memory: no database needed.
	if cfg.DatabaseURL != "memory" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "memory")
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
	}
	if cfg.Dev {
		t.Error("Dev = true, want false")
	}
	if cfg.MaxBodySize != 524288 {
		t.Errorf("MaxBodySize = %d, want 524288", cfg.MaxBodySize)
	}
	if cfg.MaxEndpointStorageBytes != 10485760 {
		t.Errorf("MaxEndpointStorageBytes = %d, want 10485760", cfg.MaxEndpointStorageBytes)
	}
	if cfg.MaxRequestsPerEndpoint != 500 {
		t.Errorf("MaxRequestsPerEndpoint = %d, want 500", cfg.MaxRequestsPerEndpoint)
	}
	if cfg.PruneIntervalSeconds != 60 {
		t.Errorf("PruneIntervalSeconds = %d, want 60", cfg.PruneIntervalSeconds)
	}
	if cfg.RingBufferSize != 100 {
		t.Errorf("RingBufferSize = %d, want 100", cfg.RingBufferSize)
	}
	if cfg.RateLimitRPS != 20 {
		t.Errorf("RateLimitRPS = %f, want 20", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != 40 {
		t.Errorf("RateLimitBurst = %d, want 40", cfg.RateLimitBurst)
	}
}

func TestLoad_PortOverridesListen(t *testing.T) {
	t.Setenv("PORT", "3000")
	t.Setenv("LISTEN", ":9090")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Listen != ":3000" {
		t.Errorf("Listen = %q, want %q (PORT should override LISTEN)", cfg.Listen, ":3000")
	}
}

func TestLoad_CustomEnvVars(t *testing.T) {
	t.Setenv("LISTEN", "0.0.0.0:9090")
	t.Setenv("DATABASE_URL", "postgres://custom:custom@db:5432/mydb")
	t.Setenv("DEV", "true")
	t.Setenv("MAX_BODY_SIZE", "1048576")
	t.Setenv("RATE_LIMIT_RPS", "50")
	t.Setenv("RATE_LIMIT_BURST", "100")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Listen != "0.0.0.0:9090" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, "0.0.0.0:9090")
	}
	if cfg.DatabaseURL != "postgres://custom:custom@db:5432/mydb" {
		t.Errorf("DatabaseURL = %q, want custom URL", cfg.DatabaseURL)
	}
	if !cfg.Dev {
		t.Error("Dev = false, want true")
	}
	if cfg.MaxBodySize != 1048576 {
		t.Errorf("MaxBodySize = %d, want 1048576", cfg.MaxBodySize)
	}
	if cfg.RateLimitRPS != 50 {
		t.Errorf("RateLimitRPS = %f, want 50", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != 100 {
		t.Errorf("RateLimitBurst = %d, want 100", cfg.RateLimitBurst)
	}
}

func TestLoad_PortZeroDoesNotOverride(t *testing.T) {
	// PORT=0 should not override LISTEN.
	t.Setenv("PORT", "0")
	t.Setenv("LISTEN", ":9090")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want %q (PORT=0 should not override)", cfg.Listen, ":9090")
	}
}

func TestParseEndpointConfig_Nil(t *testing.T) {
	cfg := ParseEndpointConfig(nil)
	if cfg.ForwardURL != "" {
		t.Errorf("ForwardURL = %q, want empty", cfg.ForwardURL)
	}
	if cfg.WASMScript != "" {
		t.Errorf("WASMScript = %q, want empty", cfg.WASMScript)
	}
	if cfg.CustomResponse != nil {
		t.Errorf("CustomResponse = %v, want nil", cfg.CustomResponse)
	}
}

func TestParseEndpointConfig_Empty(t *testing.T) {
	cfg := ParseEndpointConfig([]byte{})
	if cfg.WASMScript != "" {
		t.Errorf("WASMScript = %q, want empty", cfg.WASMScript)
	}
}

func TestParseEndpointConfig_ValidJSON(t *testing.T) {
	raw := []byte(`{
		"forward_url": "https://example.com/hook",
		"forward_mode": "sync",
		"wasm_script": "function transform(req) { return req; }",
		"transform_language": "javascript",
		"custom_response": {
			"enabled": true,
			"script": "function handler(req) { return {status: 200}; }",
			"language": "javascript"
		}
	}`)

	cfg := ParseEndpointConfig(raw)

	if cfg.ForwardURL != "https://example.com/hook" {
		t.Errorf("ForwardURL = %q, want %q", cfg.ForwardURL, "https://example.com/hook")
	}
	if cfg.ForwardMode != "sync" {
		t.Errorf("ForwardMode = %q, want %q", cfg.ForwardMode, "sync")
	}
	if cfg.WASMScript == "" {
		t.Error("WASMScript is empty, want a script")
	}
	if cfg.TransformLanguage != "javascript" {
		t.Errorf("TransformLanguage = %q, want javascript", cfg.TransformLanguage)
	}
	if cfg.CustomResponse == nil {
		t.Fatal("CustomResponse is nil")
	}
	if !cfg.CustomResponse.Enabled {
		t.Error("CustomResponse.Enabled = false")
	}
}

func TestParseEndpointConfig_InvalidJSON(t *testing.T) {
	// Invalid JSON should return zero-value (no panic).
	cfg := ParseEndpointConfig([]byte(`{invalid`))
	if cfg.WASMScript != "" {
		t.Errorf("expected zero-value config for invalid JSON")
	}
}

func TestLoad_TrimsDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@db:5432/x?sslmode=require\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://u:p@db:5432/x?sslmode=require" {
		t.Errorf("DatabaseURL = %q, want trailing newline trimmed", cfg.DatabaseURL)
	}

	t.Setenv("DATABASE_URL", "  \n")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "memory" {
		t.Errorf("blank DATABASE_URL = %q, want memory", cfg.DatabaseURL)
	}
}

func TestLoad_VercelTrustsLoopback(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("TRUSTED_PROXIES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TrustedProxiesAuto || cfg.TrustedProxies != "127.0.0.1/32,::1/128" {
		t.Errorf("on Vercel: TrustedProxies = %q (auto=%v), want loopback", cfg.TrustedProxies, cfg.TrustedProxiesAuto)
	}

	// An explicit setting always wins.
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TrustedProxiesAuto || cfg.TrustedProxies != "10.0.0.0/8" {
		t.Errorf("explicit TRUSTED_PROXIES overridden: %q (auto=%v)", cfg.TrustedProxies, cfg.TrustedProxiesAuto)
	}

	// Off Vercel nothing is trusted by default.
	t.Setenv("VERCEL", "")
	t.Setenv("TRUSTED_PROXIES", "")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TrustedProxies != "" {
		t.Errorf("off Vercel: TrustedProxies = %q, want empty", cfg.TrustedProxies)
	}
}

func TestLoad_StripHeaders(t *testing.T) {
	t.Setenv("VERCEL", "")
	os.Unsetenv("STRIP_HEADERS")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cfg.ParsedStripHeaders(), ",")
	if got != DefaultStripHeaders {
		t.Errorf("unset STRIP_HEADERS = %q, want %q", got, DefaultStripHeaders)
	}

	// Vercel also strips its signed Forwarded header.
	t.Setenv("VERCEL", "1")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.ParsedStripHeaders(), ","); got != DefaultStripHeaders+",Forwarded" {
		t.Errorf("on Vercel = %q, want defaults plus Forwarded", got)
	}

	tests := []struct {
		env  string
		want string
	}{
		{"", ""},
		{"none", ""},
		{"X-Secret, X-Internal-*", "X-Secret,X-Internal-*"},
		{"default,X-Secret", DefaultStripHeaders + ",Forwarded,X-Secret"},
	}
	for _, tt := range tests {
		t.Setenv("STRIP_HEADERS", tt.env)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(cfg.ParsedStripHeaders(), ","); got != tt.want {
			t.Errorf("STRIP_HEADERS=%q: got %q, want %q", tt.env, got, tt.want)
		}
	}
}
