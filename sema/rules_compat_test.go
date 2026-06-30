package sema

import (
	"strings"
	"testing"
)

// REQ-13.1/13.2 (positivo, atribuição): atribuir um valor de um tipo de domínio a um
// alvo de outro tipo de domínio dispara erro esperado-vs-encontrado na posição do
// valor. Aqui `state.balance` é Money e `event.id` é WalletId.
func TestTypeCompatAssignIncompatibleFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Event Deposited { id WalletId amount Money }
		Aggregate Wallet {
			state { id WalletId balance Money }
			Apply Deposited {
				state.balance = event.id
			}
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "tipo incompatível") {
		t.Fatalf("esperava erro de tipo incompatível:\n%s", r)
	}
	if !strings.Contains(r, "Money") || !strings.Contains(r, "WalletId") {
		t.Errorf("esperava esperado(Money) vs encontrado(WalletId):\n%s", r)
	}
}

// REQ-13 (negativo): o mesmo agregado com a atribuição compatível (Money = Money) e
// usos corretos fica em silêncio.
func TestTypeCompatCorrectIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Event Deposited { id WalletId amount Money }
		Aggregate Wallet {
			state { id WalletId balance Money }
			access { Get requires self.id == self.id }
			Apply Deposited {
				state.balance = event.amount
			}
		}
	`)
	mustBeSilent(t, bag, "REQ-13 (uso compatível)")
}

// REQ-13.1 (positivo, operador): comparar dois tipos de domínio distintos numa
// condição de access dispara erro de operandos incompatíveis.
func TestTypeCompatOperatorIncompatibleFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Aggregate Wallet {
			state { id WalletId balance Money }
			access { Get requires self.id == self.balance }
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "operandos incompatíveis") {
		t.Fatalf("esperava erro de operandos incompatíveis:\n%s", r)
	}
}

// REQ-13.1 (positivo, argumento): construir um evento passando um argumento de tipo
// errado para um campo dispara o erro. Aqui o 2º argumento é WalletId onde o campo
// `amount` é Money.
func TestTypeCompatArgumentIncompatibleFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Event Deposited { id WalletId amount Money }
		Aggregate Wallet {
			state { id WalletId balance Money }
			Handle Deposit(amount Money) {
				emit Deposited(self.id, self.id)
			}
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "tipo incompatível") {
		t.Fatalf("esperava erro de argumento incompatível:\n%s", r)
	}
}
