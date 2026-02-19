package extract

import (
	"testing"
)

func TestSuggestion_Backticks(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantPat string
	}{
		{
			name:    "simple backtick command",
			input:   "Run `npm install express` to install",
			want:    "npm install express",
			wantPat: "backticks",
		},
		{
			name:    "multiple backtick commands uses last",
			input:   "First `npm init` then `npm install express`",
			want:    "npm install express",
			wantPat: "backticks",
		},
		{
			name:    "backtick with flags",
			input:   "Use `pip install --upgrade requests` to upgrade",
			want:    "pip install --upgrade requests",
			wantPat: "backticks",
		},
		{
			name:    "backtick ignores non-command content",
			input:   "The `node_modules` folder contains `npm install express`",
			want:    "npm install express",
			wantPat: "backticks",
		},
		{
			name:    "backtick with path",
			input:   "Run `./scripts/setup.sh` to configure",
			want:    "",
			wantPat: "",
		},
		{
			name:    "backtick must start with letter",
			input:   "Value is `123` but try `npm start`",
			want:    "npm start",
			wantPat: "backticks",
		},
		{
			name:    "empty backticks",
			input:   "Use `` for empty",
			want:    "",
			wantPat: "",
		},
		{
			name:    "backtick with complex command",
			input:   "Run `docker run -it --rm -v $(pwd):/app node:18`",
			want:    "docker run -it --rm -v $(pwd):/app node:18",
			wantPat: "backticks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, pat := SuggestionWithPattern(tt.input)
			if got != tt.want {
				t.Errorf("Suggestion() = %q, want %q", got, tt.want)
			}
			if pat != tt.wantPat {
				t.Errorf("Pattern = %q, want %q", pat, tt.wantPat)
			}
		})
	}
}

var installCommandTests = []struct {
	name    string
	input   string
	want    string
	wantPat string
}{
	{"pip install", "ModuleNotFoundError: No module named 'requests'\npip install requests", "pip install requests", "install"},
	{"pip3 install", "pip3 install numpy pandas", "pip3 install numpy pandas", "install"},
	{"python -m pip install", "python -m pip install flask", "python -m pip install flask", "install"},
	{"python3 -m pip install", "python3 -m pip install django", "python3 -m pip install django", "install"},
	{"npm install", "npm install express", "npm install express", "install"},
	{"npm install scoped package", "npm install @types/node", "npm install @types/node", "install"},
	{"npm install with version", "npm install react@18.2.0", "npm install react@18.2.0", "install"},
	{"yarn install", "yarn install lodash", "yarn install lodash", "install"},
	{"pnpm install", "pnpm install axios", "pnpm install axios", "install"},
	{"brew install", "brew install wget", "brew install wget", "install"},
	{"cargo install", "cargo install ripgrep", "cargo install ripgrep", "install"},
	{"go install", "go install golang.org/x/tools/gopls@latest", "go install golang.org/x/tools/gopls@latest", "install"},
	{"apt-get install", "apt-get install build-essential", "apt-get install build-essential", "install"},
	{"apt install", "apt install vim", "apt install vim", "install"},
	{"dnf install", "dnf install nodejs", "dnf install nodejs", "install"},
	{"pacman -S install", "pacman -S install base-devel", "pacman -S install base-devel", "install"},
	{"install with multiple packages", "pip install requests beautifulsoup4 lxml", "pip install requests beautifulsoup4 lxml", "install"},
	{"case insensitive NPM", "NPM install express", "NPM install express", "install"},
}

func TestSuggestion_InstallCommands(t *testing.T) {
	for _, tt := range installCommandTests {
		t.Run(tt.name, func(t *testing.T) {
			got, pat := SuggestionWithPattern(tt.input)
			if got != tt.want {
				t.Errorf("Suggestion() = %q, want %q", got, tt.want)
			}
			if pat != tt.wantPat {
				t.Errorf("Pattern = %q, want %q", pat, tt.wantPat)
			}
		})
	}
}

func TestSuggestion_PrefixedCommands(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantPat string
	}{
		{
			name:    "Run: prefix",
			input:   "Run: npm start",
			want:    "npm start",
			wantPat: "prefixed",
		},
		{
			name:    "run: lowercase",
			input:   "run: python app.py",
			want:    "python app.py",
			wantPat: "prefixed",
		},
		{
			name:    "Execute: prefix",
			input:   "Execute: ./setup.sh",
			want:    "./setup.sh",
			wantPat: "prefixed",
		},
		{
			name:    "Try: prefix",
			input:   "Try: npm run build",
			want:    "npm run build",
			wantPat: "prefixed",
		},
		{
			name:    "Use: prefix",
			input:   "Use: docker-compose up -d",
			want:    "docker-compose up -d",
			wantPat: "prefixed",
		},
		{
			name:    "prefix with extra whitespace",
			input:   "Run:   npm test",
			want:    "npm test",
			wantPat: "prefixed",
		},
		{
			name:    "prefix mixed case",
			input:   "RUN: make build",
			want:    "make build",
			wantPat: "prefixed",
		},
		{
			name:    "prefix in sentence",
			input:   "To fix this error, Run: npm cache clean --force",
			want:    "npm cache clean --force",
			wantPat: "prefixed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, pat := SuggestionWithPattern(tt.input)
			if got != tt.want {
				t.Errorf("Suggestion() = %q, want %q", got, tt.want)
			}
			if pat != tt.wantPat {
				t.Errorf("Pattern = %q, want %q", pat, tt.wantPat)
			}
		})
	}
}

func TestSuggestion_DollarPrefix(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantPat string
	}{
		{
			name:    "simple dollar prefix",
			input:   "$ npm run dev",
			want:    "npm run dev",
			wantPat: "dollar",
		},
		{
			name:    "dollar with leading space",
			input:   "  $ python manage.py runserver",
			want:    "python manage.py runserver",
			wantPat: "dollar",
		},
		{
			name:    "multiple dollar lines uses last",
			input:   "$ npm install\n$ npm start",
			want:    "npm start",
			wantPat: "dollar",
		},
		{
			name:    "dollar with complex command",
			input:   "$ git commit -m \"fix: resolve issue\"",
			want:    "git commit -m \"fix: resolve issue\"",
			wantPat: "dollar",
		},
		{
			name:    "dollar in documentation block",
			input:   "Usage:\n  $ myapp --help\n  $ myapp run",
			want:    "myapp run",
			wantPat: "dollar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, pat := SuggestionWithPattern(tt.input)
			if got != tt.want {
				t.Errorf("Suggestion() = %q, want %q", got, tt.want)
			}
			if pat != tt.wantPat {
				t.Errorf("Pattern = %q, want %q", pat, tt.wantPat)
			}
		})
	}
}

func TestSuggestion_ToPrefix(t *testing.T) {
	// Note: Some "To X" patterns may match higher-priority patterns first
	// (e.g., "npm install" matches the install pattern). This is expected.
	// We just verify the correct command is extracted.
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "To install extracts install command",
			input: "To install, run: npm install foo",
			want:  "npm install foo",
		},
		{
			name:  "To fix",
			input: "To fix this issue: rm -rf node_modules",
			want:  "rm -rf node_modules",
		},
		{
			name:  "To run",
			input: "To run the tests: pytest",
			want:  "pytest",
		},
		{
			name:  "To start with execute prefix",
			input: "To start the server, execute: node server.js",
			want:  "node server.js",
		},
		{
			name:  "To build",
			input: "To build the project: make build",
			want:  "make build",
		},
		{
			name:  "To install with pip",
			input: "To install the dependencies, please run: pip install -r requirements.txt",
			want:  "pip install -r requirements.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Suggestion(tt.input)
			if got != tt.want {
				t.Errorf("Suggestion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSuggestion_NoMatch(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "plain text",
			input: "This is just regular text without any commands",
		},
		{
			name:  "error message without suggestion",
			input: "Error: Something went wrong\nPlease try again later",
		},
		{
			name:  "code without install",
			input: "const express = require('express')",
		},
		{
			name:  "partial command",
			input: "npm is a package manager",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Suggestion(tt.input)
			if got != "" {
				t.Errorf("Suggestion() = %q, want empty string", got)
			}
		})
	}
}

func TestSuggestion_PatternPriority(t *testing.T) {
	// Backticks should have higher priority than other patterns
	tests := []struct {
		name    string
		input   string
		want    string
		wantPat string
	}{
		{
			name:    "backtick beats install",
			input:   "pip install requests but use `pip install requests==2.28.0`",
			want:    "pip install requests==2.28.0",
			wantPat: "backticks",
		},
		{
			name:    "backtick beats prefixed",
			input:   "Run: npm start or `npm run dev`",
			want:    "npm run dev",
			wantPat: "backticks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, pat := SuggestionWithPattern(tt.input)
			if got != tt.want {
				t.Errorf("Suggestion() = %q, want %q", got, tt.want)
			}
			if pat != tt.wantPat {
				t.Errorf("Pattern = %q, want %q", pat, tt.wantPat)
			}
		})
	}
}

func TestClean(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no trailing punctuation", "npm install", "npm install"},
		{"trailing period", "npm install.", "npm install"},
		{"trailing comma", "npm install,", "npm install"},
		{"trailing semicolon", "npm install;", "npm install"},
		{"trailing colon", "npm install:", "npm install"},
		{"multiple trailing punctuation", "npm install.,;:", "npm install"},
		{"leading whitespace", "  npm install", "npm install"},
		{"trailing whitespace", "npm install  ", "npm install"},
		{"both whitespace", "  npm install  ", "npm install"},
		{"whitespace and punctuation", "  npm install.  ", "npm install"},
		{"empty string", "", ""},
		{"only punctuation", ".,;:", ""},
		{"only whitespace", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Clean(tt.input)
			if got != tt.want {
				t.Errorf("Clean(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSuggestion_RealWorldExamples(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "npm error with suggestion",
			input: `npm ERR! code ENOENT
npm ERR! syscall open
npm ERR! path /Users/dev/project/package.json
npm ERR! errno -2
npm ERR! enoent ENOENT: no such file or directory
npm ERR! enoent This is related to npm not being able to find a file.
npm ERR! enoent

npm ERR! A complete log of this run can be found in:
npm ERR!     /Users/dev/.npm/_logs/2024-01-15T10_30_00_000Z-debug.log

To fix this, run: npm init -y`,
			want: "npm init -y",
		},
		{
			name: "Python import error",
			input: `Traceback (most recent call last):
  File "app.py", line 1, in <module>
    import requests
ModuleNotFoundError: No module named 'requests'

pip install requests`,
			want: "pip install requests",
		},
		{
			name: "Docker command in readme",
			input: `## Quick Start

To run the application:

$ docker run -p 3000:3000 myapp:latest`,
			want: "docker run -p 3000:3000 myapp:latest",
		},
		{
			name: "Multiple dollar suggestions uses last standalone",
			input: `Usage:
$ git clone repo
$ npm start`,
			want: "npm start",
		},
		{
			name: "Go module error",
			input: `go: cannot find main module, but found .git/config in /Users/dev/project
	to create a module there, run:
	go mod init`,
			want: "go mod init",
		},
		{
			name: "Rust cargo suggestion",
			input: `error[E0432]: unresolved import 'serde'
 --> src/main.rs:1:5
  |
1 | use serde::{Serialize, Deserialize};
  |     ^^^^^ use of undeclared crate or module 'serde'

help: consider adding 'serde' to your dependencies:
       cargo install serde`,
			want: "cargo install serde",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Suggestion(tt.input)
			if got != tt.want {
				t.Errorf("Suggestion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSuggestion_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "unicode in command",
			input: "Run: echo '你好世界'",
			want:  "echo '你好世界'",
		},
		{
			name:  "very long command",
			input: "Run: npm install express body-parser cors helmet morgan compression cookie-parser dotenv",
			want:  "npm install express body-parser cors helmet morgan compression cookie-parser dotenv",
		},
		{
			name:  "command with pipes",
			input: "Try: cat file.txt | grep pattern | sort | uniq",
			want:  "cat file.txt | grep pattern | sort | uniq",
		},
		{
			name:  "command with redirects",
			input: "Run: npm test > output.log 2>&1",
			want:  "npm test > output.log 2>&1",
		},
		{
			name:  "command with env vars",
			input: "Execute: NODE_ENV=production npm start",
			want:  "NODE_ENV=production npm start",
		},
		{
			// Nested backticks is an edge case - we extract the first valid command
			name:  "backticks with quoted content",
			input: "Use `npm install 'express'` for this",
			want:  "npm install 'express'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Suggestion(tt.input)
			if got != tt.want {
				t.Errorf("Suggestion() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Benchmark tests
func BenchmarkSuggestion_Backticks(b *testing.B) {
	input := "Run `npm install express` to install the package"
	for i := 0; i < b.N; i++ {
		Suggestion(input)
	}
}

func BenchmarkSuggestion_Install(b *testing.B) {
	input := "pip install requests beautifulsoup4 lxml pandas numpy"
	for i := 0; i < b.N; i++ {
		Suggestion(input)
	}
}

func BenchmarkSuggestion_LongText(b *testing.B) {
	input := `This is a very long error message that contains a lot of text
and spans multiple lines. The error occurred because the module was not found.
To fix this issue, you need to install the required dependencies.

pip install requests

After installing, try running the script again.`
	for i := 0; i < b.N; i++ {
		Suggestion(input)
	}
}

func BenchmarkSuggestion_NoMatch(b *testing.B) {
	input := "This is just regular text without any commands that we can extract or suggest to the user."
	for i := 0; i < b.N; i++ {
		Suggestion(input)
	}
}
