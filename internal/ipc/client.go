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

// CommandContext contains optional context for a command execution.
type CommandContext struct {
	GitBranch     string // Current git branch
	GitRepoName   string // Git repo basename
	GitRepoRoot   string // Git repo full path
	PrevCommandID string // Previous command ID in session
}

// LogStart logs the start of a command execution.
// Uses fire-and-forget semantics - errors are silently ignored.
func (c *Client) LogStart(sessionID, commandID, cwd, command string) {
	c.LogStartWithContext(sessionID, commandID, cwd, command, nil)
}

// LogStartWithContext logs the start of a command execution with additional context.
// Uses fire-and-forget semantics - errors are silently ignored.
func (c *Client) LogStartWithContext(sessionID, commandID, cwd, command string, ctx_info *CommandContext) {
	ctx, cancel := context.WithTimeout(context.Background(), FireAndForgetTimeout)
	defer cancel()

	req := &pb.CommandStartRequest{
		SessionId: sessionID,
		CommandId: commandID,
		TsUnixMs:  time.Now().UnixMilli(),
		Cwd:       cwd,
		Command:   command,
	}

	// Add optional context if provided
	if ctx_info != nil {
		req.GitBranch = ctx_info.GitBranch
		req.GitRepoName = ctx_info.GitRepoName
		req.GitRepoRoot = ctx_info.GitRepoRoot
		req.PrevCommandId = ctx_info.PrevCommandID
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
// The provided context is used for cancellation; a timeout is applied internally.
func (c *Client) Suggest(ctx context.Context, sessionID, cwd, buffer string, cursorPos int, includeAI bool, maxResults int) []*pb.Suggestion {
	ctx, cancel := context.WithTimeout(ctx, SuggestTimeout)
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
// The provided context is used for cancellation; a timeout is applied internally.
func (c *Client) TextToCommand(ctx context.Context, sessionID, prompt, cwd string, maxSuggestions int) (*pb.TextToCommandResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, InteractiveTimeout)
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

// --- Feedback (Fire-and-Forget + Sync) ---

// RecordFeedback sends suggestion feedback to the daemon (fire-and-forget).
func (c *Client) RecordFeedback(sessionID, action, suggestedText, executedText, prefix string, latencyMs int64) {
	ctx, cancel := context.WithTimeout(context.Background(), FireAndForgetTimeout)
	defer cancel()

	req := &pb.RecordFeedbackRequest{
		SessionId:     sessionID,
		Action:        action,
		SuggestedText: suggestedText,
		ExecutedText:  executedText,
		Prefix:        prefix,
		LatencyMs:     latencyMs,
	}

	// Fire and forget - ignore errors
	_, _ = c.client.RecordFeedback(ctx, req)
}

// RecordFeedbackSync sends suggestion feedback to the daemon and waits for a response.
func (c *Client) RecordFeedbackSync(ctx context.Context, sessionID, action, suggestedText, executedText, prefix string, latencyMs int64) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, SuggestTimeout)
	defer cancel()

	req := &pb.RecordFeedbackRequest{
		SessionId:     sessionID,
		Action:        action,
		SuggestedText: suggestedText,
		ExecutedText:  executedText,
		Prefix:        prefix,
		LatencyMs:     latencyMs,
	}

	resp, err := c.client.RecordFeedback(ctx, req)
	if err != nil {
		return false, err
	}
	return resp.Ok, nil
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

// ImportHistoryResponse contains the result of an import operation.
type ImportHistoryResponse struct {
	ImportedCount int
	Skipped       bool
	Error         string
}

// ImportHistory imports shell history into the daemon's database.
// Shell can be "bash", "zsh", "fish", or "auto" (detect from SHELL env).
// If ifNotExists is true, skip if history was already imported for this shell.
func (c *Client) ImportHistory(ctx context.Context, shell, historyPath string, ifNotExists, force bool) (*ImportHistoryResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, InteractiveTimeout*2) // Longer timeout for import
	defer cancel()

	req := &pb.HistoryImportRequest{
		Shell:       shell,
		HistoryPath: historyPath,
		IfNotExists: ifNotExists,
		Force:       force,
	}

	resp, err := c.client.ImportHistory(ctx, req)
	if err != nil {
		return nil, err
	}

	return &ImportHistoryResponse{
		ImportedCount: int(resp.ImportedCount),
		Skipped:       resp.Skipped,
		Error:         resp.Error,
	}, nil
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
