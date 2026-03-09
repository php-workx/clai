package alias

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseBashAliases ---

func TestParseBashAliases_Simple(t *testing.T) {
	output := `alias gs='git status'
alias ll='ls -la'
alias gp='git push'
`
	aliases := parseBashAliases(output)
	assert.Equal(t, "git status", aliases["gs"])
	assert.Equal(t, "ls -la", aliases["ll"])
	assert.Equal(t, "git push", aliases["gp"])
	assert.Len(t, aliases, 3)
}

func TestParseBashAliases_DoubleQuoted(t *testing.T) {
	output := `alias gs="git status"
`
	aliases := parseBashAliases(output)
	assert.Equal(t, "git status", aliases["gs"])
}

func TestParseBashAliases_EmptyOutput(t *testing.T) {
	aliases := parseBashAliases("")
	assert.Empty(t, aliases)
}

func TestParseBashAliases_WithSpecialChars(t *testing.T) {
	output := `alias grep='grep --color=auto'
alias la='ls -A'
`
	aliases := parseBashAliases(output)
	assert.Equal(t, "grep --color=auto", aliases["grep"])
	assert.Equal(t, "ls -A", aliases["la"])
}

func TestParseBashAliases_SkipsNonAliasLines(t *testing.T) {
	output := `some random output
alias gs='git status'
another line
`
	aliases := parseBashAliases(output)
	assert.Len(t, aliases, 1)
	assert.Equal(t, "git status", aliases["gs"])
}

func TestParseBashAliases_NoEquals(t *testing.T) {
	output := `alias broken
alias gs='git status'
`
	aliases := parseBashAliases(output)
	assert.Len(t, aliases, 1)
	assert.Equal(t, "git status", aliases["gs"])
}

// --- parseZshAliases ---

func TestParseZshAliases_Simple(t *testing.T) {
	output := `gs=git status
ll=ls -la
gp=git push
`
	aliases := parseZshAliases(output)
	assert.Equal(t, "git status", aliases["gs"])
	assert.Equal(t, "ls -la", aliases["ll"])
	assert.Equal(t, "git push", aliases["gp"])
	assert.Len(t, aliases, 3)
}

func TestParseZshAliases_Quoted(t *testing.T) {
	output := `gs='git status'
ll='ls -la'
`
	aliases := parseZshAliases(output)
	assert.Equal(t, "git status", aliases["gs"])
	assert.Equal(t, "ls -la", aliases["ll"])
}

func TestParseZshAliases_EmptyOutput(t *testing.T) {
	aliases := parseZshAliases("")
	assert.Empty(t, aliases)
}

func TestParseZshAliases_EmptyLines(t *testing.T) {
	output := `
gs=git status

ll=ls -la

`
	aliases := parseZshAliases(output)
	assert.Len(t, aliases, 2)
}

func TestParseZshAliases_NoEquals(t *testing.T) {
	output := `broken
gs=git status
`
	aliases := parseZshAliases(output)
	assert.Len(t, aliases, 1)
}

// --- parseFishAbbreviations ---

func TestParseFishAbbreviations_ModernFormat(t *testing.T) {
	output := `abbr -a -- gs git status
abbr -a -- ll ls -la
abbr -a -- gp git push
`
	aliases := parseFishAbbreviations(output)
	assert.Equal(t, "git status", aliases["gs"])
	assert.Equal(t, "ls -la", aliases["ll"])
	assert.Equal(t, "git push", aliases["gp"])
	assert.Len(t, aliases, 3)
}

func TestParseFishAbbreviations_AltModernFormat(t *testing.T) {
	output := `abbr -a gs -- git status
abbr -a ll -- ls -la
`
	aliases := parseFishAbbreviations(output)
	assert.Equal(t, "git status", aliases["gs"])
	assert.Equal(t, "ls -la", aliases["ll"])
}

func TestParseFishAbbreviations_SimpleFormat(t *testing.T) {
	output := `abbr -a gs git status
`
	aliases := parseFishAbbreviations(output)
	assert.Equal(t, "git status", aliases["gs"])
}

func TestParseFishAbbreviations_LegacyFormat(t *testing.T) {
	output := `abbr gs git status
`
	aliases := parseFishAbbreviations(output)
	assert.Equal(t, "git status", aliases["gs"])
}

func TestParseFishAbbreviations_EmptyOutput(t *testing.T) {
	aliases := parseFishAbbreviations("")
	assert.Empty(t, aliases)
}

func TestParseFishAbbreviations_QuotedExpansion(t *testing.T) {
	output := `abbr -a -- gs 'git status'
`
	aliases := parseFishAbbreviations(output)
	assert.Equal(t, "git status", aliases["gs"])
}

// --- unquote ---

func TestUnquote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"'hello'", "hello"},
		{`"hello"`, "hello"},
		{"hello", "hello"},
		{"''", ""},
		{`""`, ""},
		{"'", "'"},
		{`"`, `"`},
		{"", ""},
		{"'mismatched\"", "'mismatched\""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, unquote(tt.input), "unquote(%q)", tt.input)
	}
}

// --- splitFirstWord ---

func TestSplitFirstWord(t *testing.T) {
	tests := []struct {
		input     string
		wantFirst string
		wantRest  string
	}{
		{"", "", ""},
		{"  ", "", ""},
		{"hello", "hello", ""},
		{"hello world", "hello", "world"},
		{"hello  world  foo", "hello", "world  foo"},
		{"\thello\tworld", "hello", "world"},
	}
	for _, tt := range tests {
		first, rest := splitFirstWord(tt.input)
		assert.Equal(t, tt.wantFirst, first, "splitFirstWord(%q) first", tt.input)
		assert.Equal(t, tt.wantRest, rest, "splitFirstWord(%q) rest", tt.input)
	}
}

// --- ShouldResnapshot ---

func TestShouldResnapshot(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"alias gs='git status'", true},
		{"alias -p", true},
		{"unalias gs", true},
		{"abbr -a gs git status", true},
		{"ALIAS gs='git status'", true}, // case insensitive
		{"git status", false},
		{"ls -la", false},
		{"", false},
		{"   ", false},
		{"aliases list", false},
		{"unaliased", false}, // "unaliased" != "unalias"
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ShouldResnapshot(tt.cmd), "ShouldResnapshot(%q)", tt.cmd)
	}
}

// --- BuildReverseMap ---

func TestBuildReverseMap_Basic(t *testing.T) {
	aliases := AliasMap{
		"gs": "git status",
		"gp": "git push",
		"ll": "ls -la",
	}
	rm := BuildReverseMap(aliases)
	require.Len(t, rm, 3)

	// Sorted by expansion length descending
	// "git status" (10) >= "git push" (8) >= "ls -la" (5)
	assert.Equal(t, "git status", rm[0].Expansion)
	assert.Equal(t, "gs", rm[0].AliasName)
}

func TestBuildReverseMap_DuplicateExpansions(t *testing.T) {
	aliases := AliasMap{
		"gs":        "git status",
		"gitstatus": "git status",
	}
	rm := BuildReverseMap(aliases)
	require.Len(t, rm, 1)
	// Should prefer shortest alias name
	assert.Equal(t, "gs", rm[0].AliasName)
}

func TestBuildReverseMap_EmptyExpansion(t *testing.T) {
	aliases := AliasMap{
		"gs":    "git status",
		"empty": "",
	}
	rm := BuildReverseMap(aliases)
	require.Len(t, rm, 1)
	assert.Equal(t, "git status", rm[0].Expansion)
}

func TestBuildReverseMap_Nil(t *testing.T) {
	rm := BuildReverseMap(nil)
	assert.Nil(t, rm)
}

func TestBuildReverseMap_Empty(t *testing.T) {
	rm := BuildReverseMap(AliasMap{})
	assert.Nil(t, rm)
}

func TestBuildReverseMap_SortOrder(t *testing.T) {
	aliases := AliasMap{
		"a": "short",
		"b": "medium cmd",
		"c": "very long command here",
	}
	rm := BuildReverseMap(aliases)
	require.Len(t, rm, 3)
	// Longest expansion first
	assert.Equal(t, "very long command here", rm[0].Expansion)
	assert.Equal(t, "medium cmd", rm[1].Expansion)
	assert.Equal(t, "short", rm[2].Expansion)
}

// --- RenderWithAliases ---

func TestRenderWithAliases_ExactMatch(t *testing.T) {
	rm := BuildReverseMap(AliasMap{
		"gs": "git status",
	})
	result := RenderWithAliases("git status", rm)
	assert.Equal(t, "gs", result)
}

func TestRenderWithAliases_PrefixMatch(t *testing.T) {
	rm := BuildReverseMap(AliasMap{
		"gs": "git status",
	})
	result := RenderWithAliases("git status --short", rm)
	assert.Equal(t, "gs --short", result)
}

func TestRenderWithAliases_NoMatch(t *testing.T) {
	rm := BuildReverseMap(AliasMap{
		"gs": "git status",
	})
	result := RenderWithAliases("git push origin main", rm)
	assert.Equal(t, "git push origin main", result)
}

func TestRenderWithAliases_LongestMatchFirst(t *testing.T) {
	rm := BuildReverseMap(AliasMap{
		"g":  "git",
		"gs": "git status",
	})
	// "git status --short" should match "git status" first (longer)
	result := RenderWithAliases("git status --short", rm)
	assert.Equal(t, "gs --short", result)
}

func TestRenderWithAliases_EmptyCommand(t *testing.T) {
	rm := BuildReverseMap(AliasMap{
		"gs": "git status",
	})
	result := RenderWithAliases("", rm)
	assert.Equal(t, "", result)
}

func TestRenderWithAliases_NilReverseMap(t *testing.T) {
	result := RenderWithAliases("git status", nil)
	assert.Equal(t, "git status", result)
}

func TestRenderWithAliases_WordBoundary(t *testing.T) {
	rm := BuildReverseMap(AliasMap{
		"gs": "git status",
	})
	// "git statusbar" should NOT be matched because "bar" doesn't start with whitespace
	result := RenderWithAliases("git statusbar", rm)
	assert.Equal(t, "git statusbar", result)
}

func TestRenderWithAliases_TabSeparator(t *testing.T) {
	rm := BuildReverseMap(AliasMap{
		"gs": "git status",
	})
	result := RenderWithAliases("git status\t--short", rm)
	assert.Equal(t, "gs\t--short", result)
}
