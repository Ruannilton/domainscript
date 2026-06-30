package driver

import (
	"strings"
	"testing"

	"domainscript/diag"
)

// TestNameTypeErrorsDeterministic exercita o determinismo (NFR-3/NFR-9) sobre as
// regras novas de nome e tipo: uma fonte com erros de membro (E102), de tipo
// incompatível (E103) e de nome em corpo (E100) deve render byte-a-byte idêntico em
// toda execução, independente da iteração de mapas no modelo de tipos e na tabela.
func TestNameTypeErrorsDeterministic(t *testing.T) {
	const src = `
		ValueObject Money { amount decimal currency string Valid { ok } }
		ValueObject WalletId(string) { Valid { ok } }
		Event Deposited { id WalletId amount Money }
		Aggregate Wallet {
			state { id WalletId balance Money }
			access { Get requires self.i == self.balance }
			Apply Deposited {
				state.balance = event.id
				state.id = nope
			}
		}
	`
	const runs = 25
	var want string
	for i := 0; i < runs; i++ {
		_, bag := CheckSource(src)
		got := bag.Render()
		if i == 0 {
			want = got
			if !strings.Contains(got, "E100") || !strings.Contains(got, "E102") || !strings.Contains(got, "E103") {
				t.Fatalf("esperava os três códigos de nome/tipo na fonte de teste:\n%s", got)
			}
			continue
		}
		if got != want {
			t.Fatalf("CheckSource não-determinístico na execução %d (NFR-3):\n--- esperado ---\n%s\n--- obtido ---\n%s",
				i, want, got)
		}
	}
}

// TestNameErrorDoesNotCascade fixa a anti-cascata (NFR-1/NFR-9, §design
// type-checking 4): um único nome não resolvido num corpo vira o tipo de erro e é
// absorvido pelas fases de tipo seguintes. Aqui `undefinedName` aparece num operador
// e do lado direito de uma atribuição; ainda assim gera **um só** diagnóstico (o
// erro de nome E100) — nem operando incompatível nem tipo incompatível derivados.
func TestNameErrorDoesNotCascade(t *testing.T) {
	const src = `
		ValueObject Money { amount decimal currency string Valid { ok } }
		Aggregate Wallet {
			state { balance Money }
			access { Do requires self.balance == self.balance }
			Handle Do(amount Money) {
				state.balance = undefinedName + amount
			}
		}
	`
	_, bag := CheckSource(src)
	if bag.Len() != 1 {
		t.Fatalf("anti-cascata: esperava 1 diagnóstico (só o erro de nome), obtive %d:\n%s", bag.Len(), bag.Render())
	}
	if got := bag.All()[0].Code; got != diag.CodeNameInBody {
		t.Errorf("esperava o erro de nome %s, obtive %q:\n%s", diag.CodeNameInBody, got, bag.Render())
	}
}
