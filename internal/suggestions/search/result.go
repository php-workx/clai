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
	CmdRaw      string
	CmdNorm     string
	RepoKey     string
	Cwd         string
	Backend     Backend
	TemplateID  string
	Tags        []string
	MatchedTags []string
	ID          int64
	Timestamp   int64
	Score       float64
}
