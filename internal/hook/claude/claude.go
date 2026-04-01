// Package claude handles Claude Code hook input/output.
package claude

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/hammadtq/attach-dev/attach-guard/pkg/api"
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
func FormatHookOutput(result *api.EvaluationResult) ([]byte, error) {
	output := api.HookOutput{}

	switch result.Decision {
	case api.Allow:
		output.Decision = "allow"
		if result.RewrittenCommand != "" {
			output.UpdatedInput = &struct {
				Command string `json:"command"`
			}{Command: result.RewrittenCommand}
		}
	case api.Ask:
		output.Decision = "ask"
		output.Reason = result.Reason
		if result.RewrittenCommand != "" {
			output.UpdatedInput = &struct {
				Command string `json:"command"`
			}{Command: result.RewrittenCommand}
		}
	case api.Deny:
		output.Decision = "deny"
		output.Reason = result.Reason
	}

	return json.Marshal(output)
}

// IsGuardedTool returns true if the tool name is one we should inspect.
func IsGuardedTool(toolName string) bool {
	return toolName == "Bash"
}
