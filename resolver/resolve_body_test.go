package resolver

import (
	"strings"
	"testing"

	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
)

// REQ-9.1/9.4 (positivo): um identificador solto digitado errado num corpo
// executável dispara erro localizado. Reproduz o bug `amoun` do Wallet: o Handle
// declara o parâmetro `amount`, mas o corpo emite `amoun`.
func TestResolveBodyUndeclaredIdentFires(t *testing.T) {
	bag := resolveSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Event DepositPerformed { id WalletId, amount Money }
		Aggregate Wallet {
			state { id WalletId balance Money }
			Handle Deposit(amount Money) {
				emit DepositPerformed(self.id, amoun)
			}
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "amoun") {
		t.Fatalf("esperava erro de nome não declarado citando 'amoun':\n%s", r)
	}
}

// REQ-9 (negativo): o mesmo programa com o nome correto (`amount`) resolve em
// silêncio. Cobre receptores (self), parâmetros (amount) e símbolos (Event/VO).
func TestResolveBodyCorrectIsSilent(t *testing.T) {
	bag := resolveSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Event DepositPerformed { id WalletId, amount Money }
		Aggregate Wallet {
			state { id WalletId balance Money }
			Handle Deposit(amount Money) {
				emit DepositPerformed(self.id, amount)
			}
			Apply DepositPerformed {
				state.balance = event.amount
			}
		}
	`)
	if bag.Len() != 0 {
		t.Fatalf("corpo correto não deveria gerar diagnósticos:\n%s", bag.Render())
	}
}

// REQ-9.5: um nome introduzido por um for é visível só dentro do laço e some ao
// sair. O primeiro uso de `x` (dentro do for) resolve; o segundo (fora) dispara.
func TestResolveBodyForVarScopedToLoop(t *testing.T) {
	bag := resolveSrc(t, `
		ValueObject Id(string) { Valid { ok } }
		Command Cmd { id Id }
		UseCase UC handles Cmd {
			execute {
				for x in cmd { ensure x else Nop }
				ensure x else Nop
			}
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, `"x"`) {
		t.Fatalf("esperava erro para 'x' usado fora do for:\n%s", r)
	}
}

// REQ-9.2: locais introduzidos por atribuição a nome nu, params de lambda e
// binding de query resolvem; receptores contextuais (cmd) também. Tudo silencioso.
func TestResolveBodyLocalsBindersSilent(t *testing.T) {
	bag := resolveSrc(t, `
		ValueObject Id(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Aggregate Account {
			state { id Id balance Money entries AppendList<Money> }
			Handle Touch(id Id) {
				snapshot = state.balance
				ensure snapshot else Nop
				for e in state.entries { ensure e else Nop }
			}
		}
	`)
	if bag.Len() != 0 {
		t.Fatalf("locais e binders não deveriam gerar diagnósticos:\n%s", bag.Render())
	}
}

// REQ-9.6: subárvores com nós de erro de sintaxe são puladas — o lixo no corpo não
// vira um falso "nome não declarado".
func TestResolveBodySkipsErrorNodes(t *testing.T) {
	toks, _ := lexer.Lex(`
		ValueObject Id(string) { Valid { ok } }
		Command Cmd { id Id }
		UseCase UC handles Cmd { execute { @ @ @ } }
	`)
	synBag := diag.New()
	file := parser.Parse(toks, synBag)
	semBag := diag.New()
	Resolve(file, semBag)
	if strings.Contains(semBag.Render(), "não declarado") {
		t.Errorf("não deveria reportar nome não declarado em subárvore de erro:\n%s", semBag.Render())
	}
}
