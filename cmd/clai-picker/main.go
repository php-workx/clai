package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"

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
//	1 = cancelled by user OR invalid usage
//	2 = fallback to native history (no TTY, runtime error, etc.)
const (
	exitSuccess      = 0
	exitCancelled    = 1
	exitInvalidUsage = 1
	exitFallback     = 2
)

// maxQueryLen is the maximum length of a query string in bytes.
const (
	maxQueryLen  = 4096
	pickerErrFmt = "clai-picker: %v\n"
)

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
		fmt.Fprintf(os.Stderr, pickerErrFmt, err)
		return exitFallback
	}

	// Step 2: Check TERM != "dumb".
	if err := checkTERM(); err != nil {
		fmt.Fprintf(os.Stderr, pickerErrFmt, err)
		return exitFallback
	}

	// Step 3: Check terminal width >= 20 columns.
	if err := checkTermWidth(); err != nil {
		fmt.Fprintf(os.Stderr, pickerErrFmt, err)
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
		fmt.Fprintf(os.Stderr, pickerErrFmt, err)
		return exitFallback
	}
	defer releaseLock(lockFd)

	// Step 6: Parse subcommand and flags.
	if len(args) == 0 {
		printUsage()
		return exitInvalidUsage
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
		return exitInvalidUsage
	}

	opts, err := parseHistoryFlags(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, pickerErrFmt, err)
		return exitInvalidUsage
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
// It safely truncates to maxQueryLen bytes without splitting UTF-8 runes.
func sanitizeQuery(q string) (string, error) {
	if q == "" {
		return "", nil
	}

	// Reject newlines before stripping.
	if strings.ContainsAny(q, "\n\r") {
		return "", fmt.Errorf("query must not contain newlines")
	}

	// Strip control characters (0x00-0x1F) except tab (0x09).
	// Track byte length during iteration to avoid splitting multibyte runes.
	var b strings.Builder
	b.Grow(len(q))
	currentLen := 0
	for _, r := range q {
		if r >= 0x00 && r <= 0x1F && r != 0x09 {
			continue // strip control char
		}
		runeLen := utf8.RuneLen(r)
		if currentLen+runeLen > maxQueryLen {
			break // stop before exceeding limit
		}
		b.WriteRune(r)
		currentLen += runeLen
	}

	return b.String(), nil
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch backend {
	case "fzf":
		return dispatchFzf(ctx, cfg, opts)
	case "clai":
		return dispatchBuiltin(ctx, cfg, opts)
	case "builtin":
		return dispatchBuiltin(ctx, cfg, opts)
	default:
		// Unknown backend, fall back to builtin.
		debugLog("unknown backend %q, falling back to builtin", backend)
		return dispatchBuiltin(ctx, cfg, opts)
	}
}

// resolveTabs resolves the comma-separated tab IDs in opts to []config.TabDef.
// If opts.tabs is empty, all configured tabs are returned.
// Variable substitution is performed on tab Args values.
func resolveTabs(cfg *config.Config, opts *pickerOpts) []config.TabDef {
	srcTabs := selectPickerTabs(cfg.History.PickerTabs, opts.tabs)
	return substituteTabArgs(srcTabs, opts.session)
}

func selectPickerTabs(allTabs []config.TabDef, tabIDs string) []config.TabDef {
	if tabIDs == "" {
		return allTabs
	}

	idSet := parseTabIDSet(tabIDs)
	selected := make([]config.TabDef, 0, len(idSet))
	for _, t := range allTabs {
		if _, ok := idSet[t.ID]; ok {
			selected = append(selected, t)
		}
	}
	if len(selected) == 0 {
		return allTabs
	}
	return selected
}

func parseTabIDSet(tabIDs string) map[string]struct{} {
	parts := strings.Split(tabIDs, ",")
	idSet := make(map[string]struct{}, len(parts))
	for _, id := range parts {
		idSet[strings.TrimSpace(id)] = struct{}{}
	}
	return idSet
}

func substituteTabArgs(srcTabs []config.TabDef, sessionID string) []config.TabDef {
	tabs := make([]config.TabDef, len(srcTabs))
	for i, t := range srcTabs {
		tabs[i] = t
		if len(t.Args) == 0 {
			continue
		}
		tabs[i].Args = make(map[string]string, len(t.Args))
		for k, v := range t.Args {
			if v == "$CLAI_SESSION_ID" {
				v = sessionID
			}
			tabs[i].Args[k] = v
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
func dispatchBuiltin(_ context.Context, cfg *config.Config, opts *pickerOpts) int {
	tabs := withRuntimeTabOptions(resolveTabs(cfg, opts), cfg)
	provider := picker.NewHistoryProvider(socketPath(cfg))
	defer provider.Close()

	model := picker.NewModel(tabs, provider).WithLayout(picker.LayoutBottomUp)
	model = model.WithPageSize(opts.limit)
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
func dispatchFzf(ctx context.Context, cfg *config.Config, opts *pickerOpts) int {
	_, err := exec.LookPath("fzf")
	if err != nil {
		debugLog("fzf not found on PATH, falling back to builtin")
		return dispatchBuiltin(ctx, cfg, opts)
	}

	result, cancelled, err := runFzfBackend(ctx, cfg, opts)
	if err != nil {
		debugLog("fzf backend error: %v", err)
		return exitFallback
	}

	if cancelled {
		return exitCancelled
	}

	if result != "" {
		fmt.Fprintln(os.Stdout, result)
	}

	return exitSuccess
}

// runFzfBackend fetches all history and pipes it through fzf.
// Returns the selected item, whether the user cancelled, and any error.
func runFzfBackend(ctx context.Context, cfg *config.Config, opts *pickerOpts) (string, bool, error) {
	provider := picker.NewHistoryProvider(socketPath(cfg))
	defer provider.Close()
	tabs := withRuntimeTabOptions(resolveTabs(cfg, opts), cfg)

	tabID, tabOpts := fzfTabContext(tabs)
	limit := opts.limit
	allItems := fetchFzfItems(ctx, provider, opts, tabID, tabOpts, limit)
	if len(allItems) == 0 {
		return "", false, nil
	}

	return runFzfSelection(ctx, opts.query, allItems)
}

func withRuntimeTabOptions(tabs []config.TabDef, cfg *config.Config) []config.TabDef {
	if len(tabs) == 0 {
		return tabs
	}

	out := make([]config.TabDef, len(tabs))
	caseSensitive := strconv.FormatBool(cfg.History.PickerCaseSensitive)
	for i, tab := range tabs {
		out[i] = tab
		if tab.Args == nil {
			out[i].Args = map[string]string{
				"case_sensitive": caseSensitive,
			}
			continue
		}

		args := make(map[string]string, len(tab.Args)+1)
		for k, v := range tab.Args {
			args[k] = v
		}
		args["case_sensitive"] = caseSensitive
		out[i].Args = args
	}

	return out
}

func fzfTabContext(tabs []config.TabDef) (string, map[string]string) {
	if len(tabs) == 0 {
		return "", nil
	}
	return tabs[0].ID, tabs[0].Args
}

func fetchFzfItems(
	ctx context.Context,
	provider picker.Provider,
	opts *pickerOpts,
	tabID string,
	tabOpts map[string]string,
	limit int,
) []string {
	var allItems []string
	offset := 0
	const maxFzfPages = 100
	truncated := true
	for page := 0; page < maxFzfPages; page++ {
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
			truncated = false
			break
		}
		offset += len(resp.Items)
	}
	if truncated && len(allItems) > 0 {
		debugLog("fzf backend: reached max page cap (%d)", maxFzfPages)
	}
	return allItems
}

func runFzfSelection(ctx context.Context, query string, allItems []string) (string, bool, error) {
	args := []string{"--no-sort", "--exact"}
	if query != "" {
		args = append(args, "--query", query)
	}

	cmd := exec.CommandContext(ctx, "fzf", args...)
	cmd.Stdin = strings.NewReader(strings.Join(allItems, "\n"))
	cmd.Stderr = os.Stderr // Let fzf render its TUI on stderr/tty.

	output, err := cmd.Output()
	if err != nil {
		// Check for cancellation via exit codes.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			// fzf exit code 130 = interrupted (Ctrl+C), exit code 1 = no match
			if code == 130 || code == 1 {
				return "", true, nil // User cancelled
			}
		}
		return "", false, err
	}

	return strings.TrimRight(string(output), "\n"), false, nil
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
