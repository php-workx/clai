package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
)

// DescribeConfig configures the describe search mode.
type DescribeConfig struct {
	// Logger for describe search operations.
	Logger *slog.Logger

	// TagMapper converts natural language descriptions to tags.
	// If nil, uses the built-in defaultDescriptionToTags.
	TagMapper func(description string) []string
}

// DescribeService provides tag-based descriptive search over command templates.
type DescribeService struct {
	db        *sql.DB
	logger    *slog.Logger
	tagMapper func(description string) []string
}

// NewDescribeService creates a new describe search service.
func NewDescribeService(db *sql.DB, cfg DescribeConfig) *DescribeService {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &DescribeService{
		db:        db,
		logger:    logger,
		tagMapper: cfg.TagMapper,
	}
}

// templateRow holds data scanned from a command_template row joined with command_event.
type templateRow struct {
	templateID string
	cmdNorm    string
	cmdRaw     string
	repoKey    string
	cwd        string
	tagsJSON   sql.NullString
	timestamp  int64
	eventID    int64
}

// Search performs a tag-based descriptive search.
// The query is parsed into tags, then templates with overlapping tags are scored
// by the number of matching tags divided by the total query tags (Jaccard-like).
func (s *DescribeService) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	queryTags := s.mapToTags(query)
	if len(queryTags) == 0 {
		s.logger.Debug("describe search: no tags extracted from query", "query", query)
		return nil, nil
	}

	s.logger.Debug("describe search: extracted tags", "query", query, "tags", queryTags)
	limit := normalizeLimit(opts.Limit)

	rows, err := s.queryTemplatesWithTags(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	queryTagSet := makeTagSet(queryTags)
	results, err := s.scanDescribeResults(rows, queryTags, queryTagSet)
	if err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending, then by timestamp descending for ties
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Timestamp > results[j].Timestamp
	})

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

func makeTagSet(tags []string) map[string]bool {
	out := make(map[string]bool, len(tags))
	for _, t := range tags {
		out[t] = true
	}
	return out
}

func (s *DescribeService) scanDescribeResults(
	rows *sql.Rows,
	queryTags []string,
	queryTagSet map[string]bool,
) ([]SearchResult, error) {
	var results []SearchResult
	for rows.Next() {
		row, err := scanTemplateRow(rows)
		if err != nil {
			return nil, err
		}
		result, ok := s.describeResultFromRow(&row, queryTags, queryTagSet)
		if ok {
			results = append(results, result)
		}
	}
	return results, nil
}

func scanTemplateRow(rows *sql.Rows) (templateRow, error) {
	var row templateRow
	err := rows.Scan(&row.eventID, &row.cmdRaw, &row.repoKey, &row.cwd,
		&row.timestamp, &row.templateID, &row.cmdNorm, &row.tagsJSON)
	return row, err
}

func (s *DescribeService) describeResultFromRow(
	row *templateRow,
	queryTags []string,
	queryTagSet map[string]bool,
) (SearchResult, bool) {
	tags, ok := s.parseTemplateTags(row)
	if !ok {
		return SearchResult{}, false
	}
	matchedTags := computeMatchedTags(tags, queryTagSet)
	if len(matchedTags) == 0 {
		return SearchResult{}, false
	}
	score := float64(len(matchedTags)) / float64(len(queryTags))
	return SearchResult{
		ID:          row.eventID,
		CmdRaw:      row.cmdRaw,
		CmdNorm:     row.cmdNorm,
		RepoKey:     row.repoKey,
		Cwd:         row.cwd,
		Timestamp:   row.timestamp,
		Score:       score,
		Tags:        tags,
		MatchedTags: matchedTags,
		Backend:     BackendDescribe,
		TemplateID:  row.templateID,
	}, true
}

func (s *DescribeService) parseTemplateTags(row *templateRow) ([]string, bool) {
	if !row.tagsJSON.Valid || row.tagsJSON.String == "" || row.tagsJSON.String == "null" {
		return nil, false
	}
	var tags []string
	if err := json.Unmarshal([]byte(row.tagsJSON.String), &tags); err != nil {
		s.logger.Warn("describe search: failed to unmarshal tags",
			"template_id", row.templateID, "error", err)
		return nil, false
	}
	if len(tags) == 0 {
		return nil, false
	}
	return tags, true
}

// queryTemplatesWithTags retrieves templates with tags joined to their most recent event.
func (s *DescribeService) queryTemplatesWithTags(ctx context.Context, opts SearchOptions) (*sql.Rows, error) {
	query := `
		SELECT ce.id, ce.cmd_raw, COALESCE(ce.repo_key, ''), ce.cwd, ce.ts_ms,
		       ct.template_id, ct.cmd_norm, ct.tags
		FROM command_template ct
		JOIN command_event ce ON ce.template_id = ct.template_id
		WHERE ct.tags IS NOT NULL AND ct.tags != 'null' AND ct.tags != ''
		  AND ce.ephemeral = 0
		  AND (? = '' OR ce.repo_key = ?)
		  AND (? = '' OR ce.cwd = ?)
		ORDER BY ce.ts_ms DESC
	`

	return s.db.QueryContext(ctx, query,
		opts.RepoKey, opts.RepoKey,
		opts.Cwd, opts.Cwd,
	)
}

// mapToTags maps a description string to tags using the configured mapper.
func (s *DescribeService) mapToTags(description string) []string {
	if s.tagMapper != nil {
		return s.tagMapper(description)
	}
	return defaultDescriptionToTags(description)
}

// defaultDescriptionToTags tokenizes the description and maps known words to tags.
func defaultDescriptionToTags(description string) []string {
	if description == "" {
		return nil
	}

	tagSet := make(map[string]bool)
	words := strings.Fields(strings.ToLower(description))

	for _, word := range words {
		word = strings.Trim(word, ".,;:!?\"'()[]{}")
		if word == "" {
			continue
		}

		if isKnownTag(word) {
			tagSet[word] = true
		}

		if mapped, ok := descriptionSynonymMap[word]; ok {
			for _, tag := range mapped {
				tagSet[tag] = true
			}
		}
	}

	if len(tagSet) == 0 {
		return nil
	}

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// isKnownTag checks if a word is a known tag in the vocabulary.
var knownTags = map[string]bool{
	"vcs": true, "git": true, "github": true, "gitlab": true,
	"container": true, "docker": true, "k8s": true,
	"go": true, "format": true, "lint": true,
	"js": true, "package": true, "typescript": true,
	"python": true, "test": true,
	"rust": true,
	"java": true,
	"ruby": true, "build": true,
	"system": true, "process": true, "service": true, "log": true,
	"disk": true, "memory": true, "env": true,
	"network": true, "http": true, "remote": true, "dns": true,
	"cloud": true, "aws": true, "gcp": true, "azure": true, "iac": true,
	"editor": true, "vscode": true,
	"shell": true, "nav": true, "file": true, "search": true,
	"text": true, "archive": true,
	"database": true, "postgres": true, "mysql": true, "sqlite": true,
	"redis": true, "mongo": true,
	"codegen": true,
}

func isKnownTag(word string) bool {
	return knownTags[word]
}

// descriptionSynonymMap maps natural language keywords to tag vocabulary terms.
var descriptionSynonymMap = map[string][]string{
	"version": {"vcs"}, "commit": {"vcs", "git"}, "branch": {"vcs", "git"},
	"merge": {"vcs", "git"}, "rebase": {"vcs", "git"}, "checkout": {"vcs", "git"},
	"repository": {"vcs", "git"}, "repo": {"vcs", "git"}, "clone": {"vcs", "git"},
	"pull": {"vcs", "git"}, "push": {"vcs", "git"}, "stash": {"vcs", "git"},
	"diff": {"vcs", "git"},

	"containers": {"container"}, "image": {"container", "docker"},
	"images": {"container", "docker"}, "pod": {"k8s", "container"},
	"pods": {"k8s", "container"}, "kubernetes": {"k8s", "container"},
	"deploy": {"k8s", "container"}, "deployment": {"k8s", "container"},

	"files": {"file", "shell"}, "directory": {"file", "shell", "nav"},
	"folder": {"file", "shell", "nav"}, "copy": {"file", "shell"},
	"move": {"file", "shell"}, "delete": {"file", "shell"},
	"remove": {"file", "shell"}, "rename": {"file", "shell"},
	"large": {"file", "disk", "system"}, "size": {"file", "disk", "system"},
	"space": {"disk", "system"}, "permission": {"file", "shell"},
	"permissions": {"file", "shell"},

	"look": {"search", "shell"}, "locate": {"search", "shell"},

	"compile": {"build"}, "make": {"build"},

	"tests": {"test"}, "testing": {"test"}, "spec": {"test"},

	"install": {"package"}, "uninstall": {"package"},
	"packages": {"package"}, "dependency": {"package"},
	"dependencies": {"package"}, "module": {"package"}, "modules": {"package"},

	"api": {"network", "http"}, "request": {"network", "http"},
	"download": {"network", "http"}, "upload": {"network"},
	"ssh": {"network", "remote"},

	"terraform": {"iac", "cloud"}, "infra": {"iac", "cloud"},
	"infrastructure": {"iac", "cloud"},

	"processes": {"system", "process"}, "services": {"system", "service"},
	"logs": {"system", "log"}, "environment": {"system", "env"},

	"db": {"database"}, "sql": {"database"},

	"edit": {"editor"},

	"javascript": {"js"}, "node": {"js"},
	"golang": {"go"},

	"fmt": {"format"}, "check": {"lint"},

	"compress": {"archive", "shell"}, "extract": {"archive", "shell"},
	"zip": {"archive", "shell"}, "tar": {"archive", "shell"},
	"unzip": {"archive", "shell"},

	"sort": {"text", "shell"}, "filter": {"text", "shell"},
	"transform": {"text", "shell"},

	"navigate": {"nav", "shell"}, "navigation": {"nav", "shell"},
	"cd": {"nav", "shell"},

	"find": {"file", "search", "shell"},
	"grep": {"search", "shell"},
}

// computeMatchedTags returns tags from the template that match the query tag set.
func computeMatchedTags(templateTags []string, queryTagSet map[string]bool) []string {
	var matched []string
	for _, t := range templateTags {
		if queryTagSet[t] {
			matched = append(matched, t)
		}
	}
	sort.Strings(matched)
	return matched
}
