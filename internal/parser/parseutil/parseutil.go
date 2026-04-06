package parseutil

import "strings"

var localArchiveSuffixes = []string{
	".whl",
	".zip",
	".tar.gz",
	".tgz",
	".tar.bz2",
	".tar.xz",
}

// ShouldConsumeUnknownLongFlagValue returns true when an unknown long flag is
// followed by a likely value token instead of another flag or one of the stop
// tokens (typically action verbs like "install" or "add").
func ShouldConsumeUnknownLongFlagValue(flag string, tokens []string, idx int, stopAts ...string) bool {
	if !strings.HasPrefix(flag, "--") || strings.Contains(flag, "=") || idx+1 >= len(tokens) {
		return false
	}
	next := tokens[idx+1]
	for _, stopAt := range stopAts {
		if next == stopAt {
			return false
		}
	}
	return !strings.HasPrefix(next, "-")
}

// SplitLongFlagAssignment splits --flag=value into its flag name and value.
func SplitLongFlagAssignment(flag string) (name, value string, ok bool) {
	if !strings.HasPrefix(flag, "--") {
		return "", "", false
	}
	name, value, ok = strings.Cut(flag, "=")
	if !ok || name == "" {
		return "", "", false
	}
	return name, value, true
}

// ClassifyPipLocation classifies pip location-like arguments as local or
// non-local so parsers can preserve the allow-vs-ask split for skipped inputs.
func ClassifyPipLocation(raw string) (local bool, nonLocal bool) {
	lower := strings.ToLower(raw)

	if strings.HasPrefix(lower, "file://") || strings.Contains(lower, "+file://") {
		return true, false
	}
	if strings.Contains(lower, "://") {
		return false, true
	}
	if strings.HasPrefix(lower, "git+") || strings.HasPrefix(lower, "hg+") ||
		strings.HasPrefix(lower, "svn+") || strings.HasPrefix(lower, "bzr+") {
		return false, true
	}
	if strings.HasPrefix(raw, ".") || strings.HasPrefix(raw, "/") {
		return true, false
	}
	if strings.Contains(raw, "/") {
		return true, false
	}
	for _, suffix := range localArchiveSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true, false
		}
	}
	return false, false
}
