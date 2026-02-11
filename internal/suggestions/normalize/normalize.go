// Package normalize provides command normalization for the clai suggestions engine.
// Normalization replaces variable arguments with typed slots to enable pattern matching
// and slot filling for suggestions.
//
// Per spec Section 8:
//   - Preserves command/subcommand + flags
//   - Replaces variable arguments with typed slots (<path>, <num>, <sha>, <url>, <msg>, <arg>)
//   - Deterministic output
package normalize

import (
	"regexp"
	"strings"

	"github.com/google/shlex"
)

// Slot types per spec Section 8.2
const (
	SlotPath = "<path>" // token looks like a path (/, ./, ../, ~, contains /)
	SlotNum  = "<num>"  // digits
	SlotSHA  = "<sha>"  // 7-40 hex chars (git commit SHA)
	SlotURL  = "<url>"  // http(s):// or git@...:
	SlotMsg  = "<msg>"  // commit messages in common patterns
	SlotArg  = "<arg>"  // generic argument placeholder
)

// Pre-compiled regex patterns for slot detection
var (
	// pathPattern matches tokens that look like file paths
	pathPattern = regexp.MustCompile(`^(?:[~/.]|[a-zA-Z]:[/\\]|.*/)`)

	// numPattern matches pure numeric tokens
	numPattern = regexp.MustCompile(`^\d+$`)

	// shaPattern matches git SHA-like hex strings (7-40 chars)
	// Case-insensitive, must be only hex chars
	shaPattern = regexp.MustCompile(`(?i)^[0-9a-f]{7,40}$`)

	// urlPattern matches URLs
	urlPattern = regexp.MustCompile(`^(?:https?://|git@[^:]+:)`)

	// envVarPattern matches environment variable references
	envVarPattern = regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*$`)
)

// Normalizer normalizes commands by replacing variable arguments with typed slots.
type Normalizer struct {
	// CommandRules contains command-specific normalization rules
	CommandRules map[string]CommandRule
}

// CommandRule defines normalization behavior for a specific command.
type CommandRule struct {
	// ArgSlots maps argument position to a specific slot type.
	// Position -1 means "all remaining args".
	ArgSlots map[int]string

	// FlagSlots maps flag names to slot types for their values.
	FlagSlots map[string]string

	// SubcommandRules maps subcommands to their rules.
	SubcommandRules map[string]CommandRule
}

// NewNormalizer creates a Normalizer with default command rules.
func NewNormalizer() *Normalizer {
	return &Normalizer{
		CommandRules: defaultCommandRules(),
	}
}

// Normalize normalizes a command string by replacing variable arguments with slots.
// Returns the normalized command and a list of slot values extracted.
func (n *Normalizer) Normalize(cmdRaw string) (cmdNorm string, slots []SlotValue) {
	tokens := parseCommandTokens(cmdRaw)
	if len(tokens) == 0 {
		return cmdRaw, nil
	}
	state := n.newNormalizeState(tokens)
	state.consumeSubcommand()
	state.normalizeRemaining()
	return strings.Join(state.result, " "), state.slots
}

func parseCommandTokens(cmdRaw string) []string {
	tokens, err := shlex.Split(cmdRaw)
	if err == nil && len(tokens) > 0 {
		return tokens
	}
	return strings.Fields(cmdRaw)
}

type normalizeState struct {
	n        *Normalizer
	tokens   []string
	rule     CommandRule
	hasRule  bool
	i        int
	argIndex int
	result   []string
	slots    []SlotValue
}

func (n *Normalizer) newNormalizeState(tokens []string) *normalizeState {
	cmd := tokens[0]
	rule, hasRule := n.CommandRules[cmd]
	return &normalizeState{
		n:       n,
		tokens:  tokens,
		rule:    rule,
		hasRule: hasRule,
		i:       1,
		result:  []string{cmd},
		slots:   make([]SlotValue, 0),
	}
}

func (s *normalizeState) consumeSubcommand() {
	if !s.hasRule || s.i >= len(s.tokens) || s.rule.SubcommandRules == nil {
		return
	}
	token := s.tokens[s.i]
	if strings.HasPrefix(token, "-") || strings.Contains(token, "/") || strings.HasPrefix(token, ".") {
		return
	}
	s.result = append(s.result, token)
	if subRule, ok := s.rule.SubcommandRules[token]; ok {
		s.rule = subRule
	} else {
		s.rule = CommandRule{}
		s.hasRule = false
	}
	s.i++
}

func (s *normalizeState) normalizeRemaining() {
	for s.i < len(s.tokens) {
		if s.consumeFlagToken() || s.consumePlaceholderToken() {
			continue
		}
		s.consumePositionalToken()
	}
}

func (s *normalizeState) consumeFlagToken() bool {
	token := s.tokens[s.i]
	if !strings.HasPrefix(token, "-") {
		return false
	}
	s.result = append(s.result, token)
	flagName := strings.TrimLeft(token, "-")
	if s.hasRule && s.rule.FlagSlots != nil {
		if slotType, ok := s.rule.FlagSlots[flagName]; ok && s.i+1 < len(s.tokens) {
			s.i++
			s.addSlot(slotType, s.tokens[s.i])
			s.result = append(s.result, slotType)
		}
	}
	s.i++
	return true
}

func (s *normalizeState) consumePlaceholderToken() bool {
	token := s.tokens[s.i]
	if !isSlotPlaceholder(token) {
		return false
	}
	s.result = append(s.result, token)
	s.argIndex++
	s.i++
	return true
}

func (s *normalizeState) consumePositionalToken() {
	token := s.tokens[s.i]
	slotType := s.selectSlotType(token)
	s.addSlot(slotType, token)
	s.result = append(s.result, slotType)
	s.argIndex++
	s.i++
}

func (s *normalizeState) selectSlotType(token string) string {
	slotType := s.n.detectSlotType(token)
	if !s.hasRule || s.rule.ArgSlots == nil {
		return slotType
	}
	if specificSlot, ok := s.rule.ArgSlots[s.argIndex]; ok {
		return specificSlot
	}
	if specificSlot, ok := s.rule.ArgSlots[-1]; ok {
		return specificSlot
	}
	return slotType
}

func (s *normalizeState) addSlot(slotType, value string) {
	s.slots = append(s.slots, SlotValue{
		Index: len(s.slots),
		Type:  slotType,
		Value: value,
	})
}

// SlotValue represents an extracted slot value from a command.
type SlotValue struct {
	Index int    // Position in the list of slots
	Type  string // Slot type (e.g., "<path>", "<sha>")
	Value string // Original value from the command
}

// isSlotPlaceholder returns true if the token is already a normalized slot
// placeholder (e.g. "<path>", "<arg>"). This ensures idempotency: normalizing
// an already-normalized command produces identical output.
func isSlotPlaceholder(token string) bool {
	switch token {
	case SlotPath, SlotNum, SlotSHA, SlotURL, SlotMsg, SlotArg:
		return true
	}
	return false
}

// detectSlotType determines the appropriate slot type for a token.
func (n *Normalizer) detectSlotType(token string) string {
	// Already a slot placeholder — preserve for idempotency
	if isSlotPlaceholder(token) {
		return token
	}

	// Check in order of specificity

	// SHA first (most specific pattern)
	if shaPattern.MatchString(token) {
		return SlotSHA
	}

	// URL
	if urlPattern.MatchString(token) {
		return SlotURL
	}

	// Path (contains /, starts with ~, ., or drive letter)
	if pathPattern.MatchString(token) {
		return SlotPath
	}

	// Pure number
	if numPattern.MatchString(token) {
		return SlotNum
	}

	// Environment variable - keep as-is (not a slot)
	if envVarPattern.MatchString(token) {
		return token // Return the token itself, not a slot
	}

	// Default to generic argument
	return SlotArg
}

// defaultCommandRules returns the starter set of command-specific rules per spec Section 8.3.
func defaultCommandRules() map[string]CommandRule {
	return map[string]CommandRule{
		"git": {
			SubcommandRules: map[string]CommandRule{
				"commit": {
					FlagSlots: map[string]string{
						"m":       SlotMsg,
						"message": SlotMsg,
					},
				},
				"checkout": {
					FlagSlots: map[string]string{
						"b": SlotArg, // branch name
					},
					ArgSlots: map[int]string{
						0: SlotArg, // branch or path
					},
				},
				"push": {
					ArgSlots: map[int]string{
						0: SlotArg, // remote
						1: SlotArg, // branch
					},
				},
				"pull": {
					ArgSlots: map[int]string{
						0: SlotArg, // remote
						1: SlotArg, // branch
					},
				},
				"clone": {
					ArgSlots: map[int]string{
						0: SlotURL,  // repo URL
						1: SlotPath, // destination
					},
				},
				"add": {
					ArgSlots: map[int]string{
						-1: SlotPath, // all args are paths
					},
				},
				"diff": {
					ArgSlots: map[int]string{
						-1: SlotPath, // paths or commits
					},
				},
				"log": {
					ArgSlots: map[int]string{
						-1: SlotPath, // paths
					},
				},
				"show": {
					ArgSlots: map[int]string{
						0: SlotSHA, // commit
					},
				},
				"reset": {
					ArgSlots: map[int]string{
						0:  SlotSHA,  // commit or HEAD~n
						-1: SlotPath, // paths
					},
				},
				"revert": {
					ArgSlots: map[int]string{
						0: SlotSHA, // commit
					},
				},
				"cherry-pick": {
					ArgSlots: map[int]string{
						-1: SlotSHA, // commits
					},
				},
			},
		},
		"npm": {
			SubcommandRules: map[string]CommandRule{
				"install": {
					ArgSlots: map[int]string{
						-1: SlotArg, // package names
					},
				},
				"run": {
					ArgSlots: map[int]string{
						0: SlotArg, // script name
					},
				},
				"test": {},
			},
		},
		"pnpm": {
			SubcommandRules: map[string]CommandRule{
				"install": {
					ArgSlots: map[int]string{
						-1: SlotArg,
					},
				},
				"add": {
					ArgSlots: map[int]string{
						-1: SlotArg,
					},
				},
				"run": {
					ArgSlots: map[int]string{
						0: SlotArg,
					},
				},
			},
		},
		"yarn": {
			SubcommandRules: map[string]CommandRule{
				"add": {
					ArgSlots: map[int]string{
						-1: SlotArg,
					},
				},
				"run": {
					ArgSlots: map[int]string{
						0: SlotArg,
					},
				},
			},
		},
		"go": {
			SubcommandRules: map[string]CommandRule{
				"test": {
					ArgSlots: map[int]string{
						-1: SlotPath, // paths or ./...
					},
				},
				"build": {
					ArgSlots: map[int]string{
						-1: SlotPath,
					},
				},
				"run": {
					ArgSlots: map[int]string{
						0: SlotPath, // main file
					},
				},
				"get": {
					ArgSlots: map[int]string{
						-1: SlotURL, // module paths
					},
				},
			},
		},
		"pytest": {
			ArgSlots: map[int]string{
				-1: SlotPath, // test paths
			},
		},
		"python": {
			ArgSlots: map[int]string{
				0: SlotPath, // script path
			},
		},
		"python3": {
			ArgSlots: map[int]string{
				0: SlotPath,
			},
		},
		"make": {
			ArgSlots: map[int]string{
				-1: SlotArg, // targets
			},
		},
		"kubectl": {
			SubcommandRules: map[string]CommandRule{
				"get": {
					FlagSlots: map[string]string{
						"n":         SlotArg, // namespace
						"namespace": SlotArg,
					},
				},
				"describe": {
					FlagSlots: map[string]string{
						"n":         SlotArg,
						"namespace": SlotArg,
					},
				},
				"apply": {
					FlagSlots: map[string]string{
						"f": SlotPath, // file
					},
				},
				"delete": {
					FlagSlots: map[string]string{
						"n":         SlotArg,
						"namespace": SlotArg,
					},
				},
			},
		},
		"docker": {
			SubcommandRules: map[string]CommandRule{
				"run": {
					ArgSlots: map[int]string{
						0: SlotArg, // image
					},
				},
				"build": {
					FlagSlots: map[string]string{
						"t":   SlotArg, // tag
						"tag": SlotArg,
						"f":   SlotPath, // dockerfile
					},
					ArgSlots: map[int]string{
						0: SlotPath, // context
					},
				},
				"exec": {
					ArgSlots: map[int]string{
						0: SlotArg, // container
					},
				},
			},
		},
		"cd": {
			ArgSlots: map[int]string{
				0: SlotPath,
			},
		},
		"cat": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"less": {
			ArgSlots: map[int]string{
				0: SlotPath,
			},
		},
		"vim": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"nvim": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"code": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"rm": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"mv": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"cp": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"mkdir": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"touch": {
			ArgSlots: map[int]string{
				-1: SlotPath,
			},
		},
		"chmod": {
			ArgSlots: map[int]string{
				0:  SlotNum,  // mode
				-1: SlotPath, // paths
			},
		},
		"chown": {
			ArgSlots: map[int]string{
				0:  SlotArg,  // owner
				-1: SlotPath, // paths
			},
		},
		"curl": {
			ArgSlots: map[int]string{
				0: SlotURL,
			},
		},
		"wget": {
			ArgSlots: map[int]string{
				0: SlotURL,
			},
		},
	}
}

// NormalizeSimple is a convenience function that normalizes without tracking slots.
func NormalizeSimple(cmdRaw string) string {
	n := NewNormalizer()
	norm, _ := n.Normalize(cmdRaw)
	return norm
}
