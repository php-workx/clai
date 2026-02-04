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
		// Check that the widget function exists and calls _clai_dismiss_picker.
		// We extract from function start to the next function definition rather
		// than matching braces, since widget bodies may contain nested braces.
		body := extractFunctionBody(output, widget)
		if body == "" {
			t.Errorf("widget function %s() not found in zsh script", widget)
			continue
		}
		if !strings.Contains(body, "_clai_dismiss_picker") {
			t.Errorf("widget %s() does not call _clai_dismiss_picker", widget)
		}
	}
}

// TestZshScript_ForwardCharValidatesSuggestionPrefix verifies that the
// right-arrow accept widget checks that _AI_CURRENT_SUGGESTION starts with
// BUFFER before accepting. Without this check, a stale suggestion after
// backspace can append incorrect characters (e.g., "source ~/.zshrcrc").
func TestZshScript_ForwardCharValidatesSuggestionPrefix(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	output := string(content)

	// Find the _ai_forward_char function and verify it contains a prefix check.
	// The function has nested braces so we look for the pattern between function
	// start and the next zle -N registration.
	start := strings.Index(output, "_ai_forward_char()")
	if start == -1 {
		t.Fatal("_ai_forward_char() not found in zsh script")
	}

	// Extract until the next "zle -N" line (function boundary)
	rest := output[start:]
	end := strings.Index(rest, "zle -N forward-char")
	if end == -1 {
		t.Fatal("could not find end of _ai_forward_char function")
	}
	body := rest[:end]

	if !strings.Contains(body, `"$_AI_CURRENT_SUGGESTION" == "$BUFFER"*`) {
		t.Error("_ai_forward_char() does not validate suggestion is a prefix of BUFFER; " +
			"stale suggestions after backspace will accept incorrect text")
	}
}

// TestBashScript_NoDirectBindXEscapeSequences verifies that the bash script
// does not use `bind -x` with escape sequences like \e[A. Bash 3.2 (macOS
// default) fails with "cannot find keymap for command" for these. Arrow keys
// must be mapped via readline macros to Ctrl-X prefixed sequences first.
func TestBashScript_NoDirectBindXEscapeSequences(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}

	lines := strings.Split(string(content), "\n")

	badPattern := regexp.MustCompile(`^\s*bind\s+-x\s+['"](\\e|\\033|\x1b)`)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if badPattern.MatchString(line) {
			t.Errorf("line %d: bind -x with escape sequence breaks bash 3.2: %s", i+1, trimmed)
		}
	}
}

// TestZshScript_AcceptLineClearsGhostText verifies that the Enter handler
// clears POSTDISPLAY and region_highlight before executing the command.
// Without this, ghost text from inline suggestions remains visible after
// the command output prints.
func TestZshScript_AcceptLineClearsGhostText(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}

	output := string(content)

	// Find the _ai_voice_accept_line function body up to its zle -N registration
	start := strings.Index(output, "_ai_voice_accept_line()")
	if start == -1 {
		t.Fatal("_ai_voice_accept_line() not found in zsh script")
	}
	rest := output[start:]
	end := strings.Index(rest, "zle -N _ai_voice_accept_line")
	if end == -1 {
		t.Fatal("could not find end of _ai_voice_accept_line function")
	}
	body := rest[:end]

	// The normal accept-line path must clear ghost text state
	for _, required := range []string{`POSTDISPLAY=""`, "region_highlight=()"} {
		if !strings.Contains(body, required) {
			t.Errorf("_ai_voice_accept_line() missing %q before accept-line; "+
				"ghost text will persist after Enter", required)
		}
	}
}

// TestZshScript_PickerReversedRenderAndPaging verifies that the picker:
// 1. Renders items in reversed order (last array element at top, first at bottom)
// 2. Tracks paging state (_CLAI_PICKER_PAGE, _CLAI_PICKER_AT_END)
// 3. Passes --offset to clai history for pagination
// 4. Down at bottom (index 0) does nothing (no wrapping)
func TestZshScript_PickerReversedRenderAndPaging(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	script := string(content)

	// 1. Paging state variables exist
	for _, v := range []string{"_CLAI_PICKER_PAGE=0", "_CLAI_PICKER_AT_END=false"} {
		if !strings.Contains(script, v) {
			t.Errorf("Missing paging state variable: %s", v)
		}
	}

	// 2. --offset passed in picker load
	if !strings.Contains(script, `--offset "$offset"`) {
		t.Error("_clai_picker_load should pass --offset to clai history")
	}

	// 3. Reversed render: loop counts down from count-1 to 0
	if !strings.Contains(script, "local i=$((count - 1))") {
		t.Error("_clai_picker_render should loop in reverse (i = count-1 down to 0)")
	}

	// 4. Down handler: at index 0, do nothing (no wrapping to end)
	// The old code had: _CLAI_PICKER_INDEX=0 as wrap-to-start in down handler.
	// New code should NOT wrap — only decrement when index > 0.
	downBody := extractFunctionBody(script, "_clai_picker_down")
	if downBody == "" {
		t.Fatal("_clai_picker_down() not found")
	}
	if strings.Contains(downBody, "_CLAI_PICKER_INDEX=$((${#_CLAI_PICKER_ITEMS[@]} - 1))") {
		t.Error("_clai_picker_down should NOT wrap to end when at bottom")
	}

	// 5. Up handler references paging (PAGE increment, offset calculation)
	upBody := extractFunctionBody(script, "_clai_picker_up")
	if upBody == "" {
		t.Fatal("_clai_picker_up() not found")
	}
	if !strings.Contains(upBody, "_CLAI_PICKER_PAGE++") {
		t.Error("_clai_picker_up should increment page when at top")
	}
	if !strings.Contains(upBody, "_CLAI_PICKER_AT_END") {
		t.Error("_clai_picker_up should check _CLAI_PICKER_AT_END before paging")
	}
}

// extractFunctionBody returns the text from a shell function definition
// (funcName followed by "()") up to the next top-level function definition.
// Returns empty string if the function is not found.
func extractFunctionBody(script, funcName string) string {
	start := strings.Index(script, funcName+"()")
	if start == -1 {
		return ""
	}
	rest := script[start:]
	// Find next function definition as boundary
	nextFunc := regexp.MustCompile(`\n[a-zA-Z_][a-zA-Z0-9_]*\(\)\s*\{`)
	if loc := nextFunc.FindStringIndex(rest[1:]); loc != nil {
		return rest[:loc[0]+1]
	}
	return rest
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
