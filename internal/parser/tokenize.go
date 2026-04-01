package parser

import "strings"

// Tokenize splits a command string into tokens, respecting basic quoting.
// Shell operators (&&, ||, ;, |) and newlines are emitted as separate tokens
// even when not surrounded by whitespace, so "ls&&npm install" correctly
// produces ["ls", "&&", "npm", "install"].
func Tokenize(cmd string) []string {
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

		default:
			current.WriteRune(r)
		}
	}

	flush()
	return tokens
}
