// Package api defines the public domain types for attach-guard.
package api

import "time"

// Decision represents a policy decision.
type Decision string

const (
	Allow Decision = "allow"
	Ask   Decision = "ask"
	Deny  Decision = "deny"
)

// Ecosystem identifies a package ecosystem.
type Ecosystem string

const (
	EcosystemNPM  Ecosystem = "npm"
	EcosystemPNPM Ecosystem = "pnpm"
)

// PackageRequest represents a single package requested for installation.
type PackageRequest struct {
	Ecosystem Ecosystem `json:"ecosystem"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`   // empty or "*" means unpinned
	Pinned    bool      `json:"pinned"`    // true if user specified an exact version
	RawSpec   string    `json:"raw_spec"`  // original spec string, e.g. "axios@1.7.0"
}

// PackageScore holds normalized score data from a provider.
type PackageScore struct {
	SupplyChain float64 `json:"supply_chain"`
	Overall     float64 `json:"overall"`
}

// PackageAlert represents a known issue or alert for a package version.
type PackageAlert struct {
	Severity string `json:"severity"` // critical, high, medium, low
	Title    string `json:"title"`
	Category string `json:"category"` // malware, vulnerability, quality, etc.
}

// VersionInfo holds metadata about a specific package version.
type VersionInfo struct {
	Version     string         `json:"version"`
	PublishedAt time.Time      `json:"published_at"`
	Score       PackageScore   `json:"score"`
	Alerts      []PackageAlert `json:"alerts"`
	Deprecated  bool           `json:"deprecated"`
}

// AgeHours returns the age of this version in hours.
func (v *VersionInfo) AgeHours() float64 {
	if v.PublishedAt.IsZero() {
		return 0
	}
	return time.Since(v.PublishedAt).Hours()
}

// PackageEvaluation holds the evaluation result for a single package.
type PackageEvaluation struct {
	Ecosystem       Ecosystem      `json:"ecosystem"`
	Name            string         `json:"name"`
	Requested       string         `json:"requested"`
	SelectedVersion string         `json:"selected_version"`
	Score           PackageScore   `json:"score"`
	AgeHours        float64        `json:"age_hours"`
	Alerts          []PackageAlert `json:"alerts"`
}

// EvaluationResult holds the full result of evaluating a command.
type EvaluationResult struct {
	Decision        Decision            `json:"decision"`
	Reason          string              `json:"reason"`
	OriginalCommand string              `json:"original_command"`
	RewrittenCommand string             `json:"rewritten_command,omitempty"`
	Packages        []PackageEvaluation `json:"packages"`
}

// Mode represents the execution mode.
type Mode string

const (
	ModeClaude Mode = "claude"
	ModeShell  Mode = "shell"
	ModeCI     Mode = "ci"
)

// ParsedCommand represents a parsed package manager command.
type ParsedCommand struct {
	PackageManager string           `json:"package_manager"` // npm, pnpm
	Action         string           `json:"action"`          // install, add, etc.
	Packages       []PackageRequest `json:"packages"`
	Flags          []string         `json:"flags"`
	IsInstall      bool             `json:"is_install"`
	RawCommand     string           `json:"raw_command"`
}

// HookInput is the JSON structure received from Claude Code PreToolUse hooks.
type HookInput struct {
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

// HookOutput is the JSON structure returned to Claude Code from hooks.
type HookOutput struct {
	Decision     string `json:"decision"`               // allow, ask, deny
	Reason       string `json:"reason,omitempty"`
	UpdatedInput *struct {
		Command string `json:"command"`
	} `json:"updatedInput,omitempty"`
}
