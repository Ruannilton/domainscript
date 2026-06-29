package sema

import (
	"strings"
	"testing"
)

// REQ-5.16 (positivo): canal via queue sem orderBy emite aviso.
func TestChannelQueueWithoutOrderByWarns(t *testing.T) {
	bag := checkSrc(t, `
		Topology {
			services {
				A { modules: [M1] }
				B { modules: [M2] }
			}
			channels {
				A -> B {
					via: queue
					provider: "rabbitmq"
				}
			}
		}
	`)
	r := bag.Render()
	if bag.HasErrors() {
		t.Fatalf("esperava aviso, não erro:\n%s", r)
	}
	if !strings.Contains(r, "orderBy") || !strings.Contains(r, "warning") {
		t.Fatalf("esperava aviso de canal sem orderBy:\n%s", r)
	}
}

// REQ-5.16 (negativo): canal via queue com orderBy, e canal grpc sem orderBy, não
// disparam.
func TestChannelWithOrderByAndNonQueueIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		Topology {
			services {
				A { modules: [M1] }
				B { modules: [M2] }
			}
			channels {
				A -> B {
					via: queue
					orderBy: aggregateId
				}
				B -> A {
					via: grpc
				}
			}
		}
	`)
	mustBeSilent(t, bag, "REQ-5.16")
}
