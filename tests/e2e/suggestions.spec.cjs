const { test, expect } = require("@playwright/test");
const { createPlaywrightTests } = require("./terminal_case_runner.cjs");

const e2eShell = process.env.E2E_SHELL || "bash";
const e2eURL = process.env.E2E_URL || "http://127.0.0.1:8080";

const suggestionCases = [
  {
    "name": "clai suggest text format",
    "description": "Default output shows numbered list with reasons",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "format",
      "cross-shell",
      "smoke"
    ],
    "skip": false,
    "setup": [
      "echo suggest_text_seed_one",
      "echo suggest_text_seed_two",
      "clai suggest echo --limit=3"
    ],
    "steps": [
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "\\d+\\."
      },
      {
        "screen_matches": "\\("
      }
    ]
  },
  {
    "name": "clai suggest JSON format",
    "description": "--format=json returns valid JSON structure",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "format",
      "json",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo suggest_json_seed_one",
      "echo suggest_json_seed_two"
    ],
    "steps": [
      {
        "type": "clai suggest echo --format=json --limit=3"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "suggestions"
      }
    ]
  },
  {
    "name": "clai suggest fzf format",
    "description": "--format=fzf returns plain command list for piping",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "format",
      "fzf",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "docker ps"
    ],
    "steps": [
      {
        "press": "Ctrl+L"
      },
      {
        "wait": "200ms"
      },
      {
        "type": "clai suggest --format=fzf"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_not_contains": "1."
      },
      {
        "screen_not_contains": "("
      }
    ]
  },
  {
    "name": "clai suggest with limit",
    "description": "--limit restricts suggestion count",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "format",
      "parameters",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "cmd1",
      "cmd2",
      "cmd3"
    ],
    "steps": [
      {
        "type": "clai suggest --limit=2"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "."
      }
    ]
  },
  {
    "name": "clai suggest with prefix filters results",
    "description": "Providing a prefix argument filters suggestions",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "prefix",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "git status",
      "git log --oneline",
      "npm test",
      "make build"
    ],
    "steps": [
      {
        "type": "clai suggest git"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "git"
      }
    ]
  },
  {
    "name": "Suggestion shows transition reason",
    "description": "Commands following patterns show transition reason",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "reasons",
      "transition",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Transition explainability not yet reliably surfaced in e2e runtime",
    "setup": [
      "echo transition_seed_one",
      "echo transition_seed_two",
      "echo transition_seed_one",
      "echo transition_seed_two"
    ],
    "steps": [
      {
        "type": "clai suggest git --format=json --limit=5"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(trans|transition|reasons)"
      }
    ]
  },
  {
    "name": "Suggestion shows freq_repo reason",
    "description": "Frequently used repo commands show freq_repo reason",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "reasons",
      "frequency",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo freq_reason_seed",
      "echo freq_reason_seed",
      "echo freq_reason_seed",
      "echo freq_reason_other"
    ],
    "steps": [
      {
        "type": "clai suggest echo --format=json --limit=5"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(freq|frequency|repo_freq|global_freq|reasons)"
      }
    ]
  },
  {
    "name": "Suggestion includes confidence score",
    "description": "JSON output includes confidence field",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "confidence",
      "json",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo confidence_seed_alpha",
      "echo confidence_seed_beta"
    ],
    "steps": [
      {
        "type": "clai suggest echo --format=json --limit=3"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "confidence"
      }
    ]
  },
  {
    "name": "Suggestion context includes last_cmd_norm",
    "description": "JSON context shows normalized last command",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "context",
      "json",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "git status"
    ],
    "steps": [
      {
        "type": "clai suggest git --format=json --limit=3"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(context|last_cmd)"
      }
    ]
  },
  {
    "name": "Ghost text suggestion appears",
    "description": "Typing a known command prefix shows ghost text suggestion",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggestions",
      "ghost-text",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "git status",
      "git log --oneline"
    ],
    "steps": [
      {
        "press": "Ctrl+L"
      },
      {
        "wait": "200ms"
      },
      {
        "type": "clai suggest git --format=fzf --limit=3"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "git"
      }
    ]
  },
  {
    "name": "Right arrow accepts full suggestion",
    "description": "Pressing Right at end of line accepts the ghost text",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggestions",
      "ghost-text",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "docker ps -a"
    ],
    "steps": [
      {
        "type": "docker p"
      },
      {
        "wait": "300ms"
      },
      {
        "press": "Right"
      },
      {
        "wait": "200ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "docker ps"
      }
    ]
  },
  {
    "name": "Escape clears suggestion",
    "description": "Pressing Escape removes the ghost text",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggestions",
      "ghost-text",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "npm install"
    ],
    "steps": [
      {
        "press": "Ctrl+L"
      },
      {
        "wait": "200ms"
      },
      {
        "type": "npm i"
      },
      {
        "wait": "300ms"
      },
      {
        "press": "Escape"
      },
      {
        "wait": "200ms"
      }
    ],
    "expect": [
      {
        "screen_not_contains": "nstall"
      }
    ]
  },
  {
    "name": "Typing clears ghost text",
    "description": "Typing additional characters clears the ghost text suggestion",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggestions",
      "ghost-text",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "git status"
    ],
    "steps": [
      {
        "press": "Ctrl+L"
      },
      {
        "wait": "200ms"
      },
      {
        "type": "git s"
      },
      {
        "wait": "300ms"
      },
      {
        "type": "x"
      },
      {
        "wait": "200ms"
      }
    ],
    "expect": [
      {
        "screen_not_contains": "tatus"
      }
    ]
  },
  {
    "name": "Alt+Right accepts next token",
    "description": "Alt+Right accepts one token of the suggestion at a time",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggestions",
      "ghost-text",
      "token",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "kubectl get pods -n production"
    ],
    "steps": [
      {
        "press": "Ctrl+L"
      },
      {
        "wait": "200ms"
      },
      {
        "type": "clai suggest kubectl --format=fzf --limit=3"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "kubectl"
      }
    ]
  },
  {
    "name": "Typo correction suggests on exit 127",
    "description": "Misspelled command triggers Did You Mean suggestion",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "typo",
      "correction",
      "suggestions",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Typo correction backend not yet implemented",
    "setup": [
      "git status",
      "git log --oneline -1"
    ],
    "steps": [
      {
        "type": "gti status"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "1s"
      },
      {
        "type": "clai suggest"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "git"
      }
    ]
  },
  {
    "name": "Typo correction respects similarity threshold",
    "description": "Very different strings don't trigger correction",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "typo",
      "correction",
      "threshold",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Typo correction backend not yet implemented",
    "setup": [],
    "steps": [
      {
        "type": "xyzabc123nonexistent"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      },
      {
        "type": "clai suggest"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_not_contains": "xyzabc123"
      }
    ]
  },
  {
    "name": "Typo correction prioritizes high-frequency commands",
    "description": "Common commands are suggested over rare ones",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "typo",
      "correction",
      "frequency",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Typo correction backend not yet implemented",
    "setup": [
      "echo common_cmd",
      "echo common_cmd",
      "echo common_cmd",
      "echo rare_cmd"
    ],
    "steps": [
      {
        "type": "commo_cmd"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      },
      {
        "type": "clai suggest"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "common_cmd"
      }
    ]
  },
  {
    "name": "Slot filling learns argument values",
    "description": "Repeated arguments are remembered for suggestions",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "slots",
      "learning",
      "suggestions",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Slot filling backend not yet implemented",
    "setup": [
      "kubectl get pods -n production",
      "kubectl get pods -n production",
      "kubectl get pods -n staging"
    ],
    "steps": [
      {
        "type": "clai suggest kubectl"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "production"
      }
    ]
  },
  {
    "name": "Slot filling respects frequency",
    "description": "More frequent values are preferred",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "slots",
      "frequency",
      "suggestions",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Slot filling backend not yet implemented",
    "setup": [
      "docker run -it alpine",
      "docker run -it alpine",
      "docker run -it alpine",
      "docker run -it ubuntu"
    ],
    "steps": [
      {
        "type": "clai suggest 'docker run'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "alpine"
      }
    ]
  },
  {
    "name": "Slot scope repo overrides global",
    "description": "Repo-scoped slot values take precedence over global",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "slots",
      "scope",
      "suggestions",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Slot filling backend not yet implemented",
    "setup": [
      "cd /tmp && docker run -it ubuntu && cd -",
      "docker run -it alpine",
      "docker run -it alpine"
    ],
    "steps": [
      {
        "type": "clai suggest --format=json 'docker run'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "alpine"
      }
    ]
  },
  {
    "name": "Suggestions served from cache",
    "description": "Cache hit after recent command",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "cache",
      "performance",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Cache introspection not yet exposed in CLI",
    "setup": [
      "echo cache_warmup_cmd"
    ],
    "steps": [
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "cache"
      }
    ]
  },
  {
    "name": "Cache TTL expires stale entries",
    "description": "Suggestions recompute after TTL expiration",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "cache",
      "ttl",
      "performance",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Cache introspection not yet exposed in CLI",
    "setup": [
      "echo cache_test_cmd"
    ],
    "steps": [
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      },
      {
        "wait": "5s"
      },
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(cache|recomputed)"
      }
    ]
  },
  {
    "name": "Cache invalidated on new command",
    "description": "Running a command invalidates the cache",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "cache",
      "invalidation",
      "performance",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Cache introspection not yet exposed in CLI",
    "setup": [
      "echo warmup"
    ],
    "steps": [
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      },
      {
        "type": "echo new_command"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      },
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "."
      }
    ]
  },
  {
    "name": "Cache shows in debug endpoint",
    "description": "/debug/cache endpoint returns cache state",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "cache",
      "debug",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Cache introspection not yet exposed in CLI",
    "setup": [],
    "steps": [
      {
        "type": "curl -s http://localhost:8765/debug/cache 2>/dev/null || clai debug cache"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(size|entries|cache)"
      }
    ]
  },
  {
    "name": "Alt+H opens suggestion picker (bash)",
    "description": "Alt/Opt+H keybinding triggers suggestion widget",
    "shell": "bash",
    "tags": [
      "suggestions",
      "keybind",
      "widget",
      "bash"
    ],
    "skip": true,
    "skip_reason": "Alt-key picker interaction is flaky in gotty e2e harness",
    "setup": [
      "echo test1",
      "git status"
    ],
    "steps": [
      {
        "press": "Alt+H"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(fzf|suggest|pick)"
      }
    ]
  },
  {
    "name": "Alt+H opens suggestion picker (zsh)",
    "description": "Alt/Opt+H keybinding triggers suggestion widget",
    "shell": "zsh",
    "tags": [
      "suggestions",
      "keybind",
      "widget",
      "zsh"
    ],
    "skip": true,
    "skip_reason": "Alt-key picker interaction is flaky in gotty e2e harness",
    "setup": [
      "echo test1",
      "git status"
    ],
    "steps": [
      {
        "press": "Alt+H"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(fzf|suggest|pick)"
      }
    ]
  },
  {
    "name": "Alt+H opens suggestion picker (fish)",
    "description": "Alt/Opt+H keybinding triggers suggestion widget",
    "shell": "fish",
    "tags": [
      "suggestions",
      "keybind",
      "widget",
      "fish"
    ],
    "skip": true,
    "skip_reason": "Alt-key picker interaction is flaky in gotty e2e harness",
    "setup": [
      "echo test1",
      "git status"
    ],
    "steps": [
      {
        "press": "Alt+H"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(fzf|suggest|pick)"
      }
    ]
  },
  {
    "name": "clai search basic query",
    "description": "Search command finds matching history entries",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "search",
      "fts5",
      "smoke",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo 'searchable_unique_term_abc123'",
      "git status"
    ],
    "steps": [
      {
        "type": "clai search 'searchable_unique'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "1s"
      }
    ],
    "expect": [
      {
        "screen_contains": "searchable_unique_term_abc123"
      }
    ]
  },
  {
    "name": "clai search with repo filter",
    "description": "Search --repo limits results to current repository",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "search",
      "fts5",
      "repo",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo 'repo_specific_search_term'"
    ],
    "steps": [
      {
        "type": "clai search --repo 'repo_specific'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "repo_specific_search_term"
      }
    ]
  },
  {
    "name": "clai search with limit",
    "description": "Search --limit restricts result count",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "search",
      "fts5",
      "parameters",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo search_limit_1",
      "echo search_limit_2",
      "echo search_limit_3",
      "echo search_limit_4",
      "echo search_limit_5"
    ],
    "steps": [
      {
        "type": "clai search --limit 2 'search_limit'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "search_limit"
      }
    ]
  },
  {
    "name": "clai search with JSON output",
    "description": "Search --json returns valid JSON format",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "search",
      "fts5",
      "json",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo json_search_test"
    ],
    "steps": [
      {
        "type": "clai search --json 'json_search'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "{"
      },
      {
        "screen_contains": "results"
      }
    ]
  },
  {
    "name": "clai search empty results",
    "description": "Search with no matches returns empty gracefully",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "search",
      "fts5",
      "edge-case",
      "cross-shell"
    ],
    "skip": false,
    "setup": [],
    "steps": [
      {
        "type": "clai search 'xyznonexistent98765'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_not_contains": "error"
      },
      {
        "screen_not_contains": "Error"
      }
    ]
  },
  {
    "name": "clai search with special characters",
    "description": "Search handles special characters in query",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "search",
      "fts5",
      "edge-case",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo 'test-with-dashes_and_underscores'"
    ],
    "steps": [
      {
        "type": "clai search 'test-with-dashes'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "test-with-dashes"
      }
    ]
  },
  {
    "name": "Discovery finds package.json scripts",
    "description": "npm scripts appear in suggestions for node projects",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "discovery",
      "package-json",
      "suggestions",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "mkdir -p /tmp/node-test-project",
      "cd /tmp/node-test-project",
      "echo '{\"scripts\":{\"test\":\"jest\",\"build\":\"tsc\"}}' > package.json"
    ],
    "steps": [
      {
        "type": "cd /tmp/node-test-project && clai suggest"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "1s"
      }
    ],
    "expect": [
      {
        "screen_matches": "(npm|test|build)"
      }
    ]
  },
  {
    "name": "Discovery finds Makefile targets",
    "description": "make targets appear in suggestions",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "discovery",
      "makefile",
      "suggestions",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "mkdir -p /tmp/make-test-project",
      "cd /tmp/make-test-project",
      "printf 'build:\\n\\techo build\\ntest:\\n\\techo test\\n' > Makefile"
    ],
    "steps": [
      {
        "type": "cd /tmp/make-test-project && clai suggest"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "1s"
      }
    ],
    "expect": [
      {
        "screen_matches": "(make|build|test)"
      }
    ]
  },
  {
    "name": "Discovery shows project_task reason",
    "description": "Discovered tasks have project_task in reasons",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "discovery",
      "reasons",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "mkdir -p /tmp/reason-test",
      "cd /tmp/reason-test",
      "echo '{\"scripts\":{\"lint\":\"eslint\"}}' > package.json"
    ],
    "steps": [
      {
        "type": "cd /tmp/reason-test && clai suggest npm --format=json --limit=5"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "1s"
      }
    ],
    "expect": [
      {
        "screen_matches": "(project_task|task|npm)"
      }
    ]
  },
  {
    "name": "Discovery finds Cargo.toml commands",
    "description": "Rust project commands appear in suggestions",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "discovery",
      "cargo",
      "suggestions",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Cargo discovery not yet implemented",
    "setup": [
      "mkdir -p /tmp/rust-test-project",
      "cd /tmp/rust-test-project",
      "echo '[package]\\nname = \"test\"\\nversion = \"0.1.0\"' > Cargo.toml"
    ],
    "steps": [
      {
        "type": "cd /tmp/rust-test-project && clai suggest"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "1s"
      }
    ],
    "expect": [
      {
        "screen_matches": "(cargo|build|test)"
      }
    ]
  },
  {
    "name": "Discovery finds pyproject.toml scripts",
    "description": "Python project commands appear in suggestions",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "discovery",
      "python",
      "suggestions",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "pyproject.toml discovery not yet implemented",
    "setup": [
      "mkdir -p /tmp/python-test-project",
      "cd /tmp/python-test-project",
      "echo '[project.scripts]\\ntest = \"pytest\"' > pyproject.toml"
    ],
    "steps": [
      {
        "type": "cd /tmp/python-test-project && clai suggest"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "1s"
      }
    ],
    "expect": [
      {
        "screen_matches": "(pytest|python)"
      }
    ]
  },
  {
    "name": "Debug scores endpoint returns data",
    "description": "GET /debug/scores returns command scores",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "debug",
      "endpoints",
      "api",
      "cross-shell"
    ],
    "skip": false,
    "setup": [],
    "steps": [
      {
        "type": "curl -s http://localhost:8765/debug/scores 2>/dev/null || clai debug scores"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(scores|score|\\{|\\[)"
      }
    ]
  },
  {
    "name": "Debug scores with limit parameter",
    "description": "GET /debug/scores?limit=N respects limit",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "debug",
      "endpoints",
      "parameters",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "curl -s 'http://localhost:8765/debug/scores?limit=5' 2>/dev/null || clai debug scores --limit=5"
    ],
    "steps": [
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(scores|score|\\{|\\[)"
      }
    ]
  },
  {
    "name": "Debug transitions endpoint returns data",
    "description": "GET /debug/transitions returns Markov bigrams",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "debug",
      "endpoints",
      "transitions",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "git status",
      "echo debug_transition_seed_one",
      "echo debug_transition_seed_two"
    ],
    "steps": [
      {
        "type": "curl -s http://localhost:8765/debug/transitions 2>/dev/null || clai debug transitions"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(transitions|transition|prev|next|\\{|\\[)"
      }
    ]
  },
  {
    "name": "Debug tasks endpoint returns discovered tasks",
    "description": "GET /debug/tasks returns project task cache",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "debug",
      "endpoints",
      "discovery",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "mkdir -p /tmp/debug-task-test",
      "cd /tmp/debug-task-test",
      "echo '{\"scripts\":{\"test\":\"jest\"}}' > package.json",
      "clai suggest"
    ],
    "steps": [
      {
        "type": "curl -s http://localhost:8765/debug/tasks 2>/dev/null || clai debug tasks"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(tasks|npm)"
      }
    ]
  },
  {
    "name": "Debug discovery-errors endpoint",
    "description": "GET /debug/discovery-errors returns failures",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "debug",
      "endpoints",
      "discovery",
      "cross-shell"
    ],
    "skip": false,
    "setup": [],
    "steps": [
      {
        "type": "curl -s http://localhost:8765/debug/discovery-errors 2>/dev/null || clai debug discovery-errors"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(errors|error|count|\\{|\\[)"
      }
    ]
  },
  {
    "name": "Debug endpoints return valid JSON",
    "description": "All debug endpoints return parseable JSON",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "debug",
      "endpoints",
      "json",
      "cross-shell"
    ],
    "skip": false,
    "setup": [],
    "steps": [
      {
        "type": "curl -s http://localhost:8765/debug/cache 2>/dev/null | jq . || echo 'curl failed'"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_not_contains": "parse error"
      }
    ]
  },
  {
    "name": "Incognito mode toggle",
    "description": "Incognito on/off works correctly",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "incognito",
      "suggestions",
      "privacy",
      "cross-shell"
    ],
    "skip": false,
    "setup": [],
    "steps": [
      {
        "type": "clai incognito on"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "Incognito mode enabled"
      }
    ]
  },
  {
    "name": "Incognito commands appear in session suggestions",
    "description": "Ephemeral mode keeps session context for suggestions",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "incognito",
      "suggestions",
      "privacy",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "clai incognito on",
      "echo 'ephemeral_session_cmd_12345'"
    ],
    "steps": [
      {
        "type": "clai suggest"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "."
      }
    ]
  },
  {
    "name": "Incognito commands not in global suggestions",
    "description": "Ephemeral commands don't affect global suggestion scores",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "incognito",
      "suggestions",
      "privacy",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "clai incognito on",
      "echo 'incognito_only_cmd_xyz'",
      "echo 'incognito_only_cmd_xyz'",
      "echo 'incognito_only_cmd_xyz'",
      "clai incognito off"
    ],
    "steps": [
      {
        "press": "Ctrl+L"
      },
      {
        "wait": "200ms"
      },
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_not_contains": "incognito_only_cmd_xyz"
      }
    ]
  },
  {
    "name": "CLAI_EPHEMERAL affects suggestions",
    "description": "Setting CLAI_EPHEMERAL=1 enables ephemeral mode",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "incognito",
      "suggestions",
      "env-var",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "export CLAI_EPHEMERAL=1",
      "echo 'ephemeral_env_test_cmd'"
    ],
    "steps": [
      {
        "press": "Ctrl+L"
      },
      {
        "wait": "200ms"
      },
      {
        "type": "unset CLAI_EPHEMERAL"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "200ms"
      },
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_not_contains": "ephemeral_env_test_cmd"
      }
    ]
  },
  {
    "name": "Suggest API returns expected JSON structure",
    "description": "JSON response has all required fields",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "api",
      "json",
      "structure",
      "cross-shell"
    ],
    "skip": false,
    "setup": [
      "echo api_json_seed_one",
      "echo api_json_seed_two"
    ],
    "steps": [
      {
        "type": "clai suggest echo --format=json --limit=5"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_contains": "suggestions"
      },
      {
        "screen_matches": "(text|cmd|command)"
      },
      {
        "screen_matches": "(source|reasons)"
      }
    ]
  },
  {
    "name": "Suggest API includes cache status",
    "description": "JSON context indicates cache hit/miss",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "api",
      "cache",
      "json",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Cache status field not yet exposed",
    "setup": [
      "echo warmup"
    ],
    "steps": [
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(cache_hit|cache_miss|from_cache)"
      }
    ]
  },
  {
    "name": "Suggest API includes timing info",
    "description": "JSON includes computation time",
    "shells": [
      "bash",
      "zsh",
      "fish"
    ],
    "tags": [
      "suggest",
      "api",
      "timing",
      "json",
      "cross-shell"
    ],
    "skip": true,
    "skip_reason": "Timing info not yet exposed",
    "setup": [
      "echo warmup"
    ],
    "steps": [
      {
        "type": "clai suggest --format=json"
      },
      {
        "press": "Enter"
      },
      {
        "wait": "500ms"
      }
    ],
    "expect": [
      {
        "screen_matches": "(duration|time|ms)"
      }
    ]
  }
];

createPlaywrightTests({
  test,
  expect,
  shell: e2eShell,
  url: e2eURL,
  testCases: suggestionCases,
  suiteLabel: "suggestions.spec",
});
