package cmd

import (
	"regexp"
	"strings"
	"testing"
)

func TestRunInit_Zsh(t *testing.T) {
	// Capture stdout by reading the embedded file directly
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	output := string(content)

	// Verify essential Zsh-specific content
	requiredContent := []string{
		"clai.zsh",
		"CLAI_CACHE",
		"zle -N",
		"bindkey",
		"add-zsh-hook",
		"precmd",
		"clai diagnose",
		"clai extract",
		"clai ask",
		"clai suggest",
		"POSTDISPLAY",
		"region_highlight",
		"pipestatus",
		"ai-fix",
		"run()",
	}

	for _, req := range requiredContent {
		if !strings.Contains(output, req) {
			t.Errorf("zsh script missing %q", req)
		}
	}

	if len(output) < 1000 {
		t.Errorf("zsh script too short (%d bytes)", len(output))
	}
}

func TestRunInit_Bash(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}

	output := string(content)

	requiredContent := []string{
		"clai.bash",
		"CLAI_CACHE",
		"PROMPT_COMMAND",
		"DEBUG",
		"trap",
		"clai diagnose",
		"clai extract",
		"clai ask",
		"PIPESTATUS",
		"history",
		"accept",
		"ai-fix",
		"run()",
	}

	for _, req := range requiredContent {
		if !strings.Contains(output, req) {
			t.Errorf("bash script missing %q", req)
		}
	}

	if len(output) < 1000 {
		t.Errorf("bash script too short (%d bytes)", len(output))
	}
}

func TestRunInit_Fish(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}

	output := string(content)

	requiredContent := []string{
		"clai.fish",
		"CLAI_CACHE",
		"set -gx",
		"function",
		"fish_right_prompt",
		"commandline",
		"clai diagnose",
		"clai extract",
		"clai ask",
		"status is-interactive",
		"pipestatus",
		"function ai-fix",
		"function run",
	}

	for _, req := range requiredContent {
		if !strings.Contains(output, req) {
			t.Errorf("fish script missing %q", req)
		}
	}

	if len(output) < 1000 {
		t.Errorf("fish script too short (%d bytes)", len(output))
	}
}

func TestRunInit_UnsupportedShell(t *testing.T) {
	err := runInit(initCmd, []string{"powershell"})
	if err == nil {
		t.Error("init powershell should have failed")
	}

	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("Error should mention unsupported shell, got: %v", err)
	}
}

func TestShellScripts_AllHaveCommonFeatures(t *testing.T) {
	shells := map[string]string{
		"zsh":  "shell/zsh/clai.zsh",
		"bash": "shell/bash/clai.bash",
		"fish": "shell/fish/clai.fish",
	}

	commonFeatures := []string{
		"CLAI_AUTO_EXTRACT",
		"CLAI_CACHE",
	}

	for name, path := range shells {
		t.Run(name, func(t *testing.T) {
			content, err := shellScripts.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read %s script: %v", name, err)
			}

			output := string(content)

			for _, feature := range commonFeatures {
				if !strings.Contains(output, feature) {
					t.Errorf("%s script missing common feature: %q", name, feature)
				}
			}
		})
	}
}

// TestZshScript_NoEscapeSequencesInPOSTDISPLAY verifies that POSTDISPLAY
// assignments use plain text only (colored via region_highlight), not prompt
// escapes (%F{...}) or ANSI escapes (\e[...) which ZLE renders literally.
func TestZshScript_NoEscapeSequencesInPOSTDISPLAY(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	lines := strings.Split(string(content), "\n")

	// Patterns that should never appear in a POSTDISPLAY= assignment line.
	// These cause literal escape characters to show as ghost text.
	badPatterns := []*regexp.Regexp{
		regexp.MustCompile(`POSTDISPLAY=.*%F\{`),     // zsh prompt escapes
		regexp.MustCompile(`POSTDISPLAY=.*%f`),       // zsh prompt reset
		regexp.MustCompile(`POSTDISPLAY=.*\\e\[`),    // ANSI escapes (literal \e)
		regexp.MustCompile(`POSTDISPLAY=.*\$'\\e\[`), // ANSI escapes ($'\e[...')
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, pat := range badPatterns {
			if pat.MatchString(line) {
				t.Errorf("line %d: POSTDISPLAY assignment contains escape sequences "+
					"(use region_highlight instead): %s", i+1, trimmed)
			}
		}
	}
}

// TestZshScript_NoBareEscapeBinding verifies that the zsh script does not bind
// bare '\e' (Escape) to a widget. A bare \e binding intercepts the first byte
// of multi-byte escape sequences (arrow keys, Alt+key) when KEYTIMEOUT is low,
// breaking picker navigation.
func TestZshScript_NoBareEscapeBinding(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	lines := strings.Split(string(content), "\n")

	// Match bindkey lines that bind bare \e or '^[' without additional characters.
	// OK: bindkey '^[[A' ... (CSI sequence), bindkey '^[OA' ... (application mode)
	// BAD: bindkey '\e' ... or bindkey '^[' ... (bare escape)
	bareEscape := regexp.MustCompile(`^\s*bindkey\s+["']\\e["']\s+`)
	bareCaret := regexp.MustCompile(`^\s*bindkey\s+["']\^?\[["']\s+`)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if bareEscape.MatchString(line) {
			t.Errorf("line %d: bare \\e binding breaks arrow keys with low KEYTIMEOUT: %s", i+1, trimmed)
		}
		if bareCaret.MatchString(line) && !strings.Contains(line, "^[[") && !strings.Contains(line, "^[O") {
			t.Errorf("line %d: bare ^[ binding breaks arrow keys with low KEYTIMEOUT: %s", i+1, trimmed)
		}
	}
}

// TestZshScript_ApplicationModeArrowBindings verifies that both CSI-mode (^[[A)
// and application-mode (^[OA) arrow key sequences are bound for the picker.
// Some terminals send SS3 sequences; missing bindings cause navigation to fail.
func TestZshScript_ApplicationModeArrowBindings(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	output := string(content)

	requiredBindings := []string{
		"^[[A", // Up arrow CSI mode
		"^[OA", // Up arrow application mode
		"^[[B", // Down arrow CSI mode
		"^[OB", // Down arrow application mode
	}

	for _, binding := range requiredBindings {
		if !strings.Contains(output, binding) {
			t.Errorf("zsh script missing arrow key binding %q", binding)
		}
	}
}

// TestZshScript_EditingWidgetsDismissPicker verifies that all editing and
// cursor-movement ZLE widgets call _clai_dismiss_picker to close the
// suggestion menu when the user starts editing.
func TestZshScript_EditingWidgetsDismissPicker(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	output := string(content)

	// These widgets modify the buffer or cursor position; they must dismiss
	// the picker so stale suggestions don't remain visible.
	widgets := []string{
		"_ai_self_insert",
		"_ai_backward_delete_char",
		"_ai_backward_char",
		"_ai_beginning_of_line",
		"_ai_end_of_line",
		"_ai_bracketed_paste",
	}

	for _, widget := range widgets {
		// Find the function body and check it calls _clai_dismiss_picker
		funcPattern := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(widget) + `\(\)\s*\{([^}]+)\}`)
		match := funcPattern.FindStringSubmatch(output)
		if match == nil {
			t.Errorf("widget function %s() not found in zsh script", widget)
			continue
		}
		if !strings.Contains(match[1], "_clai_dismiss_picker") {
			t.Errorf("widget %s() does not call _clai_dismiss_picker", widget)
		}
	}
}

func TestShellScripts_Embedded(t *testing.T) {
	// Verify all shell scripts are properly embedded
	shells := []string{
		"shell/zsh/clai.zsh",
		"shell/bash/clai.bash",
		"shell/fish/clai.fish",
	}

	for _, path := range shells {
		t.Run(path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read embedded file %s: %v", path, err)
			}

			if len(content) == 0 {
				t.Errorf("Embedded file %s is empty", path)
			}
		})
	}
}
