## Real behavior

people run different threads of work in different tabs:
•	one tab for a repo build/test loop
•	one tab for kubectl/terraform
•	one tab SSH’d into prod
•	one tab tailing logs

Global history is noisy. Session history is immediately useful because it answers:
•	“What was the last command I ran here?”
•	“What was the exact flag combo that worked in this context?”
•	“I know I ran a command 5 minutes ago… where is it?”

That’s not “restore”. That’s tab-local memory.

## Smart suggestions

“Suggest the next command that makes sense right now”

Smart suggestions are dramatically better when they’re session-scoped:
•	after terraform plan → suggest terraform apply / terraform show
•	after git checkout -b → suggest git push -u origin …
•	after kubectl get pods -n X → suggest kubectl logs … -n X

This is how you get fish-like UX across zsh/bash/PowerShell without switching shells.

## “Give me a breadcrumb trail I can share”

This is more common than it sounds:
•	you’re debugging with a colleague
•	you want to paste “what I did” into a ticket/PR description
•	you want a reproducible set of steps

A session log that can be exported as a clean snippet is valuable:
•	fewer “works on my machine” loops
•	less re-typing
•	less forgetting the exact sequence

## “Undo/redo but for command lines”

Not literally undoing effects — but:
•	quickly reinsert previous commands (with parameters intact)
•	yank the last good version
•	compare variants you tried

That’s a daily friction reducer.

## Make it
•	"clai: fish-like intelligence everywhere, shell-agnostic"
•	“Bring fish-level intelligence to zsh, bash, PowerShell, SSH sessions, and servers — without changing shells.”
•	“Per-tab history and suggestions”
•	“Instant recall of what you just did”
•	“Smarter autocomplete based on what you’re doing in this tab”
•	“Better error help using your recent context”
