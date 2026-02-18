package provider

import (
	"testing"
)

// TestParseCommandResponse tests the shared ParseCommandResponse function directly
func TestParseCommandResponse(t *testing.T) {
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
			name:     "empty response",
			response: "",
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
			result := ParseCommandResponse(tt.response)

			if len(result) != len(tt.expected) {
				t.Fatalf("ParseCommandResponse() returned %d suggestions, want %d",
					len(result), len(tt.expected))
			}

			for i, expected := range tt.expected {
				if result[i].Text != expected {
					t.Errorf("ParseCommandResponse()[%d].Text = %q, want %q",
						i, result[i].Text, expected)
				}
				if result[i].Source != SourceAI {
					t.Errorf("ParseCommandResponse()[%d].Source = %q, want %q",
						i, result[i].Source, SourceAI)
				}
			}
		})
	}
}

// TestParseCommandResponse_Scores verifies that scores decrease with position
func TestParseCommandResponse_Scores(t *testing.T) {
	response := "cmd1\ncmd2\ncmd3"
	result := ParseCommandResponse(response)

	if len(result) != 3 {
		t.Fatalf("Expected 3 suggestions, got %d", len(result))
	}

	// Scores should decrease
	if result[0].Score <= result[1].Score {
		t.Errorf("First suggestion score (%f) should be higher than second (%f)",
			result[0].Score, result[1].Score)
	}
	if result[1].Score <= result[2].Score {
		t.Errorf("Second suggestion score (%f) should be higher than third (%f)",
			result[1].Score, result[2].Score)
	}

	// Score should not go below 0.1
	for _, s := range result {
		if s.Score < 0.1 {
			t.Errorf("Score %f should not be below 0.1", s.Score)
		}
	}
}

// TestParseDiagnoseResponse tests the shared ParseDiagnoseResponse function directly
func TestParseDiagnoseResponse(t *testing.T) {
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
			name: "numbered fixes after explanation",
			response: `The directory does not exist.

1. mkdir -p /path/to/dir
2. cd /path/to/dir`,
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
		{
			name:                "multi-line explanation concatenated",
			response:            "Line one.\nLine two.",
			expectedExplanation: "Line one. Line two.",
			expectedFixes:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explanation, fixes := ParseDiagnoseResponse(tt.response)

			if explanation != tt.expectedExplanation {
				t.Errorf("ParseDiagnoseResponse() explanation = %q, want %q",
					explanation, tt.expectedExplanation)
			}

			if len(fixes) != len(tt.expectedFixes) {
				t.Fatalf("ParseDiagnoseResponse() returned %d fixes, want %d",
					len(fixes), len(tt.expectedFixes))
			}

			for i, expected := range tt.expectedFixes {
				if fixes[i].Text != expected {
					t.Errorf("ParseDiagnoseResponse() fix[%d].Text = %q, want %q",
						i, fixes[i].Text, expected)
				}
				if fixes[i].Source != SourceAI {
					t.Errorf("ParseDiagnoseResponse() fix[%d].Source = %q, want %q",
						i, fixes[i].Source, SourceAI)
				}
			}
		})
	}
}

// TestParseDiagnoseResponse_FixScores verifies that fix scores decrease with position
func TestParseDiagnoseResponse_FixScores(t *testing.T) {
	response := `Error message.

$ fix1
$ fix2
$ fix3`

	_, fixes := ParseDiagnoseResponse(response)

	if len(fixes) != 3 {
		t.Fatalf("Expected 3 fixes, got %d", len(fixes))
	}

	// Scores should decrease
	if fixes[0].Score <= fixes[1].Score {
		t.Errorf("First fix score (%f) should be higher than second (%f)",
			fixes[0].Score, fixes[1].Score)
	}
	if fixes[1].Score <= fixes[2].Score {
		t.Errorf("Second fix score (%f) should be higher than third (%f)",
			fixes[1].Score, fixes[2].Score)
	}

	// Score should not go below 0.1
	for _, fix := range fixes {
		if fix.Score < 0.1 {
			t.Errorf("Score %f should not be below 0.1", fix.Score)
		}
	}
}

// TestCleanCommandPrefix_TwoDigit tests two-digit numbered prefix handling
func TestCleanCommandPrefix_TwoDigit(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1. ls -la", "ls -la"},
		{"1) ls -la", "ls -la"},
		{"12. ls -la", "ls -la"},
		{"12) ls -la", "ls -la"},
		{"99. pwd", "pwd"},
		// Short inputs — too short to match numbered prefix pattern
		{"1)", "1)"},
		{"1.", "1."},
		{"12", "12"},
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

// TestShouldSkipLine tests the shouldSkipLine helper function
func TestShouldSkipLine(t *testing.T) {
	tests := []struct {
		line     string
		expected bool
	}{
		{"# comment", true},
		{"// comment", true},
		{"Here is the command", true},
		{"The following", true},
		{"This will", true},
		{"Note: be careful", true},
		{"---", true},
		{"ls -la", false},
		{"git status", false},
		{"echo hello", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			result := shouldSkipLine(tt.line)
			if result != tt.expected {
				t.Errorf("shouldSkipLine(%q) = %v, want %v", tt.line, result, tt.expected)
			}
		})
	}
}

// TestStartsFixSection tests the startsFixSection helper function
func TestStartsFixSection(t *testing.T) {
	tests := []struct {
		line     string
		expected bool
	}{
		{"1. command", true},
		{"1) command", true},
		{"9. command", true},
		{"10. command", true},
		{"99. command", true},
		{"- command", true},
		{"* command", true},
		{"command", false},
		{"# comment", false},
		{"0. zero", false},
		{"a. alpha", false},
		{"", false},
		{"x", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			result := startsFixSection(tt.line)
			if result != tt.expected {
				t.Errorf("startsFixSection(%q) = %v, want %v", tt.line, result, tt.expected)
			}
		})
	}
}

// TestCreateSuggestion tests the createSuggestion helper function
func TestCreateSuggestion(t *testing.T) {
	tests := []struct {
		text         string
		expectedRisk string
		index        int
	}{
		{"ls -la", "safe", 0},
		{"rm -rf /tmp", "destructive", 0},
		{"git push --force", "destructive", 0},
		{"echo hello", "safe", 0},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			result := createSuggestion(tt.text, tt.index)
			if result.Text != tt.text {
				t.Errorf("createSuggestion().Text = %q, want %q", result.Text, tt.text)
			}
			if result.Risk != tt.expectedRisk {
				t.Errorf("createSuggestion().Risk = %q, want %q", result.Risk, tt.expectedRisk)
			}
			if result.Source != SourceAI {
				t.Errorf("createSuggestion().Source = %q, want %q", result.Source, SourceAI)
			}
		})
	}
}

// TestCreateSuggestion_ScoreByIndex tests that score decreases with index
func TestCreateSuggestion_ScoreByIndex(t *testing.T) {
	s0 := createSuggestion("cmd", 0)
	s1 := createSuggestion("cmd", 1)
	s2 := createSuggestion("cmd", 2)
	s9 := createSuggestion("cmd", 9)

	if s0.Score <= s1.Score {
		t.Errorf("Score at index 0 (%f) should be > index 1 (%f)", s0.Score, s1.Score)
	}
	if s1.Score <= s2.Score {
		t.Errorf("Score at index 1 (%f) should be > index 2 (%f)", s1.Score, s2.Score)
	}
	// Score should floor at 0.1
	if s9.Score < 0.1 {
		t.Errorf("Score at index 9 (%f) should not be below 0.1", s9.Score)
	}
}
