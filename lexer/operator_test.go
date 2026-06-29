package lexer

import (
	"testing"

	"domainscript/token"
)

func TestSingleCharOperators(t *testing.T) {
	assertTokens(t, "{}()[],.:+-*/=<>", []want{
		{token.LBRACE, ""},
		{token.RBRACE, ""},
		{token.LPAREN, ""},
		{token.RPAREN, ""},
		{token.LBRACK, ""},
		{token.RBRACK, ""},
		{token.COMMA, ""},
		{token.DOT, ""},
		{token.COLON, ""},
		{token.PLUS, ""},
		{token.MINUS, ""},
		{token.STAR, ""},
		{token.SLASH, ""},
		{token.ASSIGN, ""},
		{token.LT, ""},
		{token.GT, ""},
		{token.EOF, ""},
	})
}

func TestTwoCharOperators(t *testing.T) {
	assertTokens(t, "-> == != <= >=", []want{
		{token.ARROW, ""},
		{token.EQ, ""},
		{token.NEQ, ""},
		{token.LE, ""},
		{token.GE, ""},
		{token.EOF, ""},
	})
}

// Garante a desambiguação entre operadores de 1 e 2 caracteres adjacentes a
// outros tokens.
func TestOperatorBoundaries(t *testing.T) {
	assertTokens(t, "a->b a-b a==b a=b a<=b a<b", []want{
		{token.IDENT, "a"}, {token.ARROW, ""}, {token.IDENT, "b"},
		{token.IDENT, "a"}, {token.MINUS, ""}, {token.IDENT, "b"},
		{token.IDENT, "a"}, {token.EQ, ""}, {token.IDENT, "b"},
		{token.IDENT, "a"}, {token.ASSIGN, ""}, {token.IDENT, "b"},
		{token.IDENT, "a"}, {token.LE, ""}, {token.IDENT, "b"},
		{token.IDENT, "a"}, {token.LT, ""}, {token.IDENT, "b"},
		{token.EOF, ""},
	})
}

// '.' é acesso a membro quando não há dígito após um número (REQ-1.2).
func TestMemberAccessVsFloat(t *testing.T) {
	assertTokens(t, "foo.bar 3.x", []want{
		{token.IDENT, "foo"},
		{token.DOT, ""},
		{token.IDENT, "bar"},
		{token.INT, "3"},
		{token.DOT, ""},
		{token.IDENT, "x"},
		{token.EOF, ""},
	})
}

// SLASH simples é divisão; "//" é comentário (não gera token).
func TestSlashVsComment(t *testing.T) {
	assertTokens(t, "a / b // resto", []want{
		{token.IDENT, "a"},
		{token.SLASH, ""},
		{token.IDENT, "b"},
		{token.EOF, ""},
	})
}
