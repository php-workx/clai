package workflow

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// StepStatus represents the status of a step execution.
type StepStatus string

// Step status constants.
const (
	StepPassed  StepStatus = "passed"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

// RunStatus represents the overall status of a workflow run.
type RunStatus string

// Run status constants.
const (
	RunPassed RunStatus = "passed"
	RunFailed RunStatus = "failed"
)

// Decision represents an LLM analysis decision (spec SS10.3).
type Decision string

// Decision constants aligned with spec (proceed/halt/needs_human).
const (
	DecisionProceed    Decision = "proceed"
	DecisionHalt       Decision = "halt"
	DecisionNeedsHuman Decision = "needs_human"
	DecisionError      Decision = "error"
)

// RiskLevel represents the risk level of a step.
type RiskLevel string

// Risk level constants.
const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// ReviewAction represents a human review action.
type ReviewAction string

// Review action constants.
const (
	ActionApprove  ReviewAction = "approve"
	ActionReject   ReviewAction = "reject"
	ActionInspect  ReviewAction = "inspect"
	ActionCommand  ReviewAction = "command"
	ActionQuestion ReviewAction = "question"
)

// WorkflowDef is the top-level workflow file.
type WorkflowDef struct {
	Name     string             `yaml:"name"`
	Env      map[string]string  `yaml:"env,omitempty"`
	Secrets  []SecretDef        `yaml:"secrets,omitempty"`
	Requires []string           `yaml:"requires,omitempty"`
	Jobs     map[string]*JobDef `yaml:"jobs"`
}

// SecretDef defines a secret to be loaded before execution.
type SecretDef struct {
	Name   string `yaml:"name"`
	From   string `yaml:"from"`             // "env" (Tier 0), "file", "interactive" (Tier 1)
	Path   string `yaml:"path,omitempty"`   // for "file" source
	Prompt string `yaml:"prompt,omitempty"` // for "interactive" source
}

// JobDef represents a single job within a workflow.
type JobDef struct {
	Name     string            `yaml:"name,omitempty"`
	Needs    []string          `yaml:"needs,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
	Strategy *StrategyDef      `yaml:"strategy,omitempty"`
	Steps    []*StepDef        `yaml:"steps"`
}

// StrategyDef controls matrix expansion and parallelism.
type StrategyDef struct {
	Matrix   *MatrixDef `yaml:"matrix,omitempty"`
	FailFast *bool      `yaml:"fail_fast,omitempty"` // default: true
}

// MatrixDef defines parameter combinations.
type MatrixDef struct {
	Include []map[string]string `yaml:"include"`
	Exclude []map[string]string `yaml:"exclude,omitempty"`
}

// StepDef represents a single step within a job.
type StepDef struct {
	ID             string            `yaml:"id,omitempty"`
	Name           string            `yaml:"name"`
	Run            string            `yaml:"run"`
	Env            map[string]string `yaml:"env,omitempty"`
	Shell          string            `yaml:"-"` // handled by custom UnmarshalYAML
	Analyze        bool              `yaml:"analyze,omitempty"`
	AnalysisPrompt string            `yaml:"analysis_prompt,omitempty"`
	RiskLevel      string            `yaml:"risk_level,omitempty"` // "low", "medium", "high"

	// Runtime fields (not from YAML)
	ResolvedArgv    []string `yaml:"-"`
	ResolvedCommand string   `yaml:"-"`
	ResolvedEnv     []string `yaml:"-"`
}

// knownStepFields is the set of YAML keys accepted in a step mapping.
// Used for unknown field detection since KnownFields(true) does not
// propagate into custom UnmarshalYAML implementations (risk M5/m16).
// Includes Tier 1 fields that are ignored but not rejected, so that
// valid YAML with future fields doesn't produce parse errors.
var knownStepFields = map[string]bool{
	// Tier 0 fields.
	"id": true, "name": true, "run": true, "env": true,
	"shell": true, "analyze": true, "analysis_prompt": true,
	"risk_level": true,
	// Tier 1 fields (ignored but tolerated).
	"if": true, "timeout_minutes": true, "retry": true,
	"continue_on_error": true, "working_directory": true,
	"outputs": true,
}

// stepFields is used for decoding all StepDef fields including Shell.
// The Shell field is typed as interface{} to accept both bool and string YAML values.
type stepFields struct {
	ID             string            `yaml:"id,omitempty"`
	Name           string            `yaml:"name"`
	Run            string            `yaml:"run"`
	Env            map[string]string `yaml:"env,omitempty"`
	Shell          interface{}       `yaml:"shell,omitempty"`
	Analyze        bool              `yaml:"analyze,omitempty"`
	AnalysisPrompt string            `yaml:"analysis_prompt,omitempty"`
	RiskLevel      string            `yaml:"risk_level,omitempty"`
}

// UnmarshalYAML handles both bool and string values for the Shell field.
// It also enforces unknown field rejection since KnownFields(true) on the
// top-level decoder does not propagate through custom unmarshalers.
//
//	shell: true  -> "true" (use default shell)
//	shell: bash  -> "bash" (explicit shell)
//	shell: (omitted) -> "" (argv mode, no shell)
func (s *StepDef) UnmarshalYAML(value *yaml.Node) error {
	// Check for unknown fields in the mapping node.
	if value.Kind == yaml.MappingNode {
		for i := 0; i < len(value.Content)-1; i += 2 {
			key := value.Content[i].Value
			if !knownStepFields[key] {
				return fmt.Errorf("line %d: field %s not found in type workflow.StepDef", value.Content[i].Line, key)
			}
		}
	}

	var raw stepFields
	if err := value.Decode(&raw); err != nil {
		return err
	}

	s.ID = raw.ID
	s.Name = raw.Name
	s.Run = raw.Run
	s.Env = raw.Env
	s.Analyze = raw.Analyze
	s.AnalysisPrompt = raw.AnalysisPrompt
	s.RiskLevel = raw.RiskLevel

	switch v := raw.Shell.(type) {
	case bool:
		if v {
			s.Shell = "true"
		}
		// false -> "" (same as omitted)
	case string:
		s.Shell = v
	case nil:
		// omitted — Shell stays ""
	default:
		return fmt.Errorf("shell: expected bool or string, got %T", v)
	}

	return nil
}

// ShellMode returns the effective shell execution mode.
func (s *StepDef) ShellMode() string {
	if s.Shell == "" {
		return "" // argv mode
	}
	if s.Shell == "true" {
		return "default" // use platform default shell
	}
	return s.Shell // explicit shell name
}

// validShellValues defines the allowed values for Shell fields.
var validShellValues = map[string]bool{
	"":     true,
	"true": true,
	"sh":   true,
	"bash": true,
	"zsh":  true,
	"fish": true,
	"pwsh": true,
	"cmd":  true,
}

// validSecretSources defines allowed values for SecretDef.From.
var validSecretSources = map[string]bool{
	"env":         true,
	"file":        true,
	"interactive": true,
}

// validRiskLevels defines allowed values for StepDef.RiskLevel.
var validRiskLevels = map[string]bool{
	"":       true, // defaults to "medium"
	"low":    true,
	"medium": true,
	"high":   true,
}
