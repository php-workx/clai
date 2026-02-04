package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/runger/clai/internal/config"

	_ "github.com/charmbracelet/bubbletea"
	_ "github.com/charmbracelet/lipgloss"
)

// Version information (set via ldflags during build).
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// Exit codes.
const (
	exitSuccess    = 0
	exitError      = 1
	exitNoTerminal = 2
)

// maxQueryLen is the maximum length of a query string in bytes.
const maxQueryLen = 4096

// pickerOpts holds the parsed command-line options for the history subcommand.
type pickerOpts struct {
	tabs    string
	limit   int
	query   string
	session string
	output  string
	cwd     string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is the main entry point, returning an exit code.
// It is separated from main() to enable testing.
func run(args []string) int {
	// Step 1: Check /dev/tty is openable.
	if err := checkTTY(); err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitNoTerminal
	}

	// Step 2: Check TERM != "dumb".
	if err := checkTERM(); err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitNoTerminal
	}

	// Step 3: Check terminal width >= 20 columns.
	if err := checkTermWidth(); err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitNoTerminal
	}

	// Step 4: Ensure cache directory exists.
	paths := config.DefaultPaths()
	cacheDir := paths.CacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: failed to create cache directory: %v\n", err)
		return exitError
	}

	// Step 5: Acquire advisory file lock.
	lockPath := cacheDir + "/picker.lock"
	lockFd, err := acquireLock(lockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitError
	}
	defer releaseLock(lockFd)

	// Step 6: Parse subcommand and flags.
	if len(args) == 0 {
		printUsage()
		return exitError
	}

	switch args[0] {
	case "history":
		// continue below
	case "--help", "-h":
		printUsage()
		return exitSuccess
	case "--version", "-v":
		printVersion()
		return exitSuccess
	default:
		fmt.Fprintf(os.Stderr, "clai-picker: unknown command %q\n", args[0])
		printUsage()
		return exitError
	}

	opts, err := parseHistoryFlags(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitError
	}

	// Step 7: Load config.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: failed to load config: %v\n", err)
		return exitError
	}

	// Apply config defaults for flags that weren't explicitly set.
	if opts.limit == 0 {
		opts.limit = cfg.History.PickerPageSize
	}
	if opts.tabs == "" {
		tabIDs := make([]string, len(cfg.History.PickerTabs))
		for i, t := range cfg.History.PickerTabs {
			tabIDs[i] = t.ID
		}
		opts.tabs = strings.Join(tabIDs, ",")
	}

	// Step 8: Dispatch to backend.
	return dispatch(cfg, opts)
}

// parseHistoryFlags parses flags for the "history" subcommand.
func parseHistoryFlags(args []string) (*pickerOpts, error) {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := &pickerOpts{}
	fs.StringVar(&opts.tabs, "tabs", "", "comma-separated tab IDs")
	fs.IntVar(&opts.limit, "limit", 0, "number of items per page (positive integer)")
	fs.StringVar(&opts.query, "query", "", "initial search query (max 4096 bytes)")
	fs.StringVar(&opts.session, "session", "", "session ID")
	fs.StringVar(&opts.output, "output", "", "output format (only \"plain\" accepted)")
	fs.StringVar(&opts.cwd, "cwd", "", "working directory")

	// Custom usage for --help within the history subcommand.
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: clai-picker history [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Reject unknown positional arguments.
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	// Validate limit.
	if opts.limit < 0 {
		return nil, fmt.Errorf("--limit must be a positive integer")
	}

	// Validate output.
	if opts.output != "" && opts.output != "plain" {
		return nil, fmt.Errorf("--output must be \"plain\" (got %q)", opts.output)
	}

	// Sanitize query.
	sanitized, err := sanitizeQuery(opts.query)
	if err != nil {
		return nil, fmt.Errorf("--query: %w", err)
	}
	opts.query = sanitized

	return opts, nil
}

// sanitizeQuery strips control characters and validates the query string.
func sanitizeQuery(q string) (string, error) {
	if q == "" {
		return "", nil
	}

	// Reject newlines before stripping.
	if strings.ContainsAny(q, "\n\r") {
		return "", fmt.Errorf("query must not contain newlines")
	}

	// Strip control characters (0x00-0x1F) except tab (0x09).
	var b strings.Builder
	b.Grow(len(q))
	for _, r := range q {
		if r >= 0x00 && r <= 0x1F && r != 0x09 {
			continue // strip control char
		}
		b.WriteRune(r)
	}
	result := b.String()

	// Truncate to maxQueryLen bytes.
	if len(result) > maxQueryLen {
		result = result[:maxQueryLen]
	}

	return result, nil
}

// dispatch routes to the appropriate backend.
func dispatch(cfg *config.Config, opts *pickerOpts) int {
	backend := cfg.History.PickerBackend
	if backend == "" {
		backend = "builtin"
	}

	return dispatchBackend(backend, opts)
}

// dispatchBackend executes the selected backend or falls back.
func dispatchBackend(backend string, opts *pickerOpts) int {
	switch backend {
	case "fzf":
		return dispatchFzf(opts)
	case "clai":
		return dispatchBuiltin(opts)
	case "builtin":
		return dispatchBuiltin(opts)
	default:
		// Unknown backend, fall back to builtin.
		debugLog("unknown backend %q, falling back to builtin", backend)
		return dispatchBuiltin(opts)
	}
}

// dispatchBuiltin runs the built-in Bubble Tea TUI (placeholder).
func dispatchBuiltin(_ *pickerOpts) int {
	fmt.Fprintln(os.Stderr, "builtin backend")
	return exitSuccess
}

// dispatchFzf checks for fzf on PATH and falls back to builtin if missing.
func dispatchFzf(opts *pickerOpts) int {
	_, err := exec.LookPath("fzf")
	if err != nil {
		debugLog("fzf not found on PATH, falling back to builtin")
		return dispatchBuiltin(opts)
	}
	// Placeholder: for now fzf backend acts like builtin.
	fmt.Fprintln(os.Stderr, "fzf backend")
	return exitSuccess
}

// debugLog logs a message to stderr when CLAI_DEBUG=1.
func debugLog(format string, args ...any) {
	if os.Getenv("CLAI_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "clai-picker: debug: "+format+"\n", args...)
	}
}

// printUsage prints the top-level usage message.
func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: clai-picker <command> [flags]

Commands:
  history    Browse and search shell history

Flags:
  --help     Show this help message
  --version  Print version information`)
}

// printVersion prints version information.
func printVersion() {
	fmt.Printf("clai-picker %s\n", Version)
	fmt.Printf("  commit: %s\n", GitCommit)
	fmt.Printf("  built:  %s\n", BuildDate)
}
