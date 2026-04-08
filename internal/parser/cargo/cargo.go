// Package cargo parses scoped cargo add commands.
package cargo

import (
	"path/filepath"
	"strings"

	"github.com/attach-dev/attach-guard/internal/parser/parseutil"
	"github.com/attach-dev/attach-guard/pkg/api"
)

var preActionFlagsWithValue = map[string]bool{
	"--color":  true,
	"--config": true,
	"-Z":       true,
}

var flagsWithValue = map[string]bool{
	"-F":              true,
	"--features":      true,
	"--rename":        true,
	"--manifest-path": true,
	"--registry":      true,
	"-p":              true,
	"--package":       true,
	"--target-dir":    true,
	"--branch":        true,
	"--tag":           true,
	"--rev":           true,
	"--git":           true,
	"--path":          true,
	"--version":       true,
	"--root":          true,
}

var booleanFlags = map[string]bool{
	"--optional":            true,
	"--dev":                 true,
	"--build":               true,
	"--public":              true,
	"--no-default-features": true,
	"--dry-run":             true,
	"--offline":             true,
	"--locked":              true,
	"--frozen":              true,
	"-q":                    true,
	"--quiet":               true,
	"-v":                    true,
	"--verbose":             true,
	"--force":               true,
	"-f":                    true,
	"--no-track":            true,
	"--list":                true,
	"--debug":               true,
	"--release":             true,
}

// Parse attempts to parse direct cargo add commands.
// Unlike npm/pnpm parsing, recognized commands may return a ParsedCommand with
// zero evaluable packages when all positional args were skipped as unsupported.
func Parse(tokens []string, rawCommand string) *api.ParsedCommand {
	if len(tokens) < 2 {
		return nil
	}

	if filepath.Base(tokens[0]) != "cargo" {
		return nil
	}

	var preActionFlags []string
	actionIdx := -1
	actionVerb := ""
	hasUnparsed := false
	hasNonLocalUnparsed := false
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "add" || tok == "install" {
			actionIdx = i
			actionVerb = tok
			break
		}
		if strings.HasPrefix(tok, "-") {
			preActionFlags = append(preActionFlags, tok)
			if name, _, ok := parseutil.SplitLongFlagAssignment(tok); ok {
				if booleanFlags[name] {
					continue
				}
				if preActionFlagsWithValue[name] {
					continue
				}
			}
			if booleanFlags[tok] {
				continue
			}
			if preActionFlagsWithValue[tok] && i+1 < len(tokens) {
				i++
				preActionFlags = append(preActionFlags, tokens[i])
				continue
			}
			if parseutil.ShouldConsumeUnknownLongFlagValue(tok, tokens, i, "add", "install") {
				hasUnparsed = true
				hasNonLocalUnparsed = true
				i++
				preActionFlags = append(preActionFlags, tokens[i])
				continue
			}
			if isUnknownLongFlag(tok) {
				hasUnparsed = true
				hasNonLocalUnparsed = true
			}
			continue
		}
		return nil
	}
	if actionIdx == -1 {
		return nil
	}

	cmd := &api.ParsedCommand{
		PackageManager:          "cargo",
		Action:                  actionVerb,
		PreActionFlags:          preActionFlags,
		IsInstall:               true,
		RawCommand:              rawCommand,
		HasUnparsedArgs:         hasUnparsed,
		HasNonLocalUnparsedArgs: hasNonLocalUnparsed,
	}

	disqualify := hasUnparsed
	versionFlag := "" // captures --version value for cargo install
	for i := actionIdx + 1; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "-") {
			cmd.Flags = append(cmd.Flags, tok)
			if name, val, ok := parseutil.SplitLongFlagAssignment(tok); ok {
				if booleanFlags[name] {
					continue
				}
				if flagsWithValue[name] {
					if name == "--git" || name == "--registry" {
						disqualify = true
						cmd.HasUnparsedArgs = true
						cmd.HasNonLocalUnparsedArgs = true
						cmd.Packages = nil
					}
					if name == "--path" {
						disqualify = true
						cmd.HasUnparsedArgs = true
						cmd.Packages = nil
					}
					if name == "--version" {
						versionFlag = val
					}
					continue
				}
			}
			if booleanFlags[tok] {
				continue
			}
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				cmd.Flags = append(cmd.Flags, tokens[i])
				if tok == "--git" || tok == "--registry" {
					disqualify = true
					cmd.HasUnparsedArgs = true
					cmd.HasNonLocalUnparsedArgs = true
					cmd.Packages = nil
				}
				if tok == "--path" {
					disqualify = true
					cmd.HasUnparsedArgs = true
					cmd.Packages = nil
				}
				if tok == "--version" {
					versionFlag = tokens[i]
				}
				continue
			}
			if isUnknownLongFlag(tok) {
				cmd.HasUnparsedArgs = true
				cmd.HasNonLocalUnparsedArgs = true
				disqualify = true
				cmd.Packages = nil
			}
			continue
		}
		if disqualify {
			cmd.HasUnparsedArgs = true
			continue
		}
		if pkg, ok := parseSpec(tok); ok {
			cmd.Packages = append(cmd.Packages, pkg)
		} else {
			cmd.HasUnparsedArgs = true
			cmd.HasNonLocalUnparsedArgs = true
		}
	}

	// Apply --version flag to the package for cargo install.
	// Only safe when exactly one package is present; with 0 or >1 packages
	// the mapping is ambiguous so mark as unparsed to prevent bad rewrites.
	if versionFlag != "" {
		if len(cmd.Packages) == 1 && !cmd.Packages[0].Pinned {
			cmd.Packages[0].Version = versionFlag
			cmd.Packages[0].Pinned = true
		} else if len(cmd.Packages) != 1 {
			cmd.HasUnparsedArgs = true
			cmd.HasNonLocalUnparsedArgs = true
		}
	}

	return cmd
}

func isUnknownLongFlag(flag string) bool {
	if name, _, ok := parseutil.SplitLongFlagAssignment(flag); ok {
		flag = name
	}
	return strings.HasPrefix(flag, "--") &&
		!flagsWithValue[flag] &&
		!booleanFlags[flag]
}

func parseSpec(tok string) (api.PackageRequest, bool) {
	req := api.PackageRequest{
		Ecosystem: api.EcosystemCargo,
		RawSpec:   tok,
	}

	name, query, hasQuery := strings.Cut(tok, "@")
	if !hasQuery {
		req.Name = tok
		return req, tok != ""
	}
	if name == "" || query == "" {
		return api.PackageRequest{}, false
	}
	if !strings.HasPrefix(query, "=") || len(query) == 1 {
		return api.PackageRequest{}, false
	}
	req.Name = name
	req.Version = query[1:]
	req.Pinned = true
	return req, true
}
