package normalize

import (
	"regexp"
	"strings"
)

// Pre-normalization placeholder constants.
const (
	PlaceholderPath = "<PATH>"
	PlaceholderUUID = "<UUID>"
	PlaceholderURL  = "<URL>"
	PlaceholderNum  = "<NUM>"
)

// Pre-compiled patterns for pre-normalization.
var (
	uuidPattern       = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	preNormURLPattern = regexp.MustCompile(`https?://\S+`)
	preNormNumPattern = regexp.MustCompile(`^\d+$`)
	multiSpacePattern = regexp.MustCompile(`\s+`)
)

// PreNormConfig holds configuration for the pre-normalization pipeline.
type PreNormConfig struct {
	// MaxEventBytes is the maximum allowed event size in bytes.
	// Events exceeding this are truncated. Default: DefaultMaxEventSize.
	MaxEventBytes int

	// Aliases maps alias names to expansions for alias resolution.
	Aliases map[string]string

	// AliasMaxDepth is the max alias expansion depth. Default: DefaultMaxAliasDepth.
	AliasMaxDepth int
}

// PreNormResult holds the output of the pre-normalization pipeline.
type PreNormResult struct {
	// CmdNorm is the fully pre-normalized command string.
	CmdNorm string

	// TemplateID is the sha256 hex of CmdNorm.
	TemplateID string

	// Tags are the semantic tags extracted from the command.
	Tags []string

	// Segments are the pipeline segments after splitting.
	Segments []Segment

	// Truncated indicates that the original command was truncated.
	Truncated bool

	// AliasExpanded indicates that alias expansion occurred.
	AliasExpanded bool

	// SlotCount is the number of placeholder slots in CmdNorm.
	SlotCount int
}

// PreNormalize runs the full pre-normalization pipeline on a raw command string.
//
// Steps:
//  1. Enforce event size limit
//  2. Expand aliases (bounded, cycle-safe)
//  3. Split into pipeline/compound segments
//  4. Normalize each segment (whitespace, lowercase cmd, placeholders)
//  5. Reassemble pipeline
//  6. Compute template_id (sha256)
//  7. Extract semantic tags
func PreNormalize(cmdRaw string, cfg PreNormConfig) PreNormResult {
	var result PreNormResult

	// Step 1: Enforce size limit
	cmd, truncated := EnforceEventSize(cmdRaw, cfg.MaxEventBytes)
	result.Truncated = truncated

	// Step 2: Alias expansion
	expander := &AliasExpander{
		Aliases:  cfg.Aliases,
		MaxDepth: cfg.AliasMaxDepth,
	}
	cmd, aliasExpanded := expander.Expand(cmd)
	result.AliasExpanded = aliasExpanded

	// Step 3: Split pipeline
	segments := SplitPipeline(cmd)
	if len(segments) == 0 {
		result.CmdNorm = ""
		result.TemplateID = ComputeTemplateID("")
		return result
	}

	// Step 4: Normalize each segment
	for i := range segments {
		segments[i].Raw = normalizeSegment(segments[i].Raw)
	}

	result.Segments = segments

	// Step 5: Reassemble
	result.CmdNorm = ReassemblePipeline(segments)

	// Step 6: Template ID
	result.TemplateID = ComputeTemplateID(result.CmdNorm)

	// Step 7: Tags
	result.Tags = ExtractTags(segments)

	// Count slots
	result.SlotCount = CountSlots(result.CmdNorm)

	return result
}

// normalizeSegment normalizes a single command segment:
//   - Collapse whitespace
//   - Lowercase the command name (first token)
//   - Replace UUIDs, URLs, paths, and bare numbers with placeholders
func normalizeSegment(raw string) string {
	// Collapse whitespace
	s := multiSpacePattern.ReplaceAllString(strings.TrimSpace(raw), " ")
	if s == "" {
		return s
	}

	// Replace UUIDs first (before other patterns)
	s = uuidPattern.ReplaceAllString(s, PlaceholderUUID)

	// Replace URLs
	s = preNormURLPattern.ReplaceAllString(s, PlaceholderURL)

	// Split into tokens for per-token processing
	tokens := strings.Fields(s)
	if len(tokens) == 0 {
		return s
	}

	// Lowercase the command name (first token), unless it's already a placeholder
	if !isPlaceholder(tokens[0]) {
		tokens[0] = strings.ToLower(tokens[0])
	}

	// Process remaining tokens
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]

		// Skip flags
		if strings.HasPrefix(tok, "-") {
			continue
		}

		// Skip already-replaced placeholders
		if isPlaceholder(tok) {
			continue
		}

		// Replace paths
		if isPathLike(tok) {
			tokens[i] = PlaceholderPath
			continue
		}

		// Replace bare numbers
		if preNormNumPattern.MatchString(tok) {
			tokens[i] = PlaceholderNum
			continue
		}
	}

	return strings.Join(tokens, " ")
}

// isPlaceholder returns true if the token is a known placeholder.
func isPlaceholder(tok string) bool {
	switch tok {
	case PlaceholderPath, PlaceholderUUID, PlaceholderURL, PlaceholderNum:
		return true
	}
	return false
}

// isPathLike returns true if the token looks like a filesystem path.
// Conservative: requires /, ~/, ./, ../, or a Windows drive letter.
func isPathLike(tok string) bool {
	if strings.HasPrefix(tok, "/") ||
		strings.HasPrefix(tok, "~/") ||
		strings.HasPrefix(tok, "./") ||
		strings.HasPrefix(tok, "../") ||
		(strings.HasPrefix(tok, "~") && len(tok) > 1) {
		return true
	}

	// Windows drive letter: C:\ or C:/
	if len(tok) >= 3 && tok[1] == ':' &&
		((tok[0] >= 'a' && tok[0] <= 'z') || (tok[0] >= 'A' && tok[0] <= 'Z')) &&
		(tok[2] == '/' || tok[2] == '\\') {
		return true
	}

	// Contains directory separator (but not just a bare filename)
	if strings.Contains(tok, "/") && !strings.HasPrefix(tok, "-") {
		return true
	}

	return false
}
