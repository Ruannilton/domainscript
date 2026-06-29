package parser

import (
	"testing"

	"domainscript/ast"
)

func TestCommand(t *testing.T) {
	got := sdecl(parseDeclOK(t, `Command DepositCmd { walletId ref Wallet, amount Money, description TransactionDescription }`))
	want := "(Command DepositCmd walletId:ref Wallet amount:Money description:TransactionDescription)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestCommandRecovers(t *testing.T) {
	p, bag := mk(`Command C { walletId ref Wallet + + amount Money }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	c, ok := d.(*ast.CommandDecl)
	if !ok {
		t.Fatalf("esperava CommandDecl, veio %T", d)
	}
	if len(c.Fields) == 0 || !c.Fields[0].Ref {
		t.Errorf("campo 'walletId ref Wallet' deveria ter sido reconhecido")
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
}
