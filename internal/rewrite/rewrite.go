// Package rewrite rewrites package manager commands with pinned versions.
package rewrite

import (
	"fmt"
	"strings"

	"github.com/attach-dev/attach-guard/pkg/api"
)

// Command rewrites a parsed command with selected versions.
// selectedVersions maps package name -> pinned version.
func Command(cmd *api.ParsedCommand, selectedVersions map[string]string) string {
	var parts []string

	// Package manager
	parts = append(parts, cmd.PackageManager)

	// Pre-action flags (must come before the action verb)
	parts = append(parts, cmd.PreActionFlags...)

	// Action verb
	parts = append(parts, cmd.Action)

	// go get expects flags before package args.
	if cmd.PackageManager == "go" {
		parts = append(parts, cmd.Flags...)
	}

	// Packages
	for _, pkg := range cmd.Packages {
		if v, ok := selectedVersions[pkg.Name]; ok && v != "" {
			parts = append(parts, formatPinnedSpec(cmd.PackageManager, pkg.Name, v))
		} else {
			parts = append(parts, pkg.RawSpec)
		}
	}

	// Post-action flags
	if cmd.PackageManager != "go" {
		parts = append(parts, cmd.Flags...)
	}

	return strings.Join(parts, " ")
}

func formatPinnedSpec(pm, name, version string) string {
	switch pm {
	case "pip", "pip3":
		return fmt.Sprintf("%s==%s", name, version)
	case "go":
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		return fmt.Sprintf("%s@%s", name, version)
	case "cargo":
		return fmt.Sprintf("%s@=%s", name, version)
	default:
		return fmt.Sprintf("%s@%s", name, version)
	}
}
