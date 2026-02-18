// Package discover provides the V2 discovery engine for surfacing suggestions
// in new or empty sessions when there is no command history to drive
// transition-based ranking.
//
// It uses three sources in priority order:
//  1. Playbook tasks from .clai/tasks.yaml
//  2. Project-type priors (common commands for detected project types)
//  3. Tool-common sets (universal commands like git status, ls)
//
// The engine enforces a cooldown to avoid re-suggesting the same candidate
// within a configurable window, and a display gate that only returns
// candidates when the prefix is empty and no high-confidence scorer results
// are available.
//
// See spec Section 6.2 (source 12), Section 11, and Section 16 (discovery config keys).
package discover

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/runger/clai/internal/suggestions/playbook"
)

// Source identifies where a discovery candidate came from.
const (
	SourcePlaybook    = "playbook"
	SourceProjectType = "project_type"
	SourceToolCommon  = "tool_common"
	makeTestCommand   = "make test"
)

// Candidate represents a single discovery suggestion.
type Candidate struct {
	Command  string
	Source   string
	Tags     []string
	Priority int
}

// DiscoverConfig configures a single discovery request.
type DiscoverConfig struct {
	PlaybookPath string
	ProjectTypes []string
	Limit        int
	CooldownMs   int64
}

// Engine is the V2 discovery engine. It is safe for concurrent use.
type Engine struct {
	cooldowns map[string]int64
	nowFn     func() int64
	mu        sync.Mutex
}

// NewEngine creates a new discovery engine.
func NewEngine() *Engine {
	return &Engine{
		cooldowns: make(map[string]int64),
		nowFn:     func() int64 { return time.Now().UnixMilli() },
	}
}

// Discover returns discovery candidates for an empty-session context.
// It checks playbook, project-type priors, and tool-common sets in priority
// order, applies cooldown filtering, and returns up to config.Limit candidates.
func (e *Engine) Discover(_ context.Context, config DiscoverConfig) []Candidate {
	limit := config.Limit
	if limit <= 0 {
		limit = 5
	}

	now := e.nowFn()
	candidates := appendPlaybookCandidates(nil, config.PlaybookPath)
	candidates = appendProjectTypeCandidates(candidates, config.ProjectTypes)
	candidates = appendToolCommonCandidates(candidates)

	// Deduplicate by command text, keeping highest priority / first seen source
	candidates = dedup(candidates)

	// Apply cooldown filtering
	e.mu.Lock()
	candidates = e.applyCooldown(candidates, config.CooldownMs, now)
	e.mu.Unlock()

	// Sort: source priority (playbook > project_type > tool_common), then by priority
	sort.SliceStable(candidates, func(i, j int) bool {
		si := sourceOrder(candidates[i].Source)
		sj := sourceOrder(candidates[j].Source)
		if si != sj {
			return si < sj
		}
		return candidates[i].Priority > candidates[j].Priority
	})

	// Apply limit
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	// Record cooldowns for returned candidates
	e.recordCooldowns(candidates, now)

	return candidates
}

func appendPlaybookCandidates(candidates []Candidate, playbookPath string) []Candidate {
	if playbookPath == "" {
		return candidates
	}
	pb, err := playbook.LoadPlaybook(playbookPath)
	if err != nil {
		return candidates
	}
	playbookTasks := pb.AllTasks()
	for i := range playbookTasks {
		task := playbookTasks[i]
		candidates = append(candidates, Candidate{
			Command:  task.Command,
			Source:   SourcePlaybook,
			Priority: task.PriorityWeight(),
			Tags:     task.Tags,
		})
	}
	return candidates
}

func appendProjectTypeCandidates(candidates []Candidate, projectTypes []string) []Candidate {
	for _, pt := range projectTypes {
		priors, ok := projectTypePriors[pt]
		if !ok {
			continue
		}
		for i, cmd := range priors {
			candidates = append(candidates, Candidate{
				Command:  cmd,
				Source:   SourceProjectType,
				Priority: len(priors) - i, // Earlier entries have higher priority
				Tags:     []string{pt},
			})
		}
	}
	return candidates
}

func appendToolCommonCandidates(candidates []Candidate) []Candidate {
	for i, cmd := range toolCommonCommands {
		candidates = append(candidates, Candidate{
			Command:  cmd,
			Source:   SourceToolCommon,
			Priority: len(toolCommonCommands) - i,
			Tags:     []string{"common"},
		})
	}
	return candidates
}

// applyCooldown filters out candidates that were recently suggested.
// Must be called with e.mu held.
func (e *Engine) applyCooldown(candidates []Candidate, cooldownMs, now int64) []Candidate {
	if cooldownMs <= 0 {
		return candidates
	}

	var filtered []Candidate
	for _, c := range candidates {
		lastSuggested, ok := e.cooldowns[c.Command]
		if !ok || (now-lastSuggested) >= cooldownMs {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// ResetCooldowns clears all cooldown state. Useful for testing.
func (e *Engine) ResetCooldowns() {
	e.mu.Lock()
	e.cooldowns = make(map[string]int64)
	e.mu.Unlock()
}

func (e *Engine) recordCooldowns(candidates []Candidate, now int64) {
	e.mu.Lock()
	for _, c := range candidates {
		e.cooldowns[c.Command] = now
	}
	e.mu.Unlock()
}

// sourceOrder returns the sort order for a source (lower = higher priority).
func sourceOrder(source string) int {
	switch source {
	case SourcePlaybook:
		return 0
	case SourceProjectType:
		return 1
	case SourceToolCommon:
		return 2
	default:
		return 3
	}
}

// dedup removes duplicate commands, keeping the first occurrence (which has
// higher source priority due to insertion order).
func dedup(candidates []Candidate) []Candidate {
	seen := make(map[string]bool, len(candidates))
	result := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if seen[c.Command] {
			continue
		}
		seen[c.Command] = true
		result = append(result, c)
	}
	return result
}

// projectTypePriors maps detected project types to common commands.
// These are suggested when a project type is detected but there is no
// command history.
var projectTypePriors = map[string][]string{
	"go":        {"go test ./...", "go build ./...", "go run .", makeTestCommand, "make build"},
	"node":      {"npm test", "npm run build", "npm start", "npm install"},
	"python":    {"pytest", "python -m pytest", "pip install -r requirements.txt"},
	"rust":      {"cargo test", "cargo build", "cargo run"},
	"docker":    {"docker build .", "docker compose up", "docker ps"},
	"java":      {"mvn test", "mvn package", "gradle build", "gradle test"},
	"ruby":      {"bundle exec rspec", "bundle install", "rake test"},
	"make":      {"make", "make build", makeTestCommand, "make clean"},
	"terraform": {"terraform plan", "terraform apply", "terraform init"},
	"cpp":       {"cmake --build .", "make", makeTestCommand},
	"haskell":   {"cabal build", "cabal test", "stack build"},
	"nix":       {"nix build", "nix develop", "nix flake check"},
}

// toolCommonCommands are universal commands suggested as a last resort
// when there is no playbook and no detected project type.
var toolCommonCommands = []string{
	"git status",
	"git log --oneline -10",
	"ls",
	"pwd",
}
