package cmd

import (
	"os"
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

func TestShellScripts_SuggestUsesPlainFormat(t *testing.T) {
	tests := []struct {
		path     string
		markers  []string
		notMatch []string
	}{
		{
			path: "shell/zsh/clai.zsh",
			markers: []string{
				"clai suggest --format fzf --limit 1",
				`clai suggest --format fzf --limit "$CLAI_MENU_LIMIT"`,
			},
			notMatch: []string{
				"clai suggest \"$BUFFER\"",
			},
		},
		{
			path: "shell/bash/clai.bash",
			markers: []string{
				"clai-picker suggest --query=\"$READLINE_LINE\"",
			},
			notMatch: []string{
				"clai suggest --limit",
			},
		},
		{
			path: "shell/fish/clai.fish",
			markers: []string{
				"clai suggest --format fzf --limit 1",
				"clai suggest --format fzf --limit $CLAI_MENU_LIMIT",
			},
			notMatch: []string{
				"clai suggest \"$current\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", tt.path, err)
			}
			script := string(content)
			for _, m := range tt.markers {
				if !strings.Contains(script, m) {
					t.Errorf("%s missing marker %q", tt.path, m)
				}
			}
			for _, bad := range tt.notMatch {
				if strings.Contains(script, bad) {
					t.Errorf("%s contains legacy pattern %q", tt.path, bad)
				}
			}
		})
	}
}

func TestShellScripts_HistoryPickerDownRestoresOriginal(t *testing.T) {
	{
		content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
		if err != nil {
			t.Fatalf("Failed to read zsh script: %v", err)
		}
		body := extractFunctionBody(string(content), "_clai_picker_down")
		if body == "" {
			t.Fatal("_clai_picker_down() not found in zsh script")
		}
		if !strings.Contains(body, "_clai_picker_cancel") {
			t.Error("zsh _clai_picker_down should call _clai_picker_cancel at the newest item to match native history UX")
		}
	}

	{
		content, err := shellScripts.ReadFile("shell/fish/clai.fish")
		if err != nil {
			t.Fatalf("Failed to read fish script: %v", err)
		}
		body := extractFishFunctionBody(string(content), "_clai_picker_down")
		if body == "" {
			t.Fatal("function _clai_picker_down not found in fish script")
		}
		if !strings.Contains(body, "_clai_picker_cancel") {
			t.Error("fish _clai_picker_down should call _clai_picker_cancel at the newest item to match native history UX")
		}
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

func TestRunInit_PreservesSessionID(t *testing.T) {
	t.Setenv("CLAI_SESSION_ID", "session-fixed-123")

	output := captureStdout(t, func() {
		if err := runInit(initCmd, []string{"zsh"}); err != nil {
			t.Fatalf("runInit error: %v", err)
		}
	})

	if !strings.Contains(output, "session-fixed-123") {
		t.Fatalf("expected CLAI_SESSION_ID to be preserved, got output: %s", output)
	}
}

func TestRunInit_GeneratesNewSessionID(t *testing.T) {
	os.Unsetenv("CLAI_SESSION_ID")

	output1 := captureStdout(t, func() {
		if err := runInit(initCmd, []string{"zsh"}); err != nil {
			t.Fatalf("runInit error: %v", err)
		}
	})

	output2 := captureStdout(t, func() {
		if err := runInit(initCmd, []string{"zsh"}); err != nil {
			t.Fatalf("runInit error: %v", err)
		}
	})

	re := regexp.MustCompile(`CLAI_SESSION_ID="([^"]+)"`)
	m1 := re.FindStringSubmatch(output1)
	m2 := re.FindStringSubmatch(output2)
	if len(m1) < 2 || len(m2) < 2 {
		t.Fatalf("expected session IDs in output")
	}
	if m1[1] == m2[1] {
		t.Fatalf("expected different session IDs for new shells, got %q", m1[1])
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
		if !strings.Contains(body, "_ai_clear_ghost_text") {
			t.Errorf("%s() should call _ai_clear_ghost_text before delegating", fn)
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

// TestZshScript_CustomHistoryPathsClearGhostText verifies that clai's
// custom Up-arrow paths (TUI picker, inline picker, single/double trigger)
// do not bypass the wrapped up-line-or-history widget. Bypassing the wrapper
// leaves stale POSTDISPLAY ghost text visible after history navigation.
func TestZshScript_CustomHistoryPathsClearGhostText(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	script := string(content)

	// These functions historically called `zle .up-line-or-history` directly,
	// which bypasses _ai_up_line_or_history (and its ghost-text clearing).
	for _, fn := range []string{
		"_clai_tui_picker_open",
		"_clai_picker_up",
		"_clai_up_arrow_single",
		"_clai_up_arrow_double",
	} {
		body := extractFunctionBody(script, fn)
		if body == "" {
			t.Fatalf("%s() not found", fn)
		}
		if strings.Contains(body, "zle .up-line-or-history") {
			t.Errorf("%s() should not call zle .up-line-or-history directly; use zle up-line-or-history", fn)
		}
	}

	downBody := extractFunctionBody(script, "_clai_picker_down")
	if downBody == "" {
		t.Fatal("_clai_picker_down() not found")
	}
	if strings.Contains(downBody, "zle .down-line-or-history") {
		t.Error("_clai_picker_down() should not call zle .down-line-or-history directly; use zle down-line-or-history")
	}

	breakBody := extractFunctionBody(script, "_clai_picker_break")
	if breakBody == "" {
		t.Fatal("_clai_picker_break() not found")
	}
	if !strings.Contains(breakBody, "_ai_clear_ghost_text") {
		t.Error("_clai_picker_break() should clear ghost text before delegating to send-break")
	}
}

func TestZshScript_GhostTextInvariantHook(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	script := string(content)

	for _, required := range []string{
		"_ai_sync_ghost_text()",
		"_ai_zle_line_pre_redraw()",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("zsh script missing ghost text invariant hook: %s", required)
		}
	}
	hasLegacy := strings.Contains(script, "zle -N zle-line-pre-redraw _ai_zle_line_pre_redraw")
	hasHook := strings.Contains(script, "add-zle-hook-widget zle-line-pre-redraw _ai_zle_line_pre_redraw")
	if !hasLegacy && !hasHook {
		t.Fatalf("zsh script missing ghost text invariant hook registration")
	}

	body := extractFunctionBody(script, "_ai_sync_ghost_text")
	if body == "" {
		t.Fatal("_ai_sync_ghost_text() not found")
	}
	if !strings.Contains(body, `"$_AI_CURRENT_SUGGESTION" != "$BUFFER"*`) {
		t.Error("_ai_sync_ghost_text() should clear when suggestion is not a prefix of BUFFER")
	}
	if !strings.Contains(body, "_ai_clear_ghost_text") {
		t.Error("_ai_sync_ghost_text() should call _ai_clear_ghost_text on mismatch")
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

func TestBashScript_HistoryPickerUsesPromptQuery(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/bash/clai.bash")
	if err != nil {
		t.Fatalf("Failed to read bash script: %v", err)
	}
	script := string(content)

	if !strings.Contains(script, "_CLAI_PICKER_QUERY=") {
		t.Fatal("bash script missing _CLAI_PICKER_QUERY state")
	}
	upBody := extractFunctionBody(script, "_clai_history_up")
	if upBody == "" {
		t.Fatal("_clai_history_up() not found")
	}
	if !strings.Contains(upBody, `_CLAI_PICKER_QUERY="$READLINE_LINE"`) {
		t.Error("_clai_history_up should snapshot READLINE_LINE into _CLAI_PICKER_QUERY when opening picker")
	}

	loadBody := extractFunctionBody(script, "_clai_picker_load_history")
	if loadBody == "" {
		t.Fatal("_clai_picker_load_history() not found")
	}
	if !strings.Contains(loadBody, "_CLAI_PICKER_QUERY") {
		t.Error("_clai_picker_load_history should use _CLAI_PICKER_QUERY so navigating selection doesn't change query")
	}
}

func TestFishScript_DateNanosecondsGuardIsNumeric(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/fish/clai.fish")
	if err != nil {
		t.Fatalf("Failed to read fish script: %v", err)
	}
	script := string(content)

	if !strings.Contains(script, "date +%s%N") {
		t.Fatalf("fish script missing date +%s usage for millisecond timing", "%s%N")
	}
	if !strings.Contains(script, "string match -rq '^[0-9]+$'") {
		t.Fatalf("fish script missing numeric guard for date +%s output", "%s%N")
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
	for _, required := range []string{`POSTDISPLAY=""`, "_ai_remove_ghost_highlight"} {
		if !strings.Contains(body, required) {
			t.Errorf("_ai_voice_accept_line() missing %q before accept-line; "+
				"ghost text will persist after Enter", required)
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

// extractFishFunctionBody returns the text from a top-level fish function
// definition (\"function <name>\") up to the next top-level fish function.
// Returns empty string if the function is not found.
func extractFishFunctionBody(script, funcName string) string {
	start := strings.Index(script, "function "+funcName)
	if start == -1 {
		return ""
	}
	rest := script[start:]
	nextFunc := regexp.MustCompile(`\nfunction\s+[a-zA-Z_][a-zA-Z0-9_]*\b`)
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

// TestShellScripts_UpArrowPlaceholders verifies that all shell scripts contain
// the Up-arrow placeholders that init.go replaces.
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
			if !strings.Contains(script, "{{CLAI_UP_ARROW_TRIGGER}}") {
				t.Errorf("%s missing {{CLAI_UP_ARROW_TRIGGER}} placeholder", path)
			}
			if !strings.Contains(script, "{{CLAI_UP_ARROW_DOUBLE_WINDOW_MS}}") {
				t.Errorf("%s missing {{CLAI_UP_ARROW_DOUBLE_WINDOW_MS}} placeholder", path)
			}
			if !strings.Contains(script, "CLAI_UP_ARROW_HISTORY") {
				t.Errorf("%s missing CLAI_UP_ARROW_HISTORY variable usage", path)
			}
			if !strings.Contains(script, "CLAI_UP_ARROW_TRIGGER") {
				t.Errorf("%s missing CLAI_UP_ARROW_TRIGGER variable usage", path)
			}
			if !strings.Contains(script, "CLAI_UP_ARROW_DOUBLE_WINDOW_MS") {
				t.Errorf("%s missing CLAI_UP_ARROW_DOUBLE_WINDOW_MS variable usage", path)
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

// TestShellScripts_DoubleUpSequenceSupport verifies shell scripts include
// explicit double-Up key sequence bindings and timeout controls.
func TestShellScripts_DoubleUpSequenceSupport(t *testing.T) {
	tests := []struct {
		path      string
		sequences []string
		timeouts  []string
	}{
		{
			path: "shell/zsh/clai.zsh",
			sequences: []string{
				`bindkey '^[[A^[[A' _clai_up_arrow_double`,
				`bindkey '^[OA^[OA' _clai_up_arrow_double`,
			},
			timeouts: []string{
				"CLAI_UP_ARROW_DOUBLE_WINDOW_MS",
				"KEYTIMEOUT",
			},
		},
		{
			path: "shell/bash/clai.bash",
			sequences: []string{
				`bind '"\e[A\e[A": "\C-x\C-u"'`,
			},
			timeouts: []string{
				"CLAI_UP_ARROW_DOUBLE_WINDOW_MS",
				"set keyseq-timeout",
			},
		},
		{
			path: "shell/fish/clai.fish",
			sequences: []string{
				`bind \e\[A\e\[A _clai_up_arrow_double`,
			},
			timeouts: []string{
				"CLAI_UP_ARROW_DOUBLE_WINDOW_MS",
				"fish_sequence_key_delay_ms",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			content, err := shellScripts.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", tt.path, err)
			}
			script := string(content)

			for _, seq := range tt.sequences {
				if !strings.Contains(script, seq) {
					t.Errorf("%s missing double-Up sequence binding %q", tt.path, seq)
				}
			}
			for _, marker := range tt.timeouts {
				if !strings.Contains(script, marker) {
					t.Errorf("%s missing timeout support marker %q", tt.path, marker)
				}
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
	if !strings.Contains(script, `bind -x '"\C-x\C-h": _clai_tui_picker_open'`) {
		t.Error("bash script missing bind -x for \\C-x\\C-h")
	}
}

// TestInitPlaceholderReplacement verifies that init.go replaces all
// shell placeholders used by init scripts.
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
	if !strings.Contains(script, "{{CLAI_UP_ARROW_TRIGGER}}") {
		t.Fatal("zsh script missing {{CLAI_UP_ARROW_TRIGGER}} placeholder")
	}
	if !strings.Contains(script, "{{CLAI_UP_ARROW_DOUBLE_WINDOW_MS}}") {
		t.Fatal("zsh script missing {{CLAI_UP_ARROW_DOUBLE_WINDOW_MS}} placeholder")
	}

	// Simulate the replacement that init.go performs.
	replaced := strings.ReplaceAll(script, "{{CLAI_SESSION_ID}}", "test-session-id")
	replaced = strings.ReplaceAll(replaced, "{{CLAI_UP_ARROW_HISTORY}}", "false")
	replaced = strings.ReplaceAll(replaced, "{{CLAI_UP_ARROW_TRIGGER}}", "double")
	replaced = strings.ReplaceAll(replaced, "{{CLAI_UP_ARROW_DOUBLE_WINDOW_MS}}", "250")

	if strings.Contains(replaced, "{{CLAI_UP_ARROW_HISTORY}}") {
		t.Error("placeholder {{CLAI_UP_ARROW_HISTORY}} not replaced")
	}
	if strings.Contains(replaced, "{{CLAI_UP_ARROW_TRIGGER}}") {
		t.Error("placeholder {{CLAI_UP_ARROW_TRIGGER}} not replaced")
	}
	if strings.Contains(replaced, "{{CLAI_UP_ARROW_DOUBLE_WINDOW_MS}}") {
		t.Error("placeholder {{CLAI_UP_ARROW_DOUBLE_WINDOW_MS}} not replaced")
	}
	if strings.Contains(replaced, "{{CLAI_SESSION_ID}}") {
		t.Error("placeholder {{CLAI_SESSION_ID}} not replaced")
	}
	if !strings.Contains(replaced, "CLAI_UP_ARROW_HISTORY:=false") {
		t.Error("expected CLAI_UP_ARROW_HISTORY:=false after replacement")
	}
	if !strings.Contains(replaced, "CLAI_UP_ARROW_TRIGGER:=double") {
		t.Error("expected CLAI_UP_ARROW_TRIGGER:=double after replacement")
	}
	if !strings.Contains(replaced, "CLAI_UP_ARROW_DOUBLE_WINDOW_MS:=250") {
		t.Error("expected CLAI_UP_ARROW_DOUBLE_WINDOW_MS:=250 after replacement")
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

// TestShellScripts_FeedbackBindings verifies that all shell scripts contain
// feedback calls using `clai suggest-feedback` for accepted, dismissed, and
// (where applicable) edited actions.
func TestShellScripts_FeedbackBindings(t *testing.T) {
	shells := []struct {
		name     string
		path     string
		required []string
	}{
		{
			"zsh", "shell/zsh/clai.zsh",
			[]string{
				"suggest-feedback --action=accepted",
				"suggest-feedback --action=dismissed",
				"suggest-feedback --action=edited",
			},
		},
		{
			"bash", "shell/bash/clai.bash",
			[]string{
				"suggest-feedback --action=accepted",
				"suggest-feedback --action=dismissed",
			},
		},
		{
			"fish", "shell/fish/clai.fish",
			[]string{
				"suggest-feedback --action=accepted",
				"suggest-feedback --action=dismissed",
			},
		},
	}

	for _, sh := range shells {
		t.Run(sh.name, func(t *testing.T) {
			content, err := shellScripts.ReadFile(sh.path)
			if err != nil {
				t.Fatalf("Failed to read %s: %v", sh.path, err)
			}
			script := string(content)

			for _, req := range sh.required {
				if !strings.Contains(script, req) {
					t.Errorf("%s missing feedback binding: %q", sh.path, req)
				}
			}
		})
	}
}

// TestZshScript_FeedbackTracksLastAccepted verifies that the zsh script
// tracks the last accepted suggestion for edit detection.
func TestZshScript_FeedbackTracksLastAccepted(t *testing.T) {
	content, err := shellScripts.ReadFile("shell/zsh/clai.zsh")
	if err != nil {
		t.Fatalf("Failed to read zsh script: %v", err)
	}
	script := string(content)

	// Must have _AI_LAST_ACCEPTED state variable
	if !strings.Contains(script, "_AI_LAST_ACCEPTED") {
		t.Error("zsh script missing _AI_LAST_ACCEPTED state variable for edit tracking")
	}

	// Must clear _AI_LAST_ACCEPTED on accept-line
	body := extractFunctionBody(script, "_ai_voice_accept_line")
	if body == "" {
		t.Fatal("_ai_voice_accept_line() not found in zsh script")
	}
	if !strings.Contains(body, `_AI_LAST_ACCEPTED=""`) {
		t.Error("_ai_voice_accept_line() should clear _AI_LAST_ACCEPTED after checking for edits")
	}
}
