package handler

import (
	"context"
	"encoding/json"

	"github.com/sarathsp06/testhooks/internal/db"
)

// Store is the database interface required by the handler package.
// Defined here so handlers can be tested with mock implementations.
type Store interface {
	// Endpoint operations
	CreateEndpoint(ctx context.Context, slug, name, mode string, config json.RawMessage) (*db.Endpoint, error)
	GetEndpointBySlug(ctx context.Context, slug string) (*db.Endpoint, error)
	GetEndpointByID(ctx context.Context, id string) (*db.Endpoint, error)
	ListEndpoints(ctx context.Context) ([]db.Endpoint, error)
	UpdateEndpoint(ctx context.Context, id, name, mode string, config json.RawMessage) (*db.Endpoint, error)
	DeleteEndpoint(ctx context.Context, id string) error

	// Request operations
	InsertRequest(ctx context.Context, r *db.CapturedRequest) error
	ListRequests(ctx context.Context, endpointID string, limit, offset int) ([]db.CapturedRequest, error)
	GetRequest(ctx context.Context, id string) (*db.CapturedRequest, error)
	DeleteRequest(ctx context.Context, id string) error
	DeleteAllRequests(ctx context.Context, endpointID string) error
	CountRequests(ctx context.Context, endpointID string) (int, error)

	// Pruning (used by cleanup, listed here for completeness)
	PruneByStorageBudget(ctx context.Context, maxBytes int64) (int64, error)
	PruneExcessRequests(ctx context.Context, maxCount int) (int64, error)
}
