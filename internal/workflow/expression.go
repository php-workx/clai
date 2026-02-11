package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// ExpressionContext holds the resolution context for ${{ }} expressions.
type ExpressionContext struct {
	Env    map[string]string            // env.VAR
	Matrix map[string]string            // matrix.KEY
	Steps  map[string]map[string]string // steps.ID.outputs.KEY
}

// exprPattern matches ${{ ... }} expressions. The inner group captures the content
// between delimiters. It uses a non-greedy match to handle multiple expressions.
var exprPattern = regexp.MustCompile(`\$\{\{(.*?)\}\}`)

// nestedPattern detects ${{ inside an already-captured expression body.
var nestedPattern = regexp.MustCompile(`\$\{\{`)

// ResolveExpressions replaces all ${{ ... }} expressions in the input string.
// Returns error if any expression cannot be resolved.
func ResolveExpressions(input string, ctx *ExpressionContext) (string, error) {
	// Fast path: no expressions at all.
	if !strings.Contains(input, "${{") {
		return input, nil
	}

	var resolveErr error
	result := exprPattern.ReplaceAllStringFunc(input, func(match string) string {
		if resolveErr != nil {
			return match
		}

		// Extract inner content (strip ${{ and }}).
		inner := match[3 : len(match)-2]

		// Check for nested expressions.
		if nestedPattern.MatchString(inner) {
			resolveErr = fmt.Errorf("nested expressions are not supported: %s", match)
			return match
		}

		expr := strings.TrimSpace(inner)
		if expr == "" {
			resolveErr = fmt.Errorf("empty expression: %s", match)
			return match
		}

		val, err := resolveExpr(expr, ctx)
		if err != nil {
			resolveErr = err
			return match
		}
		return val
	})

	if resolveErr != nil {
		return "", resolveErr
	}

	// Check for unmatched ${{ (opening without closing }}).
	remaining := exprPattern.ReplaceAllString(result, "")
	if strings.Contains(remaining, "${{") {
		return "", fmt.Errorf("unmatched expression delimiter in: %s", input)
	}

	return result, nil
}

// resolveExpr resolves a single dotted expression like "env.VAR" or "steps.ID.outputs.KEY".
func resolveExpr(expr string, ctx *ExpressionContext) (string, error) {
	parts := strings.SplitN(expr, ".", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid expression %q: expected namespace.key format", expr)
	}

	namespace := parts[0]
	rest := parts[1]

	switch namespace {
	case "env":
		return resolveEnv(rest, ctx)
	case "matrix":
		return resolveMatrix(rest, ctx)
	case "steps":
		return resolveSteps(rest, ctx)
	default:
		return "", fmt.Errorf("unknown namespace %q in expression %q", namespace, expr)
	}
}

func resolveEnv(key string, ctx *ExpressionContext) (string, error) {
	if ctx.Env == nil {
		return "", fmt.Errorf("unresolved expression: env.%s", key)
	}
	val, ok := ctx.Env[key]
	if !ok {
		return "", fmt.Errorf("unresolved expression: env.%s", key)
	}
	return val, nil
}

func resolveMatrix(key string, ctx *ExpressionContext) (string, error) {
	if ctx.Matrix == nil {
		return "", fmt.Errorf("unresolved expression: matrix.%s", key)
	}
	val, ok := ctx.Matrix[key]
	if !ok {
		return "", fmt.Errorf("unresolved expression: matrix.%s", key)
	}
	return val, nil
}

func resolveSteps(path string, ctx *ExpressionContext) (string, error) {
	// Expected format: ID.outputs.KEY
	parts := strings.SplitN(path, ".", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid steps expression %q: expected steps.ID.outputs.KEY", "steps."+path)
	}

	stepID := parts[0]
	segment := parts[1]
	outputKey := parts[2]

	if segment != "outputs" {
		return "", fmt.Errorf("invalid steps expression %q: only 'outputs' is supported after step ID", "steps."+path)
	}

	if ctx.Steps == nil {
		return "", fmt.Errorf("unresolved expression: steps.%s.outputs.%s", stepID, outputKey)
	}
	outputs, ok := ctx.Steps[stepID]
	if !ok {
		return "", fmt.Errorf("unresolved expression: steps.%s.outputs.%s", stepID, outputKey)
	}
	val, ok := outputs[outputKey]
	if !ok {
		return "", fmt.Errorf("unresolved expression: steps.%s.outputs.%s", stepID, outputKey)
	}
	return val, nil
}
