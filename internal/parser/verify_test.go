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

	// Finding 4: chained commands — install in second/later segment must be caught
	chainedCmds := []string{
		"ls && npm install evil-pkg",
		"echo hello; npm install evil-pkg",
		"echo hello || npm install evil-pkg",
		"cat /etc/hosts | npm install evil-pkg",
		"mkdir -p foo && cd foo && npm install evil-pkg",
		"echo done; pnpm add evil-pkg",
	}
	for _, cmd := range chainedCmds {
		if Parse(cmd) == nil {
			t.Errorf("Parse(%q) = nil, want install command", cmd)
		}
		if !LooksLikeInstall(cmd) {
			t.Errorf("LooksLikeInstall(%q) = false, want true", cmd)
		}
	}

	// Finding 4b: chained commands — first segment is non-install, should still allow when no install anywhere
	nonInstallChained := []string{
		"ls && echo hello",
		"mkdir foo; cd foo",
		"echo npm && echo install",
	}
	for _, cmd := range nonInstallChained {
		if Parse(cmd) != nil {
			t.Errorf("Parse(%q) should be nil (no install in any segment)", cmd)
		}
	}

	// Finding 5: command -v is introspection, not execution
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
