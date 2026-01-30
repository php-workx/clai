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

// Additional comprehensive tests for parseCommandResponse

func TestAnthropicProvider_parseCommandResponse_SkipsProseLines(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		name     string
		response string
		expected []string
	}{
		{
			name:     "skips lines starting with Here",
			response: "Here is how to do it:\nls -la",
			expected: []string{"ls -la"},
		},
		{
			name:     "skips lines starting with The",
			response: "The following command will help:\nfind . -name '*.go'",
			expected: []string{"find . -name '*.go'"},
		},
		{
			name:     "skips lines starting with This",
			response: "This will work:\npwd",
			expected: []string{"pwd"},
		},
		{
			name:     "skips lines starting with Note:",
			response: "Note: Be careful with this\nrm -rf temp",
			expected: []string{"rm -rf temp"},
		},
		{
			name:     "skips separator lines ---",
			response: "---\ngit status\n---",
			expected: []string{"git status"},
		},
		{
			name:     "skips hash comments",
			response: "# List files\nls\n# Show directory",
			expected: []string{"ls"},
		},
		{
			name:     "skips double-slash comments",
			response: "// This is a comment\necho hello\n// Another",
			expected: []string{"echo hello"},
		},
		{
			name:     "mixed prose and commands",
			response: "Here is the solution:\n# Setup\nls -la\nThe above lists files\npwd\nThis shows the path",
			expected: []string{"ls -la", "pwd"},
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
			}
		})
	}
}

func TestAnthropicProvider_parseCommandResponse_RemovesPrefixes(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		name     string
		response string
		expected string
	}{
		{
			name:     "removes numbered prefix with dot",
			response: "1. ls -la",
			expected: "ls -la",
		},
		{
			name:     "removes numbered prefix with paren",
			response: "1) pwd",
			expected: "pwd",
		},
		{
			name:     "removes double-digit numbered prefix",
			response: "10. echo test",
			expected: "echo test",
		},
		{
			name:     "removes dash bullet",
			response: "- git status",
			expected: "git status",
		},
		{
			name:     "removes asterisk bullet",
			response: "* npm install",
			expected: "npm install",
		},
		{
			name:     "removes dollar sign prefix",
			response: "$ make build",
			expected: "make build",
		},
		{
			name:     "removes single backticks",
			response: "`cat file.txt`",
			expected: "cat file.txt",
		},
		{
			name:     "handles multiple leading backticks",
			response: "```ls```",
			expected: "ls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseCommandResponse(tt.response)
			if len(result) != 1 {
				t.Fatalf("parseCommandResponse() returned %d suggestions, want 1", len(result))
			}
			if result[0].Text != tt.expected {
				t.Errorf("parseCommandResponse().Text = %q, want %q", result[0].Text, tt.expected)
			}
		})
	}
}

func TestAnthropicProvider_parseCommandResponse_DestructiveCommands(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		name         string
		response     string
		expectedRisk string
	}{
		{
			name:         "rm -rf is destructive",
			response:     "rm -rf ./temp",
			expectedRisk: "destructive",
		},
		{
			name:         "rm -r is destructive",
			response:     "rm -r directory",
			expectedRisk: "destructive",
		},
		{
			name:         "git push --force is destructive",
			response:     "git push --force origin main",
			expectedRisk: "destructive",
		},
		{
			name:         "git reset --hard is destructive",
			response:     "git reset --hard HEAD~1",
			expectedRisk: "destructive",
		},
		{
			name:         "chmod 777 is destructive",
			response:     "chmod 777 /etc/passwd",
			expectedRisk: "destructive",
		},
		{
			name:         "simple ls is safe",
			response:     "ls -la",
			expectedRisk: "safe",
		},
		{
			name:         "echo is safe",
			response:     "echo hello world",
			expectedRisk: "safe",
		},
		{
			name:         "cat is safe",
			response:     "cat README.md",
			expectedRisk: "safe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := p.parseCommandResponse(tt.response)
			if len(result) == 0 {
				t.Fatal("parseCommandResponse() returned no suggestions")
			}
			if result[0].Risk != tt.expectedRisk {
				t.Errorf("parseCommandResponse().Risk = %q, want %q for command %q",
					result[0].Risk, tt.expectedRisk, tt.response)
			}
		})
	}
}

func TestAnthropicProvider_parseCommandResponse_EdgeCases(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		name     string
		response string
		expected []string
	}{
		{
			name:     "empty string",
			response: "",
			expected: nil,
		},
		{
			name:     "only newlines",
			response: "\n\n\n",
			expected: nil,
		},
		{
			name:     "only whitespace and newlines",
			response: "  \n\t\n  ",
			expected: nil,
		},
		{
			name:     "only comments",
			response: "# comment 1\n# comment 2",
			expected: nil,
		},
		{
			name:     "only prose",
			response: "Here is information\nThe thing is\nThis explains",
			expected: nil,
		},
		{
			name:     "command with leading/trailing whitespace",
			response: "   ls -la   ",
			expected: []string{"ls -la"},
		},
		{
			name:     "command with extra internal spaces preserved",
			response: "echo    hello    world",
			expected: []string{"echo    hello    world"},
		},
		{
			name:     "exactly 3 commands",
			response: "cmd1\ncmd2\ncmd3",
			expected: []string{"cmd1", "cmd2", "cmd3"},
		},
		{
			name:     "more than 3 commands limited to 3",
			response: "cmd1\ncmd2\ncmd3\ncmd4\ncmd5\ncmd6",
			expected: []string{"cmd1", "cmd2", "cmd3"},
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

// Additional comprehensive tests for parseDiagnoseResponse

func TestAnthropicProvider_parseDiagnoseResponse_EdgeCases(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		name                string
		response            string
		expectedExplanation string
		expectedFixes       []string
	}{
		{
			name:                "empty response",
			response:            "",
			expectedExplanation: "",
			expectedFixes:       nil,
		},
		{
			name:                "only whitespace",
			response:            "   \n\n   ",
			expectedExplanation: "",
			expectedFixes:       nil,
		},
		{
			name:                "explanation only no fixes",
			response:            "The file was not found because it does not exist in the current directory.",
			expectedExplanation: "The file was not found because it does not exist in the current directory.",
			expectedFixes:       nil,
		},
		{
			name:                "multi-line explanation no fixes",
			response:            "The command failed.\nThis is because the package is not installed.",
			expectedExplanation: "The command failed. This is because the package is not installed.",
			expectedFixes:       nil,
		},
		{
			name: "explanation with dollar-prefixed fixes",
			response: `The npm package is missing.

$ npm install express
$ npm start`,
			expectedExplanation: "The npm package is missing.",
			expectedFixes:       []string{"npm install express", "npm start"},
		},
		{
			name: "mixed explanation and numbered fixes after $ fix",
			response: `You need to install the dependency first.

$ npm install
1. npm run build
2. npm test`,
			expectedExplanation: "You need to install the dependency first.",
			expectedFixes:       []string{"npm install", "npm run build", "npm test"},
		},
		{
			name: "fixes with destructive command",
			response: `The directory is corrupted.

$ rm -rf node_modules
$ npm install`,
			expectedExplanation: "The directory is corrupted.",
			expectedFixes:       []string{"rm -rf node_modules", "npm install"},
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

func TestAnthropicProvider_parseDiagnoseResponse_FixRiskDetection(t *testing.T) {
	p := NewAnthropicProvider()

	tests := []struct {
		name         string
		response     string
		expectedRisk string
	}{
		{
			name:         "safe fix command",
			response:     "Error occurred.\n\n$ npm install",
			expectedRisk: "safe",
		},
		{
			name:         "destructive rm -rf fix",
			response:     "Need to clean up.\n\n$ rm -rf dist/",
			expectedRisk: "destructive",
		},
		{
			name:         "destructive force push fix",
			response:     "Need to sync.\n\n$ git push --force",
			expectedRisk: "destructive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, fixes := p.parseDiagnoseResponse(tt.response)
			if len(fixes) == 0 {
				t.Fatal("parseDiagnoseResponse() returned no fixes")
			}
			if fixes[0].Risk != tt.expectedRisk {
				t.Errorf("parseDiagnoseResponse() fix risk = %q, want %q",
					fixes[0].Risk, tt.expectedRisk)
			}
		})
	}
}

func TestAnthropicProvider_parseDiagnoseResponse_FixScores(t *testing.T) {
	p := NewAnthropicProvider()

	response := `Multiple fixes needed.

$ fix1
$ fix2
$ fix3`

	_, fixes := p.parseDiagnoseResponse(response)

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

	// All should have "ai" as source
	for i, fix := range fixes {
		if fix.Source != "ai" {
			t.Errorf("fixes[%d].Source = %q, want %q", i, fix.Source, "ai")
		}
	}
}

// Additional comprehensive tests for cleanCommandPrefix

func TestCleanCommandPrefix_Comprehensive(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Numbered prefixes with dot
		{name: "single digit dot", input: "1. ls", expected: "ls"},
		{name: "digit 2 dot", input: "2. pwd", expected: "pwd"},
		{name: "digit 9 dot", input: "9. echo", expected: "echo"},
		{name: "double digit dot", input: "10. cat file", expected: "cat file"},
		{name: "double digit dot 99", input: "99. last", expected: "last"},

		// Numbered prefixes with parenthesis
		{name: "single digit paren", input: "1) ls", expected: "ls"},
		{name: "digit 5 paren", input: "5) pwd", expected: "pwd"},
		{name: "double digit paren", input: "12) echo", expected: "echo"},

		// Bullet prefixes
		{name: "dash bullet", input: "- ls -la", expected: "ls -la"},
		{name: "asterisk bullet", input: "* git status", expected: "git status"},
		{name: "dollar prefix", input: "$ make build", expected: "make build"},

		// Backticks
		{name: "single backticks", input: "`ls`", expected: "ls"},
		{name: "triple backticks", input: "```pwd```", expected: "pwd"},
		{name: "leading backticks only", input: "```ls", expected: "ls"},
		{name: "trailing backticks only", input: "ls```", expected: "ls"},

		// Edge cases
		{name: "empty string", input: "", expected: ""},
		{name: "only whitespace", input: "   ", expected: ""},
		{name: "no prefix needed", input: "git commit -m 'msg'", expected: "git commit -m 'msg'"},
		{name: "preserves internal spacing", input: "1. echo   hello", expected: "echo   hello"},
		{name: "handles tabs", input: "\tls", expected: "ls"},

		// Combined prefixes
		{name: "numbered then dollar", input: "1. $ ls", expected: "ls"},
		// Note: backticks are stripped last, so numbered prefix remains when wrapped in backticks
		{name: "backticks around numbered", input: "`1. ls`", expected: "1. ls"},

		// Cases that should NOT be cleaned
		{name: "dash in middle preserved", input: "ls -la", expected: "ls -la"},
		{name: "dollar in middle preserved", input: "echo $HOME", expected: "echo $HOME"},

		// Whitespace handling
		{name: "leading whitespace trimmed", input: "   ls", expected: "ls"},
		{name: "trailing whitespace trimmed", input: "ls   ", expected: "ls"},
		{name: "both whitespace trimmed", input: "   ls   ", expected: "ls"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanCommandPrefix(tt.input)
			if result != tt.expected {
				t.Errorf("cleanCommandPrefix(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanCommandPrefix_NumberedPrefixBoundary(t *testing.T) {
	// Test the boundary condition for numbered prefixes
	tests := []struct {
		input    string
		expected string
	}{
		// Numbers 1-9 with single character
		{"1. x", "x"},
		{"9. y", "y"},
		// Numbers that shouldn't be treated as prefixes
		{"0. zero", "0. zero"},   // 0 is not in 1-9 range
		{"a. alpha", "a. alpha"}, // letter, not digit
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
