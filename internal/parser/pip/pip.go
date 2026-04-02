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

var sourceValueFlags = map[string]bool{
	"-i":                true,
	"--index-url":       true,
	"--extra-index-url": true,
	"-f":                true,
	"--find-links":      true,
}

var booleanFlags = map[string]bool{
	"-U":                true,
	"--upgrade":         true,
	"-e":                true,
	"--editable":        true,
	"--no-deps":         true,
	"--user":            true,
	"--force-reinstall": true,
	"--no-cache-dir":    true,
}

var nonLocalBooleanFlags = map[string]bool{
	"--no-index": true,
	"--pre":      true,
}

var unparsedValueFlags = map[string]bool{
	"-c":            true,
	"--constraint":  true,
	"-r":            true,
	"--requirement": true,
}

var rangeOperators = []string{">=", "~=", "!=", "<=", ">", "<"}

var localArchiveSuffixes = []string{
	".whl",
	".zip",
	".tar.gz",
	".tgz",
	".tar.bz2",
	".tar.xz",
}

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
	hasNonLocalUnparsed := false
	disqualify := false

	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "install" {
			actionIdx = i
			break
		}
		if strings.HasPrefix(tok, "-") {
			preActionFlags = append(preActionFlags, tok)
			if booleanFlags[tok] {
				continue
			}
			if nonLocalBooleanFlags[tok] {
				hasUnparsed = true
				hasNonLocalUnparsed = true
				disqualify = true
				continue
			}
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				preActionFlags = append(preActionFlags, tokens[i])
				if sourceValueFlags[tok] {
					hasUnparsed = true
					hasNonLocalUnparsed = true
					disqualify = true
				}
				continue
			}
			if isUnknownLongFlag(tok) {
				hasUnparsed = true
				hasNonLocalUnparsed = true
				disqualify = true
			}
			continue
		}
		return nil
	}

	if actionIdx == -1 {
		return nil
	}

	cmd := &api.ParsedCommand{
		PackageManager:          base,
		Action:                  "install",
		PreActionFlags:          preActionFlags,
		IsInstall:               true,
		RawCommand:              rawCommand,
		HasUnparsedArgs:         hasUnparsed,
		HasNonLocalUnparsedArgs: hasNonLocalUnparsed,
	}

	for i := actionIdx + 1; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "-") {
			cmd.Flags = append(cmd.Flags, tok)
			if booleanFlags[tok] {
				continue
			}
			if nonLocalBooleanFlags[tok] {
				disqualify = true
				cmd.HasUnparsedArgs = true
				cmd.HasNonLocalUnparsedArgs = true
				cmd.Packages = nil
				continue
			}
			if flagsWithValue[tok] && i+1 < len(tokens) {
				i++
				cmd.Flags = append(cmd.Flags, tokens[i])
				if sourceValueFlags[tok] {
					disqualify = true
					cmd.HasUnparsedArgs = true
					cmd.HasNonLocalUnparsedArgs = true
					cmd.Packages = nil
				}
				if unparsedValueFlags[tok] {
					cmd.HasUnparsedArgs = true
					cmd.HasNonLocalUnparsedArgs = true
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
		if local, nonLocal := classifySkippedArg(tok); local || nonLocal {
			cmd.HasUnparsedArgs = true
			if nonLocal {
				cmd.HasNonLocalUnparsedArgs = true
			}
			continue
		}
		cmd.Packages = append(cmd.Packages, parseSpec(tok))
	}

	return cmd
}

func isUnknownLongFlag(flag string) bool {
	return strings.HasPrefix(flag, "--") &&
		!strings.Contains(flag, "=") &&
		!flagsWithValue[flag] &&
		!booleanFlags[flag] &&
		!nonLocalBooleanFlags[flag]
}

func classifySkippedArg(tok string) (local bool, nonLocal bool) {
	if strings.HasPrefix(tok, "file://") {
		return true, false
	}
	if strings.Contains(tok, "://") {
		if strings.Contains(tok, "+file://") {
			return true, false
		}
		return false, true
	}
	if strings.HasPrefix(tok, ".") || strings.HasPrefix(tok, "/") {
		return true, false
	}
	if strings.Contains(tok, "/") {
		return true, false
	}
	for _, suffix := range localArchiveSuffixes {
		if strings.HasSuffix(tok, suffix) {
			return true, false
		}
	}
	if strings.Contains(tok, "[") {
		return false, true
	}
	for _, op := range rangeOperators {
		if strings.Contains(tok, op) {
			return false, true
		}
	}
	return false, false
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
