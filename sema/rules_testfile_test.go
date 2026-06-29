package sema

import (
	"strings"
	"testing"
)

// REQ-5.14 (positivo): teste que referencia um evento inexistente em given
// dispara erro.
func TestTestFileUndeclaredEventFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		Event WalletCreated { id WalletId }
		Command Withdraw { id WalletId }
		Test Wallet {
			scenario "saque" {
				given [ WalletCreatedTYPO(id: "W1") ]
				when Withdraw(id: "W1")
			}
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "WalletCreatedTYPO") {
		t.Fatalf("esperava erro de evento inexistente no teste:\n%s", r)
	}
}

// REQ-5.14 (positivo): `fail step X` onde X não é step do Saga dispara erro.
func TestTestFileUnknownFailStepFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		Command PurchaseCmd { id WalletId }
		Saga Purchase handles PurchaseCmd {
			mode async
			state { id WalletId }
			step ReserveTickets { up { ok } }
		}
		Test Purchase {
			scenario "falha de infra" {
				fail step Inexistente with InfraError
				when PurchaseCmd(id: "W1")
			}
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "Inexistente") || !strings.Contains(r, "fail step") {
		t.Fatalf("esperava erro de fail step inexistente:\n%s", r)
	}
}

// REQ-5.14 (negativo): teste consistente (eventos/comandos declarados, fail step
// válido, then error existente) não dispara.
func TestTestFileConsistentIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		Error InfraReject { message "x" }
		Command PurchaseCmd { id WalletId }
		Saga Purchase handles PurchaseCmd {
			mode async
			state { id WalletId }
			step ReserveTickets { up { ok } }
		}
		Test Purchase {
			scenario "falha de infra" {
				fail step ReserveTickets with InfraError
				when PurchaseCmd(id: "W1")
				then error InfraReject
			}
		}
	`)
	mustBeSilent(t, bag, "REQ-5.14")
}
