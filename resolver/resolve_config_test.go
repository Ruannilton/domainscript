package resolver

import (
	"testing"
)

// configRefsOf parseia src e devolve todas as refs de configuração extraídas de
// suas declarações (a metade de "extração" da Fase B), para inspeção no teste de
// catálogo.
func configRefsOf(t *testing.T, src string) []configRef {
	t.Helper()
	file := parseSrc(t, src)
	var refs []configRef
	for _, d := range file.Decls {
		refs = append(refs, collectConfigRefs(d)...)
	}
	return refs
}

// hasRef reporta se a lista contém uma ref com o nome e o rótulo esperado dados.
func hasRef(refs []configRef, name, label string) bool {
	for _, r := range refs {
		if r.name == name && r.expect.label == label {
			return true
		}
	}
	return false
}

// REQ-10.1 (B.1, catálogo): a extração reconhece cada sítio de referência de
// configuração com o alvo esperado correto — manages, Route.Target,
// ServiceDef.modules, ChannelDef.From/To e VersionRoute.Target.
func TestConfigRefCatalog(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		want  string // nome citado
		label string // alvo esperado
	}{
		{"manages", `Module M { Database Db { provider: "pg" manages: [Wallet] } }`, "Wallet", "Aggregate"},
		{"route", `Interface HTTP { POST "/x" -> PerformDeposit }`, "PerformDeposit", "UseCase ou Query"},
		{"rpc", `Interface GRPC { service S { rpc Do -> RunIt } }`, "RunIt", "UseCase ou Query"},
		{"modules", `Topology { services { Svc { modules: [Carteira] } } }`, "Carteira", "Module"},
		{"channelFrom", `Topology { channels { Carteira -> Ledger { via: queue } } }`, "Carteira", "Module"},
		{"channelTo", `Topology { channels { Carteira -> Ledger { via: queue } } }`, "Ledger", "Module"},
		{"versionRoute", `Version v1 { route "/x" -> PerformLegacyTransfer }`, "PerformLegacyTransfer", "UseCase"},
		{"upcast", `Version v1 { upcast DepositCmd { from { v decimal } to { a = v } } }`, "DepositCmd", "Command ou View"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			refs := configRefsOf(t, c.src)
			if !hasRef(refs, c.want, c.label) {
				t.Fatalf("esperava ref %q→%q em %q; obtive %+v", c.want, c.label, c.src, refs)
			}
		})
	}
}
