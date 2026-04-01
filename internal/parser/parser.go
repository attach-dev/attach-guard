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
func Parse(rawCommand string) *api.ParsedCommand {
	tokens := Tokenize(rawCommand)
	if len(tokens) == 0 {
		return nil
	}

	// Truncate at the first shell operator to only parse the first command segment
	tokens = firstCommandSegment(tokens)

	// Strip common command prefixes (sudo, env, env-var assignments)
	tokens = unwrapPrefixes(tokens)
	if len(tokens) == 0 {
		return nil
	}

	// Try npm first
	if cmd := npm.Parse(tokens, rawCommand); cmd != nil {
		return cmd
	}

	// Try pnpm
	if cmd := pnpm.Parse(tokens, rawCommand); cmd != nil {
		return cmd
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
// command syntax of npm/pnpm.
func LooksLikeInstall(rawCommand string) bool {
	tokens := Tokenize(rawCommand)
	tokens = firstCommandSegment(tokens)

	pmSeen := false
	for _, tok := range tokens {
		base := filepath.Base(tok)
		if pmBinaries[base] {
			pmSeen = true
		} else if pmSeen && installVerbs[tok] {
			return true
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

		// command, time, nice, npx — skip the wrapper and any leading flags
		if base == "command" || base == "time" || base == "nice" || base == "npx" {
			tokens = tokens[1:]
			// Skip flags (e.g., "nice -n 10", "command -v", "npx --yes")
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
