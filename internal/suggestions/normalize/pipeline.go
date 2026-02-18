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

	parser := newPipelineParser(cmd)
	return parser.parse()
}

type pipelineParser struct {
	runes    []rune
	pos      int
	current  strings.Builder
	segments []Segment
}

func newPipelineParser(cmd string) *pipelineParser {
	return &pipelineParser{runes: []rune(cmd)}
}

func (p *pipelineParser) parse() []Segment {
	for p.pos < len(p.runes) {
		if p.consumeEscaped() || p.consumeSingleQuoted() || p.consumeDoubleQuoted() || p.consumeOperator() {
			continue
		}
		p.current.WriteRune(p.runes[p.pos])
		p.pos++
	}
	p.flush("")
	return p.segments
}

func (p *pipelineParser) consumeEscaped() bool {
	if p.runes[p.pos] != '\\' || p.pos+1 >= len(p.runes) {
		return false
	}
	p.current.WriteRune(p.runes[p.pos])
	p.current.WriteRune(p.runes[p.pos+1])
	p.pos += 2
	return true
}

func (p *pipelineParser) consumeSingleQuoted() bool {
	if p.runes[p.pos] != '\'' {
		return false
	}
	p.consumeQuoted('\'', false)
	return true
}

func (p *pipelineParser) consumeDoubleQuoted() bool {
	if p.runes[p.pos] != '"' {
		return false
	}
	p.consumeQuoted('"', true)
	return true
}

func (p *pipelineParser) consumeQuoted(quote rune, allowEscapes bool) {
	p.current.WriteRune(p.runes[p.pos])
	p.pos++
	for p.pos < len(p.runes) && p.runes[p.pos] != quote {
		if allowEscapes && p.runes[p.pos] == '\\' && p.pos+1 < len(p.runes) {
			p.current.WriteRune(p.runes[p.pos])
			p.current.WriteRune(p.runes[p.pos+1])
			p.pos += 2
			continue
		}
		p.current.WriteRune(p.runes[p.pos])
		p.pos++
	}
	if p.pos < len(p.runes) {
		p.current.WriteRune(p.runes[p.pos])
		p.pos++
	}
}

func (p *pipelineParser) consumeOperator() bool {
	if p.consumeDoubleOperator('&', OpAnd) || p.consumeDoubleOperator('|', OpOr) {
		return true
	}
	switch p.runes[p.pos] {
	case '|':
		p.flush(OpPipe)
		p.pos++
		return true
	case ';':
		p.flush(OpSemicolon)
		p.pos++
		return true
	default:
		return false
	}
}

func (p *pipelineParser) consumeDoubleOperator(ch rune, op Operator) bool {
	if p.runes[p.pos] != ch || p.pos+1 >= len(p.runes) || p.runes[p.pos+1] != ch {
		return false
	}
	p.flush(op)
	p.pos += 2
	return true
}

func (p *pipelineParser) flush(op Operator) {
	seg := strings.TrimSpace(p.current.String())
	if seg != "" {
		p.segments = append(p.segments, Segment{Raw: seg, Operator: op})
	}
	p.current.Reset()
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
