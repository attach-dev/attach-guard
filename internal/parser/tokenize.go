package parser

import "strings"

const (
	activeCommandSubstitutionPrefix = "\x1f$("
	activeCommandSubstitutionSuffix = ")\x1e"
)

type pendingHereDoc struct {
	delimiter string
	stripTabs bool
	expand    bool
}

// Tokenize splits a command string into tokens, respecting basic quoting.
// Shell operators (&&, ||, ;, |), redirections, and newlines are emitted as
// separate tokens even when not surrounded by whitespace, so
// "ls&&npm install" correctly produces ["ls", "&&", "npm", "install"].
func Tokenize(cmd string) []string {
	return tokenize(cmd, false)
}

func tokenizeForParse(cmd string) []string {
	return tokenize(cmd, true)
}

func tokenize(cmd string, markActiveCommandSubstitutions bool) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	currentHadQuote := false
	expectHereDocDelimiter := false
	nextHereDocStripTabs := false
	var pendingHereDocs []pendingHereDoc
	runes := []rune(cmd)

	flush := func() {
		if current.Len() > 0 {
			tok := current.String()
			tokens = append(tokens, tok)
			if expectHereDocDelimiter {
				pendingHereDocs = append(pendingHereDocs, pendingHereDoc{
					delimiter: tok,
					stripTabs: nextHereDocStripTabs,
					expand:    !currentHadQuote,
				})
				expectHereDocDelimiter = false
				nextHereDocStripTabs = false
			}
			current.Reset()
			currentHadQuote = false
		}
	}

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		switch {
		case r == '\\' && !inSingle:
			if expectHereDocDelimiter {
				currentHadQuote = true
			}
			escaped = true

		case r == '\'' && !inDouble:
			currentHadQuote = true
			inSingle = !inSingle

		case r == '"' && !inSingle:
			currentHadQuote = true
			inDouble = !inDouble

		case r == '$' && !inSingle && i+1 < len(runes) && runes[i+1] == '(':
			if end, inner, ok := scanCommandSubstitution(runes, i); ok {
				if markActiveCommandSubstitutions {
					current.WriteString(activeCommandSubstitutionPrefix)
					current.WriteString(inner)
					current.WriteString(activeCommandSubstitutionSuffix)
				} else {
					current.WriteString("$(")
					current.WriteString(inner)
					current.WriteRune(')')
				}
				i = end
				continue
			}
			current.WriteRune(r)

		case r == '`' && !inSingle:
			if end, inner, ok := scanBacktickCommandSubstitution(runes, i); ok {
				if markActiveCommandSubstitutions {
					current.WriteString(activeCommandSubstitutionPrefix)
					current.WriteString(inner)
					current.WriteString(activeCommandSubstitutionSuffix)
				} else {
					current.WriteRune('`')
					current.WriteString(inner)
					current.WriteRune('`')
				}
				i = end
				continue
			}
			current.WriteRune(r)

		case inSingle || inDouble:
			current.WriteRune(r)

		case r == ' ' || r == '\t':
			flush()

		case r == '\n':
			// Newline is a command separator in shell, like ;
			flush()
			tokens = append(tokens, ";")
			if len(pendingHereDocs) > 0 {
				var hereDocSubstitutions []string
				i, hereDocSubstitutions = skipHereDocBodies(runes, i+1, pendingHereDocs, markActiveCommandSubstitutions)
				tokens = append(tokens, hereDocSubstitutions...)
				if len(hereDocSubstitutions) > 0 {
					tokens = append(tokens, ";")
				}
				pendingHereDocs = nil
			}

		case r == ';':
			flush()
			tokens = append(tokens, ";")

		case r == '&':
			flush()
			if i+1 < len(runes) && runes[i+1] == '&' {
				tokens = append(tokens, "&&")
				i++
			} else if i+1 < len(runes) && runes[i+1] == '>' {
				if i+2 < len(runes) && runes[i+2] == '>' {
					tokens = append(tokens, "&>>")
					i += 2
				} else {
					tokens = append(tokens, "&>")
					i++
				}
			} else {
				// Single & (background) — emit as operator token
				tokens = append(tokens, "&")
			}

		case r == '|':
			flush()
			if i+1 < len(runes) && runes[i+1] == '|' {
				tokens = append(tokens, "||")
				i++
			} else {
				tokens = append(tokens, "|")
			}

		case r == '>' || r == '<':
			if isDigits(current.String()) {
				fd := current.String()
				current.Reset()
				op, end := readRedirectionOperator(runes, i)
				opToken := fd + op
				tokens = append(tokens, opToken)
				if stripTabs, ok := hereDocOperator(opToken); ok {
					expectHereDocDelimiter = true
					nextHereDocStripTabs = stripTabs
				}
				i = end
				continue
			}
			flush()
			op, end := readRedirectionOperator(runes, i)
			tokens = append(tokens, op)
			if stripTabs, ok := hereDocOperator(op); ok {
				expectHereDocDelimiter = true
				nextHereDocStripTabs = stripTabs
			}
			i = end

		default:
			current.WriteRune(r)
		}
	}

	flush()
	return tokens
}

func skipHereDocBodies(runes []rune, start int, docs []pendingHereDoc, markActiveCommandSubstitutions bool) (int, []string) {
	pos := start
	var substitutions []string
	for _, doc := range docs {
		for pos < len(runes) {
			lineStart := pos
			lineEnd := lineStart
			for lineEnd < len(runes) && runes[lineEnd] != '\n' {
				lineEnd++
			}

			lineRunes := runes[lineStart:lineEnd]
			line := string(lineRunes)
			compareLine := line
			if doc.stripTabs {
				compareLine = strings.TrimLeft(compareLine, "\t")
			}
			if compareLine == doc.delimiter {
				if lineEnd < len(runes) && runes[lineEnd] == '\n' {
					pos = lineEnd + 1
				} else {
					pos = lineEnd
				}
				break
			}

			if markActiveCommandSubstitutions && doc.expand {
				substitutions = append(substitutions, activeSubstitutionTokensInRunes(lineRunes)...)
			}

			if lineEnd >= len(runes) {
				return len(runes), substitutions
			}
			pos = lineEnd + 1
		}
	}
	return pos - 1, substitutions
}

func activeSubstitutionTokensInRunes(runes []rune) []string {
	var tokens []string
	for i := 0; i < len(runes); i++ {
		if runes[i] == '$' && i+1 < len(runes) && runes[i+1] == '(' {
			if isEscapedHereDocDollar(runes, i) {
				continue
			}
			end, inner, ok := scanCommandSubstitution(runes, i)
			if !ok {
				continue
			}
			tokens = append(tokens, activeCommandSubstitutionPrefix+inner+activeCommandSubstitutionSuffix)
			i = end
			continue
		}
		if runes[i] == '`' {
			if isEscapedHereDocBacktick(runes, i) {
				continue
			}
			end, inner, ok := scanBacktickCommandSubstitution(runes, i)
			if !ok {
				continue
			}
			tokens = append(tokens, activeCommandSubstitutionPrefix+inner+activeCommandSubstitutionSuffix)
			i = end
		}
	}
	return tokens
}

func isEscapedHereDocDollar(runes []rune, dollarIndex int) bool {
	backslashes := 0
	for i := dollarIndex - 1; i >= 0 && runes[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func isEscapedHereDocBacktick(runes []rune, backtickIndex int) bool {
	backslashes := 0
	for i := backtickIndex - 1; i >= 0 && runes[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func hereDocOperator(tok string) (stripTabs bool, ok bool) {
	switch trimRedirectionFD(tok) {
	case "<<":
		return false, true
	case "<<-":
		return true, true
	default:
		return false, false
	}
}

func scanCommandSubstitution(runes []rune, start int) (end int, inner string, ok bool) {
	if start+1 >= len(runes) || runes[start] != '$' || runes[start+1] != '(' {
		return 0, "", false
	}

	depth := 1
	inSingle := false
	inDouble := false
	escaped := false
	for i := start + 2; i < len(runes); i++ {
		r := runes[i]

		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		if r == '$' && i+1 < len(runes) && runes[i+1] == '(' {
			depth++
			i++
			continue
		}
		if r == ')' {
			depth--
			if depth == 0 {
				return i, string(runes[start+2 : i]), true
			}
		}
	}

	return 0, "", false
}

func scanBacktickCommandSubstitution(runes []rune, start int) (end int, inner string, ok bool) {
	if start >= len(runes) || runes[start] != '`' {
		return 0, "", false
	}
	escaped := false
	for i := start + 1; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '`' {
			return i, string(runes[start+1 : i]), true
		}
	}
	return 0, "", false
}

func readRedirectionOperator(runes []rune, start int) (op string, end int) {
	r := runes[start]
	if r == '>' {
		if start+1 < len(runes) {
			switch runes[start+1] {
			case '>', '&', '|':
				return string([]rune{r, runes[start+1]}), start + 1
			}
		}
		return ">", start
	}

	if r == '<' {
		if start+1 < len(runes) {
			switch runes[start+1] {
			case '<':
				if start+2 < len(runes) && runes[start+2] == '<' {
					return "<<<", start + 2
				}
				if start+2 < len(runes) && runes[start+2] == '-' {
					return "<<-", start + 2
				}
				return "<<", start + 1
			case '&', '>':
				return string([]rune{r, runes[start+1]}), start + 1
			}
		}
		return "<", start
	}

	return string(r), start
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isDigit(r) {
			return false
		}
	}
	return true
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
