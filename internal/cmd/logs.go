package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
)

var (
	logsFollow bool
	logsLines  int
)

var logsCmd = &cobra.Command{
	Use:    "logs",
	Short:  "View daemon logs",
	Hidden: true,
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
}

func runLogs(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	logFile, ok := resolveLogFile(paths)
	if !ok {
		fmt.Printf("No log file found at: %s\n", paths.LogFile())
		fmt.Println("The daemon may not have been started yet.")
		return nil
	}

	if logsFollow {
		return followLogs(cmd.Context(), logFile)
	}

	return tailLogs(logFile, logsLines)
}

func resolveLogFile(paths *config.Paths) (string, bool) {
	primary := paths.LogFile()
	if _, err := os.Stat(primary); err == nil {
		return primary, true
	}
	legacy := filepath.Join(paths.BaseDir, "clai.log")
	if _, err := os.Stat(legacy); err == nil {
		return legacy, true
	}
	return primary, false
}

func tailLogs(filename string, n int) error {
	if n <= 0 {
		return fmt.Errorf("lines must be a positive number")
	}

	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	size := stat.Size()
	if size == 0 {
		fmt.Println("Log file is empty.")
		return nil
	}

	lines, err := collectTailLines(f, size, n)
	if err != nil {
		return fmt.Errorf("failed to read log tail: %w", err)
	}

	for _, line := range lines {
		fmt.Println(line)
	}

	return nil
}

// collectTailLines reads the last n lines from f whose total size is fileSize.
func collectTailLines(f *os.File, fileSize int64, n int) ([]string, error) {
	lines := make([]string, 0, n)
	bufSize := int64(4096)
	offset := fileSize
	remainder := "" // Carry partial line fragment between chunks

	for len(lines) < n && offset > 0 {
		chunkLines, newRemainder, err := readChunkLines(f, &offset, bufSize, remainder)
		if err != nil {
			return nil, err
		}
		remainder = newRemainder
		lines = prependLines(lines, chunkLines, n)
	}

	// Include remainder if we have room and it's not empty.
	if remainder != "" && len(lines) < n {
		lines = append([]string{remainder}, lines...)
	}

	return lines, nil
}

// readChunkLines reads one chunk ending at *offset, splits it into lines, and
// returns the full lines plus any leading partial-line remainder.
// *offset is decremented by the bytes read.
func readChunkLines(f *os.File, offset *int64, bufSize int64, prevRemainder string) (fullLines []string, remainder string, err error) {
	readSize := bufSize
	if *offset < bufSize {
		readSize = *offset
	}
	*offset -= readSize

	buf := make([]byte, readSize)
	n, readErr := f.ReadAt(buf, *offset)
	if readErr != nil && readErr != io.EOF {
		return nil, "", fmt.Errorf("read log chunk: %w", readErr)
	}
	buf = buf[:n]

	chunk := string(buf) + prevRemainder
	chunkLines := splitLines(chunk)

	if *offset > 0 && len(chunkLines) > 0 {
		remainder = chunkLines[0]
		chunkLines = chunkLines[1:]
	}

	return chunkLines, remainder, nil
}

// prependLines collects lines from chunkLines (in reverse) into dst until cap n is reached.
func prependLines(dst, chunkLines []string, n int) []string {
	for i := len(chunkLines) - 1; i >= 0 && len(dst) < n; i-- {
		if chunkLines[i] != "" || len(dst) > 0 {
			dst = append([]string{chunkLines[i]}, dst...)
		}
	}
	return dst
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

func followLogs(ctx context.Context, filename string) error {
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Print any partial fragment before waiting
				if line != "" {
					fmt.Print(line)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
				continue
			}
			return fmt.Errorf("error reading log: %w", err)
		}

		fmt.Print(line)
	}
}
