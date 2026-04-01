// Package envdetect detects the execution environment.
package envdetect

import (
	"os"

	"github.com/attach-dev/attach-guard/pkg/api"
)

// DetectMode determines the execution mode from the environment.
func DetectMode() api.Mode {
	// Common CI environment variables
	ciVars := []string{
		"CI",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"CIRCLECI",
		"JENKINS_URL",
		"BUILDKITE",
		"TRAVIS",
	}

	for _, v := range ciVars {
		if os.Getenv(v) != "" {
			return api.ModeCI
		}
	}

	return api.ModeShell
}

// IsCI returns true if running in a CI environment.
func IsCI() bool {
	return DetectMode() == api.ModeCI
}
