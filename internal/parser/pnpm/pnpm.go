// Package pnpm parses pnpm add/install commands.
package pnpm

import (
	"path/filepath"
	"strings"

	"github.com/hammadtq/attach-dev/attach-guard/internal/parser/spec"
	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
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

// Parse attempts to parse tokens as a pnpm install command.
func Parse(tokens []string, rawCommand string) *api.ParsedCommand {
	if len(tokens) < 2 {
		return nil
	}

	base := filepath.Base(tokens[0])
	if base != "pnpm" {
		return nil
	}

	action := tokens[1]
	if !installAliases[action] {
		return nil
	}

	cmd := &api.ParsedCommand{
		PackageManager: "pnpm",
		Action:         action,
		IsInstall:      true,
		RawCommand:     rawCommand,
	}

	i := 2
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

