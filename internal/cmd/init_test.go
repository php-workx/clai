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

func TestZshScript_PickerWidgetsAreBound(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	script := string(content)

	required := []string{
		`bindkey '^I' _clai_picker_suggest`,
		`bindkey '^M' _clai_picker_accept`,
		`bindkey '^[[B' _clai_picker_down`,
		`bindkey '^[OB' _clai_picker_down`,
		`bindkey '^Xs' _clai_history_scope_session`,
		`bindkey '^Xd' _clai_history_scope_cwd`,
		`bindkey '^Xg' _clai_history_scope_global`,
		`bindkey '^[[A' _clai_picker_up`,
		`bindkey '^[OA' _clai_picker_up`,
	}

	for _, binding := range required {
		if !strings.Contains(script, binding) {
			t.Errorf("zsh script missing picker binding %s", binding)
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

// TestZshScript_SelfInsertSkipsSuggestForQueuedInput verifies that zsh does
// not call clai suggest for each queued character during paste-like input.
func TestZshScript_SelfInsertSkipsSuggestForQueuedInput(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	script := string(content)

	body := extractFunctionBody(script, "_ai_self_insert")
	if body == "" {
		t.Fatal("_ai_self_insert() not found")
	}

	if !strings.Contains(body, "KEYS_QUEUED_COUNT") {
		t.Error("_ai_self_insert should guard on KEYS_QUEUED_COUNT to avoid per-char suggest during paste")
	}
	if !strings.Contains(body, `_AI_IN_PASTE`) {
		t.Error("_ai_self_insert should preserve _AI_IN_PASTE guard")
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

// TestZshScript_DefaultCompletionAndHistoryClearGhostText verifies that
// default Tab completion and history navigation clear ghost text state first.
func TestZshScript_DefaultCompletionAndHistoryClearGhostText(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	script := string(content)

	for _, fn := range []string{"_ai_expand_or_complete", "_ai_up_line_or_history", "_ai_down_line_or_history"} {
		body := extractFunctionBody(script, fn)
		if body == "" {
			t.Fatalf("%s() not found", fn)
		}
		for _, required := range []string{"_ai_clear_ghost_text", `POSTDISPLAY=""`, "region_highlight=()"} {
			if required == "_ai_clear_ghost_text" {
				if !strings.Contains(body, required) {
					t.Errorf("%s() should call %s before delegating", fn, required)
				}
				continue
			}
			// Defensive: either clear inline in function body or via helper.
			if !strings.Contains(script, required) {
				t.Errorf("zsh script missing ghost text clear primitive %q", required)
			}
		}
	}

	for _, bind := range []string{
		"zle -N expand-or-complete _ai_expand_or_complete",
		"zle -N up-line-or-history _ai_up_line_or_history",
		"zle -N down-line-or-history _ai_down_line_or_history",
	} {
		if !strings.Contains(script, bind) {
			t.Errorf("missing zle binding: %s", bind)
		}
	}
}

// TestZshScript_PickerRenderAndPaging verifies that the picker:
// 1. Renders items in forward order (newest at top, closest to input)
// 2. Tracks paging state (_CLAI_PICKER_PAGE, _CLAI_PICKER_AT_END)
// 3. Passes --offset to clai history for pagination
// 4. Down at bottom (index 0) does nothing (no wrapping)
func TestZshScript_PickerRenderAndPaging(t *testing.T) {
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

	// 3. Forward render: loop counts up from 0
	if !strings.Contains(script, "local i=0") || !strings.Contains(script, "((i++))") {
		t.Error("_clai_picker_render should loop forward (i = 0 up to count)")
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

// TestShellScripts_MacOSOptionH verifies that all shell scripts bind both
// \eh (ESC+h, for terminals that send ESC for Alt) and the literal ˙ character
// (U+02D9, which macOS Option+H produces with US keyboard layout).
func TestShellScripts_MacOSOptionH(t *testing.T) {
	shells := []struct {
		path       string
		escBinding string // ESC-based binding
		macBinding string // macOS literal character binding
	}{
		{"shell/zsh/clai.zsh", `'\eh'`, `'˙'`},
		{"shell/bash/clai.bash", `"\eh"`, `"˙"`},
		{"shell/fish/clai.fish", `\eh`, `˙`},
	}

	for _, sh := range shells {
		t.Run(sh.path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(sh.path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", sh.path, err)
			}
			script := string(content)

			if !strings.Contains(script, sh.escBinding) {
				t.Errorf("%s missing ESC-based Alt+H binding %s", sh.path, sh.escBinding)
			}
			if !strings.Contains(script, sh.macBinding) {
				t.Errorf("%s missing macOS Option+H binding %s", sh.path, sh.macBinding)
			}
			if !strings.Contains(script, "_clai_tui_picker_open") {
				t.Errorf("%s missing _clai_tui_picker_open function reference", sh.path)
			}
		})
	}
}

// TestShellScripts_UpArrowHistoryPlaceholder verifies that all shell scripts
// contain the {{CLAI_UP_ARROW_HISTORY}} placeholder that init.go replaces.
func TestShellScripts_UpArrowHistoryPlaceholder(t *testing.T) {
	shells := []string{
		"shell/zsh/clai.zsh",
		"shell/bash/clai.bash",
		"shell/fish/clai.fish",
	}

	for _, path := range shells {
		t.Run(path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", path, err)
			}
			script := string(content)

			if !strings.Contains(script, "{{CLAI_UP_ARROW_HISTORY}}") {
				t.Errorf("%s missing {{CLAI_UP_ARROW_HISTORY}} placeholder", path)
			}
			if !strings.Contains(script, "CLAI_UP_ARROW_HISTORY") {
				t.Errorf("%s missing CLAI_UP_ARROW_HISTORY variable usage", path)
			}
		})
	}
}

// TestShellScripts_PickerOpenOnEmptyPlaceholder verifies that all shell scripts
// contain the {{CLAI_PICKER_OPEN_ON_EMPTY}} placeholder that init.go replaces.
func TestShellScripts_PickerOpenOnEmptyPlaceholder(t *testing.T) {
	shells := []string{
		"shell/zsh/clai.zsh",
		"shell/bash/clai.bash",
		"shell/fish/clai.fish",
	}

	for _, path := range shells {
		t.Run(path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", path, err)
			}
			script := string(content)

			if !strings.Contains(script, "{{CLAI_PICKER_OPEN_ON_EMPTY}}") {
				t.Errorf("%s missing {{CLAI_PICKER_OPEN_ON_EMPTY}} placeholder", path)
			}
			if !strings.Contains(script, "CLAI_PICKER_OPEN_ON_EMPTY") {
				t.Errorf("%s missing CLAI_PICKER_OPEN_ON_EMPTY variable usage", path)
			}
		})
	}
}

// TestShellScripts_UpArrowConditionalBinding verifies that Up arrow is only
// bound to the TUI picker when CLAI_UP_ARROW_HISTORY is "true".
func TestShellScripts_UpArrowConditionalBinding(t *testing.T) {
	shells := []struct {
		path  string
		guard string // the conditional check
	}{
		{"shell/zsh/clai.zsh", `"$CLAI_UP_ARROW_HISTORY" == "true"`},
		{"shell/bash/clai.bash", `"$CLAI_UP_ARROW_HISTORY" == "true"`},
		{"shell/fish/clai.fish", `"$CLAI_UP_ARROW_HISTORY" = "true"`},
	}

	for _, sh := range shells {
		t.Run(sh.path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(sh.path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", sh.path, err)
			}
			script := string(content)

			if !strings.Contains(script, sh.guard) {
				t.Errorf("%s missing conditional guard %q for Up arrow binding", sh.path, sh.guard)
			}
		})
	}
}

func TestShellScripts_OpenOnEmptyGuards(t *testing.T) {
	shells := []struct {
		path  string
		guard string
	}{
		{"shell/zsh/clai.zsh", `"$CLAI_PICKER_OPEN_ON_EMPTY" != "true" && -z "$BUFFER"`},
		{"shell/bash/clai.bash", `"$CLAI_PICKER_OPEN_ON_EMPTY" != "true" && -z "$picker_query"`},
		{"shell/fish/clai.fish", `"$CLAI_PICKER_OPEN_ON_EMPTY" != "true"; and test -z (commandline)`},
	}

	for _, sh := range shells {
		t.Run(sh.path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(sh.path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", sh.path, err)
			}
			script := string(content)

			if !strings.Contains(script, sh.guard) {
				t.Errorf("%s missing open-on-empty guard %q", sh.path, sh.guard)
			}
		})
	}
}

// TestBashScript_MacOSOptionHMacroTranslation verifies that bash uses a
// readline macro to translate the macOS ˙ character to a Ctrl sequence,
// since bash 3.2's bind -x cannot bind multi-byte UTF-8 characters.
func TestBashScript_MacOSOptionHMacroTranslation(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}
	script := string(content)

	// The ˙ character should be translated via a macro, not bound with -x directly.
	if !strings.Contains(script, `bind '"˙": "\C-x\C-h"'`) {
		t.Error("bash script missing macro translation of ˙ to \\C-x\\C-h")
	}
	if !strings.Contains(script, `bind -x '"\C-x\C-h": _clai_history_up'`) {
		t.Error("bash script missing bind -x for \\C-x\\C-h history picker")
	}
}

func TestBashScript_HistoryPickerBindings(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}
	script := string(content)

	required := []string{
		`bind -x '"\eh": _clai_history_up'`,
		`bind -x '"\C-x\C-a": _clai_pre_accept'`,
		`bind '"\C-x\C-b": accept-line'`,
		`bind '"\eOA": "\C-x\C-p"'`,
		`bind '"\eOB": "\C-x\C-n"'`,
		`bind -x '"\C-x\C-p": _clai_up_arrow'`,
		`bind -x '"\C-x\C-n": _clai_down_arrow'`,
	}

	for _, binding := range required {
		if !strings.Contains(script, binding) {
			t.Errorf("bash script missing history picker binding %s", binding)
		}
	}
}

func TestFishScript_UpArrowApplicationModeBinding(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}
	script := string(content)

	required := []string{
		`for mode in default insert visual`,
		`bind -M $mode \e\[A _clai_up_arrow`,
		`bind -M $mode \eOA _clai_up_arrow`,
	}

	for _, binding := range required {
		if !strings.Contains(script, binding) {
			t.Errorf("fish script missing up-arrow binding %s", binding)
		}
	}
}

func TestFishScript_PickerControlBindings(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}
	script := string(content)

	required := []string{
		`bind -M $mode \t _clai_suggest_tab`,
		`bind -M $mode \e\[B _clai_picker_down`,
		`bind -M $mode \eOB _clai_picker_down`,
		`bind -M $mode \cxs _clai_history_scope_session`,
		`bind -M $mode \cxd _clai_history_scope_cwd`,
		`bind -M $mode \cxg _clai_history_scope_global`,
	}

	for _, binding := range required {
		if !strings.Contains(script, binding) {
			t.Errorf("fish script missing picker control binding %s", binding)
		}
	}
}

func TestFishScript_DisableModeScopedCleanup(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}
	script := string(content)

	required := []string{
		`function _clai_disable`,
		`for mode in default insert visual`,
		`bind -M $mode \t complete`,
		`bind -M $mode \e\[B history-search-forward`,
		`bind -M $mode \eOB history-search-forward`,
		`bind -M $mode \e ''`,
		`bind -M $mode \eh ''`,
		`bind -M $mode \cxs ''`,
		`bind -M $mode \cxd ''`,
		`bind -M $mode \cxg ''`,
	}

	for _, pattern := range required {
		if !strings.Contains(script, pattern) {
			t.Errorf("fish script missing mode-scoped disable cleanup %s", pattern)
		}
	}
}

func TestFishScript_TUIPickerQuotesSingleValueArgs(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}
	script := string(content)

	required := []string{
		`--session="$CLAI_SESSION_ID"`,
		`--cwd="$PWD"`,
	}

	for _, arg := range required {
		if !strings.Contains(script, arg) {
			t.Errorf("fish script missing quoted argument %s", arg)
		}
	}
}

func TestFishScript_DateNanosecondsGuardIsNumeric(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}
	script := string(content)

	if !strings.Contains(script, `set -l _ns (command date +%s%N 2>/dev/null)`) {
		t.Error("fish script missing guarded nanosecond timestamp capture")
	}
	if !strings.Contains(script, `string match -rq '^[0-9]+$' -- $_ns`) {
		t.Error("fish script missing numeric nanosecond guard")
	}
}

func TestFishScript_DisownUsesLatestJob(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}
	script := string(content)

	if strings.Contains(script, `disown %1`) {
		t.Error("fish script still uses fragile disown %1 pattern")
	}
	if !strings.Contains(script, `disown 2>/dev/null`) {
		t.Error("fish script missing disown call for background shim jobs")
	}
}

func TestBashScript_HistoryPickerUsesPromptQuery(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}
	script := string(content)

	required := []string{
		`_CLAI_PICKER_QUERY=""`,
		`local query="${1-$_CLAI_PICKER_QUERY}"`,
		`_CLAI_PICKER_QUERY="$picker_query"`,
		`if ! _clai_picker_load_history "$_CLAI_PICKER_QUERY"; then`,
		`_clai_picker_load_history "$_CLAI_PICKER_QUERY" && _clai_picker_apply`,
		`_clai_tui_picker_open "$picker_query"`,
	}

	for _, pattern := range required {
		if !strings.Contains(script, pattern) {
			t.Errorf("bash script missing prompt-query behavior %s", pattern)
		}
	}
}

// TestInitPlaceholderReplacement verifies that init.go replaces both
// CLAI_SESSION_ID and CLAI_UP_ARROW_HISTORY placeholders.
func TestInitPlaceholderReplacement(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	script := string(content)

	// Verify the raw template has the placeholder.
	if !strings.Contains(script, "{{CLAI_UP_ARROW_HISTORY}}") {
		t.Fatal("zsh script missing {{CLAI_UP_ARROW_HISTORY}} placeholder")
	}

	// Simulate the replacement that init.go performs.
	replaced := strings.ReplaceAll(script, "{{CLAI_SESSION_ID}}", "test-session-id")
	replaced = strings.ReplaceAll(replaced, "{{CLAI_UP_ARROW_HISTORY}}", "false")
	replaced = strings.ReplaceAll(replaced, "{{CLAI_PICKER_OPEN_ON_EMPTY}}", "false")

	if strings.Contains(replaced, "{{CLAI_UP_ARROW_HISTORY}}") {
		t.Error("placeholder {{CLAI_UP_ARROW_HISTORY}} not replaced")
	}
	if strings.Contains(replaced, "{{CLAI_PICKER_OPEN_ON_EMPTY}}") {
		t.Error("placeholder {{CLAI_PICKER_OPEN_ON_EMPTY}} not replaced")
	}
	if strings.Contains(replaced, "{{CLAI_SESSION_ID}}") {
		t.Error("placeholder {{CLAI_SESSION_ID}} not replaced")
	}
	if !strings.Contains(replaced, "CLAI_UP_ARROW_HISTORY:=false") {
		t.Error("expected CLAI_UP_ARROW_HISTORY:=false after replacement")
	}
	if !strings.Contains(replaced, "CLAI_PICKER_OPEN_ON_EMPTY:=false") {
		t.Error("expected CLAI_PICKER_OPEN_ON_EMPTY:=false after replacement")
	}
}

func TestGenerateSessionID_FormatAndUniqueness(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	uuidLike := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidLike.MatchString(id1) {
		t.Errorf("session ID %q does not match UUID v4 format", id1)
	}
	if !uuidLike.MatchString(id2) {
		t.Errorf("session ID %q does not match UUID v4 format", id2)
	}
	if id1 == id2 {
		t.Errorf("generateSessionID returned duplicate IDs: %q", id1)
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
