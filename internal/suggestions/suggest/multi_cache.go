package suggest

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// Hot-path deadline constants per spec.
const (
	// CandidateRetrievalDeadline is the max time to retrieve candidates (L1+L2+L3).
	CandidateRetrievalDeadline = 20 * time.Millisecond

	// RankingDeadline is the max time for ranking candidates.
	RankingDeadline = 10 * time.Millisecond

	// HardTimeout is the absolute max time for the entire suggestion pipeline.
	HardTimeout = 150 * time.Millisecond
)

// DefaultMemoryBudgetBytes is the default memory budget for L1+L2 caches (50 MB).
const DefaultMemoryBudgetBytes = 50 * 1024 * 1024

// MultiCache is the multi-layer suggestion cache orchestrator.
// It coordinates L1 (per-session), L2 (per-repo), and L3 (SQLite) caches
// with memory budget enforcement and async precompute support.
type MultiCache struct {
	l1           *L1Cache
	l2           *L2Cache
	l3           *L3Cache
	metrics      *CacheMetrics
	logger       *slog.Logger
	precompute   *PrecomputeTracker
	memoryBudget int64
	pressureMu   sync.Mutex
}

// MultiCacheConfig configures the multi-layer cache.
type MultiCacheConfig struct {
	DB           *sql.DB
	Logger       *slog.Logger
	L1Capacity   int
	L2Capacity   int
	TTL          time.Duration
	MemoryBudget int64
}

// NewMultiCache creates a new multi-layer cache.
func NewMultiCache(cfg MultiCacheConfig) *MultiCache {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MemoryBudget <= 0 {
		cfg.MemoryBudget = DefaultMemoryBudgetBytes
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultCacheTTL
	}

	metrics := &CacheMetrics{}

	mc := &MultiCache{
		l1:           NewL1Cache(cfg.L1Capacity, cfg.TTL, metrics),
		l2:           NewL2Cache(cfg.L2Capacity, cfg.TTL, metrics),
		l3:           NewL3Cache(cfg.DB, cfg.TTL, metrics),
		metrics:      metrics,
		memoryBudget: cfg.MemoryBudget,
		logger:       cfg.Logger,
		precompute:   NewPrecomputeTracker(),
	}

	return mc
}

// NewMultiCacheFromConfig creates a MultiCache from config values.
func NewMultiCacheFromConfig(cacheTTLMs, memoryBudgetMB int, db *sql.DB, logger *slog.Logger) *MultiCache {
	ttl := time.Duration(cacheTTLMs) * time.Millisecond
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}

	budget := int64(memoryBudgetMB) * 1024 * 1024
	if budget <= 0 {
		budget = DefaultMemoryBudgetBytes
	}

	return NewMultiCache(MultiCacheConfig{
		L1Capacity:   DefaultL1Capacity,
		L2Capacity:   DefaultL2Capacity,
		TTL:          ttl,
		MemoryBudget: budget,
		DB:           db,
		Logger:       logger,
	})
}

// Get retrieves suggestions from the cache hierarchy: L1 -> L2 -> L3.
// On hit at L2/L3, the result is promoted to higher layers.
// Respects CandidateRetrievalDeadline for the lookup.
func (mc *MultiCache) Get(ctx context.Context, sessionID string, lastEventID int64, repoKey, prefixHash, contextHash string) ([]Suggestion, CacheLayer) {
	// Apply retrieval deadline
	ctx, cancel := context.WithTimeout(ctx, CandidateRetrievalDeadline)
	defer cancel()

	// L1: per-session hot cache
	l1Key := MakeL1Key(sessionID, lastEventID, prefixHash)
	if suggestions, ok := mc.l1.Get(l1Key); ok {
		return suggestions, CacheLayerL1
	}

	// L2: per-repo cold cache
	if repoKey != "" {
		if suggestions, ok := mc.l2.Get(repoKey); ok {
			// Promote to L1
			mc.l1.Set(l1Key, suggestions)
			return suggestions, CacheLayerL2
		}
	}

	// L3: SQLite persistent cache
	l3Key := MakeL3CacheKey(sessionID, contextHash)
	if suggestions, ok := mc.l3.Get(ctx, l3Key); ok {
		// Promote to L1 and L2
		mc.l1.Set(l1Key, suggestions)
		if repoKey != "" {
			mc.l2.Set(repoKey, suggestions)
		}
		return suggestions, CacheLayerL3
	}

	return nil, CacheLayerMiss
}

// Set stores suggestions in all cache layers.
// L1 and L2 are set synchronously; L3 is set asynchronously.
func (mc *MultiCache) Set(ctx context.Context, sessionID string, lastEventID int64, repoKey, prefixHash, contextHash string, suggestions []Suggestion) {
	// L1: always set
	l1Key := MakeL1Key(sessionID, lastEventID, prefixHash)
	mc.l1.Set(l1Key, suggestions)

	// L2: set if repo key is available
	if repoKey != "" {
		mc.l2.Set(repoKey, suggestions)
	}

	// L3: set asynchronously to avoid blocking the hot path
	go func() {
		l3Key := MakeL3CacheKey(sessionID, contextHash)
		if err := mc.l3.Set(ctx, l3Key, sessionID, contextHash, suggestions); err != nil {
			mc.logger.Debug("L3 cache set failed", "error", err)
		}
	}()

	// Check memory budget after adding entries
	mc.enforceMemoryBudget()
}

// OnCommandEnd handles the command_end event.
// Per spec: invalidates L1 for the session to clear stale suggestions.
func (mc *MultiCache) OnCommandEnd(sessionID string) {
	mc.l1.InvalidateSession(sessionID)
}

// OnContextChange handles context changes (e.g., directory change, repo change).
// Per spec: invalidates L1 and L2 to force fresh computation.
func (mc *MultiCache) OnContextChange(sessionID, repoKey string) {
	mc.l1.InvalidateSession(sessionID)
	if repoKey != "" {
		mc.l2.InvalidateRepo(repoKey)
	}
}

// InvalidateAll clears all cache layers.
func (mc *MultiCache) InvalidateAll(ctx context.Context) {
	mc.l1.InvalidateAll()
	mc.l2.InvalidateAll()
	_ = mc.l3.InvalidateSession(ctx, "")
}

// enforceMemoryBudget evicts entries if memory exceeds the budget.
// Per spec: evict L2 first, then L1.
func (mc *MultiCache) enforceMemoryBudget() {
	mc.pressureMu.Lock()
	defer mc.pressureMu.Unlock()

	totalSize := mc.l1.MemorySize() + mc.l2.MemorySize()
	if totalSize <= mc.memoryBudget {
		return
	}

	mc.logger.Debug("memory pressure detected",
		"total_bytes", totalSize,
		"budget_bytes", mc.memoryBudget,
	)

	// Evict L2 first (cold cache)
	overBudget := totalSize - mc.memoryBudget
	l2Size := mc.l2.MemorySize()
	if l2Size > 0 {
		targetL2 := l2Size - overBudget
		if targetL2 < 0 {
			targetL2 = 0
		}
		evicted := mc.l2.EvictToSize(targetL2)
		if evicted > 0 {
			mc.logger.Debug("evicted L2 entries", "count", evicted)
		}
	}

	// Recheck after L2 eviction
	totalSize = mc.l1.MemorySize() + mc.l2.MemorySize()
	if totalSize <= mc.memoryBudget {
		return
	}

	// Evict L1 if still over budget
	overBudget = totalSize - mc.memoryBudget
	l1Size := mc.l1.MemorySize()
	targetL1 := l1Size - overBudget
	if targetL1 < 0 {
		targetL1 = 0
	}
	evicted := mc.l1.EvictToSize(targetL1)
	if evicted > 0 {
		mc.logger.Debug("evicted L1 entries", "count", evicted)
	}
}

// TriggerPrecompute schedules an async precompute for the given context.
// Uses PrecomputeTracker for deduplication.
// The computeFunc should compute and return suggestions.
func (mc *MultiCache) TriggerPrecompute(
	ctx context.Context,
	sessionID string,
	lastEventID int64,
	repoKey, prefixHash, contextHash string,
	computeFunc func(ctx context.Context) ([]Suggestion, error),
) {
	precomputeKey := MakeL3CacheKey(sessionID, contextHash)
	if !mc.precompute.TryAcquire(precomputeKey) {
		return // Already in-flight
	}

	go func() {
		// Use a separate context with hard timeout for precompute
		preCtx, cancel := context.WithTimeout(context.Background(), HardTimeout)
		defer cancel()

		suggestions, err := computeFunc(preCtx)
		mc.precompute.Release(precomputeKey, err)

		if err != nil {
			mc.logger.Debug("precompute failed", "error", err, "key", precomputeKey)
			return
		}

		mc.Set(ctx, sessionID, lastEventID, repoKey, prefixHash, contextHash, suggestions)
	}()
}

// Metrics returns the shared metrics instance.
func (mc *MultiCache) Metrics() *CacheMetrics {
	return mc.metrics
}

// MetricsSnapshot returns a point-in-time snapshot of cache metrics.
func (mc *MultiCache) MetricsSnapshot() MetricsSnapshot {
	return mc.metrics.Snapshot()
}

// L1 returns the L1 cache for direct access.
func (mc *MultiCache) L1() *L1Cache {
	return mc.l1
}

// L2 returns the L2 cache for direct access.
func (mc *MultiCache) L2() *L2Cache {
	return mc.l2
}

// L3 returns the L3 cache for direct access.
func (mc *MultiCache) L3() *L3Cache {
	return mc.l3
}

// PrecomputeTrackerInstance returns the precompute tracker.
func (mc *MultiCache) PrecomputeTrackerInstance() *PrecomputeTracker {
	return mc.precompute
}

// Cleanup removes expired entries from L3.
func (mc *MultiCache) Cleanup(ctx context.Context) (int64, error) {
	return mc.l3.Cleanup(ctx)
}
