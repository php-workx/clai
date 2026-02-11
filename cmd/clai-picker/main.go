package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
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

var (
	checkTTYFn       = checkTTY
	checkTERMFn      = checkTERM
	checkTermWidthFn = checkTermWidth
	mkdirAllFn       = os.MkdirAll
	defaultPathsFn   = config.DefaultPaths
	acquireLockFn    = acquireLock
	releaseLockFn    = releaseLock
	loadConfigFn     = config.Load

	dispatchHistoryFn = dispatchHistory
	dispatchSuggestFn = dispatchSuggest
	dispatchBuiltinFn = dispatchBuiltin
	dispatchFzfFn     = dispatchFzf
	runTUIFn          = runTUI

	lookPathFn = exec.LookPath

	newHistoryProviderFn = func(socketPath string) picker.Provider {
		return picker.NewHistoryProvider(socketPath)
	}

	runFzfCommandOutputFn = func(args []string, input string) ([]byte, error) {
		cmd := exec.Command("fzf", args...)
		cmd.Stdin = strings.NewReader(input)
		cmd.Stderr = os.Stderr // Let fzf render its TUI on stderr/tty.
		return cmd.Output()
	}
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

const pickerErrorFmt = "clai-picker: %v\n"

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

type subcommand string

const (
	cmdHistory subcommand = "history"
	cmdSuggest subcommand = "suggest"
	cmdUnknown subcommand = "unknown"
)

// run is the main entry point, returning an exit code.
// It is separated from main() to enable testing.
func run(args []string) int {
	// Step 1: Check /dev/tty is openable.
	if err := checkTTYFn(); err != nil {
		fmt.Fprintf(os.Stderr, pickerErrorFmt, err)
		return exitFallback
	}

	// Step 2: Check TERM != "dumb".
	if err := checkTERMFn(); err != nil {
		fmt.Fprintf(os.Stderr, pickerErrorFmt, err)
		return exitFallback
	}

	// Step 3: Check terminal width >= 20 columns.
	if err := checkTermWidthFn(); err != nil {
		fmt.Fprintf(os.Stderr, pickerErrorFmt, err)
		return exitFallback
	}

	// Step 4: Ensure cache directory exists.
	paths := defaultPathsFn()
	cacheDir := paths.CacheDir()
	if err := mkdirAllFn(cacheDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: failed to create cache directory: %v\n", err)
		return exitFallback
	}

	// Step 5: Acquire advisory file lock.
	lockPath := cacheDir + "/picker.lock"
	lockFd, err := acquireLockFn(lockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, pickerErrorFmt, err)
		return exitFallback
	}
	defer releaseLockFn(lockFd)

	// Step 6: Parse subcommand and flags.
	cmd, opts, exitCode, showUsage, parseErr := parseRunInputs(args)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, pickerErrorFmt, parseErr)
		if showUsage {
			printUsage()
		}
		return exitFallback
	}
	if exitCode != 0 {
		if showUsage {
			printUsage()
		}
		return exitCode
	}

	// Step 7: Load config.
	cfg, err := loadConfigFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "clai-picker: failed to load config: %v\n", err)
		return exitFallback
	}

	// Apply config defaults for flags that weren't explicitly set.
	applyRunDefaults(cmd, cfg, opts)

	// Step 8: Dispatch to backend.
	return dispatchRunCommand(cmd, cfg, opts)
}

func parseRunInputs(args []string) (subcommand, *pickerOpts, int, bool, error) {
	if len(args) == 0 {
		return cmdUnknown, nil, exitFallback, true, nil
	}

	var cmd subcommand
	switch args[0] {
	case "history":
		cmd = cmdHistory
	case "suggest":
		cmd = cmdSuggest
	case "--help", "-h":
		printUsage()
		return cmdUnknown, nil, exitSuccess, false, nil
	case "--version", "-v":
		printVersion()
		return cmdUnknown, nil, exitSuccess, false, nil
	default:
		return cmdUnknown, nil, exitFallback, true, fmt.Errorf("unknown command %q", args[0])
	}

	var (
		opts     *pickerOpts
		parseErr error
	)
	switch cmd {
	case cmdHistory:
		opts, parseErr = parseHistoryFlags(args[1:])
	case cmdSuggest:
		opts, parseErr = parseSuggestFlags(args[1:])
	default:
		parseErr = fmt.Errorf("unknown command %q", args[0])
	}
	if parseErr != nil {
		return cmd, nil, exitFallback, false, parseErr
	}

	return cmd, opts, 0, false, nil
}

func applyRunDefaults(cmd subcommand, cfg *config.Config, opts *pickerOpts) {
	switch cmd {
	case cmdHistory:
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
	case cmdSuggest:
		if opts.limit == 0 {
			opts.limit = cfg.Suggestions.MaxResults
		}
	}
}

func dispatchRunCommand(cmd subcommand, cfg *config.Config, opts *pickerOpts) int {
	switch cmd {
	case cmdHistory:
		return dispatchHistoryFn(cfg, opts)
	case cmdSuggest:
		return dispatchSuggestFn(cfg, opts)
	default:
		printUsage()
		return exitFallback
	}
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

// parseSuggestFlags parses flags for the "suggest" subcommand.
func parseSuggestFlags(args []string) (*pickerOpts, error) {
	fs := flag.NewFlagSet("suggest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	opts := &pickerOpts{}
	fs.IntVar(&opts.limit, "limit", 0, "max results to request (positive integer)")
	fs.StringVar(&opts.query, "query", "", "initial buffer/query (max 4096 bytes)")
	fs.StringVar(&opts.session, "session", "", "session ID")
	fs.StringVar(&opts.cwd, "cwd", "", "working directory")
	fs.StringVar(&opts.output, "output", "", "output format (only \"plain\" accepted)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: clai-picker suggest [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	if opts.limit < 0 {
		return nil, fmt.Errorf("--limit must be a positive integer")
	}
	if opts.output != "" && opts.output != "plain" {
		return nil, fmt.Errorf("--output must be \"plain\" (got %q)", opts.output)
	}

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
		// Keep string valid UTF-8 by trimming to the last rune boundary.
		end := maxQueryLen
		for end > 0 && !utf8.RuneStart(result[end]) {
			end--
		}
		result = result[:end]
	}

	return result, nil
}

func dispatchHistory(cfg *config.Config, opts *pickerOpts) int {
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
		return dispatchFzfFn(cfg, opts)
	case "clai":
		return dispatchBuiltinFn(cfg, opts)
	case "builtin":
		return dispatchBuiltinFn(cfg, opts)
	default:
		// Unknown backend, fall back to builtin.
		debugLog("unknown backend %q, falling back to builtin", backend)
		return dispatchBuiltinFn(cfg, opts)
	}
}

// resolveTabs resolves the comma-separated tab IDs in opts to []config.TabDef.
// If opts.tabs is empty, all configured tabs are returned.
// Variable substitution is performed on tab Args values.
func resolveTabs(cfg *config.Config, opts *pickerOpts) []config.TabDef {
	srcTabs := selectConfiguredTabs(cfg.History.PickerTabs, opts.tabs)
	return substituteTabArgs(srcTabs, opts.session)
}

func selectConfiguredTabs(allTabs []config.TabDef, tabsArg string) []config.TabDef {
	if tabsArg == "" {
		return allTabs
	}

	ids := strings.Split(tabsArg, ",")
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[strings.TrimSpace(id)] = true
	}

	var selected []config.TabDef
	for _, t := range allTabs {
		if idSet[t.ID] {
			selected = append(selected, t)
		}
	}
	if len(selected) == 0 {
		return allTabs
	}
	return selected
}

func substituteTabArgs(srcTabs []config.TabDef, session string) []config.TabDef {
	tabs := make([]config.TabDef, len(srcTabs))
	for i, t := range srcTabs {
		tabs[i] = t
		if len(t.Args) == 0 {
			continue
		}

		tabs[i].Args = make(map[string]string, len(t.Args))
		for k, v := range t.Args {
			if v == "$CLAI_SESSION_ID" && session != "" {
				v = session
			}
			tabs[i].Args[k] = v
		}
	}
	return tabs
}

// socketPath returns the daemon socket path from config or the default.
func socketPath(cfg *config.Config) string {
	if cfg.Daemon.SocketPath != "" {
		return cfg.Daemon.SocketPath
	}
	return config.DefaultPaths().SocketFile()
}

func runTUI(model picker.Model) (int, string) {
	// Open /dev/tty for TUI input/output since stdin/stdout are used for data.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return exitFallback, fmt.Sprintf("clai-picker: cannot open /dev/tty: %v", err)
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
		return exitFallback, fmt.Sprintf("clai-picker: TUI error: %v", err)
	}

	m, ok := finalModel.(picker.Model)
	if !ok {
		return exitFallback, "clai-picker: unexpected model type"
	}

	if m.IsCancelled() {
		return exitCancelled, ""
	}

	return exitSuccess, m.Result()
}

// dispatchBuiltin runs the built-in Bubble Tea TUI for history.
func dispatchBuiltin(cfg *config.Config, opts *pickerOpts) int {
	tabs := resolveTabs(cfg, opts)
	provider := newHistoryProviderFn(socketPath(cfg))

	model := picker.NewModel(tabs, provider).WithLayout(picker.LayoutBottomUp)
	if opts.query != "" {
		model = model.WithQuery(opts.query)
	}

	code, result := runTUIFn(model)
	if code != exitSuccess {
		if code == exitFallback && result != "" {
			fmt.Fprintln(os.Stderr, result)
		}
		return code
	}

	if result != "" {
		fmt.Fprintln(os.Stdout, result)
	}

	return exitSuccess
}

func dispatchSuggest(cfg *config.Config, opts *pickerOpts) int {
	model := newSuggestModel(cfg, opts)

	code, result := runTUIFn(model)
	if code != exitSuccess {
		if code == exitFallback && result != "" {
			fmt.Fprintln(os.Stderr, result)
		}
		return code
	}

	if result != "" {
		fmt.Fprintln(os.Stdout, result)
	}
	return exitSuccess
}

func newSuggestModel(cfg *config.Config, opts *pickerOpts) picker.Model {
	// Suggestions are always rendered using the builtin TUI.
	tab := config.TabDef{
		ID:       "suggestions",
		Label:    "Suggestions",
		Provider: "suggest",
		Args: map[string]string{
			"session_id": opts.session,
			"cwd":        opts.cwd,
		},
	}

	provider := picker.NewSuggestProvider(socketPath(cfg), cfg.Suggestions.PickerView)

	// Bottom-up layout: best suggestion appears closest to the input line.
	model := picker.NewModel([]config.TabDef{tab}, provider).WithLayout(picker.LayoutBottomUp)
	if opts.query != "" {
		model = model.WithQuery(opts.query)
	}
	return model
}

// dispatchFzf checks for fzf on PATH and falls back to builtin if missing.
func dispatchFzf(cfg *config.Config, opts *pickerOpts) int {
	_, err := lookPathFn("fzf")
	if err != nil {
		debugLog("fzf not found on PATH, falling back to builtin")
		return dispatchBuiltinFn(cfg, opts)
	}

	result, err := runFzfBackend(cfg, opts)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// fzf exit code 130 = cancelled by user, exit code 1 = no match
			// Keep the original input unchanged in both cases.
			if code := exitErr.ExitCode(); code == 130 || code == 1 {
				debugLog("fzf cancelled/no-match (exit=%d)", code)
				return exitCancelled
			}
		}
		debugLog("fzf backend error: %v", err)
		return exitFallback
	}

	if result != "" {
		fmt.Fprintln(os.Stdout, result)
	}

	return exitSuccess
}

// runFzfBackend fetches all history and pipes it through fzf.
func runFzfBackend(cfg *config.Config, opts *pickerOpts) (string, error) {
	provider := newHistoryProviderFn(socketPath(cfg))
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
		for _, it := range resp.Items {
			allItems = append(allItems, it.Value)
		}
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

	output, err := runFzfCommandOutputFn(args, strings.Join(allItems, "\n"))
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
  suggest    Browse and search smart suggestions

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
