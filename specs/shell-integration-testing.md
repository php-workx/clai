# Shell Integration Manual Testing

Checklist for manually testing clai shell integrations.

## Setup

1. Source the integration: `source ~/.clai/hooks/clai.zsh` (or `.bash`, `.fish`)
2. Verify "clai loaded" message appears
3. Ensure `clai` binary is in PATH

## Test Cases

### 1. Command Suggestions

- [ ] Type a partial command (e.g., `git`) - inline ghost text appears after cursor
- [ ] Type gibberish - no suggestion should appear
- [ ] Press right arrow at end of line - suggestion is accepted into buffer
- [ ] Press Alt+Right - next token of suggestion is accepted
- [ ] Press Escape - suggestion clears (if shown)

### 2. Long Input Handling

- [ ] Type a very long prefix (20+ chars) - prefix truncates to `…`
- [ ] Suggestion over 40 chars - truncates with `…`

### 3. Voice Mode

- [ ] Type `` `list all files`` and press Enter - shows "Converting:" message
- [ ] Press `Ctrl+X Ctrl+V` - voice mode indicator appears
- [ ] In voice mode, press Escape - mode cancels

### 4. Suggestion Picker (All shells)

- [ ] Press `Tab` - suggestion picker opens
- [ ] Press Up/Down arrows - selection moves
- [ ] Press Enter - selected command replaces buffer (no execution)
- [ ] Press Escape - picker cancels and restores original buffer

### 5. History Picker (All shells)

- [ ] Press `↑` - history picker opens
- [ ] Press Up/Down arrows - selection moves
- [ ] Press Enter - selected command replaces buffer (no execution)
- [ ] Press Escape - picker cancels and restores original buffer
- [ ] Press `Ctrl+X s` / `Ctrl+X d` / `Ctrl+X g` - scope switches between session/cwd/global

### 6. Helper Commands

- [ ] Run `ai "how do I list files"` - returns helpful response
- [ ] Run `voice "list all files"` - converts to shell command
- [ ] Run a failing command, then `ai-fix` - suggests fix
- [ ] Run `run ls /nonexistent` - captures output and offers diagnosis

### 7. Coexistence

- [ ] Existing tab completions still work (e.g., `git <tab>`)
- [ ] History navigation uses picker (no inline stepping)
- [ ] Standard key bindings unaffected

### 8. Session History

- [ ] New terminal gets a new session ID (check `echo $CLAI_SESSION_ID`)
- [ ] Commands typed in this session influence suggestions
- [ ] Open new terminal - it has different session ID and fresh session history
- [ ] Global history (across sessions) still works for suggestions
- [ ] `history` shows only current session's commands
- [ ] `history --global` or `history -g` shows shell's native history
- [ ] `history git` filters session history to git commands
- [ ] `history --help` shows usage information

### 9. Disable / Toggle

- [ ] Run `clai off` - suggestions and pickers disable in all shells
- [ ] Run `clai on` - suggestions and pickers re-enable
- [ ] Run `clai off --session` - suggestions disabled only in current shell
- [ ] Run `clai on --session` - suggestions re-enable for current shell
- [ ] Set `CLAI_OFF=1` - suggestions disabled regardless of config

### 10. Edge Cases

- [ ] Works after `cd` to different directory
- [ ] Works with multiline commands
- [ ] No errors when clai daemon is not running
- [ ] No errors on empty command line
