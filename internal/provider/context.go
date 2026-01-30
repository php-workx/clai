package provider

import (
	"fmt"
	"strings"
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
	sb.WriteString("Context:\n")
	sb.WriteString(fmt.Sprintf("- OS: %s\n", b.os))
	sb.WriteString(fmt.Sprintf("- Shell: %s\n", b.shell))
	sb.WriteString(fmt.Sprintf("- Working Directory: %s\n", b.cwd))

	if len(b.recentCmds) > 0 {
		sb.WriteString("\nRecent commands:\n")
		for i, cmd := range b.recentCmds {
			sb.WriteString(fmt.Sprintf("%d. %s (exit %d)\n", i+1, cmd.Command, cmd.ExitCode))
		}
	}

	sb.WriteString(fmt.Sprintf("\nUser request: %s\n", userPrompt))
	sb.WriteString("\nRespond with 1-3 shell commands, one per line. No explanations.")

	return sb.String()
}

// BuildNextStepPrompt builds the prompt for next step prediction
func (b *ContextBuilder) BuildNextStepPrompt(lastCommand string, exitCode int) string {
	var sb strings.Builder

	sb.WriteString("You are a command-line assistant predicting the next command.\n\n")
	sb.WriteString("Context:\n")
	sb.WriteString(fmt.Sprintf("- OS: %s\n", b.os))
	sb.WriteString(fmt.Sprintf("- Shell: %s\n", b.shell))
	sb.WriteString(fmt.Sprintf("- Working Directory: %s\n", b.cwd))
	sb.WriteString(fmt.Sprintf("- Last command: %s\n", lastCommand))
	sb.WriteString(fmt.Sprintf("- Exit code: %d\n", exitCode))

	if len(b.recentCmds) > 0 {
		sb.WriteString("\nPrevious commands:\n")
		for i, cmd := range b.recentCmds {
			sb.WriteString(fmt.Sprintf("%d. %s (exit %d)\n", i+1, cmd.Command, cmd.ExitCode))
		}
	}

	sb.WriteString("\nPredict 1-3 likely next commands, one per line. No explanations.")

	return sb.String()
}

// BuildDiagnosePrompt builds the prompt for error diagnosis
func (b *ContextBuilder) BuildDiagnosePrompt(command string, exitCode int, stderr string) string {
	var sb strings.Builder

	sb.WriteString("You are a command-line assistant diagnosing a failed command.\n\n")
	sb.WriteString("Context:\n")
	sb.WriteString(fmt.Sprintf("- OS: %s\n", b.os))
	sb.WriteString(fmt.Sprintf("- Shell: %s\n", b.shell))
	sb.WriteString(fmt.Sprintf("- Working Directory: %s\n", b.cwd))
	sb.WriteString(fmt.Sprintf("\nFailed command: %s\n", command))
	sb.WriteString(fmt.Sprintf("Exit code: %d\n", exitCode))

	if stderr != "" {
		sb.WriteString(fmt.Sprintf("\nError output:\n%s\n", stderr))
	}

	if len(b.recentCmds) > 0 {
		sb.WriteString("\nRecent command history:\n")
		for i, cmd := range b.recentCmds {
			sb.WriteString(fmt.Sprintf("%d. %s (exit %d)\n", i+1, cmd.Command, cmd.ExitCode))
		}
	}

	sb.WriteString("\nProvide:\n")
	sb.WriteString("1. A brief explanation of the error (1-2 sentences)\n")
	sb.WriteString("2. 1-3 fix commands, each on its own line starting with '$ '\n")

	return sb.String()
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
