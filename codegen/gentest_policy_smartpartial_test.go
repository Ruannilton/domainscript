package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// gentest_policy_smartpartial_test.go prova o critério de conclusão
// comportamental de I6.1 (§design read-side 3.8, REQ-37): "distinct"/"sum"/
// "focus" (Smart Partial Loading, spec §20) geram Go que compila E se
// comporta como o spec descreve, sobre dados reais — não só o texto que
// codegen/lower/smartpartial_test.go já prova no nível de unidade.
//
// Fixture sintética NOVA (nem wallet, nem shop, nem Refunds/PriceCheck/
// Ranking): TicketSales. Ticket.orderId agrupa vendas por pedido (distinct),
// Ticket.price é um Money REAL (mesmo Operator + do wallet, docs/examples/
// wallet/domain.ds — sum exercita o dispatch fallível do Operator, não só a
// soma nativa), e a Policy foca um ticket específico por id (focus) — as
// TRÊS capacidades na MESMA Policy, sobre o resultado de um "list" anterior
// (a forma exata do spec §7: "soldTickets.distinct(...)").
const policySmartPartialFixtureModDs = `Module TicketSales { }
`

const policySmartPartialFixtureSrc = `
ValueObject TicketId(string) {
    Valid { value.length() > 0 }
}

ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

ValueObject Money {
    amount decimal
    currency string

    Valid { amount >= 0 }

    Operator +(other Money) -> Money {
        ensure currency == other.currency else CurrencyMismatch
        return Money(amount: amount + other.amount, currency: currency)
    }
}

Error CurrencyMismatch { message "As moedas comparadas não coincidem." }

ValueObject Ticket {
    id TicketId
    orderId OrderId
    price Money
}

Event SaleClosed {
    closedBy TicketId
}

Event DistinctOrderFound {
    orderId OrderId
}

Event TotalSalesComputed {
    total Money
}

Event TicketFocused {
    id TicketId
}

Policy SummarizeSales on SaleClosed {
    delivery BestEffort
    execute {
        tickets = list Ticket t
        orderIds = tickets.distinct(t => t.orderId)
        for oid in orderIds {
            emit DistinctOrderFound(orderId: oid)
        }
        total = tickets.sum(t => t.price)
        emit TotalSalesComputed(total: total)
        focused = tickets.focus(event.closedBy)
        emit TicketFocused(id: focused.id)
    }
}
`

// generatePolicySmartPartialFixtureProject roda o orquestrador COMPLETO
// (codegen.Generate) sobre o Program da fixture TicketSales — mesmo padrão
// de generatePolicyOrderByFixtureProject/generatePolicyPredicateFixtureProject:
// o teste comportamental abaixo aciona SummarizeSales de verdade, então
// precisa do projeto INTEIRO (ticketCollection/policyDispatcher inclusos).
func generatePolicySmartPartialFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    policySmartPartialFixtureModDs,
		"domain.ds": policySmartPartialFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de distinct/sum/focus (I6.1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de distinct/sum/focus: %v", err)
	}
	return files
}

// ticketSalesPolicyFile localiza, entre os arquivos gerados, o que carrega a
// Policy SummarizeSales (decl_policy.go emite um arquivo por módulo, pkg =
// goname.PackageName("TicketSales") = "ticketsales").
func ticketSalesPolicyFile(t *testing.T, files map[string][]byte) (path string, content []byte) {
	t.Helper()
	for p, c := range files {
		if strings.HasPrefix(p, "ticketsales/") && strings.HasSuffix(p, "policies.go") {
			return p, c
		}
	}
	var ks []string
	for k := range files {
		ks = append(ks, k)
	}
	t.Fatalf("esperava um arquivo \"ticketsales/*policies.go\" entre os arquivos gerados, não achei; arquivos: %v", ks)
	return "", nil
}

func emitPolicySmartPartialBodyFixture(t *testing.T) []byte {
	t.Helper()
	_, content := ticketSalesPolicyFile(t, filesToMap(generatePolicySmartPartialFixtureProject(t)))
	return content
}

// TestEmitPolicySmartPartialBodyGolden prova a forma Go gerada das três
// capacidades (§design read-side 3.8) no mesmo corpo.
func TestEmitPolicySmartPartialBodyGolden(t *testing.T) {
	got := string(emitPolicySmartPartialBodyFixture(t))
	for _, want := range []string{
		"map[OrderId]struct{}{}",      // distinct: mapa de vistos
		"for _, t := range tickets {", // distinct: laço sobre o resultado do list
		"var tmp",                     // sum: acumulador declarado fora do if
		"if len(tickets) > 0 {",       // sum: fold a partir do 1º item
		".Add(",                       // sum: dispatch do Operator + de Money
		"for i := range tickets {",    // focus: busca linear
		"break",                       // focus: para no 1º match
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q no corpo gerado de SummarizeSales, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "policy_ticketsales_smartpartial.go.golden"), []byte(got))
}

// TestEmitPolicySmartPartialDeterministic prova NFR-13: regerar duas vezes
// produz bytes idênticos.
func TestEmitPolicySmartPartialDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitPolicySmartPartialBodyFixture(t)
	})
}

// TestEmitPolicySmartPartialSmokeCompile prova que o projeto INTEIRO gerado
// compila e passa go vet num diretório isolado (NFR-14/NFR-17).
func TestEmitPolicySmartPartialSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generatePolicySmartPartialFixtureProject(t)))
}

// policySmartPartialHandwrittenTest é o harness ESCRITO À MÃO que aciona o
// código GERADO de verdade (mesma razão documentada em
// gentest_policy_predicate_test.go/gentest_policy_orderby_test.go: a fixture
// não passa pela máquina de *.test.ds). Semeia ticketCollection com 3
// Tickets — dois do MESMO pedido O1 (T1, T2) e um de O2 (T3), preços
// 10/5/7 BRL — e prova, na MESMA chamada:
//
//   - distinct: exatamente 2 DistinctOrderFound, na ordem de 1ª aparição
//     (O1 antes de O2 — T1, o primeiro ticket de O1, é adicionado ANTES de
//     T3, o único de O2; NFR-13, determinismo).
//   - sum: TotalSalesComputed carrega 22.0000 BRL (10+5+7) via Money.Add
//     (o Operator + fallível, dispatch de verdade) — não uma soma nativa.
//   - focus: TicketFocused aponta para o id EXATO do ticket buscado (T2, o
//     "closedBy" do evento) — nunca o 1º nem o 3º.
const policySmartPartialHandwrittenTest = `package ticketsales

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func seedTicket(t *testing.T, id, orderId string, amount int64, currency string) Ticket {
	t.Helper()
	price, err := NewMoney(runtime.NewDecimalFromInt(amount), currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q): %v", amount, currency, err)
	}
	return Ticket{Id: TicketId(id), OrderId: OrderId(orderId), Price: price}
}

// TestPolicySmartPartialHandwrittenRunGreen prova o critério de conclusão
// comportamental de I6.1: distinct/sum/focus, todos sobre o MESMO resultado
// de "list" (a forma exata da Policy §7 do spec), produzem os valores certos
// sobre dados reais.
func TestPolicySmartPartialHandwrittenRunGreen(t *testing.T) {
	ctx := context.Background()

	ticketCollection = runtime.NewMemoryCollection[Ticket]()
	for _, seed := range []struct {
		id, orderId string
		amount      int64
	}{
		{"T1", "O1", 10},
		{"T2", "O1", 5},
		{"T3", "O2", 7},
	} {
		if err := ticketCollection.Add(ctx, seedTicket(t, seed.id, seed.orderId, seed.amount, "BRL")); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}

	var published []runtime.Event
	policyDispatcher = runtime.NewDispatcher()
	for _, evName := range []string{"DistinctOrderFound", "TotalSalesComputed", "TicketFocused"} {
		policyDispatcher.Subscribe(evName, func(ctx context.Context, ev runtime.Event) error {
			published = append(published, ev)
			return nil
		})
	}

	ev := SaleClosed{ClosedBy: TicketId("T2")}
	if err := SummarizeSales(ctx, &ev); err != nil {
		t.Fatalf("esperava sucesso, got: %v", err)
	}

	var orderIds []OrderId
	var totals []Money
	var focused []TicketId
	for _, p := range published {
		switch e := p.(type) {
		case *DistinctOrderFound:
			orderIds = append(orderIds, e.OrderId)
		case *TotalSalesComputed:
			totals = append(totals, e.Total)
		case *TicketFocused:
			focused = append(focused, e.Id)
		}
	}

	// distinct: 2 pedidos, na ordem de 1ª aparição (O1 antes de O2).
	wantOrders := []OrderId{OrderId("O1"), OrderId("O2")}
	if len(orderIds) != len(wantOrders) {
		t.Fatalf("esperava %d DistinctOrderFound, got %d: %+v", len(wantOrders), len(orderIds), orderIds)
	}
	for i, want := range wantOrders {
		if orderIds[i] != want {
			t.Fatalf("orderIds[%d] = %v, want %v (ordem de 1ª aparição)", i, orderIds[i], want)
		}
	}

	// sum: 10+5+7 = 22.0000 BRL, via Money.Add (Operator + de verdade).
	if len(totals) != 1 {
		t.Fatalf("esperava 1 TotalSalesComputed, got %d: %+v", len(totals), totals)
	}
	wantTotal, err := NewMoney(runtime.NewDecimalFromInt(22), "BRL")
	if err != nil {
		t.Fatalf("NewMoney(22, BRL): %v", err)
	}
	// Money não é comparável com "==" (Amount é runtime.Decimal, backed por
	// big.Int, que embute um slice — a mesma razão por que distinct não
	// aceita decimal como chave, ver codegen/lower/smartpartial.go); compara
	// campo a campo, Amount via Decimal.Cmp.
	if totals[0].Currency != wantTotal.Currency || totals[0].Amount.Cmp(wantTotal.Amount) != 0 {
		t.Fatalf("total = %+v, want %+v", totals[0], wantTotal)
	}

	// focus: aponta para T2 (o "closedBy" do evento), nunca T1 nem T3.
	if len(focused) != 1 {
		t.Fatalf("esperava 1 TicketFocused, got %d: %+v", len(focused), focused)
	}
	if want := TicketId("T2"); focused[0] != want {
		t.Fatalf("focused = %v, want %v", focused[0], want)
	}
}
`

// TestPolicySmartPartialHandwrittenRunGreen roda `go test ./...` de VERDADE
// sobre o projeto gerado, com policySmartPartialHandwrittenTest injetado ao
// lado dos demais arquivos de ticketsales/ — path.Join usa sempre "/" (NUNCA
// filepath.Join, ver a nota em gentest_policy_predicate_test.go).
func TestPolicySmartPartialHandwrittenRunGreen(t *testing.T) {
	files := filesToMap(generatePolicySmartPartialFixtureProject(t))
	files["ticketsales/ticketsales_handwritten_test.go"] = []byte(policySmartPartialHandwrittenTest)
	runGeneratedTests(t, files)
}
