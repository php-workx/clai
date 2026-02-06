// Package playbook provides parsing and DAG-based task ordering for the
// .clai/tasks.yaml extended playbook format.
//
// The playbook supports task dependencies (after), failure triggers (after_failure),
// priority ordering, workflow associations, and tags for search/describe.
//
// See spec Section 11.4 and Section 16 (extended playbook config keys).
package playbook

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Errors returned by playbook operations.
var (
	ErrCircularDependency = errors.New("circular dependency detected in task graph")
	ErrMissingTask        = errors.New("referenced task not found")
	ErrEmptyPlaybook      = errors.New("playbook has no tasks")
	ErrInvalidYAML        = errors.New("invalid YAML in playbook")
	ErrDuplicateTask      = errors.New("duplicate task name")
)

// Priority levels for task ordering.
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
)

// priorityWeight maps priority strings to numeric weights for sorting.
var priorityWeight = map[string]int{
	PriorityHigh:   3,
	PriorityNormal: 2,
	PriorityLow:    1,
}

// Task represents a single entry in the playbook.
type Task struct {
	// Name is the unique task identifier (required).
	Name string `yaml:"name"`

	// Command is the shell command to execute (required).
	Command string `yaml:"command"`

	// Description is an optional human-readable description.
	Description string `yaml:"description,omitempty"`

	// After specifies a task name that must complete successfully before
	// this task is suggested. Forms a DAG dependency.
	After string `yaml:"after,omitempty"`

	// AfterFailure specifies a task name; this task is suggested only when
	// the referenced task (or matching command) fails.
	AfterFailure string `yaml:"after_failure,omitempty"`

	// Priority controls ordering weight: "low", "normal" (default), "high".
	Priority string `yaml:"priority,omitempty"`

	// Workflows lists workflow pattern names this task is associated with.
	// These are seeded into the workflow_pattern table.
	Workflows []string `yaml:"workflows,omitempty"`

	// Tags are labels for search and describe-mode matching.
	Tags []string `yaml:"tags,omitempty"`

	// Enabled controls whether this task is active (default: true).
	Enabled *bool `yaml:"enabled,omitempty"`
}

// IsEnabled returns whether the task is enabled.
// Tasks are enabled by default when the field is not set.
func (t *Task) IsEnabled() bool {
	if t.Enabled == nil {
		return true
	}
	return *t.Enabled
}

// PriorityWeight returns the numeric priority weight for sorting.
func (t *Task) PriorityWeight() int {
	if w, ok := priorityWeight[t.Priority]; ok {
		return w
	}
	return priorityWeight[PriorityNormal]
}

// playbookFile is the raw YAML structure of .clai/tasks.yaml.
type playbookFile struct {
	Tasks []Task `yaml:"tasks"`
}

// Playbook represents a parsed and validated .clai/tasks.yaml file.
// It provides methods to query tasks and their dependencies.
// It is safe for concurrent use.
type Playbook struct {
	mu sync.RWMutex

	// tasks stores all tasks keyed by name.
	tasks map[string]*Task

	// taskOrder stores tasks in their defined order.
	taskOrder []*Task

	// afterDeps maps task name -> list of tasks that come after it.
	afterDeps map[string][]*Task

	// afterFailureDeps maps task name -> list of tasks triggered on failure.
	afterFailureDeps map[string][]*Task

	// workflowTasks maps workflow name -> list of associated tasks.
	workflowTasks map[string][]*Task
}

// LoadPlaybook reads and parses a .clai/tasks.yaml file from the given path.
// It validates the structure, checks for circular dependencies, and builds
// the internal DAG representation.
func LoadPlaybook(path string) (*Playbook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading playbook: %w", err)
	}

	return ParsePlaybook(data)
}

// ParsePlaybook parses raw YAML bytes into a validated Playbook.
func ParsePlaybook(data []byte) (*Playbook, error) {
	var pf playbookFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidYAML, err)
	}

	if len(pf.Tasks) == 0 {
		return nil, ErrEmptyPlaybook
	}

	p := &Playbook{
		tasks:            make(map[string]*Task, len(pf.Tasks)),
		taskOrder:        make([]*Task, 0, len(pf.Tasks)),
		afterDeps:        make(map[string][]*Task),
		afterFailureDeps: make(map[string][]*Task),
		workflowTasks:    make(map[string][]*Task),
	}

	// First pass: register all tasks by name.
	for i := range pf.Tasks {
		t := &pf.Tasks[i]

		if t.Name == "" {
			return nil, fmt.Errorf("task at index %d: name is required", i)
		}
		if t.Command == "" {
			return nil, fmt.Errorf("task %q: command is required", t.Name)
		}

		// Normalize priority
		if t.Priority == "" {
			t.Priority = PriorityNormal
		}
		t.Priority = strings.ToLower(t.Priority)
		if _, ok := priorityWeight[t.Priority]; !ok {
			return nil, fmt.Errorf("task %q: invalid priority %q (must be low, normal, or high)", t.Name, t.Priority)
		}

		if _, exists := p.tasks[t.Name]; exists {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTask, t.Name)
		}

		p.tasks[t.Name] = t
		p.taskOrder = append(p.taskOrder, t)
	}

	// Second pass: validate references and build dependency maps.
	for _, t := range p.taskOrder {
		if t.After != "" {
			if _, exists := p.tasks[t.After]; !exists {
				return nil, fmt.Errorf("%w: task %q references after=%q", ErrMissingTask, t.Name, t.After)
			}
			p.afterDeps[t.After] = append(p.afterDeps[t.After], t)
		}

		if t.AfterFailure != "" {
			if _, exists := p.tasks[t.AfterFailure]; !exists {
				return nil, fmt.Errorf("%w: task %q references after_failure=%q", ErrMissingTask, t.Name, t.AfterFailure)
			}
			p.afterFailureDeps[t.AfterFailure] = append(p.afterFailureDeps[t.AfterFailure], t)
		}

		for _, wf := range t.Workflows {
			p.workflowTasks[wf] = append(p.workflowTasks[wf], t)
		}
	}

	// Validate no circular dependencies in the "after" DAG.
	if err := p.validateDAG(); err != nil {
		return nil, err
	}

	return p, nil
}

// validateDAG checks for circular dependencies using DFS cycle detection
// on the "after" dependency graph.
func (p *Playbook) validateDAG() error {
	// Build adjacency: task -> tasks it depends on (via "after").
	// We check: if A.after = B, then there is an edge B -> A (B must complete before A).
	// A cycle means A -> B -> ... -> A in the dependency chain.
	const (
		white = iota // Not visited
		gray         // Being visited (in current DFS path)
		black        // Fully visited
	)

	colors := make(map[string]int, len(p.tasks))
	for name := range p.tasks {
		colors[name] = white
	}

	var dfs func(name string) error
	dfs = func(name string) error {
		colors[name] = gray

		// Visit all tasks that depend on this one (via "after").
		for _, dep := range p.afterDeps[name] {
			switch colors[dep.Name] {
			case gray:
				return fmt.Errorf("%w: %s -> %s", ErrCircularDependency, name, dep.Name)
			case white:
				if err := dfs(dep.Name); err != nil {
					return err
				}
			}
		}

		colors[name] = black
		return nil
	}

	for name := range p.tasks {
		if colors[name] == white {
			if err := dfs(name); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetTask returns a task by name, or nil if not found.
func (p *Playbook) GetTask(name string) *Task {
	p.mu.RLock()
	defer p.mu.RUnlock()

	t, ok := p.tasks[name]
	if !ok {
		return nil
	}
	return t
}

// AllTasks returns all enabled tasks sorted by priority (high first).
func (p *Playbook) AllTasks() []Task {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []Task
	for _, t := range p.taskOrder {
		if t.IsEnabled() {
			result = append(result, *t)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		wi := result[i].PriorityWeight()
		wj := result[j].PriorityWeight()
		if wi != wj {
			return wi > wj
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// NextTasks returns the tasks that should be suggested after the given
// command has completed. If failed is true, it returns after_failure tasks;
// otherwise, it returns after (success) tasks.
//
// The lastCmd parameter is matched against task names and commands. This allows
// matching both playbook task names ("build") and arbitrary commands.
//
// Results are sorted by priority (high first).
func (p *Playbook) NextTasks(lastCmd string, failed bool) []Task {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Find matching task name for lastCmd.
	// Match by task name first, then by command text.
	matchedName := p.resolveTaskName(lastCmd)
	if matchedName == "" {
		return nil
	}

	var candidates []*Task
	if failed {
		candidates = p.afterFailureDeps[matchedName]
	} else {
		candidates = p.afterDeps[matchedName]
	}

	if len(candidates) == 0 {
		return nil
	}

	var result []Task
	for _, t := range candidates {
		if t.IsEnabled() {
			result = append(result, *t)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		wi := result[i].PriorityWeight()
		wj := result[j].PriorityWeight()
		if wi != wj {
			return wi > wj
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// resolveTaskName resolves a command string to a task name.
// It first checks for an exact name match, then checks for a command match.
func (p *Playbook) resolveTaskName(cmd string) string {
	// Exact name match
	if _, ok := p.tasks[cmd]; ok {
		return cmd
	}

	// Command text match
	for _, t := range p.taskOrder {
		if t.Command == cmd {
			return t.Name
		}
	}

	return ""
}

// WorkflowNames returns all workflow names referenced by tasks in the playbook.
func (p *Playbook) WorkflowNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.workflowTasks))
	for name := range p.workflowTasks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TasksForWorkflow returns all tasks associated with the given workflow name.
func (p *Playbook) TasksForWorkflow(workflowName string) []Task {
	p.mu.RLock()
	defer p.mu.RUnlock()

	tasks, ok := p.workflowTasks[workflowName]
	if !ok {
		return nil
	}

	result := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if t.IsEnabled() {
			result = append(result, *t)
		}
	}
	return result
}

// TaskNames returns all task names in the playbook.
func (p *Playbook) TaskNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.tasks))
	for _, t := range p.taskOrder {
		names = append(names, t.Name)
	}
	return names
}
