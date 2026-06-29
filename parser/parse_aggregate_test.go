package parser

import (
	"testing"

	"domainscript/ast"
)

func TestAggregateFull(t *testing.T) {
	src := `Aggregate Wallet {
		strategy EventSourced
		snapshot every 50 events
		state { balance Money active ActiveStatus }
		access { Deposit requires caller.authenticated }
		Handle Deposit(amount Money) {
			ensure ok else InactiveWallet
			emit DepositPerformed(self.id, amount)
		}
		Apply DepositPerformed {
			state.balance = state.balance + event.amount
		}
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Aggregate Wallet strat=EventSourced snap=50 state{balance:Money active:ActiveStatus}" +
		" acc[Deposit (. caller authenticated)]" +
		" (Handle Deposit(amount:Money) (block (ensure ok else InactiveWallet) (emit (call DepositPerformed (. self id) amount))))" +
		" (Apply DepositPerformed (block (= (. state balance) (+ (. state balance) (. event amount))))))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestAggregateStorage(t *testing.T) {
	src := `Aggregate Person {
		storage { state: PersonDb, document: DocumentStorage }
		state { document FileRef }
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Aggregate Person store[state:PersonDb] store[document:DocumentStorage] state{document:FileRef})"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestAggregateRecovers(t *testing.T) {
	p, bag := mk(`Aggregate A { state { x Money } + + Handle H(a B) { return } }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	agg, ok := d.(*ast.AggregateDecl)
	if !ok {
		t.Fatalf("esperava AggregateDecl, veio %T", d)
	}
	if len(agg.Handlers) != 1 || agg.Handlers[0].Name != "H" {
		t.Errorf("Handle 'H' deveria ser reconhecido apesar do lixo; handlers=%v", agg.Handlers)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
}
