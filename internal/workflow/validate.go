package workflow

import "fmt"

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
		errs = append(errs, validateJob(jobField, job)...)
	}

	return errs
}

func validateJob(field string, job *JobDef) []ValidationError {
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

	return errs
}

func validateStep(field string, step *StepDef, seenIDs map[string]bool) []ValidationError {
	var errs []ValidationError

	// Step must have a run field.
	if step.Run == "" {
		errs = append(errs, ValidationError{
			Field:   field + ".run",
			Message: "step run field is required",
		})
	}

	// Step ID must be unique within the job (if set).
	if step.ID != "" {
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
			Message: fmt.Sprintf("invalid shell value %q; must be one of: sh, bash, zsh, fish, pwsh, cmd, or true", step.Shell),
		})
	}

	return errs
}
