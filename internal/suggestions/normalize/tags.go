package normalize

import (
	"sort"
	"strings"
)

// commandTags maps command names to semantic tags.
// This covers 100+ common commands organized by category.
var commandTags = map[string][]string{
	// Version control
	"git":  {"vcs", "git"},
	"svn":  {"vcs"},
	"hg":   {"vcs"},
	"gh":   {"vcs", "git", "github"},
	"glab": {"vcs", "git", "gitlab"},

	// Container & orchestration
	"docker":         {"container", "docker"},
	"docker-compose": {"container", "docker"},
	"podman":         {"container"},
	"kubectl":        {"k8s", "container"},
	"helm":           {"k8s", "container"},
	"minikube":       {"k8s", "container"},
	"kind":           {"k8s", "container"},
	"skaffold":       {"k8s", "container"},

	// Go
	"go":        {"go"},
	"gofmt":     {"go", "format"},
	"golint":    {"go", "lint"},
	"goimports": {"go", "format"},

	// JavaScript / Node
	"npm":    {"js", "package"},
	"npx":    {"js"},
	"yarn":   {"js", "package"},
	"pnpm":   {"js", "package"},
	"node":   {"js"},
	"deno":   {"js"},
	"bun":    {"js", "package"},
	"tsc":    {"js", "typescript"},
	"eslint": {"js", "lint"},

	// Python
	"python":  {"python"},
	"python3": {"python"},
	"pip":     {"python", "package"},
	"pip3":    {"python", "package"},
	"pipenv":  {"python", "package"},
	"poetry":  {"python", "package"},
	"conda":   {"python", "package"},
	"pytest":  {"python", "test"},
	"mypy":    {"python", "lint"},
	"ruff":    {"python", "lint"},
	"black":   {"python", "format"},
	"isort":   {"python", "format"},
	"flake8":  {"python", "lint"},
	"pylint":  {"python", "lint"},

	// Rust
	"cargo":   {"rust", "package"},
	"rustc":   {"rust"},
	"rustup":  {"rust"},
	"rustfmt": {"rust", "format"},
	"clippy":  {"rust", "lint"},

	// Java / JVM
	"java":    {"java"},
	"javac":   {"java"},
	"mvn":     {"java", "package"},
	"gradle":  {"java", "package"},
	"gradlew": {"java", "package"},

	// Ruby
	"ruby":   {"ruby"},
	"gem":    {"ruby", "package"},
	"bundle": {"ruby", "package"},
	"rails":  {"ruby"},
	"rake":   {"ruby", "build"},

	// Build tools
	"make":  {"build"},
	"cmake": {"build"},
	"ninja": {"build"},
	"bazel": {"build"},
	"meson": {"build"},

	// Package managers (system)
	"apt":     {"package", "system"},
	"apt-get": {"package", "system"},
	"brew":    {"package", "system"},
	"dnf":     {"package", "system"},
	"yum":     {"package", "system"},
	"pacman":  {"package", "system"},
	"snap":    {"package", "system"},
	"flatpak": {"package", "system"},
	"nix":     {"package", "system"},

	// Network
	"curl":     {"network", "http"},
	"wget":     {"network", "http"},
	"ssh":      {"network", "remote"},
	"scp":      {"network", "remote"},
	"rsync":    {"network", "remote"},
	"ping":     {"network"},
	"dig":      {"network", "dns"},
	"nslookup": {"network", "dns"},
	"nc":       {"network"},
	"netcat":   {"network"},
	"telnet":   {"network"},
	"nmap":     {"network"},

	// Cloud / IaC
	"aws":       {"cloud", "aws"},
	"gcloud":    {"cloud", "gcp"},
	"az":        {"cloud", "azure"},
	"terraform": {"iac", "cloud"},
	"tf":        {"iac", "cloud"},
	"pulumi":    {"iac", "cloud"},
	"ansible":   {"iac"},
	"vagrant":   {"iac"},

	// Editors
	"vim":   {"editor"},
	"nvim":  {"editor"},
	"nano":  {"editor"},
	"emacs": {"editor"},
	"code":  {"editor", "vscode"},
	"subl":  {"editor"},

	// Shell / file ops
	"cd":     {"shell", "nav"},
	"ls":     {"shell", "file"},
	"cat":    {"shell", "file"},
	"less":   {"shell", "file"},
	"head":   {"shell", "file"},
	"tail":   {"shell", "file"},
	"find":   {"shell", "file"},
	"grep":   {"shell", "search"},
	"rg":     {"shell", "search"},
	"ag":     {"shell", "search"},
	"fd":     {"shell", "search"},
	"fzf":    {"shell", "search"},
	"sed":    {"shell", "text"},
	"awk":    {"shell", "text"},
	"sort":   {"shell", "text"},
	"uniq":   {"shell", "text"},
	"wc":     {"shell", "text"},
	"xargs":  {"shell"},
	"rm":     {"shell", "file"},
	"mv":     {"shell", "file"},
	"cp":     {"shell", "file"},
	"mkdir":  {"shell", "file"},
	"touch":  {"shell", "file"},
	"chmod":  {"shell", "file"},
	"chown":  {"shell", "file"},
	"ln":     {"shell", "file"},
	"tar":    {"shell", "archive"},
	"zip":    {"shell", "archive"},
	"unzip":  {"shell", "archive"},
	"gzip":   {"shell", "archive"},
	"gunzip": {"shell", "archive"},

	// System / process
	"ps":         {"system", "process"},
	"top":        {"system", "process"},
	"htop":       {"system", "process"},
	"kill":       {"system", "process"},
	"killall":    {"system", "process"},
	"systemctl":  {"system", "service"},
	"journalctl": {"system", "log"},
	"df":         {"system", "disk"},
	"du":         {"system", "disk"},
	"free":       {"system", "memory"},
	"uname":      {"system"},
	"env":        {"system", "env"},
	"export":     {"system", "env"},
	"source":     {"shell"},

	// Database
	"psql":      {"database", "postgres"},
	"mysql":     {"database", "mysql"},
	"sqlite3":   {"database", "sqlite"},
	"redis-cli": {"database", "redis"},
	"mongosh":   {"database", "mongo"},
}

// goSubcommandTags maps go subcommands to additional tags.
var goSubcommandTags = map[string][]string{
	"test":     {"test"},
	"build":    {"build"},
	"run":      {"build"},
	"vet":      {"lint"},
	"generate": {"codegen"},
	"install":  {"package"},
	"get":      {"package"},
	"mod":      {"package"},
	"fmt":      {"format"},
}

// ExtractTags extracts semantic tags from pipeline segments.
// Tags are deduplicated and sorted for determinism.
func ExtractTags(segments []Segment) []string {
	tagSet := make(map[string]bool)

	for _, seg := range segments {
		cmd, sub := extractCommandAndSub(seg.Raw)
		if cmd == "" {
			continue
		}

		// Look up base command tags
		if tags, ok := commandTags[cmd]; ok {
			for _, t := range tags {
				tagSet[t] = true
			}
		}

		// Special handling for go subcommands
		if cmd == "go" && sub != "" {
			if tags, ok := goSubcommandTags[sub]; ok {
				for _, t := range tags {
					tagSet[t] = true
				}
			}
		}
	}

	if len(tagSet) == 0 {
		return nil
	}

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// extractCommandAndSub extracts the command name and optional subcommand
// from a normalized segment string.
func extractCommandAndSub(seg string) (string, string) {
	seg = multiSpacePattern.ReplaceAllString(strings.TrimSpace(seg), " ")
	tokens := strings.Fields(seg)
	if len(tokens) == 0 {
		return "", ""
	}

	cmd := strings.ToLower(tokens[0])

	sub := ""
	if len(tokens) > 1 && !strings.HasPrefix(tokens[1], "-") {
		sub = strings.ToLower(tokens[1])
	}

	return cmd, sub
}
