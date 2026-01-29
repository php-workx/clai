package provider

import (
	"testing"
)

func TestNewOpenAIProvider(t *testing.T) {
	p := NewOpenAIProvider()
	if p == nil {
		t.Fatal("NewOpenAIProvider() returned nil")
	}
	if p.sanitizer == nil {
		t.Error("NewOpenAIProvider() created provider with nil sanitizer")
	}
	if p.model != "gpt-4o" {
		t.Errorf("NewOpenAIProvider() model = %q, want %q", p.model, "gpt-4o")
	}
}

func TestNewOpenAIProviderWithModel(t *testing.T) {
	p := NewOpenAIProviderWithModel("gpt-4-turbo")
	if p == nil {
		t.Fatal("NewOpenAIProviderWithModel() returned nil")
	}
	if p.model != "gpt-4-turbo" {
		t.Errorf("NewOpenAIProviderWithModel() model = %q, want %q", p.model, "gpt-4-turbo")
	}
}

func TestOpenAIProvider_Name(t *testing.T) {
	p := NewOpenAIProvider()
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
}

func TestOpenAIProvider_parseCommandResponse(t *testing.T) {
	p := NewOpenAIProvider()

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
			name:     "skip comments",
			response: "# This is a comment\nls -la",
			expected: []string{"ls -la"},
		},
		{
			name:     "empty response",
			response: "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseCommandResponse(tt.response)

			if len(result) != len(tt.expected) {
				t.Fatalf("parseCommandResponse() returned %d suggestions, want %d",
					len(result), len(tt.expected))
			}

			for i, expected := range tt.expected {
				if result[i].Text != expected {
					t.Errorf("parseCommandResponse()[%d].Text = %q, want %q",
						i, result[i].Text, expected)
				}
			}
		})
	}
}

func TestOpenAIProvider_parseCommandResponse_RiskDetection(t *testing.T) {
	p := NewOpenAIProvider()

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

func TestOpenAIProvider_parseDiagnoseResponse(t *testing.T) {
	p := NewOpenAIProvider()

	tests := []struct {
		name                string
		response            string
		expectedExplanation string
		expectedFixes       []string
	}{
		{
			name: "explanation with fixes",
			response: `The package.json file is missing.

$ npm init -y`,
			expectedExplanation: "The package.json file is missing.",
			expectedFixes:       []string{"npm init -y"},
		},
		{
			name:                "explanation only",
			response:            "Command not found in PATH.",
			expectedExplanation: "Command not found in PATH.",
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
				t.Fatalf("parseDiagnoseResponse() returned %d fixes, want %d",
					len(fixes), len(tt.expectedFixes))
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
