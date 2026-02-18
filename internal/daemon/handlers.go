package daemon

import (
	"context"
	"fmt"
	"math"
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
	"github.com/runger/clai/internal/suggestions/alias"
	"github.com/runger/clai/internal/suggestions/backfill"
	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/feedback"
	"github.com/runger/clai/internal/suggestions/learning"
	snormalize "github.com/runger/clai/internal/suggestions/normalize"
	search2 "github.com/runger/clai/internal/suggestions/search"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
)

// Common string constants to avoid duplication
const (
	errNoAIProvider = "no AI provider available"
	sourceAI        = "ai"
	riskDestructive = "destructive"
)

func v1WhyNarrative(sug suggest.Suggestion, lastCmd string) string {
	// Prefer explaining something the user cannot infer from the numbers above.
	// Avoid embedding numeric values here; those are rendered as structured hints.

	cmdTool := suggest.GetToolPrefix(sug.Text)
	lastTool := suggest.GetToolPrefix(lastCmd)
	if cmdTool != "" && lastTool != "" && cmdTool == lastTool {
		return fmt.Sprintf("Same tool as your last command (%s).", cmdTool)
	}

	src := strings.TrimSpace(strings.ToLower(sug.Source))
	base := ""
	switch src {
	case "session":
		base = "From this session's history."
	case "cwd":
		base = "Often used in this directory."
	case "global":
		base = "From your global history."
	}

	total := sug.SuccessCount + sug.FailureCount
	if total >= 3 {
		rate := float64(sug.SuccessCount) / float64(total)
		reliability := ""
		switch {
		case rate >= 0.90:
			reliability = "It has been reliable."
		case rate >= 0.60:
			reliability = "It usually works."
		}

		if reliability != "" {
			if base == "" {
				base = reliability
			} else {
				base = base + " " + reliability
			}
		}
	}

	if base != "" {
		return base
	}

	// Last-resort fallback: keep it short and non-numeric.
	if sug.LastSeenUnixMs > 0 {
		return "Used recently."
	}
	return ""
}

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
	if s.projectDetector != nil {
		s.sessionManager.SetProjectTypes(req.SessionId, s.projectDetector.Detect(req.Cwd))
	}
	if aliases, err := alias.Capture(ctx, shell); err == nil {
		s.sessionManager.SetAliases(req.SessionId, aliases)
		if s.aliasStore != nil {
			_ = s.aliasStore.SaveAliases(ctx, req.SessionId, aliases)
		}
	}

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

// AliasSync handles alias snapshot sync from shell integrations.
func (s *Server) AliasSync(ctx context.Context, req *pb.AliasSyncRequest) (*pb.Ack, error) {
	s.touchActivity()
	if req.SessionId == "" {
		return &pb.Ack{Ok: false, Error: "missing session_id"}, nil
	}

	var aliases alias.AliasMap
	if strings.TrimSpace(req.RawSnapshot) != "" {
		aliases = alias.ParseSnapshot(req.Shell, req.RawSnapshot)
	} else {
		captured, err := alias.Capture(ctx, req.Shell)
		if err != nil {
			s.logger.Debug("alias sync capture failed", "session_id", req.SessionId, "error", err)
			return &pb.Ack{Ok: false, Error: err.Error()}, nil
		}
		aliases = captured
	}

	s.sessionManager.SetAliases(req.SessionId, aliases)
	if s.aliasStore != nil {
		if err := s.aliasStore.SaveAliases(ctx, req.SessionId, aliases); err != nil {
			s.logger.Debug("alias sync persist failed", "session_id", req.SessionId, "error", err)
		}
	}
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
		if s.projectDetector != nil {
			s.sessionManager.SetProjectTypes(req.SessionId, s.projectDetector.Detect(req.Cwd))
		}
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
	if alias.ShouldResnapshot(req.Command) {
		if info, ok := s.sessionManager.Get(req.SessionId); ok {
			if aliases, err := alias.Capture(ctx, info.Shell); err == nil {
				s.sessionManager.SetAliases(req.SessionId, aliases)
				if s.aliasStore != nil {
					_ = s.aliasStore.SaveAliases(ctx, req.SessionId, aliases)
				}
			}
		}
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

	// Feed V2 batch writer (async, non-blocking)
	if s.batchWriter != nil {
		if info, ok := s.sessionManager.Get(req.SessionId); ok {
			pre := snormalize.PreNormalize(info.LastCmdRaw, snormalize.PreNormConfig{
				Aliases: info.Aliases,
			})
			s.sessionManager.SetLastTemplateID(req.SessionId, pre.TemplateID)
			durationMs := req.DurationMs
			ev := &event.CommandEvent{
				Version:      event.EventVersion,
				Type:         event.EventTypeCommandEnd,
				SessionID:    req.SessionId,
				Shell:        event.Shell(info.Shell),
				Cwd:          info.LastCmdCWD,
				CmdRaw:       info.LastCmdRaw,
				RepoKey:      info.LastGitRepo,
				Branch:       info.LastGitBranch,
				ProjectTypes: append([]string(nil), info.ProjectTypes...),
				Aliases:      info.Aliases,
				ExitCode:     int(req.ExitCode),
				DurationMs:   &durationMs,
				Ts:           tsEnd.UnixMilli(),
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
	start := time.Now()

	maxResults := int(req.MaxResults)
	if maxResults <= 0 {
		maxResults = 5
	}

	resp := s.suggestV2(ctx, req, maxResults)
	if resp == nil {
		fallback := s.suggestV1(ctx, req, maxResults)
		fallback.CacheStatus = "miss"
		fallback.LatencyMs = time.Since(start).Milliseconds()
		return fallback, nil
	}
	resp.CacheStatus = "miss"
	resp.LatencyMs = time.Since(start).Milliseconds()
	return resp, nil
}

// suggestV1 generates suggestions using the V1 ranker (history-based).
func (s *Server) suggestV1(ctx context.Context, req *pb.SuggestRequest, maxResults int) *pb.SuggestResponse {
	nowMs := time.Now().UnixMilli()
	lastCommand := s.lastCommandForSession(ctx, req.SessionId)
	suggestions, err := s.rankV1Suggestions(ctx, req, maxResults, lastCommand)
	if err != nil {
		s.logger.Warn("failed to rank suggestions",
			"session_id", req.SessionId,
			"error", err,
		)
		return &pb.SuggestResponse{}
	}

	// Convert to protobuf
	pbSuggestions := make([]*pb.Suggestion, len(suggestions))
	for i := range suggestions {
		pbSuggestions[i] = v1SuggestionToProto(suggestions[i], lastCommand, nowMs)
	}

	return &pb.SuggestResponse{
		Suggestions: pbSuggestions,
		FromCache:   false,
	}
}

func (s *Server) lastCommandForSession(ctx context.Context, sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	cmds, err := s.store.QueryCommands(ctx, storage.CommandQuery{
		SessionID: &sessionID,
		Limit:     1,
	})
	if err != nil || len(cmds) == 0 {
		return ""
	}
	return cmds[0].Command
}

func (s *Server) rankV1Suggestions(
	ctx context.Context,
	req *pb.SuggestRequest,
	maxResults int,
	lastCommand string,
) ([]suggest.Suggestion, error) {
	return s.ranker.Rank(ctx, &suggest.RankRequest{
		SessionID:   req.SessionId,
		CWD:         req.Cwd,
		Prefix:      req.Buffer,
		LastCommand: lastCommand,
		MaxResults:  maxResults,
	})
}

func v1SuggestionToProto(sug suggest.Suggestion, lastCommand string, nowMs int64) *pb.Suggestion {
	desc := strings.TrimSpace(sug.Description)
	if desc == "" {
		desc = v1WhyNarrative(sug, lastCommand)
	}
	cmdNorm := strings.TrimSpace(sug.CmdNorm)
	if cmdNorm == "" {
		cmdNorm = suggest.NormalizeCommand(sug.Text)
	}
	return &pb.Suggestion{
		Text:        sug.Text,
		Description: desc,
		Source:      sug.Source,
		Score:       sug.Score,
		Risk:        v1SuggestionRisk(sug.Text),
		CmdNorm:     cmdNorm,
		Confidence:  0, // V1 ranker does not compute a separate confidence score.
		Reasons:     v1SuggestionReasons(sug, nowMs),
	}
}

func v1SuggestionRisk(text string) string {
	if sanitize.IsDestructive(text) {
		return riskDestructive
	}
	return ""
}

func v1SuggestionReasons(sug suggest.Suggestion, nowMs int64) []*pb.SuggestionReason {
	reasons := make([]*pb.SuggestionReason, 0, len(sug.Reasons)+3)
	reasons = append(reasons, v1BaseReasons(sug)...)
	if sug.LastSeenUnixMs > 0 {
		reasons = append(reasons, &pb.SuggestionReason{
			Type:        "recency",
			Description: fmt.Sprintf("last %s ago", formatAgo(nowMs-sug.LastSeenUnixMs)),
		})
	}
	totalRuns := sug.SuccessCount + sug.FailureCount
	if totalRuns > 0 {
		reasons = append(reasons, &pb.SuggestionReason{
			Type:        "frequency",
			Description: fmt.Sprintf("freq %d", totalRuns),
		})
		successPct := int((float64(sug.SuccessCount) / float64(totalRuns)) * 100.0)
		reasons = append(reasons, &pb.SuggestionReason{
			Type:        "success",
			Description: fmt.Sprintf("success %d%% (%d/%d)", successPct, sug.SuccessCount, totalRuns),
		})
	}
	return reasons
}

func v1BaseReasons(sug suggest.Suggestion) []*pb.SuggestionReason {
	reasons := make([]*pb.SuggestionReason, 0, len(sug.Reasons))
	for _, r := range sug.Reasons {
		typ := strings.TrimSpace(r.Type)
		if typ == "" {
			continue
		}
		reasons = append(reasons, &pb.SuggestionReason{
			Type:         typ,
			Description:  r.Description,
			Contribution: float32(r.Contribution),
		})
	}
	return reasons
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
	return s.RecordFeedback(ctx, req)
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
	s.applyFeedbackUpdates(ctx, req)

	return &pb.RecordFeedbackResponse{Ok: true}, nil
}

func (s *Server) applyFeedbackUpdates(ctx context.Context, req *pb.RecordFeedbackRequest) {
	snapshot, ok := s.getSuggestSnapshot(req.SessionId)
	if !ok {
		return
	}
	nowMs := time.Now().UnixMilli()
	scope := snapshot.Context.Scope
	if scope == "" {
		if snapshot.Context.RepoKey != "" {
			scope = snapshot.Context.RepoKey
		} else {
			scope = "global"
		}
	}

	if s.dismissalStore != nil && snapshot.Context.LastTemplateID != "" {
		dismissedTemplateID := req.SuggestedText
		if sug, found := findSnapshotSuggestion(snapshot.Suggestions, req.SuggestedText); found && sug.TemplateID != "" {
			dismissedTemplateID = sug.TemplateID
		}
		switch req.Action {
		case "dismissed":
			_ = s.dismissalStore.RecordDismissal(ctx, scope, snapshot.Context.LastTemplateID, dismissedTemplateID, nowMs)
		case "accepted", "edited":
			_ = s.dismissalStore.RecordAcceptance(ctx, scope, snapshot.Context.LastTemplateID, dismissedTemplateID)
		case "never":
			_ = s.dismissalStore.RecordNever(ctx, scope, snapshot.Context.LastTemplateID, dismissedTemplateID, nowMs)
		case "unblock":
			_ = s.dismissalStore.RecordUnblock(ctx, scope, snapshot.Context.LastTemplateID, dismissedTemplateID)
		}
	}

	if s.learner == nil {
		return
	}
	pos, neg, ok := learningPairFromFeedback(snapshot.Suggestions, req)
	if !ok {
		return
	}
	s.learner.Update(ctx, scope, featureVectorFromSuggestion(pos, req), featureVectorFromSuggestion(neg, req))
}

func (s *Server) getSuggestSnapshot(sessionID string) (suggestSnapshot, bool) {
	s.snapshotMu.RLock()
	defer s.snapshotMu.RUnlock()
	snap, ok := s.lastSuggestSnapshots[sessionID]
	return snap, ok
}

func findSnapshotSuggestion(suggestions []suggest2.Suggestion, text string) (suggest2.Suggestion, bool) {
	for i := range suggestions {
		if suggestions[i].Command == text {
			return suggestions[i], true
		}
	}
	return suggest2.Suggestion{}, false
}

func learningPairFromFeedback(suggestions []suggest2.Suggestion, req *pb.RecordFeedbackRequest) (suggest2.Suggestion, suggest2.Suggestion, bool) {
	target, found := findSnapshotSuggestion(suggestions, req.SuggestedText)
	if !found || len(suggestions) == 0 {
		return suggest2.Suggestion{}, suggest2.Suggestion{}, false
	}
	var bestOther suggest2.Suggestion
	hasOther := false
	for i := range suggestions {
		if suggestions[i].Command == req.SuggestedText {
			continue
		}
		if !hasOther || suggestions[i].Score > bestOther.Score {
			bestOther = suggestions[i]
			hasOther = true
		}
	}
	if !hasOther {
		return suggest2.Suggestion{}, suggest2.Suggestion{}, false
	}

	switch req.Action {
	case "accepted", "edited":
		return target, bestOther, true
	case "dismissed", "never":
		return bestOther, target, true
	default:
		return suggest2.Suggestion{}, suggest2.Suggestion{}, false
	}
}

func featureVectorFromSuggestion(sug suggest2.Suggestion, req *pb.RecordFeedbackRequest) learning.FeatureVector {
	b := sug.ScoreBreakdown()
	prefix := 0.0
	if req.Prefix != "" && strings.HasPrefix(sug.Command, req.Prefix) {
		prefix = 1.0
	}
	riskPenalty := 0.0
	if sanitize.IsDestructive(sug.Command) {
		riskPenalty = 1.0
	}
	return learning.FeatureVector{
		Transition:          clamp01((b.RepoTransition + b.GlobalTransition + b.DirTransition) / 100.0),
		Frequency:           clamp01((b.RepoFrequency + b.GlobalFrequency + b.DirFrequency) / 100.0),
		Success:             0.5,
		Prefix:              prefix,
		Affinity:            clamp01((b.DirTransition + b.DirFrequency) / 100.0),
		Task:                clamp01(b.ProjectTask / 50.0),
		Feedback:            1.0,
		ProjectTypeAffinity: clamp01(b.ProjectTask / 50.0),
		FailureRecovery:     clamp01(b.RecoveryBoost / 50.0),
		RiskPenalty:         riskPenalty,
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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
//
//nolint:cyclop // Handler combines mode routing and legacy fallback paths.
func (s *Server) FetchHistory(ctx context.Context, req *pb.HistoryFetchRequest) (*pb.HistoryFetchResponse, error) {
	s.touchActivity()
	start := time.Now()

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}

	offset := int(req.Offset)
	if offset < 0 {
		offset = 0
	}

	usesSessionScope := strings.EqualFold(req.Scope, "session") || (!req.Global && req.SessionId != "")
	if s.v2db != nil && req.Query != "" && !usesSessionScope &&
		(req.Mode == pb.SearchMode_SEARCH_MODE_FTS ||
			req.Mode == pb.SearchMode_SEARCH_MODE_DESCRIBE ||
			req.Mode == pb.SearchMode_SEARCH_MODE_AUTO) {
		items, atEnd, backend := s.fetchHistoryV2Search(ctx, req, limit)
		return &pb.HistoryFetchResponse{
			Items:     items,
			AtEnd:     atEnd,
			LatencyMs: time.Since(start).Milliseconds(),
			Backend:   backend,
		}, nil
	}

	q := storage.CommandQuery{
		Limit:  limit + 1, // Fetch one extra to determine at_end
		Offset: offset,
		// Deduplicate by raw command string for pickers; do not collapse by command_norm.
		Deduplicate: true,
	}

	switch req.Mode {
	case pb.SearchMode_SEARCH_MODE_PREFIX:
		if req.Query != "" {
			q.Prefix = strings.ToLower(req.Query)
		}
	default:
		// Apply substring filter (normalize to lowercase for command_norm matching)
		if req.Query != "" {
			q.Substring = strings.ToLower(req.Query)
		}
	}

	// Scope handling: explicit scope overrides legacy global/session behavior.
	switch strings.ToLower(req.Scope) {
	case "session":
		if req.SessionId != "" {
			q.SessionID = &req.SessionId
		}
	case "global":
		// no filter
	default:
		// Apply session scoping
		if !req.Global && req.SessionId != "" {
			q.SessionID = &req.SessionId
		}
	}

	rows, err := s.store.QueryHistoryCommands(ctx, q)
	if err != nil {
		s.logger.Warn("failed to query history",
			"error", err,
		)
		return &pb.HistoryFetchResponse{
			LatencyMs: time.Since(start).Milliseconds(),
			Backend:   "storage",
		}, nil
	}

	atEnd := len(rows) <= limit
	if !atEnd {
		rows = rows[:limit]
	}

	items := make([]*pb.HistoryItem, len(rows))
	for i, row := range rows {
		cmd := stripANSI(row.Command)
		items[i] = &pb.HistoryItem{
			Command:     cmd,
			TimestampMs: row.TimestampMs,
			CmdNorm:     suggest.NormalizeCommand(cmd),
			RepoKey:     req.RepoKey,
		}
	}

	backend := "storage"
	if req.Mode == pb.SearchMode_SEARCH_MODE_PREFIX {
		backend = "prefix"
	}

	return &pb.HistoryFetchResponse{
		Items:     items,
		AtEnd:     atEnd,
		LatencyMs: time.Since(start).Milliseconds(),
		Backend:   backend,
	}, nil
}

func (s *Server) fetchHistoryV2Search(
	ctx context.Context,
	req *pb.HistoryFetchRequest,
	limit int,
) ([]*pb.HistoryItem, bool, string) {
	if s.v2db == nil || limit <= 0 {
		return nil, true, "storage"
	}

	opts := search2.SearchOptions{
		RepoKey: req.RepoKey,
		Limit:   limit + 1,
	}

	ftsSvc, err := search2.NewService(s.v2db.DB(), search2.Config{
		Logger:         s.logger,
		EnableFallback: true,
	})
	if err != nil {
		s.logger.Debug("history search init failed", "error", err)
		return nil, true, "storage"
	}
	defer ftsSvc.Close()

	describeSvc := search2.NewDescribeService(s.v2db.DB(), search2.DescribeConfig{Logger: s.logger})

	var (
		results []search2.SearchResult
		backend string
	)

	switch req.Mode {
	case pb.SearchMode_SEARCH_MODE_FTS:
		results, err = ftsSvc.Search(ctx, req.Query, opts)
		backend = "fts5"
	case pb.SearchMode_SEARCH_MODE_DESCRIBE:
		results, err = describeSvc.Search(ctx, req.Query, opts)
		backend = "describe"
	default:
		autoSvc := search2.NewAutoService(ftsSvc, describeSvc, search2.DefaultAutoConfig())
		results, err = autoSvc.Search(ctx, req.Query, opts)
		backend = "auto"
	}
	if err != nil {
		s.logger.Debug("history search failed", "error", err, "mode", req.Mode.String())
		return nil, true, "storage"
	}

	atEnd := len(results) <= limit
	if !atEnd {
		results = results[:limit]
	}

	items := make([]*pb.HistoryItem, 0, len(results))
	for i := range results {
		r := results[i]
		items = append(items, &pb.HistoryItem{
			Command:     stripANSI(r.CmdRaw),
			TimestampMs: r.Timestamp,
			CmdNorm:     r.CmdNorm,
			RepoKey:     r.RepoKey,
			RankScore:   r.Score,
			Tags:        append([]string(nil), r.Tags...),
			MatchedTags: append([]string(nil), r.MatchedTags...),
		})
	}
	return items, atEnd, backend
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
