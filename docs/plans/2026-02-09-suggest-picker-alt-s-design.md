# Design: Suggestions Picker Via Alt/Opt+S

Date: 2026-02-09

## Goals

- Keep inline ghosttext suggestions (best single suggestion) in all shells.
- Replace the existing Tab-driven suggestions menu behavior with a dedicated keybinding:
  - `Alt/Opt+S` opens a **TUI suggestions picker**.
- Keep Tab as native completion everywhere (zsh/bash/fish).
- Ensure the UI behaves consistently across shells.
- Provide brief feedback when a picker keybinding does nothing (missing `clai-picker`, daemon/socket error, non-TTY, etc.), throttled to once per 5 seconds.
- Add daemon-side RPC access logging (timestamp, method, latency, status/error code), without logging raw command text.

## Non-Goals

- Do not implement the spec’s persistent shim lifecycle in this change.
- Do not change the underlying suggestion ranking logic; only presentation and interaction.

## UX

### Ghosttext

- Ghosttext continues to show the best single suggestion available.
- Source order remains daemon-first (smart) and history fallback when daemon is unavailable.

### Suggestions Picker (Alt/Opt+S)

- Pressing `Alt/Opt+S` launches `clai-picker suggest`.
- Picker input starts with the current prompt buffer as `--query`.
- Empty buffer opens the picker and shows next-step suggestions (daemon suggest with empty prefix).
- If the picker exits with:
  - `0`: selected command replaces the current prompt buffer.
  - non-zero: no changes to prompt buffer.

### History Picker Feedback

- When the shell is configured to open the TUI history picker (or the user presses the explicit TUI shortcut), and it cannot be opened, show a brief error message.
- Do not spam on normal history navigation; throttle messages to once per 5 seconds.

## Cross-Shell Keybindings

Bind both variants for macOS portability:
- Meta sequence: `ESC + s` ("Option as Meta")
- Literal character: `ß` (common Option+S output)

### zsh

- Keep Tab mapped to `expand-or-complete`.
- Bind `\es` and `ß` to a widget that runs `clai-picker suggest`.

### fish

- Keep Tab mapped to `complete`.
- Bind `\es` and `ß` in default/insert/visual modes to a function that runs `clai-picker suggest`.

### bash

- Remove clai Tab completion/menu enhancement.
- Add `bind -x` handler for an internal Ctrl sequence triggered by `\es`.
- For literal `ß` (multi-byte), translate it via a macro to a bindable Ctrl sequence (same pattern used for Option+H today).

## `clai-picker suggest`

- Add a `suggest` subcommand to `clai-picker`.
- It uses the existing BubbleTea picker model with a new provider that calls the daemon `Suggest` RPC.
- Output is the selected command text on stdout.
- Supports two display modes controlled by config:
  - `detailed` (default): command + source + risk + score + reasons/description.
  - `compact`: command + source + risk.

## Configuration

- Add a config key to control suggest picker display:
  - `suggestions.picker_view: detailed|compact` (default `detailed`).

## Feedback Messages

- When `Alt/Opt+S` or explicit history TUI trigger fails:
  - show `clai: <brief reason>`.
  - throttle to once per 5 seconds per shell session.

## Daemon RPC Access Logs

- Add a gRPC unary interceptor in the daemon server.
- Log one line per RPC call with:
  - timestamp (via slog handler)
  - method name
  - latency
  - grpc status code
- Do not log raw command text or other sensitive request payload.

## Testing

- Update shell init tests to assert:
  - Tab bindings are restored to native completion.
  - Alt/Opt+S bindings exist for zsh/fish/bash.
- Add unit tests for `clai-picker suggest` flag parsing and provider plumbing.
- Add picker provider tests with a mock gRPC server for `Suggest`.
- Add daemon interceptor tests verifying access log lines are emitted without command text.
