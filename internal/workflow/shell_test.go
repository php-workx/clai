package workflow

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewShellAdapter(t *testing.T) {
	adapter := NewShellAdapter()
	assert.NotNil(t, adapter)
}

func TestBuildCommand_ArgvMode(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   "echo hello world",
		Shell: "", // argv mode
	}
	env := []string{"FOO=bar"}
	cmd, err := adapter.BuildCommand(ctx, step, "/tmp", env, "/tmp/output.txt")
	require.NoError(t, err)

	assert.Equal(t, "echo", cmd.Path[len(cmd.Path)-4:]) // ends with "echo" (full path resolved by exec)
	assert.Equal(t, []string{"echo", "hello", "world"}, cmd.Args)
	assert.Equal(t, "/tmp", cmd.Dir)
}

func TestBuildCommand_ArgvMode_QuotedArgs(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   `pulumi stack ls --json`,
		Shell: "",
	}
	cmd, err := adapter.BuildCommand(ctx, step, "/tmp", nil, "/tmp/output.txt")
	require.NoError(t, err)

	assert.Equal(t, []string{"pulumi", "stack", "ls", "--json"}, cmd.Args)
}

func TestBuildCommand_ArgvMode_QuotedString(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   `myapp --name "hello world"`,
		Shell: "",
	}
	cmd, err := adapter.BuildCommand(ctx, step, "/tmp", nil, "/tmp/output.txt")
	require.NoError(t, err)

	assert.Equal(t, []string{"myapp", "--name", "hello world"}, cmd.Args)
}

func TestBuildCommand_ShellModeTrue(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   "echo hello | grep hello",
		Shell: "true",
	}
	cmd, err := adapter.BuildCommand(ctx, step, "/tmp", nil, "/tmp/output.txt")
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		assert.Equal(t, []string{"cmd.exe", "/C", "echo hello | grep hello"}, cmd.Args)
	} else {
		assert.Equal(t, []string{"/bin/sh", "-c", "echo hello | grep hello"}, cmd.Args)
	}
	assert.Equal(t, "/tmp", cmd.Dir)
}

func TestBuildCommand_ExplicitShell(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   "echo hello",
		Shell: "bash",
	}
	cmd, err := adapter.BuildCommand(ctx, step, "/tmp", nil, "/tmp/output.txt")
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		assert.Equal(t, []string{"bash", "-c", "echo hello"}, cmd.Args)
	} else {
		assert.Equal(t, []string{"bash", "-c", "echo hello"}, cmd.Args)
	}
}

func TestBuildCommand_CLAIOutputEnv(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   "echo test",
		Shell: "",
	}
	env := []string{"PATH=/usr/bin"}
	cmd, err := adapter.BuildCommand(ctx, step, "/tmp", env, "/tmp/step-output.txt")
	require.NoError(t, err)

	assert.Contains(t, cmd.Env, "PATH=/usr/bin")
	assert.Contains(t, cmd.Env, "CLAI_OUTPUT=/tmp/step-output.txt")
}

func TestBuildCommand_EnvMerging(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   "myapp",
		Shell: "",
	}
	env := []string{"FOO=bar", "BAZ=qux", "HOME=/home/test"}
	cmd, err := adapter.BuildCommand(ctx, step, "/tmp", env, "/tmp/output.txt")
	require.NoError(t, err)

	assert.Contains(t, cmd.Env, "FOO=bar")
	assert.Contains(t, cmd.Env, "BAZ=qux")
	assert.Contains(t, cmd.Env, "HOME=/home/test")
	assert.Contains(t, cmd.Env, "CLAI_OUTPUT=/tmp/output.txt")
}

func TestBuildCommand_EmptyRun(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   "",
		Shell: "",
	}
	_, err := adapter.BuildCommand(ctx, step, "/tmp", nil, "/tmp/output.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestBuildCommand_WorkDir(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   "ls",
		Shell: "",
	}
	cmd, err := adapter.BuildCommand(ctx, step, "/var/data", nil, "/tmp/output.txt")
	require.NoError(t, err)
	assert.Equal(t, "/var/data", cmd.Dir)
}

func TestBuildCommand_NilEnv(t *testing.T) {
	adapter := NewShellAdapter()
	ctx := context.Background()

	step := &StepDef{
		Run:   "echo test",
		Shell: "",
	}
	cmd, err := adapter.BuildCommand(ctx, step, "/tmp", nil, "/tmp/output.txt")
	require.NoError(t, err)

	// Even with nil env, CLAI_OUTPUT should be set.
	assert.Contains(t, cmd.Env, "CLAI_OUTPUT=/tmp/output.txt")
}
