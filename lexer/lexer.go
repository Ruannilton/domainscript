package lexer

import (
	"fmt"
	"strings"
	"unicode"

	"domainscript/diag"
	"domainscript/token"
)

// Lex converte o source numa sequência de tokens terminada por EOF (REQ-1.1),
// mais os diagnósticos léxicos encontrados. Faz uma única passagem sobre os
// runes da entrada e nunca trava: todo caminho do laço consome ao menos um rune
// (REQ-1, NFR-2). Os diagnósticos retornados são integrados ao DiagnosticBag
// compartilhado pelo driver.
func Lex(src string) ([]token.Token, []diag.Diagnostic) {
	l := &lexer{src: []rune(src), line: 1, col: 1}
	l.run()
	return l.toks, l.diags
}

type lexer struct {
	src  []rune
	pos  int // índice do próximo rune a consumir
	line int // linha 1-based do próximo rune
	col  int // coluna 1-based do próximo rune

	toks  []token.Token
	diags []diag.Diagnostic
}

func (l *lexer) run() {
	for {
		l.skipTrivia()
		pos := l.here()
		if l.atEnd() {
			l.emit(token.EOF, "", pos)
			return
		}
		switch r := l.peek(); {
		case isIdentStart(r):
			l.lexIdentOrKeyword(pos)
		case isDigit(r):
			l.lexNumber(pos)
		case r == '"':
			l.lexString(pos)
		default:
			bad := l.advance() // progresso garantido (REQ-1.6, NFR-2)
			l.errorf(pos, "caractere inválido %q", bad)
		}
	}
}

// skipTrivia descarta espaços em branco e comentários de linha (REQ-1.5).
func (l *lexer) skipTrivia() {
	for !l.atEnd() {
		switch r := l.peek(); {
		case r == ' ', r == '\t', r == '\r', r == '\n':
			l.advance()
		case r == '/' && l.peek2() == '/':
			for !l.atEnd() && l.peek() != '\n' {
				l.advance()
			}
		default:
			return
		}
	}
}

// lexIdentOrKeyword consome um identificador e o classifica como keyword ou IDENT.
func (l *lexer) lexIdentOrKeyword(start token.Pos) {
	text := l.takeWhile(isIdentPart)
	if kind := token.Lookup(text); kind != token.IDENT {
		l.emit(kind, "", start) // keyword: o Kind já carrega o texto
		return
	}
	l.emit(token.IDENT, text, start)
}

// lexNumber consome um literal inteiro ou decimal. O '.' só inicia a parte
// fracionária se houver dígito logo após, para não engolir o ponto de acesso a
// membro em "foo.bar" / "3.field" (REQ-1.2, §design 3.2).
func (l *lexer) lexNumber(start token.Pos) {
	lit := l.takeWhile(isDigit)
	kind := token.INT
	if l.peek() == '.' && isDigit(l.peek2()) {
		l.advance() // '.'
		lit = lit + "." + l.takeWhile(isDigit)
		kind = token.FLOAT
	}
	l.emit(kind, lit, start)
}

// lexString consome um literal de string entre aspas. Nesta fase trata apenas
// strings terminadas na mesma linha; escapes e detecção de não-terminada chegam
// na tarefa 2.3.
func (l *lexer) lexString(start token.Pos) {
	l.advance() // aspas de abertura
	var sb strings.Builder
	for !l.atEnd() && l.peek() != '"' && l.peek() != '\n' {
		sb.WriteRune(l.advance())
	}
	if l.peek() == '"' {
		l.advance()
	}
	l.emit(token.STRING, sb.String(), start)
}

// --- cursor de runes ---

func (l *lexer) here() token.Pos { return token.Pos{Line: l.line, Col: l.col} }

func (l *lexer) atEnd() bool { return l.pos >= len(l.src) }

func (l *lexer) peek() rune  { return l.peekAt(0) }
func (l *lexer) peek2() rune { return l.peekAt(1) }

func (l *lexer) peekAt(off int) rune {
	i := l.pos + off
	if i < 0 || i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

func (l *lexer) advance() rune {
	r := l.src[l.pos]
	l.pos++
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

// takeWhile consome runes enquanto pred for verdadeiro e devolve o lexema.
func (l *lexer) takeWhile(pred func(rune) bool) string {
	from := l.pos
	for !l.atEnd() && pred(l.peek()) {
		l.advance()
	}
	return string(l.src[from:l.pos])
}

func (l *lexer) emit(kind token.Kind, lit string, pos token.Pos) {
	l.toks = append(l.toks, token.Token{Kind: kind, Lit: lit, Pos: pos})
}

func (l *lexer) errorf(pos token.Pos, format string, args ...any) {
	l.diags = append(l.diags, diag.Diagnostic{
		Severity: diag.SeverityError,
		Pos:      pos,
		Msg:      fmt.Sprintf(format, args...),
	})
}

// --- classes de runes ---

func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isIdentPart(r rune) bool  { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
func isDigit(r rune) bool      { return r >= '0' && r <= '9' }
