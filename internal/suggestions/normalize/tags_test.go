package normalize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractTags_Git(t *testing.T) {
	segs := []Segment{{Raw: "git commit -m 'msg'"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "vcs")
	assert.Contains(t, tags, "git")
}

func TestExtractTags_GoTest(t *testing.T) {
	segs := []Segment{{Raw: "go test ./..."}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "go")
	assert.Contains(t, tags, "test")
}

func TestExtractTags_GoBuild(t *testing.T) {
	segs := []Segment{{Raw: "go build ./cmd/app"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "go")
	assert.Contains(t, tags, "build")
}

func TestExtractTags_GoVet(t *testing.T) {
	segs := []Segment{{Raw: "go vet ./..."}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "go")
	assert.Contains(t, tags, "lint")
}

func TestExtractTags_Docker(t *testing.T) {
	segs := []Segment{{Raw: "docker run nginx"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "container")
	assert.Contains(t, tags, "docker")
}

func TestExtractTags_Npm(t *testing.T) {
	segs := []Segment{{Raw: "npm install express"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "js")
	assert.Contains(t, tags, "package")
}

func TestExtractTags_Make(t *testing.T) {
	segs := []Segment{{Raw: "make build"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "build")
}

func TestExtractTags_Kubectl(t *testing.T) {
	segs := []Segment{{Raw: "kubectl get pods"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "k8s")
	assert.Contains(t, tags, "container")
}

func TestExtractTags_Curl(t *testing.T) {
	segs := []Segment{{Raw: "curl https://api.example.com"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "network")
	assert.Contains(t, tags, "http")
}

func TestExtractTags_Vim(t *testing.T) {
	segs := []Segment{{Raw: "vim /etc/hosts"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "editor")
}

func TestExtractTags_Unknown(t *testing.T) {
	segs := []Segment{{Raw: "myunknowncmd --flag"}}
	tags := ExtractTags(segs)
	assert.Nil(t, tags)
}

func TestExtractTags_Empty(t *testing.T) {
	tags := ExtractTags(nil)
	assert.Nil(t, tags)

	tags = ExtractTags([]Segment{})
	assert.Nil(t, tags)
}

func TestExtractTags_PipelineMerges(t *testing.T) {
	segs := []Segment{
		{Raw: "cat /etc/hosts", Operator: OpPipe},
		{Raw: "grep localhost"},
	}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "shell")
	assert.Contains(t, tags, "file")
	assert.Contains(t, tags, "search")
}

func TestExtractTags_Pytest(t *testing.T) {
	segs := []Segment{{Raw: "pytest tests/"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "python")
	assert.Contains(t, tags, "test")
}

func TestExtractTags_Terraform(t *testing.T) {
	segs := []Segment{{Raw: "terraform plan"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "iac")
	assert.Contains(t, tags, "cloud")
}

func TestExtractTags_Aws(t *testing.T) {
	segs := []Segment{{Raw: "aws s3 ls"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "cloud")
	assert.Contains(t, tags, "aws")
}

func TestExtractTags_Cargo(t *testing.T) {
	segs := []Segment{{Raw: "cargo build --release"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "rust")
	assert.Contains(t, tags, "package")
}

func TestExtractTags_Brew(t *testing.T) {
	segs := []Segment{{Raw: "brew install vim"}}
	tags := ExtractTags(segs)
	assert.Contains(t, tags, "package")
	assert.Contains(t, tags, "system")
}

func TestExtractCommandAndSub(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantSub string
	}{
		{"", "", ""},
		{"ls", "ls", ""},
		{"git commit -m msg", "git", "commit"},
		{"go test ./...", "go", "test"},
		{"docker -v", "docker", ""},
		{"GIT status", "git", "status"},
	}

	for _, tt := range tests {
		cmd, sub := extractCommandAndSub(tt.input)
		assert.Equal(t, tt.wantCmd, cmd, "input: %q", tt.input)
		assert.Equal(t, tt.wantSub, sub, "input: %q", tt.input)
	}
}
