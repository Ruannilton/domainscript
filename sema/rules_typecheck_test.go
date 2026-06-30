package sema

import (
	"strings"
	"testing"
)

// REQ-12.1/12.3 (positivo): acesso a um campo inexistente do receptor self dispara
// erro localizado no membro, com sugestão do campo mais próximo. Reproduz o bug
// `self.i` do Wallet: o state tem `id`, mas a condição de access usa `self.i`.
func TestMemberAccessUndeclaredFieldFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Aggregate Wallet {
			state { id WalletId balance Money }
			access { Withdraw requires self.i == self.id }
			Handle Withdraw(amount Money) { return }
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "membro inexistente") || !strings.Contains(r, `"i"`) {
		t.Fatalf("esperava erro de membro inexistente citando 'i':\n%s", r)
	}
	if !strings.Contains(r, `"id"`) {
		t.Errorf("esperava sugestão do campo mais próximo 'id':\n%s", r)
	}
}

// REQ-12 (negativo): o mesmo agregado com o campo correto (self.id) e acessos a
// state/event válidos fica em silêncio.
func TestMemberAccessCorrectIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Event DepositPerformed { id WalletId amount Money }
		Aggregate Wallet {
			state { id WalletId balance Money }
			access { Deposit requires self.id == self.id }
			Handle Deposit(amount Money) {
				emit DepositPerformed(self.id, amount)
			}
			Apply DepositPerformed {
				state.balance = event.amount
			}
		}
	`)
	mustBeSilent(t, bag, "REQ-12 (acesso correto)")
}

// REQ-12.2 (positivo): event.<campo inexistente> num Apply dispara erro — os
// receptores estáticos cobrem state/self/event.
func TestMemberAccessEventFieldFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Event DepositPerformed { id WalletId amount Money }
		Aggregate Wallet {
			state { id WalletId balance Money }
			Apply DepositPerformed {
				state.balance = event.amoun
			}
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "membro inexistente") || !strings.Contains(r, `"amoun"`) {
		t.Fatalf("esperava erro de membro inexistente citando 'amoun' em event:\n%s", r)
	}
}

// REQ-12.4 (anti-cascata): um receptor sem tipo estático conhecido (caller) e um
// valor de tipo com métodos (Enum/VO via '.') não geram falso positivo.
func TestMemberAccessConservativeNoFalsePositive(t *testing.T) {
	bag := checkSrc(t, `
		Enum Status: string { Active = "a" Closed = "c" }
		ValueObject Name(string) { Valid { value.length() > 0 } }
		Aggregate Acc {
			state { s Status }
			access { Step requires caller.authenticated }
			Handle Step(s Status) {
				match s {
					Status.Active => s.touch()
					Status.Closed => s.touch()
				}
			}
		}
	`)
	mustBeSilent(t, bag, "REQ-12.4 (conservador)")
}
