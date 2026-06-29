package parser

import (
	"testing"

	"domainscript/ast"
)

func TestViewFrom(t *testing.T) {
	got := sdecl(parseDeclOK(t, `View WalletSummaryVW From Wallet`))
	if got != "(View WalletSummaryVW from=Wallet)" {
		t.Errorf("=> %s", got)
	}
}

func TestViewFields(t *testing.T) {
	got := sdecl(parseDeclOK(t, `View StatementEntryVW { type string, amount_value decimal, date datetime }`))
	want := "(View StatementEntryVW type:string amount_value:decimal date:datetime)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestViewVisibility(t *testing.T) {
	src := `View WalletSummaryVW From Wallet {
		visibility {
			balance requires caller.id == self.id or caller.hasRole("admin")
			holder requires caller.authenticated
		}
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(View WalletSummaryVW from=Wallet" +
		` vis[balance (or (== (. caller id) (. self id)) (call (. caller hasRole) "admin"))]` +
		" vis[holder (. caller authenticated)])"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestProjection(t *testing.T) {
	src := `Projection InvoiceWithHolderVW {
		source Invoice, Wallet
		map { invoiceId = Invoice.id, holder = Wallet.holder }
		refreshOn [InvoiceCreated, WalletUpdated]
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Projection InvoiceWithHolderVW src[Invoice Wallet]" +
		" invoiceId=(. Invoice id) holder=(. Wallet holder)" +
		" refreshOn=[InvoiceCreated WalletUpdated])"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestQueryWithCache(t *testing.T) {
	src := `Query GetWalletSummary(walletId WalletId) -> WalletSummaryVW {
		cache { ttl: 5min, negativeCacheTtl: 10s }
		return load Wallet(walletId) as WalletSummaryVW
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Query GetWalletSummary(walletId:WalletId) -> WalletSummaryVW" +
		" cache[ttl=5min] cache[negativeCacheTtl=10s]" +
		" (block (return (load (call Wallet walletId) {as WalletSummaryVW}))))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestQueryRecovers(t *testing.T) {
	p, bag := mk(`Query Q(x A) -> B { + + return load Wallet(x) }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	if _, ok := d.(*ast.QueryDecl); !ok {
		t.Fatalf("esperava QueryDecl, veio %T", d)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
}
