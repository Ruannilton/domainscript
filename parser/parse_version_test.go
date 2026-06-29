package parser

import (
	"testing"

	"domainscript/ast"
)

func TestVersionFull(t *testing.T) {
	src := `Version v1 {
		deprecated: "2026-01-01"
		sunset: "2026-06-01"

		upcast DepositCmd {
			from { value decimal, currency string, description string }
			to {
				amount = Money(amount: value, currency: currency)
				description = TransactionDescription(description)
				channel = Channel("legacy")
			}
		}

		downcast WalletSummaryVW {
			to {
				balance = self.balance_amount
				owner = self.holder
			}
		}

		route "/wallets/{walletId}/transfer" -> PerformLegacyTransfer
	}`
	d := parseDeclOK(t, src)
	v, ok := d.(*ast.VersionDecl)
	if !ok {
		t.Fatalf("esperava VersionDecl, veio %T", d)
	}
	if v.Version != "v1" {
		t.Errorf("version = %q, quero v1", v.Version)
	}
	if sexpr(v.Deprecated) != `"2026-01-01"` || sexpr(v.Sunset) != `"2026-06-01"` {
		t.Errorf("deprecated=%v sunset=%v", v.Deprecated, v.Sunset)
	}
	if len(v.Upcasts) != 1 {
		t.Fatalf("=> %d upcasts, quero 1", len(v.Upcasts))
	}
	up := v.Upcasts[0]
	if up.Target != "DepositCmd" {
		t.Errorf("upcast target = %q", up.Target)
	}
	if len(up.From) != 3 || up.From[0].Name != "value" || stype(up.From[0].Type) != "decimal" {
		t.Errorf("upcast from = %v", up.From)
	}
	if len(up.To) != 3 || up.To[0].Name != "amount" {
		t.Errorf("upcast to = %v", up.To)
	}
	if got := sexpr(up.To[0].Value); got != "(call Money amount:value currency:currency)" {
		t.Errorf("upcast to[0] => %s", got)
	}
	if len(v.Downcasts) != 1 || v.Downcasts[0].Target != "WalletSummaryVW" {
		t.Fatalf("downcasts = %v", v.Downcasts)
	}
	if len(v.Routes) != 1 || v.Routes[0].Path != "/wallets/{walletId}/transfer" || v.Routes[0].Target != "PerformLegacyTransfer" {
		t.Errorf("routes = %v", v.Routes)
	}
}

// contracts/*.ds contém apenas PublicEvent, já reconhecido pelo roteamento de
// topo (REQ-2.2): aqui só confirmamos que parseia num arquivo de contrato.
func TestContractsPublicEvent(t *testing.T) {
	file, bag := parseFileSrc(`PublicEvent WalletCreated { id WalletId, holder string }`)
	if bag.Len() != 0 {
		t.Fatalf("diagnósticos inesperados: %s", bag.Render())
	}
	if len(file.Decls) != 1 {
		t.Fatalf("=> %d declarações, quero 1", len(file.Decls))
	}
	ev, ok := file.Decls[0].(*ast.EventDecl)
	if !ok || !ev.Public {
		t.Fatalf("esperava PublicEvent, veio %T (public=%v)", file.Decls[0], ok)
	}
}

func TestVersionRecovers(t *testing.T) {
	p, bag := mk(`Version v2 { + + route "/x" -> X }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	v, ok := d.(*ast.VersionDecl)
	if !ok {
		t.Fatalf("esperava VersionDecl, veio %T", d)
	}
	if len(v.Routes) != 1 || v.Routes[0].Target != "X" {
		t.Errorf("route deveria ser reconhecida apesar do lixo; routes=%v", v.Routes)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
