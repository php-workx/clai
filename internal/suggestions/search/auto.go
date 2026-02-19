package search

import (
	"context"
	"log/slog"
	"sort"
)

// AutoConfig configures the auto search mode.
type AutoConfig struct {
	// Logger for auto search operations.
	Logger *slog.Logger

	// FTSWeight is the weight for FTS scores in the merged result.
	// Must be between 0.0 and 1.0. Default: 0.6
	FTSWeight float64

	// DescribeWeight is the weight for describe scores in the merged result.
	// Must be between 0.0 and 1.0. Default: 0.4
	DescribeWeight float64
}

// DefaultAutoConfig returns the default auto search configuration.
func DefaultAutoConfig() AutoConfig {
	return AutoConfig{
		FTSWeight:      0.6,
		DescribeWeight: 0.4,
	}
}

// AutoService merges FTS and describe search results.
type AutoService struct {
	fts      *Service
	describe *DescribeService
	logger   *slog.Logger

	ftsWeight      float64
	describeWeight float64
}

// NewAutoService creates a new auto search service.
func NewAutoService(fts *Service, describe *DescribeService, cfg AutoConfig) *AutoService {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	ftsW := cfg.FTSWeight
	descW := cfg.DescribeWeight

	if ftsW <= 0 && descW <= 0 {
		ftsW = 0.6
		descW = 0.4
	}

	total := ftsW + descW
	if total > 0 {
		ftsW /= total
		descW /= total
	}

	return &AutoService{
		fts:            fts,
		describe:       describe,
		logger:         logger,
		ftsWeight:      ftsW,
		describeWeight: descW,
	}
}

// mergedResult holds intermediate data for merging search results.
type mergedResult struct {
	result      SearchResult
	ftsScore    float64
	descScore   float64
	hasFTS      bool
	hasDescribe bool
}

// Search performs an auto search that merges FTS and describe results.
func (s *AutoService) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	offset := normalizeOffset(opts.Offset)

	internalOpts := opts
	internalOpts.Offset = 0
	internalOpts.Limit = (limit + offset) * 3
	if internalOpts.Limit > MaxLimit {
		internalOpts.Limit = MaxLimit
	}

	var ftsResults []SearchResult
	if s.fts != nil && (s.fts.FTS5Available() || s.fts.FallbackEnabled()) {
		var err error
		ftsResults, err = s.fts.Search(ctx, query, internalOpts)
		if err != nil {
			s.logger.Warn("auto search: FTS search failed, continuing with describe only",
				"error", err)
		}
	}

	var descResults []SearchResult
	if s.describe != nil {
		var err error
		descResults, err = s.describe.Search(ctx, query, internalOpts)
		if err != nil {
			s.logger.Warn("auto search: describe search failed, continuing with FTS only",
				"error", err)
		}
	}

	if len(ftsResults) == 0 && len(descResults) == 0 {
		return nil, nil
	}

	merged := s.mergeResults(ftsResults, descResults)

	if offset >= len(merged) {
		return nil, nil
	}
	if offset > 0 {
		merged = merged[offset:]
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}

	return merged, nil
}

// mergeResults merges FTS and describe results, deduplicating by template.
func (s *AutoService) mergeResults(ftsResults, descResults []SearchResult) []SearchResult {
	mergeMap := make(map[string]*mergedResult)
	s.mergeFTSResults(mergeMap, ftsResults)
	s.mergeDescribeResults(mergeMap, descResults)
	results := s.buildMergedResults(mergeMap)
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Timestamp > results[j].Timestamp
	})
	return results
}

func (s *AutoService) mergeFTSResults(mergeMap map[string]*mergedResult, ftsResults []SearchResult) {
	ftsMax := maxScore(ftsResults)
	for i := range ftsResults {
		r := ftsResults[i]
		key := dedupKey(r)
		normalizedScore := normalizeFTSScore(r.Score, ftsMax)
		if existing, ok := mergeMap[key]; ok {
			existing.ftsScore = normalizedScore
			existing.hasFTS = true
			continue
		}
		mergeMap[key] = &mergedResult{
			result:   r,
			ftsScore: normalizedScore,
			hasFTS:   true,
		}
	}
}

func normalizeFTSScore(score, maxScore float64) float64 {
	if maxScore == 0 {
		return 0
	}
	return score / maxScore
}

func (s *AutoService) mergeDescribeResults(mergeMap map[string]*mergedResult, descResults []SearchResult) {
	for i := range descResults {
		r := descResults[i]
		key := dedupKey(r)
		if existing, ok := mergeMap[key]; ok {
			mergeDescribeIntoExisting(existing, r)
			continue
		}
		mergeMap[key] = &mergedResult{
			result:      r,
			descScore:   r.Score,
			hasDescribe: true,
		}
	}
}

func mergeDescribeIntoExisting(existing *mergedResult, r SearchResult) {
	existing.descScore = r.Score
	existing.hasDescribe = true
	if len(existing.result.Tags) == 0 {
		existing.result.Tags = r.Tags
	}
	if len(existing.result.MatchedTags) == 0 {
		existing.result.MatchedTags = r.MatchedTags
	}
	if existing.result.TemplateID == "" {
		existing.result.TemplateID = r.TemplateID
	}
	if existing.result.CmdNorm == "" {
		existing.result.CmdNorm = r.CmdNorm
	}
}

func (s *AutoService) buildMergedResults(mergeMap map[string]*mergedResult) []SearchResult {
	results := make([]SearchResult, 0, len(mergeMap))
	for _, m := range mergeMap {
		m.result.Score = s.ftsWeight*m.ftsScore + s.describeWeight*m.descScore
		m.result.Backend = BackendAuto
		results = append(results, m.result)
	}
	return results
}

// dedupKey returns the key used to deduplicate results.
func dedupKey(r SearchResult) string {
	if r.TemplateID != "" {
		return "t:" + r.TemplateID
	}
	return "r:" + r.CmdRaw
}

// maxScore returns the maximum absolute score from a result set.
func maxScore(results []SearchResult) float64 {
	if len(results) == 0 {
		return 0
	}

	minScore := results[0].Score
	for i := 1; i < len(results); i++ {
		if results[i].Score < minScore {
			minScore = results[i].Score
		}
	}
	return minScore
}
