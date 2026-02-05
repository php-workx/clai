package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/picker"
)

// Version information (set via ldflags during build).
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// Exit codes.
// These match the expectations of shell scripts:
//
//	0 = selection made (use the result)
//	1 = cancelled by user (keep original input)
//	2 = fallback to native history (no TTY, error, etc.)
const (
	exitSuccess   = 0
	exitCancelled = 1
	exitFallback  = 2
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
		return exitFallback
	}

	// Step 2: Check TERM != "dumb".
	if err := checkTERM(); err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitFallback
	}

	// Step 3: Check terminal width >= 20 columns.
	if err := checkTermWidth(); err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitFallback
	}

	// Step 4: Ensure cache directory exists.
	paths := config.DefaultPaths()
	cacheDir := paths.CacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: failed to create cache directory: %v\n", err)
		return exitFallback
	}

	// Step 5: Acquire advisory file lock.
	lockPath := cacheDir + "/picker.lock"
	lockFd, err := acquireLock(lockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitFallback
	}
	defer releaseLock(lockFd)

	// Step 6: Parse subcommand and flags.
	if len(args) == 0 {
		printUsage()
		return exitFallback
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
		return exitFallback
	}

	opts, err := parseHistoryFlags(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: %v\n", err)
		return exitFallback
	}

	// Step 7: Load config.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: failed to load config: %v\n", err)
		return exitFallback
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

	return dispatchBackend(backend, cfg, opts)
}

// dispatchBackend executes the selected backend or falls back.
func dispatchBackend(backend string, cfg *config.Config, opts *pickerOpts) int {
	switch backend {
	case "fzf":
		return dispatchFzf(cfg, opts)
	case "clai":
		return dispatchBuiltin(cfg, opts)
	case "builtin":
		return dispatchBuiltin(cfg, opts)
	default:
		// Unknown backend, fall back to builtin.
		debugLog("unknown backend %q, falling back to builtin", backend)
		return dispatchBuiltin(cfg, opts)
	}
}

// resolveTabs resolves the comma-separated tab IDs in opts to []config.TabDef.
// If opts.tabs is empty, all configured tabs are returned.
// Variable substitution is performed on tab Args values.
func resolveTabs(cfg *config.Config, opts *pickerOpts) []config.TabDef {
	var srcTabs []config.TabDef
	if opts.tabs == "" {
		srcTabs = cfg.History.PickerTabs
	} else {
		ids := strings.Split(opts.tabs, ",")
		idSet := make(map[string]bool, len(ids))
		for _, id := range ids {
			idSet[strings.TrimSpace(id)] = true
		}
		for _, t := range cfg.History.PickerTabs {
			if idSet[t.ID] {
				srcTabs = append(srcTabs, t)
			}
		}
		// If no matches, return all configured tabs as fallback.
		if len(srcTabs) == 0 {
			srcTabs = cfg.History.PickerTabs
		}
	}

	// Substitute variables in tab Args.
	tabs := make([]config.TabDef, len(srcTabs))
	for i, t := range srcTabs {
		tabs[i] = t
		if len(t.Args) > 0 {
			tabs[i].Args = make(map[string]string, len(t.Args))
			for k, v := range t.Args {
				// Replace $CLAI_SESSION_ID with the actual session ID.
				if v == "$CLAI_SESSION_ID" && opts.session != "" {
					v = opts.session
				}
				tabs[i].Args[k] = v
			}
		}
	}
	return tabs
}

// socketPath returns the daemon socket path from config or the default.
func socketPath(cfg *config.Config) string {
	if path := os.Getenv("CLAI_SOCKET"); path != "" {
		return path
	}
	if cfg.Daemon.SocketPath != "" {
		return cfg.Daemon.SocketPath
	}
	return config.DefaultPaths().SocketFile()
}

// dispatchBuiltin runs the built-in Bubble Tea TUI.
func dispatchBuiltin(cfg *config.Config, opts *pickerOpts) int {
	tabs := resolveTabs(cfg, opts)
	provider := picker.NewHistoryProvider(socketPath(cfg))

	model := picker.NewModel(tabs, provider).WithLayout(picker.LayoutBottomUp)
	if opts.query != "" {
		model = model.WithQuery(opts.query)
	}

	// Open /dev/tty for TUI input/output since stdin/stdout are used for data.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: cannot open /dev/tty: %v\n", err)
		return exitFallback
	}
	defer tty.Close()

	// Detect color profile from the tty and apply it to the default renderer.
	// When invoked via $(clai-picker ...), stdout is a pipe so lipgloss
	// defaults to Ascii (no color). We detect from the real tty instead.
	// SetColorProfile modifies the existing default renderer in-place so
	// package-level styles already created in picker/model.go pick it up.
	lipgloss.SetColorProfile(termenv.NewOutput(tty).ColorProfile())

	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithInput(tty),
		tea.WithOutput(tty),
	)

	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: TUI error: %v\n", err)
		return exitFallback
	}

	m, ok := finalModel.(picker.Model)
	if !ok {
		fmt.Fprintln(os.Stderr, "clai-picker: unexpected model type")
		return exitFallback
	}

	if m.IsCancelled() {
		return exitCancelled
	}

	if result := m.Result(); result != "" {
		fmt.Fprintln(os.Stdout, result)
	}

	return exitSuccess
}

// dispatchFzf checks for fzf on PATH and falls back to builtin if missing.
func dispatchFzf(cfg *config.Config, opts *pickerOpts) int {
	_, err := exec.LookPath("fzf")
	if err != nil {
		debugLog("fzf not found on PATH, falling back to builtin")
		return dispatchBuiltin(cfg, opts)
	}

	result, err := runFzfBackend(cfg, opts)
	if err != nil {
		// fzf exit code 130 = cancelled by user, exit code 1 = no match
		debugLog("fzf backend error: %v", err)
		return exitSuccess
	}

	if result != "" {
		fmt.Fprintln(os.Stdout, result)
	}

	return exitSuccess
}

// runFzfBackend fetches all history and pipes it through fzf.
func runFzfBackend(cfg *config.Config, opts *pickerOpts) (string, error) {
	provider := picker.NewHistoryProvider(socketPath(cfg))
	tabs := resolveTabs(cfg, opts)

	// Use the first tab for fzf (fzf doesn't support tabs).
	var tabID string
	var tabOpts map[string]string
	if len(tabs) > 0 {
		tabID = tabs[0].ID
		tabOpts = tabs[0].Args
	}

	// Fetch all items by paginating until AtEnd.
	ctx := context.Background()
	var allItems []string
	offset := 0
	limit := cfg.History.PickerPageSize
	if limit <= 0 {
		limit = 100
	}

	for {
		resp, err := provider.Fetch(ctx, picker.Request{
			Query:   opts.query,
			TabID:   tabID,
			Options: tabOpts,
			Limit:   limit,
			Offset:  offset,
		})
		if err != nil {
			debugLog("fzf backend: fetch error: %v", err)
			break
		}
		allItems = append(allItems, resp.Items...)
		if resp.AtEnd || len(resp.Items) == 0 {
			break
		}
		offset += len(resp.Items)
	}

	if len(allItems) == 0 {
		return "", nil
	}

	// Build fzf command.
	args := []string{"--no-sort", "--exact"}
	if opts.query != "" {
		args = append(args, "--query", opts.query)
	}

	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(strings.Join(allItems, "\n"))
	cmd.Stderr = os.Stderr // Let fzf render its TUI on stderr/tty.

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimRight(string(output), "\n"), nil
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
