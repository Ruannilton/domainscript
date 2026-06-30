package resolver

import (
	"strings"
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

// REQ-10.2 (B.2, positivo): uma referência de config a um símbolo inexistente
// dispara erro localizado. Reproduz o bug `Walle` do Wallet: o Database declara
// `manages: [Walle]`, mas o Aggregate chama-se `Wallet`.
func TestResolveConfigUndeclaredManagesFires(t *testing.T) {
	bag := resolveSrc(t, `
		Module Wallet {
			Database MainDb { provider: "postgres" manages: [Walle] }
		}
		ValueObject WalletId(string) { Valid { ok } }
		Aggregate Wallet { state { id WalletId } }
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "Walle") || !strings.Contains(r, "não declarada") {
		t.Fatalf("esperava erro de referência não declarada citando 'Walle':\n%s", r)
	}
}

// REQ-10 (B.2, negativo): a mesma config com o nome correto (`Wallet`) resolve em
// silêncio.
func TestResolveConfigValidManagesIsSilent(t *testing.T) {
	bag := resolveSrc(t, `
		Module Wallet {
			Database MainDb { provider: "postgres" manages: [Wallet] }
		}
		ValueObject WalletId(string) { Valid { ok } }
		Aggregate Wallet { state { id WalletId } }
	`)
	if bag.Len() != 0 {
		t.Fatalf("config correta não deveria gerar diagnósticos:\n%s", bag.Render())
	}
}

// REQ-10.3 (B.2, Kind divergente): `manages` apontando para um Event (não um
// Aggregate) dispara erro de esperado-vs-encontrado.
func TestResolveConfigWrongKindFires(t *testing.T) {
	bag := resolveSrc(t, `
		Module Wallet {
			Database MainDb { provider: "postgres" manages: [Opened] }
		}
		ValueObject WalletId(string) { Valid { ok } }
		Event Opened { id WalletId }
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "esperava Aggregate") || !strings.Contains(r, "encontrou Event") {
		t.Fatalf("esperava erro de Kind (esperava Aggregate, encontrou Event):\n%s", r)
	}
}

// REQ-10 (negativo, topologia): refs a módulos declarados resolvem em silêncio.
func TestResolveConfigDeclaredModulesSilent(t *testing.T) {
	bag := resolveSrc(t, `
		Module Carteira { }
		Module Ledger { }
		Topology {
			services { Svc { modules: [Carteira, Ledger] } }
			channels { Carteira -> Ledger { via: queue orderBy: id } }
		}
	`)
	if bag.Len() != 0 {
		t.Fatalf("topologia com módulos declarados não deveria gerar diagnósticos:\n%s", bag.Render())
	}
}

// REQ-10.2 (positivo, topologia): uma ref a um módulo não declarado dispara erro.
func TestResolveConfigUndeclaredModuleFires(t *testing.T) {
	bag := resolveSrc(t, `
		Module Carteira { }
		Topology {
			services { Svc { modules: [Carteira, Ledgr] } }
		}
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "Ledgr") || !strings.Contains(r, "esperava Module") {
		t.Fatalf("esperava erro de módulo não declarado citando 'Ledgr':\n%s", r)
	}
}
