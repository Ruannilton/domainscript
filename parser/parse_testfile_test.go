package parser

import (
	"testing"

	"domainscript/ast"
)

func parseTestDeclOK(t *testing.T, src string) *ast.TestDecl {
	t.Helper()
	d := parseDeclOK(t, src)
	td, ok := d.(*ast.TestDecl)
	if !ok {
		t.Fatalf("esperava TestDecl, veio %T", d)
	}
	return td
}

func TestTestAggregateScenarios(t *testing.T) {
	src := `Test Wallet {
		scenario "saque com saldo suficiente" {
			given [
				WalletCreated(id: "W1", holder: "João"),
				DepositPerformed(id: "W1", amount: Money(100, "BRL"))
			]
			when Withdraw(amount: Money(30, "BRL"), description: "Saque")
			then [ WithdrawalPerformed(id: "W1", amount: Money(30, "BRL")) ]
		}

		scenario "saque com saldo insuficiente" {
			given [ WalletCreated(id: "W1", holder: "João") ]
			when Withdraw(amount: Money(50, "BRL"), description: "Saque")
			then error InsufficientBalance
		}
	}`
	td := parseTestDeclOK(t, src)
	if td.Name != "Wallet" {
		t.Errorf("nome = %q", td.Name)
	}
	if len(td.Scenarios) != 2 {
		t.Fatalf("=> %d cenários, quero 2", len(td.Scenarios))
	}
	s0 := td.Scenarios[0]
	if s0.Name != "saque com saldo suficiente" {
		t.Errorf("scenario[0] = %q", s0.Name)
	}
	if len(s0.Givens) != 1 || len(s0.Givens[0].Entities) != 2 {
		t.Fatalf("given[0] = %v", s0.Givens)
	}
	if got := sexpr(s0.Givens[0].Entities[0].Entity); got != `(call WalletCreated id:"W1" holder:"João")` {
		t.Errorf("given entity => %s", got)
	}
	if s0.When == nil || sexpr(s0.When.Action) != `(call Withdraw amount:(call Money 30 "BRL") description:"Saque")` {
		t.Errorf("when => %v", s0.When)
	}
	if s0.Then == nil || len(s0.Then.Events) != 1 {
		t.Fatalf("then[0] = %v", s0.Then)
	}
	// Cenário com "then error".
	if td.Scenarios[1].Then.Error != "InsufficientBalance" {
		t.Errorf("then error = %q", td.Scenarios[1].Then.Error)
	}
}

func TestTestUseCaseThenBlock(t *testing.T) {
	src := `Test PerformTransfer {
		scenario "transferência bem-sucedida" {
			given Wallet("W1") from [ WalletCreated(id: "W1") ]
			given Wallet("W2") from [ WalletCreated(id: "W2") ]
			when TransferCmd(fromWalletId: "W1", toWalletId: "W2")
			then {
				Wallet("W1") emitted TransferSent(amount: Money(30, "BRL"))
				Wallet("W2") emitted TransferReceived(amount: Money(30, "BRL"))
				committed
			}
		}

		scenario "carteira inexistente faz rollback" {
			given Wallet("W1") from [ WalletCreated(id: "W1") ]
			when TransferCmd(fromWalletId: "W1", toWalletId: "W2")
			then { error WalletNotFound, rolledback }
		}
	}`
	td := parseTestDeclOK(t, src)
	s0 := td.Scenarios[0]
	if len(s0.Givens) != 2 {
		t.Fatalf("=> %d givens, quero 2", len(s0.Givens))
	}
	if sexpr(s0.Givens[0].Subject) != `(call Wallet "W1")` {
		t.Errorf("given subject => %s", sexpr(s0.Givens[0].Subject))
	}
	as := s0.Then.Asserts
	if len(as) != 3 {
		t.Fatalf("=> %d asserts, quero 3", len(as))
	}
	if as[0].Verb != "emitted" || sexpr(as[0].Subject) != `(call Wallet "W1")` || sexpr(as[0].Object) != `(call TransferSent amount:(call Money 30 "BRL"))` {
		t.Errorf("assert[0] = %+v", as[0])
	}
	if as[2].Verb != "committed" {
		t.Errorf("assert[2].Verb = %q, quero committed", as[2].Verb)
	}
	// Bloco then com error + rolledback separados por vírgula.
	r := td.Scenarios[1].Then.Asserts
	if len(r) != 2 || r[0].Error != "WalletNotFound" || r[1].Verb != "rolledback" {
		t.Errorf("then rollback = %+v", r)
	}
}

func TestTestSagaMockAndFailStep(t *testing.T) {
	src := `Test PurchaseTickets {
		scenario "falha de infra no step de confirmação" {
			mock PaymentRequest returns PaymentResult(status: PaymentStatus.Approved)
			fail step ConfirmPurchase with InfraError
			given Event("E1") from [ EventCreated(id: "E1") ]
			when PurchaseTicketsCmd(orderId: "O1")
			then {
				saga compensated
				compensated [ConfirmPurchase, ProcessPayment, ReserveTickets]
				called RefundRequest
			}
		}
	}`
	td := parseTestDeclOK(t, src)
	s := td.Scenarios[0]
	if len(s.Mocks) != 1 || sexpr(s.Mocks[0].Target) != "PaymentRequest" {
		t.Fatalf("mock = %v", s.Mocks)
	}
	if sexpr(s.Mocks[0].Returns) != "(call PaymentResult status:(. PaymentStatus Approved))" {
		t.Errorf("mock returns => %s", sexpr(s.Mocks[0].Returns))
	}
	if len(s.Fails) != 1 || s.Fails[0].Step != "ConfirmPurchase" || s.Fails[0].With != "InfraError" {
		t.Fatalf("fail = %v", s.Fails)
	}
	as := s.Then.Asserts
	if len(as) != 3 {
		t.Fatalf("=> %d asserts, quero 3", len(as))
	}
	if as[0].Verb != "saga" || sexpr(as[0].Object) != "compensated" {
		t.Errorf("assert[0] = %+v", as[0])
	}
	if as[1].Verb != "compensated" || len(as[1].List) != 3 {
		t.Errorf("assert[1] = %+v", as[1])
	}
	if as[2].Verb != "called" || sexpr(as[2].Object) != "RefundRequest" {
		t.Errorf("assert[2] = %+v", as[2])
	}
}

func TestTestPolicyEmittedCount(t *testing.T) {
	src := `Test RefundAll {
		scenario "reembolso" {
			given tickets [
				Ticket("T1") { eventId: "E1", status: TicketStatus.Sold },
				Ticket("T2") { eventId: "E1", status: TicketStatus.Sold }
			]
			when event EventCancelled(id: "E1", reason: "Chuva")
			then {
				emitted RefundRequested(orderId: "O1")
				emitted count 2
			}
		}
	}`
	td := parseTestDeclOK(t, src)
	s := td.Scenarios[0]
	g := s.Givens[0]
	if g.Binding != "tickets" || len(g.Entities) != 2 {
		t.Fatalf("given binding = %q entities=%d", g.Binding, len(g.Entities))
	}
	if g.Entities[0].State == nil || sexpr(g.Entities[0].State) != "{eventId:\"E1\" status:(. TicketStatus Sold)}" {
		t.Errorf("entity state => %v", g.Entities[0].State)
	}
	if s.When == nil || !s.When.IsEvent {
		t.Errorf("when deveria ser event: %v", s.When)
	}
	as := s.Then.Asserts
	if len(as) != 2 || as[0].Verb != "emitted" {
		t.Fatalf("asserts = %+v", as)
	}
	if as[1].Verb != "emitted" || sexpr(as[1].Count) != "2" {
		t.Errorf("emitted count => %+v", as[1])
	}
}

func TestTestProperty(t *testing.T) {
	src := `Test Wallet {
		property "saldo nunca fica negativo" {
			forall sequence of [Deposit, Withdraw, Transfer]
			invariant state.balance >= Money(0, "BRL")
		}
	}`
	td := parseTestDeclOK(t, src)
	if len(td.Properties) != 1 {
		t.Fatalf("=> %d properties, quero 1", len(td.Properties))
	}
	pr := td.Properties[0]
	if pr.Name != "saldo nunca fica negativo" {
		t.Errorf("property name = %q", pr.Name)
	}
	if sexpr(pr.Forall) != "[Deposit Withdraw Transfer]" {
		t.Errorf("forall => %s", sexpr(pr.Forall))
	}
	if sexpr(pr.Invariant) != "(>= (. state balance) (call Money 0 \"BRL\"))" {
		t.Errorf("invariant => %s", sexpr(pr.Invariant))
	}
}

func TestFixture(t *testing.T) {
	src := `Fixture activeWallet {
		Wallet("W1") from [
			WalletCreated(id: "W1", holder: "João"),
			DepositPerformed(id: "W1", amount: Money(100, "BRL"))
		]
	}`
	d := parseDeclOK(t, src)
	fx, ok := d.(*ast.FixtureDecl)
	if !ok {
		t.Fatalf("esperava FixtureDecl, veio %T", d)
	}
	if fx.Name != "activeWallet" {
		t.Errorf("nome = %q", fx.Name)
	}
	if len(fx.Givens) != 1 || sexpr(fx.Givens[0].Subject) != `(call Wallet "W1")` {
		t.Fatalf("givens = %v", fx.Givens)
	}
	if len(fx.Givens[0].Entities) != 2 {
		t.Errorf("=> %d entidades, quero 2", len(fx.Givens[0].Entities))
	}
}

func TestTestRecovers(t *testing.T) {
	p, bag := mk(`Test T { + + scenario "ok" { when C() then error E } }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	td, ok := d.(*ast.TestDecl)
	if !ok {
		t.Fatalf("esperava TestDecl, veio %T", d)
	}
	if len(td.Scenarios) != 1 || td.Scenarios[0].Then == nil || td.Scenarios[0].Then.Error != "E" {
		t.Errorf("cenário deveria ser reconhecido apesar do lixo; cenários=%v", td.Scenarios)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}

// TestTestThenState cobre a forma "then state { ... }" (§22.1, REQ-53.1):
// simétrica ao "given state { ... }" já suportado, ela popula ThenClause.State
// e deixa Error/Events/Asserts zerados.
func TestTestThenState(t *testing.T) {
	src := `Test Counter {
		scenario "incrementa" {
			given state { id: CounterId("C1"), value: Count(1) }
			when Increment(by: Count(2))
			then state { id: CounterId("C1"), value: Count(3) }
		}
	}`
	td := parseTestDeclOK(t, src)
	s := td.Scenarios[0]
	if len(s.Givens) != 1 || s.Givens[0].State == nil {
		t.Fatalf("given state => %v", s.Givens)
	}
	if s.Then == nil || s.Then.State == nil {
		t.Fatalf("then.State = nil; then=%v", s.Then)
	}
	if got, want := sexpr(s.Then.State), `{id:(call CounterId "C1") value:(call Count 3)}`; got != want {
		t.Errorf("then state => %s, quero %s", got, want)
	}
	if s.Then.Error != "" || len(s.Then.Events) != 0 || len(s.Then.Asserts) != 0 {
		t.Errorf("then deveria ter só State; got error=%q events=%d asserts=%d",
			s.Then.Error, len(s.Then.Events), len(s.Then.Asserts))
	}
}

// TestTestThenFormsUnaffectedByState é o par de não-regressão de
// TestTestThenState: as três formas de "then" que já existiam continuam
// parseando com State == nil.
func TestTestThenFormsUnaffectedByState(t *testing.T) {
	src := `Test Wallet {
		scenario "eventos" {
			when Withdraw(amount: Money(30, "BRL"))
			then [ WithdrawalPerformed(id: "W1") ]
		}
		scenario "erro" {
			when Withdraw(amount: Money(30, "BRL"))
			then error InsufficientBalance
		}
		scenario "bloco" {
			when Withdraw(amount: Money(30, "BRL"))
			then { committed }
		}
	}`
	td := parseTestDeclOK(t, src)
	if len(td.Scenarios) != 3 {
		t.Fatalf("=> %d cenários, quero 3", len(td.Scenarios))
	}
	for i, sc := range td.Scenarios {
		if sc.Then == nil {
			t.Fatalf("cenário[%d] sem then", i)
		}
		if sc.Then.State != nil {
			t.Errorf("cenário[%d]: State deveria ser nil, got %v", i, sc.Then.State)
		}
	}
	if len(td.Scenarios[0].Then.Events) != 1 {
		t.Errorf("then [eventos] => %d eventos", len(td.Scenarios[0].Then.Events))
	}
	if td.Scenarios[1].Then.Error != "InsufficientBalance" {
		t.Errorf("then error = %q", td.Scenarios[1].Then.Error)
	}
	if len(td.Scenarios[2].Then.Asserts) != 1 || td.Scenarios[2].Then.Asserts[0].Verb != "committed" {
		t.Errorf("then { ... } => %+v", td.Scenarios[2].Then.Asserts)
	}
}
