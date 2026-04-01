// Package main is the entry point for the attach-guard CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/attach-dev/attach-guard/internal/cli"
	"github.com/attach-dev/attach-guard/internal/config"
	"github.com/attach-dev/attach-guard/internal/envdetect"
	"github.com/attach-dev/attach-guard/internal/hook/claude"
	"github.com/attach-dev/attach-guard/internal/provider"
	socketprov "github.com/attach-dev/attach-guard/internal/provider/socket"
	"github.com/attach-dev/attach-guard/pkg/api"
)

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
		// "hook run" also reads from stdin (alias)
		if len(os.Args) >= 3 && os.Args[2] == "run" {
			cmdHook()
		} else if len(os.Args) == 2 {
			cmdHook()
		} else {
			fmt.Fprintf(os.Stderr, "unknown hook subcommand: %s\nusage: attach-guard hook [run]\n", os.Args[2])
			os.Exit(exitCodeHookBlock)
		}
	case "config":
		cmdConfig()
	case "version":
		fmt.Println("attach-guard v0.1.0")
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
  hook [run]           Read Claude Code hook JSON from stdin and respond
  config init          Write default config to ~/.attach-guard/config.yaml
  version              Print version
  help                 Show this help`)
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

// loadConfigAndProvider loads configuration and creates the appropriate provider.
// exitCode controls the exit code on failure so hook mode can fail closed (exit 2).
func loadConfigAndProvider(exitCode int) (*config.Config, provider.Provider) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(exitCode)
	}

	var prov provider.Provider
	switch cfg.Provider.Kind {
	case "socket":
		p, err := socketprov.New(cfg.Provider.APITokenEnv)
		if err != nil {
			// Provider not configured — use a fallback that reports unavailable
			prov = &unavailableProvider{name: "socket"}
		} else {
			prov = p
		}
	case "mock":
		prov = provider.NewMockProvider()
	default:
		fmt.Fprintf(os.Stderr, "unknown provider: %s\n", cfg.Provider.Kind)
		os.Exit(exitCode)
	}

	return cfg, prov
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
