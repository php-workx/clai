package normalize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAliasExpand_NilExpander(t *testing.T) {
	var e *AliasExpander
	result, expanded := e.Expand("ls -la")
	assert.Equal(t, "ls -la", result)
	assert.False(t, expanded)
}

func TestAliasExpand_NoMatch(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{"ll": "ls -la"},
	}
	result, expanded := e.Expand("cat file.txt")
	assert.Equal(t, "cat file.txt", result)
	assert.False(t, expanded)
}

func TestAliasExpand_Simple(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{"ll": "ls -la"},
	}
	result, expanded := e.Expand("ll")
	assert.Equal(t, "ls -la", result)
	assert.True(t, expanded)
}

func TestAliasExpand_WithArgs(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{"ll": "ls -la"},
	}
	result, expanded := e.Expand("ll /tmp")
	assert.Equal(t, "ls -la /tmp", result)
	assert.True(t, expanded)
}

func TestAliasExpand_Chained(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{
			"ll": "ls -la",
			"l":  "ll",
		},
	}
	result, expanded := e.Expand("l")
	assert.Equal(t, "ls -la", result)
	assert.True(t, expanded)
}

func TestAliasExpand_MultiDepth(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{
			"a": "b",
			"b": "c",
			"c": "d",
			"d": "echo hello",
		},
	}
	result, expanded := e.Expand("a")
	assert.Equal(t, "echo hello", result)
	assert.True(t, expanded)
}

func TestAliasExpand_CycleDetection(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{
			"a": "b args",
			"b": "a args",
		},
	}
	result, expanded := e.Expand("a")
	assert.True(t, expanded)
	assert.Contains(t, result, "args")
}

func TestAliasExpand_SelfReferencing(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{
			"ls": "ls --color=auto",
		},
	}
	result, expanded := e.Expand("ls /tmp")
	assert.Equal(t, "ls --color=auto /tmp", result)
	assert.True(t, expanded)
}

func TestAliasExpand_EmptyCommand(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{"ll": "ls -la"},
	}
	result, expanded := e.Expand("")
	assert.Equal(t, "", result)
	assert.False(t, expanded)
}

func TestAliasExpand_WhitespaceCommand(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{"ll": "ls -la"},
	}
	result, expanded := e.Expand("   ")
	assert.Equal(t, "   ", result)
	assert.False(t, expanded)
}

func TestAliasExpand_BoundedDepth(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{
			"a1": "a2",
			"a2": "a3",
			"a3": "a4",
			"a4": "a5",
			"a5": "a6",
			"a6": "a7",
		},
	}
	result, expanded := e.Expand("a1")
	assert.True(t, expanded)
	assert.Equal(t, "a6", result)
}

func TestAliasExpand_CustomMaxDepth(t *testing.T) {
	e := &AliasExpander{
		Aliases: map[string]string{
			"a": "b",
			"b": "c",
			"c": "echo done",
		},
		MaxDepth: 2,
	}
	result, expanded := e.Expand("a")
	assert.True(t, expanded)
	assert.Equal(t, "c", result)
}

func TestSplitFirstToken(t *testing.T) {
	tests := []struct {
		input     string
		wantFirst string
		wantRest  string
	}{
		{"", "", ""},
		{"  ", "", ""},
		{"ls", "ls", ""},
		{"ls -la", "ls", "-la"},
		{"ls  -la  /tmp", "ls", "-la  /tmp"},
		{"\tls\t-la", "ls", "-la"},
	}

	for _, tt := range tests {
		first, rest := splitFirstToken(tt.input)
		assert.Equal(t, tt.wantFirst, first, "input: %q", tt.input)
		assert.Equal(t, tt.wantRest, rest, "input: %q", tt.input)
	}
}
