package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// ValidationError represents a single validation failure with its location.
type ValidationError struct {
	Field   string // dot-separated path, e.g. "jobs.deploy.steps[0].run"
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateWorkflow checks a parsed WorkflowDef for structural errors.
// It returns all errors found, not just the first.
func ValidateWorkflow(wf *WorkflowDef) []ValidationError {
	if wf == nil {
		return []ValidationError{{
			Field:   "workflow",
			Message: "workflow definition is required",
		}}
	}

	var errs []ValidationError

	// Workflow must have a name.
	if wf.Name == "" {
		errs = append(errs, ValidationError{
			Field:   "name",
			Message: "workflow name is required",
		})
	}

	// Workflow must have at least one job.
	if len(wf.Jobs) == 0 {
		errs = append(errs, ValidationError{
			Field:   "jobs",
			Message: "workflow must have at least one job",
		})
	} else if len(wf.Jobs) > 1 {
		// v0 supports a single job only (spec note in §2.2).
		errs = append(errs, ValidationError{
			Field:   "jobs",
			Message: "workflow v0 supports exactly one job",
		})
	}

	// Validate secrets.
	for i, sec := range wf.Secrets {
		field := fmt.Sprintf("secrets[%d]", i)
		if sec.Name == "" {
			errs = append(errs, ValidationError{
				Field:   field + ".name",
				Message: "secret name is required",
			})
		}
		if !validSecretSources[sec.From] {
			errs = append(errs, ValidationError{
				Field:   field + ".from",
				Message: fmt.Sprintf("invalid secret source %q; must be one of: env, file, interactive", sec.From),
			})
		}
	}

	// Validate requires entries are not empty strings.
	for i, req := range wf.Requires {
		if req == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("requires[%d]", i),
				Message: "requires entry must not be empty",
			})
		}
	}

	// Validate each job.
	for jobName, job := range wf.Jobs {
		jobField := fmt.Sprintf("jobs.%s", jobName)
		errs = append(errs, validateJob(jobField, jobName, job)...)
	}
	errs = append(errs, validateNeeds(wf)...)
	errs = append(errs, validateDependencyCycles(wf)...)

	return errs
}

func validateJob(field, jobName string, job *JobDef) []ValidationError {
	if job == nil {
		return []ValidationError{{
			Field:   field,
			Message: "job definition is nil",
		}}
	}

	var errs []ValidationError

	// Job must have at least one step.
	if len(job.Steps) == 0 {
		errs = append(errs, ValidationError{
			Field:   field + ".steps",
			Message: "job must have at least one step",
		})
	}

	// Track step IDs for uniqueness within the job.
	seenIDs := make(map[string]bool)

	for i, step := range job.Steps {
		stepField := fmt.Sprintf("%s.steps[%d]", field, i)
		errs = append(errs, validateStep(stepField, step, seenIDs)...)
	}
	errs = append(errs, validateMatrixIncludeKeys(jobName, job)...)

	return errs
}

func validateStep(field string, step *StepDef, seenIDs map[string]bool) []ValidationError {
	if step == nil {
		return []ValidationError{{
			Field:   field,
			Message: "step definition is nil",
		}}
	}

	var errs []ValidationError

	// Step must have a run field.
	if step.Run == "" {
		errs = append(errs, ValidationError{
			Field:   field + ".run",
			Message: "step run field is required",
		})
	}

	// Step ID is required and must be unique within the job.
	if step.ID == "" {
		errs = append(errs, ValidationError{
			Field:   field + ".id",
			Message: "step id is required",
		})
	} else {
		if seenIDs[step.ID] {
			errs = append(errs, ValidationError{
				Field:   field + ".id",
				Message: fmt.Sprintf("duplicate step id %q within job", step.ID),
			})
		}
		seenIDs[step.ID] = true
	}

	// risk_level must be valid.
	if !validRiskLevels[step.RiskLevel] {
		errs = append(errs, ValidationError{
			Field:   field + ".risk_level",
			Message: fmt.Sprintf("invalid risk_level %q; must be one of: low, medium, high", step.RiskLevel),
		})
	}

	// analyze: true must have a non-empty analysis_prompt.
	if step.Analyze && step.AnalysisPrompt == "" {
		errs = append(errs, ValidationError{
			Field:   field + ".analysis_prompt",
			Message: "analysis_prompt is required when analyze is true",
		})
	}

	// shell must be a valid value.
	if !validShellValues[step.Shell] {
		errs = append(errs, ValidationError{
			Field:   field + ".shell",
			Message: fmt.Sprintf("invalid shell value %q; must be one of: sh, bash, zsh, fish, pwsh, cmd, true, or false", step.Shell),
		})
	}

	return errs
}

func validateNeeds(wf *WorkflowDef) []ValidationError {
	var errs []ValidationError

	for jobName, job := range wf.Jobs {
		if job == nil {
			continue
		}
		for i, dep := range job.Needs {
			field := fmt.Sprintf("jobs.%s.needs[%d]", jobName, i)
			if dep == "" {
				errs = append(errs, ValidationError{
					Field:   field,
					Message: "job dependency must not be empty",
				})
				continue
			}
			if depJob, ok := wf.Jobs[dep]; !ok || depJob == nil {
				errs = append(errs, ValidationError{
					Field:   field,
					Message: fmt.Sprintf("job %q depends on unknown job %q", jobName, dep),
				})
			}
		}
	}

	return errs
}

func validateDependencyCycles(wf *WorkflowDef) []ValidationError {
	var (
		errs  []ValidationError
		state = map[string]int{} // 0=unvisited, 1=visiting, 2=visited
		stack []string
	)

	var dfs func(string) bool
	dfs = func(job string) bool {
		state[job] = 1
		stack = append(stack, job)

		jobDef := wf.Jobs[job]
		if jobDef == nil {
			stack = stack[:len(stack)-1]
			state[job] = 2
			return false
		}

		for _, dep := range jobDef.Needs {
			if dep == "" {
				continue
			}
			if depJob, ok := wf.Jobs[dep]; !ok || depJob == nil {
				continue
			}
			switch state[dep] {
			case 0:
				if dfs(dep) {
					return true
				}
			case 1:
				cycle := cyclePath(stack, dep)
				errs = append(errs, ValidationError{
					Field:   "jobs",
					Message: fmt.Sprintf("circular dependency: %s", strings.Join(cycle, " -> ")),
				})
				return true
			}
		}

		stack = stack[:len(stack)-1]
		state[job] = 2
		return false
	}

	jobNames := make([]string, 0, len(wf.Jobs))
	for name := range wf.Jobs {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames)

	for _, jobName := range jobNames {
		if state[jobName] == 0 && dfs(jobName) {
			break
		}
	}

	return errs
}

func cyclePath(stack []string, backEdgeTarget string) []string {
	start := 0
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == backEdgeTarget {
			start = i
			break
		}
	}

	path := append([]string{}, stack[start:]...)
	path = append(path, backEdgeTarget)
	return path
}

func validateMatrixIncludeKeys(jobName string, job *JobDef) []ValidationError {
	if job.Strategy == nil || job.Strategy.Matrix == nil || len(job.Strategy.Matrix.Include) <= 1 {
		return nil
	}

	baseline := keysSet(job.Strategy.Matrix.Include[0])
	for i := 1; i < len(job.Strategy.Matrix.Include); i++ {
		current := keysSet(job.Strategy.Matrix.Include[i])
		if !sameKeySet(baseline, current) {
			return []ValidationError{{
				Field:   fmt.Sprintf("jobs.%s.strategy.matrix.include[%d]", jobName, i),
				Message: "matrix include entries have inconsistent keys",
			}}
		}
	}

	return nil
}

func keysSet(m map[string]string) map[string]bool {
	set := make(map[string]bool, len(m))
	for k := range m {
		set[k] = true
	}
	return set
}

func sameKeySet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
