package sema

import (
	"strings"
	"testing"
)

// REQ-5.8 (positivo): uma Policy que reage a um Event privado de outro módulo
// viola o encapsulamento do produtor — exige PublicEvent, dispara erro.
func TestPolicyOnPrivateCrossModuleEventFires(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Billing { }`, "billing", "mod.ds"),
		pf(`
			ValueObject OrderId(string) { Valid { ok } }
			Event InvoicePaid { id OrderId }
		`, "billing", "events.ds"),
		pf(`Module Shipping { }`, "shipping", "mod.ds"),
		pf(`Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`, "shipping", "policy.ds"),
	)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "PublicEvent") || !strings.Contains(r, "OnPaid") {
		t.Fatalf("esperava erro de Policy cross-module sobre Event privado:\n%s", r)
	}
}

// REQ-5.8 (negativo): a mesma Policy reagindo a um PublicEvent de outro módulo é
// consumo legítimo — silêncio.
func TestPolicyOnPublicCrossModuleEventIsSilent(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Billing { }`, "billing", "mod.ds"),
		pf(`
			ValueObject OrderId(string) { Valid { ok } }
			PublicEvent InvoicePaid { id OrderId }
		`, "billing", "events.ds"),
		pf(`Module Shipping { }`, "shipping", "mod.ds"),
		pf(`Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`, "shipping", "policy.ds"),
	)
	mustBeSilent(t, bag, "REQ-5.8")
}
