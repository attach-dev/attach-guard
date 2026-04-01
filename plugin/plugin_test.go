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
