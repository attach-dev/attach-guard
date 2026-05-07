package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdRunDryRunClaudeDoesNotExecuteAgent(t *testing.T) {
	assertDryRunDoesNotExecuteAgent(t, "claude")
}

func TestCmdRunDryRunCodexDoesNotExecuteAgent(t *testing.T) {
	assertDryRunDoesNotExecuteAgent(t, "codex")
}

func TestCmdRunDryRunUnsupportedAgent(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cmdRun([]string{"--dry-run", "gemini"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stdout.String() != "" {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsupported agent: gemini") {
		t.Fatalf("expected unsupported agent error, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage: attach-guard run --dry-run <claude|codex>") {
		t.Fatalf("expected usage, got %q", stderr.String())
	}
}

func TestCmdRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing args", args: []string{}},
		{name: "missing agent", args: []string{"--dry-run"}},
		{name: "missing dry run flag", args: []string{"claude"}},
		{name: "extra args", args: []string{"--dry-run", "claude", "--verbose"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := cmdRun(tt.args, &stdout, &stderr)

			if code != 1 {
				t.Fatalf("expected exit code 1, got %d", code)
			}
			if stdout.String() != "" {
				t.Fatalf("expected no stdout, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), "usage: attach-guard run --dry-run <claude|codex>") {
				t.Fatalf("expected usage, got %q", stderr.String())
			}
		})
	}
}

func assertDryRunDoesNotExecuteAgent(t *testing.T, agent string) {
	t.Helper()

	dir := t.TempDir()
	agentPath := filepath.Join(dir, agent)
	sentinel := filepath.Join(dir, agent+".executed")
	script := []byte("#!/bin/sh\n: > \"$ATTACH_GUARD_TEST_SENTINEL\"\n")
	if err := os.WriteFile(agentPath, script, 0755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ATTACH_GUARD_TEST_SENTINEL", sentinel)

	var stdout, stderr bytes.Buffer
	code := cmdRun([]string{"--dry-run", agent}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), "dry-run: would run "+agent+"\n"; got != want {
		t.Fatalf("expected stdout %q, got %q", want, got)
	}
	if stderr.String() != "" {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("dry-run executed fake %s binary; stat err=%v", agent, err)
	}
}
