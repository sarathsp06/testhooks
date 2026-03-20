package cleanup

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// Store is the database interface required by the pruner.
type Store interface {
	PruneByStorageBudget(ctx context.Context, maxBytes int64) (int64, error)
	PruneExcessRequests(ctx context.Context, maxCount int) (int64, error)
}

// Pruner periodically removes old/excess requests from the database.
type Pruner struct {
	db       Store
	maxBytes int64
	maxCount int
	interval time.Duration
	log      zerolog.Logger
}

// NewPruner creates a Pruner with the given settings.
func NewPruner(store Store, maxBytes int64, maxCount, intervalSec int, log zerolog.Logger) *Pruner {
	return &Pruner{
		db:       store,
		maxBytes: maxBytes,
		maxCount: maxCount,
		interval: time.Duration(intervalSec) * time.Second,
		log:      log.With().Str("component", "pruner").Logger(),
	}
}

// Run starts the pruner loop. It blocks until ctx is cancelled.
func (p *Pruner) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.log.Info().
		Int64("max_bytes", p.maxBytes).
		Int("max_count", p.maxCount).
		Dur("interval", p.interval).
		Msg("pruner started")

	for {
		select {
		case <-ctx.Done():
			p.log.Info().Msg("pruner stopped")
			return
		case <-ticker.C:
			p.prune(ctx)
		}
	}
}

func (p *Pruner) prune(ctx context.Context) {
	// Prune by storage budget.
	budgetPruned, err := p.db.PruneByStorageBudget(ctx, p.maxBytes)
	if err != nil {
		p.log.Error().Err(err).Msg("failed to prune by storage budget")
	} else if budgetPruned > 0 {
		p.log.Info().Int64("count", budgetPruned).Msg("pruned requests exceeding storage budget")
	}

	// Prune by count.
	excess, err := p.db.PruneExcessRequests(ctx, p.maxCount)
	if err != nil {
		p.log.Error().Err(err).Msg("failed to prune excess requests")
	} else if excess > 0 {
		p.log.Info().Int64("count", excess).Msg("pruned excess requests")
	}
}
