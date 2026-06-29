package parser

import (
	"testing"

	"domainscript/ast"
)

func TestNotification(t *testing.T) {
	got := sdecl(parseDeclOK(t, `Notification DepositNotification { to Email, amount Money }`))
	want := "(Notification DepositNotification to:Email amount:Money)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestAdapterHTTP(t *testing.T) {
	src := `Adapter DepositNotification {
		mode async
		http POST "https://api.sendgrid.com/v3/mail/send"
		headers { "Authorization" = "Bearer x" }
		body { to = notification.to, subject = "Deposito recebido" }
	}`
	got := sdecl(parseDeclOK(t, src))
	want := `(Adapter DepositNotification mode=async http=POST "https://api.sendgrid.com/v3/mail/send"` +
		` headers{Authorization="Bearer x"}` +
		` body{to=(. notification to) subject="Deposito recebido"})`
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestAdapterFFI(t *testing.T) {
	src := `Adapter PaymentRequest {
		mode sync
		foreign "go" from "adapters/payment_gateway"
		function "ProcessPayment"
		map { paymentId = notification.paymentId, amount = notification.amount }
	}`
	got := sdecl(parseDeclOK(t, src))
	want := `(Adapter PaymentRequest mode=sync foreign="go" from="adapters/payment_gateway"` +
		` fn="ProcessPayment"` +
		` map{paymentId=(. notification paymentId) amount=(. notification amount)})`
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestForeign(t *testing.T) {
	src := `Foreign "go" from "internal/crypto" {
		function ComputeMerkleRoot(items List<bytes>) -> bytes
		function VerifySignature(message bytes, signature bytes, key string) -> boolean
	}`
	got := sdecl(parseDeclOK(t, src))
	want := `(Foreign "go" from="internal/crypto"` +
		` (fn ComputeMerkleRoot(items:List<bytes>) -> bytes)` +
		` (fn VerifySignature(message:bytes signature:bytes key:string) -> boolean))`
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestAdapterRecovers(t *testing.T) {
	p, bag := mk(`Adapter A { + + mode sync }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	a, ok := d.(*ast.AdapterDecl)
	if !ok {
		t.Fatalf("esperava AdapterDecl, veio %T", d)
	}
	if a.Mode != "sync" {
		t.Errorf("mode deveria ser reconhecido apesar do lixo; mode=%q", a.Mode)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
