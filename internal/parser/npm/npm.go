// Package npm parses npm install commands.
package npm

import (
	"path/filepath"
	"strings"

	"github.com/attach-dev/attach-guard/internal/parser/spec"
	"github.com/attach-dev/attach-guard/pkg/api"
)

var installAliases = map[string]bool{
	"install": true,
	"i":       true,
	"add":     true,
}

// npmFlags that take a following argument value (post-action)
var flagsWithValue = map[string]bool{
	"--save-prefix": true,
	"--tag":         true,
	"--registry":    true,
}

// globalFlagsWithValue are npm flags that appear before the action verb and take a value.
// Note: --registry intentionally appears here and in flagsWithValue since it's valid in both positions.
var globalFlagsWithValue = map[string]bool{
	"--prefix":     true,
	"--registry":   true,
	"--userconfig":  true,
	"--cache":      true,
	"--loglevel":   true,
}

// Parse attempts to parse tokens as an npm install command.
// Returns nil if not an npm install command.
func Parse(tokens []string, rawCommand string) *api.ParsedCommand {
	if len(tokens) < 2 {
		return nil
	}

	// Check if first token is npm (could be a path like /usr/local/bin/npm)
	base := filepath.Base(tokens[0])
	if base != "npm" {
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
		PackageManager: "npm",
		Action:         action,
		PreActionFlags: preActionFlags,
		IsInstall:      true,
		RawCommand:     rawCommand,
	}

	// Parse remaining tokens (after action verb)
	i := actionIdx + 1
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
