package parser

import (
	"testing"

	"domainscript/ast"
)

func TestMetricCounter(t *testing.T) {
	src := `Metric DepositVolume {
		type counter
		value event.amount.amount
		on DepositPerformed
		labels { currency = event.amount.currency }
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Metric DepositVolume type=counter value=(. (. event amount) amount)" +
		" on=DepositPerformed labels{currency=(. (. event amount) currency)})"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestMetricHistogram(t *testing.T) {
	src := `Metric PurchaseLatency {
		type histogram
		buckets [100ms, 250ms, 1s]
		on PurchaseTickets.completed
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Metric PurchaseLatency type=histogram buckets=[100ms 250ms 1s] on=(. PurchaseTickets completed))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestUpcast(t *testing.T) {
	src := `Upcast TransferSent v1 -> v2 {
		fee = Money(amount: 0, currency: event.amount.currency)
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Upcast TransferSent v1->v2 (block (= fee (call Money amount:0 currency:(. (. event amount) currency)))))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestUpcastRecovers(t *testing.T) {
	p, bag := mk(`Upcast E v1 -> v2 { + + fee = X() }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	u, ok := d.(*ast.UpcastDecl)
	if !ok {
		t.Fatalf("esperava UpcastDecl, veio %T", d)
	}
	if u.FromVer != "v1" || u.ToVer != "v2" {
		t.Errorf("versões deveriam ser v1->v2; veio %s->%s", u.FromVer, u.ToVer)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
