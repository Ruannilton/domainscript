package types

import (
	"testing"

	"domainscript/symbols"
)

func TestIdenticalNominal(t *testing.T) {
	cases := []struct {
		name string
		a, b Type
		want bool
	}{
		{"primitivo igual", &Primitive{"integer"}, &Primitive{"integer"}, true},
		{"primitivo difere", &Primitive{"integer"}, &Primitive{"string"}, false},
		{"VO nominal por nome", &VOType{Name: "Money"}, &VOType{Name: "Money"}, true},
		{"VO difere por nome", &VOType{Name: "Money"}, &VOType{Name: "Email"}, false},
		{"enum igual", &EnumType{Name: "Status"}, &EnumType{Name: "Status"}, true},
		{"shape igual", &ShapeType{Name: "Wallet", Kind: symbols.KindAggregate}, &ShapeType{Name: "Wallet"}, true},
		{"variantes distintas", &VOType{Name: "X"}, &ShapeType{Name: "X"}, false},
	}
	for _, c := range cases {
		if got := Identical(c.a, c.b); got != c.want {
			t.Errorf("%s: Identical = %v, quer %v", c.name, got, c.want)
		}
	}
}

func TestIdenticalStructural(t *testing.T) {
	intT := &Primitive{"integer"}
	strT := &Primitive{"string"}

	listInt := &Generic{Ctor: "List", Args: []Type{intT}}
	listInt2 := &Generic{Ctor: "List", Args: []Type{&Primitive{"integer"}}}
	listStr := &Generic{Ctor: "List", Args: []Type{strT}}

	if !Identical(listInt, listInt2) {
		t.Error("List<integer> deveria ser idêntico a List<integer>")
	}
	if Identical(listInt, listStr) {
		t.Error("List<integer> não deveria ser idêntico a List<string>")
	}

	mapA := &Generic{Ctor: "Map", Args: []Type{strT, intT}}
	mapB := &Generic{Ctor: "Map", Args: []Type{strT, strT}}
	if Identical(mapA, mapB) {
		t.Error("Map<string,integer> não deveria ser idêntico a Map<string,string>")
	}

	fnA := &FuncType{Params: []Type{intT}, Result: strT}
	fnB := &FuncType{Params: []Type{&Primitive{"integer"}}, Result: &Primitive{"string"}}
	if !Identical(fnA, fnB) {
		t.Error("assinaturas iguais deveriam ser idênticas")
	}
}

func TestErrorTypeSentinel(t *testing.T) {
	if !IsError(ErrorType) {
		t.Error("ErrorType deveria ser reconhecido por IsError")
	}
	if !IsError(nil) {
		t.Error("nil deveria ser tratado como erro")
	}
	if IsError(&Primitive{"integer"}) {
		t.Error("um primitivo não é tipo de erro")
	}
	if !Identical(ErrorType, ErrorType) {
		t.Error("ErrorType deveria ser idêntico a si mesmo")
	}
	if Identical(ErrorType, &Primitive{"integer"}) {
		t.Error("ErrorType não deveria ser idêntico a um primitivo")
	}
}

func TestAssignable(t *testing.T) {
	money := &VOType{Name: "Money"}
	walletId := &VOType{Name: "WalletId"}
	intT := &Primitive{"integer"}
	decT := &Primitive{"decimal"}
	strT := &Primitive{"string"}

	cases := []struct {
		name     string
		dst, src Type
		want     bool
	}{
		{"tipo idêntico", money, &VOType{Name: "Money"}, true},
		{"VO distinto", money, walletId, false},
		{"coerção integer→decimal", decT, intT, true},
		{"sem coerção decimal→integer", intT, decT, false},
		{"primitivos distintos", intT, strT, false},
		{"erro à esquerda absorve", ErrorType, money, true},
		{"erro à direita absorve", money, ErrorType, true},
	}
	for _, c := range cases {
		if got := Assignable(c.dst, c.src); got != c.want {
			t.Errorf("%s: Assignable(%s, %s) = %v, quer %v", c.name, c.dst, c.src, got, c.want)
		}
	}
}

func TestStringForms(t *testing.T) {
	cases := []struct {
		t    Type
		want string
	}{
		{&Primitive{"integer"}, "integer"},
		{&VOType{Name: "Money"}, "Money"},
		{&Generic{Ctor: "List", Args: []Type{&Primitive{"integer"}}}, "List<integer>"},
		{&Generic{Ctor: "Map", Args: []Type{&Primitive{"string"}, &Primitive{"integer"}}}, "Map<string, integer>"},
		{&FuncType{Params: []Type{&Primitive{"integer"}}, Result: &Primitive{"string"}}, "(integer) -> string"},
		{ErrorType, "<error>"},
	}
	for _, c := range cases {
		if got := c.t.String(); got != c.want {
			t.Errorf("String() = %q, quer %q", got, c.want)
		}
	}
}
