// Package parser provides command parsing for package manager commands.
package parser

import (
	"github.com/hammadtq/attach-dev/attach-guard/internal/parser/npm"
	"github.com/hammadtq/attach-dev/attach-guard/internal/parser/pnpm"
	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
)

// Parse attempts to parse a raw command string as a package manager install command.
// Returns nil if the command is not a recognized install command.
func Parse(rawCommand string) *api.ParsedCommand {
	tokens := Tokenize(rawCommand)
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
