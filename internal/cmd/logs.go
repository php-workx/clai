package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
)

var (
	logsFollow bool
	logsLines  int
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View daemon logs",
	Long: `View the clai daemon log file.

By default, shows the last 50 lines of the log file.
Use --follow to continuously monitor new log entries.

Examples:
  clai logs              # Show last 50 lines
  clai logs -f           # Follow log output
  clai logs --lines=100  # Show last 100 lines`,
	RunE: runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 50, "Number of lines to show")
	rootCmd.AddCommand(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	logFile := paths.LogFile()

	// Check if log file exists
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		fmt.Printf("No log file found at: %s\n", logFile)
		fmt.Println("The daemon may not have been started yet.")
		return nil
	}

	if logsFollow {
		return followLogs(logFile)
	}

	return tailLogs(logFile, logsLines)
}

func tailLogs(filename string, n int) error {
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// Get file size
	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	size := stat.Size()
	if size == 0 {
		fmt.Println("Log file is empty.")
		return nil
	}

	// Read from the end to find the last n lines
	lines := make([]string, 0, n)
	bufSize := int64(4096)
	offset := size

	for len(lines) < n && offset > 0 {
		// Calculate read position
		readSize := bufSize
		if offset < bufSize {
			readSize = offset
		}
		offset -= readSize

		// Read chunk
		buf := make([]byte, readSize)
		_, err := f.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read log file: %w", err)
		}

		// Parse lines from chunk (in reverse order)
		chunk := string(buf)
		chunkLines := splitLines(chunk)

		// Prepend lines
		for i := len(chunkLines) - 1; i >= 0 && len(lines) < n; i-- {
			if chunkLines[i] != "" || len(lines) > 0 {
				lines = append([]string{chunkLines[i]}, lines...)
			}
		}
	}

	// Print lines
	for _, line := range lines {
		fmt.Println(line)
	}

	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func followLogs(filename string) error {
	// Open file
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// Seek to end
	_, err = f.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}

	fmt.Printf("Following %s (Ctrl+C to stop)...\n", filename)
	fmt.Println()

	reader := bufio.NewReader(f)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Wait and try again
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return fmt.Errorf("error reading log: %w", err)
		}

		fmt.Print(line)
	}
}
