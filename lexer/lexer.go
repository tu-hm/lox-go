package lexer

import (
	"compiler101/lexer/scanner"
	"compiler101/lexer/token"
)

// Lex turns source text into a token stream. The returned slice always ends
// with an EOF token, which the parser relies on as a sentinel.
func Lex(source string) []token.Token {
	return scanner.NewScanner(source).ScanToken()
}
