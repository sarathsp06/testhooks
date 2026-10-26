// Package memstore is an in-memory implementation of the handler Store, for
// running testhooks without PostgreSQL (DATABASE_URL=memory).
//
// Data lives in maps behind one mutex. With a snapshot path set, the whole
// store is written to that file every 30 seconds (when changed) and on
// Close, and loaded again on start: a clean shutdown loses nothing, a crash
// loses at most the last interval.
package memstore

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/sarathsp06/testhooks/internal/db"
)

// ponytail: fixed interval; make it configurable if someone needs tighter crash recovery.
const snapshotInterval = 30 * time.Second

// ponytail: one global lock and full-file snapshots; fine for a debugging
// tool's capped dataset. Move to bbolt if the data grows to hundreds of MB.
type Store struct {
	mu        sync.RWMutex
	endpoints map[string]*db.Endpoint          // by ID
	slugs     map[string]string                // slug -> endpoint ID
	requests  map[string][]*db.CapturedRequest // endpoint ID -> requests, oldest first
	byID      map[string]*db.CapturedRequest   // request ID -> request
	interest  map[string]string                // client ID -> endpoint ID where they asked for a paid plan

	path  string
	dirty atomic.Bool
	log   zerolog.Logger
	stop  chan struct{}
	done  chan struct{}
}

// New returns an empty store, or one loaded from snapshotPath if that file
// exists. An empty snapshotPath keeps everything in memory only.
func New(snapshotPath string, log zerolog.Logger) (*Store, error) {
	s := &Store{
		endpoints: map[string]*db.Endpoint{},
		slugs:     map[string]string{},
		requests:  map[string][]*db.CapturedRequest{},
		byID:      map[string]*db.CapturedRequest{},
		interest:  map[string]string{},
		path:      snapshotPath,
		log:       log.With().Str("component", "memstore").Logger(),
	}
	if snapshotPath == "" {
		return s, nil
	}
	if err := s.load(); err != nil {
		// Refuse to start rather than run empty and later overwrite the file.
		return nil, fmt.Errorf("load snapshot %s: %w", snapshotPath, err)
	}
	s.stop, s.done = make(chan struct{}), make(chan struct{})
	go s.snapshotLoop()
	return s, nil
}

// Ping always succeeds; there is no connection to check.
func (s *Store) Ping(context.Context) error { return nil }

// Close stops the snapshot loop and writes a final snapshot.
func (s *Store) Close() {
	if s.stop == nil {
		return
	}
	close(s.stop)
	<-s.done
	if err := s.Save(); err != nil {
		s.log.Error().Err(err).Msg("final snapshot failed")
	}
}

// --- Endpoints ---

func (s *Store) CreateEndpoint(_ context.Context, slug, name, mode, clientID string, config json.RawMessage) (*db.Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.slugs[slug]; taken {
		return nil, fmt.Errorf("slug %q already exists", slug)
	}
	if config == nil {
		config = json.RawMessage(`{}`)
	}
	ep := &db.Endpoint{ID: newID(), Slug: slug, Name: name, Mode: mode, ClientID: clientID, CreatedAt: time.Now(), Config: config}
	s.endpoints[ep.ID] = ep
	s.slugs[slug] = ep.ID
	s.dirty.Store(true)
	return copyEndpoint(ep), nil
}

func (s *Store) GetEndpointBySlug(_ context.Context, slug string) (*db.Endpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ep, ok := s.endpoints[s.slugs[slug]]
	if !ok {
		return nil, db.ErrNotFound
	}
	return copyEndpoint(ep), nil
}

func (s *Store) GetEndpointByID(_ context.Context, id string) (*db.Endpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ep, ok := s.endpoints[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	return copyEndpoint(ep), nil
}

// ListEndpoints returns endpoints newest first, filtered by clientID when set.
func (s *Store) ListEndpoints(_ context.Context, clientID string, limit, offset int) ([]db.Endpoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s.mu.RLock()
	var out []db.Endpoint
	for _, ep := range s.endpoints {
		if clientID == "" || ep.ClientID == clientID {
			out = append(out, *ep)
		}
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return page(out, limit, offset), nil
}

func (s *Store) UpdateEndpoint(_ context.Context, id, name, mode string, config json.RawMessage) (*db.Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ep, ok := s.endpoints[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	if config == nil {
		config = json.RawMessage(`{}`)
	}
	ep.Name, ep.Mode, ep.Config = name, mode, config
	s.dirty.Store(true)
	return copyEndpoint(ep), nil
}

// DeleteEndpoint removes the endpoint and all its requests.
func (s *Store) DeleteEndpoint(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ep, ok := s.endpoints[id]
	if !ok {
		return nil
	}
	s.dropRequests(id, len(s.requests[id]))
	delete(s.requests, id)
	delete(s.slugs, ep.Slug)
	delete(s.endpoints, id)
	s.dirty.Store(true)
	return nil
}

// --- Requests ---

// InsertRequest stores a copy of r and sets r.ID and r.CreatedAt.
func (s *Store) InsertRequest(_ context.Context, r *db.CapturedRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.endpoints[r.EndpointID]; !ok {
		return fmt.Errorf("endpoint %s: %w", r.EndpointID, db.ErrNotFound)
	}
	r.ID, r.CreatedAt = newID(), time.Now()
	stored := *r
	stored.Signature = nil // computed when served, never stored (same as Postgres)
	s.requests[r.EndpointID] = append(s.requests[r.EndpointID], &stored)
	s.byID[stored.ID] = &stored
	s.dirty.Store(true)
	return nil
}

// ListRequests returns an endpoint's requests newest first.
func (s *Store) ListRequests(_ context.Context, endpointID string, limit, offset int) ([]db.CapturedRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	reqs := s.requests[endpointID]
	out := make([]db.CapturedRequest, 0, len(reqs))
	for i := len(reqs) - 1; i >= 0; i-- {
		out = append(out, *reqs[i])
	}
	return page(out, limit, offset), nil
}

func (s *Store) GetRequest(_ context.Context, id string) (*db.CapturedRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byID[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	c := *r
	return &c, nil
}

// SetRequestForwardResult records the forward target's reply for a request.
// Unknown IDs are ignored, as an UPDATE matching no row is in Postgres.
func (s *Store) SetRequestForwardResult(_ context.Context, id string, result json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.byID[id]; ok {
		r.ForwardResult = append(json.RawMessage(nil), result...)
		s.dirty.Store(true)
	}
	return nil
}

// SetRequestResponseInfo records the response sent back for a request.
func (s *Store) SetRequestResponseInfo(_ context.Context, id string, info json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.byID[id]; ok {
		r.ResponseInfo = append(json.RawMessage(nil), info...)
		s.dirty.Store(true)
	}
	return nil
}

func (s *Store) DeleteRequest(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[id]
	if !ok {
		return nil
	}
	reqs := s.requests[r.EndpointID]
	for i := range reqs {
		if reqs[i].ID == id {
			s.requests[r.EndpointID] = append(reqs[:i:i], reqs[i+1:]...)
			break
		}
	}
	delete(s.byID, id)
	s.dirty.Store(true)
	return nil
}

func (s *Store) DeleteAllRequests(_ context.Context, endpointID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropRequests(endpointID, len(s.requests[endpointID])) > 0 {
		s.dirty.Store(true)
	}
	return nil
}

func (s *Store) CountRequests(_ context.Context, endpointID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.requests[endpointID]), nil
}

// RecordUpgradeInterest notes clientID once and returns how many clients have asked.
func (s *Store) RecordUpgradeInterest(_ context.Context, clientID, endpointID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.interest[clientID]; !seen {
		s.interest[clientID] = endpointID
		s.dirty.Store(true)
	}
	return len(s.interest), nil
}

// --- Pruning (same rules as the Postgres queries) ---

// PruneByStorageBudget keeps each endpoint's newest requests whose cumulative
// size fits in maxBytes and deletes the rest.
func (s *Store) PruneByStorageBudget(_ context.Context, maxBytes int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var pruned int64
	for epID, reqs := range s.requests {
		var total int64
		for i := len(reqs) - 1; i >= 0; i-- {
			total += int64(reqs[i].Size)
			if total > maxBytes {
				pruned += int64(s.dropRequests(epID, i+1))
				break
			}
		}
	}
	if pruned > 0 {
		s.dirty.Store(true)
	}
	return pruned, nil
}

// PruneExcessRequests keeps each endpoint's newest maxCount requests.
func (s *Store) PruneExcessRequests(_ context.Context, maxCount int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var pruned int64
	for epID, reqs := range s.requests {
		if excess := len(reqs) - maxCount; excess > 0 {
			pruned += int64(s.dropRequests(epID, excess))
		}
	}
	if pruned > 0 {
		s.dirty.Store(true)
	}
	return pruned, nil
}

// dropRequests deletes the n oldest requests of an endpoint. Caller holds mu.
func (s *Store) dropRequests(endpointID string, n int) int {
	reqs := s.requests[endpointID]
	n = min(n, len(reqs))
	for _, r := range reqs[:n] {
		delete(s.byID, r.ID)
	}
	// Copy the survivors so the dropped requests can be garbage collected.
	s.requests[endpointID] = append([]*db.CapturedRequest(nil), reqs[n:]...)
	return n
}

// --- Snapshot ---

// snapshot is the on-disk form. gob tolerates added and removed fields, so
// older snapshots still load after the structs change.
type snapshot struct {
	Endpoints []db.Endpoint
	Requests  []db.CapturedRequest // per endpoint, oldest first
	Interest  map[string]string    // client ID -> endpoint ID
}

func (s *Store) snapshotLoop() {
	defer close(s.done)
	t := time.NewTicker(snapshotInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			if err := s.Save(); err != nil {
				s.log.Error().Err(err).Msg("snapshot failed")
			}
		}
	}
}

// Save writes the store to the snapshot path if anything changed since the
// last save. The file is replaced atomically, so it is never half-written.
func (s *Store) Save() error {
	if s.path == "" || !s.dirty.Swap(false) {
		return nil
	}
	var snap snapshot
	s.mu.RLock()
	for _, ep := range s.endpoints {
		snap.Endpoints = append(snap.Endpoints, *ep)
		for _, r := range s.requests[ep.ID] {
			snap.Requests = append(snap.Requests, *r)
		}
	}
	snap.Interest = make(map[string]string, len(s.interest))
	for k, v := range s.interest {
		snap.Interest[k] = v
	}
	s.mu.RUnlock()

	if err := writeAtomic(s.path, &snap); err != nil {
		s.dirty.Store(true) // retry on the next tick
		return err
	}
	return nil
}

func writeAtomic(path string, v any) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	w := bufio.NewWriter(tmp)
	if err := gob.NewEncoder(w).Encode(v); err != nil {
		tmp.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// Persist the rename itself; best effort (not supported on every OS).
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil // first run
	}
	if err != nil {
		return err
	}
	defer f.Close()

	var snap snapshot
	if err := gob.NewDecoder(bufio.NewReader(f)).Decode(&snap); err != nil {
		return err
	}
	for i := range snap.Endpoints {
		ep := snap.Endpoints[i]
		s.endpoints[ep.ID] = &ep
		s.slugs[ep.Slug] = ep.ID
	}
	for i := range snap.Requests {
		r := snap.Requests[i]
		if _, ok := s.endpoints[r.EndpointID]; !ok {
			continue
		}
		s.requests[r.EndpointID] = append(s.requests[r.EndpointID], &r)
		s.byID[r.ID] = &r
	}
	for k, v := range snap.Interest {
		s.interest[k] = v
	}
	s.log.Info().Int("endpoints", len(snap.Endpoints)).Int("requests", len(s.byID)).Msg("snapshot loaded")
	return nil
}

// --- helpers ---

func newID() string { return rand.Text() }

func copyEndpoint(ep *db.Endpoint) *db.Endpoint {
	c := *ep
	return &c
}

func page[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return nil
	}
	items = items[offset:]
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}
