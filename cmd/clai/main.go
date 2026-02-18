// Package main is the entry point for the clai CLI.
package main

import (
	"os"

	"github.com/runger/clai/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
