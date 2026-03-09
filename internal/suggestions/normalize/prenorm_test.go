package normalize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreNormalize_BasicCommands(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd string
	}{
		{"simple ls", "ls -la", "ls -la"},
		{"git commit", "git commit -m 'hello world'", "git commit -m 'hello world'"},
		{"echo", "ECHO hello", "echo hello"},
		{"whitespace collapse", "ls    -la     /tmp", "ls -la <PATH>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreNormalize(tt.input, PreNormConfig{})
			assert.Equal(t, tt.wantCmd, result.CmdNorm)
			assert.NotEmpty(t, result.TemplateID)
		})
	}
}

func TestPreNormalize_PathReplacement(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd string
	}{
		{"absolute path", "cat /etc/hosts", "cat <PATH>"},
		{"home path", "cd ~/Documents", "cd <PATH>"},
		{"relative dot path", "cat ./README.md", "cat <PATH>"},
		{"relative parent", "cd ../other", "cd <PATH>"},
		{"multiple paths", "cp /src/file.txt /dst/file.txt", "cp <PATH> <PATH>"},
		{"path with slash", "cat src/main.go", "cat <PATH>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreNormalize(tt.input, PreNormConfig{})
			assert.Equal(t, tt.wantCmd, result.CmdNorm)
		})
	}
}

func TestPreNormalize_UUIDReplacement(t *testing.T) {
	result := PreNormalize("docker rm 550e8400-e29b-41d4-a716-446655440000", PreNormConfig{})
	assert.Equal(t, "docker rm <UUID>", result.CmdNorm)
}

func TestPreNormalize_URLReplacement(t *testing.T) {
	result := PreNormalize("curl https://api.example.com/v1/users", PreNormConfig{})
	assert.Equal(t, "curl <URL>", result.CmdNorm)
}

func TestPreNormalize_NumericReplacement(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd string
	}{
		{"chmod with path", "chmod 755 ./script.sh", "chmod <NUM> <PATH>"},
		{"tail with path", "tail -100 /var/log/file.log", "tail -100 <PATH>"},
		{"bare number only", "kill 12345", "kill <NUM>"},
		{"bare filename no replace", "cat script.sh", "cat script.sh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PreNormalize(tt.input, PreNormConfig{})
			assert.Equal(t, tt.wantCmd, result.CmdNorm)
		})
	}
}

func TestPreNormalize_PipelineCommands(t *testing.T) {
	result := PreNormalize("cat /etc/hosts | grep localhost | wc -l", PreNormConfig{})
	assert.Equal(t, "cat <PATH> | grep localhost | wc -l", result.CmdNorm)
	require.Len(t, result.Segments, 3)
}

func TestPreNormalize_WithAliases(t *testing.T) {
	cfg := PreNormConfig{
		Aliases: map[string]string{
			"ll": "ls -la",
		},
	}
	result := PreNormalize("ll /tmp", cfg)
	assert.Equal(t, "ls -la <PATH>", result.CmdNorm)
	assert.True(t, result.AliasExpanded)
}

func TestPreNormalize_TemplateIDStability(t *testing.T) {
	r1 := PreNormalize("git commit -m 'msg'", PreNormConfig{})
	r2 := PreNormalize("git commit -m 'msg'", PreNormConfig{})
	assert.Equal(t, r1.TemplateID, r2.TemplateID)

	r3 := PreNormalize("git push origin main", PreNormConfig{})
	assert.NotEqual(t, r1.TemplateID, r3.TemplateID)
}

func TestPreNormalize_TemplateIDFormat(t *testing.T) {
	result := PreNormalize("ls -la", PreNormConfig{})
	assert.Len(t, result.TemplateID, 64)
	for _, ch := range result.TemplateID {
		assert.True(t, (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f'),
			"template ID should be lowercase hex, got char: %c", ch)
	}
}

func TestPreNormalize_EventSizeLimit(t *testing.T) {
	longCmd := "echo " + strings.Repeat("x", 20000)
	cfg := PreNormConfig{MaxEventBytes: 100}
	result := PreNormalize(longCmd, cfg)
	assert.True(t, result.Truncated)
	assert.LessOrEqual(t, len(result.CmdNorm), 100)
}

func TestPreNormalize_CaseInsensitiveCommandOnly(t *testing.T) {
	result := PreNormalize("GREP MyPattern", PreNormConfig{})
	assert.Equal(t, "grep MyPattern", result.CmdNorm)
}

func TestComputeTemplateID(t *testing.T) {
	id := ComputeTemplateID("ls -la")
	assert.Len(t, id, 64)

	id2 := ComputeTemplateID("ls -la")
	assert.Equal(t, id, id2)

	id3 := ComputeTemplateID("ls -l")
	assert.NotEqual(t, id, id3)
}

func TestNormalizeSegment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"simple", "ls", "ls"},
		{"whitespace", "  ls   -la  ", "ls -la"},
		{"uppercase cmd", "GIT status", "git status"},
		{"path", "cat /etc/hosts", "cat <PATH>"},
		{"uuid", "docker rm 550e8400-e29b-41d4-a716-446655440000", "docker rm <UUID>"},
		{"url", "curl https://example.com", "curl <URL>"},
		{"number", "kill 1234", "kill <NUM>"},
		{"flag preserved", "ls -la --color=auto", "ls -la --color=auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSegment(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkPreNormalize_Simple(b *testing.B) {
	cfg := PreNormConfig{}
	for i := 0; i < b.N; i++ {
		PreNormalize("git commit -m 'fix bug'", cfg)
	}
}

func BenchmarkPreNormalize_Pipeline(b *testing.B) {
	cfg := PreNormConfig{}
	for i := 0; i < b.N; i++ {
		PreNormalize("cat /etc/hosts | grep localhost | wc -l", cfg)
	}
}

func BenchmarkPreNormalize_WithAliases(b *testing.B) {
	cfg := PreNormConfig{
		Aliases: map[string]string{
			"ll": "ls -la",
			"g":  "git",
			"gc": "git commit",
		},
	}
	for i := 0; i < b.N; i++ {
		PreNormalize("ll /tmp", cfg)
	}
}
