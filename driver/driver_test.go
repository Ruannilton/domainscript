package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-8.1 (CheckSource, programa correto): uma fonte válida não produz erros e
// devolve uma AST não-nil.
func TestCheckSourceClean(t *testing.T) {
	file, bag := CheckSource(`
		ValueObject Email(string) { Valid { ok } }
	`)
	if file == nil {
		t.Fatal("CheckSource nunca deve devolver AST nil")
	}
	if bag.HasErrors() {
		t.Fatalf("fonte válida não deveria ter erros:\n%s", bag.Render())
	}
}

// REQ-8.1 (CheckSource, com erro): uma regra semântica violada (primitivo no Write
// Side) é reportada como erro e sinaliza falha via HasErrors.
func TestCheckSourceReportsSemanticError(t *testing.T) {
	_, bag := CheckSource(`
		Aggregate Account { state { balance integer } }
	`)
	if !bag.HasErrors() {
		t.Fatalf("esperava erro semântico (primitivo no Write Side):\n%s", bag.Render())
	}
}

// REQ-8.1 (CheckSource, erro de sintaxe acumulado): erros de sintaxe entram no
// mesmo bag das demais fases (REQ-6.1).
func TestCheckSourceReportsSyntaxError(t *testing.T) {
	_, bag := CheckSource(`Aggregate {`)
	if !bag.HasErrors() {
		t.Fatalf("esperava erro de sintaxe:\n%s", bag.Render())
	}
}

// REQ-8.1/8.4 (CheckProject): valida um projeto multi-arquivo de um diretório,
// disparando uma regra cross-file (Policy cross-module sobre Event privado).
func TestCheckProjectCrossFileRule(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, map[string]string{
		"billing/mod.ds": `Module Billing { }`,
		"billing/events.ds": `
			ValueObject OrderId(string) { Valid { ok } }
			Event InvoicePaid { id OrderId }
		`,
		"shipping/mod.ds":    `Module Shipping { }`,
		"shipping/policy.ds": `Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`,
	})

	prog, bag := CheckProject(dir)
	if prog == nil {
		t.Fatal("CheckProject não deveria devolver Program nil para um diretório válido")
	}
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "PublicEvent") {
		t.Fatalf("esperava erro cross-file de Policy sobre Event privado:\n%s", r)
	}
}

// REQ-8.1 (CheckProject, projeto correto): um projeto bem-formado não gera erros.
func TestCheckProjectClean(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, map[string]string{
		"shop/mod.ds": `Module Shop { }`,
		"shop/domain.ds": `
			ValueObject OrderId(string) { Valid { ok } }
			Aggregate Order { state { id OrderId } }
			Query ListOrders() -> List<Order> { list Order take 10 }
		`,
		"shop/interface.ds": `Interface HTTP { GET "/orders" -> ListOrders }`,
	})

	_, bag := CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("projeto correto não deveria ter erros:\n%s", bag.Render())
	}
}

// writeProject materializa os arquivos (caminho relativo → fonte) sob dir.
func writeProject(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, src := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
