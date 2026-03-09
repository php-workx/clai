package workflow

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
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

// scanOrError reads the next line from the scanner, returning an error on failure.
func scanOrError(scanner *bufio.Scanner) (string, error) {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.ErrUnexpectedEOF
	}
	return scanner.Text(), nil
}

// readInputDecision prompts for additional input and returns a decision with that input.
func (t *TerminalReviewer) readInputDecision(scanner *bufio.Scanner, prompt, action string) (*ReviewDecision, error) {
	fmt.Fprint(t.writer, prompt)
	text, err := scanOrError(scanner)
	if err != nil {
		return nil, err
	}
	return &ReviewDecision{Action: action, Input: text}, nil
}

// PromptReview displays the analysis and prompts the user for a decision.
func (t *TerminalReviewer) PromptReview(ctx context.Context, stepName string, analysis *AnalysisResult, output string) (*ReviewDecision, error) {
	t.printReviewBlock(stepName, analysis)

	scanner := bufio.NewScanner(t.reader)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		fmt.Fprint(t.writer, "  [a]pprove  [r]eject  [i]nspect  [c]ommand  [q]uestion > ")

		choice, err := scanOrError(scanner)
		if err != nil {
			return nil, err
		}

		switch strings.TrimSpace(choice) {
		case "a":
			return &ReviewDecision{Action: string(ActionApprove)}, nil
		case "r":
			return &ReviewDecision{Action: string(ActionReject)}, nil
		case "i":
			fmt.Fprintf(t.writer, "\n%s\n\n", output)
		case "c":
			return t.readInputDecision(scanner, "Command: ", string(ActionCommand))
		case "q":
			return t.readInputDecision(scanner, "Question: ", string(ActionQuestion))
		}
	}
}

// printReviewBlock renders a formatted analysis review block.
func (t *TerminalReviewer) printReviewBlock(stepName string, analysis *AnalysisResult) {
	icon := iconForDecision(analysis.Decision)
	fmt.Fprintf(t.writer, "\n\u2500\u2500\u2500 Review: %s \u2500\u2500\u2500\n", stepName)
	fmt.Fprintf(t.writer, "  %s Decision: %s\n", icon, analysis.Decision)

	if reasoning := strings.TrimSpace(analysis.Reasoning); reasoning != "" {
		for _, line := range strings.Split(reasoning, "\n") {
			fmt.Fprintf(t.writer, "  \u2502 %s\n", line)
		}
	}

	if len(analysis.Flags) > 0 {
		flagParts := make([]string, 0, len(analysis.Flags))
		for k, v := range analysis.Flags {
			flagParts = append(flagParts, k+"="+v)
		}
		sort.Strings(flagParts)
		fmt.Fprintf(t.writer, "  \u2502 Flags: %s\n", strings.Join(flagParts, ", "))
	}

	fmt.Fprintln(t.writer)
}

// ErrNonInteractive indicates a review was requested in non-interactive mode.
var ErrNonInteractive = errors.New("review required but running in non-interactive mode")

// NonInteractiveHandler returns reject for any review prompt.
type NonInteractiveHandler struct{}

// PromptReview always returns a reject decision with an error indicating non-interactive mode.
func (n *NonInteractiveHandler) PromptReview(_ context.Context, _ string, _ *AnalysisResult, _ string) (*ReviewDecision, error) {
	return &ReviewDecision{Action: string(ActionReject)}, ErrNonInteractive
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
