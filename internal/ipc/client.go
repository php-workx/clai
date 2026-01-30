package ipc

import (
	"context"
	"os"
	"runtime"
	"time"

	"google.golang.org/grpc"

	pb "github.com/runger/clai/gen/clai/v1"
)

// Client wraps the gRPC client with convenience methods and proper timeouts.
type Client struct {
	conn   *grpc.ClientConn
	client pb.ClaiServiceClient
}

// NewClient creates a new IPC client connected to the daemon.
// It will attempt to spawn the daemon if it's not running.
func NewClient() (*Client, error) {
	// Try to ensure daemon is running (ignore error, we'll try to connect anyway)
	_ = EnsureDaemon()

	conn, err := QuickDial()
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:   conn,
		client: pb.NewClaiServiceClient(conn),
	}, nil
}

// NewClientWithConn creates a client with an existing connection.
// Useful for testing with mock connections.
func NewClientWithConn(conn *grpc.ClientConn) *Client {
	return &Client{
		conn:   conn,
		client: pb.NewClaiServiceClient(conn),
	}
}

// Close closes the client connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// --- Session Lifecycle (Fire-and-Forget) ---

// SessionStart notifies the daemon of a new shell session.
// Uses fire-and-forget semantics - errors are silently ignored.
func (c *Client) SessionStart(sessionID, cwd string, info *ClientInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), FireAndForgetTimeout)
	defer cancel()

	req := &pb.SessionStartRequest{
		SessionId:       sessionID,
		Cwd:             cwd,
		StartedAtUnixMs: time.Now().UnixMilli(),
		Client:          info.toProto(),
	}

	// Fire and forget - ignore errors
	_, _ = c.client.SessionStart(ctx, req)
}

// SessionEnd notifies the daemon of a shell session ending.
// Uses fire-and-forget semantics - errors are silently ignored.
func (c *Client) SessionEnd(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), FireAndForgetTimeout)
	defer cancel()

	req := &pb.SessionEndRequest{
		SessionId:     sessionID,
		EndedAtUnixMs: time.Now().UnixMilli(),
	}

	// Fire and forget - ignore errors
	_, _ = c.client.SessionEnd(ctx, req)
}

// --- Command Lifecycle (Fire-and-Forget) ---

// LogStart logs the start of a command execution.
// Uses fire-and-forget semantics - errors are silently ignored.
func (c *Client) LogStart(sessionID, commandID, cwd, command string) {
	ctx, cancel := context.WithTimeout(context.Background(), FireAndForgetTimeout)
	defer cancel()

	req := &pb.CommandStartRequest{
		SessionId: sessionID,
		CommandId: commandID,
		TsUnixMs:  time.Now().UnixMilli(),
		Cwd:       cwd,
		Command:   command,
	}

	// Fire and forget - ignore errors
	_, _ = c.client.CommandStarted(ctx, req)
}

// LogEnd logs the completion of a command execution.
// Uses fire-and-forget semantics - errors are silently ignored.
func (c *Client) LogEnd(sessionID, commandID string, exitCode int, durationMs int64) {
	ctx, cancel := context.WithTimeout(context.Background(), FireAndForgetTimeout)
	defer cancel()

	req := &pb.CommandEndRequest{
		SessionId:  sessionID,
		CommandId:  commandID,
		TsUnixMs:   time.Now().UnixMilli(),
		ExitCode:   int32(exitCode),
		DurationMs: durationMs,
	}

	// Fire and forget - ignore errors
	_, _ = c.client.CommandEnded(ctx, req)
}

// --- Suggestions (With Timeout) ---

// Suggest requests command suggestions from the daemon.
// Returns suggestions or nil on timeout/error.
func (c *Client) Suggest(sessionID, cwd, buffer string, cursorPos int, includeAI bool, maxResults int) []*pb.Suggestion {
	ctx, cancel := context.WithTimeout(context.Background(), SuggestTimeout)
	defer cancel()

	if maxResults <= 0 {
		maxResults = 5
	}

	req := &pb.SuggestRequest{
		SessionId:  sessionID,
		Cwd:        cwd,
		Buffer:     buffer,
		CursorPos:  int32(cursorPos),
		IncludeAi:  includeAI,
		MaxResults: int32(maxResults),
	}

	resp, err := c.client.Suggest(ctx, req)
	if err != nil {
		return nil
	}

	return resp.Suggestions
}

// TextToCommand converts natural language to shell commands.
// Uses a longer timeout suitable for AI operations.
func (c *Client) TextToCommand(sessionID, prompt, cwd string, maxSuggestions int) (*pb.TextToCommandResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), InteractiveTimeout)
	defer cancel()

	if maxSuggestions <= 0 {
		maxSuggestions = 3
	}

	req := &pb.TextToCommandRequest{
		SessionId:      sessionID,
		Prompt:         prompt,
		Cwd:            cwd,
		MaxSuggestions: int32(maxSuggestions),
	}

	return c.client.TextToCommand(ctx, req)
}

// --- Ops ---

// Ping checks if the daemon is responsive.
func (c *Client) Ping() bool {
	ctx, cancel := context.WithTimeout(context.Background(), SuggestTimeout)
	defer cancel()

	resp, err := c.client.Ping(ctx, &pb.Ack{Ok: true})
	return err == nil && resp.Ok
}

// GetStatus retrieves daemon status information.
func (c *Client) GetStatus() (*pb.StatusResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), SuggestTimeout)
	defer cancel()

	return c.client.GetStatus(ctx, &pb.Ack{Ok: true})
}

// --- Helper Types ---

// ClientInfo contains information about the client environment.
type ClientInfo struct {
	Version  string
	OS       string
	Shell    string
	Hostname string
	Username string
}

// DefaultClientInfo returns a ClientInfo populated with current environment.
func DefaultClientInfo(version string) *ClientInfo {
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "unknown"
	}

	return &ClientInfo{
		Version:  version,
		OS:       runtime.GOOS,
		Shell:    shell,
		Hostname: hostname,
		Username: username,
	}
}

func (ci *ClientInfo) toProto() *pb.ClientInfo {
	if ci == nil {
		return nil
	}
	return &pb.ClientInfo{
		Version:  ci.Version,
		Os:       ci.OS,
		Shell:    ci.Shell,
		Hostname: ci.Hostname,
		Username: ci.Username,
	}
}
