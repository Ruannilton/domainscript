package codegen

import (
	"reflect"
	"testing"
)

func TestIdent(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"type", "type_"},
		{"range", "range_"},
		{"func", "func_"},
		{"amount", "amount"},
		{"Wallet", "Wallet"},
	}
	for _, c := range cases {
		if got := Ident(c.in); got != c.want {
			t.Errorf("Ident(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOperatorMethod(t *testing.T) {
	cases := []struct {
		op   string
		want string
	}{
		{"+", "Add"},
		{"-", "Sub"},
		{"*", "Mul"},
		{"/", "Div"},
		{">=", "Gte"},
		{"<=", "Lte"},
		{">", "Gt"},
		{"<", "Lt"},
		{"==", "Eq"},
		{"!=", "Neq"},
	}
	for _, c := range cases {
		got, ok := OperatorMethod(c.op)
		if !ok {
			t.Errorf("OperatorMethod(%q) ok = false, want true", c.op)
		}
		if got != c.want {
			t.Errorf("OperatorMethod(%q) = %q, want %q", c.op, got, c.want)
		}
	}

	if _, ok := OperatorMethod("%%"); ok {
		t.Errorf("OperatorMethod(%%%%) ok = true, want false")
	}
}

func TestEnumConstName(t *testing.T) {
	if got := EnumConstName("TransactionType", "Deposit"); got != "TransactionTypeDeposit" {
		t.Errorf("EnumConstName = %q, want %q", got, "TransactionTypeDeposit")
	}
}

func TestModuleAggregateSameName(t *testing.T) {
	// Module Wallet + Aggregate Wallet: pacote "wallet", tipo "Wallet" —
	// namespaces diferentes, sem colisão (nunca passam pelo mesmo Dedupe).
	if got := PackageName("Wallet"); got != "wallet" {
		t.Errorf("PackageName(Wallet) = %q, want %q", got, "wallet")
	}
	if got := Ident("Wallet"); got != "Wallet" {
		t.Errorf("Ident(Wallet) = %q, want %q (identidade)", got, "Wallet")
	}
}

func TestPackageName(t *testing.T) {
	if got := PackageName("Carteira"); got != "carteira" {
		t.Errorf("PackageName(Carteira) = %q, want %q", got, "carteira")
	}
}

func TestDedupeRegister(t *testing.T) {
	d := NewDedupe()
	first := d.Register("Deposit")
	second := d.Register("Deposit")
	third := d.Register("Deposit")

	if first != "Deposit" {
		t.Errorf("1st Register = %q, want %q", first, "Deposit")
	}
	if second != "Deposit2" {
		t.Errorf("2nd Register = %q, want %q", second, "Deposit2")
	}
	if third != "Deposit3" {
		t.Errorf("3rd Register = %q, want %q", third, "Deposit3")
	}
}

func TestDedupeIndependentPerInstance(t *testing.T) {
	d1 := NewDedupe()
	d2 := NewDedupe()

	if got := d1.Register("Wallet"); got != "Wallet" {
		t.Errorf("d1.Register(Wallet) = %q, want %q", got, "Wallet")
	}
	if got := d2.Register("Wallet"); got != "Wallet" {
		t.Errorf("d2.Register(Wallet) = %q, want %q (instância separada)", got, "Wallet")
	}
}

func TestQualifiedRef(t *testing.T) {
	if got := QualifiedRef("contracts", "OrderPlaced"); got != "contracts.OrderPlaced" {
		t.Errorf("QualifiedRef = %q, want %q", got, "contracts.OrderPlaced")
	}
}

func TestExportField(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"amount", "Amount"},
		{"id", "Id"},
		{"balance", "Balance"},
		{"", ""},
	}
	for _, c := range cases {
		if got := ExportField(c.in); got != c.want {
			t.Errorf("ExportField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestJSONTag(t *testing.T) {
	tag := JSONTag("amount")

	// Confirma o formato exato via reflect.StructTag: uma tag Go válida
	// colável após o tipo do campo (ex. `Amount runtime.Decimal `+tag).
	st := reflect.StructTag(tag[1 : len(tag)-1]) // remove os backticks
	if got := st.Get("json"); got != "amount" {
		t.Errorf("JSONTag(%q).Get(json) = %q, want %q", "amount", got, "amount")
	}
	if len(tag) < 2 || tag[0] != '`' || tag[len(tag)-1] != '`' {
		t.Errorf("JSONTag(%q) = %q, want value wrapped in backticks", "amount", tag)
	}
}
