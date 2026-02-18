package recovery

import (
	"strings"
)

// SafetyGate checks recovery suggestions against a risk policy.
// It prevents dangerous commands from being suggested as recoveries.
type SafetyGate struct {
	// destructivePatterns are command prefixes/patterns considered dangerous.
	destructivePatterns []string

	// destructiveExact are exact command strings considered dangerous.
	destructiveExact map[string]bool

	// allowSudo controls whether sudo-prefixed suggestions are permitted.
	// When false, sudo commands are blocked unless the base command is safe.
	allowSudo bool
}

// SafetyConfig configures the safety gate.
type SafetyConfig struct {
	// AllowSudo permits sudo-prefixed recovery suggestions.
	// Default: true (sudo is a common and expected recovery for permission errors).
	AllowSudo bool

	// AdditionalBlocked adds extra blocked patterns beyond the defaults.
	AdditionalBlocked []string
}

// DefaultSafetyConfig returns the default safety configuration.
func DefaultSafetyConfig() SafetyConfig {
	return SafetyConfig{
		AllowSudo: true,
	}
}

// NewSafetyGate creates a safety gate with the given configuration.
func NewSafetyGate(cfg SafetyConfig) *SafetyGate {
	sg := &SafetyGate{
		allowSudo: cfg.AllowSudo,
		destructiveExact: map[string]bool{
			"rm -rf /":                 true,
			"rm -rf /*":                true,
			"rm -rf .":                 true,
			"rm -rf *":                 true,
			"rm -rf ~":                 true,
			"rm -rf ~/":                true,
			"dd if=/dev/zero":          true,
			"dd if=/dev/urandom":       true,
			":(){:|:&};:":              true, // fork bomb
			"chmod -r 777 /":           true,
			"chmod -r 777 /*":          true,
			"mkfs":                     true,
			"mkfs.ext4":                true,
			"> /dev/sda":               true,
			"cat /dev/zero > /dev/sda": true,
		},
		destructivePatterns: []string{
			"rm -rf /",
			"rm -rf /*",
			"rm -rf ~",
			"mkfs.",
			"dd if=/dev/zero of=/dev/",
			"dd if=/dev/urandom of=/dev/",
			"> /dev/sd",
			"chmod -r 777 /",
			"chown -r root /",
			"mv /* ",
			"mv / ",
		},
	}

	// Add user-provided blocked patterns
	sg.destructivePatterns = append(sg.destructivePatterns, cfg.AdditionalBlocked...)

	return sg
}

// IsSafe returns true if the command is safe to suggest as a recovery.
// A command is unsafe if it matches any destructive pattern.
func (sg *SafetyGate) IsSafe(cmdNorm string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmdNorm))
	if lower == "" {
		return false
	}

	// Strip sudo prefix for checking the base command
	base := lower
	if strings.HasPrefix(lower, "sudo ") {
		if !sg.allowSudo {
			return false
		}
		base = strings.TrimSpace(strings.TrimPrefix(lower, "sudo"))
	}

	// Check exact matches
	if sg.destructiveExact[base] {
		return false
	}

	// Check prefix/pattern matches
	for _, pat := range sg.destructivePatterns {
		if strings.HasPrefix(base, pat) || strings.Contains(base, pat) {
			return false
		}
	}

	return true
}

// FilterSafe filters a list of recovery candidates, returning only safe ones.
func (sg *SafetyGate) FilterSafe(candidates []RecoveryCandidate) []RecoveryCandidate {
	safe := make([]RecoveryCandidate, 0, len(candidates))
	for _, c := range candidates {
		if sg.IsSafe(c.RecoveryCmdNorm) {
			safe = append(safe, c)
		}
	}
	return safe
}
