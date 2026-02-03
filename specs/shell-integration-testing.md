# Shell Integration Manual Testing

Checklist for manually testing clai shell integrations.

## Setup

1. Source the integration: `source ~/.clai/hooks/clai.zsh` (or `.bash`, `.fish`)
2. Verify "clai loaded" message appears
3. Ensure `clai` binary is in PATH

## Test Cases

### 1. Command Suggestions

- [ ] Type a partial command (e.g., `git`) - suggestion should appear in right prompt
- [ ] Suggestion shows format: `(git → git status)`
- [ ] Type gibberish - no suggestion should appear
- [ ] Press right arrow at end of line - suggestion is accepted into buffer
- [ ] Press Escape - suggestion clears

### 2. Long Input Handling

- [ ] Type a very long prefix (20+ chars) - prefix truncates to `…`
- [ ] Suggestion over 40 chars - truncates with `…`

### 3. Voice Mode

- [ ] Type `` `list all files`` and press Enter - shows "Converting:" message
- [ ] Press `Ctrl+X Ctrl+V` - voice mode indicator appears
- [ ] In voice mode, press Escape - mode cancels

### 4. Menu Selection (Zsh only)

- [ ] Type partial command, press `Ctrl+Space` - menu appears with multiple suggestions
- [ ] Press Up/Down arrows - selection moves
- [ ] Press Enter - selected command goes to buffer
- [ ] Press Escape - menu closes

### 5. Helper Commands

- [ ] Run `ai "how do I list files"` - returns helpful response
- [ ] Run `voice "list all files"` - converts to shell command
- [ ] Run a failing command, then `ai-fix` - suggests fix
- [ ] Run `run ls /nonexistent` - captures output and offers diagnosis

### 6. Coexistence

- [ ] Existing tab completions still work (e.g., `git <tab>`)
- [ ] History navigation (Up/Down) still works
- [ ] Standard key bindings unaffected

### 7. Session History

- [ ] New terminal gets a new session ID (check `echo $CLAI_SESSION_ID`)
- [ ] Commands typed in this session influence suggestions
- [ ] Open new terminal - it has different session ID and fresh session history
- [ ] Global history (across sessions) still works for suggestions
- [ ] `history` shows only current session's commands
- [ ] `history --global` or `history -g` shows shell's native history
- [ ] `history git` filters session history to git commands
- [ ] `history --help` shows usage information

### 8. Edge Cases

- [ ] Works after `cd` to different directory
- [ ] Works with multiline commands
- [ ] No errors when clai daemon is not running
- [ ] No errors on empty command line
