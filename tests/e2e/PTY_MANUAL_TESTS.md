# PTY Wrapper Manual Test Plan

**Spec:** `specs/tech_pty_design.md` v2.3
**Binary:** `clai-wrap`
**Date:** 2026-02-06

---

## Prerequisites

- `clai-wrap` binary built (`cargo build --release` in `clai-wrap/`)
- macOS or Linux with Bash, Zsh, and Fish installed
- A second terminal open for sending signals (`kill -<SIG> <pid>`)
- Optional: SSH server on localhost for SSH tests
- Optional: `clai-daemon` running for daemon/suggestion tests

### Notation

- **PASS**: Observed behavior matches expected
- **FAIL**: Observed behavior does NOT match expected
- **SKIP**: Test cannot be run in current environment (note reason)
- `$WRAP` = path to `clai-wrap` binary
- `$PID` = PID of the running `clai-wrap` process (find with `ps aux | grep clai-wrap`)

---

## 1. Launching & Shell Spawning (Spec 6.1)

### T1.1 Default shell launch

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` with no arguments |
| **Expected** | User's login shell (`$SHELL`) starts. Prompt appears. Shell is interactive. |
| **Verify** | Type `echo $0` -- shows shell name. Type `echo $CLAI_WRAP` -- shows `1`. |
| **Result** | |

### T1.2 Custom shell via `--shell`

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --shell /bin/bash` |
| **Expected** | Bash starts regardless of user's default shell. |
| **Verify** | `echo $0` shows `bash` or `-bash`. |
| **Result** | |

### T1.3 Login shell mode

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --login-shell` |
| **Expected** | Shell starts as login shell. Login profile files sourced. |
| **Verify** | In Bash: `shopt login_shell` shows `on`. In Zsh: check `$-` contains `l`. |
| **Result** | |

### T1.4 Environment passthrough

| Field | Value |
|-------|-------|
| **Steps** | 1. `export MY_TEST_VAR=hello` 2. Run `$WRAP` |
| **Expected** | Inside wrapper, `echo $MY_TEST_VAR` shows `hello`. |
| **Result** | |

### T1.5 CLAI_WRAP environment variable set

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` |
| **Expected** | `echo $CLAI_WRAP` outputs `1`. |
| **Result** | |

### T1.6 Nested wrapper detection

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Inside, run `$WRAP` again |
| **Expected** | Warning about nested instance printed to stderr. |
| **Result** | |

---

## 2. Raw Mode & Terminal Ownership (Spec 6.2)

### T2.1 Raw mode engaged on start

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Type characters |
| **Expected** | Characters appear normally. Shell input/output works. No double-echo or missing echo. |
| **Result** | |

### T2.2 Terminal restored on clean exit

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Type `exit` 3. Back in parent shell |
| **Expected** | Terminal behaves normally: echo on, cursor visible, line editing works. `stty` shows sane settings. |
| **Result** | |

### T2.3 Terminal restored after crash/kill

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` and note PID 2. From another terminal: `kill -9 $PID` |
| **Expected** | Terminal may be disrupted. Run `stty sane` or `$WRAP reset-terminal` to verify recovery command works. |
| **Result** | |

### T2.4 Non-TTY stdin (piped)

| Field | Value |
|-------|-------|
| **Steps** | 1. `echo "echo hello" | $WRAP` |
| **Expected** | Hotkey detection disabled. `hello` printed. Wrapper exits cleanly. Warning on stderr about non-TTY stdin. |
| **Result** | |

### T2.5 Non-TTY stdout (piped)

| Field | Value |
|-------|-------|
| **Steps** | 1. `$WRAP > /tmp/wrap-out.txt` then type `echo test` + Enter + `exit` |
| **Expected** | Picker UI disabled. Output captured to file. `cat /tmp/wrap-out.txt` shows shell output. |
| **Result** | |

### T2.6 All non-TTY without flag

| Field | Value |
|-------|-------|
| **Steps** | 1. `echo "echo hi" | $WRAP > /dev/null 2>&1` |
| **Expected** | Exits with error about no TTY detected. |
| **Result** | |

### T2.7 `--force-non-tty` mode

| Field | Value |
|-------|-------|
| **Steps** | 1. `echo "echo hi" | $WRAP --force-non-tty > /tmp/test.txt 2>&1` |
| **Expected** | Runs as pure passthrough. No error. Output in file. |
| **Result** | |

---

## 3. Basic I/O Passthrough (Spec 3.5)

### T3.1 Simple echo

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `echo "hello world"` |
| **Expected** | `hello world` appears on screen. |
| **Result** | |

### T3.2 Interactive input

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `read -p "Name: " name && echo "Hi $name"` 3. Type `Claude` + Enter |
| **Expected** | Prompt shows `Name: `, accepts input, prints `Hi Claude`. |
| **Result** | |

### T3.3 ANSI color passthrough

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `ls --color=always` (or `printf '\033[31mRed\033[0m\n'`) |
| **Expected** | Colors display correctly. No garbled escape sequences. |
| **Result** | |

### T3.4 Tab completion works

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Type `ech` then press Tab |
| **Expected** | Shell completes to `echo`. Tab completion works normally. |
| **Result** | |

### T3.5 Line editing (arrow keys, backspace, Ctrl-A/E)

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Type `echo test` 3. Press Left arrow 4 times 4. Type `my_` 5. Press Enter |
| **Expected** | Outputs `echo my_test`. All readline/ZLE editing keys work. |
| **Result** | |

### T3.6 Ctrl-C interrupts running command

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `sleep 60` 3. Press Ctrl-C |
| **Expected** | Sleep interrupted. Prompt returns immediately. |
| **Result** | |

### T3.7 Ctrl-D sends EOF

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. At empty prompt, press Ctrl-D |
| **Expected** | Shell exits (if configured to exit on EOF). Wrapper exits cleanly. Terminal restored. |
| **Result** | |

---

## 4. Signal Handling (Spec 6.3, 6.9)

### T4.1 SIGWINCH / Resize propagation

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Drag terminal window to resize 3. `stty size` |
| **Expected** | Output matches new terminal dimensions. |
| **Result** | |

### T4.2 Rapid resize debouncing

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Rapidly drag terminal corner to resize many times in ~1 second 3. Wait 200ms 4. `stty size` |
| **Expected** | Final size matches actual window. No crash. No leftover resize artifacts. |
| **Result** | |

### T4.3 SIGINT (Ctrl-C)

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `sleep 60` 3. Ctrl-C |
| **Expected** | Command interrupted. Shell prompt returns. Wrapper continues running. |
| **Result** | |

### T4.4 SIGTERM

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` (note PID) 2. From another terminal: `kill $PID` |
| **Expected** | Wrapper exits cleanly. Terminal restored. Child shell terminated. |
| **Result** | |

### T4.5 SIGHUP

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` (note PID) 2. From another terminal: `kill -HUP $PID` |
| **Expected** | Clean shutdown. Terminal state restored. |
| **Result** | |

### T4.6 SIGTSTP / SIGCONT (suspend/resume)

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` from a parent shell 2. `echo before` 3. Press Ctrl-Z 4. Observe parent shell prompt 5. `fg` to resume 6. `echo after` |
| **Expected** | Wrapper suspends. On resume, raw mode re-entered. Both `before` and `after` work. No terminal corruption. |
| **Result** | |

### T4.7 SIGPIPE ignored

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `yes | head -5` |
| **Expected** | Prints 5 lines of `y`. No crash or error from SIGPIPE. |
| **Result** | |

### T4.8 Child exit code passthrough

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Type `exit 42` 3. In parent shell: `echo $?` |
| **Expected** | Exit code is `42`. |
| **Result** | |

### T4.9 Child killed by signal

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Find child shell PID: `echo $$` 3. From another terminal: `kill -9 <child_pid>` |
| **Expected** | Wrapper exits with code 128 + 9 = 137 (POSIX convention). Terminal restored. |
| **Result** | |

---

## 5. Hotkey Detection (Spec 6.4)

### T5.1 Default hotkey chord opens picker

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Press `Ctrl-\` then `h` (within 500ms) |
| **Expected** | Picker UI opens (alt-screen). History items visible. |
| **Result** | |

### T5.2 Hotkey timeout forwards bytes

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Press `Ctrl-\` 3. Wait >600ms 4. Press `h` |
| **Expected** | No picker opens. The `h` character appears at shell prompt (along with forwarded `0x1C` byte). |
| **Result** | |

### T5.3 Custom hotkey via `--hotkey`

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --hotkey="ctrl-] h"` 2. Press `Ctrl-]` then `h` |
| **Expected** | Picker UI opens. |
| **Result** | |

### T5.4 Hotkey does not eat unrelated bytes

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Rapidly type `echo hello world` (normal typing speed) |
| **Expected** | Full text appears at prompt. No characters lost or eaten. |
| **Result** | |

### T5.5 SIGQUIT not triggered by Ctrl-\

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Press `Ctrl-\` |
| **Expected** | No core dump. No crash. Byte `0x1C` handled as hotkey prefix. |
| **Result** | |

---

## 6. Picker UI (Spec 6.5)

### T6.1 Picker opens on hotkey

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Run a few commands (`ls`, `pwd`, `echo test`) 3. Trigger hotkey |
| **Expected** | Alt-screen activates. Picker shows history items. Cursor hidden. |
| **Result** | |

### T6.2 Picker incremental search

| Field | Value |
|-------|-------|
| **Steps** | 1. Open picker 2. Type `ec` |
| **Expected** | List filters to show only commands containing `ec` (e.g., `echo`). |
| **Result** | |

### T6.3 Arrow key navigation

| Field | Value |
|-------|-------|
| **Steps** | 1. Open picker 2. Press Down arrow 3 times 3. Press Up arrow once |
| **Expected** | Selection highlight moves. Currently selected item visually distinct. |
| **Result** | |

### T6.4 Enter selects and closes

| Field | Value |
|-------|-------|
| **Steps** | 1. Open picker 2. Navigate to a command 3. Press Enter |
| **Expected** | Picker closes. Alt-screen deactivated. Selected command appears at shell prompt. |
| **Result** | |

### T6.5 Escape cancels

| Field | Value |
|-------|-------|
| **Steps** | 1. Open picker 2. Press Escape |
| **Expected** | Picker closes. No text inserted. Shell returns to prior state. |
| **Result** | |

### T6.6 Picker opens in <100ms

| Field | Value |
|-------|-------|
| **Steps** | 1. Trigger hotkey and observe latency |
| **Expected** | UI appears near-instantly. Perceived latency <100ms on typical hardware. |
| **Result** | |

### T6.7 Resize during picker

| Field | Value |
|-------|-------|
| **Steps** | 1. Open picker 2. Drag-resize the terminal window 3. Close picker |
| **Expected** | Picker redraws at new size. After close, `stty size` matches new dimensions. |
| **Result** | |

### T6.8 Cursor restored after picker

| Field | Value |
|-------|-------|
| **Steps** | 1. Open picker 2. Close with Escape 3. Type normally |
| **Expected** | Cursor is visible. Blinking cursor at prompt. No invisible cursor. |
| **Result** | |

---

## 7. Output Buffering During Picker (Spec 6.6)

### T7.1 Output buffered while picker open

| Field | Value |
|-------|-------|
| **Steps** | 1. `sleep 1 && echo "DELAYED" &` 2. Immediately open picker 3. Wait 2s 4. Close picker |
| **Expected** | `DELAYED` does not appear while picker is open. Appears after close. |
| **Result** | |

### T7.2 Buffered output flushed in order

| Field | Value |
|-------|-------|
| **Steps** | 1. `for i in 1 2 3 4 5; do sleep 0.5 && echo "LINE_$i"; done &` 2. Open picker immediately 3. Wait 4s 4. Close picker |
| **Expected** | Lines appear in order: LINE_1, LINE_2, LINE_3, LINE_4, LINE_5. |
| **Result** | |

### T7.3 High-output stress test

| Field | Value |
|-------|-------|
| **Steps** | 1. `yes \| head -100000 &` 2. Open picker 3. Wait 2s 4. Close picker |
| **Expected** | No deadlock. No crash. Wrapper remains responsive. Picker closes cleanly. |
| **Result** | |

### T7.4 Buffer overflow warning

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --buffer-cap=1024 --debug` 2. `yes \| head -100000 &` 3. Open picker 4. Wait 2s 5. Close picker |
| **Expected** | stderr (or log file) shows buffer overflow warning. `[...truncated...]` indicator may appear. |
| **Result** | |

### T7.5 PTY read thread never blocks

| Field | Value |
|-------|-------|
| **Steps** | 1. Open picker 2. From within shell (before picker): `dd if=/dev/zero bs=1M count=10 \| cat > /dev/null &` 3. Wait 5s 4. Close picker |
| **Expected** | No deadlock. Wrapper stays responsive. Background job completes. |
| **Result** | |

---

## 8. Selection Injection (Spec 6.7)

### T8.1 Selected command inserted at prompt

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `echo test123` to seed history 2. Open picker 3. Select `echo test123` 4. Observe prompt |
| **Expected** | `echo test123` appears at the prompt, ready to execute. |
| **Result** | |

### T8.2 Execute-on-select mode

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --execute-on-select` 2. Seed history 3. Open picker 4. Select a command |
| **Expected** | Command is inserted AND executed immediately (newline appended). Output appears. |
| **Result** | |

### T8.3 Bracketed paste when available

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` 2. Open picker 3. Select a command 4. Check debug logs |
| **Expected** | If shell emitted `\x1b[?2004h` (bracketed paste enable), selection wrapped in `\x1b[200~...\x1b[201~`. |
| **Result** | |

### T8.4 Raw bytes fallback

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` inside an environment where bracketed paste is not enabled 2. Select from picker 3. Check debug logs |
| **Expected** | Raw bytes sent (no bracketed paste wrapper). Log indicates fallback. |
| **Result** | |

### T8.5 UTF-8 content injected correctly

| Field | Value |
|-------|-------|
| **Steps** | 1. Seed history with `echo "Hello"` (or CJK chars) 2. Select from picker 3. Execute |
| **Expected** | UTF-8 characters preserved. No encoding corruption. |
| **Result** | |

---

## 9. Full-Screen Application Interop (Spec 1.1 Goal 2, Spec 11.4)

### T9.1 vim

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `vim -u NONE` 3. Type `i` to enter insert mode, type some text 4. Press Escape 5. Trigger hotkey to open picker 6. Close picker with Escape 7. Continue editing in vim 8. `:q!` to exit |
| **Expected** | vim state preserved after picker close. No terminal corruption. Cursor position correct. |
| **Result** | |

### T9.2 less

| Field | Value |
|-------|-------|
| **Steps** | 1. `seq 1 200 \| $WRAP` is not right -- run `$WRAP` then `seq 1 200 \| less` 2. Scroll down 3. Open picker 4. Close picker 5. Continue scrolling, press `q` |
| **Expected** | less continues normally. No display artifacts. |
| **Result** | |

### T9.3 top / htop

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `top` (or `htop` if available) 3. Open picker 4. Close picker 5. Observe top/htop continues refreshing 6. `q` to exit |
| **Expected** | top/htop resumes correctly. No frozen display. |
| **Result** | |

### T9.4 man page

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `man ls` 3. Scroll down 4. Open picker 5. Close picker 6. `q` to exit |
| **Expected** | Man page navigation works. Prompt returns cleanly. |
| **Result** | |

---

## 10. SSH Session (Spec 11.4)

> **Prerequisite:** SSH server running on localhost (or another reachable host).

### T10.1 SSH works inside wrapper

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `ssh localhost` (or target host) 3. Run commands on remote |
| **Expected** | SSH session fully functional. Remote commands work. |
| **Result** | |

### T10.2 Hotkey triggers local picker during SSH

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. SSH to remote 3. Trigger hotkey |
| **Expected** | Local picker opens (not remote). Shows local history. |
| **Result** | |

### T10.3 Selection injected into SSH session

| Field | Value |
|-------|-------|
| **Steps** | 1. SSH into remote 2. Open picker 3. Select a command |
| **Expected** | Command text appears at remote prompt (sent as keystrokes/paste through PTY). |
| **Result** | |

### T10.4 SSH session stable after picker

| Field | Value |
|-------|-------|
| **Steps** | 1. SSH into remote 2. Open picker 3. Close with Escape 4. Run `echo still_working` on remote |
| **Expected** | Remote session continues normally. No disconnection or corruption. |
| **Result** | |

### T10.5 SSH exit returns to local shell

| Field | Value |
|-------|-------|
| **Steps** | 1. SSH into remote 2. `exit` 3. Observe local prompt |
| **Expected** | Local shell prompt returns. Wrapper still active. |
| **Result** | |

---

## 11. Privacy & Output Capture (Spec 7)

### T11.1 Denylist: ssh pauses capture

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` 2. `ssh localhost` 3. Run commands 4. `exit` 5. Check debug logs |
| **Expected** | Logs show capture paused when SSH started, resumed after exit. |
| **Result** | |

### T11.2 Denylist: vim pauses capture

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` 2. `vim -u NONE` 3. `:q!` 4. Check debug logs |
| **Expected** | Logs show `vim` detected on denylist. Capture paused during vim session. |
| **Result** | |

### T11.3 Denylist: sudo pauses capture

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` 2. `sudo echo test` 3. Check debug logs |
| **Expected** | Logs show `sudo` detected. Capture paused. |
| **Result** | |

### T11.4 Capture resumes after sensitive command

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` 2. `vim -u NONE -c 'q!'` 3. `echo should_be_captured` 4. Check debug logs |
| **Expected** | `echo should_be_captured` output appears in capture logs. |
| **Result** | |

### T11.5 Echo-gap heuristic (password prompt)

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` 2. Run a program that prompts for password with echo disabled (e.g., `ssh` to a host that asks for password) 3. Check debug logs |
| **Expected** | Logs show secure mode entered due to echo gap. Input scrubbed from buffer. |
| **Result** | |

### T11.6 Ring buffer stores output

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` 2. `echo "unique_capture_test_string"` 3. Check debug logs / daemon data |
| **Expected** | Output captured in ring buffer. `unique_capture_test_string` present. |
| **Result** | |

---

## 12. OSC 133 Shell Integration (Spec 8)

### T12.1 OSC 133 injection -- Bash

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --shell /bin/bash --debug` 2. Run a command 3. Check debug logs |
| **Expected** | Logs show OSC 133 sequences detected (PROMPT -> INPUT -> OUTPUT -> FINISHED transitions). |
| **Result** | |

### T12.2 OSC 133 injection -- Zsh

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --shell /bin/zsh --debug` 2. Run a command 3. Check debug logs |
| **Expected** | OSC 133 sequences detected. State transitions logged. |
| **Result** | |

### T12.3 OSC 133 -- Fish (native detection)

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --shell fish --debug` (Fish >= 3.6) 2. Run a command 3. Check debug logs |
| **Expected** | Logs show native OSC 133 detected. Injection skipped for Fish >= 3.6. |
| **Result** | |

### T12.4 Passthrough mode fallback

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --shell /bin/sh --debug` 2. Wait 1 second 3. Check debug logs |
| **Expected** | Log shows "passthrough mode" because `/bin/sh` does not emit OSC 133. Warning printed to stderr. |
| **Result** | |

### T12.5 Split-packet OSC 133 handling

| Field | Value |
|-------|-------|
| **Steps** | This is primarily a unit test concern, but verify: 1. Run `$WRAP --debug` 2. Run several rapid commands 3. Check for parser errors in logs |
| **Expected** | No parser errors. OSC 133 detected even when escape sequences span read boundaries. |
| **Result** | |

### T12.6 Shell injection does not break user config

| Field | Value |
|-------|-------|
| **Steps** | 1. Set a custom PS1 in `~/.bashrc` (e.g., `export PS1="CUSTOM> "`) 2. Run `$WRAP --shell /bin/bash` 3. Observe prompt |
| **Expected** | Custom prompt (`CUSTOM>`) appears. User's `.bashrc` fully sourced. |
| **Result** | |

### T12.7 Zsh ZDOTDIR handling

| Field | Value |
|-------|-------|
| **Steps** | 1. Ensure `~/.zshrc` has custom settings (aliases, prompt, etc.) 2. Run `$WRAP --shell /bin/zsh` 3. Check aliases work, prompt is custom |
| **Expected** | All user Zsh configuration loaded correctly. ZDOTDIR reset to `$HOME` after injection. |
| **Result** | |

---

## 13. Resize Handling (Spec 6.8)

### T13.1 Basic resize propagation

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Resize terminal 3. `stty size` |
| **Expected** | Reported size matches actual terminal dimensions. |
| **Result** | |

### T13.2 Resize during picker

| Field | Value |
|-------|-------|
| **Steps** | 1. Open picker 2. Resize terminal 3. Observe picker redraws 4. Close picker 5. `stty size` |
| **Expected** | Picker layout updates. After close, child PTY has correct final size. |
| **Result** | |

### T13.3 Debounce trailing edge

| Field | Value |
|-------|-------|
| **Steps** | 1. Rapidly resize 10+ times in 500ms 2. Wait 200ms 3. `stty size` |
| **Expected** | Final size matches actual window. Not an intermediate size. At most ~20 propagations per second. |
| **Result** | |

---

## 14. Daemon Connection (Spec 3.2, 3.4)

### T14.1 Standalone mode when no daemon

| Field | Value |
|-------|-------|
| **Steps** | 1. Ensure no `clai-daemon` running 2. Run `$WRAP` |
| **Expected** | One-time warning on stderr: "Daemon unavailable, running in standalone mode". PTY works. Picker works with local history. |
| **Result** | |

### T14.2 `--no-daemon` flag

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --no-daemon` |
| **Expected** | No daemon connection attempted. No warning about daemon. PTY + picker work. |
| **Result** | |

### T14.3 Daemon connection timeout (500ms)

| Field | Value |
|-------|-------|
| **Steps** | 1. Point `--daemon-socket` at a socket that accepts but never responds 2. Run `$WRAP` and time startup |
| **Expected** | Wrapper starts within ~500ms. Falls back to standalone mode. |
| **Result** | |

### T14.4 Stale socket cleanup

| Field | Value |
|-------|-------|
| **Steps** | 1. Create a stale socket file (e.g., `touch /tmp/clai-test.sock`) 2. Run `$WRAP --daemon-socket=/tmp/clai-test.sock --debug` 3. Check debug logs |
| **Expected** | Log shows stale socket detected and cleaned (ECONNREFUSED handling). |
| **Result** | |

### T14.5 Standalone feature matrix

| Field | Value |
|-------|-------|
| **Steps** | Run `$WRAP --no-daemon` and verify each feature |
| **Expected** | PTY passthrough: works. Hotkey: works. Picker: works (local history). Output capture: disabled. AI suggestions: disabled. |
| **Result** | |

---

## 15. Color Detection (Spec 6.5)

### T15.1 `NO_COLOR` disables colors

| Field | Value |
|-------|-------|
| **Steps** | 1. `NO_COLOR=1 $WRAP` 2. Open picker |
| **Expected** | Picker renders without any ANSI color codes. Monochrome. |
| **Result** | |

### T15.2 `COLORTERM=truecolor`

| Field | Value |
|-------|-------|
| **Steps** | 1. `COLORTERM=truecolor $WRAP --debug` 2. Open picker 3. Check debug logs |
| **Expected** | Logs indicate 24-bit color mode. Picker uses rich colors. |
| **Result** | |

### T15.3 `TERM=dumb` disables picker

| Field | Value |
|-------|-------|
| **Steps** | 1. `TERM=dumb $WRAP` 2. Trigger hotkey |
| **Expected** | Picker does not open. Warning logged. Wrapper operates as passthrough with hotkey. |
| **Result** | |

### T15.4 256-color detection

| Field | Value |
|-------|-------|
| **Steps** | 1. `TERM=xterm-256color $WRAP --debug` 2. Open picker 3. Check debug logs |
| **Expected** | Logs indicate 256-color mode detected. |
| **Result** | |

---

## 16. Assistant Comment / Suggestions (Spec 9)

> **Prerequisite:** `clai-daemon` running with AI provider configured.

### T16.1 Suggestion after failed command

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` (with daemon) 2. `git psuh` 3. Wait for AI suggestion (~2-5s) |
| **Expected** | After new prompt appears, a comment line like `# clai suggestion: git push` is shown. |
| **Result** | |

### T16.2 Correct comment syntax per shell

| Field | Value |
|-------|-------|
| **Steps** | 1. Test in Bash: comment prefix is `#` 2. Test in Zsh: comment prefix is `#` 3. Test in Fish: comment prefix is `#` |
| **Expected** | Each shell uses appropriate comment syntax. |
| **Result** | |

### T16.3 No suggestion after successful command

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` (with daemon) 2. `echo success` (exit code 0) 3. Wait 3s |
| **Expected** | No suggestion comment appears. Prompt returns normally. |
| **Result** | |

---

## 17. Encoding & Unicode (Spec 6.10, 6.5)

### T17.1 UTF-8 passthrough

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `echo "Hello World"` |
| **Expected** | UTF-8 text rendered correctly. Emojis, accented characters, etc. |
| **Result** | |

### T17.2 CJK wide characters in picker

| Field | Value |
|-------|-------|
| **Steps** | 1. Run command with CJK: `echo "Hello"` 2. Open picker 3. Search for CJK text |
| **Expected** | CJK characters occupy 2 columns. Alignment correct. No overlapping. |
| **Result** | |

### T17.3 Emoji in picker

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `echo "deploy"` 2. Open picker |
| **Expected** | Emoji renders correctly. Column width calculated correctly. |
| **Result** | |

### T17.4 Non-UTF-8 bytes don't crash

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `printf '\xff\xfe\xfd'` 3. Continue typing |
| **Expected** | No crash. Invalid bytes handled (lossy conversion to U+FFFD). Shell continues working. |
| **Result** | |

### T17.5 Locale warning for non-UTF-8

| Field | Value |
|-------|-------|
| **Steps** | 1. `LANG=C $WRAP --debug` 2. Check debug logs |
| **Expected** | Warning about non-UTF-8 locale logged. Wrapper continues. |
| **Result** | |

---

## 18. CLI Options (Spec 13)

### T18.1 `--debug` enables verbose logging

| Field | Value |
|-------|-------|
| **Steps** | 1. `$WRAP --debug 2>/tmp/wrap-debug.log` 2. Run some commands 3. `exit` 4. Check `/tmp/wrap-debug.log` |
| **Expected** | Log file contains DEBUG-level messages: shell path, mode, buffer cap, signal events. |
| **Result** | |

### T18.2 `CLAI_DEBUG=1` alternative

| Field | Value |
|-------|-------|
| **Steps** | 1. `CLAI_DEBUG=1 $WRAP 2>/tmp/wrap-debug.log` |
| **Expected** | Same debug output as `--debug`. |
| **Result** | |

### T18.3 `--buffer-cap` customization

| Field | Value |
|-------|-------|
| **Steps** | 1. `$WRAP --buffer-cap=1048576 --debug` 2. Check debug logs |
| **Expected** | Logs show buffer capacity set to 1048576 (1 MiB). |
| **Result** | |

### T18.4 `--no-ui` disables picker

| Field | Value |
|-------|-------|
| **Steps** | 1. `$WRAP --no-ui` 2. Trigger hotkey |
| **Expected** | Picker does not open. Output capture still active (if daemon available). |
| **Result** | |

### T18.5 `--history-file` custom history

| Field | Value |
|-------|-------|
| **Steps** | 1. Create `/tmp/test_history` with some commands 2. `$WRAP --history-file=/tmp/test_history` 3. Open picker |
| **Expected** | Picker shows commands from custom history file. |
| **Result** | |

### T18.6 `version` subcommand

| Field | Value |
|-------|-------|
| **Steps** | 1. `$WRAP version` |
| **Expected** | Prints version string. Exits immediately. No shell spawned. |
| **Result** | |

### T18.7 `reset-terminal` subcommand

| Field | Value |
|-------|-------|
| **Steps** | 1. Deliberately corrupt terminal (e.g., `printf '\033[?1049h'` to enter alt-screen) 2. `$WRAP reset-terminal` |
| **Expected** | Terminal reset to sane state: cursor visible, normal screen, echo on. |
| **Result** | |

---

## 19. History Parsing (Spec 4.5)

### T19.1 Bash history (plain)

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --shell /bin/bash` 2. Run some commands 3. Open picker |
| **Expected** | Recent Bash commands appear in picker. |
| **Result** | |

### T19.2 Bash history (timestamped)

| Field | Value |
|-------|-------|
| **Steps** | 1. Ensure `HISTTIMEFORMAT` is set in `.bashrc` 2. Run `$WRAP --shell /bin/bash` 3. Open picker |
| **Expected** | Commands parsed correctly despite `#timestamp` lines in history file. |
| **Result** | |

### T19.3 Zsh history

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --shell /bin/zsh` 2. Open picker |
| **Expected** | Zsh history commands visible. `: timestamp:0;command` format parsed. |
| **Result** | |

### T19.4 Fish history

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --shell fish` 2. Open picker |
| **Expected** | Fish history commands visible (best-effort parsing of YAML-like format). |
| **Result** | |

---

## 20. Temp Directory Management (Spec 8.1)

### T20.1 Temp directory created

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP --debug` 2. Check `$XDG_RUNTIME_DIR/clai/` or `/tmp/clai-$UID/` |
| **Expected** | Temp directory `clai-shell-{pid}/` exists with 0700 permissions. |
| **Result** | |

### T20.2 Temp directory cleaned on exit

| Field | Value |
|-------|-------|
| **Steps** | 1. Note temp directory location during run 2. `exit` wrapper 3. Check if directory is removed |
| **Expected** | Temp directory removed on clean exit. |
| **Result** | |

### T20.3 Orphan cleanup on startup

| Field | Value |
|-------|-------|
| **Steps** | 1. Create fake orphan: `mkdir -p /tmp/clai-$(id -u)/clai-shell-99999/` 2. Run `$WRAP --debug` 3. Check debug logs |
| **Expected** | Log shows orphan detected and cleaned (PID 99999 doesn't exist). |
| **Result** | |

---

## 21. Edge Cases (Spec 17, 16.1)

### T21.1 Shell exits during picker

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `bash -c 'sleep 2 && kill -9 $$' &` (kills shell in 2s) 3. Open picker 4. Wait for shell to die |
| **Expected** | Picker closes gracefully. Message shown (e.g., "Shell exited"). Terminal restored. |
| **Result** | |

### T21.2 Very long command in history

| Field | Value |
|-------|-------|
| **Steps** | 1. Run a command >1000 characters long 2. Open picker 3. Find it in list |
| **Expected** | Command truncated in display with ellipsis. Selecting it injects full text. |
| **Result** | |

### T21.3 Nested tmux inside wrapper

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Start `tmux` 3. Run commands 4. Open picker 5. Close picker 6. `exit` tmux |
| **Expected** | tmux works. Picker works (may have higher latency). Warning about tmux may appear. |
| **Result** | |

### T21.4 Right-to-left text

| Field | Value |
|-------|-------|
| **Steps** | 1. Seed history with Arabic/Hebrew text 2. Open picker 3. Search for it |
| **Expected** | MVP: RTL not fully supported. Should not crash. Warning may be logged. |
| **Result** | |

### T21.5 Rapid shell restart

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. `exit` 3. Immediately run `$WRAP` again 4. Run commands |
| **Expected** | Second instance works fine. No stale state from first. |
| **Result** | |

---

## 22. Performance (Spec 11.6)

### T22.1 Picker latency <100ms

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Trigger hotkey 10 times (open/close) |
| **Expected** | Each open feels near-instant. p95 < 100ms. |
| **Result** | |

### T22.2 No memory growth in steady state

| Field | Value |
|-------|-------|
| **Steps** | 1. Run `$WRAP` 2. Note RSS with `ps -o rss -p $PID` 3. Run 100 commands 4. Check RSS again |
| **Expected** | Memory does not grow proportionally to number of commands. Ring buffer is bounded. |
| **Result** | |

### T22.3 I/O throughput

| Field | Value |
|-------|-------|
| **Steps** | 1. `time seq 1 1000000` outside wrapper 2. `$WRAP` then `time seq 1 1000000` inside |
| **Expected** | Overhead is minimal (<20% slower). No visible stuttering or line tearing. |
| **Result** | |

---

## 23. Cross-Shell Matrix

Run the following smoke tests in each shell:

| Test | Bash | Zsh | Fish |
|------|------|-----|------|
| Shell launches and prompt appears | | | |
| `echo hello` works | | | |
| Tab completion works | | | |
| Ctrl-C interrupts command | | | |
| Hotkey opens picker | | | |
| Picker search and select works | | | |
| Terminal restored on exit | | | |
| OSC 133 detected in debug logs | | | |

---

## Summary Checklist

| Section | Tests | Passed | Failed | Skipped |
|---------|-------|--------|--------|---------|
| 1. Launching & Shell Spawning | 6 | | | |
| 2. Raw Mode & Terminal | 7 | | | |
| 3. Basic I/O Passthrough | 7 | | | |
| 4. Signal Handling | 9 | | | |
| 5. Hotkey Detection | 5 | | | |
| 6. Picker UI | 8 | | | |
| 7. Output Buffering | 5 | | | |
| 8. Selection Injection | 5 | | | |
| 9. Full-Screen Interop | 4 | | | |
| 10. SSH Session | 5 | | | |
| 11. Privacy & Output Capture | 6 | | | |
| 12. OSC 133 Integration | 7 | | | |
| 13. Resize Handling | 3 | | | |
| 14. Daemon Connection | 5 | | | |
| 15. Color Detection | 4 | | | |
| 16. Suggestions | 3 | | | |
| 17. Encoding & Unicode | 5 | | | |
| 18. CLI Options | 7 | | | |
| 19. History Parsing | 4 | | | |
| 20. Temp Directory | 3 | | | |
| 21. Edge Cases | 5 | | | |
| 22. Performance | 3 | | | |
| 23. Cross-Shell Matrix | 8x3 | | | |
| **Total** | **~135** | | | |

---

**Tester:** ________________
**Date:** ________________
**Build:** ________________
**Platform:** ________________
