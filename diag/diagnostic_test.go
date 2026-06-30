package diag

import (
	"strings"
	"testing"

	"domainscript/token"
)

func p(line, col int) token.Pos { return token.Pos{Line: line, Col: col} }

func TestDedupExato(t *testing.T) {
	b := New()
	b.Errorf(p(1, 1), "boom")
	b.Errorf(p(1, 1), "boom")   // duplicata exata → ignorada
	b.Warningf(p(1, 1), "boom") // mesma pos/msg, severidade diferente → mantida
	b.Errorf(p(1, 2), "boom")   // mesma msg, coluna diferente → mantida
	if b.Len() != 3 {
		t.Fatalf("Len = %d, quero 3 (%q)", b.Len(), b.Render())
	}
}

func TestTeto(t *testing.T) {
	b := NewWithMax(3)
	for i := 0; i < 10; i++ {
		b.Errorf(p(i+1, 1), "erro %d", i)
	}
	if b.Len() != 3 {
		t.Errorf("Len = %d, quero 3 (teto)", b.Len())
	}
	if !b.Truncated() {
		t.Errorf("esperava bag truncado")
	}
	if !strings.Contains(b.Render(), "interrompida") {
		t.Errorf("Render deveria conter a sentinela de truncamento:\n%s", b.Render())
	}
}

func TestTetoNaoContaWarnings(t *testing.T) {
	b := NewWithMax(1)
	b.Warningf(p(1, 1), "a")
	b.Warningf(p(2, 1), "b")
	b.Errorf(p(3, 1), "c")
	if b.Truncated() {
		t.Errorf("avisos não deveriam estourar o teto de erros")
	}
	if b.Len() != 3 {
		t.Errorf("Len = %d, quero 3", b.Len())
	}
}

func TestOrdenacaoEstavelERender(t *testing.T) {
	b := New()
	b.Errorf(p(3, 2), "c")
	b.Errorf(p(1, 5), "a")
	b.Warningf(p(1, 2), "b")
	want := "1:2: warning: b\n1:5: error: a\n3:2: error: c"
	if got := b.Render(); got != want {
		t.Errorf("Render =\n%s\n\nquero\n%s", got, want)
	}
}

// REQ-6.6 / §design type-checking 3.7: um diagnóstico com código o renderiza
// anexado à severidade ("error[E103]"), e um sem código mantém o formato base
// inalterado — a extensão é compatível com os diagnósticos das fases anteriores.
func TestRenderComCodigo(t *testing.T) {
	b := New()
	b.CodedErrorf(p(2, 4), CodeTypeMismatch, "tipo incompatível: esperado Money, encontrado WalletId")
	b.Errorf(p(1, 1), "sem código")
	want := "1:1: error: sem código\n2:4: error[E103]: tipo incompatível: esperado Money, encontrado WalletId"
	if got := b.Render(); got != want {
		t.Errorf("Render =\n%s\n\nquero\n%s", got, want)
	}
}

func TestHasErrors(t *testing.T) {
	b := New()
	if b.HasErrors() {
		t.Errorf("bag vazio não tem erros")
	}
	b.Warningf(p(1, 1), "aviso")
	if b.HasErrors() {
		t.Errorf("só aviso não sinaliza falha")
	}
	b.Errorf(p(2, 1), "erro")
	if !b.HasErrors() {
		t.Errorf("esperava HasErrors após um erro")
	}
}
