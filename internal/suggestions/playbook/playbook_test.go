package playbook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlaybook_BasicTasks(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
  - name: "test"
    command: "go test ./..."
  - name: "lint"
    command: "golangci-lint run"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)
	require.NotNil(t, pb)

	tasks := pb.AllTasks()
	assert.Len(t, tasks, 3)

	// All should have default priority (normal)
	for _, task := range tasks {
		assert.Equal(t, PriorityNormal, task.Priority)
	}

	// Verify individual tasks
	build := pb.GetTask("build")
	require.NotNil(t, build)
	assert.Equal(t, "go build ./...", build.Command)

	test := pb.GetTask("test")
	require.NotNil(t, test)
	assert.Equal(t, "go test ./...", test.Command)
}

func TestParsePlaybook_ExtendedFields(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
    priority: high
    tags:
      - "go"
      - "compile"
  - name: "deploy"
    command: "kubectl apply -f deploy/"
    after: "build"
    after_failure: "rollback"
    priority: normal
    workflows:
      - "deploy"
    tags:
      - "kubernetes"
      - "deploy"
  - name: "rollback"
    command: "kubectl rollout undo"
    priority: high
    tags:
      - "kubernetes"
      - "rollback"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	// Check deploy task
	deploy := pb.GetTask("deploy")
	require.NotNil(t, deploy)
	assert.Equal(t, "build", deploy.After)
	assert.Equal(t, "rollback", deploy.AfterFailure)
	assert.Equal(t, PriorityNormal, deploy.Priority)
	assert.Equal(t, []string{"deploy"}, deploy.Workflows)
	assert.Equal(t, []string{"kubernetes", "deploy"}, deploy.Tags)

	// Check priority ordering
	tasks := pb.AllTasks()
	assert.Equal(t, PriorityHigh, tasks[0].Priority)
	assert.Equal(t, PriorityHigh, tasks[1].Priority)
	assert.Equal(t, PriorityNormal, tasks[2].Priority)

	// Check workflow names
	wfNames := pb.WorkflowNames()
	assert.Equal(t, []string{"deploy"}, wfNames)
}

func TestParsePlaybook_CircularDependency(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "a"
    command: "echo a"
    after: "b"
  - name: "b"
    command: "echo b"
    after: "a"
`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCircularDependency)
}

func TestParsePlaybook_CircularDependencyThreeNodes(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "a"
    command: "echo a"
    after: "c"
  - name: "b"
    command: "echo b"
    after: "a"
  - name: "c"
    command: "echo c"
    after: "b"
`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCircularDependency)
}

func TestNextTasks_AfterSuccessfulCommand(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
  - name: "test"
    command: "go test ./..."
    after: "build"
  - name: "deploy"
    command: "kubectl apply -f deploy/"
    after: "build"
    priority: high
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	// After successful "build", should suggest "test" and "deploy"
	next := pb.NextTasks("build", false)
	require.Len(t, next, 2)

	// "deploy" should come first (high priority)
	assert.Equal(t, "deploy", next[0].Name)
	assert.Equal(t, "test", next[1].Name)
}

func TestNextTasks_AfterFailedCommand(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "deploy"
    command: "kubectl apply -f deploy/"
  - name: "rollback"
    command: "kubectl rollout undo"
    after_failure: "deploy"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	// After failed "deploy", should suggest "rollback"
	next := pb.NextTasks("deploy", true)
	require.Len(t, next, 1)
	assert.Equal(t, "rollback", next[0].Name)

	// After successful "deploy", should not suggest rollback
	next = pb.NextTasks("deploy", false)
	assert.Len(t, next, 0)
}

func TestNextTasks_MatchByCommand(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
  - name: "test"
    command: "go test ./..."
    after: "build"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	// Match by command text
	next := pb.NextTasks("go build ./...", false)
	require.Len(t, next, 1)
	assert.Equal(t, "test", next[0].Name)
}

func TestNextTasks_NoMatch(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
  - name: "test"
    command: "go test ./..."
    after: "build"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	// No match for unknown command
	next := pb.NextTasks("ls -la", false)
	assert.Len(t, next, 0)
}

func TestParsePlaybook_EmptyPlaybook(t *testing.T) {
	t.Parallel()

	yaml := `tasks: []`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyPlaybook)
}

func TestParsePlaybook_InvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := ParsePlaybook([]byte("not: valid: yaml: [[["))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidYAML)
}

func TestParsePlaybook_MissingName(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - command: "go build ./..."
`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestParsePlaybook_MissingCommand(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestParsePlaybook_MissingAfterTarget(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "deploy"
    command: "kubectl apply -f deploy/"
    after: "nonexistent"
`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingTask)
}

func TestParsePlaybook_MissingAfterFailureTarget(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "deploy"
    command: "kubectl apply -f deploy/"
    after_failure: "nonexistent"
`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMissingTask)
}

func TestParsePlaybook_DuplicateTaskName(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
  - name: "build"
    command: "make build"
`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateTask)
}

func TestParsePlaybook_InvalidPriority(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
    priority: "critical"
`
	_, err := ParsePlaybook([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid priority")
}

func TestParsePlaybook_DisabledTask(t *testing.T) {
	t.Parallel()

	disabled := false
	_ = disabled // suppress unused warning in yaml

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
  - name: "legacy"
    command: "make legacy"
    enabled: false
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	// AllTasks should exclude disabled tasks
	tasks := pb.AllTasks()
	assert.Len(t, tasks, 1)
	assert.Equal(t, "build", tasks[0].Name)

	// GetTask should still return disabled tasks
	legacy := pb.GetTask("legacy")
	require.NotNil(t, legacy)
	assert.False(t, legacy.IsEnabled())
}

func TestParsePlaybook_PrioritySorting(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "low_task"
    command: "echo low"
    priority: low
  - name: "high_task"
    command: "echo high"
    priority: high
  - name: "normal_task"
    command: "echo normal"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	tasks := pb.AllTasks()
	require.Len(t, tasks, 3)

	assert.Equal(t, "high_task", tasks[0].Name)
	assert.Equal(t, "normal_task", tasks[1].Name)
	assert.Equal(t, "low_task", tasks[2].Name)
}

func TestPlaybook_WorkflowAssociations(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
    workflows:
      - "ci"
      - "deploy"
  - name: "test"
    command: "go test ./..."
    workflows:
      - "ci"
  - name: "deploy"
    command: "kubectl apply -f deploy/"
    workflows:
      - "deploy"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	// Check workflow names
	wfNames := pb.WorkflowNames()
	assert.Equal(t, []string{"ci", "deploy"}, wfNames)

	// Check tasks for "ci" workflow
	ciTasks := pb.TasksForWorkflow("ci")
	assert.Len(t, ciTasks, 2)

	// Check tasks for "deploy" workflow
	deployTasks := pb.TasksForWorkflow("deploy")
	assert.Len(t, deployTasks, 2)

	// Check tasks for nonexistent workflow
	noneTasks := pb.TasksForWorkflow("nonexistent")
	assert.Len(t, noneTasks, 0)
}

func TestPlaybook_TaskNames(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
  - name: "test"
    command: "go test ./..."
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	names := pb.TaskNames()
	assert.Equal(t, []string{"build", "test"}, names)
}

func TestLoadPlaybook_FromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")

	yaml := `
tasks:
  - name: "build"
    command: "make build"
    description: "Build the project"
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	pb, err := LoadPlaybook(path)
	require.NoError(t, err)

	build := pb.GetTask("build")
	require.NotNil(t, build)
	assert.Equal(t, "make build", build.Command)
	assert.Equal(t, "Build the project", build.Description)
}

func TestLoadPlaybook_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadPlaybook("/nonexistent/path/tasks.yaml")
	require.Error(t, err)
}

func TestTask_IsEnabled_Default(t *testing.T) {
	t.Parallel()

	task := Task{Name: "test", Command: "echo"}
	assert.True(t, task.IsEnabled())
}

func TestTask_PriorityWeight_Unknown(t *testing.T) {
	t.Parallel()

	task := Task{Name: "test", Command: "echo", Priority: "unknown"}
	// Falls back to normal weight
	assert.Equal(t, priorityWeight[PriorityNormal], task.PriorityWeight())
}

func TestPlaybook_GetTask_NotFound(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "make build"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	assert.Nil(t, pb.GetTask("nonexistent"))
}

func TestParsePlaybook_DAGValidation_NoCycle(t *testing.T) {
	t.Parallel()

	// Linear chain: build -> test -> deploy -> notify
	yaml := `
tasks:
  - name: "build"
    command: "make build"
  - name: "test"
    command: "make test"
    after: "build"
  - name: "deploy"
    command: "make deploy"
    after: "test"
  - name: "notify"
    command: "make notify"
    after: "deploy"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)
	require.NotNil(t, pb)

	// After build, get test
	next := pb.NextTasks("build", false)
	require.Len(t, next, 1)
	assert.Equal(t, "test", next[0].Name)

	// After test, get deploy
	next = pb.NextTasks("test", false)
	require.Len(t, next, 1)
	assert.Equal(t, "deploy", next[0].Name)

	// After deploy, get notify
	next = pb.NextTasks("deploy", false)
	require.Len(t, next, 1)
	assert.Equal(t, "notify", next[0].Name)
}

func TestNextTasks_DisabledTasksExcluded(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "go build ./..."
  - name: "test"
    command: "go test ./..."
    after: "build"
    enabled: false
  - name: "lint"
    command: "golangci-lint run"
    after: "build"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	next := pb.NextTasks("build", false)
	require.Len(t, next, 1)
	assert.Equal(t, "lint", next[0].Name)
}

func TestParsePlaybook_CaseSensitivePriority(t *testing.T) {
	t.Parallel()

	yaml := `
tasks:
  - name: "build"
    command: "make build"
    priority: "HIGH"
`
	pb, err := ParsePlaybook([]byte(yaml))
	require.NoError(t, err)

	build := pb.GetTask("build")
	require.NotNil(t, build)
	assert.Equal(t, PriorityHigh, build.Priority)
}
