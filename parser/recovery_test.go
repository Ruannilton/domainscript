package parser

import (
	"testing"

	"domainscript/token"
)

func TestFirstErrorAlwaysEmitted(t *testing.T) {
	p, bag := mk("x")
	p.errorf(p.cur().Pos, "primeiro")
	if bag.Len() != 1 {
		t.Errorf("o primeiro erro deveria sempre passar; len=%d", bag.Len())
	}
}

// Erros emitidos em rajada, com poucos tokens entre eles, são parcialmente
// suprimidos: dois erros adjacentes não viram 5+ (REQ-3.5, NFR-1).
func TestSilenceWindowSuppressesCascade(t *testing.T) {
	p, bag := mk("a b c d e f")
	for i := 0; i < 6; i++ {
		p.errorf(p.cur().Pos, "erro sintético")
		p.advance()
	}
	if bag.Len() >= 5 {
		t.Errorf("janela de silêncio falhou: %d erros (esperava poucos)\n%s", bag.Len(), bag.Render())
	}
	if bag.Len() == 0 {
		t.Errorf("ao menos o primeiro erro deveria passar")
	}
}

// Após consumir a janela inteira, um novo erro real volta a ser emitido.
func TestSilenceWindowReopens(t *testing.T) {
	p, bag := mk("a b c d e")
	p.errorf(p.cur().Pos, "e1") // emitido; janela fecha
	p.advance()
	p.advance() // consumiu silenceWindow tokens → janela reabre
	p.errorf(p.cur().Pos, "e2")
	if bag.Len() != 2 {
		t.Errorf("=> %d erros, quero 2 (%s)", bag.Len(), bag.Render())
	}
}

// Erros de expect também respeitam a janela: chamadas consecutivas sem progresso
// não duplicam o diagnóstico.
func TestExpectErrorsRespectSilenceWindow(t *testing.T) {
	p, bag := mk("1 2 3")
	p.expect(token.IDENT) // inserção virtual: erro, não consome
	p.expect(token.IDENT) // mesma posição, ainda na janela → suprimido
	if bag.Len() != 1 {
		t.Errorf("=> %d erros, quero 1 (%s)", bag.Len(), bag.Render())
	}
}

// ensureProgress impede laço infinito quando o corpo não consome nada (REQ-3.6).
func TestEnsureProgressBreaksStuckLoop(t *testing.T) {
	p, _ := mk("a b c")
	iter := 0
	for !p.atEnd() {
		before := p.pos
		// nenhum parsing real: sem ensureProgress, isto seria laço infinito
		p.ensureProgress(before)
		iter++
		if iter > 100 {
			t.Fatalf("laço não terminou: ensureProgress não garantiu progresso")
		}
	}
	if iter != 3 { // 3 tokens reais antes do EOF
		t.Errorf("terminou em %d iterações, quero 3", iter)
	}
}

// ensureProgress não força avanço quando o corpo já consumiu algo.
func TestEnsureProgressNoopWhenAdvanced(t *testing.T) {
	p, _ := mk("a b c")
	before := p.pos
	p.advance() // corpo consumiu um token
	p.ensureProgress(before)
	if p.pos != 1 {
		t.Errorf("pos = %d, quero 1 (ensureProgress não deveria avançar de novo)", p.pos)
	}
}
