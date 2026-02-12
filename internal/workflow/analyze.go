package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// DefaultMaxBytes is the default maximum size for LLM analysis context.
const DefaultMaxBytes = 100 * 1024

// AnalysisResult holds the parsed LLM analysis.
type AnalysisResult struct {
	Decision  string            `json:"decision"` // "proceed", "halt", "needs_human", "error"
	Reasoning string            `json:"reasoning"`
	Flags     map[string]string `json:"flags,omitempty"`
}

// Analyzer builds prompts, queries LLM, and parses responses.
type Analyzer struct {
	masker *SecretMasker
}

// NewAnalyzer creates a new analyzer with optional secret masking.
func NewAnalyzer(masker *SecretMasker) *Analyzer {
	return &Analyzer{masker: masker}
}

// BuildAnalysisContext prepares scrubbed output for LLM analysis.
// Caps at maxBytes (DefaultMaxBytes if <= 0). Uses head+tail preservation
// with rune-aware truncation (D20 -- no broken UTF-8).
func (a *Analyzer) BuildAnalysisContext(stdout, stderr string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	var combined string
	if stderr == "" {
		combined = stdout
	} else if stdout == "" {
		combined = stderr
	} else {
		combined = stdout + "\n" + stderr
	}

	// Apply secret masking if available.
	if a.masker != nil {
		combined = a.masker.Mask(combined)
	}

	if len(combined) <= maxBytes {
		return combined
	}

	return truncateRuneAware(combined, maxBytes)
}

// truncateRuneAware keeps head (70%) and tail (30%) of a string that
// exceeds maxBytes, inserting a separator in between. It never breaks
// multi-byte UTF-8 characters (D20).
func truncateRuneAware(s string, maxBytes int) string {
	separator := "\n...[truncated]...\n"
	sepLen := len(separator)

	budget := maxBytes - sepLen
	if budget <= 0 {
		// maxBytes too small for even the separator; return what we can.
		return runeSliceBytes(s, maxBytes)
	}

	headBudget := budget * 7 / 10
	tailBudget := budget - headBudget

	head := runeSliceBytes(s, headBudget)
	tail := runeSliceBytesFromEnd(s, tailBudget)

	return head + separator + tail
}

// runeSliceBytes returns the longest prefix of s that is at most maxBytes,
// without breaking a multi-byte rune.
func runeSliceBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk backward from maxBytes to find a valid rune boundary.
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

// runeSliceBytesFromEnd returns the longest suffix of s that is at most
// maxBytes, without breaking a multi-byte rune.
func runeSliceBytesFromEnd(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	// Walk forward from start to find a valid rune boundary.
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// BuildPrompt constructs the LLM prompt for step analysis.
func (a *Analyzer) BuildPrompt(stepName, riskLevel, context, customPrompt string) string {
	if riskLevel == "" {
		riskLevel = "medium"
	}

	var b strings.Builder
	b.WriteString("You are analyzing the output of a workflow step.\n\n")
	fmt.Fprintf(&b, "Step: %s\n", stepName)
	fmt.Fprintf(&b, "Risk level: %s\n\n", riskLevel)

	if customPrompt != "" {
		fmt.Fprintf(&b, "Analysis instructions: %s\n\n", customPrompt)
	}

	b.WriteString("Output:\n```\n")
	b.WriteString(context)
	b.WriteString("\n```\n\n")

	b.WriteString("Respond with a JSON object: {\"decision\": \"proceed|halt|needs_human\", \"reasoning\": \"...\", \"flags\": {}}\n")
	b.WriteString("Valid decisions: proceed, halt, needs_human\n")

	return b.String()
}

// ParseAnalysisResponse extracts decision/reasoning/flags from LLM response.
// Unparseable response returns AnalysisResult{Decision: "needs_human"} (FR-24).
func ParseAnalysisResponse(raw string) *AnalysisResult {
	// Try JSON parse first.
	if result := parseJSON(raw); result != nil {
		return result
	}

	// Try extracting JSON from markdown code block.
	if result := parseJSONFromCodeBlock(raw); result != nil {
		return result
	}

	// Fallback: look for decision: keyword in plain text.
	if result := parsePlainText(raw); result != nil {
		return result
	}

	// FR-24: nothing parseable -> needs_human.
	return &AnalysisResult{
		Decision:  string(DecisionNeedsHuman),
		Reasoning: "could not parse LLM response",
	}
}

// ValidDecisions is the set of recognized decision values.
var ValidDecisions = map[string]bool{
	string(DecisionProceed):    true,
	string(DecisionHalt):       true,
	string(DecisionNeedsHuman): true,
	string(DecisionError):      true,
	// Legacy aliases for backward compatibility with existing LLM responses.
	"approve": true,
	"reject":  true,
}

// normalizeDecision maps legacy decision values to spec-aligned values.
func normalizeDecision(d string) string {
	switch d {
	case "approve":
		return string(DecisionProceed)
	case "reject":
		return string(DecisionHalt)
	default:
		return d
	}
}

func parseJSON(raw string) *AnalysisResult {
	raw = strings.TrimSpace(raw)

	// Find first '{' and last '}' to extract JSON from surrounding text.
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < 0 || end <= start {
		return nil
	}

	candidate := raw[start : end+1]
	var result AnalysisResult
	if err := json.Unmarshal([]byte(candidate), &result); err != nil {
		return nil
	}

	result.Decision = strings.ToLower(strings.TrimSpace(result.Decision))
	if !ValidDecisions[result.Decision] {
		return nil
	}
	result.Decision = normalizeDecision(result.Decision)

	return &result
}

func parseJSONFromCodeBlock(raw string) *AnalysisResult {
	// Look for ```json ... ``` or ``` ... ```
	const fence = "```"
	start := strings.Index(raw, fence)
	if start < 0 {
		return nil
	}
	// Skip the opening fence line.
	contentStart := strings.Index(raw[start:], "\n")
	if contentStart < 0 {
		return nil
	}
	contentStart += start + 1

	end := strings.Index(raw[contentStart:], fence)
	if end < 0 {
		return nil
	}

	return parseJSON(raw[contentStart : contentStart+end])
}

func parsePlainText(raw string) *AnalysisResult {
	lower := strings.ToLower(raw)
	for _, keyword := range []string{"decision:", "decision ="} {
		idx := strings.Index(lower, keyword)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(raw[idx+len(keyword):])
		// Take the first word.
		word := strings.Fields(rest)
		if len(word) == 0 {
			continue
		}
		decision := strings.ToLower(strings.Trim(word[0], `"',.:;`))
		if ValidDecisions[decision] {
			return &AnalysisResult{
				Decision:  normalizeDecision(decision),
				Reasoning: strings.TrimSpace(raw),
			}
		}
	}
	return nil
}

// ShouldPromptHuman applies the risk matrix (spec SS10.7).
// Returns true if human review is needed based on decision + risk level.
func ShouldPromptHuman(decision, riskLevel string) bool {
	if riskLevel == "" {
		riskLevel = string(RiskMedium)
	}

	switch Decision(decision) {
	case DecisionProceed:
		// proceed + high -> true; proceed + low/medium -> false
		return riskLevel == string(RiskHigh)
	case DecisionHalt, DecisionNeedsHuman, DecisionError:
		// always require human review
		return true
	default:
		// unknown decision -> require human review
		return true
	}
}
