package workflow

import "strings"

// dangerousEnvKeys is a blocklist of environment variable names (or prefixes)
// that must never be exported through workflow step outputs. Exporting these
// could overwrite security-sensitive process state in downstream steps.
var dangerousEnvKeys = []string{
	"PATH",
	"LD_PRELOAD",
	"LD_LIBRARY_PATH",
	"DYLD_INSERT_LIBRARIES",
	"DYLD_LIBRARY_PATH",
	"DYLD_FRAMEWORK_PATH",
	"HOME",
	"SHELL",
	"USER",
	"LOGNAME",
	"TMPDIR",
	"SSH_AUTH_SOCK",
	"GPG_AGENT_INFO",
}

// IsDangerousEnvKey reports whether key matches the blocklist of
// security-sensitive environment variable names. The comparison is
// case-insensitive.
func IsDangerousEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, dk := range dangerousEnvKeys {
		if upper == dk {
			return true
		}
	}
	return false
}
