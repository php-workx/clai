package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validWorkflow() *WorkflowDef {
	return &WorkflowDef{
		Name: "test-workflow",
		Jobs: map[string]*JobDef{
			"build": {
				Steps: []*StepDef{
					{ID: "step-one", Name: "step one", Run: "echo hello"},
				},
			},
		},
	}
}

func TestValidateWorkflow_Valid(t *testing.T) {
	wf := validWorkflow()
	errs := ValidateWorkflow(wf)
	assert.Empty(t, errs)
}

func TestValidateWorkflow_MissingName(t *testing.T) {
	wf := validWorkflow()
	wf.Name = ""
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "name", "required")
}

func TestValidateWorkflow_NoJobs(t *testing.T) {
	wf := &WorkflowDef{
		Name: "empty",
		Jobs: map[string]*JobDef{},
	}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs", "at least one job")
}

func TestValidateWorkflow_JobNoSteps(t *testing.T) {
	wf := &WorkflowDef{
		Name: "no-steps",
		Jobs: map[string]*JobDef{
			"empty": {Steps: []*StepDef{}},
		},
	}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.empty.steps", "at least one step")
}

func TestValidateWorkflow_StepMissingRun(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Steps[0].Run = ""
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.steps[0].run", "required")
}

func TestValidateWorkflow_StepMissingID(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Steps[0].ID = ""
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.steps[0].id", "required")
}

func TestValidateWorkflow_DuplicateStepIDs(t *testing.T) {
	wf := &WorkflowDef{
		Name: "dup-ids",
		Jobs: map[string]*JobDef{
			"build": {
				Steps: []*StepDef{
					{ID: "step-a", Name: "first", Run: "echo 1"},
					{ID: "step-a", Name: "second", Run: "echo 2"},
				},
			},
		},
	}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.steps[1].id", "duplicate")
}

func TestValidateWorkflow_MultiJobNotSupportedInV0(t *testing.T) {
	wf := &WorkflowDef{
		Name: "multi-job",
		Jobs: map[string]*JobDef{
			"job-a": {
				Steps: []*StepDef{
					{ID: "step-x", Name: "first", Run: "echo 1"},
				},
			},
			"job-b": {
				Steps: []*StepDef{
					{ID: "step-x", Name: "also first", Run: "echo 2"},
				},
			},
		},
	}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs", "exactly one job")
}

func TestValidateWorkflow_InvalidRiskLevel(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Steps[0].RiskLevel = "critical"
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.steps[0].risk_level", "invalid")
}

func TestValidateWorkflow_ValidRiskLevels(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level string
	}{
		{name: "empty", level: ""},
		{name: "low", level: "low"},
		{name: "medium", level: "medium"},
		{name: "high", level: "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := validWorkflow()
			wf.Jobs["build"].Steps[0].RiskLevel = tc.level
			errs := ValidateWorkflow(wf)
			assert.Empty(t, errs)
		})
	}
}

func TestValidateWorkflow_RiskLevelWithExpression(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Steps[0].RiskLevel = "${{ matrix.risk }}"
	errs := ValidateWorkflow(wf)
	assert.Empty(t, errs, "expression in risk_level should pass validation")
}

func TestValidateWorkflow_AnalyzeMissingPrompt(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Steps[0].Analyze = true
	wf.Jobs["build"].Steps[0].AnalysisPrompt = ""
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.steps[0].analysis_prompt", "required when analyze is true")
}

func TestValidateWorkflow_AnalyzeWithPrompt(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Steps[0].Analyze = true
	wf.Jobs["build"].Steps[0].AnalysisPrompt = "Check the output"
	errs := ValidateWorkflow(wf)
	assert.Empty(t, errs)
}

func TestValidateWorkflow_InvalidShell(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Steps[0].Shell = "powershell"
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.steps[0].shell", "invalid shell")
}

func TestValidateWorkflow_ValidShells(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shell string
	}{
		{name: "empty", shell: ""},
		{name: "true", shell: "true"},
		{name: "false", shell: "false"},
		{name: "sh", shell: "sh"},
		{name: "bash", shell: "bash"},
		{name: "zsh", shell: "zsh"},
		{name: "fish", shell: "fish"},
		{name: "pwsh", shell: "pwsh"},
		{name: "cmd", shell: "cmd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := validWorkflow()
			wf.Jobs["build"].Steps[0].Shell = tc.shell
			errs := ValidateWorkflow(wf)
			assert.Empty(t, errs)
		})
	}
}

func TestValidateWorkflow_InvalidSecretSource(t *testing.T) {
	wf := validWorkflow()
	wf.Secrets = []SecretDef{
		{Name: "MY_SECRET", From: "vault"},
	}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "secrets[0].from", "invalid secret source")
}

func TestValidateWorkflow_ValidSecretSources(t *testing.T) {
	for _, src := range []string{"env", "file", "interactive"} {
		t.Run(src, func(t *testing.T) {
			wf := validWorkflow()
			wf.Secrets = []SecretDef{
				{Name: "MY_SECRET", From: src},
			}
			errs := ValidateWorkflow(wf)
			assert.Empty(t, errs)
		})
	}
}

func TestValidateWorkflow_SecretMissingName(t *testing.T) {
	wf := validWorkflow()
	wf.Secrets = []SecretDef{
		{Name: "", From: "env"},
	}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "secrets[0].name", "required")
}

func TestValidateWorkflow_EmptyRequiresEntry(t *testing.T) {
	wf := validWorkflow()
	wf.Requires = []string{"pulumi", ""}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "requires[1]", "must not be empty")
}

func TestValidateWorkflow_CollectsAllErrors(t *testing.T) {
	// A workflow with multiple errors should report all of them.
	wf := &WorkflowDef{
		Name: "",
		Jobs: map[string]*JobDef{
			"build": {
				Steps: []*StepDef{
					{
						ID:        "dup",
						Name:      "first",
						Run:       "",
						RiskLevel: "critical",
					},
					{
						ID:   "dup",
						Name: "second",
						Run:  "echo ok",
					},
				},
			},
		},
		Secrets: []SecretDef{
			{Name: "", From: "vault"},
		},
	}
	errs := ValidateWorkflow(wf)
	assertFieldError(t, errs, "name", "required")
	assertFieldError(t, errs, "secrets[0].name", "required")
	assertFieldError(t, errs, "secrets[0].from", "invalid secret source")
	assertFieldError(t, errs, "jobs.build.steps[0].run", "required")
	assertFieldError(t, errs, "jobs.build.steps[0].risk_level", "invalid")
	assertFieldError(t, errs, "jobs.build.steps[1].id", "duplicate")
}

func TestValidateWorkflow_NilWorkflow(t *testing.T) {
	errs := ValidateWorkflow(nil)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "workflow", "required")
}

func TestValidateWorkflow_NilJobDefinition(t *testing.T) {
	wf := &WorkflowDef{
		Name: "nil-job",
		Jobs: map[string]*JobDef{
			"build": nil,
		},
	}

	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build", "job definition is nil")
}

func TestValidateWorkflow_NilStepDefinition(t *testing.T) {
	wf := &WorkflowDef{
		Name: "nil-step",
		Jobs: map[string]*JobDef{
			"build": {
				Steps: []*StepDef{nil},
			},
		},
	}

	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.steps[0]", "step definition is nil")
}

func TestValidateWorkflow_UnknownNeed(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Needs = []string{"deploy"}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.needs[0]", "unknown job")
}

func TestValidateWorkflow_DependencyCycle(t *testing.T) {
	wf := &WorkflowDef{
		Name: "cycle",
		Jobs: map[string]*JobDef{
			"build": {
				Needs: []string{"build"},
				Steps: []*StepDef{
					{ID: "s1", Name: "step", Run: "echo hi"},
				},
			},
		},
	}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs", "circular dependency")
}

func TestValidateWorkflow_MatrixIncludeKeysMustMatch(t *testing.T) {
	wf := validWorkflow()
	wf.Jobs["build"].Strategy = &StrategyDef{
		Matrix: &MatrixDef{
			Include: []map[string]string{
				{"os": "linux", "go": "1.22"},
				{"os": "darwin"},
			},
		},
	}
	errs := ValidateWorkflow(wf)
	require.NotEmpty(t, errs)
	assertFieldError(t, errs, "jobs.build.strategy.matrix.include[1]", "inconsistent keys")
}

// assertFieldError checks that at least one error matches the given field and contains the message substring.
func assertFieldError(t *testing.T, errs []ValidationError, field, msgSubstring string) {
	t.Helper()
	for _, e := range errs {
		if e.Field == field {
			assert.Contains(t, e.Message, msgSubstring, "error for field %q", field)
			return
		}
	}
	t.Errorf("no error found for field %q; errors: %v", field, errs)
}
