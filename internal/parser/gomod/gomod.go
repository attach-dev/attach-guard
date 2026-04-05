// Package gomod parses scoped go get commands.
package gomod

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/attach-dev/attach-guard/pkg/api"
)

var semverPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[\w.]+)?(\+[\w.]+)?$`)

// Parse attempts to parse direct go get commands.
// Unlike npm/pnpm parsing, recognized commands may return a ParsedCommand with
// zero evaluable packages when all positional args were skipped as unsupported.
func Parse(tokens []string, rawCommand string) *api.ParsedCommand {
	if len(tokens) < 2 {
		return nil
	}

	if filepath.Base(tokens[0]) != "go" {
		return nil
	}

	var preActionFlags []string
	actionIdx := -1
	actionVerb := ""
	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "get" || tok == "install" {
			actionIdx = i
			actionVerb = tok
			break
		}
		if strings.HasPrefix(tok, "-") {
			preActionFlags = append(preActionFlags, tok)
			if tok == "-C" && i+1 < len(tokens) {
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
		PackageManager: "go",
		Action:         actionVerb,
		PreActionFlags: preActionFlags,
		IsInstall:      true,
		RawCommand:     rawCommand,
	}

	for i := actionIdx + 1; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "-") {
			cmd.Flags = append(cmd.Flags, tok)
			continue
		}
		if tok == "." || tok == ".." || strings.HasPrefix(tok, "./") || strings.HasPrefix(tok, "../") || strings.HasPrefix(tok, "/") {
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

	return cmd
}

func parseSpec(tok string) (api.PackageRequest, bool) {
	req := api.PackageRequest{
		Ecosystem: api.EcosystemGo,
		RawSpec:   tok,
	}

	name, query, hasQuery := strings.Cut(tok, "@")
	if !hasQuery {
		req.Name = tok
		return req, tok != ""
	}
	if name == "" {
		return api.PackageRequest{}, false
	}

	switch {
	case query == "", query == "latest":
		req.Name = name
		return req, true
	case semverPattern.MatchString(query):
		req.Name = name
		req.Version = query
		req.Pinned = true
		return req, true
	default:
		return api.PackageRequest{}, false
	}
}
