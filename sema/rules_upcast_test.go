package sema

import (
	"strings"
	"testing"
)

// REQ-5.18 (positivo): um Upcast cujo corpo só atribui constantes não é uma
// transformação — poderia ser um `default` no Event — e dispara aviso.
func TestUpcastReplaceableByDefaultFires(t *testing.T) {
	bag := checkSrc(t, `
		Upcast DepositPerformed v1 -> v2 {
			channel = Channel("unknown")
		}
	`)
	r := bag.Render()
	if bag.HasErrors() {
		t.Fatalf("a regra é um aviso, não deveria gerar erro:\n%s", r)
	}
	if !strings.Contains(r, "default") || !strings.Contains(r, "DepositPerformed") {
		t.Fatalf("esperava aviso de Upcast substituível por default citando o evento:\n%s", r)
	}
}

// REQ-5.18 (negativo): um Upcast que lê o evento de origem (`event.*`) é uma
// transformação legítima — o exemplo canônico do spec — e não dispara.
func TestUpcastWithTransformationSilent(t *testing.T) {
	bag := checkSrc(t, `
		Upcast TransferSent v1 -> v2 {
			fee = Money(amount: 0, currency: event.amount.currency)
		}
	`)
	mustBeSilent(t, bag, "REQ-5.18")
}
