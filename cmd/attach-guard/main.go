// Package main is the entry point for the attach-guard CLI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/attach-dev/attach-guard/internal/cli"
	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/internal/envdetect"
	"github.com/attach-dev/attach-guard/internal/hook/claude"
	"github.com/attach-dev/attach-guard/internal/hook/codex"
	"github.com/attach-dev/attach-guard/internal/platform"
	"github.com/attach-dev/attach-guard/internal/provider"
	openscoreprov "github.com/attach-dev/attach-guard/internal/provider/openscore"
	socketprov "github.com/attach-dev/attach-guard/internal/provider/socket"
	"github.com/attach-dev/attach-guard/pkg/api"
)

// version is set at build time via -ldflags.
var version = "dev"

// exitCodeHookBlock is the exit code that tells Claude Code to block the tool
// call. Claude Code treats exit code 2 as a blocking hook error; any other
// non-zero exit is non-blocking (fail-open). We use this in hook mode so that
// internal errors (config, provider, evaluation) fail closed.
const exitCodeHookBlock = 2

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "evaluate":
		cmdEvaluate()
	case "hook":
		// "hook" with no subcommand reads hook JSON from stdin
		// "hook run" also reads from stdin (Claude compatibility alias)
		if len(os.Args) >= 3 && os.Args[2] == "run" {
			cmdHook()
		} else if len(os.Args) >= 3 && os.Args[2] == "claude" {
			cmdHook()
		} else if len(os.Args) >= 3 && os.Args[2] == "codex" {
			cmdCodexHook()
		} else if len(os.Args) == 2 {
			cmdHook()
		} else {
			fmt.Fprintf(os.Stderr, "unknown hook subcommand: %s\nusage: attach-guard hook [run|claude|codex]\n", os.Args[2])
			os.Exit(exitCodeHookBlock)
		}
	case "run":
		os.Exit(cmdRun(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	case "config":
		cmdConfig()
	case "version":
		fmt.Printf("attach-guard v%s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: attach-guard <command> [args]

Commands:
  evaluate <command>   Evaluate a package manager command against policy
  hook [run|claude|codex]
                       Read runtime hook JSON from stdin and respond
  run [--dry-run] <claude|codex> [args...] Run an agent with Attach Platform setup preflight and runtime hardening
  config init          Write default config to ~/.attach-guard/config.yaml
  version              Print version
  help                 Show this help`)
}

const runUsage = "usage: attach-guard run [--dry-run] <claude|codex> [args...]"

var runPreflightAttachSetup = preflightAttachSetup

// cmdRun handles agent-wrapper commands.
func cmdRun(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("attach-guard run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, runUsage)
	}
	dryRun := fs.Bool("dry-run", false, "print the wrapped command without executing it")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	positional := fs.Args()
	if len(positional) < 1 {
		fmt.Fprintln(stderr, runUsage)
		return 1
	}

	agent := positional[0]
	wrappedCommand, err := wrappedAgentCommand(agent, positional[1:])
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintln(stderr, runUsage)
		return 1
	}

	if *dryRun {
		fmt.Fprintln(stdout, shellQuoteLine(wrappedCommand))
		return 0
	}

	setup, err := runPreflightAttachSetup()
	if err != nil {
		fmt.Fprintf(stderr, "Attach Platform setup required: %v\n", err)
		fmt.Fprintln(stderr, "Run `attach setup` before `attach-guard run`, then retry.")
		return 1
	}

	return executeAgent(wrappedCommand, guardedAgentEnv(os.Environ(), setup, agent), stdin, stdout, stderr)
}

func preflightAttachSetup() (*platform.Config, error) {
	path := platform.DefaultConfigPath()
	setup, err := platform.Load(path)
	if err != nil {
		return nil, err
	}
	if err := platform.Verify(context.Background(), nil, setup); err != nil {
		return nil, err
	}
	return setup, nil
}

func executeAgent(argv []string, env []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "failed to start %s: %v\n", argv[0], err)
		return 1
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-signals:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		case <-done:
		}
	}()

	err := cmd.Wait()
	close(done)
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(stderr, "failed to run %s: %v\n", argv[0], err)
	return 1
}

func guardedAgentEnv(base []string, setup *platform.Config, agent string) []string {
	runtimeKind := agent
	if agent == "claude" {
		runtimeKind = "claude_code"
	}
	return withEnv(base, map[string]string{
		"ATTACH_API_URL":              setup.APIURL,
		"ATTACH_API_KEY":              setup.APIKey,
		"ATTACH_SCORE_API_URL":        setup.APIURL,
		"ATTACH_SCORE_API_KEY":        setup.APIKey,
		"ATTACH_RUNTIME_KIND":         runtimeKind,
		"ATTACH_GUARD_ACTIVE":         "1",
		"ATTACH_GUARD_AGENT_COMMAND":  agent,
		"ATTACH_GUARD_PLATFORM_SETUP": "1",
	})
}

type claudeRunSettings struct {
	Permissions     claudePermissionSettings `json:"permissions"`
	DisableAutoMode string                   `json:"disableAutoMode"`
	Sandbox         claudeSandboxSettings    `json:"sandbox"`
}

type claudePermissionSettings struct {
	Deny        []string `json:"deny"`
	DefaultMode string   `json:"defaultMode"`

	// This key prevents the bypass permissions mode from being activated for
	// this session.
	DisableBypassPermissionsMode string `json:"disableBypassPermissionsMode"`
}

type claudeSandboxSettings struct {
	Enabled                  bool `json:"enabled"`
	FailIfUnavailable        bool `json:"failIfUnavailable"`
	AutoAllowBashIfSandboxed bool `json:"autoAllowBashIfSandboxed"`
	AllowUnsandboxedCommands bool `json:"allowUnsandboxedCommands"`
}

func withEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	seen := make(map[string]bool, len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if value, exists := overrides[key]; exists {
				out = append(out, key+"="+value)
				seen[key] = true
				continue
			}
		}
		out = append(out, entry)
	}
	for key, value := range overrides {
		if !seen[key] {
			out = append(out, key+"="+value)
		}
	}
	return out
}

func wrappedAgentCommand(agent string, args []string) ([]string, error) {
	switch agent {
	case "claude":
		hardenedArgs, err := hardenedClaudeArgs(args)
		if err != nil {
			return nil, err
		}
		argv := make([]string, 1, 1+len(hardenedArgs))
		argv[0] = agent
		argv = append(argv, hardenedArgs...)
		return argv, nil
	case "codex":
		hardenedArgs, err := hardenedCodexArgs(args)
		if err != nil {
			return nil, err
		}
		argv := make([]string, 1, 1+len(hardenedArgs))
		argv[0] = agent
		argv = append(argv, hardenedArgs...)
		return argv, nil
	default:
		return nil, fmt.Errorf("unsupported agent: %s", agent)
	}
}

func hardenedClaudeArgs(args []string) ([]string, error) {
	if err := validateClaudeRunArgs(args); err != nil {
		return nil, err
	}

	settings := claudeRunSettings{
		Permissions: claudePermissionSettings{
			Deny: []string{
				"WebFetch",
				"WebSearch",
				"Read(./.env)",
				"Read(./.env.*)",
				"Read(./secrets/**)",
				"Bash(curl *)",
				"Bash(wget *)",
			},
			DefaultMode:                  "default",
			DisableBypassPermissionsMode: "disable",
		},
		DisableAutoMode: "disable",
		Sandbox: claudeSandboxSettings{
			Enabled:                  true,
			FailIfUnavailable:        true,
			AutoAllowBashIfSandboxed: false,
			AllowUnsandboxedCommands: false,
		},
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to build Claude run settings: %w", err)
	}

	out := []string{"--settings", string(settingsJSON)}
	if !hasFlagWithValue(args, "--permission-mode") {
		out = append(out, "--permission-mode", "default")
	}
	return append(out, args...), nil
}

func validateClaudeRunArgs(args []string) error {
	if hasBoolFlag(args, "--dangerously-skip-permissions") {
		return fmt.Errorf("Claude Code bypass permissions mode is not allowed under attach-guard run")
	}
	if hasFlagWithValue(args, "--settings") {
		return fmt.Errorf("Claude Code --settings is managed by attach-guard run so the hardened session policy cannot be overridden")
	}
	if mode, ok := flagValue(args, "--permission-mode"); ok {
		switch mode {
		case "default", "plan":
			return nil
		default:
			return fmt.Errorf("Claude Code permission mode %q is not allowed under attach-guard run; use default or plan", mode)
		}
	}
	return nil
}

func hardenedCodexArgs(args []string) ([]string, error) {
	if err := validateCodexRunArgs(args); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(args)+6)
	if !hasFlagWithValue(args, "--sandbox") && !hasCodexConfigKey(args, "sandbox_mode") && !hasCodexConfigKey(args, "default_permissions") {
		out = append(out, "--sandbox", "workspace-write")
	}
	if !hasFlagWithValue(args, "--ask-for-approval") && !hasFlagWithValue(args, "-a") && !hasCodexConfigKey(args, "approval_policy") {
		out = append(out, "--ask-for-approval", "on-request")
	}
	if !hasCodexConfigKey(args, "sandbox_workspace_write.network_access") {
		out = append(out, "-c", "sandbox_workspace_write.network_access=false")
	}
	return append(out, args...), nil
}

func validateCodexRunArgs(args []string) error {
	if hasBoolFlag(args, "--dangerously-bypass-approvals-and-sandbox") || hasBoolFlag(args, "--yolo") {
		return fmt.Errorf("Codex danger-full-access mode is not allowed under attach-guard run")
	}
	if sandbox, ok := flagValue(args, "--sandbox"); ok && sandbox == "danger-full-access" {
		return fmt.Errorf("Codex sandbox %q is not allowed under attach-guard run", sandbox)
	}
	for _, value := range codexConfigValues(args) {
		normalized := normalizeConfigAssignment(value)
		switch {
		case normalized == "sandbox_mode=danger-full-access":
			return fmt.Errorf("Codex sandbox_mode danger-full-access is not allowed under attach-guard run")
		case normalized == "default_permissions=:danger-full-access":
			return fmt.Errorf("Codex default_permissions :danger-full-access is not allowed under attach-guard run")
		case normalized == "sandbox_workspace_write.network_access=true":
			return fmt.Errorf("Codex command network access is disabled under attach-guard run")
		}
	}
	return nil
}

func hasBoolFlag(args []string, name string) bool {
	prefix := name + "="
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func hasFlagWithValue(args []string, name string) bool {
	_, ok := flagValue(args, name)
	return ok
}

func flagValue(args []string, name string) (string, bool) {
	prefix := name + "="
	for i, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix), true
		}
		if arg == name && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func hasCodexConfigKey(args []string, key string) bool {
	prefix := key + "="
	for _, value := range codexConfigValues(args) {
		if strings.HasPrefix(normalizeConfigAssignment(value), prefix) {
			return true
		}
	}
	return false
}

func codexConfigValues(args []string) []string {
	var values []string
	for i, arg := range args {
		switch {
		case arg == "-c" || arg == "--config":
			if i+1 < len(args) {
				values = append(values, args[i+1])
			}
		case strings.HasPrefix(arg, "-c="):
			values = append(values, strings.TrimPrefix(arg, "-c="))
		case strings.HasPrefix(arg, "--config="):
			values = append(values, strings.TrimPrefix(arg, "--config="))
		}
	}
	return values
}

func normalizeConfigAssignment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "\t", "")
	value = strings.ReplaceAll(value, `"`, "")
	value = strings.ReplaceAll(value, `'`, "")
	return value
}

func shellQuoteLine(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = shellQuoteArg(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if isShellSafe(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

func isShellSafe(arg string) bool {
	for _, r := range arg {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '@', '%', '_', '+', '=', ':', ',', '.', '/', '-':
			continue
		default:
			return false
		}
	}
	return true
}

// cmdEvaluate evaluates a command string passed as arguments.
// Note: the shell strips quoting before Go sees os.Args, so commands with
// shell-significant characters (&&, ||, quotes) should be passed as a single
// quoted argument: attach-guard evaluate "bash -c 'npm install axios'"
// For accurate parsing of complex commands, use the hook path instead.
func cmdEvaluate() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: attach-guard evaluate <command>")
		os.Exit(1)
	}

	rawCommand := strings.Join(os.Args[2:], " ")
	mode := envdetect.DetectMode()

	cfg, prov := loadConfigAndProvider(1)
	eval := cli.NewEvaluator(cfg, prov)

	data, err := eval.EvaluateJSON(context.Background(), rawCommand, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(data))
}

// cmdHook reads Claude Code hook JSON from stdin and writes hook output.
// All error paths use exitCodeHookBlock (2) so Claude Code blocks the tool call
// on internal failures rather than failing open.
func cmdHook() {
	input, err := claude.ReadHookInput(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading hook input: %v\n", err)
		os.Exit(exitCodeHookBlock)
	}

	if !claude.IsGuardedTool(input.ToolName) {
		// Not a guarded tool — allow
		out, err := claude.FormatHookOutput(&api.EvaluationResult{
			Decision: api.Allow,
			Reason:   "not a guarded tool",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error formatting output: %v\n", err)
			os.Exit(exitCodeHookBlock)
		}
		fmt.Println(string(out))
		return
	}

	// Skip evaluation when the command is invoking attach-guard itself
	// (e.g. the evaluate subcommand via bootstrap.sh or the binary directly).
	// The hook sees the full bash text and would otherwise block on the
	// "npm install axios" arguments.
	if isSelfInvocation(input.ToolInput.Command) {
		out, err := claude.FormatHookOutput(&api.EvaluationResult{
			Decision: api.Allow,
			Reason:   "attach-guard self-invocation",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error formatting output: %v\n", err)
			os.Exit(exitCodeHookBlock)
		}
		fmt.Println(string(out))
		return
	}

	mode := api.ModeClaude
	cfg, prov := loadConfigAndProvider(exitCodeHookBlock)
	eval := cli.NewEvaluator(cfg, prov)

	result, err := eval.Evaluate(context.Background(), input.ToolInput.Command, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error evaluating: %v\n", err)
		os.Exit(exitCodeHookBlock)
	}

	out, err := claude.FormatHookOutput(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error formatting output: %v\n", err)
		os.Exit(exitCodeHookBlock)
	}

	fmt.Println(string(out))
}

// cmdCodexHook reads Codex hook JSON from stdin and writes Codex PreToolUse output.
func cmdCodexHook() {
	input, err := codex.ReadHookInput(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading hook input: %v\n", err)
		os.Exit(exitCodeHookBlock)
	}

	if !codex.IsGuardedTool(input.ToolName) {
		out, err := codex.FormatHookOutput(&api.EvaluationResult{
			Decision: api.Allow,
			Reason:   "not a guarded tool",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error formatting output: %v\n", err)
			os.Exit(exitCodeHookBlock)
		}
		fmt.Println(string(out))
		return
	}

	if isSelfInvocation(input.ToolInput.Command) {
		out, err := codex.FormatHookOutput(&api.EvaluationResult{
			Decision: api.Allow,
			Reason:   "attach-guard self-invocation",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error formatting output: %v\n", err)
			os.Exit(exitCodeHookBlock)
		}
		fmt.Println(string(out))
		return
	}

	cfg, prov := loadConfigAndProvider(exitCodeHookBlock)
	eval := cli.NewEvaluator(cfg, prov)

	result, err := eval.Evaluate(context.Background(), input.ToolInput.Command, api.ModeCodex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error evaluating: %v\n", err)
		os.Exit(exitCodeHookBlock)
	}

	out, err := codex.FormatHookOutput(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error formatting output: %v\n", err)
		os.Exit(exitCodeHookBlock)
	}

	fmt.Println(string(out))
}

// cmdConfig handles config subcommands.
func cmdConfig() {
	if len(os.Args) < 3 || os.Args[2] != "init" {
		fmt.Fprintln(os.Stderr, "usage: attach-guard config init")
		os.Exit(1)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	path := home + "/.attach-guard/config.yaml"
	if err := config.WriteDefault(path); err != nil {
		fmt.Fprintf(os.Stderr, "error writing config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Default config written to %s\n", path)
}

// isSelfInvocation returns true when the command text is invoking attach-guard
// itself (e.g. the evaluate subcommand via bootstrap.sh or the binary directly).
// We check two things:
//  1. The command contains "attach-guard" (direct binary invocation).
//  2. The command references the plugin config directory (bootstrap.sh invocation
//     from the marketplace plugin copy, whose path contains the org name but not
//     necessarily "attach-guard").
func isSelfInvocation(command string) bool {
	if strings.Contains(command, "attach-guard") {
		return true
	}
	if pluginDir := os.Getenv("ATTACH_GUARD_PLUGIN_CONFIG_DIR"); pluginDir != "" {
		// pluginDir is <plugin-root>/config — derive the plugin root
		pluginRoot := strings.TrimSuffix(pluginDir, "/config")
		if strings.Contains(command, pluginRoot) {
			return true
		}
	}
	return false
}

// loadConfigAndProvider loads configuration and creates the appropriate provider.
// exitCode controls the exit code on failure so hook mode can fail closed (exit 2).
func loadConfigAndProvider(exitCode int) (*config.Config, provider.Provider) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(exitCode)
	}

	prov, err := newProviderFromConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error configuring provider: %v\n", err)
		os.Exit(exitCode)
	}

	return cfg, prov
}

func newProviderFromConfig(cfg *config.Config) (provider.Provider, error) {
	switch cfg.Provider.Kind {
	case "socket":
		p, err := socketprov.New(cfg.Provider.APITokenEnv)
		if err != nil {
			// Provider not configured — use a fallback that reports unavailable
			return &unavailableProvider{name: "socket"}, nil
		}
		return p, nil
	case "open-score":
		timeoutSeconds := 0
		if cfg.Provider.TimeoutSeconds != nil {
			timeoutSeconds = *cfg.Provider.TimeoutSeconds
		}
		return openscoreprov.New(cfg.Provider.Endpoint, timeoutSeconds)
	case "mock":
		return provider.NewMockProvider(), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", cfg.Provider.Kind)
	}
}

// unavailableProvider is a provider that always reports unavailable.
type unavailableProvider struct {
	name string
}

func (u *unavailableProvider) Name() string { return u.name }
func (u *unavailableProvider) IsAvailable(_ context.Context) bool {
	return false
}
func (u *unavailableProvider) GetPackageScore(_ context.Context, _ api.Ecosystem, name, version string) (*api.VersionInfo, error) {
	return nil, fmt.Errorf("provider %s is not available", u.name)
}
func (u *unavailableProvider) ListVersions(_ context.Context, _ api.Ecosystem, name string) ([]api.VersionInfo, error) {
	return nil, fmt.Errorf("provider %s is not available", u.name)
}
