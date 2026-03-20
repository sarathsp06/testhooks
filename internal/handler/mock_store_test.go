package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sarathsp06/testhooks/internal/db"
)

// Ensure mockStore satisfies the Store interface at compile time.
var _ Store = (*mockStore)(nil)

// mockStore implements Store for testing handlers without a real database.
type mockStore struct {
	mu sync.Mutex

	endpoints map[string]*db.Endpoint // keyed by ID
	slugIndex map[string]string       // slug → ID
	requests  map[string]*db.CapturedRequest
	reqOrder  []string // request IDs in insertion order

	// Error injection
	createEndpointErr    error
	getEndpointBySlugErr error
	getEndpointByIDErr   error
	listEndpointsErr     error
	updateEndpointErr    error
	deleteEndpointErr    error
	insertRequestErr     error
	listRequestsErr      error
	getRequestErr        error
	deleteRequestErr     error
	deleteAllRequestsErr error
	countRequestsErr     error
}

func newMockStore() *mockStore {
	return &mockStore{
		endpoints: make(map[string]*db.Endpoint),
		slugIndex: make(map[string]string),
		requests:  make(map[string]*db.CapturedRequest),
	}
}

func (m *mockStore) CreateEndpoint(_ context.Context, slug, name, mode string, config json.RawMessage) (*db.Endpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createEndpointErr != nil {
		return nil, m.createEndpointErr
	}
	id := fmt.Sprintf("ep-%d", len(m.endpoints)+1)
	if config == nil {
		config = json.RawMessage(`{}`)
	}
	ep := &db.Endpoint{
		ID:        id,
		Slug:      slug,
		Name:      name,
		Mode:      mode,
		CreatedAt: time.Now(),
		Config:    config,
	}
	m.endpoints[id] = ep
	m.slugIndex[slug] = id
	return ep, nil
}

func (m *mockStore) GetEndpointBySlug(_ context.Context, slug string) (*db.Endpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getEndpointBySlugErr != nil {
		return nil, m.getEndpointBySlugErr
	}
	id, ok := m.slugIndex[slug]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return m.endpoints[id], nil
}

func (m *mockStore) GetEndpointByID(_ context.Context, id string) (*db.Endpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getEndpointByIDErr != nil {
		return nil, m.getEndpointByIDErr
	}
	ep, ok := m.endpoints[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return ep, nil
}

func (m *mockStore) ListEndpoints(_ context.Context) ([]db.Endpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listEndpointsErr != nil {
		return nil, m.listEndpointsErr
	}
	var result []db.Endpoint
	for _, ep := range m.endpoints {
		result = append(result, *ep)
	}
	return result, nil
}

func (m *mockStore) UpdateEndpoint(_ context.Context, id, name, mode string, config json.RawMessage) (*db.Endpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateEndpointErr != nil {
		return nil, m.updateEndpointErr
	}
	ep, ok := m.endpoints[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	ep.Name = name
	ep.Mode = mode
	ep.Config = config
	return ep, nil
}

func (m *mockStore) DeleteEndpoint(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteEndpointErr != nil {
		return m.deleteEndpointErr
	}
	ep, ok := m.endpoints[id]
	if ok {
		delete(m.slugIndex, ep.Slug)
		delete(m.endpoints, id)
	}
	return nil
}

func (m *mockStore) InsertRequest(_ context.Context, r *db.CapturedRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertRequestErr != nil {
		return m.insertRequestErr
	}
	r.ID = fmt.Sprintf("req-%d", len(m.requests)+1)
	r.CreatedAt = time.Now()
	m.requests[r.ID] = r
	m.reqOrder = append(m.reqOrder, r.ID)
	return nil
}

func (m *mockStore) ListRequests(_ context.Context, endpointID string, limit, offset int) ([]db.CapturedRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listRequestsErr != nil {
		return nil, m.listRequestsErr
	}
	var result []db.CapturedRequest
	for _, id := range m.reqOrder {
		r := m.requests[id]
		if r.EndpointID == endpointID {
			result = append(result, *r)
		}
	}
	// Apply offset/limit.
	if offset >= len(result) {
		return nil, nil
	}
	result = result[offset:]
	if limit < len(result) {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockStore) GetRequest(_ context.Context, id string) (*db.CapturedRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getRequestErr != nil {
		return nil, m.getRequestErr
	}
	r, ok := m.requests[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return r, nil
}

func (m *mockStore) DeleteRequest(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteRequestErr != nil {
		return m.deleteRequestErr
	}
	delete(m.requests, id)
	return nil
}

func (m *mockStore) DeleteAllRequests(_ context.Context, endpointID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteAllRequestsErr != nil {
		return m.deleteAllRequestsErr
	}
	for id, r := range m.requests {
		if r.EndpointID == endpointID {
			delete(m.requests, id)
		}
	}
	return nil
}

func (m *mockStore) CountRequests(_ context.Context, endpointID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.countRequestsErr != nil {
		return 0, m.countRequestsErr
	}
	count := 0
	for _, r := range m.requests {
		if r.EndpointID == endpointID {
			count++
		}
	}
	return count, nil
}

func (m *mockStore) PruneByStorageBudget(_ context.Context, _ int64) (int64, error) {
	return 0, nil
}

func (m *mockStore) PruneExcessRequests(_ context.Context, _ int) (int64, error) {
	return 0, nil
}

// seedEndpoint is a helper to pre-populate an endpoint in the mock store.
func (m *mockStore) seedEndpoint(id, slug, name, mode string, config json.RawMessage) *db.Endpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	if config == nil {
		config = json.RawMessage(`{}`)
	}
	ep := &db.Endpoint{
		ID:        id,
		Slug:      slug,
		Name:      name,
		Mode:      mode,
		CreatedAt: time.Now(),
		Config:    config,
	}
	m.endpoints[id] = ep
	m.slugIndex[slug] = id
	return ep
}

// seedRequest is a helper to pre-populate a request in the mock store.
func (m *mockStore) seedRequest(id, endpointID, method, path string) *db.CapturedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := &db.CapturedRequest{
		ID:          id,
		EndpointID:  endpointID,
		Method:      method,
		Path:        path,
		Headers:     json.RawMessage(`{}`),
		ContentType: "application/json",
		CreatedAt:   time.Now(),
	}
	m.requests[id] = r
	m.reqOrder = append(m.reqOrder, id)
	return r
}
