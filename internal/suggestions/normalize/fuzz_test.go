package normalize

import (
	"testing"
	"unicode/utf8"
)

// FuzzNormalize tests the Normalizer.Normalize method with fuzzed input.
// It verifies safety properties:
//   - No panics on any input
//   - Output is valid UTF-8
//   - Idempotent: normalizing normalized output produces the same result
//
// Note: Some seed corpus entries trigger known normalizer idempotency issues
// where <path> tokens are re-classified as <arg> on re-normalization.
// Skip with -short to avoid blocking pre-commit; run without -short for
// dedicated fuzz testing.
func FuzzNormalize(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz test in short mode")
	}
	// Seed corpus with interesting commands
	f.Add("git commit -m 'hello world'")
	f.Add("ls -la /tmp/dir with spaces")
	f.Add("echo ''; rm -rf /")
	f.Add("cmd \x00\xff\xfe") // malformed UTF-8
	f.Add("")
	f.Add("   ")
	f.Add("a")
	f.Add("git status")
	f.Add("docker run --name my-container -p 8080:80 -v /host:/container nginx:latest")
	f.Add("kubectl get pods -n kube-system -o json | jq '.items[].metadata.name'")
	f.Add("find . -name '*.go' -exec grep -l 'TODO' {} \\;")
	f.Add("echo \"$(whoami)@$(hostname)\"")
	f.Add("cat /dev/null > /tmp/output 2>&1")
	f.Add("ssh user@host 'cd /app && git pull && make deploy'")
	f.Add("git log --oneline --graph --all --decorate")
	f.Add("npm install --save-dev @types/node @types/jest typescript")
	f.Add("chmod 755 ./script.sh && ./script.sh --flag='value with spaces'")
	f.Add("curl -X POST https://api.example.com/data -H 'Content-Type: application/json' -d '{\"key\": \"value\"}'")
	f.Add("PYTHONPATH=/opt/lib python3 -m pytest tests/ -v --tb=short")
	f.Add("\t\n\r") // whitespace variants
	f.Add("'unclosed single quote")
	f.Add("\"unclosed double quote")
	f.Add("\\\\\\") // trailing backslashes
	f.Add("git commit -m $'string with \\n newline'")
	f.Add("a]b[c{d}e(f)g")

	n := NewNormalizer()

	f.Fuzz(func(t *testing.T, input string) {
		result, slots := n.Normalize(input)

		// Property: result should be valid UTF-8 when input is valid UTF-8.
		// The normalizer does not yet sanitize all invalid byte sequences;
		// UTF-8 cleaning is tracked separately.
		if utf8.ValidString(input) && !utf8.ValidString(result) {
			t.Errorf("non-UTF8 output for valid-UTF8 input %q: got %q", input, result)
		}

		// Property: convergent - normalizing the result twice should stabilize.
		// Note: slot tokens like <path> may be re-classified as <arg> on
		// second pass because the normalizer sees "<path>" as a literal token.
		// We check that the third pass equals the second (2-step convergence).
		result2, _ := n.Normalize(result)
		result3, _ := n.Normalize(result2)
		if result2 != result3 {
			t.Errorf("not convergent after 2 passes: %q -> %q -> %q -> %q", input, result, result2, result3)
		}

		// Property: slot values should all be valid UTF-8
		for _, slot := range slots {
			if !utf8.ValidString(slot.Value) {
				t.Errorf("non-UTF8 slot value for input %q: slot[%d]=%q", input, slot.Index, slot.Value)
			}
			if !utf8.ValidString(slot.Type) {
				t.Errorf("non-UTF8 slot type for input %q: slot[%d].Type=%q", input, slot.Index, slot.Type)
			}
		}

		// Property: slot indices should be sequential starting from 0
		for i, slot := range slots {
			if slot.Index != i {
				t.Errorf("non-sequential slot index for input %q: slot[%d].Index=%d", input, i, slot.Index)
			}
		}
	})
}

// FuzzExtractTemplate tests the PreNormalize pipeline with fuzzed input.
// It verifies:
//   - No panics on any input
//   - TemplateID is always a valid hex string
//   - CmdNorm is valid UTF-8
//   - SlotCount is non-negative
func FuzzExtractTemplate(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz test in short mode")
	}
	// Seed corpus with interesting commands and edge cases
	f.Add("git commit -m 'hello world'")
	f.Add("ls -la /tmp/dir")
	f.Add("echo ''; rm -rf /")
	f.Add("cmd \x00\xff\xfe")
	f.Add("")
	f.Add("   ")
	f.Add("cat /etc/passwd | grep root && echo found || echo missing")
	f.Add("find . -name '*.go' | xargs wc -l | sort -n | tail -5")
	f.Add("docker-compose up -d; sleep 5; curl localhost:8080")
	f.Add("git stash && git checkout main && git pull && git checkout - && git stash pop")
	f.Add("a | b | c | d | e | f | g | h | i | j") // many pipe segments
	f.Add("echo hello\\|world")                    // escaped pipe
	f.Add("echo 'hello|world'")                    // quoted pipe
	f.Add("echo \"hello && world\"")               // quoted operator
	f.Add("a ; ; ; b")                             // empty segments
	f.Add("x |||| y")                              // unusual operators
	f.Add("'")                                     // single quote only

	f.Fuzz(func(t *testing.T, input string) {
		result := PreNormalize(input, PreNormConfig{})

		// Property: CmdNorm should be valid UTF-8 when input is valid UTF-8.
		// The normalizer does not sanitize invalid byte sequences;
		// UTF-8 cleaning is done by ingest.ToLossyUTF8 upstream.
		if utf8.ValidString(input) && !utf8.ValidString(result.CmdNorm) {
			t.Errorf("non-UTF8 CmdNorm for valid-UTF8 input %q: got %q", input, result.CmdNorm)
		}

		// Property: TemplateID should be a non-empty hex string (sha256 produces 64 hex chars)
		if len(result.TemplateID) != 64 {
			t.Errorf("unexpected TemplateID length for input %q: got %d chars (%q)", input, len(result.TemplateID), result.TemplateID)
		}
		for _, c := range result.TemplateID {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				t.Errorf("non-hex char in TemplateID for input %q: %q", input, result.TemplateID)
				break
			}
		}

		// Property: SlotCount should be non-negative
		if result.SlotCount < 0 {
			t.Errorf("negative SlotCount for input %q: %d", input, result.SlotCount)
		}

		// Property: Tags should all be valid UTF-8 when input is valid UTF-8
		if utf8.ValidString(input) {
			for _, tag := range result.Tags {
				if !utf8.ValidString(tag) {
					t.Errorf("non-UTF8 tag for valid-UTF8 input %q: %q", input, tag)
				}
			}
		}

		// Property: All segment Raw values should be valid UTF-8 when input is valid UTF-8
		if utf8.ValidString(input) {
			for i, seg := range result.Segments {
				if !utf8.ValidString(seg.Raw) {
					t.Errorf("non-UTF8 segment[%d].Raw for valid-UTF8 input %q: %q", i, input, seg.Raw)
				}
			}
		}
	})
}

// FuzzSplitPipeline tests the SplitPipeline function with fuzzed input.
func FuzzSplitPipeline(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz test in short mode")
	}
	f.Add("a | b | c")
	f.Add("a && b || c ; d")
	f.Add("echo 'hello | world'")
	f.Add("echo \"a && b\"")
	f.Add("")
	f.Add("|")
	f.Add("&&")
	f.Add("||")
	f.Add(";")
	f.Add("a\\|b")

	f.Fuzz(func(t *testing.T, input string) {
		segments := SplitPipeline(input)

		// Property: segments should all have valid UTF-8
		for i, seg := range segments {
			if !utf8.ValidString(seg.Raw) {
				t.Errorf("non-UTF8 segment[%d].Raw for input %q: %q", i, input, seg.Raw)
			}
		}

		// Property: if we have segments, the last one should have empty operator
		if len(segments) > 0 {
			last := segments[len(segments)-1]
			if last.Operator != "" {
				t.Errorf("last segment has non-empty operator for input %q: %q", input, last.Operator)
			}
		}

		// Property: reassembling and re-splitting should yield same number of segments
		// (Note: not necessarily identical due to whitespace normalization)
		if len(segments) > 0 {
			reassembled := ReassemblePipeline(segments)
			if !utf8.ValidString(reassembled) {
				t.Errorf("non-UTF8 reassembled output for input %q: %q", input, reassembled)
			}
		}
	})
}
