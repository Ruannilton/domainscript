package diag

import (
	"testing"

	"domainscript/token"
)

// TestRenderDeterministicAcrossInsertionOrder verifica a propriedade que permite
// mesclar diagnósticos de sintaxe e semântica naturalmente (NFR-3): para posições
// distintas, a ordem de inserção é irrelevante — a ordenação por (linha, coluna)
// só acontece no Render. (Empates exatos de posição preservam a ordem de inserção,
// que é determinística porque o pipeline insere sempre na mesma ordem para a mesma
// entrada; ver TestRenderStableForEqualPositions.)
func TestRenderDeterministicAcrossInsertionOrder(t *testing.T) {
	mk := func() []Diagnostic {
		return []Diagnostic{
			{Severity: SeverityError, Pos: token.Pos{Line: 10, Col: 3}, Msg: "c"},
			{Severity: SeverityWarning, Pos: token.Pos{Line: 1, Col: 5}, Msg: "a"},
			{Severity: SeverityError, Pos: token.Pos{Line: 5, Col: 2}, Msg: "b"},
			{Severity: SeverityError, Pos: token.Pos{Line: 2, Col: 1}, Msg: "d"},
		}
	}
	forward := New()
	for _, d := range mk() {
		forward.Add(d)
	}
	ds := mk()
	reverse := New()
	for i := len(ds) - 1; i >= 0; i-- {
		reverse.Add(ds[i])
	}
	if forward.Render() != reverse.Render() {
		t.Fatalf("Render depende da ordem de inserção (NFR-3):\n--- forward ---\n%s\n--- reverse ---\n%s",
			forward.Render(), reverse.Render())
	}
}

// TestRenderStableForEqualPositions garante que diagnósticos com a mesma posição
// preservam a ordem de inserção (SliceStable), de modo que a saída seja totalmente
// determinística mesmo em empates de (linha, coluna) (NFR-3).
func TestRenderStableForEqualPositions(t *testing.T) {
	const runs = 50
	var want string
	for r := 0; r < runs; r++ {
		b := New()
		b.Errorf(token.Pos{Line: 1, Col: 1}, "primeiro")
		b.Errorf(token.Pos{Line: 1, Col: 1}, "segundo")
		b.Errorf(token.Pos{Line: 1, Col: 1}, "terceiro")
		got := b.Render()
		if r == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("Render não-determinístico em empate de posição:\n%s\n!=\n%s", got, want)
		}
	}
}
