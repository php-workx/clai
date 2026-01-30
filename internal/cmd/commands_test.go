package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCmd_HasCommands(t *testing.T) {
	// Verify expected commands are registered
	expectedCommands := []string{
		"ask",
		"config",
		"daemon",
		"diagnose",
		"doctor",
		"extract",
		"history",
		"init",
		"install",
		"logs",
		"status",
		"suggest",
		"uninstall",
		"version",
		"voice",
	}

	commands := rootCmd.Commands()
	cmdNames := make(map[string]bool)
	for _, cmd := range commands {
		cmdNames[cmd.Name()] = true
	}

	for _, expected := range expectedCommands {
		if !cmdNames[expected] {
			t.Errorf("Expected command %q to be registered, but it's not", expected)
		}
	}
}

func TestRootCmd_Description(t *testing.T) {
	if rootCmd.Short == "" {
		t.Error("Root command should have a short description")
	}
	if rootCmd.Long == "" {
		t.Error("Root command should have a long description")
	}
	if rootCmd.Use != "clai" {
		t.Errorf("Root command Use should be 'clai', got %q", rootCmd.Use)
	}
}

func TestDaemonCmd_HasSubcommands(t *testing.T) {
	// Find daemon command
	var daemon *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "daemon" {
			daemon = cmd
			break
		}
	}

	if daemon == nil {
		t.Fatal("daemon command not found")
	}

	expectedSubs := []string{"start", "stop", "status"}
	subCmds := make(map[string]bool)
	for _, cmd := range daemon.Commands() {
		subCmds[cmd.Name()] = true
	}

	for _, expected := range expectedSubs {
		if !subCmds[expected] {
			t.Errorf("Expected daemon subcommand %q to be registered", expected)
		}
	}
}

// Ensure cobra is used
var _ *cobra.Command = nil
