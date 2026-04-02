// Package cargo parses scoped cargo add commands.
package cargo

import (
	"path/filepath"
	"strings"

	"github.com/attach-dev/attach-guard/pkg/api"
)

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
	hasUnparsed := false
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "add" {
			actionIdx = i
			break
		}
		if strings.HasPrefix(tok, "-") {
			preActionFlags = append(preActionFlags, tok)
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				preActionFlags = append(preActionFlags, tokens[i])
				if tok == "--git" || tok == "--path" || tok == "--registry" {
					hasUnparsed = true
				}
				continue
			}
			if shouldConsumeUnknownLongFlagValue(tok, tokens, i, "add") {
				hasUnparsed = true
				i++
				preActionFlags = append(preActionFlags, tokens[i])
			}
			continue
		}
		return nil
	}
	if actionIdx == -1 {
		return nil
	}

	cmd := &api.ParsedCommand{
		PackageManager:  "cargo",
		Action:          "add",
		PreActionFlags:  preActionFlags,
		IsInstall:       true,
		RawCommand:      rawCommand,
		HasUnparsedArgs: hasUnparsed,
	}

	disqualify := hasUnparsed
	for i := actionIdx + 1; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "-") {
			cmd.Flags = append(cmd.Flags, tok)
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				cmd.Flags = append(cmd.Flags, tokens[i])
				if tok == "--git" || tok == "--path" || tok == "--registry" {
					disqualify = true
					cmd.HasUnparsedArgs = true
					cmd.Packages = nil
				}
				continue
			}
			if shouldConsumeUnknownLongFlagValue(tok, tokens, i, "") {
				cmd.HasUnparsedArgs = true
				i++
				cmd.Flags = append(cmd.Flags, tokens[i])
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
		}
	}

	return cmd
}

func shouldConsumeUnknownLongFlagValue(flag string, tokens []string, idx int, stopAt string) bool {
	if !strings.HasPrefix(flag, "--") || strings.Contains(flag, "=") || idx+1 >= len(tokens) {
		return false
	}
	next := tokens[idx+1]
	if next == stopAt {
		return false
	}
	return !strings.HasPrefix(next, "-")
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
