package normalize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitPipeline_Empty(t *testing.T) {
	assert.Nil(t, SplitPipeline(""))
	assert.Nil(t, SplitPipeline("   "))
}

func TestSplitPipeline_SimpleCommand(t *testing.T) {
	segs := SplitPipeline("ls -la")
	require.Len(t, segs, 1)
	assert.Equal(t, "ls -la", segs[0].Raw)
	assert.Equal(t, Operator(""), segs[0].Operator)
}

func TestSplitPipeline_Pipe(t *testing.T) {
	segs := SplitPipeline("cat file.txt | grep foo")
	require.Len(t, segs, 2)
	assert.Equal(t, "cat file.txt", segs[0].Raw)
	assert.Equal(t, OpPipe, segs[0].Operator)
	assert.Equal(t, "grep foo", segs[1].Raw)
	assert.Equal(t, Operator(""), segs[1].Operator)
}

func TestSplitPipeline_And(t *testing.T) {
	segs := SplitPipeline("mkdir foo && cd foo")
	require.Len(t, segs, 2)
	assert.Equal(t, "mkdir foo", segs[0].Raw)
	assert.Equal(t, OpAnd, segs[0].Operator)
	assert.Equal(t, "cd foo", segs[1].Raw)
}

func TestSplitPipeline_Or(t *testing.T) {
	segs := SplitPipeline("test -f foo || echo missing")
	require.Len(t, segs, 2)
	assert.Equal(t, "test -f foo", segs[0].Raw)
	assert.Equal(t, OpOr, segs[0].Operator)
	assert.Equal(t, "echo missing", segs[1].Raw)
}

func TestSplitPipeline_Semicolon(t *testing.T) {
	segs := SplitPipeline("echo hello; echo world")
	require.Len(t, segs, 2)
	assert.Equal(t, "echo hello", segs[0].Raw)
	assert.Equal(t, OpSemicolon, segs[0].Operator)
	assert.Equal(t, "echo world", segs[1].Raw)
}

func TestSplitPipeline_MultiplePipes(t *testing.T) {
	segs := SplitPipeline("cat file | grep foo | wc -l")
	require.Len(t, segs, 3)
	assert.Equal(t, "cat file", segs[0].Raw)
	assert.Equal(t, OpPipe, segs[0].Operator)
	assert.Equal(t, "grep foo", segs[1].Raw)
	assert.Equal(t, OpPipe, segs[1].Operator)
	assert.Equal(t, "wc -l", segs[2].Raw)
}

func TestSplitPipeline_MixedOperators(t *testing.T) {
	segs := SplitPipeline("make build && ./bin/app | tee log.txt; echo done")
	require.Len(t, segs, 4)
	assert.Equal(t, "make build", segs[0].Raw)
	assert.Equal(t, OpAnd, segs[0].Operator)
	assert.Equal(t, "./bin/app", segs[1].Raw)
	assert.Equal(t, OpPipe, segs[1].Operator)
	assert.Equal(t, "tee log.txt", segs[2].Raw)
	assert.Equal(t, OpSemicolon, segs[2].Operator)
	assert.Equal(t, "echo done", segs[3].Raw)
}

func TestSplitPipeline_QuotedStringsPreserved(t *testing.T) {
	segs := SplitPipeline(`echo "hello | world" | grep hello`)
	require.Len(t, segs, 2)
	assert.Equal(t, `echo "hello | world"`, segs[0].Raw)
	assert.Equal(t, "grep hello", segs[1].Raw)
}

func TestSplitPipeline_SingleQuotedStringsPreserved(t *testing.T) {
	segs := SplitPipeline(`echo 'a && b' && echo c`)
	require.Len(t, segs, 2)
	assert.Equal(t, `echo 'a && b'`, segs[0].Raw)
	assert.Equal(t, "echo c", segs[1].Raw)
}

func TestSplitPipeline_EscapedPipe(t *testing.T) {
	segs := SplitPipeline(`echo hello\|world`)
	require.Len(t, segs, 1)
	assert.Equal(t, `echo hello\|world`, segs[0].Raw)
}

func TestSplitPipeline_WhitespaceTrimming(t *testing.T) {
	segs := SplitPipeline("  ls -la  |  grep foo  ")
	require.Len(t, segs, 2)
	assert.Equal(t, "ls -la", segs[0].Raw)
	assert.Equal(t, "grep foo", segs[1].Raw)
}

func TestSplitPipeline_NestedQuotes(t *testing.T) {
	segs := SplitPipeline(`echo "it's a 'test'" | cat`)
	require.Len(t, segs, 2)
	assert.Equal(t, `echo "it's a 'test'"`, segs[0].Raw)
	assert.Equal(t, "cat", segs[1].Raw)
}

func TestReassemblePipeline_Empty(t *testing.T) {
	assert.Equal(t, "", ReassemblePipeline(nil))
	assert.Equal(t, "", ReassemblePipeline([]Segment{}))
}

func TestReassemblePipeline_Single(t *testing.T) {
	segs := []Segment{{Raw: "ls -la"}}
	assert.Equal(t, "ls -la", ReassemblePipeline(segs))
}

func TestReassemblePipeline_Multiple(t *testing.T) {
	segs := []Segment{
		{Raw: "cat file", Operator: OpPipe},
		{Raw: "grep foo", Operator: OpPipe},
		{Raw: "wc -l"},
	}
	assert.Equal(t, "cat file | grep foo | wc -l", ReassemblePipeline(segs))
}

func TestPipeline_RoundTrip(t *testing.T) {
	original := "make build && ./bin/app | tee log.txt"
	segs := SplitPipeline(original)
	reassembled := ReassemblePipeline(segs)
	assert.Equal(t, original, reassembled)
}
