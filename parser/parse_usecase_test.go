package parser

import (
	"testing"

	"domainscript/ast"
)

func TestUseCase(t *testing.T) {
	src := `UseCase PerformDeposit handles DepositCmd {
		timeout 5s
		execute {
			wallet = load Wallet(cmd.walletId)
			ensure wallet exists else WalletNotFound
			wallet.Deposit(cmd.amount, cmd.description)
		}
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(UseCase PerformDeposit handles=DepositCmd timeout=5s execute(block" +
		" (= wallet (load (call Wallet (. cmd walletId))))" +
		" (ensure (exists wallet) else WalletNotFound)" +
		" (call (. wallet Deposit) (. cmd amount) (. cmd description))))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestUseCaseTenancy(t *testing.T) {
	got := sdecl(parseDeclOK(t, `UseCase U handles C { tenancy: cross_tenant execute { return } }`))
	want := "(UseCase U handles=C tenancy=cross_tenant execute(block (return)))"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestUseCaseRecovers(t *testing.T) {
	p, bag := mk(`UseCase U handles C { + + execute { return } }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	uc, ok := d.(*ast.UseCaseDecl)
	if !ok {
		t.Fatalf("esperava UseCaseDecl, veio %T", d)
	}
	if uc.Execute == nil {
		t.Errorf("bloco execute deveria ter sido reconhecido apesar do lixo")
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
}
