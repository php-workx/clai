package suggest

import "testing"

func TestGetToolPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple command",
			input:    "git status",
			expected: "git",
		},
		{
			name:     "command with flags",
			input:    "docker run -d nginx",
			expected: "docker",
		},
		{
			name:     "command with subcommand",
			input:    "kubectl get pods",
			expected: "kubectl",
		},
		{
			name:     "single word command",
			input:    "ls",
			expected: "ls",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "leading whitespace",
			input:    "   git push",
			expected: "git",
		},
		{
			name:     "env var prefix",
			input:    "FOO=bar git push",
			expected: "git",
		},
		{
			name:     "multiple env var prefixes",
			input:    "FOO=bar BAZ=qux npm install",
			expected: "npm",
		},
		{
			name:     "env var only",
			input:    "FOO=bar",
			expected: "",
		},
		{
			name:     "uppercase command",
			input:    "GIT status",
			expected: "git",
		},
		{
			name:     "mixed case command",
			input:    "Docker run",
			expected: "docker",
		},
		{
			name:     "path command",
			input:    "/usr/bin/git status",
			expected: "/usr/bin/git",
		},
		{
			name:     "command with equals in arg",
			input:    "git config user.name=foo",
			expected: "git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GetToolPrefix(tt.input)
			if got != tt.expected {
				t.Errorf("GetToolPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeForDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple command",
			input:    "git status",
			expected: "git status",
		},
		{
			name:     "extra spaces",
			input:    "git    status",
			expected: "git status",
		},
		{
			name:     "leading and trailing whitespace",
			input:    "   git status   ",
			expected: "git status",
		},
		{
			name:     "tabs and spaces",
			input:    "git\t  status\t",
			expected: "git status",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   \t   ",
			expected: "",
		},
		{
			name:     "complex command",
			input:    "docker   run  -d   -p 8080:80   nginx",
			expected: "docker run -d -p 8080:80 nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeForDisplay(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeForDisplay(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestDeduplicateKey(t *testing.T) {
	t.Parallel()

	// DeduplicateKey should return the same key for the same normalized command
	tests := []struct {
		name   string
		input1 string
		input2 string
		same   bool
	}{
		{
			name:   "identical commands",
			input1: "git status",
			input2: "git status",
			same:   true,
		},
		{
			name:   "different commands",
			input1: "git status",
			input2: "git push",
			same:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key1 := DeduplicateKey(tt.input1)
			key2 := DeduplicateKey(tt.input2)
			if (key1 == key2) != tt.same {
				if tt.same {
					t.Errorf("DeduplicateKey(%q) != DeduplicateKey(%q), expected same", tt.input1, tt.input2)
				} else {
					t.Errorf("DeduplicateKey(%q) == DeduplicateKey(%q), expected different", tt.input1, tt.input2)
				}
			}
		})
	}
}
