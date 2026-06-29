package parser

import (
	"testing"

	"domainscript/ast"
)

func TestErrorDecl(t *testing.T) {
	got := sdecl(parseDeclOK(t, `Error InsufficientBalance { message "Saldo insuficiente." }`))
	want := `(Error InsufficientBalance "Saldo insuficiente.")`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestEventCommaSeparated(t *testing.T) {
	got := sdecl(parseDeclOK(t, `Event WalletCreated { id WalletId, holder HolderName, email Email }`))
	want := "(Event WalletCreated id:WalletId holder:HolderName email:Email)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestPublicEvent(t *testing.T) {
	got := sdecl(parseDeclOK(t, `PublicEvent DepositPerformed { id WalletId, amount Money }`))
	want := "(PublicEvent DepositPerformed id:WalletId amount:Money)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestEventRedactableAndDefault(t *testing.T) {
	src := `Event WalletCreated {
		id WalletId
		holder HolderName redactable
		channel Channel = Channel("unknown")
	}`
	got := sdecl(parseDeclOK(t, src))
	want := `(Event WalletCreated id:WalletId holder:HolderName redactable channel:Channel=(call Channel "unknown"))`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestEventRecovers(t *testing.T) {
	p, bag := mk(`Event E { id WalletId + + amount Money }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	ev, ok := d.(*ast.EventDecl)
	if !ok {
		t.Fatalf("esperava EventDecl, veio %T", d)
	}
	if len(ev.Fields) == 0 || ev.Fields[0].Name != "id" {
		t.Errorf("campo 'id' deveria ter sido reconhecido")
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
}
