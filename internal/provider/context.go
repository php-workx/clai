package provider

import (
	"fmt"
	"strings"
)

// Prompt format fragments reused across multiple builder methods.
const (
	contextHeader  = "Context:\n"
	fmtOS          = "- OS: %s\n"
	fmtShell       = "- Shell: %s\n"
	fmtWorkDir     = "- Working Directory: %s\n"
	fmtCmdHistItem = "%d. %s (exit %d)\n"
)

// ContextBuilder constructs AI prompts with appropriate system context
type ContextBuilder struct {
	os         string
	shell      string
	cwd        string
	recentCmds []CommandContext
}

// NewContextBuilder creates a new ContextBuilder with the given parameters
func NewContextBuilder(os, shell, cwd string, recentCmds []CommandContext) *ContextBuilder {
	return &ContextBuilder{
		os:         os,
		shell:      shell,
		cwd:        cwd,
		recentCmds: TrimRecentCommands(recentCmds),
	}
}

// BuildTextToCommandPrompt builds the prompt for text-to-command requests
func (b *ContextBuilder) BuildTextToCommandPrompt(userPrompt string) string {
	var sb strings.Builder

	sb.WriteString("You are a command-line assistant. Generate shell commands for the user's request.\n\n")
	sb.WriteString(contextHeader)
	fmt.Fprintf(&sb, fmtOS, b.os)
	fmt.Fprintf(&sb, fmtShell, b.shell)
	fmt.Fprintf(&sb, fmtWorkDir, b.cwd)

	if len(b.recentCmds) > 0 {
		sb.WriteString("\nRecent commands:\n")
		for i, cmd := range b.recentCmds {
			fmt.Fprintf(&sb, fmtCmdHistItem, i+1, cmd.Command, cmd.ExitCode)
		}
	}

	fmt.Fprintf(&sb, "\nUser request: %s\n", userPrompt)
	sb.WriteString("\nRespond with 1-3 shell commands, one per line. No explanations.")

	return sb.String()
}

// BuildNextStepPrompt builds the prompt for next step prediction
func (b *ContextBuilder) BuildNextStepPrompt(lastCommand string, exitCode int) string {
	var sb strings.Builder

	sb.WriteString("You are a command-line assistant predicting the next command.\n\n")
	sb.WriteString(contextHeader)
	fmt.Fprintf(&sb, fmtOS, b.os)
	fmt.Fprintf(&sb, fmtShell, b.shell)
	fmt.Fprintf(&sb, fmtWorkDir, b.cwd)
	fmt.Fprintf(&sb, "- Last command: %s\n", lastCommand)
	fmt.Fprintf(&sb, "- Exit code: %d\n", exitCode)

	if len(b.recentCmds) > 0 {
		sb.WriteString("\nPrevious commands:\n")
		for i, cmd := range b.recentCmds {
			fmt.Fprintf(&sb, fmtCmdHistItem, i+1, cmd.Command, cmd.ExitCode)
		}
	}

	sb.WriteString("\nPredict 1-3 likely next commands, one per line. No explanations.")

	return sb.String()
}

// BuildDiagnosePrompt builds the prompt for error diagnosis
func (b *ContextBuilder) BuildDiagnosePrompt(command string, exitCode int, stderr string) string {
	var sb strings.Builder

	sb.WriteString("You are a command-line assistant diagnosing a failed command.\n\n")
	sb.WriteString(contextHeader)
	fmt.Fprintf(&sb, fmtOS, b.os)
	fmt.Fprintf(&sb, fmtShell, b.shell)
	fmt.Fprintf(&sb, fmtWorkDir, b.cwd)
	fmt.Fprintf(&sb, "\nFailed command: %s\n", command)
	fmt.Fprintf(&sb, "Exit code: %d\n", exitCode)

	if stderr != "" {
		fmt.Fprintf(&sb, "\nError output:\n%s\n", truncateStderr(stderr))
	}

	if len(b.recentCmds) > 0 {
		sb.WriteString("\nRecent command history:\n")
		for i, cmd := range b.recentCmds {
			fmt.Fprintf(&sb, fmtCmdHistItem, i+1, cmd.Command, cmd.ExitCode)
		}
	}

	sb.WriteString("\nProvide:\n")
	sb.WriteString("1. A brief explanation of the error (1-2 sentences)\n")
	sb.WriteString("2. 1-3 fix commands, each on its own line starting with '$ '\n")

	return sb.String()
}

// maxStderrLen is the maximum number of characters of stderr to include in prompts.
// The tail is kept since the most relevant error info is typically at the end.
const maxStderrLen = 4096

// truncateStderr keeps the tail of stderr if it exceeds maxStderrLen.
func truncateStderr(s string) string {
	if len(s) <= maxStderrLen {
		return s
	}
	return "…" + s[len(s)-maxStderrLen:]
}

// MaxRecentCommands is the maximum number of recent commands to include in context
const MaxRecentCommands = 10

// TrimRecentCommands limits the recent commands list to MaxRecentCommands
func TrimRecentCommands(cmds []CommandContext) []CommandContext {
	if len(cmds) <= MaxRecentCommands {
		return cmds
	}
	return cmds[len(cmds)-MaxRecentCommands:]
}
