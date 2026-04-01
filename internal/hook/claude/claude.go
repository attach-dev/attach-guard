// Package claude handles Claude Code hook input/output.
package claude

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/attach-dev/attach-guard/pkg/api"
)

// ReadHookInput reads and parses Claude hook JSON from a reader.
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

// FormatHookOutput converts an evaluation result to Claude hook output JSON.
// Uses the hookSpecificOutput contract for PreToolUse events.
func FormatHookOutput(result *api.EvaluationResult) ([]byte, error) {
	specific := &api.HookSpecificOutput{
		HookEventName: "PreToolUse",
	}

	switch result.Decision {
	case api.Allow:
		specific.PermissionDecision = "allow"
		if result.RewrittenCommand != "" {
			specific.UpdatedInput = map[string]string{
				"command": result.RewrittenCommand,
			}
		}
	case api.Ask:
		specific.PermissionDecision = "ask"
		specific.PermissionDecisionReason = result.Reason
		if result.RewrittenCommand != "" {
			specific.UpdatedInput = map[string]string{
				"command": result.RewrittenCommand,
			}
		}
	case api.Deny:
		specific.PermissionDecision = "deny"
		specific.PermissionDecisionReason = result.Reason
	}

	output := api.HookOutput{
		HookSpecificOutput: specific,
	}

	return json.Marshal(output)
}

// IsGuardedTool returns true if the tool name is one we should inspect.
func IsGuardedTool(toolName string) bool {
	return toolName == "Bash"
}
