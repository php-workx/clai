package cmd

import (
	"testing"
)

func TestCleanCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain command",
			input: "ls -la",
			want:  "ls -la",
		},
		{
			name:  "with backticks",
			input: "`ls -la`",
			want:  "ls -la",
		},
		{
			name:  "with triple backticks",
			input: "```ls -la```",
			want:  "ls -la",
		},
		{
			name:  "with dollar prefix",
			input: "$ ls -la",
			want:  "ls -la",
		},
		{
			name:  "with leading whitespace",
			input: "  ls -la  ",
			want:  "ls -la",
		},
		{
			name:  "with bash code block prefix",
			input: "bash\nls -la",
			want:  "ls -la",
		},
		{
			name:  "multiline takes first line",
			input: "ls -la\ncd ..\npwd",
			want:  "ls -la",
		},
		{
			name:  "complex command",
			input: "find . -name '*.go' -type f | xargs grep 'TODO'",
			want:  "find . -name '*.go' -type f | xargs grep 'TODO'",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only whitespace",
			input: "   \n   ",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanCommand(tt.input)
			if got != tt.want {
				t.Errorf("cleanCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestVoiceCmd_RequiresArgs(t *testing.T) {
	// Test that voice requires at least one argument
	err := voiceCmd.Args(voiceCmd, []string{})
	if err == nil {
		t.Error("voice should require at least 1 argument")
	}

	// Test that voice accepts arguments
	err = voiceCmd.Args(voiceCmd, []string{"list all files"})
	if err != nil {
		t.Errorf("voice should accept arguments, got error: %v", err)
	}

	// Test that voice accepts multiple arguments (they get joined)
	err = voiceCmd.Args(voiceCmd, []string{"list", "all", "files"})
	if err != nil {
		t.Errorf("voice should accept multiple arguments, got error: %v", err)
	}
}
