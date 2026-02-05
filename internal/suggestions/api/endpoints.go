// Package api provides HTTP/IPC endpoints for the suggestions engine.
// Per spec Section 15.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/search"
	"github.com/runger/clai/internal/suggestions/suggest"
)

// SuggestRequest is the request for /suggest endpoint.
type SuggestRequest struct {
	SessionID string `json:"session_id"`
	RepoKey   string `json:"repo_key,omitempty"`
	LastCmd   string `json:"last_cmd,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// SuggestResponse is the response for /suggest endpoint.
// Per spec Section 15.2.
type SuggestResponse struct {
	Suggestions []SuggestionItem `json:"suggestions"`
	Context     SuggestContext   `json:"context"`
}

// SuggestionItem represents a single suggestion.
type SuggestionItem struct {
	Cmd        string   `json:"cmd"`
	CmdNorm    string   `json:"cmd_norm"`
	Score      float64  `json:"score"`
	Reasons    []string `json:"reasons"`
	Confidence float64  `json:"confidence"`
}

// SuggestContext provides context about the suggestion response.
type SuggestContext struct {
	CacheStatus string `json:"cache"` // "hit" or "miss"
	RepoKey     string `json:"repo_key,omitempty"`
	LastCmdNorm string `json:"last_cmd_norm,omitempty"`
}

// SearchRequest is the request for /search endpoint.
// Per spec Section 15.3.
type SearchRequest struct {
	Query   string `json:"query"`
	RepoKey string `json:"repo_key,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// SearchResponse is the response for /search endpoint.
// Per spec Section 15.3.
type SearchResponse struct {
	Results   []SearchResult `json:"results"`
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated"`
}

// SearchResult represents a single search result.
type SearchResult struct {
	CmdRaw  string `json:"cmd_raw"`
	Ts      int64  `json:"ts"`
	Cwd     string `json:"cwd"`
	RepoKey string `json:"repo_key,omitempty"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// DebugScoresResponse is the response for /debug/scores.
type DebugScoresResponse struct {
	Scores []DebugScoreEntry `json:"scores"`
}

// DebugScoreEntry represents a score entry in debug output.
type DebugScoreEntry struct {
	Scope   string  `json:"scope"`
	CmdNorm string  `json:"cmd_norm"`
	Score   float64 `json:"score"`
	LastTs  int64   `json:"last_ts"`
}

// DebugCacheResponse is the response for /debug/cache.
type DebugCacheResponse struct {
	Entries []DebugCacheEntry `json:"entries"`
	Size    int               `json:"size"`
}

// DebugCacheEntry represents a cache entry in debug output.
type DebugCacheEntry struct {
	SessionID  string `json:"session_id"`
	ComputedAt int64  `json:"computed_at"`
	TTL        int64  `json:"ttl_ms"`
	Count      int    `json:"suggestion_count"`
}

// DebugDiscoveryErrorsResponse is the response for /debug/discovery-errors.
type DebugDiscoveryErrorsResponse struct {
	Errors []discovery.DiscoveryError `json:"errors"`
	Count  int                        `json:"count"`
}

// Handler provides HTTP handlers for the suggestions API.
type Handler struct {
	scorer       *suggest.Scorer
	cache        *suggest.Cache
	searchSvc    *search.Service
	errorTracker *discovery.DiscoveryErrorTracker
	logger       *slog.Logger
}

// HandlerDependencies contains required dependencies for the handler.
type HandlerDependencies struct {
	Scorer       *suggest.Scorer
	Cache        *suggest.Cache
	SearchSvc    *search.Service
	ErrorTracker *discovery.DiscoveryErrorTracker
	Logger       *slog.Logger
}

// NewHandler creates a new API handler.
func NewHandler(deps HandlerDependencies) *Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Handler{
		scorer:       deps.Scorer,
		cache:        deps.Cache,
		searchSvc:    deps.SearchSvc,
		errorTracker: deps.ErrorTracker,
		logger:       deps.Logger,
	}
}

// RegisterRoutes registers all API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /suggest", h.HandleSuggest)
	mux.HandleFunc("POST /search", h.HandleSearch)
	mux.HandleFunc("GET /debug/scores", h.HandleDebugScores)
	mux.HandleFunc("GET /debug/cache", h.HandleDebugCache)
	mux.HandleFunc("GET /debug/discovery-errors", h.HandleDebugDiscoveryErrors)
}

// HandleSuggest handles the /suggest endpoint.
// Per spec Section 15.2.
func (h *Handler) HandleSuggest(w http.ResponseWriter, r *http.Request) {
	var req SuggestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request body")
		return
	}

	if req.SessionID == "" {
		h.writeError(w, http.StatusBadRequest, "missing_session_id", "session_id is required")
		return
	}

	ctx := r.Context()
	cacheStatus := "miss"

	// Try cache first per spec Section 11.2.3
	var suggestions []suggest.Suggestion
	if h.cache != nil {
		// Try to get from cache (we don't have event ID here, so use GetAny)
		if cached, ok := h.cache.GetAny(req.SessionID); ok {
			suggestions = cached
			cacheStatus = "hit"
		}
	}

	// Compute if cache miss
	if suggestions == nil && h.scorer != nil {
		nowMs := time.Now().UnixMilli()
		suggestCtx := suggest.SuggestContext{
			SessionID: req.SessionID,
			RepoKey:   req.RepoKey,
			LastCmd:   req.LastCmd,
			Cwd:       req.Cwd,
			NowMs:     nowMs,
		}

		var err error
		suggestions, err = h.scorer.Suggest(ctx, suggestCtx)
		if err != nil {
			h.logger.Error("suggestion failed", "error", err)
			h.writeError(w, http.StatusInternalServerError, "suggest_failed", "Failed to compute suggestions")
			return
		}

		// Populate cache
		if h.cache != nil {
			h.cache.Set(req.SessionID, 0, suggestions)
		}
	}

	// Limit results if requested
	limit := req.Limit
	if limit <= 0 || limit > len(suggestions) {
		limit = len(suggestions)
	}
	suggestions = suggestions[:limit]

	// Build response
	items := make([]SuggestionItem, len(suggestions))
	for i, s := range suggestions {
		items[i] = SuggestionItem{
			Cmd:        s.Command,
			CmdNorm:    s.Command, // cmd_norm is the same as Command in our implementation
			Score:      s.Score,
			Reasons:    s.Reasons,
			Confidence: s.Confidence,
		}
	}

	resp := SuggestResponse{
		Suggestions: items,
		Context: SuggestContext{
			CacheStatus: cacheStatus,
			RepoKey:     req.RepoKey,
			LastCmdNorm: req.LastCmd,
		},
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// HandleSearch handles the /search endpoint.
// Per spec Section 15.3.
func (h *Handler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request body")
		return
	}

	if req.Query == "" {
		h.writeError(w, http.StatusBadRequest, "missing_query", "query is required")
		return
	}

	// Check if search service is available
	if h.searchSvc == nil {
		h.writeError(w, http.StatusServiceUnavailable, "fts5_unavailable",
			"History search requires SQLite with FTS5. Rebuild SQLite or use pattern matching.")
		return
	}

	// Check if FTS5 is available
	if !h.searchSvc.FTS5Available() && !h.searchSvc.FallbackEnabled() {
		h.writeError(w, http.StatusServiceUnavailable, "fts5_unavailable",
			"History search requires SQLite with FTS5. Rebuild SQLite or use pattern matching.")
		return
	}

	ctx := r.Context()

	// Set default limit
	limit := req.Limit
	if limit <= 0 {
		limit = search.DefaultLimit
	}

	// Perform search
	results, err := h.searchSvc.Search(ctx, req.Query, search.SearchOptions{
		RepoKey: req.RepoKey,
		Cwd:     req.Cwd,
		Limit:   limit,
	})
	if err != nil {
		h.logger.Error("search failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "search_failed", "Failed to search history")
		return
	}

	// Build response
	items := make([]SearchResult, len(results))
	for i, r := range results {
		items[i] = SearchResult{
			CmdRaw:  r.CmdRaw,
			Ts:      r.Timestamp,
			Cwd:     r.Cwd,
			RepoKey: r.RepoKey,
		}
	}

	// Check if truncated
	truncated := len(results) >= limit

	resp := SearchResponse{
		Results:   items,
		Total:     len(items),
		Truncated: truncated,
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// HandleDebugScores handles the /debug/scores endpoint.
// Per spec Section 15.4.
func (h *Handler) HandleDebugScores(w http.ResponseWriter, r *http.Request) {
	// This would need a direct DB query - for now return empty
	// In production, this would query the command_score table
	resp := DebugScoresResponse{
		Scores: []DebugScoreEntry{},
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// HandleDebugCache handles the /debug/cache endpoint.
// Per spec Section 15.4.
func (h *Handler) HandleDebugCache(w http.ResponseWriter, r *http.Request) {
	var entries []DebugCacheEntry
	size := 0

	if h.cache != nil {
		size = h.cache.Size()
		// Note: We don't expose internal cache entries directly
		// This is a simplified implementation
	}

	resp := DebugCacheResponse{
		Entries: entries,
		Size:    size,
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// HandleDebugDiscoveryErrors handles the /debug/discovery-errors endpoint.
// Per spec Section 10.2.1.
func (h *Handler) HandleDebugDiscoveryErrors(w http.ResponseWriter, r *http.Request) {
	var errors []discovery.DiscoveryError
	count := 0

	if h.errorTracker != nil {
		errors = h.errorTracker.GetRecent(100)
		count = h.errorTracker.Count()
	}

	resp := DebugDiscoveryErrorsResponse{
		Errors: errors,
		Count:  count,
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// writeJSON writes a JSON response.
func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to write response", "error", err)
	}
}

// writeError writes an error response.
func (h *Handler) writeError(w http.ResponseWriter, status int, errorCode, message string) {
	h.writeJSON(w, status, ErrorResponse{
		Error:   errorCode,
		Message: message,
	})
}

// SuggestFromContext generates suggestions from a context (for pre-computation).
func (h *Handler) SuggestFromContext(ctx context.Context, suggestCtx suggest.SuggestContext) ([]suggest.Suggestion, error) {
	if h.scorer == nil {
		return nil, nil
	}
	return h.scorer.Suggest(ctx, suggestCtx)
}

// UpdateCache updates the suggestion cache for a session.
func (h *Handler) UpdateCache(sessionID string, eventID int64, suggestions []suggest.Suggestion) {
	if h.cache != nil {
		h.cache.Set(sessionID, eventID, suggestions)
	}
}

// InvalidateCache invalidates the cache for a session.
func (h *Handler) InvalidateCache(sessionID string) {
	if h.cache != nil {
		h.cache.Invalidate(sessionID)
	}
}
