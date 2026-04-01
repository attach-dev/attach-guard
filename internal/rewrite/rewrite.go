// Package rewrite rewrites package manager commands with pinned versions.
package rewrite

import (
	"fmt"
	"strings"

	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
)

// Command rewrites a parsed command with selected versions.
// selectedVersions maps package name -> pinned version.
func Command(cmd *api.ParsedCommand, selectedVersions map[string]string) string {
	var parts []string

	switch cmd.PackageManager {
	case "npm":
		parts = append(parts, "npm", cmd.Action)
	case "pnpm":
		parts = append(parts, "pnpm", cmd.Action)
	default:
		parts = append(parts, cmd.PackageManager, cmd.Action)
	}

	for _, pkg := range cmd.Packages {
		if v, ok := selectedVersions[pkg.Name]; ok && v != "" {
			parts = append(parts, fmt.Sprintf("%s@%s", pkg.Name, v))
		} else {
			parts = append(parts, pkg.RawSpec)
		}
	}

	parts = append(parts, cmd.Flags...)

	return strings.Join(parts, " ")
}
