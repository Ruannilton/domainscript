package sema

import (
	"strings"
	"testing"
)

// REQ-5.22 (positivo): Aggregate sob teste cujo Handle com caminho de erro de
// negócio só tem cenário de sucesso emite aviso de cobertura.
func TestHandleErrorWithoutErrorScenarioWarns(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money(integer) { Valid { ok } }
		Error InsufficientBalance { message "x" }
		Event WithdrawalPerformed { id WalletId }
		Command Withdraw { amount Money }
		Aggregate Wallet {
			state { id WalletId }
			access { Withdraw requires ok }
			Handle Withdraw(amount Money) { ensure cond else InsufficientBalance }
		}
		Test Wallet {
			scenario "saque ok" {
				when Withdraw(amount: Money(10))
				then [ WithdrawalPerformed(id: "W1") ]
			}
		}
	`)
	r := bag.Render()
	if bag.HasErrors() {
		t.Fatalf("esperava aviso, não erro:\n%s", r)
	}
	if !strings.Contains(r, "cobertura") || !strings.Contains(r, "Withdraw") {
		t.Fatalf("esperava aviso de cobertura de erro por Handle:\n%s", r)
	}
}

// REQ-5.22 (negativo): Handle com caminho de erro que tem um cenário `then error`
// correspondente não dispara.
func TestHandleErrorWithErrorScenarioIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money(integer) { Valid { ok } }
		Error InsufficientBalance { message "x" }
		Event WithdrawalPerformed { id WalletId }
		Command Withdraw { amount Money }
		Aggregate Wallet {
			state { id WalletId }
			access { Withdraw requires ok }
			Handle Withdraw(amount Money) { ensure cond else InsufficientBalance }
		}
		Test Wallet {
			scenario "saque ok" {
				when Withdraw(amount: Money(10))
				then [ WithdrawalPerformed(id: "W1") ]
			}
			scenario "saldo insuficiente" {
				when Withdraw(amount: Money(50))
				then error InsufficientBalance
			}
		}
	`)
	mustBeSilent(t, bag, "REQ-5.22")
}
