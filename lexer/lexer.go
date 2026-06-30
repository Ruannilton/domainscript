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
		case l.lexOperator(pos):
			// pontuação/operador reconhecido em lexOperator
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

// lexIdentOrKeyword consome um identificador e o classifica como version_id
// (ex.: v1, v12), keyword ou IDENT.
func (l *lexer) lexIdentOrKeyword(start token.Pos) {
	text := l.takeWhile(isIdentPart)
	if isVersionID(text) {
		l.emit(token.VERSIONID, text, start)
		return
	}
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
	if k, suffix, ok := l.scanNumberSuffix(); ok {
		l.emit(k, lit+suffix, start)
		return
	}
	l.emit(kind, lit, start)
}

// scanNumberSuffix detecta um sufixo de domínio colado a um número e o consome:
// uma unidade de tempo (5s, 48h → DURATION) ou de tamanho (100MB → SIZE) logo
// após o número, ou "/" seguido de unidade de tempo (300/min → RATE). Devolve
// ok=false sem consumir nada quando não há sufixo reconhecido — o caso comum,
// que preserva "3 * 4" e a divisão "a / b" (REQ-1.2, §design 3.2).
func (l *lexer) scanNumberSuffix() (kind token.Kind, suffix string, ok bool) {
	if isIdentStart(l.peek()) {
		unit := l.peekIdentRun(0)
		switch {
		case durationUnits[unit]:
			l.advanceN(len([]rune(unit)))
			return token.DURATION, unit, true
		case sizeUnits[unit]:
			l.advanceN(len([]rune(unit)))
			return token.SIZE, unit, true
		}
		return 0, "", false // sufixo desconhecido: não pertence ao número
	}
	if l.peek() == '/' {
		unit := l.peekIdentRun(1) // unidade após a '/'
		if durationUnits[unit] {
			l.advance() // '/'
			l.advanceN(len([]rune(unit)))
			return token.RATE, "/" + unit, true
		}
	}
	return 0, "", false
}

// peekIdentRun devolve, sem consumir, a sequência de runes de identificador que
// começa em offset runes à frente do cursor.
func (l *lexer) peekIdentRun(offset int) string {
	from := l.pos + offset
	i := from
	for i < len(l.src) && isIdentPart(l.src[i]) {
		i++
	}
	if from >= len(l.src) {
		return ""
	}
	return string(l.src[from:i])
}

// advanceN consome n runes.
func (l *lexer) advanceN(n int) {
	for i := 0; i < n; i++ {
		l.advance()
	}
}

// lexString consome um literal de string entre aspas, resolvendo as sequências
// de escape \n \t \" \\ (REQ-1.8). Uma string não terminada antes do fim da
// linha ou do arquivo gera um diagnóstico localizado no início da string
// (REQ-1.7); o token STRING ainda é emitido com o conteúdo lido até ali, para o
// parser poder prosseguir. O Lit guarda o valor já decodificado, sem aspas.
// lineOrFile nomeia a fronteira onde a string deixou de ser fechada, para que o
// diagnóstico aponte a causa concreta (fim de linha vs. fim de arquivo).
func lineOrFile(atEOF bool) string {
	if atEOF {
		return "arquivo"
	}
	return "linha"
}

func (l *lexer) lexString(start token.Pos) {
	l.advance() // aspas de abertura
	var sb strings.Builder
	for {
		if l.atEnd() || l.peek() == '\n' {
			l.errorf(start, "string não terminada: esperava aspas de fechamento %q antes do fim da %s", `"`, lineOrFile(l.atEnd()))
			l.emit(token.STRING, sb.String(), start)
			return
		}
		chPos := l.here()
		r := l.advance()
		switch r {
		case '"':
			l.emit(token.STRING, sb.String(), start)
			return
		case '\\':
			if l.atEnd() || l.peek() == '\n' {
				l.errorf(start, "string não terminada: esperava aspas de fechamento %q antes do fim da %s", `"`, lineOrFile(l.atEnd()))
				l.emit(token.STRING, sb.String(), start)
				return
			}
			switch e := l.advance(); e {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				l.errorf(chPos, "sequência de escape inválida %q", "\\"+string(e))
				sb.WriteRune(e)
			}
		default:
			sb.WriteRune(r)
		}
	}
}

// lexOperator reconhece toda a pontuação e os operadores do spec (REQ-1.3),
// incluindo os de dois caracteres (->, ==, !=, <=, >=). Devolve false sem
// consumir nada quando o rune atual não inicia um operador. A barra simples vira
// SLASH; "//" já foi tratado como comentário em skipTrivia.
func (l *lexer) lexOperator(start token.Pos) bool {
	switch l.peek() {
	case '{':
		l.advance()
		l.emit(token.LBRACE, "", start)
	case '}':
		l.advance()
		l.emit(token.RBRACE, "", start)
	case '(':
		l.advance()
		l.emit(token.LPAREN, "", start)
	case ')':
		l.advance()
		l.emit(token.RPAREN, "", start)
	case '[':
		l.advance()
		l.emit(token.LBRACK, "", start)
	case ']':
		l.advance()
		l.emit(token.RBRACK, "", start)
	case ',':
		l.advance()
		l.emit(token.COMMA, "", start)
	case '.':
		l.advance()
		l.emit(token.DOT, "", start)
	case ':':
		l.advance()
		l.emit(token.COLON, "", start)
	case '+':
		l.advance()
		l.emit(token.PLUS, "", start)
	case '*':
		l.advance()
		l.emit(token.STAR, "", start)
	case '/':
		l.advance()
		l.emit(token.SLASH, "", start)
	case '-':
		l.advance()
		if l.peek() == '>' {
			l.advance()
			l.emit(token.ARROW, "", start)
		} else {
			l.emit(token.MINUS, "", start)
		}
	case '=':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(token.EQ, "", start)
		} else {
			l.emit(token.ASSIGN, "", start)
		}
	case '!':
		if l.peek2() == '=' {
			l.advance()
			l.advance()
			l.emit(token.NEQ, "", start)
		} else {
			return false // '!' sozinho não é operador válido
		}
	case '<':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(token.LE, "", start)
		} else {
			l.emit(token.LT, "", start)
		}
	case '>':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			l.emit(token.GE, "", start)
		} else {
			l.emit(token.GT, "", start)
		}
	default:
		return false
	}
	return true
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

// Unidades de domínio reconhecidas como sufixos de literais numéricos.
// As de tempo servem tanto para DURATION (5s) quanto para RATE (300/min).
var (
	durationUnits = map[string]bool{"ms": true, "s": true, "min": true, "h": true, "d": true}
	sizeUnits     = map[string]bool{"B": true, "KB": true, "MB": true, "GB": true, "TB": true}
)

// isVersionID reconhece um version_id: 'v' minúsculo seguido só de dígitos
// (v1, v12). Distingue-se de identificadores comuns (value) e da keyword Version.
func isVersionID(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, r := range s[1:] {
		if !isDigit(r) {
			return false
		}
	}
	return true
}
