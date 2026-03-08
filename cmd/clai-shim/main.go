// clai-shim is a thin client that communicates with the clai daemon via gRPC.
// It is designed for minimal startup time and silent failure behavior.
//
// Subcommands:
//   - session-start: Notify daemon of new shell session
//   - session-end: Notify daemon of shell session ending
//   - log-start: Log command start
//   - log-end: Log command completion
//   - suggest: Get command suggestions
//   - text-to-command: Convert natural language to commands
//   - --persistent: Enter persistent mode (NDJSON stdin loop)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/runger/clai/internal/ipc"
	"github.com/runger/clai/internal/shim"
)

// Version info - injected at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// Flag name constants to avoid duplication
const (
	flagSessionID     = "session-id"
	flagCommandID     = "command-id"
	flagCwd           = "cwd"
	flagShell         = "shell"
	flagCommand       = "command"
	flagBuffer        = "buffer"
	flagCursor        = "cursor"
	flagPrompt        = "prompt"
	flagExitCode      = "exit-code"
	flagDuration      = "duration"
	flagGitBranch     = "git-branch"
	flagGitRepoName   = "git-repo-name"
	flagGitRepoRoot   = "git-repo-root"
	flagPrevCommandID = "prev-command-id"
	flagHistoryPath   = "history-path"
	flagIfNotExists   = "if-not-exists"
	flagForce         = "force"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			os.Exit(0)
		}
	}()

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	cmd := os.Args[1]

	switch cmd {
	case "--persistent":
		runPersistent()
	case "session-start":
		runSessionStart()
	case "session-end":
		runSessionEnd()
	case "alias-sync":
		runAliasSync()
	case "log-start":
		runLogStart()
	case "log-end":
		runLogEnd()
	case "suggest":
		runSuggest()
	case "text-to-command":
		runTextToCommand()
	case "ping":
		runPing()
	case "status":
		runStatus()
	case "import-history":
		runImportHistory()
	case "version", "--version", "-v":
		printVersion()
	case "help", "--help", "-h":
		printUsage()
	default:
		printUsage()
	}

	// Always exit 0 for silent failure (defer above is only for panic recovery)
	os.Exit(0) //nolint:gocritic // exitAfterDefer: defer is for panic recovery only
}

func printVersion() {
	fmt.Printf("clai-shim %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate)
}

func printUsage() {
	const usage = `clai-shim - Thin client for clai daemon

Usage: clai-shim <command> [flags...]

Commands:
  --persistent                                Enter persistent NDJSON stdin mode
  session-start, session-end, alias-sync (--stdin), log-start, log-end, suggest, text-to-command
  import-history, ping, status, version, help

Environment:
  CLAI_SOCKET       Override daemon socket path
  CLAI_DAEMON_PATH  Override daemon binary path`
	fmt.Println(usage)
}

func parseFlags(args []string) map[string]string {
	result := make(map[string]string)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			if idx := strings.Index(key, "="); idx >= 0 {
				result[key[:idx]] = key[idx+1:]
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				result[key] = args[i+1]
				i++
			} else {
				result[key] = "true"
			}
		}
	}
	return result
}

// runPersistent enters persistent mode: reads NDJSON events from stdin
// and dispatches them to the daemon over a single long-lived gRPC connection.
// On connection loss, it retries with exponential backoff (100ms, 500ms).
// If reconnection fails, it falls back to oneshot mode (new connection per event).
// Up to 16 events are buffered in a ring buffer during temporary connection loss.
func runPersistent() {
	signal.Ignore(syscall.SIGPIPE)
	ctx, cancel := signalAwareContext()
	defer cancel()
	dialFn := shim.DefaultDialFunc(Version)
	runner := shim.NewRunner(dialFn, Version)
	_ = runner.Run(ctx, os.Stdin)
}

func signalAwareContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
}

func runSessionStart() {
	flags := parseFlags(os.Args[2:])
	sessionID := flags[flagSessionID]
	cwd := flags[flagCwd]
	shell := flags[flagShell]
	if sessionID == "" {
		return
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	client, err := ipc.NewClient()
	if err != nil {
		return
	}
	defer client.Close()
	info := ipc.DefaultClientInfo(Version)
	if shell != "" {
		info.Shell = shell
	}
	client.SessionStart(sessionID, cwd, info)
}

func runSessionEnd() {
	flags := parseFlags(os.Args[2:])
	sessionID := flags[flagSessionID]
	if sessionID == "" {
		return
	}
	client, err := ipc.NewClient()
	if err != nil {
		return
	}
	defer client.Close()
	client.SessionEnd(sessionID)
}

func runAliasSync() {
	flags := parseFlags(os.Args[2:])
	sessionID := flags[flagSessionID]
	shell := flags[flagShell]
	if sessionID == "" {
		return
	}
	rawSnapshot := ""
	if flags["stdin"] == "true" {
		data, err := io.ReadAll(os.Stdin)
		if err == nil {
			rawSnapshot = string(data)
		}
	}
	client, err := ipc.NewClient()
	if err != nil {
		return
	}
	defer client.Close()
	client.AliasSync(sessionID, shell, rawSnapshot)
}

func runLogStart() {
	flags := parseFlags(os.Args[2:])
	sessionID := flags[flagSessionID]
	commandID := flags[flagCommandID]
	cwd := flags[flagCwd]
	command := flags[flagCommand]
	if sessionID == "" || commandID == "" {
		return
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	client, err := ipc.NewClient()
	if err != nil {
		return
	}
	defer client.Close()
	cmdCtx := &ipc.CommandContext{
		GitBranch:     flags[flagGitBranch],
		GitRepoName:   flags[flagGitRepoName],
		GitRepoRoot:   flags[flagGitRepoRoot],
		PrevCommandID: flags[flagPrevCommandID],
	}
	client.LogStartWithContext(sessionID, commandID, cwd, command, cmdCtx)
}

func runLogEnd() {
	flags := parseFlags(os.Args[2:])
	sessionID := flags[flagSessionID]
	commandID := flags[flagCommandID]
	exitCodeStr := flags[flagExitCode]
	durationStr := flags[flagDuration]
	if sessionID == "" || commandID == "" {
		return
	}
	exitCode, _ := strconv.Atoi(exitCodeStr)
	durationMs, _ := strconv.ParseInt(durationStr, 10, 64)
	client, err := ipc.NewClient()
	if err != nil {
		return
	}
	defer client.Close()
	client.LogEnd(sessionID, commandID, exitCode, durationMs)
}

func runSuggest() {
	flags := parseFlags(os.Args[2:])
	sessionID := flags[flagSessionID]
	cwd := flags[flagCwd]
	buffer := flags[flagBuffer]
	cursorStr := flags[flagCursor]
	limitStr := flags["limit"]
	if sessionID == "" {
		if len(os.Args) >= 5 {
			sessionID = os.Args[2]
			cwd = os.Args[3]
			buffer = os.Args[4]
		}
	}
	if sessionID == "" {
		return
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cursorPos := len(buffer)
	if cursorStr != "" {
		cursorPos, _ = strconv.Atoi(cursorStr)
	}
	limit := 1
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	client, err := ipc.NewClient()
	if err != nil {
		return
	}
	defer client.Close()
	ctx, cancel := signalAwareContext()
	defer cancel()
	suggestions := client.Suggest(ctx, sessionID, cwd, buffer, cursorPos, false, limit)
	if len(suggestions) == 0 {
		return
	}
	for _, s := range suggestions {
		fmt.Println(s.Text)
	}
}

func runTextToCommand() {
	flags := parseFlags(os.Args[2:])
	sessionID := flags[flagSessionID]
	cwd := flags[flagCwd]
	prompt := flags[flagPrompt]
	if sessionID == "" {
		if len(os.Args) >= 5 {
			sessionID = os.Args[2]
			cwd = os.Args[3]
			prompt = os.Args[4]
		}
	}
	if sessionID == "" || prompt == "" {
		return
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	client, err := ipc.NewClient()
	if err != nil {
		return
	}
	defer client.Close()
	ctx, cancel := signalAwareContext()
	defer cancel()
	resp, err := client.TextToCommand(ctx, sessionID, prompt, cwd, 3)
	if err != nil || resp == nil || len(resp.Suggestions) == 0 {
		return
	}
	fmt.Println(resp.Suggestions[0].Text)
}

func runPing() {
	client, err := ipc.NewClient()
	if err != nil {
		fmt.Println("not connected")
		return
	}
	defer client.Close()
	if client.Ping() {
		fmt.Println("ok")
	} else {
		fmt.Println("not responding")
	}
}

func runStatus() {
	client, err := ipc.NewClient()
	if err != nil {
		fmt.Println(`{"error": "not connected"}`)
		return
	}
	defer client.Close()
	status, err := client.GetStatus()
	if err != nil {
		fmt.Println(`{"error": "failed to get status"}`)
		return
	}
	output := map[string]interface{}{
		"version":         status.Version,
		"active_sessions": status.ActiveSessions,
		"uptime_seconds":  status.UptimeSeconds,
		"commands_logged": status.CommandsLogged,
	}
	data, _ := json.Marshal(output)
	fmt.Println(string(data))
}

func runImportHistory() {
	flags := parseFlags(os.Args[2:])
	shell := flags[flagShell]
	historyPath := flags[flagHistoryPath]
	ifNotExists := flags[flagIfNotExists] == "true"
	force := flags[flagForce] == "true"
	if shell == "" {
		shell = "auto"
	}
	client, err := ipc.NewClient()
	if err != nil {
		fmt.Println(`{"error": "not connected"}`)
		return
	}
	defer client.Close()
	ctx, cancel := signalAwareContext()
	defer cancel()
	resp, err := client.ImportHistory(ctx, shell, historyPath, ifNotExists, force)
	if err != nil {
		output := map[string]interface{}{
			"error": err.Error(),
		}
		data, _ := json.Marshal(output)
		fmt.Println(string(data))
		return
	}
	output := map[string]interface{}{
		"imported_count": resp.ImportedCount,
		"skipped":        resp.Skipped,
	}
	if resp.Error != "" {
		output["error"] = resp.Error
	}
	data, _ := json.Marshal(output)
	fmt.Println(string(data))
}
