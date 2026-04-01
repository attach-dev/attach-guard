// Package pnpm parses pnpm add/install commands.
package pnpm

import (
	"path/filepath"
	"strings"

	"github.com/attach-dev/attach-guard/internal/parser/spec"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// pnpm uses "add" for installing new packages
var installAliases = map[string]bool{
	"add":     true,
	"install": true,
	"i":       true,
}

var flagsWithValue = map[string]bool{
	"--filter":   true,
	"--registry": true,
	"--tag":      true,
}

// globalFlagsWithValue are pnpm flags that appear before the action verb and take a value.
var globalFlagsWithValue = map[string]bool{
	"--filter": true,
	"--dir":    true,
	"-C":       true,
}

// Parse attempts to parse tokens as a pnpm install command.
func Parse(tokens []string, rawCommand string) *api.ParsedCommand {
	if len(tokens) < 2 {
		return nil
	}

	base := filepath.Base(tokens[0])
	if base != "pnpm" {
		return nil
	}

	// Skip pre-action flags to find the action verb
	var preActionFlags []string
	actionIdx := -1
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "-") {
			preActionFlags = append(preActionFlags, tok)
			if globalFlagsWithValue[tok] && i+1 < len(tokens) {
				i++
				preActionFlags = append(preActionFlags, tokens[i])
			}
			continue
		}
		// First non-flag token is the action verb
		actionIdx = i
		break
	}

	if actionIdx == -1 {
		return nil
	}

	action := tokens[actionIdx]
	if !installAliases[action] {
		return nil
	}

	cmd := &api.ParsedCommand{
		PackageManager: "pnpm",
		Action:         action,
		PreActionFlags: preActionFlags,
		IsInstall:      true,
		RawCommand:     rawCommand,
	}

	i := actionIdx + 1
	for i < len(tokens) {
		tok := tokens[i]

		if strings.HasPrefix(tok, "-") {
			cmd.Flags = append(cmd.Flags, tok)
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				cmd.Flags = append(cmd.Flags, tokens[i])
			}
			i++
			continue
		}

		pkg := spec.ParsePackageSpec(tok)
		pkg.Ecosystem = api.EcosystemPNPM
		cmd.Packages = append(cmd.Packages, pkg)
		i++
	}

	// pnpm install with no packages (installs from lockfile) — not guarded
	// pnpm add requires packages
	if len(cmd.Packages) == 0 {
		return nil
	}

	return cmd
}
