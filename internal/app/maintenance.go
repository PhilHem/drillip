package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/PhilHem/drillip/internal/notify"
	"github.com/PhilHem/drillip/internal/store"
)

// maintenance runs periodic housekeeping tasks: auto-resolving stale errors,
// pruning expired silences, and garbage-collecting old occurrences.
type maintenance struct {
	Store        *store.Store
	Notifier     *notify.Notifier // nil if notifications disabled
	ResolveAfter time.Duration
	RetainFor    time.Duration
}

// run starts the maintenance loop, ticking once per hour until ctx is cancelled.
func (m *maintenance) run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.runTask("auto-resolve", m.autoResolve)
			m.runTask("prune-silences", m.pruneSilences)
			m.runTask("gc-occurrences", m.gcOccurrences)
		case <-ctx.Done():
			return
		}
	}
}

func (m *maintenance) runTask(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("maintenance task panicked", "task", name, "panic", r)
		}
	}()
	fn()
}

func (m *maintenance) autoResolve() {
	resolved, err := m.Store.AutoResolve(m.ResolveAfter)
	if err != nil {
		slog.Error("auto-resolve error", "err", err)
		return
	}
	if len(resolved) > 0 {
		slog.Info("auto-resolved errors", "count", len(resolved), "older_than", m.ResolveAfter)
		if m.Notifier != nil {
			go m.Notifier.NotifyResolved(resolved)
		}
	}
}

func (m *maintenance) pruneSilences() {
	pruned, err := m.Store.PruneExpiredSilences()
	if err != nil {
		slog.Error("prune silences error", "err", err)
		return
	}
	if pruned > 0 {
		slog.Info("pruned expired silences", "count", pruned)
	}
}

func (m *maintenance) gcOccurrences() {
	if m.RetainFor <= 0 {
		return
	}
	threshold := time.Now().UTC().Add(-m.RetainFor)
	deleted, err := m.Store.GCOccurrences(threshold)
	if err != nil {
		slog.Error("gc occurrences failed", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("gc: pruned old occurrences", "deleted", deleted, "older_than", m.RetainFor)
	}
}
