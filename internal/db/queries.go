package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Endpoint represents a webhook capture endpoint.
type Endpoint struct {
	ID        string          `json:"id"`
	Slug      string          `json:"slug"`
	Name      string          `json:"name"`
	Mode      string          `json:"mode"`
	CreatedAt time.Time       `json:"created_at"`
	Config    json.RawMessage `json:"config"`
}

// CapturedRequest represents a captured inbound HTTP request.
type CapturedRequest struct {
	ID          string          `json:"id"`
	EndpointID  string          `json:"endpoint_id"`
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	Headers     json.RawMessage `json:"headers"`
	Query       json.RawMessage `json:"query,omitempty"`
	Body        []byte          `json:"-"` // custom-serialized by MarshalJSON
	ContentType string          `json:"content_type"`
	IP          string          `json:"ip"`
	Size        int             `json:"size"`
	CreatedAt   time.Time       `json:"created_at"`
}

// isTextBody heuristically determines whether the body is displayable text.
// It first checks the Content-Type, then falls back to UTF-8 validity.
func isTextBody(body []byte, contentType string) bool {
	if len(body) == 0 {
		return true
	}
	// Check content-type first.
	ct := strings.ToLower(contentType)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	textTypes := []string{
		"application/json",
		"application/xml",
		"application/javascript",
		"application/x-www-form-urlencoded",
		"application/graphql",
		"application/soap+xml",
		"application/xhtml+xml",
	}
	for _, t := range textTypes {
		if strings.HasPrefix(ct, t) {
			return true
		}
	}
	// Anything with +json or +xml suffix.
	if strings.Contains(ct, "+json") || strings.Contains(ct, "+xml") {
		return true
	}
	// Fall back to UTF-8 validity check.
	return utf8.Valid(body)
}

// MarshalJSON customises JSON serialisation so that text bodies are emitted
// as plain strings (not base64) and a body_encoding field indicates the format.
func (r *CapturedRequest) MarshalJSON() ([]byte, error) {
	// Alias prevents infinite recursion.
	type Alias CapturedRequest

	if len(r.Body) == 0 {
		type WithEncoding struct {
			*Alias
			Body         *string `json:"body,omitempty"`
			BodyEncoding string  `json:"body_encoding,omitempty"`
		}
		return json.Marshal(&WithEncoding{Alias: (*Alias)(r)})
	}

	if isTextBody(r.Body, r.ContentType) {
		type WithText struct {
			*Alias
			Body         string `json:"body"`
			BodyEncoding string `json:"body_encoding"`
		}
		return json.Marshal(&WithText{
			Alias:        (*Alias)(r),
			Body:         string(r.Body),
			BodyEncoding: "text",
		})
	}

	type WithBase64 struct {
		*Alias
		Body         string `json:"body"`
		BodyEncoding string `json:"body_encoding"`
	}
	return json.Marshal(&WithBase64{
		Alias:        (*Alias)(r),
		Body:         base64.StdEncoding.EncodeToString(r.Body),
		BodyEncoding: "base64",
	})
}

// --- Endpoint queries ---

// CreateEndpoint inserts a new endpoint.
func (p *Pool) CreateEndpoint(ctx context.Context, slug, name, mode string, config json.RawMessage) (*Endpoint, error) {
	if config == nil {
		config = json.RawMessage(`{}`)
	}
	e := &Endpoint{}
	err := p.QueryRow(ctx,
		`INSERT INTO endpoints (slug, name, mode, config)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, slug, name, mode, created_at, config`,
		slug, name, mode, config,
	).Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.CreatedAt, &e.Config)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetEndpointBySlug finds an endpoint by its slug.
func (p *Pool) GetEndpointBySlug(ctx context.Context, slug string) (*Endpoint, error) {
	e := &Endpoint{}
	err := p.QueryRow(ctx,
		`SELECT id, slug, name, mode, created_at, config
		 FROM endpoints WHERE slug = $1`, slug,
	).Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.CreatedAt, &e.Config)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetEndpointByID finds an endpoint by its UUID.
func (p *Pool) GetEndpointByID(ctx context.Context, id string) (*Endpoint, error) {
	e := &Endpoint{}
	err := p.QueryRow(ctx,
		`SELECT id, slug, name, mode, created_at, config
		 FROM endpoints WHERE id = $1`, id,
	).Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.CreatedAt, &e.Config)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// ListEndpoints returns all endpoints, ordered by creation date descending.
func (p *Pool) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	rows, err := p.Query(ctx,
		`SELECT id, slug, name, mode, created_at, config
		 FROM endpoints ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []Endpoint
	for rows.Next() {
		var e Endpoint
		if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.CreatedAt, &e.Config); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, e)
	}
	return endpoints, rows.Err()
}

// UpdateEndpoint updates an endpoint's name, mode, and config.
func (p *Pool) UpdateEndpoint(ctx context.Context, id, name, mode string, config json.RawMessage) (*Endpoint, error) {
	e := &Endpoint{}
	err := p.QueryRow(ctx,
		`UPDATE endpoints SET name = $2, mode = $3, config = $4
		 WHERE id = $1
		 RETURNING id, slug, name, mode, created_at, config`,
		id, name, mode, config,
	).Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.CreatedAt, &e.Config)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// DeleteEndpoint deletes an endpoint and its requests (CASCADE).
func (p *Pool) DeleteEndpoint(ctx context.Context, id string) error {
	_, err := p.Exec(ctx, `DELETE FROM endpoints WHERE id = $1`, id)
	return err
}

// --- Request queries ---

// InsertRequest stores a captured request.
func (p *Pool) InsertRequest(ctx context.Context, r *CapturedRequest) error {
	return p.QueryRow(ctx,
		`INSERT INTO requests (endpoint_id, method, path, headers, query, body, content_type, ip, size)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at`,
		r.EndpointID, r.Method, r.Path, r.Headers, r.Query, r.Body, r.ContentType, r.IP, r.Size,
	).Scan(&r.ID, &r.CreatedAt)
}

// ListRequests returns requests for an endpoint, paginated.
func (p *Pool) ListRequests(ctx context.Context, endpointID string, limit, offset int) ([]CapturedRequest, error) {
	rows, err := p.Query(ctx,
		`SELECT id, endpoint_id, method, path, headers, query, body, content_type, ip, size, created_at
		 FROM requests WHERE endpoint_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		endpointID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []CapturedRequest
	for rows.Next() {
		var r CapturedRequest
		if err := rows.Scan(&r.ID, &r.EndpointID, &r.Method, &r.Path, &r.Headers, &r.Query, &r.Body, &r.ContentType, &r.IP, &r.Size, &r.CreatedAt); err != nil {
			return nil, err
		}
		reqs = append(reqs, r)
	}
	return reqs, rows.Err()
}

// GetRequest returns a single request by ID.
func (p *Pool) GetRequest(ctx context.Context, id string) (*CapturedRequest, error) {
	r := &CapturedRequest{}
	err := p.QueryRow(ctx,
		`SELECT id, endpoint_id, method, path, headers, query, body, content_type, ip, size, created_at
		 FROM requests WHERE id = $1`, id,
	).Scan(&r.ID, &r.EndpointID, &r.Method, &r.Path, &r.Headers, &r.Query, &r.Body, &r.ContentType, &r.IP, &r.Size, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// DeleteRequest deletes a single request by ID.
func (p *Pool) DeleteRequest(ctx context.Context, id string) error {
	_, err := p.Exec(ctx, `DELETE FROM requests WHERE id = $1`, id)
	return err
}

// DeleteAllRequests deletes all requests for an endpoint.
func (p *Pool) DeleteAllRequests(ctx context.Context, endpointID string) error {
	_, err := p.Exec(ctx, `DELETE FROM requests WHERE endpoint_id = $1`, endpointID)
	return err
}

// PruneByStorageBudget deletes the oldest requests per endpoint so that each
// endpoint's total stored body size stays within maxBytes. Returns the total
// number of rows deleted across all endpoints.
func (p *Pool) PruneByStorageBudget(ctx context.Context, maxBytes int64) (int64, error) {
	tag, err := p.Exec(ctx, `
		DELETE FROM requests r
		USING (
			SELECT id,
			       SUM(size) OVER (PARTITION BY endpoint_id ORDER BY created_at DESC) AS cumulative_size
			FROM requests
		) ranked
		WHERE r.id = ranked.id AND ranked.cumulative_size > $1`, maxBytes)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PruneExcessRequests keeps only the most recent maxCount requests per endpoint.
func (p *Pool) PruneExcessRequests(ctx context.Context, maxCount int) (int64, error) {
	tag, err := p.Exec(ctx, `
		DELETE FROM requests r
		USING (
			SELECT id, ROW_NUMBER() OVER (PARTITION BY endpoint_id ORDER BY created_at DESC) AS rn
			FROM requests
		) ranked
		WHERE r.id = ranked.id AND ranked.rn > $1`, maxCount)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountRequests returns the total number of requests for an endpoint.
func (p *Pool) CountRequests(ctx context.Context, endpointID string) (int, error) {
	var count int
	err := p.QueryRow(ctx,
		`SELECT COUNT(*) FROM requests WHERE endpoint_id = $1`, endpointID,
	).Scan(&count)
	return count, err
}

// Ensure pgx import is used.
var _ pgx.Rows = nil
