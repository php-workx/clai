package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version information (set via ldflags during build)
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print version information",
	GroupID: groupSetup,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("clai %s\n", Version)
		fmt.Printf("  commit: %s\n", GitCommit)
		fmt.Printf("  built:  %s\n", BuildDate)
	},
}

// test
