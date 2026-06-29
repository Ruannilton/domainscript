package parser

import (
	"testing"

	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/token"
)

// mk lexa src e devolve um parser pronto, com o bag de diagnósticos.
func mk(src string) (*parser, *diag.DiagnosticBag) {
	toks, _ := lexer.Lex(src)
	bag := diag.New()
	return newParser(toks, bag), bag
}

func TestCursor(t *testing.T) {
	p, _ := mk("foo 42")
	if !p.at(token.IDENT) {
		t.Fatalf("cur = %v, quero IDENT", p.cur().Kind)
	}
	if p.peek().Kind != token.INT {
		t.Errorf("peek = %v, quero INT", p.peek().Kind)
	}
	if !p.accept(token.IDENT) {
		t.Errorf("accept(IDENT) deveria consumir")
	}
	if !p.at(token.INT) {
		t.Errorf("após accept, cur = %v, quero INT", p.cur().Kind)
	}
	if p.accept(token.IDENT) {
		t.Errorf("accept(IDENT) não deveria consumir um INT")
	}
	p.advance() // consome INT
	if !p.atEnd() {
		t.Errorf("deveria estar no EOF")
	}
	p.advance() // no-op no EOF
	if !p.atEnd() {
		t.Errorf("advance no EOF deveria ser no-op")
	}
}

func TestExpectPresent(t *testing.T) {
	p, bag := mk("foo")
	if !p.expect(token.IDENT) {
		t.Errorf("expect(IDENT) presente deveria devolver true")
	}
	if bag.Len() != 0 {
		t.Errorf("não deveria haver diagnósticos: %s", bag.Render())
	}
	if !p.atEnd() {
		t.Errorf("o IDENT deveria ter sido consumido")
	}
}

func TestExpectSingleTokenDeletion(t *testing.T) {
	// Esperamos IDENT; o corrente é INT (ruído) mas o próximo é IDENT.
	p, bag := mk("42 foo")
	if !p.expect(token.IDENT) {
		t.Errorf("deleção de token único deveria devolver true")
	}
	if bag.Len() != 1 {
		t.Errorf("=> %d diagnósticos, quero 1 (%s)", bag.Len(), bag.Render())
	}
	if !p.atEnd() {
		t.Errorf("ruído e token esperado deveriam ter sido consumidos")
	}
}

func TestExpectVirtualInsertion(t *testing.T) {
	// Esperamos IDENT; nem o corrente nem o próximo casam.
	p, bag := mk("42 43")
	if p.expect(token.IDENT) {
		t.Errorf("inserção virtual deveria devolver false")
	}
	if bag.Len() != 1 {
		t.Errorf("=> %d diagnósticos, quero 1 (%s)", bag.Len(), bag.Render())
	}
	if !p.at(token.INT) {
		t.Errorf("inserção virtual não deveria consumir; cur = %v", p.cur().Kind)
	}
}
