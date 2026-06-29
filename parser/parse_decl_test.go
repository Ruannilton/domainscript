package parser

import (
	"testing"

	"domainscript/ast"
)

// --- renderizadores de declaração para asserções ---

func stype(t *ast.TypeRef) string {
	if t == nil {
		return "?"
	}
	s := t.Name
	if len(t.Args) > 0 {
		s += "<"
		for i, a := range t.Args {
			if i > 0 {
				s += ","
			}
			s += stype(a)
		}
		s += ">"
	}
	return s
}

func sfield(f *ast.Field) string {
	s := f.Name + ":"
	if f.Ref {
		s += "ref "
	}
	s += stype(f.Type)
	if f.Redactable {
		s += " redactable"
	}
	if f.Default != nil {
		s += "=" + sexpr(f.Default)
	}
	return s
}

func soperator(o *ast.OperatorDecl) string {
	s := "(op " + o.Op + " ("
	for i, pm := range o.Params {
		if i > 0 {
			s += " "
		}
		s += sfield(pm)
	}
	return s + ") " + stype(o.Return) + " " + sstmt(o.Body) + ")"
}

func sdecl(d ast.Decl) string {
	switch n := d.(type) {
	case *ast.ValueObjectDecl:
		s := "(ValueObject " + n.Name
		if n.Base != nil {
			s += "(" + stype(n.Base) + ")"
		}
		for _, f := range n.Fields {
			s += " " + sfield(f)
		}
		if n.Valid != nil {
			s += " Valid" + sstmt(n.Valid)
		}
		for _, o := range n.Operators {
			s += " " + soperator(o)
		}
		return s + ")"
	case *ast.EnumDecl:
		s := "(Enum " + n.Name
		if n.Base != nil {
			s += ":" + stype(n.Base)
		}
		for _, m := range n.Members {
			s += " " + m.Name + "=" + sexpr(m.Value)
		}
		if n.Coerce != nil {
			s += " (coerce " + stype(n.Coerce.From) + " " + sstmt(n.Coerce.Body) + ")"
		}
		return s + ")"
	case *ast.ErrorTypeDecl:
		s := "(Error " + n.Name
		if n.Message != nil {
			s += " " + sexpr(n.Message)
		}
		return s + ")"
	case *ast.EventDecl:
		kw := "Event"
		if n.Public {
			kw = "PublicEvent"
		}
		s := "(" + kw + " " + n.Name
		for _, f := range n.Fields {
			s += " " + sfield(f)
		}
		return s + ")"
	case *ast.ErrorDecl:
		return "<errdecl>"
	default:
		return "?decl"
	}
}

func parseDeclOK(t *testing.T, src string) ast.Decl {
	t.Helper()
	p, bag := mk(src)
	d := p.parseDecl()
	if bag.Len() != 0 {
		t.Fatalf("parseDecl(%q) gerou diagnósticos: %s", src, bag.Render())
	}
	if !p.atEnd() {
		t.Fatalf("parseDecl(%q) não consumiu tudo; parou em %v", src, p.cur().Kind)
	}
	return d
}

func TestValueObjectWrapper(t *testing.T) {
	got := sdecl(parseDeclOK(t, `ValueObject Email(string) { Valid { self.contains("@") } }`))
	want := `(ValueObject Email(string) Valid(block (call (. self contains) "@")))`
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestValueObjectComposite(t *testing.T) {
	src := `ValueObject Money {
		amount decimal
		currency string
		Valid { amount >= 0 }
		Operator +(other Money) -> Money {
			return Money(amount: self.amount + other.amount, currency: self.currency)
		}
		Operator >=(other Money) -> boolean { return self.amount >= other.amount }
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(ValueObject Money amount:decimal currency:string Valid(block (>= amount 0))" +
		" (op + (other:Money) Money (block (return (call Money amount:(+ (. self amount) (. other amount)) currency:(. self currency)))))" +
		" (op >= (other:Money) boolean (block (return (>= (. self amount) (. other amount))))))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestValueObjectGenericField(t *testing.T) {
	got := sdecl(parseDeclOK(t, `ValueObject Bag { items AppendList<StatementEntry> meta Map<string, string> }`))
	want := "(ValueObject Bag items:AppendList<StatementEntry> meta:Map<string,string>)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

// Recovery: campo malformado no corpo não impede o resto de ser parseado nem
// trava o parser.
func TestValueObjectRecovers(t *testing.T) {
	p, bag := mk(`ValueObject V { amount decimal + + currency string }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico para corpo malformado")
	}
	vo, ok := d.(*ast.ValueObjectDecl)
	if !ok {
		t.Fatalf("esperava ValueObjectDecl, veio %T", d)
	}
	if !p.atEnd() {
		t.Errorf("parser não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
	// O primeiro campo válido foi reconhecido apesar do lixo seguinte.
	if len(vo.Fields) == 0 || vo.Fields[0].Name != "amount" {
		t.Errorf("campo 'amount' deveria ter sido reconhecido; campos=%v", vo.Fields)
	}
}
