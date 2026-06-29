package symbols

import "testing"

func TestDefineAndLookup(t *testing.T) {
	tab := New()
	sym, ok := tab.Define(&Symbol{Name: "Wallet", Kind: KindAggregate, Module: "wallet"})
	if !ok {
		t.Fatalf("primeira definição deveria ter sucesso")
	}
	got, found := tab.Lookup("wallet", "Wallet")
	if !found || got != sym {
		t.Errorf("Lookup(wallet, Wallet) = %v,%v; quero o símbolo definido", got, found)
	}
	if _, found := tab.Lookup("outro", "Wallet"); found {
		t.Errorf("símbolo privado não deveria ser visível de outro módulo")
	}
}

func TestDefineDuplicate(t *testing.T) {
	tab := New()
	first, _ := tab.Define(&Symbol{Name: "X", Kind: KindCommand, Module: "m"})
	existing, ok := tab.Define(&Symbol{Name: "X", Kind: KindEvent, Module: "m"})
	if ok {
		t.Fatalf("definição duplicada deveria falhar")
	}
	if existing != first {
		t.Errorf("duplicata deveria devolver o símbolo já registrado")
	}
	// O nome do mesmo módulo continua ligado à primeira definição.
	got, _ := tab.Lookup("m", "X")
	if got.Kind != KindCommand {
		t.Errorf("Kind após duplicata = %v; quero KindCommand (primeira definição preservada)", got.Kind)
	}
}

func TestSameNameDifferentModule(t *testing.T) {
	tab := New()
	if _, ok := tab.Define(&Symbol{Name: "Id", Kind: KindValueObject, Module: "a"}); !ok {
		t.Fatalf("definição em a deveria ter sucesso")
	}
	if _, ok := tab.Define(&Symbol{Name: "Id", Kind: KindValueObject, Module: "b"}); !ok {
		t.Errorf("mesmo nome em outro módulo não é duplicata")
	}
}

func TestPublicLevel(t *testing.T) {
	tab := New()
	tab.Define(&Symbol{Name: "OrderPlaced", Kind: KindEvent, Module: "order", Public: true})
	// Visível cross-module pelo nível público (REQ-7.4).
	if _, ok := tab.Lookup("billing", "OrderPlaced"); !ok {
		t.Errorf("PublicEvent deveria ser visível de outro módulo")
	}
	if _, ok := tab.LookupPublic("OrderPlaced"); !ok {
		t.Errorf("PublicEvent deveria estar no nível público")
	}

	tab.Define(&Symbol{Name: "Internal", Kind: KindEvent, Module: "order", Public: false})
	if _, ok := tab.LookupPublic("Internal"); ok {
		t.Errorf("Event privado não deveria estar no nível público")
	}
}
