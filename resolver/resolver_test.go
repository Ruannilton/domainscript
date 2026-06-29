package resolver

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
)

// parseSrc lexa e parseia src, falhando o teste se houver erro de sintaxe — os
// testes do resolver isolam erros semânticos, então a entrada deve ser
// sintaticamente limpa (NFR-6).
func parseSrc(t *testing.T, src string) *ast.File {
	t.Helper()
	toks, lexBag := lexer.Lex(src)
	if len(lexBag) > 0 {
		t.Fatalf("erro léxico inesperado: %v", lexBag)
	}
	bag := diag.New()
	file := parser.Parse(toks, bag)
	if bag.Len() != 0 {
		t.Fatalf("erro de sintaxe inesperado:\n%s", bag.Render())
	}
	return file
}

// resolveSrc parseia e resolve src num módulo anônimo, devolvendo os diagnósticos
// semânticos da resolução.
func resolveSrc(t *testing.T, src string) *diag.DiagnosticBag {
	t.Helper()
	file := parseSrc(t, src)
	bag := diag.New()
	Resolve(file, bag)
	return bag
}

// Programa correto: todas as referências resolvem → silêncio (REQ-4, NFR-4).
func TestResolveCorrectIsSilent(t *testing.T) {
	src := `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string }
		Aggregate Wallet {
			state { id WalletId balance Money }
			Handle Open(owner WalletId) { return }
		}
		Command OpenWallet { walletId ref Wallet amount Money }
		Event WalletOpened { id WalletId }
		UseCase OpenWalletUC handles OpenWallet { execute { return } }
		Policy OnOpened on WalletOpened { delivery AtLeastOnce execute { return } }
		Query GetBalance(id WalletId) -> Money { return }
		Notification Receipt { id WalletId }
	`
	bag := resolveSrc(t, src)
	if bag.Len() != 0 {
		t.Fatalf("programa correto não deveria gerar diagnósticos:\n%s", bag.Render())
	}
}

// Tipo de campo inexistente dispara erro de resolução (REQ-4.2).
func TestResolveUnknownFieldType(t *testing.T) {
	bag := resolveSrc(t, `Command C { id Nonexistent }`)
	if bag.Len() == 0 {
		t.Fatalf("esperava erro para tipo não declarado")
	}
	if !strings.Contains(bag.Render(), "Nonexistent") {
		t.Errorf("mensagem deveria citar o tipo: %s", bag.Render())
	}
}

// ref a um Aggregate inexistente dispara erro (REQ-4.4).
func TestResolveUnknownRef(t *testing.T) {
	bag := resolveSrc(t, `Command C { walletId ref Wallet }`)
	if bag.Len() == 0 {
		t.Errorf("esperava erro para ref a tipo não declarado")
	}
}

// handles a Command inexistente dispara erro (REQ-4.4).
func TestResolveUnknownHandles(t *testing.T) {
	bag := resolveSrc(t, `UseCase U handles MissingCmd { execute { return } }`)
	r := bag.Render()
	if bag.Len() == 0 || !strings.Contains(r, "MissingCmd") {
		t.Errorf("esperava erro citando MissingCmd: %s", r)
	}
}

// on a Event inexistente dispara erro (REQ-4.4).
func TestResolveUnknownOn(t *testing.T) {
	bag := resolveSrc(t, `Policy P on MissingEvent { execute { return } }`)
	r := bag.Render()
	if bag.Len() == 0 || !strings.Contains(r, "MissingEvent") {
		t.Errorf("esperava erro citando MissingEvent: %s", r)
	}
}

// Builtins (primitivos, coleções) e genéricos com argumento declarado resolvem
// sem erro; argumento genérico não declarado dispara (REQ-4.2).
func TestResolveGenerics(t *testing.T) {
	ok := resolveSrc(t, `
		ValueObject Tag(string) { Valid { ok } }
		Event E { tags AppendList<Tag> meta Map<string, string> }
	`)
	if ok.Len() != 0 {
		t.Errorf("genéricos com tipos válidos não deveriam gerar erro:\n%s", ok.Render())
	}

	bad := resolveSrc(t, `Event E { tags AppendList<Missing> }`)
	if bad.Len() == 0 || !strings.Contains(bad.Render(), "Missing") {
		t.Errorf("argumento genérico não declarado deveria disparar: %s", bad.Render())
	}
}

// Declaração duplicada no mesmo módulo dispara erro (REQ-4.3).
func TestResolveDuplicate(t *testing.T) {
	bag := resolveSrc(t, `
		Command Dup { id Ref }
		ValueObject Ref(string) { Valid { ok } }
		Event Dup { x Ref }
	`)
	r := bag.Render()
	if bag.Len() == 0 || !strings.Contains(r, "duplicado") {
		t.Errorf("esperava erro de nome duplicado: %s", r)
	}
}

// Subárvores de erro de sintaxe são puladas: um decl quebrado não vira erro
// semântico falso (REQ-4.5). O programa tem um erro de sintaxe, mas a resolução
// (rodando sobre a AST resultante) não deve adicionar ruído sobre o ErrorDecl.
func TestResolveSkipsErrorNodes(t *testing.T) {
	toks, _ := lexer.Lex(`Command C { id Money } @ @ @ ValueObject Money { amount decimal }`)
	synBag := diag.New()
	file := parser.Parse(toks, synBag)
	semBag := diag.New()
	Resolve(file, semBag)
	// A resolução não deve reportar "Money não declarado": Money existe, e o
	// lixo virou um ErrorDecl que é ignorado.
	if strings.Contains(semBag.Render(), "não declarado") {
		t.Errorf("resolução não deveria reportar tipo não declarado:\n%s", semBag.Render())
	}
}

// Resolução cross-module: um PublicEvent é visível de outro módulo; um Event
// privado não (REQ-7.4). Exercita o caminho multi-arquivo do Resolver.
func TestResolveCrossModulePublicEvent(t *testing.T) {
	pub := parseSrc(t, `PublicEvent OrderPlaced { id OrderId } ValueObject OrderId(string) { Valid { ok } }`)
	priv := parseSrc(t, `Event Internal { x OrderId } ValueObject OrderId(string) { Valid { ok } }`)
	consumer := parseSrc(t, `Policy P on OrderPlaced { execute { return } }`)

	bag := diag.New()
	r := New(bag)
	r.Add("order", pub)
	r.Add("billing", priv)
	r.Add("billing", consumer)
	r.ResolveAll()

	if bag.HasErrors() {
		t.Fatalf("Policy reagindo a PublicEvent de outro módulo não deveria errar:\n%s", bag.Render())
	}
}
