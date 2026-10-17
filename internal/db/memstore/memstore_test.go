package memstore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"github.com/sarathsp06/testhooks/internal/db"
	"github.com/sarathsp06/testhooks/internal/db/memstore"
	"github.com/sarathsp06/testhooks/internal/handler"
)

var _ handler.Store = (*memstore.Store)(nil)

func ids(reqs []db.CapturedRequest) []string {
	out := make([]string, len(reqs))
	for i, r := range reqs {
		out[i] = string(r.Body)
	}
	return out
}

func TestStore(t *testing.T) {
	ctx := context.Background()
	s, err := memstore.New("", zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}

	ep, err := s.CreateEndpoint(ctx, "abc", "first", "server", "client-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(ep.Config) != "{}" {
		t.Errorf("nil config stored as %q, want {}", ep.Config)
	}
	if _, err := s.CreateEndpoint(ctx, "abc", "dup", "server", "", nil); err == nil {
		t.Error("duplicate slug accepted")
	}
	other, _ := s.CreateEndpoint(ctx, "xyz", "second", "browser", "", nil)

	// Endpoint lists: newest first, optional client filter.
	eps, _ := s.ListEndpoints(ctx, "", 0, 0)
	if len(eps) != 2 || eps[0].ID != other.ID {
		t.Errorf("ListEndpoints = %+v, want 2 endpoints newest first", eps)
	}
	if eps, _ := s.ListEndpoints(ctx, "client-1", 0, 0); len(eps) != 1 || eps[0].ID != ep.ID {
		t.Errorf("client filter = %+v, want only %s", eps, ep.ID)
	}

	// Requests: newest first, ID and timestamp filled in, body sizes for the budget prune.
	for _, body := range []string{"r1", "r2", "r3", "r4"} {
		r := &db.CapturedRequest{EndpointID: ep.ID, Body: []byte(body), Size: 10}
		if err := s.InsertRequest(ctx, r); err != nil || r.ID == "" || r.CreatedAt.IsZero() {
			t.Fatalf("InsertRequest: err=%v id=%q created=%v", err, r.ID, r.CreatedAt)
		}
	}
	reqs, _ := s.ListRequests(ctx, ep.ID, 50, 0)
	if got := ids(reqs); len(got) != 4 || got[0] != "r4" || got[3] != "r1" {
		t.Errorf("ListRequests = %v, want r4..r1", got)
	}
	if page, _ := s.ListRequests(ctx, ep.ID, 2, 1); len(page) != 2 || string(page[0].Body) != "r3" {
		t.Errorf("paging = %v, want [r3 r2]", ids(page))
	}

	// Not found maps to db.ErrNotFound, which handlers turn into 404.
	if _, err := s.GetRequest(ctx, "nope"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetRequest missing: %v, want ErrNotFound", err)
	}
	if _, err := s.GetEndpointBySlug(ctx, "nope"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetEndpointBySlug missing: %v, want ErrNotFound", err)
	}

	// Deleting one request keeps the rest listable (the old mock panicked here).
	if err := s.DeleteRequest(ctx, reqs[1].ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListRequests(ctx, ep.ID, 50, 0); len(got) != 3 {
		t.Errorf("after DeleteRequest: %v, want 3 requests", ids(got))
	}

	// Pruning keeps the newest.
	if n, _ := s.PruneExcessRequests(ctx, 2); n != 1 {
		t.Errorf("PruneExcessRequests pruned %d, want 1", n)
	}
	if n, _ := s.PruneByStorageBudget(ctx, 15); n != 1 {
		t.Errorf("PruneByStorageBudget pruned %d, want 1", n)
	}
	if got, _ := s.ListRequests(ctx, ep.ID, 50, 0); len(got) != 1 || string(got[0].Body) != "r4" {
		t.Errorf("after prunes: %v, want [r4]", ids(got))
	}

	// Deleting an endpoint deletes its requests and frees the slug.
	s.DeleteEndpoint(ctx, ep.ID)
	if n, _ := s.CountRequests(ctx, ep.ID); n != 0 {
		t.Errorf("requests after DeleteEndpoint: %d, want 0", n)
	}
	if _, err := s.CreateEndpoint(ctx, "abc", "reused", "server", "", nil); err != nil {
		t.Errorf("slug not freed by DeleteEndpoint: %v", err)
	}
}

func TestSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "testhooks.snapshot")

	s, err := memstore.New(path, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	ep, _ := s.CreateEndpoint(ctx, "abc", "kept", "server", "", []byte(`{"forward_url":"https://example.com"}`))
	s.InsertRequest(ctx, &db.CapturedRequest{EndpointID: ep.ID, Body: []byte{0, 1, 2, 255}, Size: 4})
	s.Close() // writes the final snapshot

	s2, err := memstore.New(path, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.GetEndpointBySlug(ctx, "abc")
	if err != nil || got.Name != "kept" || string(got.Config) != `{"forward_url":"https://example.com"}` {
		t.Fatalf("restored endpoint = %+v, %v", got, err)
	}
	reqs, _ := s2.ListRequests(ctx, ep.ID, 50, 0)
	if len(reqs) != 1 || string(reqs[0].Body) != "\x00\x01\x02\xff" {
		t.Fatalf("restored requests = %+v", reqs)
	}

	// A corrupt snapshot must stop startup, not be silently replaced.
	os.WriteFile(path, []byte("not a snapshot"), 0o600)
	if _, err := memstore.New(path, zerolog.Nop()); err == nil {
		t.Fatal("corrupt snapshot loaded without error")
	}
}
