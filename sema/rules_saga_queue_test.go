package sema

import (
	"strings"
	"testing"
)

// REQ-5.17 (positivo): uma Saga em modo await que coordena por um canal queue
// pode bloquear até o timeout — aviso.
func TestSagaAwaitOverQueueWarns(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Orders { }`, "orders", "mod.ds"),
		pf(`
			ValueObject Ref(string) { Valid { ok } }
			Command PurchaseCmd { r Ref }
			Saga Purchase handles PurchaseCmd {
				mode await timeout 60s
				state { id Ref }
				step Do { up { return } }
			}
		`, "orders", "saga.ds"),
		pf(`Module Payments { }`, "payments", "mod.ds"),
		pf(`Topology {
			services {
				A { modules: [Orders] }
				B { modules: [Payments] }
			}
			channels {
				Orders -> Payments { via: queue orderBy: id }
			}
		}`, "topology.ds"),
	)
	r := bag.Render()
	if bag.HasErrors() {
		t.Fatalf("esperava apenas aviso, veio erro:\n%s", r)
	}
	if !strings.Contains(r, "await") || !strings.Contains(r, "Purchase") {
		t.Fatalf("esperava aviso de Saga await sobre queue:\n%s", r)
	}
}

// REQ-5.17 (negativo): a mesma Saga await sobre um canal síncrono (grpc) não
// bloqueia sobre fila — silêncio.
func TestSagaAwaitOverGrpcIsSilent(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Orders { }`, "orders", "mod.ds"),
		pf(`
			ValueObject Ref(string) { Valid { ok } }
			Command PurchaseCmd { r Ref }
			Saga Purchase handles PurchaseCmd {
				mode await timeout 60s
				state { id Ref }
				step Do { up { return } }
			}
		`, "orders", "saga.ds"),
		pf(`Module Payments { }`, "payments", "mod.ds"),
		pf(`Topology {
			services {
				A { modules: [Orders] }
				B { modules: [Payments] }
			}
			channels {
				Orders -> Payments { via: grpc }
			}
		}`, "topology.ds"),
	)
	mustBeSilent(t, bag, "REQ-5.17")
}
