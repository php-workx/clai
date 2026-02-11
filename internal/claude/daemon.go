package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultIdleTimeout = 2 * time.Hour

func idleTimeout() time.Duration {
	if val := os.Getenv("CLAI_IDLE_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			return d
		}
	}
	return defaultIdleTimeout
}

// Daemon paths
func daemonDir() string {
	cacheDir := os.Getenv("CLAI_CACHE")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache", "clai")
	}
	return cacheDir
}

func socketPath() string {
	return filepath.Join(daemonDir(), "daemon.sock")
}

func pidPath() string {
	return filepath.Join(daemonDir(), "daemon.pid")
}

func logPath() string {
	return filepath.Join(daemonDir(), "daemon.log")
}

// StreamMessage represents a message in the stream-json format
type StreamMessage struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
}

// StreamResponse represents a response from Claude in stream-json format
type StreamResponse struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
	Result  string `json:"result,omitempty"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message,omitempty"`
}

// DaemonRequest is sent to the daemon over the socket
type DaemonRequest struct {
	Prompt string `json:"prompt"`
}

// DaemonResponse is received from the daemon
type DaemonResponse struct {
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// IsDaemonRunning checks if a daemon is already running
func IsDaemonRunning() bool {
	conn, err := net.Dial("unix", socketPath())
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// StartDaemonProcess starts the daemon as a background process
func StartDaemonProcess() error {
	if IsDaemonRunning() {
		return nil // Already running
	}

	// Ensure directory exists
	os.MkdirAll(daemonDir(), 0o755)

	// Start daemon process
	exe, err := os.Executable()
	if err != nil {
		exe = "clai"
	}

	// Create log file for daemon output
	logFile, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		logFile = nil
	}

	cmd := exec.Command(exe, "claude-daemon", "run") //nolint:gosec // G204: exe is our own binary path
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Detach from parent process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	if logFile != nil {
		// Child inherited the descriptor; close parent's copy to avoid leaking fds
		// across repeated start attempts.
		logFile.Close()
	}

	// Write PID file
	os.WriteFile(pidPath(), []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0o644)

	// Wait for socket to be available (up to 90 seconds for Claude init with hooks)
	for i := 0; i < 900; i++ {
		if IsDaemonRunning() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("daemon failed to start (check %s for details)", logPath())
}

// StopDaemon stops the running daemon
func StopDaemon() error {
	// Remove socket to signal shutdown
	os.Remove(socketPath())

	// Read and kill PID if exists
	pidData, err := os.ReadFile(pidPath())
	if err == nil {
		var pid int
		fmt.Sscanf(string(pidData), "%d", &pid)
		if pid > 0 {
			if proc, err := os.FindProcess(pid); err == nil {
				proc.Kill()
			}
		}
	}
	os.Remove(pidPath())
	return nil
}

// QueryViaDaemon sends a query to the daemon and returns the response
func QueryViaDaemon(ctx context.Context, prompt string) (string, error) {
	conn, err := net.Dial("unix", socketPath())
	if err != nil {
		return "", fmt.Errorf("daemon not running: %w", err)
	}
	defer conn.Close()

	// Set deadline based on context
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(60 * time.Second))
	}

	// Send request
	req := DaemonRequest{Prompt: prompt}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	var resp DaemonResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.Error != "" {
		return "", fmt.Errorf("daemon error: %s", resp.Error)
	}

	return resp.Result, nil
}

// QueryFast tries daemon first, falls back to direct CLI
func QueryFast(ctx context.Context, prompt string) (string, error) {
	// Try daemon first
	if IsDaemonRunning() {
		result, err := QueryViaDaemon(ctx, prompt)
		if err == nil {
			return result, nil
		}
		// Daemon failed, fall back
	}

	// Fall back to regular CLI
	return QueryWithContext(ctx, prompt)
}

// claudeProcess manages the Claude CLI process
type claudeProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
}

// startClaudeProcess starts the Claude CLI and waits for initialization
func startClaudeProcess() (*claudeProcess, error) {
	fmt.Println("Starting Claude process...")

	cmd := exec.Command("claude",
		"--print",
		"--verbose",
		"--model", "haiku",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	if err := sendInitMessage(stdin); err != nil {
		cmd.Process.Kill()
		return nil, err
	}

	if err := waitForInit(scanner); err != nil {
		cmd.Process.Kill()
		return nil, err
	}

	if err := waitForResult(scanner); err != nil {
		cmd.Process.Kill()
		return nil, err
	}

	return &claudeProcess{cmd: cmd, stdin: stdin, scanner: scanner}, nil
}

// sendInitMessage sends the initial message to trigger Claude startup
func sendInitMessage(stdin io.Writer) error {
	fmt.Println("Sending init message to trigger Claude startup...")

	msg := StreamMessage{Type: "user"}
	msg.Message.Role = "user"
	msg.Message.Content = "Ready"

	initBytes, _ := json.Marshal(msg)
	if _, err := stdin.Write(append(initBytes, '\n')); err != nil {
		return fmt.Errorf("failed to send init message: %w", err)
	}
	return nil
}

// waitForInit reads stream lines until the system init message is received
func waitForInit(scanner *bufio.Scanner) error {
	fmt.Println("Waiting for Claude initialization...")

	for scanner.Scan() {
		line := scanner.Text()
		logLine := line
		if len(logLine) > 200 {
			logLine = logLine[:200] + "..."
		}
		fmt.Printf("Init: %s\n", logLine)

		var resp StreamResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Type == "system" && resp.Subtype == "init" {
			fmt.Println("Claude initialized successfully")
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error during init: %w", err)
	}
	return fmt.Errorf("claude initialization failed: unexpected end of stream")
}

// waitForResult reads stream lines until the result message is received
func waitForResult(scanner *bufio.Scanner) error {
	fmt.Println("Reading init response...")

	for scanner.Scan() {
		line := scanner.Text()
		var resp StreamResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Type == "result" {
			fmt.Println("Init response complete")
			return nil
		}
	}
	return scanner.Err()
}

// handleDaemonConn handles a single client connection to the daemon
func handleDaemonConn(c net.Conn, claude *claudeProcess, activityMu *sync.Mutex, lastActivity *time.Time) {
	defer c.Close()

	activityMu.Lock()
	*lastActivity = time.Now()
	activityMu.Unlock()

	var req DaemonRequest
	if err := json.NewDecoder(c).Decode(&req); err != nil {
		json.NewEncoder(c).Encode(DaemonResponse{Error: err.Error()})
		return
	}

	logPrompt := req.Prompt
	if len(logPrompt) > 50 {
		logPrompt = logPrompt[:50] + "..."
	}
	fmt.Printf("Received request: %s\n", logPrompt)

	result, err := claude.query(req.Prompt)
	if err != nil {
		json.NewEncoder(c).Encode(DaemonResponse{Error: err.Error()})
		return
	}

	logResult := result
	if len(logResult) > 50 {
		logResult = logResult[:50] + "..."
	}
	fmt.Printf("Sending response: %s\n", logResult)

	json.NewEncoder(c).Encode(DaemonResponse{Result: result})
}

// RunDaemon runs the daemon server (called by "daemon run" command)
func RunDaemon() error {
	os.Remove(socketPath())

	claude, err := startClaudeProcess()
	if err != nil {
		return err
	}
	defer claude.cmd.Process.Kill()

	listener, err := net.Listen("unix", socketPath())
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath())

	lastActivity := time.Now()
	var activityMu sync.Mutex

	go func() {
		for {
			time.Sleep(30 * time.Second)
			activityMu.Lock()
			idle := time.Since(lastActivity)
			activityMu.Unlock()

			if idle > idleTimeout() {
				fmt.Println("Idle timeout, shutting down")
				listener.Close()
				return
			}
		}
	}()

	fmt.Println("Daemon ready, accepting connections")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Accept error: %v\n", err)
			return nil
		}

		go handleDaemonConn(conn, claude, &activityMu, &lastActivity)
	}
}

// extractResponseText extracts text content from a stream response message
func extractResponseText(resp StreamResponse) string {
	var text string
	for _, content := range resp.Message.Content {
		if content.Type == "text" {
			text += content.Text
		}
	}
	return text
}

// query sends a prompt to Claude and returns the response
func (c *claudeProcess) query(prompt string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	msg := StreamMessage{Type: "user"}
	msg.Message.Role = "user"
	msg.Message.Content = prompt

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message: %w", err)
	}

	if _, err := c.stdin.Write(append(msgBytes, '\n')); err != nil {
		return "", fmt.Errorf("failed to write to claude: %w", err)
	}

	var result string
	for c.scanner.Scan() {
		var resp StreamResponse
		if err := json.Unmarshal([]byte(c.scanner.Text()), &resp); err != nil {
			continue
		}

		if resp.Type == "assistant" {
			result += extractResponseText(resp)
		}

		if resp.Type == "result" {
			if resp.Result != "" {
				result = resp.Result
			}
			break
		}
	}

	if err := c.scanner.Err(); err != nil {
		return "", fmt.Errorf("scanner error: %w", err)
	}

	return strings.TrimSpace(result), nil
}
