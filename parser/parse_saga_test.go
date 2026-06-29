package parser

import (
	"testing"

	"domainscript/ast"
)

func TestSaga(t *testing.T) {
	src := `Saga PurchaseTickets handles PurchaseTicketsCmd {
		mode await timeout 60s
		state { orderId OrderId, ticketIds List<TicketId> }
		step ReserveTickets {
			up { reserve() }
			down { for ticketId in state.ticketIds { release(ticketId) } }
			onInfraError { RetryWithBackoff(3) }
		}
		step ConfirmPurchase {
			up { confirm() }
			down { unrecoverable }
		}
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Saga PurchaseTickets handles=PurchaseTicketsCmd mode=await timeout=60s" +
		" state{orderId:OrderId ticketIds:List<TicketId>}" +
		" (step ReserveTickets up(block (call reserve))" +
		" down(block (for ticketId (. state ticketIds) (block (call release ticketId))))" +
		" onInfra(block (call RetryWithBackoff 3)))" +
		" (step ConfirmPurchase up(block (call confirm)) down(block unrecoverable)))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestSagaRecovers(t *testing.T) {
	p, bag := mk(`Saga S handles C { + + step X { up { return } } }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	s, ok := d.(*ast.SagaDecl)
	if !ok {
		t.Fatalf("esperava SagaDecl, veio %T", d)
	}
	if len(s.Steps) != 1 || s.Steps[0].Name != "X" {
		t.Errorf("step 'X' deveria ser reconhecido apesar do lixo")
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
