package normalize

import (
	"testing"
)

func TestNormalizer_Normalize(t *testing.T) {
	n := NewNormalizer()

	tests := []struct {
		name     string
		cmdRaw   string
		wantNorm string
		wantLen  int // expected number of slots
	}{
		// Basic commands
		{
			name:     "simple command no args",
			cmdRaw:   "ls",
			wantNorm: "ls",
			wantLen:  0,
		},
		{
			name:     "command with flags only",
			cmdRaw:   "ls -la",
			wantNorm: "ls -la",
			wantLen:  0,
		},

		// Path detection
		{
			name:     "cd with path",
			cmdRaw:   "cd /home/user/projects",
			wantNorm: "cd <path>",
			wantLen:  1,
		},
		{
			name:     "cd with relative path",
			cmdRaw:   "cd ./src",
			wantNorm: "cd <path>",
			wantLen:  1,
		},
		{
			name:     "cd with tilde path",
			cmdRaw:   "cd ~/Documents",
			wantNorm: "cd <path>",
			wantLen:  1,
		},
		{
			name:     "cat multiple paths",
			cmdRaw:   "cat /etc/passwd /etc/hosts",
			wantNorm: "cat <path> <path>",
			wantLen:  2,
		},

		// Git commands
		{
			name:     "git status (no args)",
			cmdRaw:   "git status",
			wantNorm: "git status",
			wantLen:  0,
		},
		{
			name:     "git commit with message",
			cmdRaw:   "git commit -m \"fix bug\"",
			wantNorm: "git commit -m <msg>",
			wantLen:  1,
		},
		{
			name:     "git checkout branch",
			cmdRaw:   "git checkout -b feature/new-feature",
			wantNorm: "git checkout -b <arg>",
			wantLen:  1,
		},
		{
			name:     "git add paths",
			cmdRaw:   "git add src/main.go internal/util.go",
			wantNorm: "git add <path> <path>",
			wantLen:  2,
		},
		{
			name:     "git push remote branch",
			cmdRaw:   "git push origin main",
			wantNorm: "git push <arg> <arg>",
			wantLen:  2,
		},
		{
			name:     "git show SHA",
			cmdRaw:   "git show abc1234def5678",
			wantNorm: "git show <sha>",
			wantLen:  1,
		},
		{
			name:     "git clone URL",
			cmdRaw:   "git clone https://github.com/user/repo.git",
			wantNorm: "git clone <url>",
			wantLen:  1,
		},
		{
			name:     "git clone with destination",
			cmdRaw:   "git clone https://github.com/user/repo.git ./myrepo",
			wantNorm: "git clone <url> <path>",
			wantLen:  2,
		},

		// npm commands
		{
			name:     "npm install",
			cmdRaw:   "npm install",
			wantNorm: "npm install",
			wantLen:  0,
		},
		{
			name:     "npm install package",
			cmdRaw:   "npm install lodash",
			wantNorm: "npm install <arg>",
			wantLen:  1,
		},
		{
			name:     "npm run script",
			cmdRaw:   "npm run test",
			wantNorm: "npm run <arg>",
			wantLen:  1,
		},

		// go commands
		{
			name:     "go test path",
			cmdRaw:   "go test ./...",
			wantNorm: "go test <path>",
			wantLen:  1,
		},
		{
			name:     "go build",
			cmdRaw:   "go build ./cmd/app",
			wantNorm: "go build <path>",
			wantLen:  1,
		},

		// pytest
		{
			name:     "pytest with path",
			cmdRaw:   "pytest tests/test_main.py",
			wantNorm: "pytest <path>",
			wantLen:  1,
		},

		// kubectl
		{
			name:     "kubectl get pods with namespace",
			cmdRaw:   "kubectl get pods -n kube-system",
			wantNorm: "kubectl get <arg> -n <arg>",
			wantLen:  2,
		},

		// docker
		{
			name:     "docker build",
			cmdRaw:   "docker build -t myapp:latest .",
			wantNorm: "docker build -t <arg> <path>",
			wantLen:  2,
		},
		{
			name:     "docker run",
			cmdRaw:   "docker run nginx:latest",
			wantNorm: "docker run <arg>",
			wantLen:  1,
		},

		// URL detection
		{
			name:     "curl URL",
			cmdRaw:   "curl https://api.example.com/data",
			wantNorm: "curl <url>",
			wantLen:  1,
		},

		// Number detection
		{
			name:     "chmod with mode",
			cmdRaw:   "chmod 755 script.sh",
			wantNorm: "chmod <num> <path>",
			wantLen:  2,
		},

		// make
		{
			name:     "make targets",
			cmdRaw:   "make build test",
			wantNorm: "make <arg> <arg>",
			wantLen:  2,
		},

		// Edge cases
		{
			name:     "empty command",
			cmdRaw:   "",
			wantNorm: "",
			wantLen:  0,
		},
		{
			name:     "whitespace only",
			cmdRaw:   "   ",
			wantNorm: "   ",
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNorm, gotSlots := n.Normalize(tt.cmdRaw)
			if gotNorm != tt.wantNorm {
				t.Errorf("Normalize() norm = %q, want %q", gotNorm, tt.wantNorm)
			}
			if len(gotSlots) != tt.wantLen {
				t.Errorf("Normalize() slots len = %d, want %d", len(gotSlots), tt.wantLen)
			}
		})
	}
}

func TestNormalizer_SlotValues(t *testing.T) {
	n := NewNormalizer()

	// Test that slot values are correctly extracted
	_, slots := n.Normalize("git commit -m \"my commit message\"")
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}
	if slots[0].Type != SlotMsg {
		t.Errorf("expected slot type %s, got %s", SlotMsg, slots[0].Type)
	}
	if slots[0].Value != "my commit message" {
		t.Errorf("expected slot value %q, got %q", "my commit message", slots[0].Value)
	}
}

func TestDetectSlotType(t *testing.T) {
	n := NewNormalizer()

	tests := []struct {
		token string
		want  string
	}{
		// Paths
		{"/etc/passwd", SlotPath},
		{"./src", SlotPath},
		{"../parent", SlotPath},
		{"~/Documents", SlotPath},
		{"src/main.go", SlotPath},

		// SHAs
		{"abc1234", SlotSHA},
		{"abc1234567890abcdef1234567890abcdef1234", SlotSHA}, // 40 chars

		// URLs
		{"https://example.com", SlotURL},
		{"http://localhost:8080", SlotURL},
		{"git@github.com:user/repo.git", SlotURL},

		// Numbers
		{"123", SlotNum},
		{"0", SlotNum},
		{"999999", SlotNum},

		// Generic args
		{"lodash", SlotArg},
		{"main", SlotArg},
		{"test", SlotArg},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			got := n.detectSlotType(tt.token)
			if got != tt.want {
				t.Errorf("detectSlotType(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

func TestNormalizeSimple(t *testing.T) {
	// Test the convenience function
	norm := NormalizeSimple("git commit -m \"test message\"")
	if norm != "git commit -m <msg>" {
		t.Errorf("NormalizeSimple() = %q, want %q", norm, "git commit -m <msg>")
	}
}

func BenchmarkNormalize(b *testing.B) {
	n := NewNormalizer()
	cmd := "git commit -m \"fix: resolve issue with path handling\" --no-verify"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n.Normalize(cmd)
	}
}
