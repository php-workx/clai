package provider

import (
	"testing"
)

func TestNewAnthropicProvider(t *testing.T) {
	p := NewAnthropicProvider()
	if p == nil {
		t.Fatal("NewAnthropicProvider() returned nil")
	}
	if p.sanitizer == nil {
		t.Error("NewAnthropicProvider() created provider with nil sanitizer")
	}
	if p.model != "" {
		t.Errorf("NewAnthropicProvider() model = %q, want empty string", p.model)
	}
}

func TestNewAnthropicProviderWithModel(t *testing.T) {
	p := NewAnthropicProviderWithModel("claude-3-opus")
	if p == nil {
		t.Fatal("NewAnthropicProviderWithModel() returned nil")
	}
	if p.model != "claude-3-opus" {
		t.Errorf("NewAnthropicProviderWithModel() model = %q, want %q", p.model, "claude-3-opus")
	}
}

func TestAnthropicProvider_Name(t *testing.T) {
	p := NewAnthropicProvider()
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
}

func TestAnthropicProvider_parseCommandResponse(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		name     string
		response string
		expected []string
	}{
		{
			name:     "single command",
			response: "ls -la",
			expected: []string{"ls -la"},
		},
		{
			name:     "multiple commands",
			response: "git status\ngit add .\ngit commit -m 'test'",
			expected: []string{"git status", "git add .", "git commit -m 'test'"},
		},
		{
			name:     "numbered commands",
			response: "1. ls -la\n2. pwd\n3. echo hello",
			expected: []string{"ls -la", "pwd", "echo hello"},
		},
		{
			name:     "commands with bullet points",
			response: "- ls -la\n- pwd",
			expected: []string{"ls -la", "pwd"},
		},
		{
			name:     "commands with $ prefix",
			response: "$ ls -la\n$ pwd",
			expected: []string{"ls -la", "pwd"},
		},
		{
			name:     "skip comments",
			response: "# This is a comment\nls -la\n# Another comment",
			expected: []string{"ls -la"},
		},
		{
			name:     "skip explanatory text",
			response: "Here is the command:\nls -la\nThis will list all files",
			expected: []string{"ls -la"},
		},
		{
			name:     "commands with backticks",
			response: "`ls -la`\n`pwd`",
			expected: []string{"ls -la", "pwd"},
		},
		{
			name:     "empty response",
			response: "",
			expected: nil,
		},
		{
			name:     "only whitespace",
			response: "   \n\n   ",
			expected: nil,
		},
		{
			name:     "limit to 3 suggestions",
			response: "cmd1\ncmd2\ncmd3\ncmd4\ncmd5",
			expected: []string{"cmd1", "cmd2", "cmd3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseCommandResponse(tt.response)

			if len(result) != len(tt.expected) {
				t.Fatalf("parseCommandResponse() returned %d suggestions, want %d\nResponse: %q\nResult: %+v",
					len(result), len(tt.expected), tt.response, result)
			}

			for i, expected := range tt.expected {
				if result[i].Text != expected {
					t.Errorf("parseCommandResponse()[%d].Text = %q, want %q",
						i, result[i].Text, expected)
				}
				if result[i].Source != "ai" {
					t.Errorf("parseCommandResponse()[%d].Source = %q, want %q",
						i, result[i].Source, "ai")
				}
			}
		})
	}
}

func TestAnthropicProvider_parseCommandResponse_RiskDetection(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		response     string
		expectedRisk string
	}{
		{
			response:     "rm -rf /tmp/test",
			expectedRisk: "destructive",
		},
		{
			response:     "ls -la",
			expectedRisk: "safe",
		},
		{
			response:     "git push --force",
			expectedRisk: "destructive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.response, func(t *testing.T) {
			result := p.parseCommandResponse(tt.response)
			if len(result) == 0 {
				t.Fatal("parseCommandResponse() returned no suggestions")
			}
			if result[0].Risk != tt.expectedRisk {
				t.Errorf("parseCommandResponse() risk = %q, want %q", result[0].Risk, tt.expectedRisk)
			}
		})
	}
}

func TestAnthropicProvider_parseDiagnoseResponse(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		name                string
		response            string
		expectedExplanation string
		expectedFixes       []string
	}{
		{
			name: "explanation with $ prefixed fixes",
			response: `The command failed because package.json is missing.

$ npm init -y
$ npm install`,
			expectedExplanation: "The command failed because package.json is missing.",
			expectedFixes:       []string{"npm init -y", "npm install"},
		},
		{
			name: "numbered explanation and fixes",
			response: `The directory does not exist.

$ mkdir -p /path/to/dir
$ cd /path/to/dir`,
			expectedExplanation: "The directory does not exist.",
			expectedFixes:       []string{"mkdir -p /path/to/dir", "cd /path/to/dir"},
		},
		{
			name:                "explanation only",
			response:            "The command was not found in PATH.",
			expectedExplanation: "The command was not found in PATH.",
			expectedFixes:       nil,
		},
		{
			name:                "empty response",
			response:            "",
			expectedExplanation: "",
			expectedFixes:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explanation, fixes := p.parseDiagnoseResponse(tt.response)

			if explanation != tt.expectedExplanation {
				t.Errorf("parseDiagnoseResponse() explanation = %q, want %q",
					explanation, tt.expectedExplanation)
			}

			if len(fixes) != len(tt.expectedFixes) {
				t.Fatalf("parseDiagnoseResponse() returned %d fixes, want %d\nFixes: %+v",
					len(fixes), len(tt.expectedFixes), fixes)
			}

			for i, expected := range tt.expectedFixes {
				if fixes[i].Text != expected {
					t.Errorf("parseDiagnoseResponse() fix[%d].Text = %q, want %q",
						i, fixes[i].Text, expected)
				}
			}
		})
	}
}

func TestCleanCommandPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1. ls -la", "ls -la"},
		{"2) pwd", "pwd"},
		{"- echo hello", "echo hello"},
		{"* git status", "git status"},
		{"$ npm install", "npm install"},
		{"`ls -la`", "ls -la"},
		{"ls -la", "ls -la"},
		{"10. longer command", "longer command"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := cleanCommandPrefix(tt.input)
			if result != tt.expected {
				t.Errorf("cleanCommandPrefix(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAnthropicProvider_Score(t *testing.T) {
	p := NewAnthropicProvider()

	// Multiple commands should have decreasing scores
	response := "cmd1\ncmd2\ncmd3"
	result := p.parseCommandResponse(response)

	if len(result) != 3 {
		t.Fatalf("Expected 3 suggestions, got %d", len(result))
	}

	if result[0].Score <= result[1].Score {
		t.Errorf("First suggestion score (%f) should be higher than second (%f)",
			result[0].Score, result[1].Score)
	}
	if result[1].Score <= result[2].Score {
		t.Errorf("Second suggestion score (%f) should be higher than third (%f)",
			result[1].Score, result[2].Score)
	}
}
