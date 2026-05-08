package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/attach-dev/attach-guard/internal/config"
)

func TestCmdRunDryRunClaudePrintsExpectedWrappedArgv(t *testing.T) {
	assertDryRunPrintsWrappedArgv(t, "claude", "claude\n")
}

func TestCmdRunDryRunCodexPrintsExpectedWrappedArgv(t *testing.T) {
	assertDryRunPrintsWrappedArgv(t, "codex", "codex\n")
}

func TestCmdRunDryRunQuotesWrappedArgv(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cmdRun([]string{"--dry-run", "claude", "--model", "sonnet 4", "can't"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), "claude --model 'sonnet 4' 'can'\\''t'\n"; got != want {
		t.Fatalf("expected stdout %q, got %q", want, got)
	}
	if stderr.String() != "" {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestCmdRunDryRunDoesNotExecuteAgent(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "claude")
	sentinel := filepath.Join(dir, "claude.executed")
	script := []byte("#!/bin/sh\n: > \"$ATTACH_GUARD_TEST_SENTINEL\"\n")
	if err := os.WriteFile(agentPath, script, 0755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ATTACH_GUARD_TEST_SENTINEL", sentinel)

	var stdout, stderr bytes.Buffer
	code := cmdRun([]string{"--dry-run", "claude"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if got, want := stdout.String(), "claude\n"; got != want {
		t.Fatalf("expected stdout %q, got %q", want, got)
	}
	if stderr.String() != "" {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("dry-run executed fake claude binary; stat err=%v", err)
	}
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
	if !strings.Contains(stderr.String(), runUsage) {
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
		{name: "unknown flag", args: []string{"--unknown", "claude"}},
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
			if !strings.Contains(stderr.String(), runUsage) {
				t.Fatalf("expected usage, got %q", stderr.String())
			}
		})
	}
}

func TestNewProviderFromConfigKeepsSocketDefault(t *testing.T) {
	t.Setenv("SOCKET_API_TOKEN", "")

	prov, err := newProviderFromConfig(config.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if prov.Name() != "socket" {
		t.Fatalf("expected default provider socket, got %q", prov.Name())
	}
}

func TestNewProviderFromConfigOpenScore(t *testing.T) {
	timeoutSeconds := 1
	cfg := config.DefaultConfig()
	cfg.Provider.Kind = "open-score"
	cfg.Provider.Endpoint = "http://127.0.0.1:8757/v0/verdict"
	cfg.Provider.TimeoutSeconds = &timeoutSeconds

	prov, err := newProviderFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if prov.Name() != "open-score" {
		t.Fatalf("expected open-score provider, got %q", prov.Name())
	}
}

func assertDryRunPrintsWrappedArgv(t *testing.T, agent, wantStdout string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := cmdRun([]string{"--dry-run", agent}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); got != wantStdout {
		t.Fatalf("expected stdout %q, got %q", wantStdout, got)
	}
	if stderr.String() != "" {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}
