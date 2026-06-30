package sema

import (
	"strings"
	"testing"
)

// REQ-13.3 (coordenação com a Regra de Ouro, REQ-5.1): um campo primitivo no Write
// Side gera **um** diagnóstico (o da REQ-5.1), não dois. Sem a coordenação, a
// atribuição `state.balance = amount` (decimal ← Money) emitiria um segundo erro de
// tipo incompatível para a mesma causa-raiz (o campo deveria ser um ValueObject).
func TestTypeCompatWriteSidePrimitiveSingleDiagnostic(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Money { amount decimal currency string Valid { ok } }
		Aggregate Wallet {
			state { balance decimal }
			access { Deposit requires self.balance == self.balance }
			Handle Deposit(amount Money) {
				state.balance = amount
			}
		}
	`)
	r := bag.Render()
	if bag.Len() != 1 {
		t.Fatalf("esperava exatamente 1 diagnóstico (só a Regra de Ouro), obtive %d:\n%s", bag.Len(), r)
	}
	if !strings.Contains(r, "primitivo") || !strings.Contains(r, "Write Side") {
		t.Errorf("esperava o diagnóstico da Regra de Ouro (REQ-5.1):\n%s", r)
	}
	if strings.Contains(r, "tipo incompatível") {
		t.Errorf("não esperava um segundo diagnóstico de tipo para a mesma causa:\n%s", r)
	}
}
