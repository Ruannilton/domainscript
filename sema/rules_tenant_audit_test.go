package sema

import (
	"strings"
	"testing"
)

// REQ-5.21 (positivo): um UseCase com `tenancy: cross_tenant` emite aviso de
// auditoria — o opt-in privilegiado deve ser consciente.
func TestCrossTenantDeclaredWarns(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Ref(string) { Valid { ok } }
		Command C { r Ref }
		UseCase U handles C { tenancy: cross_tenant execute { return } }
	`)
	r := bag.Render()
	if bag.HasErrors() {
		t.Fatalf("esperava apenas aviso, veio erro:\n%s", r)
	}
	if !strings.Contains(r, "cross_tenant") || !strings.Contains(r, "auditoria") {
		t.Fatalf("esperava aviso de auditoria cross_tenant:\n%s", r)
	}
}

// REQ-5.21 (negativo): um UseCase sem o opt-in não gera o aviso — silêncio.
func TestNonCrossTenantIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Ref(string) { Valid { ok } }
		Command C { r Ref }
		UseCase U handles C { execute { return } }
	`)
	mustBeSilent(t, bag, "REQ-5.21")
}
