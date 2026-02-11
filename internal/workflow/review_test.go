package workflow

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

var testAnalysis = &AnalysisResult{
	Decision:  "proceed",
	Reasoning: "output looks correct",
	Flags:     map[string]string{"exit_code": "0"},
}

const testOutput = "line 1\nline 2\nline 3"

func TestReviewTerminalApprove(t *testing.T) {
	in := strings.NewReader("a\n")
	out := &bytes.Buffer{}
	r := NewTerminalReviewer(in, out)

	d, err := r.PromptReview(context.Background(), "build", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Action != "approve" {
		t.Fatalf("expected approve, got %s", d.Action)
	}

	if !strings.Contains(out.String(), "Step: build") {
		t.Errorf("output should contain step name, got: %s", out.String())
	}
	if !strings.Contains(out.String(), "Decision: proceed") {
		t.Errorf("output should contain decision, got: %s", out.String())
	}
}

func TestReviewTerminalReject(t *testing.T) {
	in := strings.NewReader("r\n")
	out := &bytes.Buffer{}
	r := NewTerminalReviewer(in, out)

	d, err := r.PromptReview(context.Background(), "deploy", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Action != "reject" {
		t.Fatalf("expected reject, got %s", d.Action)
	}
}

func TestReviewTerminalInspectThenApprove(t *testing.T) {
	in := strings.NewReader("i\na\n")
	out := &bytes.Buffer{}
	r := NewTerminalReviewer(in, out)

	d, err := r.PromptReview(context.Background(), "test", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Action != "approve" {
		t.Fatalf("expected approve after inspect, got %s", d.Action)
	}

	if !strings.Contains(out.String(), testOutput) {
		t.Errorf("inspect should print full output, got: %s", out.String())
	}
}

func TestReviewTerminalCommand(t *testing.T) {
	in := strings.NewReader("c\nls -la\n")
	out := &bytes.Buffer{}
	r := NewTerminalReviewer(in, out)

	d, err := r.PromptReview(context.Background(), "check", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Action != "command" {
		t.Fatalf("expected command, got %s", d.Action)
	}
	if d.Input != "ls -la" {
		t.Fatalf("expected 'ls -la', got %q", d.Input)
	}
}

func TestReviewTerminalQuestion(t *testing.T) {
	in := strings.NewReader("q\nwhy did it fail?\n")
	out := &bytes.Buffer{}
	r := NewTerminalReviewer(in, out)

	d, err := r.PromptReview(context.Background(), "analyze", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Action != "question" {
		t.Fatalf("expected question, got %s", d.Action)
	}
	if d.Input != "why did it fail?" {
		t.Fatalf("expected 'why did it fail?', got %q", d.Input)
	}
}

func TestReviewTerminalInvalidThenApprove(t *testing.T) {
	in := strings.NewReader("x\nz\na\n")
	out := &bytes.Buffer{}
	r := NewTerminalReviewer(in, out)

	d, err := r.PromptReview(context.Background(), "step1", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Action != "approve" {
		t.Fatalf("expected approve after invalid inputs, got %s", d.Action)
	}

	// Should have printed the prompt 3 times (two invalid + one valid).
	count := strings.Count(out.String(), "[a]pprove")
	if count != 3 {
		t.Errorf("expected 3 prompts, got %d", count)
	}
}

func TestReviewTerminalContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Provide no input so the context check triggers before scanning.
	in := strings.NewReader("")
	out := &bytes.Buffer{}
	r := NewTerminalReviewer(in, out)

	_, err := r.PromptReview(ctx, "step", testAnalysis, testOutput)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

func TestReviewNonInteractiveAlwaysRejects(t *testing.T) {
	h := &NonInteractiveHandler{}

	d, err := h.PromptReview(context.Background(), "step", testAnalysis, testOutput)
	if d.Action != "reject" {
		t.Fatalf("expected reject, got %s", d.Action)
	}
	if !errors.Is(err, ErrNonInteractive) {
		t.Fatalf("expected ErrNonInteractive, got %v", err)
	}
}

func TestReviewScriptedApprove(t *testing.T) {
	h := NewScriptedHandler(&ReviewDecision{Action: "approve"})

	d, err := h.PromptReview(context.Background(), "step", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Action != "approve" {
		t.Fatalf("expected approve, got %s", d.Action)
	}
}

func TestReviewScriptedSequence(t *testing.T) {
	h := NewScriptedHandler(
		&ReviewDecision{Action: "inspect"},
		&ReviewDecision{Action: "command", Input: "echo hello"},
		&ReviewDecision{Action: "approve"},
	)

	d1, err := h.PromptReview(context.Background(), "s1", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d1.Action != "inspect" {
		t.Fatalf("expected inspect, got %s", d1.Action)
	}

	d2, err := h.PromptReview(context.Background(), "s2", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d2.Action != "command" || d2.Input != "echo hello" {
		t.Fatalf("expected command 'echo hello', got %s %q", d2.Action, d2.Input)
	}

	d3, err := h.PromptReview(context.Background(), "s3", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d3.Action != "approve" {
		t.Fatalf("expected approve, got %s", d3.Action)
	}
}

func TestReviewScriptedExhausted(t *testing.T) {
	h := NewScriptedHandler(&ReviewDecision{Action: "approve"})

	_, err := h.PromptReview(context.Background(), "s1", testAnalysis, testOutput)
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}

	_, err = h.PromptReview(context.Background(), "s2", testAnalysis, testOutput)
	if !errors.Is(err, ErrScriptedExhausted) {
		t.Fatalf("expected ErrScriptedExhausted, got %v", err)
	}
}
