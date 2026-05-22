// Package yarn parses safe Yarn add commands for Open Score evaluation.
package yarn

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/attach-dev/attach-guard/internal/parser/parseutil"
	"github.com/attach-dev/attach-guard/internal/parser/spec"
	"github.com/attach-dev/attach-guard/pkg/api"
)

var exactVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

var installAliases = map[string]bool{
	"add": true,
}

var flagsWithValue = map[string]bool{
	"--cache-folder":        true,
	"--cwd":                 true,
	"--mode":                true,
	"--mutex":               true,
	"--npm-registry-server": true,
	"--npmRegistryServer":   true,
	"--registry":            true,
	"--tag":                 true,
	"--use-yarnrc":          true,
}

var sourceFlagsWithValue = map[string]bool{
	"--cache-folder":        true,
	"--cwd":                 true,
	"--npm-registry-server": true,
	"--npmRegistryServer":   true,
	"--registry":            true,
	"--use-yarnrc":          true,
}

var sourceBooleanFlags = map[string]bool{
	"--cached":          true,
	"--check-cache":     true,
	"--immutable-cache": true,
	"--offline":         true,
	"--prefer-offline":  true,
}

var booleanFlags = map[string]bool{
	"-D":                            true,
	"--dev":                         true,
	"-E":                            true,
	"--exact":                       true,
	"-i":                            true,
	"--interactive":                 true,
	"-O":                            true,
	"--optional":                    true,
	"-P":                            true,
	"--peer":                        true,
	"-T":                            true,
	"--tilde":                       true,
	"-W":                            true,
	"--cached":                      true,
	"--check-cache":                 true,
	"--ignore-scripts":              true,
	"--ignore-workspace-root-check": true,
	"--immutable":                   true,
	"--immutable-cache":             true,
	"--json":                        true,
	"--offline":                     true,
	"--prefer-offline":              true,
	"--prod":                        true,
	"--production":                  true,
	"--refresh-lockfile":            true,
	"--silent":                      true,
	"--verbose":                     true,
}

// Parse attempts to parse direct Yarn add commands. Recognized commands return
// a ParsedCommand with zero packages when Yarn source cues make public-registry
// identity unsafe to request from Open Score.
func Parse(tokens []string, rawCommand string) *api.ParsedCommand {
	if len(tokens) < 2 {
		return nil
	}
	if filepath.Base(tokens[0]) != "yarn" {
		return nil
	}

	var preActionFlags []string
	actionIdx := -1
	hasUnparsed := false
	hasNonLocalUnparsed := false

	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if installAliases[tok] {
			actionIdx = i
			break
		}
		if strings.HasPrefix(tok, "-") {
			preActionFlags = append(preActionFlags, tok)
			next, unsafeSource, unparsed := consumeFlag(tokens, i, "add", "workspace", "global")
			if next > i {
				preActionFlags = append(preActionFlags, tokens[i+1:next+1]...)
			}
			if unsafeSource {
				hasNonLocalUnparsed = true
			}
			if unparsed {
				hasUnparsed = true
			}
			i = next
			continue
		}
		if tok == "workspace" {
			return deferredSubcommandAdd(tokens, rawCommand, preActionFlags, i, 2)
		}
		if tok == "global" {
			return deferredSubcommandAdd(tokens, rawCommand, preActionFlags, i, 1)
		}
		return nil
	}
	if actionIdx == -1 {
		return nil
	}

	cmd := &api.ParsedCommand{
		PackageManager:          "yarn",
		Action:                  tokens[actionIdx],
		PreActionFlags:          preActionFlags,
		IsInstall:               true,
		RawCommand:              rawCommand,
		HasUnparsedArgs:         hasUnparsed,
		HasNonLocalUnparsedArgs: hasNonLocalUnparsed,
	}

	disqualify := hasNonLocalUnparsed
	for i := actionIdx + 1; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "-") {
			cmd.Flags = append(cmd.Flags, tok)
			next, unsafeSource, unparsed := consumeFlag(tokens, i)
			if next > i {
				cmd.Flags = append(cmd.Flags, tokens[i+1:next+1]...)
			}
			if unsafeSource {
				disqualify = true
				cmd.Packages = nil
				cmd.HasNonLocalUnparsedArgs = true
			}
			if unparsed {
				cmd.HasUnparsedArgs = true
			}
			i = next
			continue
		}

		if disqualify {
			cmd.HasUnparsedArgs = true
			continue
		}
		if yarnSpecIsCustomSource(tok) {
			cmd.Packages = nil
			cmd.HasUnparsedArgs = true
			cmd.HasNonLocalUnparsedArgs = true
			disqualify = true
			continue
		}

		pkg := spec.ParsePackageSpec(tok)
		if !spec.IsPublicNPMPackageName(pkg.Name) {
			cmd.Packages = nil
			cmd.HasUnparsedArgs = true
			cmd.HasNonLocalUnparsedArgs = true
			disqualify = true
			continue
		}
		pkg.Ecosystem = api.EcosystemNPM
		pkg.Pinned = exactVersionPattern.MatchString(pkg.Version)
		cmd.Packages = append(cmd.Packages, pkg)
	}

	if len(cmd.Packages) == 0 && !cmd.HasUnparsedArgs {
		return nil
	}
	return cmd
}

func deferredSubcommandAdd(tokens []string, rawCommand string, preActionFlags []string, subcommandIdx, actionOffset int) *api.ParsedCommand {
	actionIdx := subcommandIdx + actionOffset
	if actionIdx >= len(tokens) || !installAliases[tokens[actionIdx]] {
		return nil
	}

	return &api.ParsedCommand{
		PackageManager:          "yarn",
		Action:                  tokens[actionIdx],
		PreActionFlags:          preActionFlags,
		IsInstall:               true,
		RawCommand:              rawCommand,
		HasUnparsedArgs:         true,
		HasNonLocalUnparsedArgs: true,
	}
}

func consumeFlag(tokens []string, idx int, stopAts ...string) (next int, unsafeSource bool, unparsed bool) {
	tok := tokens[idx]
	if name, _, ok := parseutil.SplitLongFlagAssignment(tok); ok {
		if sourceFlagsWithValue[name] {
			return idx, true, true
		}
		if flagsWithValue[name] || booleanFlags[name] {
			return idx, false, false
		}
		return idx, true, true
	}

	if sourceFlagsWithValue[tok] {
		next := idx
		if idx+1 < len(tokens) {
			next = idx + 1
		}
		return next, true, true
	}
	if sourceBooleanFlags[tok] {
		return idx, true, true
	}
	if flagsWithValue[tok] {
		next := idx
		if idx+1 < len(tokens) {
			next = idx + 1
		}
		return next, false, next == idx
	}
	if booleanFlags[tok] {
		return idx, false, false
	}
	if parseutil.ShouldConsumeUnknownLongFlagValue(tok, tokens, idx, stopAts...) {
		return idx + 1, true, true
	}
	if strings.HasPrefix(tok, "-") {
		return idx, true, true
	}
	return idx, false, false
}

func yarnSpecIsCustomSource(raw string) bool {
	if raw == "" {
		return true
	}

	lower := strings.ToLower(raw)
	if raw == "." || raw == ".." ||
		strings.HasPrefix(raw, "./") ||
		strings.HasPrefix(raw, "../") ||
		strings.HasPrefix(raw, "/") ||
		strings.HasPrefix(raw, "~/") {
		return true
	}
	if strings.HasPrefix(lower, "git+") ||
		strings.HasPrefix(lower, "github:") ||
		strings.HasPrefix(lower, "gitlab:") ||
		strings.HasPrefix(lower, "bitbucket:") {
		return true
	}
	if strings.Contains(lower, "://") {
		return true
	}
	if strings.Contains(lower, "@npm:") {
		return true
	}
	if strings.Contains(raw, ":") {
		return true
	}
	for _, suffix := range []string{".tgz", ".tar.gz", ".tar.bz2", ".tar.xz", ".zip"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}
