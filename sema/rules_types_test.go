package sema

import (
	"strings"
	"testing"
)

// REQ-5.1 (positivo): primitivo direto num Command do Write Side dispara erro.
func TestWriteSidePrimitiveFires(t *testing.T) {
	bag := checkSrc(t, `Command DepositCmd { amount decimal }`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "decimal") || !strings.Contains(r, "Write Side") {
		t.Fatalf("esperava erro de primitivo no Write Side citando decimal:\n%s", r)
	}
}

// REQ-5.1 (positivo): vale também para Aggregate (state) e Event.
func TestWriteSidePrimitiveAggregateAndEvent(t *testing.T) {
	agg := checkSrc(t, `Aggregate Wallet { state { balance decimal } }`)
	if !agg.HasErrors() {
		t.Errorf("esperava erro de primitivo no state do Aggregate:\n%s", agg.Render())
	}
	evt := checkSrc(t, `Event WalletOpened { at datetime }`)
	if !evt.HasErrors() {
		t.Errorf("esperava erro de primitivo num Event:\n%s", evt.Render())
	}
}

// REQ-5.1 (negativo): campos com ValueObject/Enum e coleções de domínio não
// disparam — primitivos são permitidos dentro do ValueObject.
func TestWriteSidePrimitiveSilentWithValueObjects(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Money { amount decimal currency string Valid { ok } }
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Entry(string) { Valid { ok } }
		Command DepositCmd { walletId ref Wallet amount Money }
		Aggregate Wallet { state { id WalletId entries AppendList<Entry> } }
		Event WalletOpened { id WalletId amount Money }
	`)
	mustBeSilent(t, bag, "REQ-5.1")
}
