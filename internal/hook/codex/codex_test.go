package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestReadHookInput(t *testing.T) {
	input, err := ReadHookInput(strings.NewReader(`{
		"session_id":"sess_1",
		"hook_event_name":"PreToolUse",
		"tool_name":"Bash",
		"tool_input":{"command":"npm install left-pad"}
	}`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if input.ToolName != "Bash" {
		t.Fatalf("expected Bash tool, got %q", input.ToolName)
	}
	if input.ToolInput.Command != "npm install left-pad" {
		t.Fatalf("unexpected command %q", input.ToolInput.Command)
	}
}

func TestFormatHookOutputAllowsRewrite(t *testing.T) {
	out, err := FormatHookOutput(&api.EvaluationResult{
		Decision:         api.Allow,
		RewrittenCommand: "npm install left-pad@1.3.0",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}
	specific := raw["hookSpecificOutput"].(map[string]interface{})
	if specific["permissionDecision"] != "allow" {
		t.Fatalf("expected allow, got %+v", specific)
	}
	updated := specific["updatedInput"].(map[string]interface{})
	if updated["command"] != "npm install left-pad@1.3.0" {
		t.Fatalf("unexpected updatedInput %+v", updated)
	}
}

func TestFormatHookOutputMapsAskToDeny(t *testing.T) {
	out, err := FormatHookOutput(&api.EvaluationResult{
		Decision: api.Ask,
		Reason:   "score in gray band",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}
	specific := raw["hookSpecificOutput"].(map[string]interface{})
	if specific["permissionDecision"] != "deny" {
		t.Fatalf("expected deny, got %+v", specific)
	}
	if !strings.Contains(specific["permissionDecisionReason"].(string), "manual review required") {
		t.Fatalf("expected manual-review reason, got %+v", specific)
	}
}

func TestFormatHookOutputAllowsNoopWithoutUnsupportedFields(t *testing.T) {
	out, err := FormatHookOutput(&api.EvaluationResult{Decision: api.Allow})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := string(out); got != `{}` {
		t.Fatalf("expected empty success JSON, got %s", got)
	}
}
