package sema

import (
	"strings"
	"testing"
)

// REQ-5.12 (positivo): um UseCase que acessa um Aggregate de outro módulo, sem
// declarar `tenancy: cross_tenant`, sai do seu contexto isolado sem opt-in — erro.
func TestCrossTenantWithoutOptInFires(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Orders { }`, "orders", "mod.ds"),
		pf(`
			ValueObject Ref(string) { Valid { ok } }
			Command ReportCmd { r Ref }
			UseCase Report handles ReportCmd {
				execute {
					list Wallet take 10
				}
			}
		`, "orders", "domain.ds"),
		pf(`Module Wallet { }`, "wallet", "mod.ds"),
		pf(`
			ValueObject WalletId(string) { Valid { ok } }
			Aggregate Wallet { state { id WalletId } }
		`, "wallet", "domain.ds"),
		pf(`Topology {
			services {
				S { modules: [Orders, Wallet] }
			}
		}`, "topology.ds"),
	)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "cross_tenant") || !strings.Contains(r, "Report") {
		t.Fatalf("esperava erro de acesso cross-tenant sem opt-in:\n%s", r)
	}
}

// REQ-5.12 (negativo): um UseCase que só acessa Aggregates do próprio módulo fica
// no seu bounded context — nenhum opt-in é exigido, silêncio.
func TestSameTenantAccessIsSilent(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Orders { }`, "orders", "mod.ds"),
		pf(`
			ValueObject Ref(string) { Valid { ok } }
			ValueObject OrderId(string) { Valid { ok } }
			Aggregate Order { state { id OrderId } }
			Command ReportCmd { r Ref }
			UseCase Report handles ReportCmd {
				execute {
					list Order take 10
				}
			}
		`, "orders", "domain.ds"),
		pf(`Interface HTTP { GET "/report" -> Report }`, "orders", "interface.ds"),
	)
	mustBeSilent(t, bag, "REQ-5.12")
}
