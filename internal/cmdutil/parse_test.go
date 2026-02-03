package cmdutil

import "testing"

func TestIsSudo(t *testing.T) {
	tests := []struct {
		cmd      string
		expected bool
	}{
		{"sudo apt update", true},
		{"sudo", true},
		{"  sudo apt update", true},
		{"apt update", false},
		{"sudoku", false},
		{"echo sudo", false},
		{"", false},
	}

	for _, tt := range tests {
		result := IsSudo(tt.cmd)
		if result != tt.expected {
			t.Errorf("IsSudo(%q) = %v, want %v", tt.cmd, result, tt.expected)
		}
	}
}

func TestCountPipes(t *testing.T) {
	tests := []struct {
		cmd      string
		expected int
	}{
		{"ls", 0},
		{"ls | grep foo", 1},
		{"cat file | grep pattern | wc -l", 2},
		{"echo '|' | cat", 1},    // pipe in quotes doesn't count
		{`echo "|" | cat`, 1},    // double quotes
		{`echo "a|b|c" | wc`, 1}, // multiple pipes in quotes
		{"ls |grep|wc", 2},       // no spaces
		{"", 0},
		{"echo 'test | more' | less", 1}, // one real pipe, one in quotes
	}

	for _, tt := range tests {
		result := CountPipes(tt.cmd)
		if result != tt.expected {
			t.Errorf("CountPipes(%q) = %v, want %v", tt.cmd, result, tt.expected)
		}
	}
}

func TestCountWords(t *testing.T) {
	tests := []struct {
		cmd      string
		expected int
	}{
		{"ls", 1},
		{"ls -la", 2},
		{"git commit -m 'message'", 4},
		{"  ls   -la  ", 2},
		{"", 0},
		{"   ", 0},
		{"docker run -d -p 8080:80 nginx", 6},
	}

	for _, tt := range tests {
		result := CountWords(tt.cmd)
		if result != tt.expected {
			t.Errorf("CountWords(%q) = %v, want %v", tt.cmd, result, tt.expected)
		}
	}
}
