package daemon

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"time"

	pb "github.com/runger/clai/gen/clai/v1"
	"github.com/runger/clai/internal/history"
	"github.com/runger/clai/internal/provider"
	"github.com/runger/clai/internal/sanitize"
	"github.com/runger/clai/internal/storage"
	"github.com/runger/clai/internal/suggest"
	"github.com/runger/clai/internal/suggestions/backfill"
	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/feedback"
)

// Common string constants to avoid duplication
const (
	errNoAIProvider = "no AI provider available"
	sourceAI        = "ai"
	riskDestructive = "destructive"
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
		IsSuccess:     boolPtr(true), // Assume success until CommandEnded
	}

	// Add git context if provided
	if req.GitBranch != "" {
		cmd.GitBranch = &req.GitBranch
	}
	if req.GitRepoName != "" {
		cmd.GitRepoName = &req.GitRepoName
	}
	if req.GitRepoRoot != "" {
		cmd.GitRepoRoot = &req.GitRepoRoot
	}
	if req.PrevCommandId != "" {
		cmd.PrevCommandID = &req.PrevCommandId
	}

	if err := s.store.CreateCommand(ctx, cmd); err != nil {
		s.logger.Warn("failed to create command",
			"command_id", req.CommandId,
			"session_id", req.SessionId,
			"error", err,
		)
		return &pb.Ack{Ok: false, Error: err.Error()}, nil
	}

	// Stash command data in session for V2 pipeline (CommandEnded reads it back)
	s.sessionManager.StashCommand(req.SessionId, req.CommandId, req.Command, req.Cwd, req.GitRepoName, req.GitRepoRoot, req.GitBranch)

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

	// Feed V2 batch writer (async, non-blocking)
	if s.batchWriter != nil {
		if info, ok := s.sessionManager.Get(req.SessionId); ok {
			durationMs := req.DurationMs
			ev := &event.CommandEvent{
				Version:    event.EventVersion,
				Type:       event.EventTypeCommandEnd,
				SessionID:  req.SessionId,
				Shell:      event.Shell(info.Shell),
				Cwd:        info.LastCmdCWD,
				CmdRaw:     info.LastCmdRaw,
				RepoKey:    info.LastGitRepo,
				Branch:     info.LastGitBranch,
				ExitCode:   int(req.ExitCode),
				DurationMs: &durationMs,
				Ts:         time.Now().UnixMilli(),
			}
			s.batchWriter.Enqueue(ev)
		}
	}

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
// The scorer version (v1/v2/blend) determines which scoring engine is used.
func (s *Server) Suggest(ctx context.Context, req *pb.SuggestRequest) (*pb.SuggestResponse, error) {
	s.touchActivity()

	maxResults := int(req.MaxResults)
	if maxResults <= 0 {
		maxResults = 5
	}

	// V2-only mode: skip V1 entirely
	if s.scorerVersion == "v2" {
		if resp := s.suggestV2(ctx, req, maxResults); resp != nil {
			return resp, nil
		}
		// V2 failed or unavailable; fall through to V1
		s.logger.Debug("v2 scorer unavailable, falling back to v1")
	}

	// V1 scoring path
	v1Resp := s.suggestV1(ctx, req, maxResults)

	// Blend mode: merge V1 + V2
	if s.scorerVersion == "blend" {
		return s.suggestBlend(ctx, req, maxResults, v1Resp), nil
	}

	return v1Resp, nil
}

// suggestV1 generates suggestions using the V1 ranker (history-based).
func (s *Server) suggestV1(ctx context.Context, req *pb.SuggestRequest, maxResults int) *pb.SuggestResponse {
	nowMs := time.Now().UnixMilli()

	// Get the last command from the session for affinity scoring
	lastCommand := ""
	sessionID := req.SessionId
	cmds, err := s.store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		Limit:     1,
	})
	if err == nil && len(cmds) > 0 {
		lastCommand = cmds[0].Command
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
		return &pb.SuggestResponse{}
	}

	// Convert to protobuf
	pbSuggestions := make([]*pb.Suggestion, len(suggestions))
	for i, sug := range suggestions {
		risk := ""
		if sanitize.IsDestructive(sug.Text) {
			risk = riskDestructive
		}

		// Best-effort enriched reasons for explainability and richer UI.
		pbReasons := make([]*pb.SuggestionReason, 0, len(sug.Reasons)+4)
		for _, r := range sug.Reasons {
			typ := strings.TrimSpace(r.Type)
			if typ == "" {
				continue
			}
			pbReasons = append(pbReasons, &pb.SuggestionReason{
				Type:         typ,
				Description:  r.Description,
				Contribution: float32(r.Contribution),
			})
		}

		// Add simple recency/frequency hints. These are not strictly score features,
		// but they help users understand why something was suggested.
		if sug.LastSeenUnixMs > 0 {
			pbReasons = append(pbReasons, &pb.SuggestionReason{
				Type:        "recency",
				Description: fmt.Sprintf("last %s ago", formatAgo(nowMs-sug.LastSeenUnixMs)),
			})
		}
		totalRuns := sug.SuccessCount + sug.FailureCount
		if totalRuns > 0 {
			pbReasons = append(pbReasons, &pb.SuggestionReason{
				Type:        "frequency",
				Description: fmt.Sprintf("freq %d", totalRuns),
			})
			successPct := int((float64(sug.SuccessCount) / float64(totalRuns)) * 100.0)
			pbReasons = append(pbReasons, &pb.SuggestionReason{
				Type:        "success",
				Description: fmt.Sprintf("success %d%% (%d/%d)", successPct, sug.SuccessCount, totalRuns),
			})
		}

		// Prefer a short, stable "why" description for the UI.
		desc := strings.TrimSpace(sug.Description)
		if desc == "" {
			var whyParts []string
			if sug.LastSeenUnixMs > 0 {
				whyParts = append(whyParts, fmt.Sprintf("last %s ago", formatAgo(nowMs-sug.LastSeenUnixMs)))
			}
			if totalRuns > 0 {
				whyParts = append(whyParts, fmt.Sprintf("freq %d", totalRuns))
				successPct := int((float64(sug.SuccessCount) / float64(totalRuns)) * 100.0)
				whyParts = append(whyParts, fmt.Sprintf("success %d%%", successPct))
			}
			if len(whyParts) > 0 {
				desc = strings.Join(whyParts, "; ")
			}
		}

		cmdNorm := strings.TrimSpace(sug.CmdNorm)
		if cmdNorm == "" {
			cmdNorm = suggest.NormalizeCommand(sug.Text)
		}

		pbSuggestions[i] = &pb.Suggestion{
			Text:        sug.Text,
			Description: desc,
			Source:      sug.Source,
			Score:       sug.Score,
			Risk:        risk,
			CmdNorm:     cmdNorm,
			Confidence:  0, // V1 ranker does not compute a separate confidence score.
			Reasons:     pbReasons,
		}
	}

	return &pb.SuggestResponse{
		Suggestions: pbSuggestions,
		FromCache:   false,
	}
}

// TextToCommand handles the TextToCommand RPC.
// It converts natural language to shell commands using AI.
func (s *Server) TextToCommand(ctx context.Context, req *pb.TextToCommandRequest) (*pb.TextToCommandResponse, error) {
	s.touchActivity()

	// Get the best available provider
	prov, err := s.registry.GetBest()
	if err != nil {
		s.logger.Warn(errNoAIProvider, "error", err)
		return &pb.TextToCommandResponse{}, nil
	}

	// Get session info for context
	osName, shell := s.getSessionContext(req.SessionId)

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
			risk = riskDestructive
		}
		pbSuggestions[i] = &pb.Suggestion{
			Text:        sug.Text,
			Description: sug.Description,
			Source:      sourceAI,
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
		s.logger.Warn(errNoAIProvider, "error", err)
		return &pb.NextStepResponse{}, nil
	}

	// Get session info for context
	osName, shell := s.getSessionContext(req.SessionId)

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
			risk = riskDestructive
		}
		pbSuggestions[i] = &pb.Suggestion{
			Text:        sug.Text,
			Description: sug.Description,
			Source:      sourceAI,
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
		s.logger.Warn(errNoAIProvider, "error", err)
		return &pb.DiagnoseResponse{
			Explanation: "No AI provider available for diagnosis",
		}, nil
	}

	// Get session info for context
	osName, shell := s.getSessionContext(req.SessionId)

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
			risk = riskDestructive
		}
		pbFixes[i] = &pb.Suggestion{
			Text:        sug.Text,
			Description: sug.Description,
			Source:      sourceAI,
			Score:       sug.Score,
			Risk:        risk,
		}
	}

	return &pb.DiagnoseResponse{
		Explanation: resp.Explanation,
		Fixes:       pbFixes,
	}, nil
}

// RecordFeedback handles the RecordFeedback RPC.
// It records user feedback on a suggestion.
func (s *Server) RecordFeedback(ctx context.Context, req *pb.RecordFeedbackRequest) (*pb.RecordFeedbackResponse, error) {
	s.touchActivity()
	return s.handleRecordFeedback(ctx, req)
}

// SuggestFeedback handles the SuggestFeedback RPC.
// It records user feedback on a suggestion (alias for RecordFeedback).
func (s *Server) SuggestFeedback(ctx context.Context, req *pb.RecordFeedbackRequest) (*pb.RecordFeedbackResponse, error) {
	s.touchActivity()
	return s.handleRecordFeedback(ctx, req)
}

// handleRecordFeedback is the shared implementation for RecordFeedback and SuggestFeedback.
func (s *Server) handleRecordFeedback(ctx context.Context, req *pb.RecordFeedbackRequest) (*pb.RecordFeedbackResponse, error) {
	if s.feedbackStore == nil {
		return &pb.RecordFeedbackResponse{
			Ok: false,
			Error: &pb.ApiError{
				Code:    "E_NO_FEEDBACK_STORE",
				Message: "feedback store not configured",
			},
		}, nil
	}

	if req.SessionId == "" {
		return &pb.RecordFeedbackResponse{
			Ok: false,
			Error: &pb.ApiError{
				Code:    "E_INVALID_REQUEST",
				Message: "session_id is required",
			},
		}, nil
	}
	if req.SuggestedText == "" {
		return &pb.RecordFeedbackResponse{
			Ok: false,
			Error: &pb.ApiError{
				Code:    "E_INVALID_REQUEST",
				Message: "suggested_text is required",
			},
		}, nil
	}
	if req.Action == "" {
		return &pb.RecordFeedbackResponse{
			Ok: false,
			Error: &pb.ApiError{
				Code:    "E_INVALID_REQUEST",
				Message: "action is required",
			},
		}, nil
	}

	rec := feedback.FeedbackRecord{
		SessionID:     req.SessionId,
		SuggestedText: req.SuggestedText,
		Action:        feedback.FeedbackAction(req.Action),
		ExecutedText:  req.ExecutedText,
		PromptPrefix:  req.Prefix,
		LatencyMs:     req.LatencyMs,
	}

	_, err := s.feedbackStore.RecordFeedback(ctx, rec)
	if err != nil {
		s.logger.Warn("failed to record feedback",
			"session_id", req.SessionId,
			"action", req.Action,
			"error", err,
		)
		return &pb.RecordFeedbackResponse{
			Ok: false,
			Error: &pb.ApiError{
				Code:    "E_STORE_ERROR",
				Message: err.Error(),
			},
		}, nil
	}

	s.logger.Debug("feedback recorded",
		"session_id", req.SessionId,
		"action", req.Action,
	)

	return &pb.RecordFeedbackResponse{Ok: true}, nil
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

// ansiRegexp matches ANSI escape sequences for stripping from command text.
var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

// FetchHistory handles the FetchHistory RPC.
// It returns paginated, deduplicated command history with optional substring filtering.
func (s *Server) FetchHistory(ctx context.Context, req *pb.HistoryFetchRequest) (*pb.HistoryFetchResponse, error) {
	s.touchActivity()

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}

	offset := int(req.Offset)
	if offset < 0 {
		offset = 0
	}

	q := storage.CommandQuery{
		Limit:  limit + 1, // Fetch one extra to determine at_end
		Offset: offset,
		// Deduplicate by raw command string for pickers; do not collapse by command_norm.
		Deduplicate: true,
	}

	// Apply substring filter (normalize to lowercase for command_norm matching)
	if req.Query != "" {
		q.Substring = strings.ToLower(req.Query)
	}

	// Apply session scoping
	if !req.Global && req.SessionId != "" {
		q.SessionID = &req.SessionId
	}

	rows, err := s.store.QueryHistoryCommands(ctx, q)
	if err != nil {
		s.logger.Warn("failed to query history",
			"error", err,
		)
		return &pb.HistoryFetchResponse{}, nil
	}

	atEnd := len(rows) <= limit
	if !atEnd {
		rows = rows[:limit]
	}

	items := make([]*pb.HistoryItem, len(rows))
	for i, row := range rows {
		items[i] = &pb.HistoryItem{
			Command:     stripANSI(row.Command),
			TimestampMs: row.TimestampMs,
		}
	}

	return &pb.HistoryFetchResponse{
		Items: items,
		AtEnd: atEnd,
	}, nil
}

// ImportHistory handles the ImportHistory RPC.
// It imports shell history entries from the specified shell's history file.
// The operation runs synchronously (caller should invoke asynchronously if needed).
func (s *Server) ImportHistory(ctx context.Context, req *pb.HistoryImportRequest) (*pb.HistoryImportResponse, error) {
	s.touchActivity()

	// Resolve shell type
	shell := req.Shell
	if shell == "" || shell == "auto" {
		shell = history.DetectShell()
	}
	if shell == "" {
		return &pb.HistoryImportResponse{
			Error: "could not detect shell type",
		}, nil
	}

	// Check if already imported (if_not_exists mode)
	if req.IfNotExists {
		has, err := s.store.HasImportedHistory(ctx, shell)
		if err != nil {
			return nil, fmt.Errorf("failed to check import status: %w", err)
		}
		if has {
			s.logger.Debug("import skipped: already imported",
				"shell", shell,
			)
			return &pb.HistoryImportResponse{
				Skipped: true,
			}, nil
		}
	}

	// Import shell history
	var entries []history.ImportEntry
	var err error
	switch shell {
	case "bash":
		entries, err = history.ImportBashHistory(req.HistoryPath)
	case "zsh":
		entries, err = history.ImportZshHistory(req.HistoryPath)
	case "fish":
		entries, err = history.ImportFishHistory(req.HistoryPath)
	default:
		return &pb.HistoryImportResponse{
			Error: "unsupported shell: " + shell,
		}, nil
	}

	if err != nil {
		s.logger.Warn("failed to read shell history",
			"shell", shell,
			"path", req.HistoryPath,
			"error", err,
		)
		return nil, fmt.Errorf("failed to read shell history: %w", err)
	}

	if len(entries) == 0 {
		s.logger.Debug("no history entries to import",
			"shell", shell,
		)
		return &pb.HistoryImportResponse{
			ImportedCount: 0,
		}, nil
	}

	// Import into database
	count, err := s.store.ImportHistory(ctx, entries, shell)
	if err != nil {
		s.logger.Warn("failed to import history",
			"shell", shell,
			"error", err,
		)
		return nil, fmt.Errorf("failed to import history: %w", err)
	}

	s.logger.Info("imported shell history",
		"shell", shell,
		"count", count,
	)

	// Seed V2 suggestions tables (non-fatal)
	if s.v2db != nil {
		if err := backfill.Seed(ctx, s.v2db.DB(), entries, shell); err != nil {
			s.logger.Warn("V2 backfill failed (non-fatal)", "error", err)
		}
	}

	return &pb.HistoryImportResponse{
		ImportedCount: int32(count),
	}, nil
}

// truncate truncates a string to the given length with "..." suffix.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// getSessionContext retrieves OS and shell information from a session.
// Returns runtime.GOOS and "bash" as defaults if session not found.
func (s *Server) getSessionContext(sessionID string) (osName, shell string) {
	osName = runtime.GOOS
	shell = "bash"
	if info, ok := s.sessionManager.Get(sessionID); ok {
		if info.OS != "" {
			osName = info.OS
		}
		if info.Shell != "" {
			shell = info.Shell
		}
	}
	return osName, shell
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}
