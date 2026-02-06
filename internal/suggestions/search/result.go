package search

// Backend identifies which search backend produced a result.
type Backend string

const (
	// BackendFTS indicates results from FTS5 full-text search.
	BackendFTS Backend = "fts"
	// BackendDescribe indicates results from tag-based describe search.
	BackendDescribe Backend = "describe"
	// BackendAuto indicates results from merged FTS + describe search.
	BackendAuto Backend = "auto"
	// BackendFallback indicates results from LIKE-based fallback search.
	BackendFallback Backend = "fallback"
)

// SearchResult represents a single search result from any search backend.
type SearchResult struct {
	ID          int64    // Command event ID
	CmdRaw      string   // Raw command
	CmdNorm     string   // Normalized command (may be empty for event-based results)
	RepoKey     string   // Repository key (may be empty)
	Cwd         string   // Working directory
	Timestamp   int64    // Event timestamp (ms)
	Score       float64  // BM25 or relevance score
	Tags        []string // All tags for this command's template
	MatchedTags []string // Tags that matched the query
	Backend     Backend  // Which search backend produced this result
	TemplateID  string   // Template ID (for deduplication in auto mode)
}
