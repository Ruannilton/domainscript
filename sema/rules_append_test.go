package sema

import (
	"strings"
	"testing"
)

const appendListPreamble = `
	ValueObject Entry(string) { Valid { ok } }
`

// REQ-5.4 (positivo): remove()/clear() sobre AppendList de state dispara erro.
func TestAppendListRemoveFires(t *testing.T) {
	bag := checkSrc(t, appendListPreamble+`
		Aggregate Ledger {
			state { entries AppendList<Entry> }
			Handle Wipe() { state.entries.clear() }
			Handle Drop() { state.entries.remove(0) }
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "AppendList") {
		t.Fatalf("esperava erro de mutação em AppendList:\n%s", r)
	}
	if bag.Len() != 2 {
		t.Errorf("esperava 2 erros (clear e remove), obtive %d:\n%s", bag.Len(), r)
	}
}

// REQ-5.4 (negativo): add() em AppendList, e remove() sobre outra coleção, não
// disparam.
func TestAppendListAddIsSilent(t *testing.T) {
	bag := checkSrc(t, appendListPreamble+`
		Aggregate Ledger {
			state { entries AppendList<Entry> history List<Entry> }
			Handle Append() { state.entries.add(Entry("x")) }
			Handle Trim() { state.history.remove(0) }
		}
	`)
	mustBeSilent(t, bag, "REQ-5.4")
}
