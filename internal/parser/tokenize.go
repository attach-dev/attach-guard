package parser

import "strings"

const (
	activeCommandSubstitutionPrefix = "\x1f$("
	activeCommandSubstitutionSuffix = ")\x1e"
)

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
	runes := []rune(cmd)

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
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
			escaped = true

		case r == '\'' && !inDouble:
			inSingle = !inSingle

		case r == '"' && !inSingle:
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

		case inSingle || inDouble:
			current.WriteRune(r)

		case r == ' ' || r == '\t':
			flush()

		case r == '\n':
			// Newline is a command separator in shell, like ;
			flush()
			tokens = append(tokens, ";")

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
			if current.Len() > 0 && i+1 < len(runes) && (runes[i+1] == '=' || isDigit(runes[i+1])) {
				current.WriteRune(r)
				continue
			}
			if isDigits(current.String()) {
				fd := current.String()
				current.Reset()
				op, end := readRedirectionOperator(runes, i)
				tokens = append(tokens, fd+op)
				i = end
				continue
			}
			flush()
			op, end := readRedirectionOperator(runes, i)
			tokens = append(tokens, op)
			i = end

		default:
			current.WriteRune(r)
		}
	}

	flush()
	return tokens
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
