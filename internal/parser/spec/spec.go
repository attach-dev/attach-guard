// Package spec provides shared package spec parsing for npm and pnpm parsers.
package spec

import (
	"strings"

	"github.com/attach-dev/attach-guard/pkg/api"
)

// ParsePackageSpec parses a package spec like "axios", "axios@1.7.0", "@scope/pkg@^2.0.0".
func ParsePackageSpec(s string) api.PackageRequest {
	req := api.PackageRequest{
		RawSpec: s,
	}

	name, version := SplitSpec(s)
	req.Name = name
	req.Version = version
	req.Pinned = IsExactVersion(version)

	return req
}

// SplitSpec splits "name@version" into name and version.
// Handles scoped packages like "@scope/name@version".
func SplitSpec(s string) (string, string) {
	// Handle scoped packages
	if strings.HasPrefix(s, "@") {
		// Find the second @ if it exists
		rest := s[1:]
		idx := strings.Index(rest, "@")
		if idx == -1 {
			return s, ""
		}
		return s[:idx+1], rest[idx+1:]
	}

	idx := strings.Index(s, "@")
	if idx == -1 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

// IsExactVersion returns true if the version string is an exact version (no range operators).
func IsExactVersion(version string) bool {
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
