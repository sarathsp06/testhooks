package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Pool wraps a pgxpool.Pool and provides query methods.
type Pool struct {
	*pgxpool.Pool
}

// ErrInvalidURL marks a DATABASE_URL that can't be parsed. Retrying won't help.
var ErrInvalidURL = errors.New("invalid DATABASE_URL")

// Connect creates a new connection pool and runs migrations.
//
// serverless picks pool defaults for platforms like Vercel, where many short-
// lived instances share the database's connection limit: a few connections
// per instance, none held open while idle. pool_max_conns, pool_min_conns and
// pool_max_conn_idle_time in the URL override the defaults either way.
func Connect(ctx context.Context, databaseURL string, serverless bool) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	maxConns, minConns, idle := int32(20), int32(2), 30*time.Minute
	if serverless {
		maxConns, minConns, idle = 3, 0, 30*time.Second
	}
	if !urlSets(databaseURL, "pool_max_conns") {
		cfg.MaxConns = maxConns
	}
	if !urlSets(databaseURL, "pool_min_conns") {
		cfg.MinConns = minConns
	}
	if !urlSets(databaseURL, "pool_max_conn_idle_time") {
		cfg.MaxConnIdleTime = idle
	}
	cfg.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if err := runMigrations(databaseURL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Pool{pool}, nil
}

// urlSets reports whether a connection string (URL or key=value form) sets key.
func urlSets(databaseURL, key string) bool {
	return strings.Contains(databaseURL, key+"=")
}

// migrationURL drops pgx's pool_* settings from the URL. The migrator connects
// with lib/pq, which sends unknown parameters to the server and fails on them.
func migrationURL(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil || u.RawQuery == "" {
		return databaseURL
	}
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "pool_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func runMigrations(databaseURL string) error {
	d, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("open migrations fs: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, migrationURL(databaseURL))
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
