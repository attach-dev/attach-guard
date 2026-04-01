// Package npm parses npm install commands.
package npm

import (
	"path/filepath"
	"strings"

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
		pkg := parsePackageSpec(tok)
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

// parsePackageSpec parses a package spec like "axios", "axios@1.7.0", "@scope/pkg@^2.0.0"
func parsePackageSpec(spec string) api.PackageRequest {
	req := api.PackageRequest{
		RawSpec: spec,
	}

	name, version := splitSpec(spec)
	req.Name = name
	req.Version = version
	req.Pinned = isExactVersion(version)

	return req
}

// splitSpec splits "name@version" into name and version.
// Handles scoped packages like "@scope/name@version".
func splitSpec(spec string) (string, string) {
	// Handle scoped packages
	if strings.HasPrefix(spec, "@") {
		// Find the second @ if it exists
		rest := spec[1:]
		idx := strings.Index(rest, "@")
		if idx == -1 {
			return spec, ""
		}
		return spec[:idx+1], rest[idx+1:]
	}

	idx := strings.Index(spec, "@")
	if idx == -1 {
		return spec, ""
	}
	return spec[:idx], spec[idx+1:]
}

// isExactVersion returns true if the version string is an exact version (no range operators).
func isExactVersion(version string) bool {
	if version == "" || version == "latest" || version == "*" {
		return false
	}

	// Contains range operators
	for _, prefix := range []string{"^", "~", ">=", "<=", ">", "<"} {
		if strings.HasPrefix(version, prefix) {
			return false
		}
	}

	// Contains spaces or || (range expression)
	if strings.Contains(version, " ") || strings.Contains(version, "||") {
		return false
	}

	return true
}
