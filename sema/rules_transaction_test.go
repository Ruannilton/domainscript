package sema

import (
	"strings"
	"testing"
)

// REQ-5.9 (positivo cross-database): um UseCase que toca Aggregates geridos por
// bancos distintos sem suporte XA universal dispara erro — a transação não é
// atômica sem coordenação distribuída.
func TestCrossDatabaseWithoutXAFires(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Wallet {
			Database MainDb { provider: "pg" supportsXA: false manages: [Account] }
			Database SideDb { provider: "pg" supportsXA: false manages: [Ledger] }
		}`, "wallet", "mod.ds"),
		pf(`
			ValueObject AccId(string) { Valid { ok } }
			ValueObject LedId(string) { Valid { ok } }
			Aggregate Account { state { id AccId } }
			Aggregate Ledger { state { id LedId } }
			Command MoveCmd { from AccId }
			UseCase Move handles MoveCmd {
				execute {
					load Account(from)
					load Ledger(from)
				}
			}
		`, "wallet", "domain.ds"),
	)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "cross-database") || !strings.Contains(r, "Move") {
		t.Fatalf("esperava erro cross-database sem XA:\n%s", r)
	}
}

// REQ-5.9 (negativo cross-database): com XA em todos os bancos a transação é
// segura — silêncio. O UseCase é exposto numa interface para não disparar o aviso
// de exposição (REQ-5.23).
func TestCrossDatabaseWithXAIsSilent(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Wallet {
			Database MainDb { provider: "pg" supportsXA: true manages: [Account] }
			Database SideDb { provider: "pg" supportsXA: true manages: [Ledger] }
		}`, "wallet", "mod.ds"),
		pf(`
			ValueObject AccId(string) { Valid { ok } }
			ValueObject LedId(string) { Valid { ok } }
			Aggregate Account { state { id AccId } }
			Aggregate Ledger { state { id LedId } }
			Command MoveCmd { from AccId }
			UseCase Move handles MoveCmd {
				execute {
					load Account(from)
					load Ledger(from)
				}
			}
		`, "wallet", "domain.ds"),
		pf(`Interface HTTP { POST "/move" -> Move }`, "wallet", "interface.ds"),
	)
	mustBeSilent(t, bag, "REQ-5.9 cross-database")
}

// REQ-5.9 (positivo cross-service): um UseCase que toca Aggregates de services
// distintos não pode ser uma transação simples — exige Saga. O opt-in
// cross_tenant isola esta regra do erro de cross-tenant (REQ-5.12).
func TestCrossServiceWithoutSagaFires(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Wallet { Database WDb { provider: "pg" supportsXA: true manages: [Account] } }`, "wallet", "mod.ds"),
		pf(`
			ValueObject AccId(string) { Valid { ok } }
			Aggregate Account { state { id AccId } }
			Command MoveCmd { from AccId }
			UseCase Move handles MoveCmd {
				tenancy: cross_tenant
				execute {
					load Account(from)
					load Entry(from)
				}
			}
		`, "wallet", "domain.ds"),
		pf(`Module Ledger { Database LDb { provider: "pg" supportsXA: true manages: [Entry] } }`, "ledger", "mod.ds"),
		pf(`
			ValueObject EntId(string) { Valid { ok } }
			Aggregate Entry { state { id EntId } }
		`, "ledger", "domain.ds"),
		pf(`Topology {
			services {
				A { modules: [Wallet] }
				B { modules: [Ledger] }
			}
			channels {
				Wallet -> Ledger { via: grpc }
			}
		}`, "topology.ds"),
	)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "cross-service") || !strings.Contains(r, "Move") {
		t.Fatalf("esperava erro cross-service sem Saga:\n%s", r)
	}
}

// REQ-5.9 (negativo cross-service): a mesma operação cross-service modelada como
// Saga é válida — Sagas coordenam passos compensáveis, então a regra não dispara.
func TestCrossServiceWithSagaIsSilent(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Wallet { Database WDb { provider: "pg" supportsXA: true manages: [Account] } }`, "wallet", "mod.ds"),
		pf(`
			ValueObject AccId(string) { Valid { ok } }
			Aggregate Account { state { id AccId } }
			Command MoveCmd { from AccId }
			Saga Move handles MoveCmd {
				mode async
				state { id AccId }
				step Do {
					up {
						load Account(from)
						load Entry(from)
					}
				}
			}
		`, "wallet", "domain.ds"),
		pf(`Module Ledger { Database LDb { provider: "pg" supportsXA: true manages: [Entry] } }`, "ledger", "mod.ds"),
		pf(`
			ValueObject EntId(string) { Valid { ok } }
			Aggregate Entry { state { id EntId } }
		`, "ledger", "domain.ds"),
		pf(`Topology {
			services {
				A { modules: [Wallet] }
				B { modules: [Ledger] }
			}
			channels {
				Wallet -> Ledger { via: grpc }
			}
		}`, "topology.ds"),
	)
	mustBeSilent(t, bag, "REQ-5.9 cross-service")
}
