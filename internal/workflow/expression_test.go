package workflow

import (
	"testing"
)

func TestExpressionSingleEnv(t *testing.T) {
	ctx := &ExpressionContext{
		Env: map[string]string{"HOME": "/home/user"},
	}
	got, err := ResolveExpressions("${{ env.HOME }}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/home/user" {
		t.Errorf("got %q, want %q", got, "/home/user")
	}
}

func TestExpressionSingleMatrix(t *testing.T) {
	ctx := &ExpressionContext{
		Matrix: map[string]string{"region": "us-east-1"},
	}
	got, err := ResolveExpressions("${{ matrix.region }}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "us-east-1" {
		t.Errorf("got %q, want %q", got, "us-east-1")
	}
}

func TestExpressionStepOutput(t *testing.T) {
	ctx := &ExpressionContext{
		Steps: map[string]map[string]string{
			"build": {"artifact": "app.zip"},
		},
	}
	got, err := ResolveExpressions("${{ steps.build.outputs.artifact }}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "app.zip" {
		t.Errorf("got %q, want %q", got, "app.zip")
	}
}

func TestExpressionMultiple(t *testing.T) {
	ctx := &ExpressionContext{
		Env:    map[string]string{"NAME": "World"},
		Matrix: map[string]string{"region": "eu-west-1"},
	}
	got, err := ResolveExpressions("Hello ${{ env.NAME }} from ${{ matrix.region }}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Hello World from eu-west-1"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpressionUnresolved(t *testing.T) {
	ctx := &ExpressionContext{
		Env: map[string]string{},
	}
	_, err := ResolveExpressions("${{ env.MISSING }}", ctx)
	if err == nil {
		t.Fatal("expected error for unresolved expression")
	}
}

func TestExpressionEmpty(t *testing.T) {
	ctx := &ExpressionContext{}
	_, err := ResolveExpressions("${{  }}", ctx)
	if err == nil {
		t.Fatal("expected error for empty expression")
	}
}

func TestExpressionInvalidNamespace(t *testing.T) {
	ctx := &ExpressionContext{}
	_, err := ResolveExpressions("${{ secrets.TOKEN }}", ctx)
	if err == nil {
		t.Fatal("expected error for invalid namespace")
	}
}

func TestExpressionStepsMissingOutputKey(t *testing.T) {
	ctx := &ExpressionContext{
		Steps: map[string]map[string]string{
			"build": {"artifact": "app.zip"},
		},
	}
	_, err := ResolveExpressions("${{ steps.build.outputs.missing }}", ctx)
	if err == nil {
		t.Fatal("expected error for missing step output key")
	}
}

func TestExpressionNoExpressions(t *testing.T) {
	ctx := &ExpressionContext{}
	input := "plain string with no expressions"
	got, err := ResolveExpressions(input, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestExpressionNilContextWithExpressions(t *testing.T) {
	_, err := ResolveExpressions("${{ env.NAME }}", nil)
	if err == nil {
		t.Fatal("expected error for nil expression context")
	}
}

func TestExpressionWhitespaceHandling(t *testing.T) {
	ctx := &ExpressionContext{
		Env: map[string]string{"NAME": "trimmed"},
	}
	tests := []struct {
		name  string
		input string
	}{
		{"no spaces", "${{env.NAME}}"},
		{"single spaces", "${{ env.NAME }}"},
		{"extra spaces", "${{  env.NAME  }}"},
		{"tabs", "${{	env.NAME	}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveExpressions(tt.input, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != "trimmed" {
				t.Errorf("got %q, want %q", got, "trimmed")
			}
		})
	}
}

func TestExpressionInvalidFormat(t *testing.T) {
	ctx := &ExpressionContext{}
	// No dot separator — not namespace.key format.
	_, err := ResolveExpressions("${{ NODOT }}", ctx)
	if err == nil {
		t.Fatal("expected error for expression without namespace.key format")
	}
}

func TestExpressionStepsInvalidSegment(t *testing.T) {
	ctx := &ExpressionContext{
		Steps: map[string]map[string]string{
			"build": {"artifact": "app.zip"},
		},
	}
	_, err := ResolveExpressions("${{ steps.build.result.artifact }}", ctx)
	if err == nil {
		t.Fatal("expected error for invalid steps segment (not 'outputs')")
	}
}

func TestExpressionStepsTooFewParts(t *testing.T) {
	ctx := &ExpressionContext{
		Steps: map[string]map[string]string{
			"build": {"artifact": "app.zip"},
		},
	}
	_, err := ResolveExpressions("${{ steps.build }}", ctx)
	if err == nil {
		t.Fatal("expected error for steps expression with too few parts")
	}
}

func TestExpressionNilEnvMap(t *testing.T) {
	ctx := &ExpressionContext{}
	_, err := ResolveExpressions("${{ env.VAR }}", ctx)
	if err == nil {
		t.Fatal("expected error when Env map is nil")
	}
}

func TestExpressionNilMatrixMap(t *testing.T) {
	ctx := &ExpressionContext{}
	_, err := ResolveExpressions("${{ matrix.KEY }}", ctx)
	if err == nil {
		t.Fatal("expected error when Matrix map is nil")
	}
}

func TestExpressionNilStepsMap(t *testing.T) {
	ctx := &ExpressionContext{}
	_, err := ResolveExpressions("${{ steps.build.outputs.artifact }}", ctx)
	if err == nil {
		t.Fatal("expected error when Steps map is nil")
	}
}

func TestExpressionMissingStepID(t *testing.T) {
	ctx := &ExpressionContext{
		Steps: map[string]map[string]string{
			"deploy": {"url": "https://example.com"},
		},
	}
	_, err := ResolveExpressions("${{ steps.build.outputs.artifact }}", ctx)
	if err == nil {
		t.Fatal("expected error for missing step ID")
	}
}

func TestExpressionEmbeddedInText(t *testing.T) {
	ctx := &ExpressionContext{
		Env: map[string]string{"VERSION": "1.2.3"},
	}
	got, err := ResolveExpressions("deploy-${{ env.VERSION }}-prod", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "deploy-1.2.3-prod"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpressionEmptyValue(t *testing.T) {
	ctx := &ExpressionContext{
		Env: map[string]string{"EMPTY": ""},
	}
	got, err := ResolveExpressions("prefix-${{ env.EMPTY }}-suffix", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "prefix--suffix"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
