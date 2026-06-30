package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Benchmarks de guarda sobre a API pública. Não há alvo numérico nas NFRs —
// servem de guard-rail de regressão (tempo/alocação) e detectam um custo
// acidentalmente superlinear no pipeline. Rodar com:
//   go test ./driver/ -bench=. -benchmem

// benchWalletSource carrega o domain.ds do exemplo Wallet uma vez para servir de
// entrada realista ao benchmark de CheckSource.
func benchWalletSource(b *testing.B) string {
	b.Helper()
	raw, err := os.ReadFile(filepath.Join(walletExampleDir, "domain.ds"))
	if err != nil {
		b.Fatalf("não consegui ler o domain.ds do exemplo: %v", err)
	}
	return string(raw)
}

// BenchmarkCheckSource mede o pipeline de arquivo único (léxico → regras locais)
// sobre o domain.ds do Wallet.
func BenchmarkCheckSource(b *testing.B) {
	src := benchWalletSource(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CheckSource(src)
	}
}

// BenchmarkCheckProject mede o pipeline de projeto inteiro (agregação cross-file +
// regras locais e cross-file) sobre o diretório do exemplo Wallet.
func BenchmarkCheckProject(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CheckProject(walletExampleDir)
	}
}

// genProgram gera um programa válido com n Events que referenciam um ValueObject
// compartilhado, estressando a resolução de tipos/símbolos n vezes — o ponto onde
// um custo O(n²) acidental na tabela de símbolos apareceria.
func genProgram(n int) string {
	var sb strings.Builder
	sb.WriteString("ValueObject Money { amount decimal currency string Valid { amount >= 0 } }\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "Event Ev%d { amount Money }\n", i)
	}
	return sb.String()
}

// BenchmarkCheckSourceScale roda CheckSource sobre entradas de tamanho crescente.
// Comparar ns/op entre os tamanhos revela se o pipeline escala de forma linear.
func BenchmarkCheckSourceScale(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		src := genProgram(n)
		b.Run(fmt.Sprintf("events=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				CheckSource(src)
			}
		})
	}
}
