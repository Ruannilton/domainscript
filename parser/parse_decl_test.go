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

func sparams(ps []*ast.Field) string {
	s := "("
	for i, pm := range ps {
		if i > 0 {
			s += " "
		}
		s += sfield(pm)
	}
	return s + ")"
}

func soperator(o *ast.OperatorDecl) string {
	return "(op " + o.Op + " " + sparams(o.Params) + " " + stype(o.Return) + " " + sstmt(o.Body) + ")"
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
	case *ast.AggregateDecl:
		s := "(Aggregate " + n.Name
		if n.Strategy != "" {
			s += " strat=" + n.Strategy
		}
		if n.Snapshot != nil {
			s += " snap=" + sexpr(n.Snapshot)
		}
		for _, st := range n.Storage {
			s += " store[" + st.Key + ":" + st.Value + "]"
		}
		if len(n.State) > 0 {
			s += " state{"
			for i, f := range n.State {
				if i > 0 {
					s += " "
				}
				s += sfield(f)
			}
			s += "}"
		}
		for _, a := range n.Access {
			s += " acc[" + a.Name + " " + sexpr(a.Condition) + "]"
		}
		for _, h := range n.Handlers {
			s += " (Handle " + h.Name + sparams(h.Params) + " " + sstmt(h.Body) + ")"
		}
		for _, ap := range n.Appliers {
			s += " (Apply " + ap.Event + " " + sstmt(ap.Body) + ")"
		}
		return s + ")"
	case *ast.CommandDecl:
		s := "(Command " + n.Name
		for _, f := range n.Fields {
			s += " " + sfield(f)
		}
		return s + ")"
	case *ast.UseCaseDecl:
		s := "(UseCase " + n.Name
		if n.Handles != "" {
			s += " handles=" + n.Handles
		}
		if n.Timeout != nil {
			s += " timeout=" + sexpr(n.Timeout)
		}
		if n.Idempotency != nil {
			s += " idem=" + sexpr(n.Idempotency)
		}
		if n.Tenancy != "" {
			s += " tenancy=" + n.Tenancy
		}
		if n.Execute != nil {
			s += " execute" + sstmt(n.Execute)
		}
		return s + ")"
	case *ast.ViewDecl:
		s := "(View " + n.Name
		if n.From != "" {
			s += " from=" + n.From
		}
		for _, f := range n.Fields {
			s += " " + sfield(f)
		}
		for _, v := range n.Visibility {
			s += " vis[" + v.Name + " " + sexpr(v.Condition) + "]"
		}
		return s + ")"
	case *ast.ProjectionDecl:
		s := "(Projection " + n.Name
		if len(n.Sources) > 0 {
			s += " src["
			for i, src := range n.Sources {
				if i > 0 {
					s += " "
				}
				s += src
			}
			s += "]"
		}
		for _, m := range n.Map {
			s += " " + m.Name + "=" + sexpr(m.Value)
		}
		if n.RefreshOn != nil {
			s += " refreshOn=" + sexpr(n.RefreshOn)
		}
		return s + ")"
	case *ast.QueryDecl:
		s := "(Query " + n.Name + sparams(n.Params)
		if n.Return != nil {
			s += " -> " + stype(n.Return)
		}
		for _, c := range n.Cache {
			s += " cache[" + c.Key + "=" + sexpr(c.Value) + "]"
		}
		if n.Body != nil {
			s += " " + sstmt(n.Body)
		}
		return s + ")"
	case *ast.PolicyDecl:
		s := "(Policy " + n.Name
		if n.On != "" {
			s += " on=" + n.On
		}
		if n.Delivery != "" {
			s += " delivery=" + n.Delivery
		}
		if n.Execute != nil {
			s += " execute" + sstmt(n.Execute)
		}
		return s + ")"
	case *ast.WorkerDecl:
		s := "(Worker " + n.Name
		if n.Schedule != "" {
			s += " sched=" + n.Schedule
			if n.ScheduleArg != nil {
				s += "/" + sexpr(n.ScheduleArg)
			}
		}
		if n.Scope != "" {
			s += " scope=" + n.Scope
		}
		for _, c := range n.Settings {
			s += " set[" + c.Key + "=" + sexpr(c.Value) + "]"
		}
		if n.Source != nil {
			s += " source" + sstmt(n.Source)
		}
		if n.Execute != nil {
			s += " exec(" + n.ExecuteParam + ")" + sstmt(n.Execute)
		}
		return s + ")"
	case *ast.NotificationDecl:
		s := "(Notification " + n.Name
		for _, f := range n.Fields {
			s += " " + sfield(f)
		}
		return s + ")"
	case *ast.AdapterDecl:
		s := "(Adapter " + n.Name
		if n.Mode != "" {
			s += " mode=" + n.Mode
		}
		if n.HTTPMethod != "" {
			s += " http=" + n.HTTPMethod + " " + sexpr(n.HTTPUrl)
		}
		if len(n.Headers) > 0 {
			s += " headers" + smap(n.Headers)
		}
		if len(n.Body) > 0 {
			s += " body" + smap(n.Body)
		}
		if n.Lang != nil {
			s += " foreign=" + sexpr(n.Lang)
			if n.From != nil {
				s += " from=" + sexpr(n.From)
			}
		}
		if n.Function != nil {
			s += " fn=" + sexpr(n.Function)
		}
		if len(n.Map) > 0 {
			s += " map" + smap(n.Map)
		}
		return s + ")"
	case *ast.ForeignDecl:
		s := "(Foreign " + sexpr(n.Lang)
		if n.From != nil {
			s += " from=" + sexpr(n.From)
		}
		for _, fn := range n.Functions {
			s += " (fn " + fn.Name + sparams(fn.Params)
			if fn.Return != nil {
				s += " -> " + stype(fn.Return)
			}
			s += ")"
		}
		return s + ")"
	case *ast.SagaDecl:
		s := "(Saga " + n.Name
		if n.Handles != "" {
			s += " handles=" + n.Handles
		}
		if n.Mode != "" {
			s += " mode=" + n.Mode
		}
		if n.Timeout != nil {
			s += " timeout=" + sexpr(n.Timeout)
		}
		if len(n.State) > 0 {
			s += " state{"
			for i, f := range n.State {
				if i > 0 {
					s += " "
				}
				s += sfield(f)
			}
			s += "}"
		}
		for _, st := range n.Steps {
			s += " (step " + st.Name
			if st.Up != nil {
				s += " up" + sstmt(st.Up)
			}
			if st.Down != nil {
				s += " down" + sstmt(st.Down)
			}
			if st.OnInfraError != nil {
				s += " onInfra" + sstmt(st.OnInfraError)
			}
			s += ")"
		}
		return s + ")"
	case *ast.ErrorDecl:
		return "<errdecl>"
	default:
		return "?decl"
	}
}

func smap(es []ast.MapEntry) string {
	s := "{"
	for i, e := range es {
		if i > 0 {
			s += " "
		}
		s += e.Name + "=" + sexpr(e.Value)
	}
	return s + "}"
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
