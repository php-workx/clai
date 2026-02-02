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

func TestCmdCmd_RequiresArgs(t *testing.T) {
	// Test that cmd requires at least one argument
	err := cmdCmd.Args(cmdCmd, []string{})
	if err == nil {
		t.Error("cmd should require at least 1 argument")
	}

	// Test that cmd accepts arguments
	err = cmdCmd.Args(cmdCmd, []string{"list all files"})
	if err != nil {
		t.Errorf("cmd should accept arguments, got error: %v", err)
	}

	// Test that cmd accepts multiple arguments (they get joined)
	err = cmdCmd.Args(cmdCmd, []string{"list", "all", "files"})
	if err != nil {
		t.Errorf("cmd should accept multiple arguments, got error: %v", err)
	}
}
