package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	exitClassNotFound = "class:not_found"
	exitClassGeneral  = "class:general"
)

// BootstrapPattern defines a pre-seeded recovery pattern.
type BootstrapPattern struct {
	// FailedCmdNorm is the normalized command that fails.
	// Use "*" as a wildcard meaning "any command failing with this exit class".
	FailedCmdNorm string

	// ExitCodeClass is the failure class (e.g., "class:not_found").
	ExitCodeClass string

	// RecoveryCmdNorm is the suggested recovery command.
	RecoveryCmdNorm string

	// InitialSuccessRate is the bootstrap confidence (0-1).
	InitialSuccessRate float64
}

// DefaultBootstrapPatterns returns the built-in set of common recovery patterns.
// These cover the most frequent failure-recovery pairs observed in practice.
func DefaultBootstrapPatterns() []BootstrapPattern {
	return []BootstrapPattern{
		// Command not found (exit 127) => suggest package installation
		{
			FailedCmdNorm:      "*",
			ExitCodeClass:      exitClassNotFound,
			RecoveryCmdNorm:    "brew install <arg>",
			InitialSuccessRate: 0.70,
		},
		{
			FailedCmdNorm:      "*",
			ExitCodeClass:      exitClassNotFound,
			RecoveryCmdNorm:    "apt install <arg>",
			InitialSuccessRate: 0.70,
		},
		{
			FailedCmdNorm:      "*",
			ExitCodeClass:      exitClassNotFound,
			RecoveryCmdNorm:    "npm install -g <arg>",
			InitialSuccessRate: 0.50,
		},
		{
			FailedCmdNorm:      "*",
			ExitCodeClass:      exitClassNotFound,
			RecoveryCmdNorm:    "pip install <arg>",
			InitialSuccessRate: 0.50,
		},

		// Permission denied (exit 1 with class:permission, or
		// exit 126 not_executable) => sudo or chmod
		{
			FailedCmdNorm:      "*",
			ExitCodeClass:      "class:not_executable",
			RecoveryCmdNorm:    "chmod +x <arg>",
			InitialSuccessRate: 0.80,
		},
		{
			FailedCmdNorm:      "*",
			ExitCodeClass:      "class:not_executable",
			RecoveryCmdNorm:    "sudo <arg>",
			InitialSuccessRate: 0.60,
		},

		// Git merge conflicts
		{
			FailedCmdNorm:      "git merge <arg>",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "git merge --abort",
			InitialSuccessRate: 0.85,
		},
		{
			FailedCmdNorm:      "git merge <arg>",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "git stash",
			InitialSuccessRate: 0.60,
		},
		{
			FailedCmdNorm:      "git pull",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "git stash && git pull",
			InitialSuccessRate: 0.70,
		},
		{
			FailedCmdNorm:      "git rebase <arg>",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "git rebase --abort",
			InitialSuccessRate: 0.85,
		},

		// Build failures
		{
			FailedCmdNorm:      "make <arg>",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "make clean",
			InitialSuccessRate: 0.50,
		},
		{
			FailedCmdNorm:      "make <arg>",
			ExitCodeClass:      "class:misuse",
			RecoveryCmdNorm:    "make clean && make <arg>",
			InitialSuccessRate: 0.45,
		},

		// npm/yarn errors
		{
			FailedCmdNorm:      "npm install",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "rm -rf node_modules && npm install",
			InitialSuccessRate: 0.65,
		},
		{
			FailedCmdNorm:      "npm run <arg>",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "npm install",
			InitialSuccessRate: 0.55,
		},

		// Docker errors
		{
			FailedCmdNorm:      "docker build <arg>",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "docker system prune -f",
			InitialSuccessRate: 0.40,
		},

		// Go errors
		{
			FailedCmdNorm:      "go build <arg>",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "go mod tidy",
			InitialSuccessRate: 0.50,
		},
		{
			FailedCmdNorm:      "go test <arg>",
			ExitCodeClass:      exitClassGeneral,
			RecoveryCmdNorm:    "go mod tidy",
			InitialSuccessRate: 0.35,
		},
	}
}

// SeedBootstrapPatterns inserts the bootstrap recovery patterns into the database.
// It uses source='bootstrap' to distinguish from learned patterns.
// Existing bootstrap patterns are not overwritten (INSERT OR IGNORE).
//
// The safety gate is applied: patterns whose recovery command is unsafe are skipped.
func SeedBootstrapPatterns(ctx context.Context, db *sql.DB, patterns []BootstrapPattern, safety *SafetyGate) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}

	nowMs := time.Now().UnixMilli()
	seeded := 0

	for _, bp := range patterns {
		// Safety gate: skip dangerous recovery commands
		if safety != nil && !safety.IsSafe(bp.RecoveryCmdNorm) {
			continue
		}

		failedTemplateID := bp.FailedCmdNorm
		if failedTemplateID == "*" {
			failedTemplateID = "__wildcard__"
		}

		recoveryTemplateID := computeBootstrapTemplateID(bp.RecoveryCmdNorm)

		_, err := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO failure_recovery (
				scope, failed_template_id, exit_code_class,
				recovery_template_id, weight, count,
				success_rate, last_seen_ms, source
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'bootstrap')
		`,
			"global",
			failedTemplateID,
			bp.ExitCodeClass,
			recoveryTemplateID,
			bp.InitialSuccessRate, // weight = initial success rate
			1,                     // count = 1 (bootstrap seed)
			bp.InitialSuccessRate,
			nowMs,
		)
		if err != nil {
			return seeded, fmt.Errorf("seed bootstrap pattern (%s -> %s): %w",
				bp.FailedCmdNorm, bp.RecoveryCmdNorm, err)
		}

		seeded++
	}

	return seeded, nil
}

// computeBootstrapTemplateID creates a stable template ID for bootstrap patterns.
// Bootstrap patterns use a "bootstrap:" prefix to avoid collision with learned IDs.
func computeBootstrapTemplateID(cmdNorm string) string {
	return "bootstrap:" + cmdNorm
}

// IsBootstrapTemplateID returns true if the template ID is a bootstrap pattern ID.
func IsBootstrapTemplateID(templateID string) bool {
	return len(templateID) > 10 && templateID[:10] == "bootstrap:"
}

// ExtractBootstrapCmd extracts the command template from a bootstrap template ID.
// Returns the raw command norm if it is a bootstrap ID, empty string otherwise.
func ExtractBootstrapCmd(templateID string) string {
	if IsBootstrapTemplateID(templateID) {
		return templateID[10:]
	}
	return ""
}
