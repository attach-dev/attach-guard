// Package parser provides command parsing for package manager commands.
package parser

import (
	"path/filepath"
	"strings"

	"github.com/attach-dev/attach-guard/internal/parser/npm"
	"github.com/attach-dev/attach-guard/internal/parser/pnpm"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// shellOperators are tokens that indicate command chaining.
// We only parse the first command segment to avoid treating operators as package names.
var shellOperators = map[string]bool{
	"&&": true,
	"||": true,
	";":  true,
	"|":  true,
}

// Parse attempts to parse a raw command string as a package manager install command.
// Returns nil if the command is not a recognized install command.
// It checks ALL command segments separated by shell operators (&&, ||, ;, |)
// so that chained commands like "ls && npm install evil-pkg" are caught.
func Parse(rawCommand string) *api.ParsedCommand {
	tokens := Tokenize(rawCommand)
	if len(tokens) == 0 {
		return nil
	}

	for _, segment := range commandSegments(tokens) {
		seg := unwrapPrefixes(segment)
		if len(seg) == 0 {
			continue
		}
		if cmd := npm.Parse(seg, rawCommand); cmd != nil {
			return cmd
		}
		if cmd := pnpm.Parse(seg, rawCommand); cmd != nil {
			return cmd
		}
	}

	return nil
}

// IsInstallCommand returns true if the raw command is a guarded install command.
func IsInstallCommand(rawCommand string) bool {
	return Parse(rawCommand) != nil
}

// installVerbs are action tokens that indicate a package install operation.
var installVerbs = map[string]bool{
	"install": true,
	"i":       true,
	"add":     true,
}

// pmBinaries are package manager basenames we guard.
var pmBinaries = map[string]bool{
	"npm":  true,
	"pnpm": true,
}

// LooksLikeInstall returns true if the raw command contains tokens that look
// like a package manager install even though Parse() could not fully classify
// it. This catches novel wrapper/prefix combinations that bypass structured
// parsing. The caller can use this to fail closed on suspicious commands
// instead of silently allowing them.
//
// The heuristic requires that a PM binary token appears before an install verb
// token (i.e., "npm install", not "install npm"), which matches the actual
// command syntax of npm/pnpm. It only inspects top-level tokens — it does NOT
// re-tokenize compound tokens (e.g., quoted strings) to avoid false positives
// on commands like echo "npm install axios". Shell -c forms are already handled
// by unwrapPrefixes in Parse(), so they don't need a heuristic fallback.
func LooksLikeInstall(rawCommand string) bool {
	tokens := Tokenize(rawCommand)
	for _, segment := range commandSegments(tokens) {
		if looksLikeInstallTokens(segment) {
			return true
		}
	}
	return false
}

// nonWrapperBinaries are commands that take arguments as text, not as commands
// to execute. If the heuristic encounters one of these, it stops scanning
// because the following tokens are data, not a real command.
var nonWrapperBinaries = map[string]bool{
	"echo":    true,
	"printf":  true,
	"cat":     true,
	"grep":    true,
	"awk":     true,
	"sed":     true,
	"head":    true,
	"tail":    true,
	"tee":     true,
	"wc":      true,
	"sort":    true,
	"cut":     true,
	"tr":      true,
	"xargs":   true,
	"find":    true,
	"less":    true,
	"more":    true,
	"vi":      true,
	"vim":     true,
	"nano":    true,
	"python":  true,
	"python3": true,
	"ruby":    true,
	"perl":    true,
	"node":    true,
}

// shellBinaries are shells that take script filenames as positional arguments.
// "bash script.sh npm install axios" should NOT be flagged because npm/install
// are script arguments, not a real command.
var shellBinaries = map[string]bool{
	"bash": true,
	"sh":   true,
	"zsh":  true,
}

func isShellFlagToken(tok string) bool {
	return strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "+")
}

func shellFlagTakesValue(flag string) bool {
	switch flag {
	case "-o", "+o", "-O", "+O", "--rcfile", "--init-file":
		return true
	default:
		return false
	}
}

// looksLikeInstallTokens checks if a flat token list contains a PM binary
// followed by an install verb in a plausible command position.
//
// It skips known wrapper prefixes, flags, and env-var assignments. Unknown
// binaries (strace, nohup, etc.) are treated as potential wrappers and skipped.
// However, after a shell binary (bash/sh/zsh) without -c, remaining tokens are
// treated as script arguments and NOT scanned for PM commands.
func looksLikeInstallTokens(tokens []string) bool {
	i := 0
	for i < len(tokens) {
		base := filepath.Base(tokens[i])

		// PM binary found — check for install verb
		if pmBinaries[base] {
			for j := i + 1; j < len(tokens); j++ {
				if installVerbs[tokens[j]] {
					return true
				}
				if !strings.HasPrefix(tokens[j], "-") {
					break
				}
			}
			return false
		}

		// Shell binary without -c: remaining tokens are script args, stop scanning
		if shellBinaries[base] {
			i++
			// Check if -c is present (standalone or combined like -lc).
			// Shell flags that take a value (-o, -O, --rcfile) must skip it.
			hasC := false
			for i < len(tokens) {
				tok := tokens[i]
				if !isShellFlagToken(tok) {
					break // non-flag token — stop scanning flags
				}
				if tok == "-c" {
					hasC = true
				}
				if strings.HasPrefix(tok, "-") && !strings.HasPrefix(tok, "--") &&
					len(tok) > 2 && strings.ContainsRune(tok[1:], 'c') {
					hasC = true
				}
				i++
				// Consume value for flags that take one.
				if shellFlagTakesValue(tok) && i < len(tokens) {
					i++
				}
			}
			if !hasC {
				// bash script.sh ... — stop, these are script arguments
				return false
			}
			// -c was found — re-tokenize the command string and check inside it.
			// Any tokens after the command string are positional args ($0, $1),
			// not commands, so stop scanning regardless of the result.
			if i < len(tokens) {
				inner := Tokenize(tokens[i])
				return looksLikeInstallTokens(inner)
			}
			return false
		}

		// Known wrapper — skip it and its flags
		if transparentWrappers[base] {
			i++
			// "command -v/-V" is introspection, not execution — stop scanning
			if base == "command" && i < len(tokens) && (tokens[i] == "-v" || tokens[i] == "-V") {
				return false
			}
			for i < len(tokens) && strings.HasPrefix(tokens[i], "-") {
				i++
			}
			continue
		}

		// Env-var assignment — skip
		if isEnvVarAssignment(tokens[i]) {
			i++
			continue
		}

		// If it's a known non-wrapper (output/text commands), stop scanning.
		// These commands take arguments but do not exec them.
		if nonWrapperBinaries[base] {
			return false
		}

		// Unknown binary (strace, nohup, etc.) — treat as potential wrapper, skip
		i++
		// Skip its flags
		for i < len(tokens) && strings.HasPrefix(tokens[i], "-") {
			i++
		}
	}
	return false
}

// transparentWrappers are commands that simply exec their arguments.
// We strip these (and their flags) to reach the underlying PM command.
var transparentWrappers = map[string]bool{
	"sudo":    true,
	"env":     true,
	"command": true,
	"time":    true,
	"nice":    true,
	"npx":     true,
}

// unwrapPrefixes strips common command prefixes like sudo, env, command, time,
// nice, npx, and environment variable assignments (KEY=val) so the underlying
// package manager command is visible to the parsers.
func unwrapPrefixes(tokens []string) []string {
	for len(tokens) > 0 {
		tok := tokens[0]
		base := filepath.Base(tok)

		// sudo — skip it (and optional -E, -u user, etc.)
		if base == "sudo" {
			tokens = tokens[1:]
			// Skip sudo flags
			for len(tokens) > 0 && strings.HasPrefix(tokens[0], "-") {
				flag := tokens[0]
				tokens = tokens[1:]
				// -u takes a following argument
				if (flag == "-u" || flag == "--user") && len(tokens) > 0 {
					tokens = tokens[1:]
				}
			}
			continue
		}

		// env — skip it and any env flags / VAR=val assignments
		if base == "env" {
			tokens = tokens[1:]
			// Skip env flags and VAR=val pairs
			for len(tokens) > 0 {
				if strings.HasPrefix(tokens[0], "-") {
					tokens = tokens[1:]
					continue
				}
				if strings.Contains(tokens[0], "=") && !strings.HasPrefix(tokens[0], "-") {
					tokens = tokens[1:]
					continue
				}
				break
			}
			continue
		}

		// Shell wrappers (bash -c '...', sh -c '...', zsh -c '...')
		// Only unwrap when -c is present; other forms like "bash script.sh ..."
		// are not transparent wrappers and must not expose trailing arguments.
		if base == "bash" || base == "sh" || base == "zsh" {
			saved := tokens // preserve in case we can't find -c
			tokens = tokens[1:]
			// Look for -c flag. It can be standalone (-c) or combined (-lc, -xc).
			// Shell flags that take a separate value (-o, -O, --rcfile, etc.) must
			// consume that value to avoid mistaking it for a script filename.
			foundC := false
			for len(tokens) > 0 {
				flag := tokens[0]
				tokens = tokens[1:]
				if flag == "-c" {
					foundC = true
					break
				}
				// Combined short flags like -lc, -cl, -xce — check if 'c' appears anywhere
				if strings.HasPrefix(flag, "-") && !strings.HasPrefix(flag, "--") &&
					len(flag) > 2 && strings.ContainsRune(flag[1:], 'c') {
					foundC = true
					break
				}
				// Flags that take a value must consume the next token.
				if shellFlagTakesValue(flag) {
					if len(tokens) > 0 {
						tokens = tokens[1:]
					}
					continue
				}
				// Other flags like -l, -i, -x, +m — skip
				if isShellFlagToken(flag) {
					continue
				}
				// Non-flag, non -c token (e.g., "bash script.sh npm install axios").
				// This is NOT a transparent wrapper — restore original tokens and
				// let the outer loop break so the parsers see "bash" as tokens[0]
				// (which won't match npm/pnpm).
				return saved
			}
			if foundC && len(tokens) > 0 {
				// The next token is the command string; re-tokenize it
				// and truncate at shell operators so chained commands inside
				// the -c string (e.g., "npm install axios && rm -rf /")
				// don't leak into the parser.
				// Any tokens after the command string are positional args
				// ($0, $1, ...) for the shell, NOT commands — discard them.
				inner := Tokenize(tokens[0])
				inner = firstCommandSegment(inner)
				tokens = inner
				continue
			}
			// No -c found or ran out of tokens — not a wrapper we handle
			return saved
		}

		// command, time, nice, npx — skip the wrapper and any leading flags
		if base == "command" || base == "time" || base == "nice" || base == "npx" {
			tokens = tokens[1:]
			// "command -v" is introspection (like `which`), not execution.
			// Return empty to prevent the remaining tokens from being parsed.
			if base == "command" && len(tokens) > 0 && (tokens[0] == "-v" || tokens[0] == "-V") {
				return nil
			}
			// Skip flags (e.g., "nice -n 10", "npx --yes")
			for len(tokens) > 0 && strings.HasPrefix(tokens[0], "-") {
				flag := tokens[0]
				tokens = tokens[1:]
				// nice -n takes a value; npx --package takes a value
				if (flag == "-n" || flag == "--package" || flag == "-p") && len(tokens) > 0 {
					tokens = tokens[1:]
				}
			}
			continue
		}

		// Inline env-var assignment (e.g., NODE_ENV=production npm install axios)
		if strings.Contains(tok, "=") && !strings.HasPrefix(tok, "-") && isEnvVarAssignment(tok) {
			tokens = tokens[1:]
			continue
		}

		// Nothing more to strip
		break
	}
	return tokens
}

// isEnvVarAssignment returns true if tok looks like KEY=value where KEY is a
// valid environment variable name (letters, digits, underscores, starting with
// a letter or underscore).
func isEnvVarAssignment(tok string) bool {
	eqIdx := strings.Index(tok, "=")
	if eqIdx <= 0 {
		return false
	}
	key := tok[:eqIdx]
	for i, c := range key {
		if c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// commandSegments splits tokens at shell operators into separate command segments.
// For example, ["ls", "&&", "npm", "install", "axios"] returns [["ls"], ["npm", "install", "axios"]].
// Also handles trailing semicolons attached to tokens (e.g., "axios;" becomes "axios" ending a segment).
func commandSegments(tokens []string) [][]string {
	var segments [][]string
	start := 0
	for i, tok := range tokens {
		if shellOperators[tok] {
			if i > start {
				segments = append(segments, tokens[start:i])
			}
			start = i + 1
			continue
		}
		if strings.HasSuffix(tok, ";") {
			seg := make([]string, i-start+1)
			copy(seg, tokens[start:i])
			seg[i-start] = strings.TrimSuffix(tok, ";")
			segments = append(segments, seg)
			start = i + 1
		}
	}
	if start < len(tokens) {
		segments = append(segments, tokens[start:])
	}
	return segments
}

// firstCommandSegment returns tokens up to the first shell operator.
// Also handles cases where a semicolon is attached to a token (e.g., "axios;").
func firstCommandSegment(tokens []string) []string {
	for i, tok := range tokens {
		if shellOperators[tok] {
			return tokens[:i]
		}
		// Handle trailing semicolons (e.g., "axios;" from "npm install axios; ...")
		if strings.HasSuffix(tok, ";") {
			trimmed := strings.TrimSuffix(tok, ";")
			result := make([]string, i+1)
			copy(result, tokens[:i])
			result[i] = trimmed
			return result
		}
	}
	return tokens
}
