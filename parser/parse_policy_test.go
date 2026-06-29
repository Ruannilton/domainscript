package parser

import (
	"testing"

	"domainscript/ast"
)

func TestPolicy(t *testing.T) {
	src := `Policy ExpireReservations on ReservationExpired {
		delivery AtLeastOnce
		execute {
			order = load Order(event.orderId)
			ensure order.state.status == OrderStatus.Pending else Nop
			order.Cancel("Reserva expirada")
		}
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Policy ExpireReservations on=ReservationExpired delivery=AtLeastOnce execute(block" +
		" (= order (load (call Order (. event orderId))))" +
		" (ensure (== (. (. order state) status) (. OrderStatus Pending)) else Nop)" +
		` (call (. order Cancel) "Reserva expirada")))`
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestPolicyRecovers(t *testing.T) {
	p, bag := mk(`Policy P on E { + + execute { return } }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	pol, ok := d.(*ast.PolicyDecl)
	if !ok {
		t.Fatalf("esperava PolicyDecl, veio %T", d)
	}
	if pol.Execute == nil {
		t.Errorf("execute deveria ser reconhecido apesar do lixo")
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
