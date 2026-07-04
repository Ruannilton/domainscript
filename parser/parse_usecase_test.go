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

// TestUseCaseIdempotencyObject prova a correção desta task (G2, achado ao
// implementar REQ-20.4): "idempotency { ... }" é a ÚNICA forma que o spec usa
// (§14 — "idempotency { required: true, window: 48h }"), mas o parser, antes
// desta correção, só sabia ler "idempotency" seguido de p.parseExpr() — que
// nunca reconheceu um objeto "{ ... }" (essa forma sempre falhava com
// "esperava uma expressão, encontrei {"). idempotency agora usa
// p.parseConfigValue() (parse_config.go), a mesma função que mod.ds/Worker.
// onError já usam para "Key { Object }" — mudança aditiva, não muda nenhuma
// forma que já parseava (ex. TestUseCase acima continua idêntico).
func TestUseCaseIdempotencyObject(t *testing.T) {
	src := `UseCase PerformDeposit handles DepositCmd {
		idempotency { required: true, window: 48h }
		execute { return }
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(UseCase PerformDeposit handles=DepositCmd idem={required:true window:48h} execute(block (return)))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

// TestUseCaseIdempotencyRequiredFalse prova a 2ª forma do spec §14 ("idempotency
// { required: false } // operação naturalmente idempotente").
func TestUseCaseIdempotencyRequiredFalse(t *testing.T) {
	src := `UseCase MarkAsRead handles MarkAsReadCmd {
		idempotency { required: false }
		execute { return }
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(UseCase MarkAsRead handles=MarkAsReadCmd idem={required:false} execute(block (return)))"
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
