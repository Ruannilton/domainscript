package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shopExampleDir é o exemplo multi-módulo empacotado no repositório, relativo ao
// diretório do pacote driver. Diferente do Wallet (módulo único), o Shop tem dois
// módulos em services distintos, ligados por um canal na topologia — exercita as
// regras cross-file que são o diferencial do DomainScript.
var shopExampleDir = filepath.Join("..", "docs", "examples", "shop")

// TestShopExampleClean fixa o lado positivo (DoD §5): o exemplo Shop que acompanha
// o repositório passa pela validação completa — agregação cross-file, regras
// locais e cross-service — sem nenhum diagnóstico. `dsc docs/examples/shop` → exit 0.
func TestShopExampleClean(t *testing.T) {
	_, bag := CheckProject(shopExampleDir)
	if bag.HasErrors() || bag.Len() != 0 {
		t.Fatalf("o exemplo Shop não deveria gerar diagnósticos:\n%s", bag.Render())
	}
}

// TestShopMissingChannelRegression fixa o lado negativo da regra cross-service
// (REQ-5.11): removido o canal Orders -> Shipping da topologia, a Policy do módulo
// Shipping passa a reagir a um evento de Orders através da fronteira de service
// sem transporte declarado — exatamente um erro, citando a Policy. Prova que o
// exemplo de fato exercita a regra (não valida em silêncio por acidente).
func TestShopMissingChannelRegression(t *testing.T) {
	dir := t.TempDir()
	copyShopWithoutChannel(t, dir)

	_, bag := CheckProject(dir)

	r := bag.Render()
	if !bag.HasErrors() {
		t.Fatalf("esperava erro ao remover o canal cross-service:\n%s", r)
	}
	if bag.Len() != 1 {
		t.Fatalf("esperava exatamente 1 diagnóstico (a regra cross-service), obtive %d:\n%s", bag.Len(), r)
	}
	if !strings.Contains(r, "sem canal") || !strings.Contains(r, "NotifyShipping") {
		t.Fatalf("esperava erro de módulos sem canal citando a Policy NotifyShipping:\n%s", r)
	}
}

// channelDecl é a linha de canal cuja remoção da topologia quebra a comunicação
// cross-service. Mantida como constante para o teste falhar de forma explícita se
// o exemplo mudar (em vez de testar uma remoção que não remove nada).
const channelDecl = "Orders -> Shipping { via: queue orderBy: id }"

// copyShopWithoutChannel copia o exemplo Shop para dst (recursivamente, preservando
// os subdiretórios de módulo) removendo a declaração de canal da topology.ds.
func copyShopWithoutChannel(t *testing.T, dst string) {
	t.Helper()
	err := filepath.WalkDir(shopExampleDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(shopExampleDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(raw)
		if filepath.Base(path) == "topology.ds" {
			if !strings.Contains(content, channelDecl) {
				t.Fatalf("a declaração de canal sumiu de topology.ds: %q (o exemplo mudou?)", channelDecl)
			}
			content = strings.Replace(content, channelDecl, "", 1)
		}
		return os.WriteFile(target, []byte(content), 0o644)
	})
	if err != nil {
		t.Fatalf("não consegui copiar o exemplo Shop: %v", err)
	}
}
