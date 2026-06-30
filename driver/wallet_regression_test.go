package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domainscript/diag"
)

// walletExampleDir é o exemplo de referência empacotado no repositório, relativo
// ao diretório do pacote driver.
var walletExampleDir = filepath.Join("..", "docs", "examples", "wallet")

// TestWalletExampleClean fixa o lado positivo da regressão (DoD §5): o exemplo
// Wallet **corrigido** que acompanha o repositório passa pela validação completa
// sem nenhum diagnóstico — `dsc docs/examples/wallet` → exit 0.
func TestWalletExampleClean(t *testing.T) {
	_, bag := CheckProject(walletExampleDir)
	if bag.HasErrors() || bag.Len() != 0 {
		t.Fatalf("o exemplo Wallet corrigido não deveria gerar diagnósticos:\n%s", bag.Render())
	}
}

// TestWalletThreeBugsRegression fixa o lado negativo: a versão com os três typos
// históricos — `amoun` (corpo, REQ-9/E100), `Walle` (config, REQ-10/E101) e
// `self.i` (membro, REQ-12/E102) — dispara **exatamente** três erros, um por bug,
// cada um com o seu código estável. Antes da resolução completa de nomes/tipos os
// três passavam silenciosos; este teste garante que não regridem.
//
// A versão buggy é derivada do exemplo real injetando só os três typos: assim o
// teste prova que essas três diferenças, e nada mais, produzem os três erros.
func TestWalletThreeBugsRegression(t *testing.T) {
	dir := t.TempDir()
	copyWalletWithBugs(t, dir)

	_, bag := CheckProject(dir)

	codes := map[diag.Code]int{}
	for _, d := range bag.All() {
		codes[d.Code]++
	}
	want := map[diag.Code]int{
		diag.CodeNameInBody:    1, // amoun
		diag.CodeConfigRef:     1, // Walle
		diag.CodeUnknownMember: 1, // self.i
	}
	if bag.Len() != 3 {
		t.Fatalf("esperava exatamente 3 erros (um por bug), obtive %d:\n%s", bag.Len(), bag.Render())
	}
	for code, n := range want {
		if codes[code] != n {
			t.Errorf("esperava %d diagnóstico(s) com código %s, obtive %d:\n%s", n, code, codes[code], bag.Render())
		}
	}
}

// copyWalletWithBugs copia o exemplo Wallet para dst, injetando os três typos
// históricos nos arquivos onde ocorrem. Falha se algum dos contextos esperados
// sumiu do exemplo (o exemplo mudou): a regressão não pode passar testando nada.
func copyWalletWithBugs(t *testing.T, dst string) {
	t.Helper()
	bugs := map[string][]struct{ old, new string }{
		"domain.ds": {
			{"caller.id == self.id", "caller.id == self.i"},
			{"DepositPerformed(self.id, amount, description)", "DepositPerformed(self.id, amoun, description)"},
		},
		"mod.ds": {
			{"manages: [Wallet]", "manages: [Walle]"},
		},
	}

	entries, err := os.ReadDir(walletExampleDir)
	if err != nil {
		t.Fatalf("não consegui ler o exemplo Wallet: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(walletExampleDir, e.Name()))
		if err != nil {
			t.Fatalf("não consegui ler %s: %v", e.Name(), err)
		}
		content := string(raw)
		for _, b := range bugs[e.Name()] {
			if !strings.Contains(content, b.old) {
				t.Fatalf("contexto do typo sumiu de %s: %q (o exemplo mudou?)", e.Name(), b.old)
			}
			content = strings.Replace(content, b.old, b.new, 1)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), []byte(content), 0o644); err != nil {
			t.Fatalf("não consegui escrever %s: %v", e.Name(), err)
		}
	}
}
