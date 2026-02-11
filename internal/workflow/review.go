package workflow

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ReviewDecision is the human's response to a review prompt.
type ReviewDecision struct {
	Action string // "approve", "reject", "inspect", "command", "question"
	Input  string // For "command" or "question": the user's input
}

// InteractionHandler presents analysis results and collects human decisions.
type InteractionHandler interface {
	// PromptReview shows the analysis and prompts for a decision.
	PromptReview(ctx context.Context, stepName string, analysis *AnalysisResult, output string) (*ReviewDecision, error)
}

// TerminalReviewer implements InteractionHandler for interactive terminals.
type TerminalReviewer struct {
	reader io.Reader
	writer io.Writer
}

// NewTerminalReviewer creates a reviewer that reads from reader and writes to writer.
func NewTerminalReviewer(reader io.Reader, writer io.Writer) *TerminalReviewer {
	return &TerminalReviewer{reader: reader, writer: writer}
}

// PromptReview displays the analysis and prompts the user for a decision.
func (t *TerminalReviewer) PromptReview(ctx context.Context, stepName string, analysis *AnalysisResult, output string) (*ReviewDecision, error) {
	fmt.Fprintf(t.writer, "Step: %s\n", stepName)
	fmt.Fprintf(t.writer, "Decision: %s\n", analysis.Decision)
	fmt.Fprintf(t.writer, "Reasoning: %s\n", analysis.Reasoning)

	scanner := bufio.NewScanner(t.reader)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		fmt.Fprint(t.writer, "[a]pprove  [r]eject  [i]nspect  [c]ommand  [q]uestion > ")

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			return nil, io.ErrUnexpectedEOF
		}

		choice := strings.TrimSpace(scanner.Text())
		switch choice {
		case "a":
			return &ReviewDecision{Action: "approve"}, nil
		case "r":
			return &ReviewDecision{Action: "reject"}, nil
		case "i":
			fmt.Fprintf(t.writer, "\n%s\n\n", output)
			continue
		case "c":
			fmt.Fprint(t.writer, "Command: ")
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					return nil, err
				}
				return nil, io.ErrUnexpectedEOF
			}
			return &ReviewDecision{Action: "command", Input: scanner.Text()}, nil
		case "q":
			fmt.Fprint(t.writer, "Question: ")
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					return nil, err
				}
				return nil, io.ErrUnexpectedEOF
			}
			return &ReviewDecision{Action: "question", Input: scanner.Text()}, nil
		default:
			continue
		}
	}
}

// ErrNonInteractive indicates a review was requested in non-interactive mode.
var ErrNonInteractive = errors.New("review required but running in non-interactive mode")

// NonInteractiveHandler returns reject for any review prompt.
type NonInteractiveHandler struct{}

// PromptReview always returns a reject decision with an error indicating non-interactive mode.
func (n *NonInteractiveHandler) PromptReview(_ context.Context, _ string, _ *AnalysisResult, _ string) (*ReviewDecision, error) {
	return &ReviewDecision{Action: "reject"}, ErrNonInteractive
}

// ErrScriptedExhausted indicates that the scripted handler has no more decisions.
var ErrScriptedExhausted = errors.New("scripted interaction handler: no more decisions")

// ScriptedInteractionHandler replays a fixed sequence of decisions for testing.
type ScriptedInteractionHandler struct {
	decisions []*ReviewDecision
	index     int
}

// NewScriptedHandler creates a handler that returns decisions in order.
func NewScriptedHandler(decisions ...*ReviewDecision) *ScriptedInteractionHandler {
	return &ScriptedInteractionHandler{decisions: decisions}
}

// PromptReview returns the next scripted decision, or an error if exhausted.
func (s *ScriptedInteractionHandler) PromptReview(_ context.Context, _ string, _ *AnalysisResult, _ string) (*ReviewDecision, error) {
	if s.index >= len(s.decisions) {
		return nil, ErrScriptedExhausted
	}
	d := s.decisions[s.index]
	s.index++
	return d, nil
}
