package program

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
)

// parseSrc lexa e parseia src, exigindo entrada sintaticamente limpa (NFR-6).
func parseSrc(t *testing.T, src string) *ast.File {
	t.Helper()
	toks, lexDiags := lexer.Lex(src)
	if len(lexDiags) > 0 {
		t.Fatalf("erro léxico inesperado: %v", lexDiags)
	}
	bag := diag.New()
	file := parser.Parse(toks, bag)
	if bag.Len() != 0 {
		t.Fatalf("erro de sintaxe inesperado:\n%s", bag.Render())
	}
	return file
}

// p constrói o caminho de um arquivo num módulo, independente do separador do SO.
func modPath(parts ...string) string { return filepath.Join(parts...) }

// Agrega arquivos de dois módulos: cada arquivo é atribuído ao módulo do seu
// mod.ds e os símbolos ficam acessíveis globalmente (REQ-7.1/7.3).
func TestNewAggregatesFilesByModule(t *testing.T) {
	carteiraMod := parseSrc(t, `Module Carteira { Database WalletDb { provider: "pg" manages: [Wallet] } }`)
	carteiraDomain := parseSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		Aggregate Wallet { state { id WalletId } Handle Open(owner WalletId) { return } }
	`)
	notifMod := parseSrc(t, `Module Notificacoes { Database NotifDb { provider: "pg" manages: [Inbox] } }`)
	notifDomain := parseSrc(t, `
		ValueObject InboxId(string) { Valid { ok } }
		Aggregate Inbox { state { id InboxId } Handle Open(owner InboxId) { return } }
	`)

	bag := diag.New()
	prog := New([]Source{
		{Path: modPath("carteira", "mod.ds"), File: carteiraMod},
		{Path: modPath("carteira", "wallet.ds"), File: carteiraDomain},
		{Path: modPath("notificacoes", "mod.ds"), File: notifMod},
		{Path: modPath("notificacoes", "inbox.ds"), File: notifDomain},
	}, bag)

	if bag.HasErrors() {
		t.Fatalf("projeto correto não deveria gerar erros:\n%s", bag.Render())
	}
	if len(prog.Files) != 4 {
		t.Errorf("esperava 4 arquivos agregados, obtive %d", len(prog.Files))
	}

	// Arquivos herdam o módulo do mod.ds do seu diretório.
	if got := prog.ModuleOf(modPath("carteira", "wallet.ds")); got != "Carteira" {
		t.Errorf("wallet.ds deveria pertencer a Carteira, obtive %q", got)
	}
	if got := prog.ModuleOf(modPath("notificacoes", "inbox.ds")); got != "Notificacoes" {
		t.Errorf("inbox.ds deveria pertencer a Notificacoes, obtive %q", got)
	}

	// Acesso global a símbolos de qualquer módulo (REQ-7.3).
	if _, ok := prog.Symbols.Lookup("Carteira", "Wallet"); !ok {
		t.Errorf("símbolo Wallet deveria estar acessível no módulo Carteira")
	}
	if _, ok := prog.Symbols.Lookup("Notificacoes", "Inbox"); !ok {
		t.Errorf("símbolo Inbox deveria estar acessível no módulo Notificacoes")
	}
}

// Arquivos em contracts/ herdam o módulo do mod.ds do diretório-pai, e um
// PublicEvent declarado ali é visível de outro módulo (REQ-7.4).
func TestNewContractsInheritParentModule(t *testing.T) {
	mod := parseSrc(t, `Module Pedidos { Database OrdersDb { provider: "pg" manages: [Order] } }`)
	contract := parseSrc(t, `PublicEvent OrderPlaced { id OrderId } ValueObject OrderId(string) { Valid { ok } }`)
	consumer := parseSrc(t, `Policy OnPlaced on OrderPlaced { delivery AtLeastOnce execute { return } }`)

	bag := diag.New()
	prog := New([]Source{
		{Path: modPath("pedidos", "mod.ds"), File: mod},
		{Path: modPath("pedidos", "contracts", "events.ds"), File: contract},
		{Path: modPath("faturamento", "policy.ds"), File: consumer},
	}, bag)

	if got := prog.ModuleOf(modPath("pedidos", "contracts", "events.ds")); got != "Pedidos" {
		t.Errorf("contracts/events.ds deveria herdar o módulo Pedidos, obtive %q", got)
	}
	// A Policy de faturamento reage a um PublicEvent de pedidos: deve resolver sem
	// erro através do nível público da tabela global.
	if bag.HasErrors() {
		t.Fatalf("Policy reagindo a PublicEvent cross-module não deveria errar:\n%s", bag.Render())
	}
}

// Build lê um diretório de disco, parseia tudo e agrega; diretórios aninhados
// (contracts/) são percorridos recursivamente (REQ-7.1, REQ-8.4).
func TestBuildReadsDirectory(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join("carteira", "mod.ds"), `Module Carteira { Database WalletDb { provider: "pg" manages: [Wallet] } }`)
	write(filepath.Join("carteira", "wallet.ds"), `ValueObject WalletId(string) { Valid { ok } }
		Aggregate Wallet { state { id WalletId } Handle Open(owner WalletId) { return } }`)

	bag := diag.New()
	prog, err := Build(dir, bag)
	if err != nil {
		t.Fatalf("Build falhou: %v", err)
	}
	if bag.HasErrors() {
		t.Fatalf("projeto correto não deveria gerar erros:\n%s", bag.Render())
	}
	if len(prog.Files) != 2 {
		t.Errorf("esperava 2 arquivos lidos do diretório, obtive %d", len(prog.Files))
	}
	if got := prog.ModuleOf(filepath.Join(dir, "carteira", "wallet.ds")); got != "Carteira" {
		t.Errorf("wallet.ds deveria pertencer a Carteira, obtive %q", got)
	}
	if _, ok := prog.Symbols.Lookup("Carteira", "Wallet"); !ok {
		t.Errorf("símbolo Wallet deveria estar acessível após Build")
	}
}

// Determinismo (NFR-3): a mesma entrada em ordens diferentes produz o mesmo
// conjunto de diagnósticos, na mesma ordem.
func TestNewDeterministicDiagnostics(t *testing.T) {
	// Dois arquivos no mesmo módulo declarando o mesmo símbolo → duplicata.
	a := func() *ast.File {
		return parseSrc(t, `Command Dup { id RefT } ValueObject RefT(string) { Valid { ok } }`)
	}
	b := func() *ast.File { return parseSrc(t, `Event Dup { x RefT }`) }

	render := func(order []Source) string {
		bag := diag.New()
		New(order, bag)
		return bag.Render()
	}
	first := render([]Source{
		{Path: modPath("m", "a.ds"), File: a()},
		{Path: modPath("m", "b.ds"), File: b()},
	})
	second := render([]Source{
		{Path: modPath("m", "b.ds"), File: b()},
		{Path: modPath("m", "a.ds"), File: a()},
	})
	if first != second {
		t.Errorf("diagnósticos não determinísticos:\n--- 1 ---\n%s\n--- 2 ---\n%s", first, second)
	}
	if !strings.Contains(first, "duplicado") {
		t.Errorf("esperava erro de duplicata: %s", first)
	}
}
