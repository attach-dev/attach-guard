package parser

import "testing"

func TestBypassChainedInsideBashC(t *testing.T) {
	// Finding 6: chained commands inside bash -c bypass both Parse and LooksLikeInstall
	cmds := []string{
		"bash -c 'echo hello && npm install evil-pkg'",
		"sh -c 'ls; npm install evil-pkg'",
		"bash -c 'cd /tmp && npm install evil-pkg'",
		"bash -c 'mkdir -p foo && npm i evil-pkg'",
	}
	for _, cmd := range cmds {
		if Parse(cmd) == nil {
			t.Errorf("Parse(%q) = nil, BYPASS: install not detected", cmd)
		}
	}
	for _, cmd := range cmds {
		if !LooksLikeInstall(cmd) {
			t.Errorf("LooksLikeInstall(%q) = false, BYPASS: install not detected", cmd)
		}
	}
}

func TestBypassMultiSegmentOnlyFirstChecked(t *testing.T) {
	// ParseAll must return install commands from ALL segments
	cmd := "npm install lodash && npm install evil-pkg"
	results := ParseAll(cmd)
	if len(results) < 2 {
		t.Fatalf("ParseAll(%q) returned %d commands, want at least 2", cmd, len(results))
	}
	found := false
	for _, r := range results {
		for _, pkg := range r.Packages {
			if pkg.Name == "evil-pkg" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("ParseAll(%q) did not find evil-pkg across segments", cmd)
	}
}
