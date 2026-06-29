package sema

import (
	"strings"
	"testing"
)

// REQ-5.15 (positivo): chamada a uma função foreign com aridade diferente da
// assinatura declarada dispara erro.
func TestForeignArityMismatchFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		Foreign "go" from "internal/crypto" {
			function ComputeMerkleRoot(items List<bytes>) -> bytes
		}
		Aggregate Wallet {
			state { id WalletId }
			access { Seal requires ok }
			Handle Seal() { hash = ComputeMerkleRoot(a, b) }
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "ComputeMerkleRoot") {
		t.Fatalf("esperava erro de assinatura foreign incompatível:\n%s", r)
	}
}

// REQ-5.15 (negativo): chamada com a aridade correta não dispara.
func TestForeignArityMatchIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		Foreign "go" from "internal/crypto" {
			function ComputeMerkleRoot(items List<bytes>) -> bytes
		}
		Aggregate Wallet {
			state { id WalletId }
			access { Seal requires ok }
			Handle Seal() { hash = ComputeMerkleRoot(items) }
		}
	`)
	mustBeSilent(t, bag, "REQ-5.15")
}
