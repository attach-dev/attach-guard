// Package parser provides command parsing for package manager commands.
package parser

import (
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
