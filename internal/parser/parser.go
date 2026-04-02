// Package parser provides command parsing for package manager commands.
package parser

import (
	"path/filepath"
	"strings"

	"github.com/attach-dev/attach-guard/internal/parser/cargo"
	"github.com/attach-dev/attach-guard/internal/parser/gomod"
	"github.com/attach-dev/attach-guard/internal/parser/npm"
	"github.com/attach-dev/attach-guard/internal/parser/pip"
	"github.com/attach-dev/attach-guard/internal/parser/pnpm"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// shellOperators are tokens that indicate command chaining.
// We only parse the first command segment to avoid treating operators as package names.
var shellOperators = map[string]bool{
	"&&": true,
	"||": true,
	";":  true,
	"&":  true,
	"|":  true,
}

// Parse attempts to parse a raw command string as a package manager install command.
// Returns the first recognized install-like command in evaluation order, or nil
// if the command is not recognized as a guarded package-manager install form.
// Some recognized commands may intentionally contain zero evaluable packages
// when all positional args were skipped as unsupported inputs.
func Parse(rawCommand string) *api.ParsedCommand {
	for _, cmd := range ParseAll(rawCommand) {
		return cmd
	}
	return nil
}

type sourceOverrideContext struct {
	pipNonLocal bool
	goNonLocal  bool
}

func (c sourceOverrideContext) merge(other sourceOverrideContext) sourceOverrideContext {
	return sourceOverrideContext{
		pipNonLocal: c.pipNonLocal || other.pipNonLocal,
		goNonLocal:  c.goNonLocal || other.goNonLocal,
	}
}

type unwrapResult struct {
	tokens []string
	source sourceOverrideContext
}

// parseSegmentAll parses every install command reachable from a single command
// segment. After unwrapping prefixes (which may expand shell -c strings
// containing operators), it re-splits on shell operators and checks each
// resulting sub-segment.
func parseSegmentAll(tokens []string, rawCommand string) []*api.ParsedCommand {
	return parseSegmentAllWithContext(tokens, rawCommand, sourceOverrideContext{})
}

func parseSegmentAllWithContext(tokens []string, rawCommand string, inherited sourceOverrideContext) []*api.ParsedCommand {
	unwrapped := unwrapPrefixes(tokens)
	if len(unwrapped.tokens) == 0 {
		return nil
	}
	return parseUnwrappedTokensAll(unwrapped.tokens, rawCommand, inherited.merge(unwrapped.source))
}

func parseUnwrappedTokensAll(tokens []string, rawCommand string, inherited sourceOverrideContext) []*api.ParsedCommand {
	segments := commandSegments(tokens)
	if len(segments) > 1 {
		var results []*api.ParsedCommand
		for _, seg := range segments {
			results = append(results, parseSegmentAllWithContext(seg, rawCommand, inherited)...)
		}
		return results
	}

	var results []*api.ParsedCommand
	if cmd := npm.Parse(tokens, rawCommand); cmd != nil {
		results = append(results, cmd)
		return results
	}
	if cmd := pnpm.Parse(tokens, rawCommand); cmd != nil {
		results = append(results, cmd)
		return results
	}
	if cmd := pip.Parse(tokens, rawCommand); cmd != nil {
		applySourceOverrideContext(cmd, inherited)
		results = append(results, cmd)
		return results
	}
	if cmd := gomod.Parse(tokens, rawCommand); cmd != nil {
		applySourceOverrideContext(cmd, inherited)
		results = append(results, cmd)
		return results
	}
	if cmd := cargo.Parse(tokens, rawCommand); cmd != nil {
		results = append(results, cmd)
	}
	return results
}

// ParseAll returns all install commands found across all command segments.
// Unlike Parse (which returns only the first), this catches every install in
// chained commands, including commands revealed by wrapper unwrapping such as
// "bash -c 'npm install lodash && npm install evil-pkg'".
func ParseAll(rawCommand string) []*api.ParsedCommand {
	tokens := Tokenize(rawCommand)
	if len(tokens) == 0 {
		return nil
	}

	var results []*api.ParsedCommand
	for _, segment := range commandSegments(tokens) {
		results = append(results, parseSegmentAllWithContext(segment, rawCommand, sourceOverrideContext{})...)
	}
	return results
}

// IsInstallCommand returns true if the raw command is a recognized install-like
// package-manager command. Recognized commands may still contain zero
// evaluable packages when all positional inputs were skipped as unsupported.
func IsInstallCommand(rawCommand string) bool {
	return Parse(rawCommand) != nil
}

// installVerbs are action tokens that indicate a package install operation.
var installVerbs = map[string]bool{
	"install": true,
	"i":       true,
	"add":     true,
	"get":     true,
}

// pmBinaries are package manager basenames we guard.
var pmBinaries = map[string]bool{
	"npm":   true,
	"pnpm":  true,
	"pip":   true,
	"pip3":  true,
	"go":    true,
	"cargo": true,
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

func envFlagTakesValue(flag string) bool {
	switch flag {
	case "-C", "--chdir", "-P", "--path", "-S", "--split-string", "-u", "--unset":
		return true
	default:
		return false
	}
}

func isEnvSplitStringFlag(flag string) bool {
	return flag == "-S" || flag == "--split-string"
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
			// -c was found — re-tokenize the command string and check each
			// segment (the inner string may contain && or ; operators).
			// Any tokens after the command string are positional args ($0, $1),
			// not commands, so stop scanning regardless of the result.
			if i < len(tokens) {
				inner := Tokenize(tokens[i])
				for _, seg := range commandSegments(inner) {
					if looksLikeInstallTokens(seg) {
						return true
					}
				}
			}
			return false
		}

		// env may rewrite its argv before execing the underlying command.
		// Handle env flags, assignments, and -S/--split-string explicitly so
		// split-string forms do not bypass the heuristic.
		if base == "env" {
			i++
			for i < len(tokens) {
				tok := tokens[i]
				if strings.HasPrefix(tok, "-") {
					i++
					if isEnvSplitStringFlag(tok) {
						if i >= len(tokens) {
							return false
						}
						inner := Tokenize(tokens[i])
						combined := append(inner, tokens[i+1:]...)
						return looksLikeInstallTokens(combined)
					}
					if envFlagTakesValue(tok) && i < len(tokens) {
						i++
					}
					continue
				}
				if isEnvVarAssignment(tok) {
					i++
					continue
				}
				break
			}
			continue
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
func unwrapPrefixes(tokens []string) unwrapResult {
	result := unwrapResult{tokens: tokens}
	for len(result.tokens) > 0 {
		tok := result.tokens[0]
		base := filepath.Base(tok)

		// sudo — skip it (and optional -E, -u user, etc.)
		if base == "sudo" {
			result.tokens = result.tokens[1:]
			// Skip sudo flags
			for len(result.tokens) > 0 && strings.HasPrefix(result.tokens[0], "-") {
				flag := result.tokens[0]
				result.tokens = result.tokens[1:]
				// -u takes a following argument
				if (flag == "-u" || flag == "--user") && len(result.tokens) > 0 {
					result.tokens = result.tokens[1:]
				}
			}
			continue
		}

		// env — skip it and any env flags / VAR=val assignments
		if base == "env" {
			result.tokens = result.tokens[1:]
			// Skip env flags and VAR=val pairs. -S/--split-string retokenizes its
			// argument because env will split that string into argv before exec.
			for len(result.tokens) > 0 {
				if strings.HasPrefix(result.tokens[0], "-") {
					flag := result.tokens[0]
					result.tokens = result.tokens[1:]
					if isEnvSplitStringFlag(flag) {
						if len(result.tokens) == 0 {
							return unwrapResult{}
						}
						inner := Tokenize(result.tokens[0])
						result.tokens = append(inner, result.tokens[1:]...)
						continue
					}
					if envFlagTakesValue(flag) && len(result.tokens) > 0 {
						result.tokens = result.tokens[1:]
					}
					continue
				}
				if isEnvVarAssignment(result.tokens[0]) {
					recordSourceEnvAssignment(result.tokens[0], &result.source)
					result.tokens = result.tokens[1:]
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
			saved := result.tokens // preserve in case we can't find -c
			result.tokens = result.tokens[1:]
			// Look for -c flag. It can be standalone (-c) or combined (-lc, -xc).
			// Shell flags that take a separate value (-o, -O, --rcfile, etc.) must
			// consume that value to avoid mistaking it for a script filename.
			foundC := false
			for len(result.tokens) > 0 {
				flag := result.tokens[0]
				result.tokens = result.tokens[1:]
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
					if len(result.tokens) > 0 {
						result.tokens = result.tokens[1:]
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
				result.tokens = saved
				return result
			}
			if foundC && len(result.tokens) > 0 {
				// The next token is the command string; re-tokenize it.
				// Any tokens after the command string are positional args
				// ($0, $1, ...) for the shell, NOT commands — discard them.
				// The caller (parseSegment) handles splitting at shell
				// operators inside the expanded -c string.
				inner := Tokenize(result.tokens[0])
				result.tokens = inner
				continue
			}
			// No -c found or ran out of tokens — not a wrapper we handle
			result.tokens = saved
			return result
		}

		// command, time, nice, npx — skip the wrapper and any leading flags
		if base == "command" || base == "time" || base == "nice" || base == "npx" {
			result.tokens = result.tokens[1:]
			// "command -v" is introspection (like `which`), not execution.
			// Return empty to prevent the remaining tokens from being parsed.
			if base == "command" && len(result.tokens) > 0 && (result.tokens[0] == "-v" || result.tokens[0] == "-V") {
				return unwrapResult{}
			}
			// Skip flags (e.g., "nice -n 10", "npx --yes")
			for len(result.tokens) > 0 && strings.HasPrefix(result.tokens[0], "-") {
				flag := result.tokens[0]
				result.tokens = result.tokens[1:]
				// nice -n takes a value; npx --package takes a value
				if (flag == "-n" || flag == "--package" || flag == "-p") && len(result.tokens) > 0 {
					result.tokens = result.tokens[1:]
				}
			}
			continue
		}

		// Inline env-var assignment (e.g., NODE_ENV=production npm install axios)
		if strings.Contains(tok, "=") && !strings.HasPrefix(tok, "-") && isEnvVarAssignment(tok) {
			recordSourceEnvAssignment(tok, &result.source)
			result.tokens = result.tokens[1:]
			continue
		}

		// Nothing more to strip
		break
	}
	return result
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

func applySourceOverrideContext(cmd *api.ParsedCommand, ctx sourceOverrideContext) {
	switch cmd.PackageManager {
	case "pip", "pip3":
		if ctx.pipNonLocal {
			cmd.Packages = nil
			cmd.HasUnparsedArgs = true
			cmd.HasNonLocalUnparsedArgs = true
		}
	case "go":
		if ctx.goNonLocal {
			cmd.Packages = nil
			cmd.HasUnparsedArgs = true
			cmd.HasNonLocalUnparsedArgs = true
		}
	}
}

func recordSourceEnvAssignment(tok string, ctx *sourceOverrideContext) {
	key, value, ok := strings.Cut(tok, "=")
	if !ok {
		return
	}

	switch key {
	case "PIP_INDEX_URL", "PIP_EXTRA_INDEX_URL", "PIP_FIND_LINKS", "PIP_REQUIREMENT", "PIP_CONSTRAINT", "PIP_NO_INDEX":
		if strings.TrimSpace(value) != "" {
			ctx.pipNonLocal = true
		}
	case "GOPRIVATE", "GONOPROXY":
		if strings.TrimSpace(value) != "" {
			ctx.goNonLocal = true
		}
	case "GOPROXY":
		if !goProxyUsesPublicRegistryValue(value) {
			ctx.goNonLocal = true
		}
	}
}

func goProxyUsesPublicRegistryValue(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}

	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '|'
	}) {
		switch entry {
		case "", "direct", "off":
			return false
		case "https://proxy.golang.org", "https://proxy.golang.org/", "http://proxy.golang.org", "http://proxy.golang.org/":
			return true
		default:
			return false
		}
	}

	return false
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
