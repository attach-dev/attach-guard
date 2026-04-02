package plugin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHooksCommandQuotesPluginRoot(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}

	var manifest struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}

	if len(manifest.Hooks.PreToolUse) != 1 || len(manifest.Hooks.PreToolUse[0].Hooks) != 1 {
		t.Fatalf("unexpected hook manifest shape: %+v", manifest.Hooks)
	}

	command := manifest.Hooks.PreToolUse[0].Hooks[0].Command
	if !strings.Contains(command, "\"${CLAUDE_PLUGIN_ROOT}/hooks/bootstrap.sh\"") {
		t.Fatalf("hook command does not quote CLAUDE_PLUGIN_ROOT: %q", command)
	}

	root := filepath.Join(t.TempDir(), "Plugin Root")
	hooksDir := filepath.Join(root, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(hooksDir, "bootstrap.sh")
	script := "#!/usr/bin/env bash\nprintf '%s\\n' \"$0\" \"$1\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-lc", command)
	cmd.Env = append(os.Environ(), "CLAUDE_PLUGIN_ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook command failed: %v\n%s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected hook command output: %q", out)
	}
	if lines[0] != scriptPath {
		t.Fatalf("expected script path %q, got %q", scriptPath, lines[0])
	}
	if lines[1] != "hook" {
		t.Fatalf("expected hook arg, got %q", lines[1])
	}
}

func TestExplainSkillQuotesPluginRoot(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("skills", "explain", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	const wantQuoted = "\"${CLAUDE_PLUGIN_ROOT}/hooks/bootstrap.sh\" evaluate npm install"
	if !strings.Contains(string(data), wantQuoted) {
		t.Fatalf("skill example does not quote CLAUDE_PLUGIN_ROOT path")
	}
	if !strings.Contains(string(data), "$ARGUMENTS") {
		t.Fatalf("skill example does not use $ARGUMENTS for package name substitution")
	}
}

func TestBootstrapMapsSocketTokenFromClaudeUserConfig(t *testing.T) {
	bootstrap, err := os.ReadFile(filepath.Join("hooks", "bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		envName string
	}{
		{
			name:    "manifest key casing",
			envName: "CLAUDE_PLUGIN_OPTION_socket_api_token",
		},
		{
			name:    "uppercase fallback",
			envName: "CLAUDE_PLUGIN_OPTION_SOCKET_API_TOKEN",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			hooksDir := filepath.Join(root, "hooks")
			binDir := filepath.Join(hooksDir, "bin")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}

			bootstrapPath := filepath.Join(hooksDir, "bootstrap.sh")
			if err := os.WriteFile(bootstrapPath, bootstrap, 0o755); err != nil {
				t.Fatal(err)
			}

			stub := "#!/usr/bin/env bash\nprintf '%s\\n' \"${SOCKET_API_TOKEN:-}\" \"$1\"\n"
			for _, binaryName := range []string{
				"attach-guard-darwin-amd64",
				"attach-guard-darwin-arm64",
				"attach-guard-linux-amd64",
				"attach-guard-linux-arm64",
			} {
				binaryPath := filepath.Join(binDir, binaryName)
				if err := os.WriteFile(binaryPath, []byte(stub), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			cmd := exec.Command(bootstrapPath, "version")
			cmd.Env = append(testEnvWithoutSocket(), tc.envName+"=token-from-plugin")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bootstrap failed: %v\n%s", err, out)
			}

			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != 2 {
				t.Fatalf("unexpected bootstrap output: %q", out)
			}
			if lines[0] != "token-from-plugin" {
				t.Fatalf("expected mapped token, got %q", lines[0])
			}
			if lines[1] != "version" {
				t.Fatalf("expected forwarded arg, got %q", lines[1])
			}
		})
	}
}

func testEnvWithoutSocket() []string {
	var env []string
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "SOCKET_API_TOKEN=") {
			continue
		}
		if strings.HasPrefix(entry, "CLAUDE_PLUGIN_OPTION_socket_api_token=") {
			continue
		}
		if strings.HasPrefix(entry, "CLAUDE_PLUGIN_OPTION_SOCKET_API_TOKEN=") {
			continue
		}
		env = append(env, entry)
	}
	return env
}
