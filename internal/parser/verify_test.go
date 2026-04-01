package parser

import (
	"testing"
)

func TestVerifyReviewFindings(t *testing.T) {
	// Finding 1: workspace flags should be parsed as installs
	workspaceCmds := []string{
		"npm --workspace web install axios",
		"npm -w web install axios",
	}
	for _, cmd := range workspaceCmds {
		if Parse(cmd) == nil {
			t.Errorf("Parse(%q) = nil, want install command", cmd)
		}
	}

	// Finding 2: combined shell flags with c NOT last should still unwrap
	shellCmds := []string{
		"bash -cl 'npm install axios'",
		"sh -ce 'pnpm add react'",
	}
	for _, cmd := range shellCmds {
		if Parse(cmd) == nil {
			t.Errorf("Parse(%q) = nil, want install command", cmd)
		}
	}

	// Finding 3: shell flags that take a value should not break -c detection
	shellFlagValueCmds := []string{
		"bash -o pipefail -c 'npm install axios'",
		"bash -O extglob -c 'npm install axios'",
		"bash +O extglob -c 'npm install axios'",
		"bash --rcfile /dev/null -c 'npm install axios'",
		"bash --init-file /dev/null -c 'npm install axios'",
		"sh +o posix -c 'pnpm add react'",
		"bash -o pipefail -o errexit -c 'npm install axios'",
	}
	for _, cmd := range shellFlagValueCmds {
		if Parse(cmd) == nil {
			t.Errorf("Parse(%q) = nil, want install command", cmd)
		}
		if !LooksLikeInstall(cmd) {
			t.Errorf("LooksLikeInstall(%q) = false, want true", cmd)
		}
	}

	// Finding 4: command -v is introspection, not execution
	introspecCmds := []string{
		"command -v npm",
		"command -v npm install axios",
		"command -V npm install axios",
	}
	for _, cmd := range introspecCmds {
		if Parse(cmd) != nil {
			t.Errorf("Parse(%q) should be nil (introspection, not install)", cmd)
		}
		if LooksLikeInstall(cmd) {
			t.Errorf("LooksLikeInstall(%q) = true, want false (introspection)", cmd)
		}
	}
}
