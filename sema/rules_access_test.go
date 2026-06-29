package sema

import (
	"strings"
	"testing"
)

const accessPreamble = `ValueObject WalletId(string) { Valid { ok } }`

// REQ-5.2 (positivo): Handle sem entrada no access dispara erro.
func TestHandleWithoutAccessFires(t *testing.T) {
	bag := checkSrc(t, accessPreamble+`
		Aggregate Wallet {
			state { id WalletId }
			access { Deposit requires caller.authenticated }
			Handle Deposit() { return }
			Handle Withdraw() { return }
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "Withdraw") || !strings.Contains(r, "access") {
		t.Fatalf("esperava erro de Handle Withdraw sem access:\n%s", r)
	}
}

// REQ-5.2 (negativo): todos os Handles com entrada em access não disparam.
func TestHandleWithAccessIsSilent(t *testing.T) {
	bag := checkSrc(t, accessPreamble+`
		Aggregate Wallet {
			state { id WalletId }
			access {
				Deposit  requires caller.authenticated
				Withdraw requires caller.authenticated
			}
			Handle Deposit() { return }
			Handle Withdraw() { return }
		}
	`)
	mustBeSilent(t, bag, "REQ-5.2")
}
