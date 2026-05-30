package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/internal/platform"
)

func TestCmdRunDryRunClaudePrintsExpectedWrappedArgv(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cmdRun([]string{"--dry-run", "claude"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	got := stdout.String()
	if !strings.HasPrefix(got, "claude --settings ") {
		t.Fatalf("expected Claude settings injection, got %q", got)
	}
	if !strings.Contains(got, "--permission-mode default") {
		t.Fatalf("expected default permission mode, got %q", got)
	}
	if !strings.Contains(got, "disableBypassPermissionsMode") {
		t.Fatalf("expected bypass mode disable setting, got %q", got)
	}
	if !strings.Contains(got, `"sandbox":{"enabled":true,"failIfUnavailable":true,"autoAllowBashIfSandboxed":false,"allowUnsandboxedCommands":false}`) {
		t.Fatalf("expected Claude sandbox settings, got %q", got)
	}
	if !strings.Contains(got, `"disableAutoMode":"disable"`) {
		t.Fatalf("expected auto mode disable setting, got %q", got)
	}
	if stderr.String() != "" {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestCmdRunDryRunCodexPrintsExpectedWrappedArgv(t *testing.T) {
	assertDryRunPrintsWrappedArgv(t, "codex", "codex --sandbox workspace-write --ask-for-approval on-request -c sandbox_workspace_write.network_access=false\n")
}

func TestCmdRunDryRunQuotesWrappedArgv(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cmdRun([]string{"--dry-run", "claude", "--model", "sonnet 4", "can't"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "--model 'sonnet 4' 'can'\\''t'") {
		t.Fatalf("expected quoted user args, got %q", got)
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
	code := cmdRun([]string{"--dry-run", "claude"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); !strings.HasPrefix(got, "claude --settings ") {
		t.Fatalf("expected hardened Claude command, got %q", got)
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

	code := cmdRun([]string{"--dry-run", "gemini"}, strings.NewReader(""), &stdout, &stderr)

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

func TestCmdRunDryRunRejectsUnsafeClaudePermissionModes(t *testing.T) {
	tests := [][]string{
		{"--dry-run", "claude", "--permission-mode", "bypassPermissions"},
		{"--dry-run", "claude", "--permission-mode=acceptEdits"},
		{"--dry-run", "claude", "--dangerously-skip-permissions"},
		{"--dry-run", "claude", "--settings", `{"permissions":{"allow":["Bash"]}}`},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := cmdRun(args, strings.NewReader(""), &stdout, &stderr)

			if code != 1 {
				t.Fatalf("expected exit code 1, got %d", code)
			}
			if stdout.String() != "" {
				t.Fatalf("expected no stdout, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), "Claude Code") {
				t.Fatalf("expected Claude hardening error, got %q", stderr.String())
			}
		})
	}
}

func TestCmdRunDryRunRejectsUnsafeCodexSandboxModes(t *testing.T) {
	tests := [][]string{
		{"--dry-run", "codex", "--sandbox", "danger-full-access"},
		{"--dry-run", "codex", "--sandbox=danger-full-access"},
		{"--dry-run", "codex", "--dangerously-bypass-approvals-and-sandbox"},
		{"--dry-run", "codex", "--yolo"},
		{"--dry-run", "codex", "-c", "sandbox_mode = \"danger-full-access\""},
		{"--dry-run", "codex", "-c", "default_permissions=\":danger-full-access\""},
		{"--dry-run", "codex", "-c", "sandbox_workspace_write.network_access=true"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := cmdRun(args, strings.NewReader(""), &stdout, &stderr)

			if code != 1 {
				t.Fatalf("expected exit code 1, got %d", code)
			}
			if stdout.String() != "" {
				t.Fatalf("expected no stdout, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), "Codex") {
				t.Fatalf("expected Codex hardening error, got %q", stderr.String())
			}
		})
	}
}

func TestCmdRunDryRunPreservesStricterRuntimeSandboxArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := cmdRun([]string{"--dry-run", "codex", "--sandbox", "read-only", "--ask-for-approval", "untrusted"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); got != "codex -c sandbox_workspace_write.network_access=false --sandbox read-only --ask-for-approval untrusted\n" {
		t.Fatalf("unexpected stdout %q", got)
	}
	if stderr.String() != "" {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestCmdRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing args", args: []string{}},
		{name: "missing agent", args: []string{"--dry-run"}},
		{name: "unknown flag", args: []string{"--unknown", "claude"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := cmdRun(tt.args, strings.NewReader(""), &stdout, &stderr)

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

func TestCmdRunRequiresAttachPlatformSetupByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATTACH_CONFIG_PATH", filepath.Join(home, ".attach", "config.json"))

	var stdout, stderr bytes.Buffer
	code := cmdRun([]string{"claude"}, strings.NewReader(""), &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stdout.String() != "" {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Run `attach setup`") {
		t.Fatalf("expected setup guidance, got %q", stderr.String())
	}
}

func TestCmdRunExecutesAgentWithPlatformEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATTACH_CONFIG_PATH", filepath.Join(home, ".attach", "config.json"))
	restorePreflight := stubRunPreflight(t, &platform.Config{
		APIURL: "http://127.0.0.1:2009",
		APIKey: "arun_usr_secret",
	})
	defer restorePreflight()

	binDir := t.TempDir()
	agentPath := filepath.Join(binDir, "claude")
	script := []byte("#!/bin/sh\nprintf 'runtime=%s key_present=%s args=%s\\n' \"$ATTACH_RUNTIME_KIND\" \"${ATTACH_API_KEY:+yes}\" \"$*\"\nexit 7\n")
	if err := os.WriteFile(agentPath, script, 0755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}
	t.Setenv("PATH", binDir)

	var stdout, stderr bytes.Buffer
	code := cmdRun([]string{"claude", "--model", "sonnet"}, strings.NewReader(""), &stdout, &stderr)

	if code != 7 {
		t.Fatalf("expected child exit code 7, got %d; stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "runtime=claude_code key_present=yes") || !strings.Contains(got, "--model sonnet") {
		t.Fatalf("unexpected stdout %q", got)
	}
	if strings.Contains(stderr.String(), "arun_usr_secret") {
		t.Fatalf("stderr leaked token: %q", stderr.String())
	}
}

func TestCmdRunReportsRejectedPlatformCredentialWithoutLeakingToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATTACH_CONFIG_PATH", filepath.Join(home, ".attach", "config.json"))
	restorePreflight := stubRunPreflightError(t, errors.New("credential rejected by Attach Platform (401)"))
	defer restorePreflight()

	var stdout, stderr bytes.Buffer
	code := cmdRun([]string{"codex"}, strings.NewReader(""), &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stdout.String() != "" {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "credential rejected") {
		t.Fatalf("expected rejected credential error, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "arun_usr_secret") {
		t.Fatalf("stderr leaked token: %q", stderr.String())
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
	code := cmdRun([]string{"--dry-run", agent}, strings.NewReader(""), &stdout, &stderr)

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

func writeAttachConfig(t *testing.T, home, apiURL, apiKey string) {
	t.Helper()
	dir := filepath.Join(home, ".attach")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("create attach config dir: %v", err)
	}
	content := []byte(`{"api_url":` + quoteJSON(apiURL) + `,"api_key":` + quoteJSON(apiKey) + `}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), content, 0600); err != nil {
		t.Fatalf("write attach config: %v", err)
	}
}

func quoteJSON(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func stubRunPreflight(t *testing.T, cfg *platform.Config) func() {
	t.Helper()
	original := runPreflightAttachSetup
	runPreflightAttachSetup = func() (*platform.Config, error) {
		return cfg, nil
	}
	return func() { runPreflightAttachSetup = original }
}

func stubRunPreflightError(t *testing.T, err error) func() {
	t.Helper()
	original := runPreflightAttachSetup
	runPreflightAttachSetup = func() (*platform.Config, error) {
		return nil, err
	}
	return func() { runPreflightAttachSetup = original }
}
