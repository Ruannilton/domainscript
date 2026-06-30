package resolver

import "testing"

// Define e Lookup no mesmo nível: um nome definido resolve; um ausente não.
func TestScopeDefineLookup(t *testing.T) {
	s := NewScope()
	s.Define("amount", Binding{Name: "amount", Kind: BindParam})

	b, ok := s.Lookup("amount")
	if !ok {
		t.Fatalf("esperava encontrar 'amount'")
	}
	if b.Name != "amount" || b.Kind != BindParam {
		t.Errorf("binding inesperado: %+v", b)
	}
	if _, ok := s.Lookup("inexistente"); ok {
		t.Errorf("nome não definido não deveria resolver")
	}
}

// Lookup sobe a cadeia: um filho enxerga os nomes do pai.
func TestScopeLookupSobeCadeia(t *testing.T) {
	root := NewScope()
	root.Define("self", Binding{Name: "self", Kind: BindReceiver})
	child := root.Child()

	if _, ok := child.Lookup("self"); !ok {
		t.Errorf("filho deveria enxergar o nome do pai")
	}
}

// Sombra: um nome redefinido num filho sombreia o do pai enquanto o filho existe.
func TestScopeShadow(t *testing.T) {
	root := NewScope()
	root.Define("t", Binding{Name: "t", Kind: BindParam})
	child := root.Child()
	child.Define("t", Binding{Name: "t", Kind: BindLocal})

	b, ok := child.Lookup("t")
	if !ok || b.Kind != BindLocal {
		t.Errorf("filho deveria ver a ligação local que sombreia o pai: %+v", b)
	}
	// O pai permanece intacto: a sombra é estrutural, não mutável.
	pb, _ := root.Lookup("t")
	if pb.Kind != BindParam {
		t.Errorf("o escopo pai não deveria ser afetado pela definição no filho")
	}
}

// Nome introduzido num filho some ao sair (descartar o filho) — REQ-9.5.
func TestScopeChildNameSomeAoSair(t *testing.T) {
	root := NewScope()
	child := root.Child()
	child.Define("i", Binding{Name: "i", Kind: BindLocal})

	if _, ok := child.Lookup("i"); !ok {
		t.Fatalf("o nome deveria existir dentro do filho")
	}
	// Descartar o filho = voltar a usar o root: o nome não está mais visível.
	if _, ok := root.Lookup("i"); ok {
		t.Errorf("nome de binder aninhado não deveria vazar para o escopo externo")
	}
}
