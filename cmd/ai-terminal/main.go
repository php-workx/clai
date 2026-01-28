package main

import (
	"os"

	"github.com/runger/ai-terminal/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
