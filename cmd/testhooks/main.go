package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/cors"
	"github.com/rs/zerolog"

	"github.com/sarathsp06/testhooks/internal/cleanup"
	"github.com/sarathsp06/testhooks/internal/config"
	"github.com/sarathsp06/testhooks/internal/crypto"
	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/db/memstore"
	"github.com/sarathsp06/testhooks/internal/forward"
	"github.com/sarathsp06/testhooks/internal/handler"
	"github.com/sarathsp06/testhooks/internal/hub"
	"github.com/sarathsp06/testhooks/internal/middleware"
	"github.com/sarathsp06/testhooks/internal/wasm"
	"github.com/sarathsp06/testhooks/web"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("testhooks", version)
		return
	}

	// Load .env file if present (before anything else).
	// Silently ignored if the file doesn't exist.
	_ = godotenv.Load()

	// Structured logger (pretty for dev, JSON for prod).
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Logger()

	// Load config from environment.
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	if cfg.TrustedProxiesAuto {
		log.Info().Str("trusted_proxies", cfg.TrustedProxies).Msg("running on Vercel: trusting its proxy for client IPs (set TRUSTED_PROXIES to override)")
	}

	log.Info().
		Str("version", version).
		Str("listen", cfg.Listen).
		Bool("dev", cfg.Dev).
		Msg("starting testhooks")

	// Cancelled on SIGINT/SIGTERM so a stuck startup (e.g. DB retry) can be interrupted.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Storage: Postgres by default, or in memory (with an optional snapshot
	// file) when DATABASE_URL=memory, for local testing without a database.
	var store interface {
		handler.Store
		Ping(context.Context) error
		Close()
	}
	if cfg.DatabaseURL == "memory" {
		mem, err := memstore.New(cfg.SnapshotPath, log)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to open in-memory store")
		}
		store = mem
		if cfg.SnapshotPath == "" {
			log.Warn().Msg("in-memory storage: all data is lost on restart (set SNAPSHOT_PATH to keep it)")
		} else {
			log.Info().Str("snapshot", cfg.SnapshotPath).Msg("in-memory storage with snapshot file")
		}
	} else {
		// Connect to Postgres and run migrations, retrying while the database comes
		// up (common in container orchestration where start order isn't guaranteed).
		// Vercel kills an instance that isn't listening within 30s, so give up
		// sooner there and log the real database error instead.
		wait := 60 * time.Second
		if cfg.OnVercel {
			wait = 20 * time.Second
		}
		pool, err := connectWithRetry(ctx, cfg.DatabaseURL, wait, cfg.OnVercel, log)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to connect to database")
		}
		store = pool
		log.Info().Msg("database connected and migrations applied")
	}
	defer store.Close()

	// Encryption for signature secrets at rest. Memory-only storage dies with
	// the process, so a per-process key is enough there; anything persistent
	// needs ENCRYPTION_KEYS, or saving a secret is refused.
	cryptoSvc, err := encryptionService(cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to set up encryption")
	}
	if n, err := handler.ResealSecrets(ctx, store, cryptoSvc, log); err != nil {
		log.Error().Err(err).Msg("failed to reseal signature secrets")
	} else if n > 0 {
		log.Info().Int("endpoints", n).Str("primary_key", cryptoSvc.PrimaryKeyID()).Msg("encrypted or rewrapped signature secrets")
	}

	// In-memory pub/sub hub with TTL sweep and subscriber limits.
	h := hub.New(cfg.RingBufferSize, cfg.RingBufferTTLSeconds, cfg.MaxSubscribersPerSlug)
	defer h.Stop()

	// Server-side forwarder for webhook fan-out.
	fwd := forward.New(forward.DefaultConfig(), log)

	// Server-side WASM runner for JS transforms.
	wasmRunner, err := wasm.New(ctx, wasm.DefaultConfig(), log)
	if err != nil {
		log.Warn().Err(err).Msg("failed to initialize WASM runner (transforms disabled)")
	}
	if wasmRunner != nil {
		defer wasmRunner.Close(context.Background())
	}

	// Create handlers.
	allowedOrigins := cfg.ParsedAllowedOrigins()
	trustedProxies := cfg.ParsedTrustedProxies()
	capture := handler.NewCapture(store, h, fwd, wasmRunner, cfg.MaxBodySize, trustedProxies, log)
	stripHeaders := cfg.ParsedStripHeaders()
	capture.SetStripHeaders(stripHeaders)
	capture.SetEncryption(cryptoSvc)
	log.Info().Strs("strip_headers", stripHeaders).Msg("stripping headers from captured requests (set STRIP_HEADERS to change)")
	api := handler.NewAPI(store, h, log)
	api.SetEncryption(cryptoSvc)
	ws := handler.NewWS(store, h, log, allowedOrigins)

	// Build the HTTP router using Go 1.22+ stdlib patterns.
	mux := http.NewServeMux()

	// Rate limiter(s). Create a shared rate limiter when RPS > 0.
	// We use the same limiter for both capture and API routes so that
	// per-IP budgets are shared — a single abusive IP can't exhaust
	// both independently.
	var rl *middleware.RateLimiter
	if cfg.RateLimitRPS > 0 {
		rl = middleware.NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst, trustedProxies, log)
		defer rl.Close()
		log.Info().
			Float64("rps", cfg.RateLimitRPS).
			Int("burst", cfg.RateLimitBurst).
			Msg("rate limiting enabled")
	}

	// wrapRate wraps a handler with the rate limiter (no-op if disabled).
	wrapRate := func(h http.Handler) http.Handler {
		if rl != nil {
			return rl.Wrap(h)
		}
		return h
	}

	// Webhook capture — catch all methods, rate-limited.
	mux.Handle("/h/{slug}", wrapRate(capture))
	mux.Handle("/h/{slug}/{rest...}", wrapRate(capture))

	// WebSocket — not rate-limited (long-lived connections, auth-gated).
	mux.Handle("/ws/{slug}", ws)

	// Health check — always public, exempt from auth and rate limiting.
	// Returns 200 with {"status":"ok"} if the DB is reachable, 503 otherwise.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ping(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "detail": "database unreachable"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// REST API — wrapped with timeout handler and rate limiter.
	apiTimeout := time.Duration(cfg.APITimeoutSeconds) * time.Second
	wrapAPI := func(h http.HandlerFunc) http.Handler {
		return wrapRate(http.TimeoutHandler(h, apiTimeout, `{"error":"request timeout"}`))
	}
	mux.Handle("POST /api/endpoints", wrapAPI(api.CreateEndpoint))
	mux.Handle("GET /api/endpoints", wrapAPI(api.ListEndpoints))
	mux.Handle("GET /api/slug/{slug}", wrapAPI(api.GetEndpointBySlug))
	mux.Handle("GET /api/endpoints/{id}", wrapAPI(api.GetEndpoint))
	mux.Handle("PATCH /api/endpoints/{id}", wrapAPI(api.UpdateEndpoint))
	mux.Handle("DELETE /api/endpoints/{id}", wrapAPI(api.DeleteEndpoint))
	mux.Handle("GET /api/endpoints/{id}/requests", wrapAPI(api.ListRequests))
	mux.Handle("DELETE /api/endpoints/{id}/requests", wrapAPI(api.DeleteAllRequests))
	mux.Handle("GET /api/requests/{reqId}", wrapAPI(api.GetRequest))
	mux.Handle("DELETE /api/requests/{reqId}", wrapAPI(api.DeleteRequest))

	// SPA serving: dev mode proxies to Vite, prod serves embedded static files.
	if cfg.Dev {
		viteURL, err := url.Parse(cfg.ViteURL)
		if err != nil {
			log.Fatal().Err(err).Str("vite_url", cfg.ViteURL).Msg("invalid vite url")
		}
		proxy := httputil.NewSingleHostReverseProxy(viteURL)
		mux.Handle("/", proxy)
		log.Info().Str("vite_url", cfg.ViteURL).Msg("dev mode: proxying SPA to vite")
	} else {
		spaFS, err := web.DistFS()
		if err != nil {
			log.Fatal().Err(err).Msg("failed to load embedded SPA")
		}
		mux.Handle("/", spaHandler(spaFS))
		log.Info().Msg("serving embedded SPA")
	}

	// CORS middleware (needed for SPA dev proxy and cross-origin access).
	// M-02: Use configured origins and explicit allowed headers instead of wildcards.
	c := cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: cfg.ParsedAllowedHeaders(),
	})

	// C-01: Token auth middleware. When AUTH_TOKEN is set, all API and WS
	// requests require a valid Bearer token. Capture endpoints (/h/) and
	// /healthz remain public.
	authHandler := middleware.TokenAuth(cfg.AuthToken, log, trustedProxies)(c.Handler(mux))

	// LOW-001: Wrap with security headers middleware.
	securityHandler := middleware.SecurityHeaders(authHandler)

	// HIGH-003: Set HTTP server timeouts to prevent slowloris and resource exhaustion.
	// Note: WriteTimeout is not set because it kills long-lived WebSocket connections.
	// Instead, individual non-WS handlers should use http.TimeoutHandler if needed.
	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           securityHandler,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// Start background pruner.
	prunerCtx, prunerCancel := context.WithCancel(ctx)
	defer prunerCancel()
	pruner := cleanup.NewPruner(
		store,
		cfg.MaxEndpointStorageBytes,
		cfg.MaxRequestsPerEndpoint,
		cfg.PruneIntervalSeconds,
		log,
	)
	go pruner.Run(prunerCtx)

	// Graceful shutdown. A listen failure takes the same path so deferred
	// cleanup still runs (log.Fatal in the goroutine would skip it).
	serverErr := make(chan error, 1)
	go func() {
		log.Info().Str("addr", cfg.Listen).Msg("listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	exitCode := 0
	select {
	case <-ctx.Done():
		log.Info().Msg("shutting down...")
	case err := <-serverErr:
		log.Error().Err(err).Msg("server error, shutting down")
		exitCode = 1
	}

	prunerCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("shutdown error")
	}
	// Let in-flight async forwards finish so webhooks aren't silently dropped on deploy.
	if err := capture.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("async forwards still in flight at shutdown")
	}
	if exitCode != 0 {
		store.Close() // os.Exit skips defers; flush the snapshot / close the pool first
		os.Exit(exitCode)
	}
	log.Info().Msg("goodbye")
}

// connectWithRetry calls db.Connect with exponential backoff until it succeeds,
// maxWait elapses, or ctx is cancelled.
func connectWithRetry(ctx context.Context, databaseURL string, maxWait time.Duration, serverless bool, log zerolog.Logger) (*db.Pool, error) {
	deadline := time.Now().Add(maxWait)
	delay := 500 * time.Millisecond
	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		pool, err := db.Connect(attemptCtx, databaseURL, serverless)
		cancel()
		if err == nil {
			return pool, nil
		}
		if errors.Is(err, db.ErrInvalidURL) {
			return nil, err // a malformed URL won't fix itself
		}
		if ctx.Err() != nil || time.Now().Add(delay).After(deadline) {
			return nil, err
		}
		log.Warn().Err(err).Int("attempt", attempt).Dur("retry_in", delay).Msg("database not ready, retrying")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, 5*time.Second)
	}
}

// spaHandler serves static files from the embedded FS. For any path that doesn't
// match a real file, it falls back to index.html (SPA client-side routing).
func spaHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly.
		path := r.URL.Path
		if path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Check if file exists in embedded FS.
		f, err := root.Open(path[1:]) // strip leading /
		if err != nil {
			// File doesn't exist — serve index.html for SPA routing.
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}

// encryptionService builds the service that encrypts signature secrets from
// ENCRYPTION_KEYS. Without keys it uses a random per-process key for
// memory-only storage and is disabled otherwise.
func encryptionService(cfg *config.Config, log zerolog.Logger) (*crypto.Service, error) {
	keyring, err := cfg.EncryptionKeyring()
	if err != nil {
		return nil, err
	}
	if keyring == nil {
		if cfg.DatabaseURL != "memory" || cfg.SnapshotPath != "" {
			log.Warn().Msg("ENCRYPTION_KEYS is not set: signature secrets can't be saved (generate a key with: openssl rand -hex 32)")
			return nil, nil
		}
		_, key, err := crypto.GenerateKey()
		if err != nil {
			return nil, err
		}
		if keyring, err = crypto.NewKeyring([]crypto.Key{{ID: "ephemeral", Material: key}}, ""); err != nil {
			return nil, err
		}
		log.Info().Msg("signature secrets encrypted with a per-process key (in-memory storage)")
	}
	return crypto.NewServiceFromKeyring(keyring)
}
