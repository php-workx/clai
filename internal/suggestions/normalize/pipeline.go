package normalize

import "strings"

// Operator represents a shell pipeline/compound operator.
type Operator string

const (
	OpPipe      Operator = "|"
	OpAnd       Operator = "&&"
	OpOr        Operator = "||"
	OpSemicolon Operator = ";"
)

// Segment represents one command within a pipeline or compound command.
type Segment struct {
	Raw      string   // The raw command text for this segment
	Operator Operator // The operator that follows this segment ("" for the last)
}

// SplitPipeline splits a compound/pipeline command into segments.
// It correctly handles quoted strings and escape sequences.
func SplitPipeline(cmd string) []Segment {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}

	var segments []Segment
	var current strings.Builder
	runes := []rune(cmd)
	n := len(runes)
	i := 0

	for i < n {
		ch := runes[i]

		// Handle escape sequences
		if ch == '\\' && i+1 < n {
			current.WriteRune(ch)
			current.WriteRune(runes[i+1])
			i += 2
			continue
		}

		// Handle single-quoted strings
		if ch == '\'' {
			current.WriteRune(ch)
			i++
			for i < n && runes[i] != '\'' {
				current.WriteRune(runes[i])
				i++
			}
			if i < n {
				current.WriteRune(runes[i]) // closing quote
				i++
			}
			continue
		}

		// Handle double-quoted strings
		if ch == '"' {
			current.WriteRune(ch)
			i++
			for i < n && runes[i] != '"' {
				if runes[i] == '\\' && i+1 < n {
					current.WriteRune(runes[i])
					current.WriteRune(runes[i+1])
					i += 2
					continue
				}
				current.WriteRune(runes[i])
				i++
			}
			if i < n {
				current.WriteRune(runes[i]) // closing quote
				i++
			}
			continue
		}

		// Check for operators (order matters: && before &, || before |)
		if ch == '&' && i+1 < n && runes[i+1] == '&' {
			seg := strings.TrimSpace(current.String())
			if seg != "" {
				segments = append(segments, Segment{Raw: seg, Operator: OpAnd})
			}
			current.Reset()
			i += 2
			continue
		}

		if ch == '|' && i+1 < n && runes[i+1] == '|' {
			seg := strings.TrimSpace(current.String())
			if seg != "" {
				segments = append(segments, Segment{Raw: seg, Operator: OpOr})
			}
			current.Reset()
			i += 2
			continue
		}

		if ch == '|' {
			seg := strings.TrimSpace(current.String())
			if seg != "" {
				segments = append(segments, Segment{Raw: seg, Operator: OpPipe})
			}
			current.Reset()
			i++
			continue
		}

		if ch == ';' {
			seg := strings.TrimSpace(current.String())
			if seg != "" {
				segments = append(segments, Segment{Raw: seg, Operator: OpSemicolon})
			}
			current.Reset()
			i++
			continue
		}

		current.WriteRune(ch)
		i++
	}

	// Add the last segment (no trailing operator)
	seg := strings.TrimSpace(current.String())
	if seg != "" {
		segments = append(segments, Segment{Raw: seg, Operator: ""})
	}

	return segments
}

// ReassemblePipeline reconstructs a command string from segments.
func ReassemblePipeline(segments []Segment) string {
	if len(segments) == 0 {
		return ""
	}

	var b strings.Builder
	for i, seg := range segments {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(seg.Raw)
		if seg.Operator != "" {
			b.WriteString(" ")
			b.WriteString(string(seg.Operator))
		}
	}
	return b.String()
}
