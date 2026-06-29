package parser

import (
	"testing"

	"domainscript/ast"
)

func TestEnumString(t *testing.T) {
	got := sdecl(parseDeclOK(t, `Enum TransactionType : string { Deposit = "DEPOSIT" Withdrawal = "WITHDRAWAL" }`))
	want := `(Enum TransactionType:string Deposit="DEPOSIT" Withdrawal="WITHDRAWAL")`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestEnumInteger(t *testing.T) {
	got := sdecl(parseDeclOK(t, `Enum Priority : integer { Low = 1 Medium = 2 High = 3 }`))
	want := "(Enum Priority:integer Low=1 Medium=2 High=3)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestEnumCoerce(t *testing.T) {
	src := `Enum PaymentMethod : string {
		CreditCard = "CREDIT_CARD"
		Pix = "PIX"
		coerce from string {
			match self.uppercase() {
				"CREDIT_CARD", "CC" => CreditCard
				"PIX" => Pix
				_ => InvalidPaymentMethodError
			}
		}
	}`
	got := sdecl(parseDeclOK(t, src))
	want := `(Enum PaymentMethod:string CreditCard="CREDIT_CARD" Pix="PIX"` +
		` (coerce string (block (matchS (call (. self uppercase))` +
		` (arm ["CREDIT_CARD","CC"] CreditCard) (arm ["PIX"] Pix) (arm [_] InvalidPaymentMethodError)))))`
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

// Recovery: membro malformado não impede o reconhecimento dos demais.
func TestEnumRecovers(t *testing.T) {
	p, bag := mk(`Enum E : string { A = "a" + + B = "b" }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	e, ok := d.(*ast.EnumDecl)
	if !ok {
		t.Fatalf("esperava EnumDecl, veio %T", d)
	}
	if len(e.Members) == 0 || e.Members[0].Name != "A" {
		t.Errorf("membro 'A' deveria ter sido reconhecido")
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
}
