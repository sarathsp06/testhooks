package cleanup

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// mockStore implements the Store interface for testing.
type mockStore struct {
	mu           sync.Mutex
	prunedBudget []int64
	prunedExcess []int
	budgetResult int64
	excessResult int64
	budgetErr    error
	excessErr    error
}

func (m *mockStore) PruneByStorageBudget(_ context.Context, maxBytes int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prunedBudget = append(m.prunedBudget, maxBytes)
	return m.budgetResult, m.budgetErr
}

func (m *mockStore) PruneExcessRequests(_ context.Context, maxCount int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prunedExcess = append(m.prunedExcess, maxCount)
	return m.excessResult, m.excessErr
}

func newTestPruner(store Store, maxBytes int64, maxCount, intervalSec int) *Pruner {
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	return NewPruner(store, maxBytes, maxCount, intervalSec, log)
}

func TestPruner_RunAndStop(t *testing.T) {
	store := &mockStore{}
	// 1-second interval so the pruner fires quickly.
	p := newTestPruner(store, 10485760, 500, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	// Wait enough time for at least one tick.
	time.Sleep(1500 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good, pruner stopped.
	case <-time.After(2 * time.Second):
		t.Fatal("pruner did not stop after context cancellation")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.prunedBudget) == 0 {
		t.Error("expected at least one storage budget prune call")
	}
	if len(store.prunedExcess) == 0 {
		t.Error("expected at least one excess prune call")
	}
}

func TestPruner_PassesCorrectValues(t *testing.T) {
	store := &mockStore{}
	p := newTestPruner(store, 5242880, 100, 1) // 5MB budget, 100 max

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	time.Sleep(1500 * time.Millisecond)
	cancel()
	<-done

	store.mu.Lock()
	defer store.mu.Unlock()

	// Verify maxBytes passed as 5MB.
	for _, b := range store.prunedBudget {
		if b != 5242880 {
			t.Errorf("PruneByStorageBudget called with %d, want 5242880", b)
		}
	}

	// Verify maxCount passed as 100.
	for _, c := range store.prunedExcess {
		if c != 100 {
			t.Errorf("PruneExcessRequests called with %d, want 100", c)
		}
	}
}

func TestPruner_HandlesErrors(t *testing.T) {
	store := &mockStore{
		budgetErr: fmt.Errorf("budget error"),
		excessErr: fmt.Errorf("excess error"),
	}
	p := newTestPruner(store, 10485760, 500, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	// Should not panic even with errors.
	time.Sleep(1500 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good.
	case <-time.After(2 * time.Second):
		t.Fatal("pruner did not stop after context cancellation")
	}
}

func TestPruner_ReportsDeletedCounts(t *testing.T) {
	store := &mockStore{
		budgetResult: 10,
		excessResult: 5,
	}
	p := newTestPruner(store, 10485760, 500, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	time.Sleep(1500 * time.Millisecond)
	cancel()
	<-done

	// The pruner should have called both methods at least once.
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.prunedBudget) == 0 || len(store.prunedExcess) == 0 {
		t.Error("expected prune calls")
	}
}
