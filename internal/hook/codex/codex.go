// Package codex handles Codex hook input/output.
package codex

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/attach-dev/attach-guard/pkg/api"
)

// ReadHookInput reads and parses Codex hook JSON from a reader.
func ReadHookInput(r io.Reader) (*api.HookInput, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading hook input: %w", err)
	}

	var input api.HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("parsing hook input: %w", err)
	}

	return &input, nil
}

// FormatHookOutput converts an evaluation result to Codex PreToolUse output.
// Codex PreToolUse currently supports allow+rewrite and deny, but not ask. To
// preserve safety, manual-review ASK decisions become deny responses.
func FormatHookOutput(result *api.EvaluationResult) ([]byte, error) {
	switch result.Decision {
	case api.Allow:
		if result.RewrittenCommand == "" {
			return []byte(`{}`), nil
		}
		return json.Marshal(api.HookOutput{
			HookSpecificOutput: &api.HookSpecificOutput{
				HookEventName:      "PreToolUse",
				PermissionDecision: "allow",
				UpdatedInput: map[string]string{
					"command": result.RewrittenCommand,
				},
			},
		})
	case api.Ask:
		return denyOutput("manual review required by Attach Guard: " + result.Reason)
	case api.Deny:
		return denyOutput(result.Reason)
	default:
		return []byte(`{}`), nil
	}
}

// IsGuardedTool returns true if the tool name is one we should inspect.
func IsGuardedTool(toolName string) bool {
	return toolName == "Bash"
}

func denyOutput(reason string) ([]byte, error) {
	return json.Marshal(api.HookOutput{
		HookSpecificOutput: &api.HookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		},
	})
}
