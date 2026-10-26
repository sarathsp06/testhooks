package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sarathsp06/testhooks/internal/signature"
)

// ErrNotFound is returned by lookups that match no row. Callers use it to
// distinguish a genuinely missing record from a database failure.
var ErrNotFound = errors.New("not found")

// mapNotFound converts "no row" errors (and malformed UUID input, which can
// never match a row) into ErrNotFound, leaving real database errors intact.
func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" { // invalid_text_representation
		return ErrNotFound
	}
	return err
}

// Endpoint represents a webhook capture endpoint.
type Endpoint struct {
	ID        string          `json:"id"`
	Slug      string          `json:"slug"`
	Name      string          `json:"name"`
	Mode      string          `json:"mode"`
	ClientID  string          `json:"client_id,omitempty"`
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

	// ForwardResult is the forward target's reply and ResponseInfo the
	// response sent back to the caller, in the same JSON shape as the
	// forward_result and response_info WebSocket messages. Set after insert.
	ForwardResult json.RawMessage `json:"forward_result,omitempty"`
	ResponseInfo  json.RawMessage `json:"response_info,omitempty"`

	// Signature is the Standard Webhooks verification result. Not stored:
	// computed from the endpoint's config when the request is served.
	Signature *signature.Result `json:"signature,omitempty"`
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
func (p *Pool) CreateEndpoint(ctx context.Context, slug, name, mode, clientID string, config json.RawMessage) (*Endpoint, error) {
	if config == nil {
		config = json.RawMessage(`{}`)
	}
	e := &Endpoint{}
	err := p.QueryRow(ctx,
		`INSERT INTO endpoints (slug, name, mode, client_id, config)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, slug, name, mode, COALESCE(client_id, ''), created_at, config`,
		slug, name, mode, nilIfEmpty(clientID), config,
	).Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.ClientID, &e.CreatedAt, &e.Config)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetEndpointBySlug finds an endpoint by its slug.
func (p *Pool) GetEndpointBySlug(ctx context.Context, slug string) (*Endpoint, error) {
	e := &Endpoint{}
	err := p.QueryRow(ctx,
		`SELECT id, slug, name, mode, COALESCE(client_id, ''), created_at, config
		 FROM endpoints WHERE slug = $1`, slug,
	).Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.ClientID, &e.CreatedAt, &e.Config)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return e, nil
}

// GetEndpointByID finds an endpoint by its UUID.
func (p *Pool) GetEndpointByID(ctx context.Context, id string) (*Endpoint, error) {
	e := &Endpoint{}
	err := p.QueryRow(ctx,
		`SELECT id, slug, name, mode, COALESCE(client_id, ''), created_at, config
		 FROM endpoints WHERE id = $1`, id,
	).Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.ClientID, &e.CreatedAt, &e.Config)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return e, nil
}

// ListEndpoints returns endpoints paginated, ordered by creation date descending.
// H-03: Added LIMIT/OFFSET to prevent unbounded result sets.
// When clientID is non-empty, only endpoints matching that client_id are returned.
func (p *Pool) ListEndpoints(ctx context.Context, clientID string, limit, offset int) ([]Endpoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var rows pgx.Rows
	var err error
	if clientID != "" {
		rows, err = p.Query(ctx,
			`SELECT id, slug, name, mode, COALESCE(client_id, ''), created_at, config
			 FROM endpoints WHERE client_id = $1
			 ORDER BY created_at DESC
			 LIMIT $2 OFFSET $3`, clientID, limit, offset)
	} else {
		rows, err = p.Query(ctx,
			`SELECT id, slug, name, mode, COALESCE(client_id, ''), created_at, config
			 FROM endpoints ORDER BY created_at DESC
			 LIMIT $1 OFFSET $2`, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []Endpoint
	for rows.Next() {
		var e Endpoint
		if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.ClientID, &e.CreatedAt, &e.Config); err != nil {
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
		 RETURNING id, slug, name, mode, COALESCE(client_id, ''), created_at, config`,
		id, name, mode, config,
	).Scan(&e.ID, &e.Slug, &e.Name, &e.Mode, &e.ClientID, &e.CreatedAt, &e.Config)
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
		`SELECT id, endpoint_id, method, path, headers, query, body, content_type, ip, size, created_at, forward_result, response_info
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
		if err := rows.Scan(&r.ID, &r.EndpointID, &r.Method, &r.Path, &r.Headers, &r.Query, &r.Body, &r.ContentType, &r.IP, &r.Size, &r.CreatedAt, &r.ForwardResult, &r.ResponseInfo); err != nil {
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
		`SELECT id, endpoint_id, method, path, headers, query, body, content_type, ip, size, created_at, forward_result, response_info
		 FROM requests WHERE id = $1`, id,
	).Scan(&r.ID, &r.EndpointID, &r.Method, &r.Path, &r.Headers, &r.Query, &r.Body, &r.ContentType, &r.IP, &r.Size, &r.CreatedAt, &r.ForwardResult, &r.ResponseInfo)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return r, nil
}

// SetRequestForwardResult records the forward target's reply for a request.
func (p *Pool) SetRequestForwardResult(ctx context.Context, id string, result json.RawMessage) error {
	_, err := p.Exec(ctx, `UPDATE requests SET forward_result = $2 WHERE id = $1`, id, result)
	return err
}

// SetRequestResponseInfo records the response sent back for a request.
func (p *Pool) SetRequestResponseInfo(ctx context.Context, id string, info json.RawMessage) error {
	_, err := p.Exec(ctx, `UPDATE requests SET response_info = $2 WHERE id = $1`, id, info)
	return err
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

// RecordUpgradeInterest stores one row per client and returns the number of
// clients who have asked for a paid plan.
func (p *Pool) RecordUpgradeInterest(ctx context.Context, clientID, endpointID string) (int, error) {
	if _, err := p.Exec(ctx,
		`INSERT INTO upgrade_interest (client_id, endpoint_id) VALUES ($1, $2) ON CONFLICT (client_id) DO NOTHING`,
		clientID, endpointID,
	); err != nil {
		return 0, err
	}
	var count int
	err := p.QueryRow(ctx, `SELECT COUNT(*) FROM upgrade_interest`).Scan(&count)
	return count, err
}

// Ensure pgx import is used.
var _ pgx.Rows = nil

// nilIfEmpty returns nil if s is empty, otherwise returns &s.
// Used to store empty strings as SQL NULL for optional columns like client_id.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
