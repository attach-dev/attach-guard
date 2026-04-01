package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/attach-dev/attach-guard/pkg/api"
)

func TestReadHookInput(t *testing.T) {
	input := `{"session_id":"abc","tool_name":"Bash","tool_input":{"command":"npm install axios"}}`

	result, err := ReadHookInput(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolName != "Bash" {
		t.Errorf("expected tool_name=Bash, got %s", result.ToolName)
	}
	if result.ToolInput.Command != "npm install axios" {
		t.Errorf("expected command 'npm install axios', got %s", result.ToolInput.Command)
	}
}

func TestFormatHookOutput_Allow(t *testing.T) {
	result := &api.EvaluationResult{
		Decision: api.Allow,
		Reason:   "all good",
	}

	data, err := FormatHookOutput(result)
	if err != nil {
		t.Fatal(err)
	}

	var output api.HookOutput
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}

	s := output.HookSpecificOutput
	if s == nil {
		t.Fatal("expected hookSpecificOutput to be set")
	}
	if s.HookEventName != "PreToolUse" {
		t.Errorf("expected hookEventName=PreToolUse, got %s", s.HookEventName)
	}
	if s.PermissionDecision != "allow" {
		t.Errorf("expected permissionDecision=allow, got %s", s.PermissionDecision)
	}
	if s.UpdatedInput != nil {
		t.Error("expected no updatedInput for plain allow")
	}
}

func TestFormatHookOutput_AllowWithRewrite(t *testing.T) {
	result := &api.EvaluationResult{
		Decision:         api.Allow,
		Reason:           "rewritten",
		RewrittenCommand: "npm install axios@1.7.0",
	}

	data, err := FormatHookOutput(result)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	specific, ok := raw["hookSpecificOutput"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hookSpecificOutput object")
	}
	if specific["permissionDecision"] != "allow" {
		t.Errorf("expected allow, got %v", specific["permissionDecision"])
	}
	updated, ok := specific["updatedInput"].(map[string]interface{})
	if !ok {
		t.Fatal("expected updatedInput for allow with rewrite")
	}
	if updated["command"] != "npm install axios@1.7.0" {
		t.Errorf("expected rewritten command, got %v", updated["command"])
	}
}

func TestFormatHookOutput_Ask(t *testing.T) {
	result := &api.EvaluationResult{
		Decision:         api.Ask,
		Reason:           "score in gray band",
		RewrittenCommand: "npm install pkg@1.2.0",
	}

	data, err := FormatHookOutput(result)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	specific, ok := raw["hookSpecificOutput"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hookSpecificOutput object")
	}
	if specific["permissionDecision"] != "ask" {
		t.Errorf("expected ask, got %v", specific["permissionDecision"])
	}
	if specific["permissionDecisionReason"] == nil || specific["permissionDecisionReason"] == "" {
		t.Error("expected permissionDecisionReason for ask")
	}
	updated, ok := specific["updatedInput"].(map[string]interface{})
	if !ok {
		t.Fatal("expected updatedInput for ask with rewrite")
	}
	if updated["command"] != "npm install pkg@1.2.0" {
		t.Errorf("expected rewritten command, got %v", updated["command"])
	}
}

func TestFormatHookOutput_Deny(t *testing.T) {
	result := &api.EvaluationResult{
		Decision: api.Deny,
		Reason:   "known malware",
	}

	data, err := FormatHookOutput(result)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	specific, ok := raw["hookSpecificOutput"].(map[string]interface{})
	if !ok {
		t.Fatal("expected hookSpecificOutput object")
	}
	if specific["permissionDecision"] != "deny" {
		t.Errorf("expected deny, got %v", specific["permissionDecision"])
	}
	if specific["permissionDecisionReason"] != "known malware" {
		t.Errorf("expected reason, got %v", specific["permissionDecisionReason"])
	}
}

func TestFormatHookOutput_ValidJSON(t *testing.T) {
	results := []*api.EvaluationResult{
		{Decision: api.Allow, Reason: "ok"},
		{Decision: api.Ask, Reason: "review", RewrittenCommand: "npm i x@1.0"},
		{Decision: api.Deny, Reason: "blocked"},
	}

	for _, r := range results {
		data, err := FormatHookOutput(r)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(data) {
			t.Errorf("output is not valid JSON: %s", string(data))
		}
	}
}

func TestIsGuardedTool(t *testing.T) {
	if !IsGuardedTool("Bash") {
		t.Error("Bash should be guarded")
	}
	if IsGuardedTool("Read") {
		t.Error("Read should not be guarded")
	}
	if IsGuardedTool("Write") {
		t.Error("Write should not be guarded")
	}
}
