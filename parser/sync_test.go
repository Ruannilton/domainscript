package parser

import (
	"testing"

	"domainscript/token"
)

func TestSynchronizeStopsAtTopLevelKeyword(t *testing.T) {
	p, _ := mk("+ * - Aggregate Foo")
	p.synchronize(topLevelStop)
	if !p.at(token.AGGREGATE) {
		t.Errorf("synchronize parou em %v, quero AGGREGATE (a âncora não é consumida)", p.cur().Kind)
	}
}

// O delimitador de fechamento de um bloco externo nunca é consumido, mesmo que
// o stop set não o liste (REQ-3.4).
func TestSynchronizeDoesNotEatClosingBrace(t *testing.T) {
	p, _ := mk("+ + + }")
	p.synchronize(stmtStop)
	if !p.at(token.RBRACE) {
		t.Errorf("synchronize parou em %v, quero RBRACE", p.cur().Kind)
	}
}

func TestSynchronizeStopsAtEOF(t *testing.T) {
	p, _ := mk("+ + +")
	p.synchronize(topLevelStop)
	if !p.atEnd() {
		t.Errorf("synchronize deveria parar no EOF; cur = %v", p.cur().Kind)
	}
}

func TestSynchronizeStopsAtListDelimiter(t *testing.T) {
	p, _ := mk("+ + , x")
	p.synchronize(listStop)
	if !p.at(token.COMMA) {
		t.Errorf("synchronize parou em %v, quero COMMA", p.cur().Kind)
	}
}

// Reancoragem independente: após descartar lixo, o parser para na próxima
// declaração de topo, permitindo reconhecê-la do zero (REQ-3.7).
func TestSynchronizeReanchorsOnNextTopLevel(t *testing.T) {
	p, _ := mk("ValueObject + + + Aggregate")
	p.advance() // consome ValueObject (simula uma declaração que falhou)
	p.synchronize(topLevelStop)
	if !p.at(token.AGGREGATE) {
		t.Errorf("reancorou em %v, quero AGGREGATE", p.cur().Kind)
	}
}
