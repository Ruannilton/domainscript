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

// gentest_policy_test.go prova os critérios de conclusão da 6ª (e última)
// fatia de H4 (§design codegen 3.14, REQ-31.3, §22.4): um Test cujo Name
// resolve a um *ast.PolicyDecl deste módulo vira um arquivo Go de teste que
// roda `go test` verde — mesmo alvo de conclusão que gentest_test.go/
// gentest_saga_test.go já provam para Aggregate/UseCase/Saga (§22.1/22.2/
// 22.3), agora para "given <binding> [...]" (semeando runtime.Collection[T]),
// "when event Evento(...)" (chamando a Policy DIRETO) e "then { emitted
// Evento(...), emitted count N }" (ver a doc de gentest.go sobre o mecanismo
// completo).
//
// --- Por que uma fixture sintética NOVA (nem wallet, nem shop) ---
//
// Investigação registrada em tasks.md (H4, "Policy/Query investigado e
// adiado"): nem o wallet nem o shop têm uma Policy real que exercite list/
// count/emit — a ÚNICA Policy real de hoje (shop, NotifyShipping) tem corpo
// "execute { return }" (docs/examples/shop/shipping/policy.ds). Mesmo
// precedente já usado por CADA fatia anterior de H4 que precisou de um corpo
// real e não tinha um: Saga sintetizou "Booking/PurchaseTickets"
// (gentest_saga_test.go), property dropou o "Transfer" ilustrativo do spec
// (o wallet real só tem Deposit/Withdraw) — ver `.claude/specs/codegen/
// design.md` §6, "Fixtures de exemplo não são fonte de verdade".
//
// --- Fixture canônica do spec (§7, §22.4) — I6.2 des-adapta ---
//
// Até I6.1, "distinct" não existia — a fixture desviava do spec dando a
// CADA ticket um orderId DISTINTO (histórico preservado em
// .claude/specs/codegen/tasks.md, nota da 6ª fatia de H4). I6.1 fechou
// "col.distinct(x => x.k)" (§20) especificamente para este caso ("resultado
// de list — a variável materializada — ex. "soldTickets.distinct(...)", a
// forma da Policy §7", §design read-side 3.8) — esta task volta a fixture à
// forma EXATA do spec: Policy §7 (soldTickets = list ... where ...; orderIds
// = soldTickets.distinct(t => t.orderId); for orderId in orderIds { emit
// RefundRequested(orderId: orderId, reason: "Evento cancelado") }) e o
// cenário de §22.4 (3 tickets, 2 orders, "emitted count 2").
//
// Único desvio remanescente: o spec usa "reason: string" cru no literal do
// Event RefundRequested (§22.4) — primitivo cru é proibido no Write Side
// (REQ-5.1: campo de Event exige ValueObject/Enum, nunca primitivo nu),
// então "reason" usa um VO wrapper novo, RefundReason(string), em vez do
// primitivo. Um 2º scenario ("nenhum ticket casa"), fora do texto literal do
// spec mas já presente antes desta task, prova a metade "vazia" do
// predicado: o Collection[T] tem 1 item de um evento diferente do
// cancelado — "emitted count 0" confirma que o "where" de fato FILTRA.
const policyTestFixtureModDs = `Module Refunds { }
`

const policyTestFixtureSrc = `
ValueObject EventId(string) {
    Valid { value.length() > 0 }
}

ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

ValueObject CancellationReason(string) {
    Valid { value.length() > 0 }
}

ValueObject RefundReason(string) {
    Valid { value.length() > 0 }
}

Enum TicketStatus : string {
    Sold     = "SOLD"
    Refunded = "REFUNDED"
}

ValueObject Ticket {
    eventId EventId
    status TicketStatus
    orderId OrderId
}

Event EventCancelled {
    id EventId
    reason CancellationReason
}

Event RefundRequested {
    orderId OrderId
    reason  RefundReason
}

Policy RefundAllOnEventCancelled on EventCancelled {
    delivery AtLeastOnce
    execute {
        soldTickets = list Ticket t
            where t.eventId == event.id and t.status == TicketStatus.Sold
        orderIds = soldTickets.distinct(t => t.orderId)
        for orderId in orderIds {
            emit RefundRequested(orderId: orderId, reason: RefundReason("Evento cancelado"))
        }
    }
}

Test RefundAllOnEventCancelled {
    scenario "reembolso de todos os pedidos" {
        given tickets [
            Ticket("T1") { eventId: EventId("E1"), status: TicketStatus.Sold, orderId: OrderId("O1") },
            Ticket("T2") { eventId: EventId("E1"), status: TicketStatus.Sold, orderId: OrderId("O1") },
            Ticket("T3") { eventId: EventId("E1"), status: TicketStatus.Sold, orderId: OrderId("O2") }
        ]
        when event EventCancelled(id: EventId("E1"), reason: CancellationReason("Chuva"))
        then {
            emitted RefundRequested(orderId: OrderId("O1"), reason: RefundReason("Evento cancelado"))
            emitted RefundRequested(orderId: OrderId("O2"), reason: RefundReason("Evento cancelado"))
            emitted count 2
        }
    }

    scenario "nenhum ticket do evento cancelado gera zero reembolsos" {
        given tickets [
            Ticket("T5") { eventId: EventId("E9"), status: TicketStatus.Sold, orderId: OrderId("O9") }
        ]
        when event EventCancelled(id: EventId("E1"), reason: CancellationReason("Chuva"))
        then {
            emitted count 0
        }
    }
}
`

// generatePolicyTestFixtureProject roda o orquestrador COMPLETO
// (codegen.Generate) sobre o Program da fixture acima — mesmo padrão de
// generateSagaTestFixtureProject: precisamos do projeto INTEIRO (não só o
// arquivo de teste isolado) porque o cenário exercita a Policy
// RefundAllOnEventCancelled de verdade (policies.go, incl. o var de pacote
// ticketCollection e policyDispatcher que decl_policy.go declara — ver a doc
// de lá).
func generatePolicyTestFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    policyTestFixtureModDs,
		"domain.ds": policyTestFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Test de Policy (H4, §22.4) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de Test de Policy: %v", err)
	}
	return files
}

// refundsTestGoPath é o caminho do arquivo de teste gerado para o módulo
// Refunds — "<pkg>_test.go" ao lado dos demais arquivos do pacote (EmitTests,
// ver a doc do arquivo gentest.go), pkg = goname.PackageName("Refunds") =
// "refunds". codegen.File.Path usa path.Join (sempre "/") — NUNCA
// filepath.Join aqui, que em Windows produziria "\\" e não bateria com a
// chave do map (mesma nota de bookingTestGoPath, gentest_saga_test.go).
const refundsTestGoPath = "refunds/refunds_test.go"

// emitPolicyTestFixture devolve só o conteúdo de refunds/refunds_test.go, do
// projeto inteiro gerado — helper para os testes de golden/determinismo, que
// não precisam do restante do projeto.
func emitPolicyTestFixture(t *testing.T) []byte {
	t.Helper()
	files := filesToMap(generatePolicyTestFixtureProject(t))
	got, ok := files[refundsTestGoPath]
	if !ok {
		t.Fatalf("esperava %q entre os arquivos gerados, não achei", refundsTestGoPath)
	}
	return got
}

// TestEmitPolicyTestsGolden prova os elementos centrais do critério de
// conclusão desta fatia: os 2 func de cenário; o reset de ticketCollection
// (emitPolicyGivenReset) ANTES de cada given; cada Ticket semeado via
// "itemN := Ticket{}" + overlay + ".Add(ctx, itemN)"; o dispatcher
// reatribuído + Subscribe("RefundRequested", collect) ANTES da chamada
// direta a RefundAllOnEventCancelled(ctx, &ev); e as duas formas de "then"
// (emitted Evento(...) por busca em "published", emitted count N).
func TestEmitPolicyTestsGolden(t *testing.T) {
	got := string(emitPolicyTestFixture(t))
	for _, want := range []string{
		"package refunds",
		"func TestRefundAllOnEventCancelled_ReembolsoDeTodosOsPedidos(t *testing.T)",
		"func TestRefundAllOnEventCancelled_NenhumTicketDoEventoCanceladoGeraZeroReembolsos(t *testing.T)",
		"ctx := context.Background()",
		"ticketCollection = runtime.NewMemoryCollection[Ticket]()",
		"item1 := Ticket{}",
		"item1.EventId = EventId(\"E1\")",
		"item1.Status = TicketStatusSold",
		"item1.OrderId = OrderId(\"O1\")",
		"if err := ticketCollection.Add(ctx, item1); err != nil {",
		"var published []runtime.Event",
		"policyDispatcher = runtime.NewDispatcher()",
		"collect := func(ctx context.Context, ev runtime.Event) error {",
		"published = append(published, ev)",
		`policyDispatcher.Subscribe("RefundRequested", collect)`,
		"ev := EventCancelled{",
		"err := RefundAllOnEventCancelled(ctx, &ev)",
		"if err != nil {",
		"want1 := &RefundRequested{OrderId: OrderId(\"O1\"), Reason: RefundReason(\"Evento cancelado\")}",
		"found1 := false",
		"for _, got := range published {",
		"if reflect.DeepEqual(got, want1) {",
		"found1 = true",
		"if !found1 {",
		"want2 := &RefundRequested{OrderId: OrderId(\"O2\"), Reason: RefundReason(\"Evento cancelado\")}",
		"wantCount3 := 2",
		"if len(published) != wantCount3 {",
		"wantCount1 := 0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q no Go gerado do Test RefundAllOnEventCancelled (Policy), não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "tests_policy_refunds.go.golden"), []byte(got))
}

// TestEmitPolicyTestsDeterministic prova NFR-13: regerar duas vezes produz
// bytes idênticos — mesma forma de TestEmitSagaTestsDeterministic.
func TestEmitPolicyTestsDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitPolicyTestFixture(t)
	})
}

// TestEmitPolicyTestsSmokeCompile prova NFR-14 sobre o projeto INTEIRO
// gerado (não só o arquivo de teste): compila e passa go vet num diretório
// isolado.
func TestEmitPolicyTestsSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generatePolicyTestFixtureProject(t)))
}

// TestEmitPolicyTestsRunGreen prova o alvo de conclusão desta fatia de H4,
// agora sobre a fixture canônica des-adaptada por I6.2 (mesmo espírito de
// TestEmitSagaTestsRunGreen): roda `go test ./...` de VERDADE sobre o
// projeto gerado — os 2 func TestRefundAllOnEventCancelled_* gerados a
// partir do *.test.ds SÃO os testes que rodam aqui — a prova mais direta
// possível de que Collection[T]/BuiltinLowerer (Parte A, camada 3), o seam
// de Dispatcher de "emit" (Parte A), "distinct" (I6.1) e o "then { emitted
// ... }" desta fatia (Parte B) produzem Go que corretamente prova cada
// cenário: o predicado "t.eventId == event.id and t.status ==
// TicketStatus.Sold" filtra de verdade, "soldTickets.distinct(t =>
// t.orderId)" agrupa os 3 tickets do 1º cenário (2 sob "O1", 1 sob "O2") em
// exatamente 2 orderId distintos — 2 RefundRequested, não 3 — e o 2º cenário
// prova a metade vazia (0 de 1 ticket casa, 0 reembolsos).
func TestEmitPolicyTestsRunGreen(t *testing.T) {
	runGeneratedTests(t, filesToMap(generatePolicyTestFixtureProject(t)))
}
