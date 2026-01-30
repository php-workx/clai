package daemon

import (
	"context"
	"runtime"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/provider"
	"github.com/runger/clai/internal/sanitize"
	"github.com/runger/clai/internal/storage"
	"github.com/runger/clai/internal/suggest"
)

// SessionStart handles the SessionStart RPC.
// It creates a new session in the database and registers it with the session manager.
func (s *Server) SessionStart(ctx context.Context, req *pb.SessionStartRequest) (*pb.Ack, error) {
	s.touchActivity()

	shell := ""
	osName := runtime.GOOS
	hostname := ""
	username := ""
	if req.Client != nil {
		shell = req.Client.Shell
		if req.Client.Os != "" {
			osName = req.Client.Os
		}
		hostname = req.Client.Hostname
		username = req.Client.Username
	}

	startedAt := time.Now()
	if req.StartedAtUnixMs > 0 {
		startedAt = time.UnixMilli(req.StartedAtUnixMs)
	}

	// Create session in database
	session := &storage.Session{
		SessionID:       req.SessionId,
		StartedAtUnixMs: startedAt.UnixMilli(),
		Shell:           shell,
		OS:              osName,
		Hostname:        hostname,
		Username:        username,
		InitialCWD:      req.Cwd,
	}

	if err := s.store.CreateSession(ctx, session); err != nil {
		s.logger.Warn("failed to create session",
			"session_id", req.SessionId,
			"error", err,
		)
		return &pb.Ack{Ok: false, Error: err.Error()}, nil
	}

	// Register with session manager
	s.sessionManager.Start(req.SessionId, shell, osName, hostname, username, req.Cwd, startedAt)

	s.logger.Debug("session started",
		"session_id", req.SessionId,
		"shell", shell,
		"cwd", req.Cwd,
	)

	return &pb.Ack{Ok: true}, nil
}

// SessionEnd handles the SessionEnd RPC.
// It marks the session as ended in the database and removes it from the session manager.
func (s *Server) SessionEnd(ctx context.Context, req *pb.SessionEndRequest) (*pb.Ack, error) {
	s.touchActivity()

	endedAt := time.Now()
	if req.EndedAtUnixMs > 0 {
		endedAt = time.UnixMilli(req.EndedAtUnixMs)
	}

	// Update session in database
	if err := s.store.EndSession(ctx, req.SessionId, endedAt.UnixMilli()); err != nil {
		s.logger.Warn("failed to end session",
			"session_id", req.SessionId,
			"error", err,
		)
		return &pb.Ack{Ok: false, Error: err.Error()}, nil
	}

	// Remove from session manager
	s.sessionManager.End(req.SessionId)

	s.logger.Debug("session ended", "session_id", req.SessionId)

	return &pb.Ack{Ok: true}, nil
}

// CommandStarted handles the CommandStarted RPC.
// It logs the start of a command execution.
func (s *Server) CommandStarted(ctx context.Context, req *pb.CommandStartRequest) (*pb.Ack, error) {
	s.touchActivity()
	s.sessionManager.Touch(req.SessionId)

	// Update CWD if provided
	if req.Cwd != "" {
		s.sessionManager.UpdateCWD(req.SessionId, req.Cwd)
	}

	tsStart := time.Now()
	if req.TsUnixMs > 0 {
		tsStart = time.UnixMilli(req.TsUnixMs)
	}

	// Create command in database
	cmd := &storage.Command{
		CommandID:     req.CommandId,
		SessionID:     req.SessionId,
		TsStartUnixMs: tsStart.UnixMilli(),
		CWD:           req.Cwd,
		Command:       req.Command,
		CommandNorm:   suggest.Normalize(req.Command),
		CommandHash:   suggest.Hash(req.Command),
		IsSuccess:     true, // Assume success until CommandEnded
	}

	if err := s.store.CreateCommand(ctx, cmd); err != nil {
		s.logger.Warn("failed to create command",
			"command_id", req.CommandId,
			"session_id", req.SessionId,
			"error", err,
		)
		return &pb.Ack{Ok: false, Error: err.Error()}, nil
	}

	s.logger.Debug("command started",
		"command_id", req.CommandId,
		"session_id", req.SessionId,
		"command", truncate(req.Command, 50),
	)

	return &pb.Ack{Ok: true}, nil
}

// CommandEnded handles the CommandEnded RPC.
// It logs the end of a command execution with exit code and duration.
func (s *Server) CommandEnded(ctx context.Context, req *pb.CommandEndRequest) (*pb.Ack, error) {
	s.touchActivity()
	s.sessionManager.Touch(req.SessionId)

	tsEnd := time.Now()
	if req.TsUnixMs > 0 {
		tsEnd = time.UnixMilli(req.TsUnixMs)
	}

	// Update command in database
	if err := s.store.UpdateCommandEnd(ctx, req.CommandId, int(req.ExitCode), tsEnd.UnixMilli(), req.DurationMs); err != nil {
		s.logger.Warn("failed to update command end",
			"command_id", req.CommandId,
			"session_id", req.SessionId,
			"error", err,
		)
		return &pb.Ack{Ok: false, Error: err.Error()}, nil
	}

	s.incrementCommandsLogged()

	s.logger.Debug("command ended",
		"command_id", req.CommandId,
		"session_id", req.SessionId,
		"exit_code", req.ExitCode,
		"duration_ms", req.DurationMs,
	)

	return &pb.Ack{Ok: true}, nil
}

// Suggest handles the Suggest RPC.
// It returns command suggestions based on history and optionally AI.
func (s *Server) Suggest(ctx context.Context, req *pb.SuggestRequest) (*pb.SuggestResponse, error) {
	s.touchActivity()

	maxResults := int(req.MaxResults)
	if maxResults <= 0 {
		maxResults = 5
	}

	// Get the last command from the session for affinity scoring
	lastCommand := ""
	if info, ok := s.sessionManager.Get(req.SessionId); ok {
		// Could query the last command from DB, but for now use empty
		_ = info
	}

	// Rank suggestions from history
	rankReq := &suggest.RankRequest{
		SessionID:   req.SessionId,
		CWD:         req.Cwd,
		Prefix:      req.Buffer,
		LastCommand: lastCommand,
		MaxResults:  maxResults,
	}

	suggestions, err := s.ranker.Rank(ctx, rankReq)
	if err != nil {
		s.logger.Warn("failed to rank suggestions",
			"session_id", req.SessionId,
			"error", err,
		)
		return &pb.SuggestResponse{}, nil
	}

	// Convert to protobuf
	pbSuggestions := make([]*pb.Suggestion, len(suggestions))
	for i, sug := range suggestions {
		risk := ""
		if sanitize.IsDestructive(sug.Text) {
			risk = "destructive"
		}
		pbSuggestions[i] = &pb.Suggestion{
			Text:        sug.Text,
			Description: sug.Description,
			Source:      sug.Source,
			Score:       sug.Score,
			Risk:        risk,
		}
	}

	return &pb.SuggestResponse{
		Suggestions: pbSuggestions,
		FromCache:   false,
	}, nil
}

// TextToCommand handles the TextToCommand RPC.
// It converts natural language to shell commands using AI.
func (s *Server) TextToCommand(ctx context.Context, req *pb.TextToCommandRequest) (*pb.TextToCommandResponse, error) {
	s.touchActivity()

	// Get the best available provider
	prov, err := s.registry.GetBest()
	if err != nil {
		s.logger.Warn("no AI provider available", "error", err)
		return &pb.TextToCommandResponse{}, nil
	}

	// Get session info for context
	osName := runtime.GOOS
	shell := "bash"
	if info, ok := s.sessionManager.Get(req.SessionId); ok {
		if info.OS != "" {
			osName = info.OS
		}
		if info.Shell != "" {
			shell = info.Shell
		}
	}

	// Build AI request
	aiReq := &provider.TextToCommandRequest{
		Prompt: req.Prompt,
		CWD:    req.Cwd,
		OS:     osName,
		Shell:  shell,
	}

	// Call AI provider
	start := time.Now()
	resp, err := prov.TextToCommand(ctx, aiReq)
	if err != nil {
		s.logger.Warn("AI text-to-command failed",
			"provider", prov.Name(),
			"error", err,
		)
		return &pb.TextToCommandResponse{}, nil
	}

	latency := time.Since(start).Milliseconds()

	// Convert to protobuf
	pbSuggestions := make([]*pb.Suggestion, len(resp.Suggestions))
	for i, sug := range resp.Suggestions {
		risk := ""
		if sanitize.IsDestructive(sug.Text) {
			risk = "destructive"
		}
		pbSuggestions[i] = &pb.Suggestion{
			Text:        sug.Text,
			Description: sug.Description,
			Source:      "ai",
			Score:       sug.Score,
			Risk:        risk,
		}
	}

	return &pb.TextToCommandResponse{
		Suggestions: pbSuggestions,
		Provider:    prov.Name(),
		LatencyMs:   latency,
	}, nil
}

// NextStep handles the NextStep RPC.
// It predicts the next command based on the last command and exit code.
func (s *Server) NextStep(ctx context.Context, req *pb.NextStepRequest) (*pb.NextStepResponse, error) {
	s.touchActivity()

	// Get the best available provider
	prov, err := s.registry.GetBest()
	if err != nil {
		s.logger.Warn("no AI provider available", "error", err)
		return &pb.NextStepResponse{}, nil
	}

	// Get session info for context
	osName := runtime.GOOS
	shell := "bash"
	if info, ok := s.sessionManager.Get(req.SessionId); ok {
		if info.OS != "" {
			osName = info.OS
		}
		if info.Shell != "" {
			shell = info.Shell
		}
	}

	// Build AI request
	aiReq := &provider.NextStepRequest{
		SessionID:    req.SessionId,
		LastCommand:  req.LastCommand,
		LastExitCode: int(req.LastExitCode),
		CWD:          req.Cwd,
		OS:           osName,
		Shell:        shell,
	}

	// Call AI provider
	resp, err := prov.NextStep(ctx, aiReq)
	if err != nil {
		s.logger.Warn("AI next-step failed",
			"provider", prov.Name(),
			"error", err,
		)
		return &pb.NextStepResponse{}, nil
	}

	// Convert to protobuf
	pbSuggestions := make([]*pb.Suggestion, len(resp.Suggestions))
	for i, sug := range resp.Suggestions {
		risk := ""
		if sanitize.IsDestructive(sug.Text) {
			risk = "destructive"
		}
		pbSuggestions[i] = &pb.Suggestion{
			Text:        sug.Text,
			Description: sug.Description,
			Source:      "ai",
			Score:       sug.Score,
			Risk:        risk,
		}
	}

	return &pb.NextStepResponse{
		Suggestions: pbSuggestions,
	}, nil
}

// Diagnose handles the Diagnose RPC.
// It analyzes a failed command and suggests fixes using AI.
func (s *Server) Diagnose(ctx context.Context, req *pb.DiagnoseRequest) (*pb.DiagnoseResponse, error) {
	s.touchActivity()

	// Get the best available provider
	prov, err := s.registry.GetBest()
	if err != nil {
		s.logger.Warn("no AI provider available", "error", err)
		return &pb.DiagnoseResponse{
			Explanation: "No AI provider available for diagnosis",
		}, nil
	}

	// Get session info for context
	osName := runtime.GOOS
	shell := "bash"
	if info, ok := s.sessionManager.Get(req.SessionId); ok {
		if info.OS != "" {
			osName = info.OS
		}
		if info.Shell != "" {
			shell = info.Shell
		}
	}

	// Build AI request
	aiReq := &provider.DiagnoseRequest{
		SessionID: req.SessionId,
		Command:   req.Command,
		ExitCode:  int(req.ExitCode),
		CWD:       req.Cwd,
		OS:        osName,
		Shell:     shell,
	}

	// Call AI provider
	resp, err := prov.Diagnose(ctx, aiReq)
	if err != nil {
		s.logger.Warn("AI diagnose failed",
			"provider", prov.Name(),
			"error", err,
		)
		return &pb.DiagnoseResponse{
			Explanation: "Failed to get diagnosis from AI provider",
		}, nil
	}

	// Convert to protobuf
	pbFixes := make([]*pb.Suggestion, len(resp.Fixes))
	for i, sug := range resp.Fixes {
		risk := ""
		if sanitize.IsDestructive(sug.Text) {
			risk = "destructive"
		}
		pbFixes[i] = &pb.Suggestion{
			Text:        sug.Text,
			Description: sug.Description,
			Source:      "ai",
			Score:       sug.Score,
			Risk:        risk,
		}
	}

	return &pb.DiagnoseResponse{
		Explanation: resp.Explanation,
		Fixes:       pbFixes,
	}, nil
}

// Ping handles the Ping RPC.
// It returns a simple acknowledgment to verify the daemon is running.
func (s *Server) Ping(ctx context.Context, req *pb.Ack) (*pb.Ack, error) {
	s.touchActivity()
	return &pb.Ack{Ok: true}, nil
}

// GetStatus handles the GetStatus RPC.
// It returns the current status of the daemon.
func (s *Server) GetStatus(ctx context.Context, req *pb.Ack) (*pb.StatusResponse, error) {
	s.touchActivity()

	uptime := time.Since(s.startTime).Seconds()

	return &pb.StatusResponse{
		Version:        Version,
		ActiveSessions: int32(s.sessionManager.ActiveCount()),
		UptimeSeconds:  int64(uptime),
		CommandsLogged: s.getCommandsLogged(),
	}, nil
}

// truncate truncates a string to the given length with "..." suffix.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
