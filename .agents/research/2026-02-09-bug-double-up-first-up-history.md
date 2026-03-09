# Bug Report: Double-Up Trigger Replaced Prompt On First Up

**Date:** 2026-02-09  
**Severity:** medium  
**Status:** fixed

## Symptom
With `history.up_arrow_trigger=double`, the first Up arrow immediately moved history instead of waiting for the configured double-up window.

## Expected Behavior
In double mode:
- first Up should wait for `up_arrow_double_window_ms`
- if second Up arrives in that window: open picker
- if not: perform normal single-Up history behavior

## Root Cause
The initial implementation used timestamp comparison in each shell binding, so first Up executed history immediately and only second Up opened picker.

## Fix
Switched to shell-native multi-key sequence detection:
- bind single Up separately from Up+Up
- use shell sequence timeout (`KEYTIMEOUT` / `keyseq-timeout` / `fish_sequence_key_delay_ms`) derived from `CLAI_UP_ARROW_DOUBLE_WINDOW_MS`
- in double mode, single Up is delayed by shell key-sequence resolution and only executed if no second Up arrives.

## Validation
- `bash -n internal/cmd/shell/bash/clai.bash`
- `zsh -n internal/cmd/shell/zsh/clai.zsh`
- `fish -n internal/cmd/shell/fish/clai.fish`
- `go test ./internal/cmd -run 'TestShellScripts_UpArrow|TestShellScripts_DoubleUpSequenceSupport|TestInitPlaceholderReplacement'`
- `go test ./internal/cmd ./internal/picker ./cmd/clai-picker`

## Files Changed
- `internal/cmd/shell/zsh/clai.zsh`
- `internal/cmd/shell/bash/clai.bash`
- `internal/cmd/shell/fish/clai.fish`
- `internal/cmd/init_test.go`
