package parser

import (
	"testing"

	"domainscript/diag"
	"domainscript/lexer"
)

// parseCount lexa e parseia src e devolve quantos diagnósticos o parser emitiu
// (ignora os léxicos, que não são cascata de sintaxe).
func parseCount(t *testing.T, src string) int {
	t.Helper()
	toks, _ := lexer.Lex(src)
	bag := diag.New()
	Parse(toks, bag)
	return bag.Len()
}

// maxCascade é o teto de diagnósticos aceitável para uma única falha de sintaxe.
// A janela de silêncio (REQ-3.5) e o reâncoramento de topo (REQ-3.7) devem manter o
// número derivado pequeno e fixo — não proporcional ao tamanho do resto do arquivo
// (NFR-1).
const maxCascade = 3

// TestCascadeSingleErrorIsBounded garante que um único erro de sintaxe, seguido de
// muito código bem-formado, não dispara uma avalanche de diagnósticos falsos. A
// janela de silêncio absorve o ruído imediato e o parser reancora (NFR-1, REQ-3.5).
func TestCascadeSingleErrorIsBounded(t *testing.T) {
	// Um único token inesperado (`)`) dentro do state, seguido de declarações
	// perfeitamente válidas que não podem virar erros.
	src := `
		Aggregate Wallet { state { id WalletId ) balance Money } }
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Command DepositCmd { walletId ref Wallet amount Money }
		Event WalletOpened { id WalletId }
		Query ListWallets() -> List<Wallet> { list Wallet take 10 }
	`
	if n := parseCount(t, src); n > maxCascade {
		_, _ = lexer.Lex(src)
		t.Fatalf("um único erro de sintaxe gerou %d diagnósticos (> %d): cascata não contida (NFR-1)", n, maxCascade)
	}
}

// TestCascadeAdjacentErrorsDoNotMultiply confirma o efeito da janela de silêncio:
// dois erros adjacentes não viram 5+. O teto continua pequeno mesmo com falhas
// próximas (REQ-3.5).
func TestCascadeAdjacentErrorsDoNotMultiply(t *testing.T) {
	// Dois tokens-lixo adjacentes no meio de uma declaração válida.
	src := `Aggregate Wallet { state { id WalletId ) ) balance Money } }`
	if n := parseCount(t, src); n > maxCascade {
		t.Fatalf("dois erros adjacentes geraram %d diagnósticos (> %d): janela de silêncio ineficaz (REQ-3.5)", n, maxCascade)
	}
}

// TestCascadeReanchorsPerDeclaration garante que uma declaração quebrada não
// contamina as seguintes: cada declaração de topo válida após o erro é reconhecida
// independentemente, então o número de erros é o número de declarações realmente
// quebradas — não cresce com as declarações boas (REQ-3.7).
func TestCascadeReanchorsPerDeclaration(t *testing.T) {
	good := `
		ValueObject Email(string) { Valid { ok } }
		ValueObject Phone(string) { Valid { ok } }
		ValueObject Name(string) { Valid { ok } }
		ValueObject Cpf(string) { Valid { ok } }
	`
	clean := parseCount(t, good)
	if clean != 0 {
		t.Fatalf("declarações boas deveriam ter 0 erros, têm %d", clean)
	}
	// Uma única declaração quebrada prefixada às boas: o total deve permanecer
	// pequeno e fixo, não escalar com as quatro declarações válidas seguintes.
	broken := `ValueObject Broken( { Valid { ok } }` + good
	if n := parseCount(t, broken); n > maxCascade {
		t.Fatalf("declaração quebrada contaminou as seguintes: %d diagnósticos (> %d) (REQ-3.7)", n, maxCascade)
	}
}
