package types

import (
	"testing"

	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
	"domainscript/resolver"
	"domainscript/symbols"
)

// modelFrom parseia e resolve src no módulo "m" e devolve um Model sobre a tabela
// resultante. A entrada deve ser sintaticamente limpa.
func modelFrom(t *testing.T, src string) (*Model, *symbols.SymbolTable) {
	t.Helper()
	toks, lexBag := lexer.Lex(src)
	if len(lexBag) > 0 {
		t.Fatalf("erro léxico inesperado: %v", lexBag)
	}
	bag := diag.New()
	file := parser.Parse(toks, bag)
	if bag.HasErrors() {
		t.Fatalf("erro de sintaxe inesperado:\n%s", bag.Render())
	}
	r := resolver.New(bag)
	r.Add("m", file)
	r.ResolveAll()
	tab := r.Table()
	return NewModel(tab), tab
}

func lookup(t *testing.T, tab *symbols.SymbolTable, name string) *symbols.Symbol {
	t.Helper()
	sym, ok := tab.Lookup("m", name)
	if !ok {
		t.Fatalf("símbolo %q não encontrado na tabela", name)
	}
	return sym
}

// REQ-11.1: um Aggregate expõe os campos do seu state como membros — a base de
// self/state na checagem de membro (REQ-12.2).
func TestTypeOfAggregateExposesStateFields(t *testing.T) {
	m, tab := modelFrom(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
		Aggregate Wallet {
			state { id WalletId balance Money }
			Handle Open(owner WalletId) { return }
		}
	`)
	sh, ok := m.TypeOf(lookup(t, tab, "Wallet")).(*ShapeType)
	if !ok {
		t.Fatalf("TypeOf(Wallet) deveria ser *ShapeType, foi %T", m.TypeOf(lookup(t, tab, "Wallet")))
	}
	if sh.Kind != symbols.KindAggregate {
		t.Errorf("Kind = %v, quer Aggregate", sh.Kind)
	}
	mem := m.Members(sh)
	id, ok := mem["id"]
	if !ok {
		t.Fatalf("state.id deveria ser membro; membros: %v", mem)
	}
	if vo, ok := id.(*VOType); !ok || vo.Name != "WalletId" {
		t.Errorf("tipo de id = %v, quer WalletId", id)
	}
	if bal, ok := mem["balance"]; !ok || bal.String() != "Money" {
		t.Errorf("balance ausente ou tipo errado: %v", bal)
	}
	if _, ok := mem["inexistente"]; ok {
		t.Error("membro inexistente não deveria aparecer no catálogo")
	}
}

// REQ-11.1: VO composto expõe seus campos; VO wrapper carrega o tipo base e não
// tem campos.
func TestTypeOfValueObjectForms(t *testing.T) {
	m, tab := modelFrom(t, `
		ValueObject WalletId(string) { Valid { ok } }
		ValueObject Money { amount decimal currency string Valid { ok } }
	`)
	wrapper := m.TypeOf(lookup(t, tab, "WalletId")).(*VOType)
	if len(wrapper.Fields) != 0 {
		t.Errorf("VO wrapper não deveria ter campos, tem %d", len(wrapper.Fields))
	}
	if p, ok := wrapper.Base.(*Primitive); !ok || p.Name != "string" {
		t.Errorf("base do wrapper = %v, quer string", wrapper.Base)
	}

	comp := m.TypeOf(lookup(t, tab, "Money")).(*VOType)
	mem := m.Members(comp)
	if a, ok := mem["amount"]; !ok || a.String() != "decimal" {
		t.Errorf("amount ausente ou tipo errado: %v", a)
	}
	if _, ok := mem["currency"]; !ok {
		t.Error("currency deveria ser membro do VO composto")
	}
}

// REQ-11.1: Enum expõe seus membros, cada um com o tipo do próprio Enum.
func TestTypeOfEnumMembers(t *testing.T) {
	m, tab := modelFrom(t, `
		Enum Status: string { Active = "a" Closed = "c" }
	`)
	en, ok := m.TypeOf(lookup(t, tab, "Status")).(*EnumType)
	if !ok {
		t.Fatalf("TypeOf(Status) deveria ser *EnumType")
	}
	if len(en.Members) != 2 {
		t.Fatalf("esperava 2 membros, tem %d: %v", len(en.Members), en.Members)
	}
	mem := m.Members(en)
	if got, ok := mem["Active"]; !ok || got != Type(en) {
		t.Errorf("membro Active deveria ter o tipo do próprio Enum")
	}
}

// REQ-11.1: TypeOf memoiza — o mesmo símbolo devolve o mesmo ponteiro, e tipos
// recursivos (campo cujo tipo referencia o agregado) não causam laço infinito.
func TestTypeOfMemoizesAndHandlesRecursion(t *testing.T) {
	m, tab := modelFrom(t, `
		ValueObject Id(string) { Valid { ok } }
		Aggregate Node {
			state { id Id children List<Id> }
			Handle Touch(id Id) { return }
		}
	`)
	sym := lookup(t, tab, "Node")
	a := m.TypeOf(sym)
	b := m.TypeOf(sym)
	if a != b {
		t.Error("TypeOf deveria memoizar e devolver o mesmo ponteiro")
	}
	mem := m.Members(a)
	if g, ok := mem["children"].(*Generic); !ok || g.Ctor != "List" {
		t.Errorf("children deveria ser List<...>, foi %v", mem["children"])
	}
}

// REQ-11.3: um símbolo nil (referência não resolvida) vira tipo de erro.
func TestTypeOfNilIsError(t *testing.T) {
	m := NewModel(symbols.New())
	if !IsError(m.TypeOf(nil)) {
		t.Error("TypeOf(nil) deveria ser tipo de erro")
	}
}
