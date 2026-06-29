package sema

import (
	"strings"
	"testing"
)

// REQ-5.11 (positivo): uma Policy que reage a um evento de um módulo em outro
// service, sem canal declarado na topologia, dispara erro — a fronteira de rede
// precisa de transporte explícito.
func TestCrossServiceWithoutChannelFires(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Billing { }`, "billing", "mod.ds"),
		pf(`
			ValueObject OrderId(string) { Valid { ok } }
			PublicEvent OrderPlaced { id OrderId }
		`, "billing", "events.ds"),
		pf(`Module Shipping { }`, "shipping", "mod.ds"),
		pf(`Policy NotifyShip on OrderPlaced { delivery AtLeastOnce execute { return } }`, "shipping", "policy.ds"),
		pf(`Topology {
			services {
				A { modules: [Billing] }
				B { modules: [Shipping] }
			}
		}`, "topology.ds"),
	)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "sem canal") || !strings.Contains(r, "NotifyShip") {
		t.Fatalf("esperava erro de módulos sem canal entre services:\n%s", r)
	}
}

// REQ-5.11 (negativo): com o canal declarado entre os services a comunicação é
// válida — silêncio.
func TestCrossServiceWithChannelIsSilent(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Billing { }`, "billing", "mod.ds"),
		pf(`
			ValueObject OrderId(string) { Valid { ok } }
			PublicEvent OrderPlaced { id OrderId }
		`, "billing", "events.ds"),
		pf(`Module Shipping { }`, "shipping", "mod.ds"),
		pf(`Policy NotifyShip on OrderPlaced { delivery AtLeastOnce execute { return } }`, "shipping", "policy.ds"),
		pf(`Topology {
			services {
				A { modules: [Billing] }
				B { modules: [Shipping] }
			}
			channels {
				Billing -> Shipping { via: queue orderBy: id }
			}
		}`, "topology.ds"),
	)
	mustBeSilent(t, bag, "REQ-5.11")
}
