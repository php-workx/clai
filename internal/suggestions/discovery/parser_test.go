package discovery

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewParser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parserType string
		path       string
		pattern    string
		wantErr    bool
	}{
		{"json_keys", ParserTypeJSONKeys, "scripts", "", false},
		{"json_array", ParserTypeJSONArray, "tasks", "", false},
		{"regex_lines", ParserTypeRegexLines, "", `^(\w+):`, false},
		{"make_qp", ParserTypeMakeQP, "", "", false},
		{"unknown", "unknown", "", "", true},
		{"invalid_regex", ParserTypeRegexLines, "", "[invalid", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ParserConfig{
				Type:    tc.parserType,
				Path:    tc.path,
				Pattern: tc.pattern,
			}
			parser, err := NewParser(cfg, "test")
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, parser)
			}
		})
	}
}

func TestJSONKeysParser_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "simple object",
			path:  "scripts",
			input: `{"scripts": {"build": "npm run build", "test": "jest"}}`,
			want:  []string{"build", "test"},
		},
		{
			name:  "nested path",
			path:  "config.tasks",
			input: `{"config": {"tasks": {"lint": "eslint", "format": "prettier"}}}`,
			want:  []string{"lint", "format"},
		},
		{
			name:  "root object",
			path:  ".",
			input: `{"build": "make", "test": "make test"}`,
			want:  []string{"build", "test"},
		},
		{
			name:    "invalid json",
			path:    "scripts",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "path not found",
			path:    "missing",
			input:   `{"scripts": {}}`,
			wantErr: true,
		},
		{
			name:    "path not object",
			path:    "scripts",
			input:   `{"scripts": ["a", "b"]}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := &JSONKeysParser{path: tc.path, kind: "test"}
			tasks, err := parser.Parse([]byte(tc.input))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			names := make([]string, len(tasks))
			for i, task := range tasks {
				names[i] = task.Name
			}
			assert.ElementsMatch(t, tc.want, names)

			// Verify commands are generated correctly
			for _, task := range tasks {
				assert.Equal(t, "test "+task.Name, task.Command)
			}
		})
	}
}

func TestJSONKeysParser_ParseWithDescriptions(t *testing.T) {
	t.Parallel()

	input := `{"scripts": {"build": "Builds the project", "test": "Runs tests"}}`
	parser := &JSONKeysParser{path: "scripts", kind: "npm"}

	tasks, err := parser.Parse([]byte(input))
	require.NoError(t, err)
	assert.Len(t, tasks, 2)

	// Values are strings, should be used as descriptions
	for _, task := range tasks {
		if task.Name == "build" {
			assert.Equal(t, "Builds the project", task.Description)
		}
	}
}

func TestJSONArrayParser_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "string array",
			path:  "tasks",
			input: `{"tasks": ["build", "test", "lint"]}`,
			want:  []string{"build", "test", "lint"},
		},
		{
			name:  "object array with name field",
			path:  "recipes",
			input: `{"recipes": [{"name": "build"}, {"name": "test"}]}`,
			want:  []string{"build", "test"},
		},
		{
			name:  "mixed array",
			path:  "items",
			input: `{"items": ["simple", {"name": "complex", "description": "desc"}]}`,
			want:  []string{"simple", "complex"},
		},
		{
			name:    "invalid json",
			path:    "tasks",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "path not array",
			path:    "tasks",
			input:   `{"tasks": {"a": "b"}}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := &JSONArrayParser{path: tc.path, kind: "test"}
			tasks, err := parser.Parse([]byte(tc.input))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			names := make([]string, len(tasks))
			for i, task := range tasks {
				names[i] = task.Name
			}
			assert.ElementsMatch(t, tc.want, names)
		})
	}
}

func TestJSONArrayParser_ParseWithDescriptions(t *testing.T) {
	t.Parallel()

	input := `{"recipes": [{"name": "build", "description": "Build the project"}, {"name": "test"}]}`
	parser := &JSONArrayParser{path: "recipes", kind: "just"}

	tasks, err := parser.Parse([]byte(input))
	require.NoError(t, err)
	assert.Len(t, tasks, 2)

	for _, task := range tasks {
		if task.Name == "build" {
			assert.Equal(t, "Build the project", task.Description)
			assert.Equal(t, "just build", task.Command)
		}
	}
}

func TestRegexLinesParser_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		input   string
		want    []string
	}{
		{
			name:    "simple pattern",
			pattern: `^(\w+):`,
			input:   "build:\ntest:\n# comment\nclean:",
			want:    []string{"build", "test", "clean"},
		},
		{
			name:    "with description capture",
			pattern: `^\* (\w+):\s+(.+)$`,
			input:   "* build: Builds the project\n* test: Runs tests\nNot a task",
			want:    []string{"build", "test"},
		},
		{
			name:    "no matches",
			pattern: `^target: (\w+)$`,
			input:   "nothing matches here",
			want:    nil,
		},
		{
			name:    "empty lines",
			pattern: `^(\w+)$`,
			input:   "\nbuild\n\ntest\n",
			want:    []string{"build", "test"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			regex := regexp.MustCompile(tc.pattern)
			parser := &RegexLinesParser{regex: regex, kind: "test"}

			tasks, err := parser.Parse([]byte(tc.input))
			require.NoError(t, err)

			if tc.want == nil {
				assert.Empty(t, tasks)
				return
			}

			names := make([]string, len(tasks))
			for i, task := range tasks {
				names[i] = task.Name
			}
			assert.Equal(t, tc.want, names)
		})
	}
}

func TestRegexLinesParser_ParseWithDescriptions(t *testing.T) {
	t.Parallel()

	input := "* build: Builds everything\n* test: Runs the test suite"
	regex := regexp.MustCompile(`^\* (\w+):\s+(.+)$`)
	parser := &RegexLinesParser{regex: regex, kind: "task"}

	tasks, err := parser.Parse([]byte(input))
	require.NoError(t, err)
	assert.Len(t, tasks, 2)

	for _, task := range tasks {
		if task.Name == "build" {
			assert.Equal(t, "Builds everything", task.Description)
			assert.Equal(t, "task build", task.Command)
		}
	}
}

func TestMakeQPParser_Parse(t *testing.T) {
	t.Parallel()

	// Simulated make -qp output
	input := `# GNU Make 4.3
# Reading makefiles...

# Files
# Not a target:
.PHONY:

build:
	echo "building"

test:
	echo "testing"

# Not a target:
.DEFAULT:

clean:
	rm -rf build/
`

	parser := &MakeQPParser{}
	tasks, err := parser.Parse([]byte(input))
	require.NoError(t, err)

	names := make([]string, len(tasks))
	for i, task := range tasks {
		names[i] = task.Name
	}

	assert.Contains(t, names, "build")
	assert.Contains(t, names, "test")
	assert.Contains(t, names, "clean")

	// Verify command generation
	for _, task := range tasks {
		assert.Equal(t, "make "+task.Name, task.Command)
	}
}

func TestMakeQPParser_Parse_SkipsSpecialTargets(t *testing.T) {
	t.Parallel()

	input := `# Files
.PHONY:

.DEFAULT:

.SUFFIXES:

build:
	echo "build"
`

	parser := &MakeQPParser{}
	tasks, err := parser.Parse([]byte(input))
	require.NoError(t, err)

	// Should only have "build", not the special targets
	assert.Len(t, tasks, 1)
	assert.Equal(t, "build", tasks[0].Name)
}

func TestNavigateJSONPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    interface{}
		path    string
		want    interface{}
		wantErr bool
	}{
		{
			name: "empty path",
			data: map[string]interface{}{"a": 1},
			path: "",
			want: map[string]interface{}{"a": 1},
		},
		{
			name: "dot path",
			data: map[string]interface{}{"a": 1},
			path: ".",
			want: map[string]interface{}{"a": 1},
		},
		{
			name: "single level",
			data: map[string]interface{}{"scripts": map[string]interface{}{"build": "npm build"}},
			path: "scripts",
			want: map[string]interface{}{"build": "npm build"},
		},
		{
			name: "nested path",
			data: map[string]interface{}{
				"config": map[string]interface{}{
					"tasks": map[string]interface{}{"a": 1},
				},
			},
			path: "config.tasks",
			want: map[string]interface{}{"a": 1},
		},
		{
			name:    "path not found",
			data:    map[string]interface{}{"a": 1},
			path:    "b",
			wantErr: true,
		},
		{
			name:    "path through non-object",
			data:    map[string]interface{}{"a": "string"},
			path:    "a.b",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := navigateJSONPath(tc.data, tc.path)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, result)
		})
	}
}
