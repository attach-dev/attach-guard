// Package pip parses scoped pip install commands.
package pip

import (
	"path/filepath"
	"strings"

	"github.com/attach-dev/attach-guard/pkg/api"
)

var flagsWithValue = map[string]bool{
	"-i":                true,
	"--index-url":       true,
	"--extra-index-url": true,
	"-c":                true,
	"--constraint":      true,
	"-f":                true,
	"--find-links":      true,
	"--no-binary":       true,
	"--only-binary":     true,
	"--platform":        true,
	"--python-version":  true,
	"--implementation":  true,
	"--abi":             true,
	"-r":                true,
	"--requirement":     true,
	"-t":                true,
	"--target":          true,
	"--root":            true,
	"--prefix":          true,
}

var unparsedValueFlags = map[string]bool{
	"-i":                true,
	"--index-url":       true,
	"--extra-index-url": true,
	"-c":                true,
	"--constraint":      true,
	"-f":                true,
	"--find-links":      true,
	"-r":                true,
	"--requirement":     true,
}

var rangeOperators = []string{">=", "~=", "!=", "<=", ">", "<"}

// Parse attempts to parse direct pip/pip3 install commands.
// Unlike npm/pnpm parsing, recognized commands may return a ParsedCommand with
// zero evaluable packages when all positional args were skipped as unsupported.
func Parse(tokens []string, rawCommand string) *api.ParsedCommand {
	if len(tokens) < 2 {
		return nil
	}

	base := filepath.Base(tokens[0])
	if base != "pip" && base != "pip3" {
		return nil
	}

	var preActionFlags []string
	actionIdx := -1
	hasUnparsed := false

	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "install" {
			actionIdx = i
			break
		}
		if strings.HasPrefix(tok, "-") {
			preActionFlags = append(preActionFlags, tok)
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				preActionFlags = append(preActionFlags, tokens[i])
				continue
			}
			if shouldConsumeUnknownLongFlagValue(tok, tokens, i, "install") {
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
		PackageManager:  base,
		Action:          "install",
		PreActionFlags:  preActionFlags,
		IsInstall:       true,
		RawCommand:      rawCommand,
		HasUnparsedArgs: hasUnparsed,
	}

	for i := actionIdx + 1; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "-") {
			cmd.Flags = append(cmd.Flags, tok)
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				cmd.Flags = append(cmd.Flags, tokens[i])
				if unparsedValueFlags[tok] {
					cmd.HasUnparsedArgs = true
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
		if shouldSkipArg(tok) {
			cmd.HasUnparsedArgs = true
			continue
		}
		cmd.Packages = append(cmd.Packages, parseSpec(tok))
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

func shouldSkipArg(tok string) bool {
	if strings.HasPrefix(tok, ".") || strings.HasPrefix(tok, "/") ||
		strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://") {
		return true
	}
	if strings.Contains(tok, "[") {
		return true
	}
	for _, op := range rangeOperators {
		if strings.Contains(tok, op) {
			return true
		}
	}
	return false
}

func parseSpec(tok string) api.PackageRequest {
	req := api.PackageRequest{
		Ecosystem: api.EcosystemPyPI,
		RawSpec:   tok,
	}
	if name, version, ok := strings.Cut(tok, "=="); ok {
		req.Name = name
		req.Version = version
		req.Pinned = name != "" && version != ""
		return req
	}
	req.Name = tok
	return req
}
