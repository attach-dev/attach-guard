package parser

import (
	"strings"

	"github.com/attach-dev/attach-guard/pkg/api"
)

const dynamicShellArg = "__attach_guard_dynamic_shell_arg__"

type shellTokenInfo struct {
	tokens                 []string
	hadRedirection         bool
	hadCommandSubstitution bool
}

func prepareParserTokens(tokens []string) shellTokenInfo {
	info := stripRedirectionTokens(tokens)
	if len(info.tokens) == 0 {
		return info
	}

	prepared := make([]string, 0, len(info.tokens))
	for _, tok := range info.tokens {
		if hasActiveCommandSubstitution(tok) {
			info.hadCommandSubstitution = true
			prepared = append(prepared, dynamicShellArg)
			continue
		}
		prepared = append(prepared, tok)
	}
	info.tokens = prepared
	return info
}

func stripRedirectionTokens(tokens []string) shellTokenInfo {
	info := shellTokenInfo{tokens: make([]string, 0, len(tokens))}
	for i := 0; i < len(tokens); i++ {
		if isRedirectionOperator(tokens[i]) {
			info.hadRedirection = true
			if i+1 < len(tokens) {
				i++
			}
			continue
		}
		info.tokens = append(info.tokens, tokens[i])
	}
	return info
}

func applyShellTokenInfo(cmd *api.ParsedCommand, info shellTokenInfo) {
	if info.hadRedirection {
		cmd.HasUnparsedArgs = true
	}
	if info.hadCommandSubstitution {
		cmd.Packages = nil
		cmd.HasUnparsedArgs = true
		cmd.HasNonLocalUnparsedArgs = true
	}
}

func isRedirectionOperator(tok string) bool {
	if tok == "" {
		return false
	}
	op := trimRedirectionFD(tok)
	switch op {
	case ">", ">>", ">|", ">&", "<", "<<", "<<-", "<<<", "<&", "<>", "&>", "&>>":
		return true
	default:
		return false
	}
}

func trimRedirectionFD(tok string) string {
	if strings.HasPrefix(tok, "&") {
		return tok
	}
	i := 0
	for i < len(tok) && tok[i] >= '0' && tok[i] <= '9' {
		i++
	}
	if i > 0 && i < len(tok) {
		return tok[i:]
	}
	return tok
}

func hasActiveCommandSubstitution(tok string) bool {
	return strings.Contains(tok, activeCommandSubstitutionPrefix)
}

func activeCommandSubstitutions(tok string) []string {
	var subs []string
	rest := tok
	for {
		start := strings.Index(rest, activeCommandSubstitutionPrefix)
		if start == -1 {
			return subs
		}
		afterStart := rest[start+len(activeCommandSubstitutionPrefix):]
		end := strings.Index(afterStart, activeCommandSubstitutionSuffix)
		if end == -1 {
			return subs
		}
		subs = append(subs, afterStart[:end])
		rest = afterStart[end+len(activeCommandSubstitutionSuffix):]
	}
}
