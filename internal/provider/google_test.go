package provider

import (
	"testing"
)

func TestNewGoogleProvider(t *testing.T) {
	p := NewGoogleProvider()
	if p == nil {
		t.Fatal("NewGoogleProvider() returned nil")
	}
	if p.sanitizer == nil {
		t.Error("NewGoogleProvider() created provider with nil sanitizer")
	}
	if p.model != "gemini-pro" {
		t.Errorf("NewGoogleProvider() model = %q, want %q", p.model, "gemini-pro")
	}
}

func TestNewGoogleProviderWithModel(t *testing.T) {
	p := NewGoogleProviderWithModel("gemini-ultra")
	if p == nil {
		t.Fatal("NewGoogleProviderWithModel() returned nil")
	}
	if p.model != "gemini-ultra" {
		t.Errorf("NewGoogleProviderWithModel() model = %q, want %q", p.model, "gemini-ultra")
	}
}

func TestGoogleProvider_Name(t *testing.T) {
	p := NewGoogleProvider()
	if p.Name() != "google" {
		t.Errorf("Name() = %q, want %q", p.Name(), "google")
	}
}

func TestGoogleProvider_parseCommandResponse(t *testing.T) {
	p := NewGoogleProvider()

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
			response: "git status\ngit add .",
			expected: []string{"git status", "git add ."},
		},
		{
			name:     "numbered commands",
			response: "1. ls -la\n2. pwd",
			expected: []string{"ls -la", "pwd"},
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

func TestGoogleProvider_parseCommandResponse_RiskDetection(t *testing.T) {
	p := NewGoogleProvider()

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

func TestGoogleProvider_parseDiagnoseResponse(t *testing.T) {
	p := NewGoogleProvider()

	tests := []struct {
		name                string
		response            string
		expectedExplanation string
		expectedFixes       []string
	}{
		{
			name: "explanation with fixes",
			response: `File not found.

$ touch missing_file.txt`,
			expectedExplanation: "File not found.",
			expectedFixes:       []string{"touch missing_file.txt"},
		},
		{
			name:                "explanation only",
			response:            "Permission denied.",
			expectedExplanation: "Permission denied.",
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
