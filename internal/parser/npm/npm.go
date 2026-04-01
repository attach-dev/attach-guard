// Package npm parses npm install commands.
package npm

import (
	"path/filepath"
	"strings"

	"github.com/hammadtq/attach-dev/attach-guard/internal/parser/spec"
	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
)

var installAliases = map[string]bool{
	"install": true,
	"i":       true,
	"add":     true,
}

// npmFlags that take a following argument value
var flagsWithValue = map[string]bool{
	"--save-prefix": true,
	"--tag":         true,
	"--registry":    true,
}

// Parse attempts to parse tokens as an npm install command.
// Returns nil if not an npm install command.
func Parse(tokens []string, rawCommand string) *api.ParsedCommand {
	if len(tokens) < 2 {
		return nil
	}

	// Check if first token is npm (could be a path like /usr/local/bin/npm)
	base := filepath.Base(tokens[0])
	if base != "npm" && base != "npx" {
		return nil
	}

	// npx is not an install command we guard
	if base == "npx" {
		return nil
	}

	action := tokens[1]
	if !installAliases[action] {
		return nil
	}

	cmd := &api.ParsedCommand{
		PackageManager: "npm",
		Action:         action,
		IsInstall:      true,
		RawCommand:     rawCommand,
	}

	// Parse remaining tokens
	i := 2
	for i < len(tokens) {
		tok := tokens[i]

		// Skip flags
		if strings.HasPrefix(tok, "-") {
			cmd.Flags = append(cmd.Flags, tok)
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				cmd.Flags = append(cmd.Flags, tokens[i])
			}
			i++
			continue
		}

		// Package spec
		pkg := spec.ParsePackageSpec(tok)
		pkg.Ecosystem = api.EcosystemNPM
		cmd.Packages = append(cmd.Packages, pkg)
		i++
	}

	// npm install with no packages (installs from package.json) — not guarded
	if len(cmd.Packages) == 0 {
		return nil
	}

	return cmd
}

