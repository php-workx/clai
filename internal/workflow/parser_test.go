package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWorkflow_ValidMinimal(t *testing.T) {
	yaml := `
name: minimal
jobs:
  build:
    steps:
      - name: run build
        run: go build ./...
`
	wf, err := ParseWorkflow([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "minimal", wf.Name)
	require.Contains(t, wf.Jobs, "build")
	require.Len(t, wf.Jobs["build"].Steps, 1)
	assert.Equal(t, "run build", wf.Jobs["build"].Steps[0].Name)
	assert.Equal(t, "go build ./...", wf.Jobs["build"].Steps[0].Run)
}

func TestParseWorkflow_FullExample(t *testing.T) {
	yaml := `
name: pulumi-compliance
env:
  PULUMI_CONFIG_PASSPHRASE: ""
secrets:
  - name: AWS_ACCESS_KEY_ID
    from: env
  - name: AWS_SECRET_ACCESS_KEY
    from: env
requires:
  - pulumi
  - jq
jobs:
  compliance:
    name: Run compliance checks
    strategy:
      matrix:
        include:
          - stack: dev
            region: us-east-1
          - stack: staging
            region: us-west-2
    steps:
      - id: list-stacks
        name: List stacks
        run: pulumi stack ls --json
      - id: check-config
        name: Check stack config
        shell: true
        run: pulumi config get aws:region | grep ${{ matrix.region }}
        analyze: true
        analysis_prompt: Check if region matches expected value
        risk_level: low
      - id: preview
        name: Preview changes
        shell: bash
        run: pulumi preview --json --stack ${{ matrix.stack }}
        analyze: true
        analysis_prompt: Review the Pulumi preview for unexpected changes
        risk_level: high
`
	wf, err := ParseWorkflow([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, "pulumi-compliance", wf.Name)
	assert.Equal(t, "", wf.Env["PULUMI_CONFIG_PASSPHRASE"])
	require.Len(t, wf.Secrets, 2)
	assert.Equal(t, "AWS_ACCESS_KEY_ID", wf.Secrets[0].Name)
	assert.Equal(t, "env", wf.Secrets[0].From)
	assert.Equal(t, []string{"pulumi", "jq"}, wf.Requires)

	job := wf.Jobs["compliance"]
	require.NotNil(t, job)
	assert.Equal(t, "Run compliance checks", job.Name)

	// Matrix
	require.NotNil(t, job.Strategy)
	require.NotNil(t, job.Strategy.Matrix)
	require.Len(t, job.Strategy.Matrix.Include, 2)
	assert.Equal(t, "dev", job.Strategy.Matrix.Include[0]["stack"])
	assert.Equal(t, "us-west-2", job.Strategy.Matrix.Include[1]["region"])

	// Steps
	require.Len(t, job.Steps, 3)

	// Step 0: omitted shell uses default shell mode.
	assert.Equal(t, "list-stacks", job.Steps[0].ID)
	assert.Equal(t, "", job.Steps[0].Shell)
	assert.Equal(t, "default", job.Steps[0].ShellMode())

	// Step 1: shell: true
	assert.Equal(t, "true", job.Steps[1].Shell)
	assert.Equal(t, "default", job.Steps[1].ShellMode())
	assert.True(t, job.Steps[1].Analyze)
	assert.Equal(t, "low", job.Steps[1].RiskLevel)

	// Step 2: shell: bash
	assert.Equal(t, "bash", job.Steps[2].Shell)
	assert.Equal(t, "bash", job.Steps[2].ShellMode())
	assert.Equal(t, "high", job.Steps[2].RiskLevel)
}

func TestParseWorkflow_ShellFieldBoolTrue(t *testing.T) {
	yaml := `
name: shell-bool
jobs:
  test:
    steps:
      - name: with shell
        run: echo hello | grep hello
        shell: true
`
	wf, err := ParseWorkflow([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "true", wf.Jobs["test"].Steps[0].Shell)
	assert.Equal(t, "default", wf.Jobs["test"].Steps[0].ShellMode())
}

func TestParseWorkflow_ShellFieldBoolFalse(t *testing.T) {
	yaml := `
name: shell-bool-false
jobs:
  test:
    steps:
      - name: no shell
        run: echo hello
        shell: false
`
	wf, err := ParseWorkflow([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "false", wf.Jobs["test"].Steps[0].Shell)
	assert.Equal(t, "argv", wf.Jobs["test"].Steps[0].ShellMode())
}

func TestParseWorkflow_ShellFieldString(t *testing.T) {
	shells := []string{"sh", "bash", "zsh", "fish", "pwsh", "cmd"}
	for _, sh := range shells {
		t.Run(sh, func(t *testing.T) {
			yaml := `
name: shell-string
jobs:
  test:
    steps:
      - name: explicit shell
        run: echo hello
        shell: ` + sh + `
`
			wf, err := ParseWorkflow([]byte(yaml))
			require.NoError(t, err)
			assert.Equal(t, sh, wf.Jobs["test"].Steps[0].Shell)
			assert.Equal(t, sh, wf.Jobs["test"].Steps[0].ShellMode())
		})
	}
}

func TestParseWorkflow_ShellOmitted(t *testing.T) {
	yaml := `
name: no-shell
jobs:
  test:
    steps:
      - name: argv mode
        run: go test ./...
`
	wf, err := ParseWorkflow([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "", wf.Jobs["test"].Steps[0].Shell)
	assert.Equal(t, "default", wf.Jobs["test"].Steps[0].ShellMode())
}

func TestParseWorkflow_UnknownFieldsRejected(t *testing.T) {
	yaml := `
name: unknown-fields
jobs:
  test:
    steps:
      - name: step one
        run: echo hello
        unknown_field: value
`
	_, err := ParseWorkflow([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown_field")
}

func TestParseWorkflow_UnknownTopLevelField(t *testing.T) {
	yaml := `
name: bad-top
bogus: thing
jobs:
  test:
    steps:
      - name: step one
        run: echo hello
`
	_, err := ParseWorkflow([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func TestParseWorkflow_UnknownJobField(t *testing.T) {
	yaml := `
name: bad-job
jobs:
  test:
    badfield: true
    steps:
      - name: step one
        run: echo hello
`
	_, err := ParseWorkflow([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "badfield")
}

func TestParseWorkflow_EmptyInput(t *testing.T) {
	_, err := ParseWorkflow([]byte(""))
	require.Error(t, err)
}

func TestParseWorkflow_InvalidYAML(t *testing.T) {
	_, err := ParseWorkflow([]byte("not: [valid: yaml"))
	require.Error(t, err)
}

func TestParseWorkflow_StepEnv(t *testing.T) {
	yaml := `
name: with-env
env:
  GLOBAL: value
jobs:
  test:
    env:
      JOB_VAR: jobval
    steps:
      - name: step with env
        run: echo hello
        env:
          STEP_VAR: stepval
`
	wf, err := ParseWorkflow([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "value", wf.Env["GLOBAL"])
	assert.Equal(t, "jobval", wf.Jobs["test"].Env["JOB_VAR"])
	assert.Equal(t, "stepval", wf.Jobs["test"].Steps[0].Env["STEP_VAR"])
}

func TestParseWorkflow_MatrixInclude(t *testing.T) {
	yaml := `
name: matrix-test
jobs:
  deploy:
    strategy:
      matrix:
        include:
          - env: dev
            region: us-east-1
          - env: prod
            region: eu-west-1
    steps:
      - name: deploy
        run: deploy --env ${{ matrix.env }}
`
	wf, err := ParseWorkflow([]byte(yaml))
	require.NoError(t, err)
	require.NotNil(t, wf.Jobs["deploy"].Strategy)
	require.NotNil(t, wf.Jobs["deploy"].Strategy.Matrix)
	assert.Len(t, wf.Jobs["deploy"].Strategy.Matrix.Include, 2)
	assert.Equal(t, "dev", wf.Jobs["deploy"].Strategy.Matrix.Include[0]["env"])
	assert.Equal(t, "eu-west-1", wf.Jobs["deploy"].Strategy.Matrix.Include[1]["region"])
}

func TestSanitizePathComponent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple", input: "hello", want: "hello"},
		{name: "empty", input: "", want: "_"},
		{name: "slashes", input: "a/b\\c", want: "a_b_c"},
		{name: "colons", input: "step:one", want: "step_one"},
		{name: "special chars", input: `a*b?c"d<e>f|g`, want: "a_b_c_d_e_f_g"},
		{name: "dots only", input: "...", want: "_"},
		{name: "path traversal", input: "../../../etc/passwd", want: "_etc_passwd"},
		{name: "windows reserved CON", input: "CON", want: "_CON"},
		{name: "windows reserved con lowercase", input: "con", want: "_con"},
		{name: "windows reserved NUL", input: "NUL.txt", want: "_NUL.txt"},
		{name: "windows reserved COM1", input: "COM1", want: "_COM1"},
		{name: "leading dots", input: ".hidden", want: "hidden"},
		{name: "trailing dots", input: "file.", want: "file"},
		{name: "leading spaces", input: "  file", want: "file"},
		{name: "control chars", input: "a\x00b\x1fc", want: "a_b_c"},
		{name: "unicode safe", input: "hello-world_123", want: "hello-world_123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizePathComponent(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizePathComponent_Truncation(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	result := sanitizePathComponent(string(long))
	assert.LessOrEqual(t, len(result), maxPathComponentLen)
}
