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
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/runger/clai/internal/ipc"
)

// Version info - injected at build time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	// Silent exit on any panic
	defer func() {
		if r := recover(); r != nil {
			os.Exit(0)
		}
	}()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	cmd := os.Args[1]

	switch cmd {
	case "session-start":
		runSessionStart()
	case "session-end":
		runSessionEnd()
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
	case "version", "--version", "-v":
		printVersion()
	case "help", "--help", "-h":
		printUsage()
	default:
		printUsage()
	}

	// Always exit 0 for silent failure
	os.Exit(0)
}

func printVersion() {
	fmt.Printf("clai-shim %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate)
}

func printUsage() {
	fmt.Println(`clai-shim - Thin client for clai daemon

Usage: clai-shim <command> [flags...]

Commands:
  session-start --session-id=ID --cwd=PATH --shell=SHELL  Notify new shell session
  session-end --session-id=ID                              Notify session ending
  log-start --session-id=ID --command-id=ID --cwd=PATH --command="CMD"  Log command start
  log-end --session-id=ID --command-id=ID --exit-code=N --duration=MS   Log command end
  suggest --session-id=ID --cwd=PATH --buffer="TEXT" [--cursor=N]       Get suggestions
  text-to-command --session-id=ID --cwd=PATH --prompt="TEXT"            Text to command
  ping                                        Check daemon connectivity
  status                                      Get daemon status
  version                                     Print version info
  help                                        Print this help

Environment:
  CLAI_SOCKET       Override daemon socket path
  CLAI_DAEMON_PATH  Override daemon binary path`)
}

// parseFlags parses flag-style arguments (--key=value or --key value)
// Returns a map of key -> value
func parseFlags(args []string) map[string]string {
	result := make(map[string]string)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")
			if idx := strings.Index(key, "="); idx >= 0 {
				// --key=value format
				result[key[:idx]] = key[idx+1:]
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				// --key value format
				result[key] = args[i+1]
				i++
			} else {
				// --key (boolean flag)
				result[key] = "true"
			}
		}
	}
	return result
}

// runSessionStart handles the session-start command
// Flags: --session-id, --cwd, --shell
func runSessionStart() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags["session-id"]
	cwd := flags["cwd"]
	shell := flags["shell"]

	if sessionID == "" {
		return // Silent failure
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	info := ipc.DefaultClientInfo(Version)
	if shell != "" {
		info.Shell = shell
	}

	client.SessionStart(sessionID, cwd, info)
}

// runSessionEnd handles the session-end command
// Flags: --session-id
func runSessionEnd() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags["session-id"]
	if sessionID == "" {
		return // Silent failure
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	client.SessionEnd(sessionID)
}

// runLogStart handles the log-start command
// Flags: --session-id, --command-id, --cwd, --command
func runLogStart() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags["session-id"]
	commandID := flags["command-id"]
	cwd := flags["cwd"]
	command := flags["command"]

	if sessionID == "" || commandID == "" {
		return // Silent failure
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	client.LogStart(sessionID, commandID, cwd, command)
}

// runLogEnd handles the log-end command
// Flags: --session-id, --command-id, --exit-code, --duration
func runLogEnd() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags["session-id"]
	commandID := flags["command-id"]
	exitCodeStr := flags["exit-code"]
	durationStr := flags["duration"]

	if sessionID == "" || commandID == "" {
		return // Silent failure
	}

	exitCode, _ := strconv.Atoi(exitCodeStr)
	durationMs, _ := strconv.ParseInt(durationStr, 10, 64)

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	client.LogEnd(sessionID, commandID, exitCode, durationMs)
}

// runSuggest handles the suggest command
// Flags: --session-id, --cwd, --buffer, --cursor
// Output: JSON array of suggestions or single line for shell
func runSuggest() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags["session-id"]
	cwd := flags["cwd"]
	buffer := flags["buffer"]
	cursorStr := flags["cursor"]

	if sessionID == "" {
		// Try without flags for backwards compatibility
		if len(os.Args) >= 5 {
			sessionID = os.Args[2]
			cwd = os.Args[3]
			buffer = os.Args[4]
		}
	}

	if sessionID == "" {
		return // Silent failure
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	cursorPos := len(buffer)
	if cursorStr != "" {
		cursorPos, _ = strconv.Atoi(cursorStr)
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	suggestions := client.Suggest(context.Background(), sessionID, cwd, buffer, cursorPos, false, 5)
	if len(suggestions) == 0 {
		return
	}

	// Output first suggestion for shell integration
	fmt.Println(suggestions[0].Text)
}

// runTextToCommand handles the text-to-command command
// Flags: --session-id, --cwd, --prompt
// Output: First suggestion or JSON if multiple
func runTextToCommand() {
	flags := parseFlags(os.Args[2:])

	sessionID := flags["session-id"]
	cwd := flags["cwd"]
	prompt := flags["prompt"]

	if sessionID == "" {
		// Try without flags for backwards compatibility
		if len(os.Args) >= 5 {
			sessionID = os.Args[2]
			cwd = os.Args[3]
			prompt = os.Args[4]
		}
	}

	if sessionID == "" || prompt == "" {
		return // Silent failure
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	client, err := ipc.NewClient()
	if err != nil {
		return // Silent failure
	}
	defer client.Close()

	resp, err := client.TextToCommand(context.Background(), sessionID, prompt, cwd, 3)
	if err != nil || resp == nil || len(resp.Suggestions) == 0 {
		return
	}

	// Output first suggestion for shell integration
	fmt.Println(resp.Suggestions[0].Text)
}

// runPing checks daemon connectivity
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

// runStatus prints daemon status
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
