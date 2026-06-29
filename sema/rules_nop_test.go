package sema

import (
	"strings"
	"testing"
)

// REQ-5.6 (positivo): Nop como ação num Handle dispara erro.
func TestNopInHandleFires(t *testing.T) {
	bag := checkSrc(t, `
		Error Inactive { message "x" }
		Aggregate Wallet {
			state { id WalletId }
			access { Freeze requires caller.authenticated }
			Handle Freeze() { ensure active else Nop }
		}
		ValueObject WalletId(string) { Valid { ok } }
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "Nop") {
		t.Fatalf("esperava erro de Nop em Handle:\n%s", r)
	}
}

// REQ-5.6 (positivo): Nop num UseCase dispara erro.
func TestNopInUseCaseFires(t *testing.T) {
	bag := checkSrc(t, `
		Command Cmd { id WalletId }
		UseCase UC handles Cmd { execute { ensure cond else Nop } }
		ValueObject WalletId(string) { Valid { ok } }
	`)
	if !bag.HasErrors() || !strings.Contains(bag.Render(), "Nop") {
		t.Fatalf("esperava erro de Nop em UseCase:\n%s", bag.Render())
	}
}

// REQ-5.6 (negativo): Handle/UseCase com ações reais (ensure else Error) não
// disparam.
func TestNopSilentWithRealActions(t *testing.T) {
	bag := checkSrc(t, `
		Error Inactive { message "x" }
		Aggregate Wallet {
			state { id WalletId }
			access { Freeze requires caller.authenticated }
			Handle Freeze() { ensure active else Inactive }
		}
		ValueObject WalletId(string) { Valid { ok } }
	`)
	mustBeSilent(t, bag, "REQ-5.6")
}
