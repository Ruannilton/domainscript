package sema

import (
	"strings"
	"testing"
)

// REQ-5.19 (positivo): VO wrapper sobre string validado contra um conjunto fechado
// de literais emite aviso de que poderia ser Enum.
func TestValueObjectAsEnumWarns(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Color(string) {
			Valid { value == "red" or value == "green" or value == "blue" }
		}
	`)
	r := bag.Render()
	if bag.HasErrors() {
		t.Fatalf("esperava aviso, não erro:\n%s", r)
	}
	if !strings.Contains(r, "Enum") || !strings.Contains(r, "warning") {
		t.Fatalf("esperava aviso de VO que poderia ser Enum:\n%s", r)
	}
}

// REQ-5.19 (negativo): VO com validação real (não um conjunto fechado de literais)
// não dispara.
func TestValueObjectWithRealValidationIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Email(string) {
			Valid { value.contains("@") }
		}
	`)
	mustBeSilent(t, bag, "REQ-5.19")
}
