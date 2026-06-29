package sema

import (
	"strings"
	"testing"
)

// REQ-5.13 (positivo): upcast que não atribui um campo obrigatório (sem default)
// do Command-alvo dispara erro.
func TestUpcastMissingRequiredFieldFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Money(integer) { Valid { ok } }
		ValueObject Note(string) { Valid { ok } }
		Command DepositCmd { amount Money, note Note }
		Version v1 {
			upcast DepositCmd {
				from { value integer }
				to { amount = Money(value) }
			}
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "note") {
		t.Fatalf("esperava erro de campo obrigatório sem default no upcast:\n%s", r)
	}
}

// REQ-5.13 (negativo): upcast que atribui todos os campos obrigatórios não
// dispara.
func TestUpcastWithAllRequiredFieldsIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Money(integer) { Valid { ok } }
		ValueObject Note(string) { Valid { ok } }
		Command DepositCmd { amount Money, note Note }
		Version v1 {
			upcast DepositCmd {
				from { value integer }
				to {
					amount = Money(value)
					note = Note("legacy")
				}
			}
		}
	`)
	mustBeSilent(t, bag, "REQ-5.13")
}
