package sanitize

import (
	"testing"
)

func TestGetSecretPatterns(t *testing.T) {
	patterns := GetSecretPatterns()
	if len(patterns) == 0 {
		t.Error("GetSecretPatterns() returned empty list")
	}

	// Verify all patterns have required fields
	for _, p := range patterns {
		if p.Name == "" {
			t.Error("Pattern has empty name")
		}
		if p.Regex == nil {
			t.Errorf("Pattern %q has nil regex", p.Name)
		}
		if p.Replacement == "" {
			t.Errorf("Pattern %q has empty replacement", p.Name)
		}
	}
}

func TestPatterns_AWSAccessKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{
			name:    "valid AWS access key",
			input:   "AKIAIOSFODNN7EXAMPLE",
			matches: true,
		},
		{
			name:    "AWS access key in context",
			input:   "export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
			matches: true,
		},
		{
			name:    "invalid prefix",
			input:   "BKIAIOSFODNN7EXAMPLE",
			matches: false,
		},
		{
			name:    "too short",
			input:   "AKIA123456789012345",
			matches: false,
		},
		{
			name:    "lowercase not matched",
			input:   "akiaiosfodnn7example",
			matches: false,
		},
	}

	var pattern Pattern
	for _, p := range GetSecretPatterns() {
		if p.Name == "AWS Access Key" {
			pattern = p
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pattern.Regex.MatchString(tt.input) != tt.matches {
				t.Errorf("AWS Access Key pattern.MatchString(%q) = %v, want %v",
					tt.input, !tt.matches, tt.matches)
			}
		})
	}
}

func TestPatterns_AWSSecretKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{
			name:    "aws_secret_access_key with equals",
			input:   "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			matches: true,
		},
		{
			name:    "AWS_SECRET_ACCESS_KEY uppercase",
			input:   "AWS_SECRET_ACCESS_KEY=somevalue",
			matches: true,
		},
		{
			name:    "secret_access_key with colon",
			input:   "secret_access_key: secretvalue123",
			matches: true,
		},
		{
			name:    "no secret",
			input:   "some random text",
			matches: false,
		},
	}

	var pattern Pattern
	for _, p := range GetSecretPatterns() {
		if p.Name == "AWS Secret Key" {
			pattern = p
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pattern.Regex.MatchString(tt.input) != tt.matches {
				t.Errorf("AWS Secret Key pattern.MatchString(%q) = %v, want %v",
					tt.input, !tt.matches, tt.matches)
			}
		})
	}
}

func TestPatterns_JWT(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{
			name:    "valid JWT",
			input:   "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			matches: true,
		},
		{
			name:    "JWT in Authorization header",
			input:   "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			matches: true,
		},
		{
			name:    "incomplete JWT",
			input:   "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0",
			matches: false,
		},
		{
			name:    "random base64",
			input:   "dGhpcyBpcyBub3QgYSBqd3Q=",
			matches: false,
		},
	}

	var pattern Pattern
	for _, p := range GetSecretPatterns() {
		if p.Name == "JWT Token" {
			pattern = p
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pattern.Regex.MatchString(tt.input) != tt.matches {
				t.Errorf("JWT pattern.MatchString(%q) = %v, want %v",
					tt.input, !tt.matches, tt.matches)
			}
		})
	}
}

func TestPatterns_SlackToken(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{
			name:    "xoxb bot token",
			input:   "xoxb-123456789012-1234567890123-abcdefghijklmnopqrst",
			matches: true,
		},
		{
			name:    "xoxp user token",
			input:   "xoxp-123456789012-1234567890123-abcdefghij",
			matches: true,
		},
		{
			name:    "xoxa app token",
			input:   "xoxa-123456789012",
			matches: true,
		},
		{
			name:    "invalid prefix",
			input:   "xoxz-123456789012",
			matches: false,
		},
	}

	var pattern Pattern
	for _, p := range GetSecretPatterns() {
		if p.Name == "Slack Token" {
			pattern = p
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pattern.Regex.MatchString(tt.input) != tt.matches {
				t.Errorf("Slack Token pattern.MatchString(%q) = %v, want %v",
					tt.input, !tt.matches, tt.matches)
			}
		})
	}
}

func TestPatterns_PEMBlock(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{
			name: "RSA private key",
			input: `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA...
-----END RSA PRIVATE KEY-----`,
			matches: true,
		},
		{
			name: "certificate",
			input: `-----BEGIN CERTIFICATE-----
MIIDXTCCAkWgAwIB...
-----END CERTIFICATE-----`,
			matches: true,
		},
		{
			name:    "partial PEM",
			input:   "-----BEGIN RSA PRIVATE KEY-----",
			matches: false,
		},
	}

	var pattern Pattern
	for _, p := range GetSecretPatterns() {
		if p.Name == "PEM Block" {
			pattern = p
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pattern.Regex.MatchString(tt.input) != tt.matches {
				t.Errorf("PEM Block pattern.MatchString(%q) = %v, want %v",
					tt.input, !tt.matches, tt.matches)
			}
		})
	}
}

func TestPatterns_GenericSecret(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{
			name:    "password with equals",
			input:   "password=hunter2",
			matches: true,
		},
		{
			name:    "PASSWORD uppercase",
			input:   "PASSWORD=secret123",
			matches: true,
		},
		{
			name:    "token with colon",
			input:   "token: abc123xyz",
			matches: true,
		},
		{
			name:    "api_key",
			input:   "api_key=my-api-key-value",
			matches: true,
		},
		{
			name:    "secret with spaces",
			input:   "secret = supersecret",
			matches: true,
		},
		{
			name:    "no secret keyword",
			input:   "username=john",
			matches: false,
		},
	}

	var pattern Pattern
	for _, p := range GetSecretPatterns() {
		if p.Name == "Generic Secret" {
			pattern = p
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pattern.Regex.MatchString(tt.input) != tt.matches {
				t.Errorf("Generic Secret pattern.MatchString(%q) = %v, want %v",
					tt.input, !tt.matches, tt.matches)
			}
		})
	}
}

func TestPatterns_GitHubToken(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches bool
	}{
		{
			name:    "valid github personal access token",
			input:   "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			matches: true,
		},
		{
			name:    "github token in env",
			input:   "GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789",
			matches: true,
		},
		{
			name:    "too short",
			input:   "ghp_short",
			matches: false,
		},
		{
			name:    "wrong prefix",
			input:   "ghi_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			matches: false,
		},
	}

	var pattern Pattern
	for _, p := range GetSecretPatterns() {
		if p.Name == "GitHub Token" {
			pattern = p
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pattern.Regex.MatchString(tt.input) != tt.matches {
				t.Errorf("GitHub Token pattern.MatchString(%q) = %v, want %v",
					tt.input, !tt.matches, tt.matches)
			}
		})
	}
}
