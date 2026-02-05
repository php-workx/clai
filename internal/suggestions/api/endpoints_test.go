package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/search"
	"github.com/runger/clai/internal/suggestions/suggest"

	_ "modernc.org/sqlite"
)

// createTestDB creates a temporary SQLite database for testing.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir, err := os.MkdirTemp("", "clai-api-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Create all required tables
	_, err = db.Exec(`
		CREATE TABLE command_event (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id    TEXT NOT NULL,
			ts            INTEGER NOT NULL,
			cmd_raw       TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			cwd           TEXT NOT NULL,
			repo_key      TEXT,
			ephemeral     INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE command_score (
			scope     TEXT NOT NULL,
			cmd_norm  TEXT NOT NULL,
			score     REAL NOT NULL,
			last_ts   INTEGER NOT NULL,
			PRIMARY KEY(scope, cmd_norm)
		);

		CREATE TABLE transition (
			scope         TEXT NOT NULL,
			prev_norm     TEXT NOT NULL,
			next_norm     TEXT NOT NULL,
			count         INTEGER NOT NULL,
			last_ts       INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_norm, next_norm)
		);

		CREATE TABLE project_task (
			repo_key      TEXT NOT NULL,
			kind          TEXT NOT NULL,
			name          TEXT NOT NULL,
			command       TEXT NOT NULL,
			description   TEXT,
			discovered_ts INTEGER NOT NULL,
			PRIMARY KEY(repo_key, kind, name)
		);
	`)
	require.NoError(t, err)

	return db
}

func TestHandler_NewHandler(t *testing.T) {
	t.Parallel()

	handler := NewHandler(HandlerDependencies{})
	assert.NotNil(t, handler)
}

func TestHandler_HandleSuggest_CacheHit(t *testing.T) {
	t.Parallel()

	cache := suggest.NewCache(suggest.DefaultCacheConfig())

	// Pre-populate cache
	cache.Set("session123", 1, []suggest.Suggestion{
		{Command: "git status", Score: 100, Confidence: 0.9, Reasons: []string{"cache"}},
	})

	handler := NewHandler(HandlerDependencies{
		Cache: cache,
	})

	req := SuggestRequest{
		SessionID: "session123",
	}
	body, _ := json.Marshal(req)

	r := httptest.NewRequest("POST", "/suggest", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleSuggest(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SuggestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "hit", resp.Context.CacheStatus)
	assert.Len(t, resp.Suggestions, 1)
	assert.Equal(t, "git status", resp.Suggestions[0].Cmd)
}

func TestHandler_HandleSuggest_CacheMiss(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	cache := suggest.NewCache(suggest.DefaultCacheConfig())

	scorer, err := suggest.NewScorer(suggest.ScorerDependencies{DB: db}, suggest.DefaultScorerConfig())
	require.NoError(t, err)

	handler := NewHandler(HandlerDependencies{
		Scorer: scorer,
		Cache:  cache,
	})

	req := SuggestRequest{
		SessionID: "session456",
	}
	body, _ := json.Marshal(req)

	r := httptest.NewRequest("POST", "/suggest", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleSuggest(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SuggestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "miss", resp.Context.CacheStatus)
}

func TestHandler_HandleSuggest_MissingSessionID(t *testing.T) {
	t.Parallel()

	handler := NewHandler(HandlerDependencies{})

	req := SuggestRequest{}
	body, _ := json.Marshal(req)

	r := httptest.NewRequest("POST", "/suggest", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleSuggest(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "missing_session_id", resp.Error)
}

func TestHandler_HandleSuggest_InvalidJSON(t *testing.T) {
	t.Parallel()

	handler := NewHandler(HandlerDependencies{})

	r := httptest.NewRequest("POST", "/suggest", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	handler.HandleSuggest(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "invalid_request", resp.Error)
}

func TestHandler_HandleSuggest_WithLimit(t *testing.T) {
	t.Parallel()

	cache := suggest.NewCache(suggest.DefaultCacheConfig())

	// Pre-populate cache with multiple suggestions
	cache.Set("session123", 1, []suggest.Suggestion{
		{Command: "git status", Score: 100},
		{Command: "git commit", Score: 80},
		{Command: "git push", Score: 60},
	})

	handler := NewHandler(HandlerDependencies{
		Cache: cache,
	})

	req := SuggestRequest{
		SessionID: "session123",
		Limit:     2,
	}
	body, _ := json.Marshal(req)

	r := httptest.NewRequest("POST", "/suggest", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleSuggest(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SuggestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Suggestions, 2)
}

func TestHandler_HandleSearch_FTS5Unavailable(t *testing.T) {
	t.Parallel()

	handler := NewHandler(HandlerDependencies{})

	req := SearchRequest{
		Query: "docker",
	}
	body, _ := json.Marshal(req)

	r := httptest.NewRequest("POST", "/search", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleSearch(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "fts5_unavailable", resp.Error)
}

func TestHandler_HandleSearch_MissingQuery(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	searchSvc, err := search.NewService(db, search.DefaultConfig())
	require.NoError(t, err)
	defer searchSvc.Close()

	handler := NewHandler(HandlerDependencies{
		SearchSvc: searchSvc,
	})

	req := SearchRequest{}
	body, _ := json.Marshal(req)

	r := httptest.NewRequest("POST", "/search", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleSearch(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "missing_query", resp.Error)
}

func TestHandler_HandleSearch_Success(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	searchSvc, err := search.NewService(db, search.DefaultConfig())
	require.NoError(t, err)
	defer searchSvc.Close()

	// Insert test data
	_, err = db.Exec(`
		INSERT INTO command_event (session_id, ts, cmd_raw, cmd_norm, cwd, repo_key, ephemeral)
		VALUES ('session1', 1000000, 'docker run nginx', 'docker run nginx', '/home/user', '/repo', 0)
	`)
	require.NoError(t, err)

	// Index the event
	ctx := context.Background()
	require.NoError(t, searchSvc.IndexEvent(ctx, 1))

	handler := NewHandler(HandlerDependencies{
		SearchSvc: searchSvc,
	})

	req := SearchRequest{
		Query: "docker",
	}
	body, _ := json.Marshal(req)

	r := httptest.NewRequest("POST", "/search", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.HandleSearch(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SearchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Results, 1)
	assert.Equal(t, "docker run nginx", resp.Results[0].CmdRaw)
}

func TestHandler_HandleDebugScores(t *testing.T) {
	t.Parallel()

	handler := NewHandler(HandlerDependencies{})

	r := httptest.NewRequest("GET", "/debug/scores", nil)
	w := httptest.NewRecorder()

	handler.HandleDebugScores(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DebugScoresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp.Scores)
}

func TestHandler_HandleDebugCache(t *testing.T) {
	t.Parallel()

	cache := suggest.NewCache(suggest.DefaultCacheConfig())
	cache.Set("session1", 1, []suggest.Suggestion{})
	cache.Set("session2", 2, []suggest.Suggestion{})

	handler := NewHandler(HandlerDependencies{
		Cache: cache,
	})

	r := httptest.NewRequest("GET", "/debug/cache", nil)
	w := httptest.NewRecorder()

	handler.HandleDebugCache(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DebugCacheResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Size)
}

func TestHandler_HandleDebugDiscoveryErrors(t *testing.T) {
	t.Parallel()

	tracker := discovery.NewDiscoveryErrorTracker(100)
	tracker.Record("makefile", "make -qp", "parse error", "/repo1")
	tracker.Record("package.json", "cat package.json", "not found", "/repo2")

	handler := NewHandler(HandlerDependencies{
		ErrorTracker: tracker,
	})

	r := httptest.NewRequest("GET", "/debug/discovery-errors", nil)
	w := httptest.NewRecorder()

	handler.HandleDebugDiscoveryErrors(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp DebugDiscoveryErrorsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Count)
	assert.Len(t, resp.Errors, 2)
}

func TestHandler_RegisterRoutes(t *testing.T) {
	t.Parallel()

	handler := NewHandler(HandlerDependencies{})

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Routes should be registered
	// We can't directly test the routes, but we can verify no panic
}

func TestHandler_UpdateCache(t *testing.T) {
	t.Parallel()

	cache := suggest.NewCache(suggest.DefaultCacheConfig())
	handler := NewHandler(HandlerDependencies{
		Cache: cache,
	})

	suggestions := []suggest.Suggestion{
		{Command: "git status", Score: 100},
	}

	handler.UpdateCache("session123", 42, suggestions)

	// Verify cache was updated
	result, ok := cache.Get("session123", 42)
	assert.True(t, ok)
	assert.Equal(t, suggestions, result)
}

func TestHandler_InvalidateCache(t *testing.T) {
	t.Parallel()

	cache := suggest.NewCache(suggest.DefaultCacheConfig())
	cache.Set("session123", 1, []suggest.Suggestion{})

	handler := NewHandler(HandlerDependencies{
		Cache: cache,
	})

	handler.InvalidateCache("session123")

	// Verify cache was invalidated
	_, ok := cache.Get("session123", 1)
	assert.False(t, ok)
}

func TestHandler_SuggestFromContext(t *testing.T) {
	t.Parallel()

	db := createTestDB(t)
	scorer, err := suggest.NewScorer(suggest.ScorerDependencies{DB: db}, suggest.DefaultScorerConfig())
	require.NoError(t, err)

	handler := NewHandler(HandlerDependencies{
		Scorer: scorer,
	})

	ctx := context.Background()
	suggestions, err := handler.SuggestFromContext(ctx, suggest.SuggestContext{
		SessionID: "session123",
	})
	require.NoError(t, err)
	// May be empty, but shouldn't error
	assert.NotNil(t, suggestions)
}
