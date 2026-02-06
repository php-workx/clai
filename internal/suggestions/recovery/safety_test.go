package recovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafetyGate_SafeCommands(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(DefaultSafetyConfig())

	safeCommands := []string{
		"git status",
		"make clean",
		"go mod tidy",
		"npm install",
		"brew install wget",
		"apt install curl",
		"chmod +x script.sh",
		"git merge --abort",
		"git stash",
		"docker system prune -f",
		"rm -rf node_modules",
		"pip install requests",
	}

	for _, cmd := range safeCommands {
		t.Run(cmd, func(t *testing.T) {
			assert.True(t, sg.IsSafe(cmd), "expected %q to be safe", cmd)
		})
	}
}

func TestSafetyGate_UnsafeCommands(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(DefaultSafetyConfig())

	unsafeCommands := []string{
		"rm -rf /",
		"rm -rf /*",
		"rm -rf .",
		"rm -rf *",
		"rm -rf ~",
		"rm -rf ~/",
		"dd if=/dev/zero",
		"dd if=/dev/urandom",
		":(){:|:&};:",
		"chmod -r 777 /",
		"chmod -r 777 /*",
		"mkfs",
		"mkfs.ext4",
		"> /dev/sda",
		"cat /dev/zero > /dev/sda",
		"dd if=/dev/zero of=/dev/sda",
	}

	for _, cmd := range unsafeCommands {
		t.Run(cmd, func(t *testing.T) {
			assert.False(t, sg.IsSafe(cmd), "expected %q to be unsafe", cmd)
		})
	}
}

func TestSafetyGate_EmptyCommand(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(DefaultSafetyConfig())

	assert.False(t, sg.IsSafe(""))
	assert.False(t, sg.IsSafe("   "))
}

func TestSafetyGate_SudoWithAllowSudo(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(SafetyConfig{AllowSudo: true})

	// Safe commands with sudo prefix
	assert.True(t, sg.IsSafe("sudo apt install curl"))
	assert.True(t, sg.IsSafe("sudo chmod +x script.sh"))
	assert.True(t, sg.IsSafe("sudo make install"))

	// Unsafe commands with sudo prefix
	assert.False(t, sg.IsSafe("sudo rm -rf /"))
	assert.False(t, sg.IsSafe("sudo mkfs.ext4"))
}

func TestSafetyGate_SudoDisabled(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(SafetyConfig{AllowSudo: false})

	// All sudo commands blocked when sudo is disabled
	assert.False(t, sg.IsSafe("sudo apt install curl"))
	assert.False(t, sg.IsSafe("sudo make install"))

	// Non-sudo commands still work
	assert.True(t, sg.IsSafe("apt install curl"))
	assert.True(t, sg.IsSafe("make install"))
}

func TestSafetyGate_CaseInsensitive(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(DefaultSafetyConfig())

	assert.False(t, sg.IsSafe("RM -RF /"))
	assert.False(t, sg.IsSafe("Rm -Rf /"))
}

func TestSafetyGate_AdditionalBlocked(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(SafetyConfig{
		AllowSudo:         true,
		AdditionalBlocked: []string{"dangerous-custom-cmd"},
	})

	assert.False(t, sg.IsSafe("dangerous-custom-cmd --force"))
	assert.True(t, sg.IsSafe("safe-custom-cmd"))
}

func TestSafetyGate_FilterSafe(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(DefaultSafetyConfig())

	candidates := []RecoveryCandidate{
		{RecoveryCmdNorm: "git merge --abort", SuccessRate: 0.85},
		{RecoveryCmdNorm: "rm -rf /", SuccessRate: 0.99},
		{RecoveryCmdNorm: "make clean", SuccessRate: 0.50},
		{RecoveryCmdNorm: "mkfs.ext4", SuccessRate: 0.30},
	}

	safe := sg.FilterSafe(candidates)
	assert.Len(t, safe, 2)
	assert.Equal(t, "git merge --abort", safe[0].RecoveryCmdNorm)
	assert.Equal(t, "make clean", safe[1].RecoveryCmdNorm)
}

func TestSafetyGate_FilterSafe_EmptyInput(t *testing.T) {
	t.Parallel()
	sg := NewSafetyGate(DefaultSafetyConfig())

	safe := sg.FilterSafe(nil)
	assert.Empty(t, safe)

	safe = sg.FilterSafe([]RecoveryCandidate{})
	assert.Empty(t, safe)
}
