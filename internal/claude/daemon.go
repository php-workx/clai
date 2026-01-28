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
	if val := os.Getenv("AI_TERMINAL_IDLE_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			return d
		}
	}
	return defaultIdleTimeout
}

// Daemon paths
func daemonDir() string {
	cacheDir := os.Getenv("AI_TERMINAL_CACHE")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache", "ai-terminal")
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
	os.MkdirAll(daemonDir(), 0755)

	// Start daemon process
	exe, err := os.Executable()
	if err != nil {
		exe = "ai-terminal"
	}

	// Create log file for daemon output
	logFile, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		logFile = nil
	}

	cmd := exec.Command(exe, "daemon", "run")
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

	// Write PID file
	os.WriteFile(pidPath(), []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644)

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

// RunDaemon runs the daemon server (called by "daemon run" command)
func RunDaemon() error {
	// Remove old socket if exists
	os.Remove(socketPath())

	fmt.Println("Starting Claude process...")

	// Start Claude process with an initial message to trigger initialization
	cmd := exec.Command("claude",
		"--print",
		"--verbose",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr // Log stderr for debugging

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to start claude: %w", err)
	}
	defer cmd.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	// Increase scanner buffer for large responses
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	fmt.Println("Sending init message to trigger Claude startup...")

	// Send an initial message to trigger Claude startup
	initMsg := StreamMessage{Type: "user"}
	initMsg.Message.Role = "user"
	initMsg.Message.Content = "Ready"

	initBytes, _ := json.Marshal(initMsg)
	if _, err := stdin.Write(append(initBytes, '\n')); err != nil {
		return fmt.Errorf("failed to send init message: %w", err)
	}

	fmt.Println("Waiting for Claude initialization...")

	// Wait for initialization by reading lines until we get the init message
	initialized := false
	for scanner.Scan() {
		line := scanner.Text()
		// Log first 200 chars of each line for debugging
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
			initialized = true
			fmt.Println("Claude initialized successfully")
			break
		}
	}

	if !initialized {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scanner error during init: %w", err)
		}
		return fmt.Errorf("claude initialization failed: unexpected end of stream")
	}

	// Now read through the init response until we get the result
	fmt.Println("Reading init response...")
	for scanner.Scan() {
		line := scanner.Text()
		var resp StreamResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Type == "result" {
			fmt.Println("Init response complete")
			break
		}
	}

	// Now create the socket - Claude is ready
	listener, err := net.Listen("unix", socketPath())
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath())

	// Create the claude process wrapper
	claude := &claudeProcess{
		cmd:     cmd,
		stdin:   stdin,
		scanner: scanner,
	}

	lastActivity := time.Now()
	var activityMu sync.Mutex

	// Idle timeout goroutine
	go func() {
		for {
			time.Sleep(30 * time.Second)
			activityMu.Lock()
			idle := time.Since(lastActivity)
			activityMu.Unlock()

			if idle > idleTimeout() {
				fmt.Println("Idle timeout, shutting down")
				listener.Close() // This will cause Accept to fail
				return
			}
		}
	}()

	fmt.Println("Daemon ready, accepting connections")

	// Handle connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Accept error: %v\n", err)
			return nil // Shutdown
		}

		// Handle connection synchronously to avoid scanner race conditions
		go func(c net.Conn) {
			defer c.Close()

			// Update activity
			activityMu.Lock()
			lastActivity = time.Now()
			activityMu.Unlock()

			// Read request
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

			// Process request with mutex to serialize Claude access
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

			// Send response
			json.NewEncoder(c).Encode(DaemonResponse{Result: result})
		}(conn)
	}
}

// query sends a prompt to Claude and returns the response
func (c *claudeProcess) query(prompt string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Send prompt to Claude
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

	// Read response
	var result string
	for c.scanner.Scan() {
		line := c.scanner.Text()

		var resp StreamResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}

		if resp.Type == "assistant" && len(resp.Message.Content) > 0 {
			for _, content := range resp.Message.Content {
				if content.Type == "text" {
					result += content.Text
				}
			}
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
