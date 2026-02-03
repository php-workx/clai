package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCmd_HasCommands(t *testing.T) {
	// Verify expected commands are registered
	expectedCommands := []string{
		"ask",
		"cmd",
		"config",
		"daemon",
		"doctor",
		"history",
		"init",
		"install",
		"logs",
		"status",
		"suggest",
		"uninstall",
		"version",
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

func TestRootCmd_CommandGroups(t *testing.T) {
	// Verify command groups are defined
	groups := rootCmd.Groups()
	if len(groups) != 2 {
		t.Errorf("Expected 2 command groups, got %d", len(groups))
	}

	groupIDs := make(map[string]bool)
	for _, g := range groups {
		groupIDs[g.ID] = true
	}

	if !groupIDs["core"] {
		t.Error("Expected 'core' command group")
	}
	if !groupIDs["setup"] {
		t.Error("Expected 'setup' command group")
	}
}

func TestRootCmd_HiddenCommands(t *testing.T) {
	// These commands should be hidden but still functional
	hiddenCommands := []string{"daemon", "logs", "doctor"}

	for _, name := range hiddenCommands {
		var found *cobra.Command
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == name {
				found = cmd
				break
			}
		}

		if found == nil {
			t.Errorf("Hidden command %q should still be registered", name)
			continue
		}

		if !found.Hidden {
			t.Errorf("Command %q should be hidden", name)
		}
	}
}

func TestRootCmd_CoreCommandsGrouped(t *testing.T) {
	// Core commands should be in the core group
	coreCommands := []string{"ask", "cmd", "suggest", "history"}

	for _, name := range coreCommands {
		var found *cobra.Command
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == name {
				found = cmd
				break
			}
		}

		if found == nil {
			t.Errorf("Core command %q not found", name)
			continue
		}

		if found.GroupID != groupCore {
			t.Errorf("Command %q should be in group %q, got %q", name, groupCore, found.GroupID)
		}
	}
}

func TestRootCmd_SetupCommandsGrouped(t *testing.T) {
	// Setup commands should be in the setup group
	setupCommands := []string{"status", "config", "install", "uninstall", "init", "version"}

	for _, name := range setupCommands {
		var found *cobra.Command
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == name {
				found = cmd
				break
			}
		}

		if found == nil {
			t.Errorf("Setup command %q not found", name)
			continue
		}

		if found.GroupID != groupSetup {
			t.Errorf("Command %q should be in group %q, got %q", name, groupSetup, found.GroupID)
		}
	}
}

// Ensure cobra is used
var _ *cobra.Command = nil
