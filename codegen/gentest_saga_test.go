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

// gentest_saga_test.go prova os critérios de conclusão da fatia de Saga de
// H4 (§design codegen 3.14, REQ-31.3, §22.3): um Test cujo Name resolve a um
// *ast.SagaDecl deste módulo vira um arquivo Go de teste que roda `go test`
// verde — mesmo alvo de conclusão que gentest_test.go já prova para
// Aggregate/UseCase (§22.1/22.2), agora para "mock ... returns ...",
// "fail step X with Y" e "then { saga compensated / compensated [...] /
// called ... }" (o subconjunto de §22.3 que emitSagaTestDecl cobre — ver a
// doc do arquivo gentest.go sobre "Subject emitted .../Subject released"
// ficarem para uma fatia futura).
//
// --- Por que uma fixture NOVA, e não a PurchaseTickets de decl_saga_test.go ---
//
// decl_saga_test.go já tem uma fixture PurchaseTickets (Marco F3) exercitada
// por golden/smoke/behavioral tests hoje verdes — mexer nela para acrescentar
// um Adapter/Test arriscaria misturar, no diff desta fatia, uma mudança de
// comportamento não relacionada com um golden já congelado (gentest.Golden
// compara byte a byte). Esta fixture SINTETIZA uma segunda Saga, também
// chamada "PurchaseTickets" mas num módulo isolado ("Booking", só usado
// aqui), reproduzindo a MESMA estrutura de passos do exemplo canônico do
// spec (§22.3: ReserveTickets/ProcessPayment/ConfirmPurchase, mock
// PaymentRequest, fail step ConfirmPurchase) — mas com um 4º elemento que o
// spec elide: ProcessPayment REALMENTE chama o Adapter PaymentRequest (via
// "PaymentRequest(orderId: \"O-1\")", a mesma forma de invocação de
// Notification que Policy/UseCase já reconhecem — ver decl_io_test.go), o
// que o F3 fixture nunca fez (nenhum step chamava Adapter antes desta
// task) — sem isso, "mock PaymentRequest returns ..." não teria nada de
// verdade para instalar nem "called PaymentRequest" nada para observar.
//
// --- As 3 formas de scenario (mesmos verbos do spec §22.3) ---
//
//  1. "sem mock nem fail step": prova o caminho 100% feliz — os 3 passos
//     completam, NADA é compensado. Note que ProcessPayment.up chama o
//     Adapter PaymentRequest SEM nenhum mock instalado: como o Adapter é
//     "mode async" (Notify, nunca propaga erro — REQ-25.3), a chamada HTTP de
//     verdade (para "" — PAYMENT_URL não setada no ambiente de teste) falha
//     RÁPIDO e LOCAL (net/http recusa o scheme vazio antes de qualquer I/O de
//     rede — sem DNS, sem timeout) e é só logada; o passo ainda retorna nil.
//     Prova, sobre o Go de fato gerado, a MESMA propriedade que a doc de
//     decl_io.go já descreve para Notify: um Adapter que falha de verdade
//     nunca derruba o fluxo de negócio. "then { compensated [] }" é a forma
//     correta de expressar "nenhuma compensação rodou" (ver a doc de
//     emitSagaThenAssert sobre por que o comparador normaliza res.Compensated
//     nil para não quebrar contra uma wantCompensated vazia).
//  2. "falha de infra na confirmação": "fail step ConfirmPurchase with
//     InfraError" substitui o Up do último passo por uma falha sintética —
//     ProcessPayment e ReserveTickets (já completados) compensam em ordem
//     REVERSA ("compensated [ProcessPayment, ReserveTickets]"), e
//     "saga compensated" prova FinalState() == SagaCompensated (nenhum passo
//     é unrecoverable nesta fixture, ao contrário da de F3 — a fatia
//     unrecoverable já está coberta lá).
//  3. "adapter mockado": "mock PaymentRequest returns true" substitui a var
//     de pacote que Notify invoca por uma closure que só registra a chamada
//     (ver a doc de emitSagaMock) — "then { called PaymentRequest }" prova
//     que ProcessPayment.up de fato invocou o Adapter.

const sagaTestFixtureModDs = `Module Booking { }
`

const sagaTestFixtureSrc = `
ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

ValueObject TicketId(string) {
    Valid { value.length() > 0 }
}

Command PurchaseTicketsCmd {
    orderId OrderId
}

Notification PaymentRequest {
    orderId string
}

Adapter PaymentRequest {
    mode async
    http POST env("PAYMENT_URL")
    headers { "Authorization" = env("PAYMENT_KEY") }
    body { orderId = notification.orderId }
}

Saga PurchaseTickets handles PurchaseTicketsCmd {
    mode await timeout 2s
    state { orderId OrderId, ticketIds AppendList<TicketId>, compensationLog AppendList<string> }

    step ReserveTickets {
        up {
            state.ticketIds.add(TicketId("T1"))
        }
        down {
            state.compensationLog.add("ReserveTickets")
        }
    }

    step ProcessPayment {
        up {
            PaymentRequest(orderId: "O-1")
            state.compensationLog.add("ProcessPayment-up")
        }
        down {
            state.compensationLog.add("ProcessPayment")
        }
    }

    step ConfirmPurchase {
        up {
            return
        }
        down {
            state.compensationLog.add("ConfirmPurchase")
        }
    }
}

Test PurchaseTickets {
    scenario "sem mock nem fail step completa sem compensacao" {
        given state { orderId: OrderId("O-1") }
        when PurchaseTicketsCmd(orderId: OrderId("O-1"))
        then { compensated [] }
    }

    scenario "falha de infra na confirmacao compensa pagamento e reserva em ordem reversa" {
        fail step ConfirmPurchase with InfraError
        given state { orderId: OrderId("O-1") }
        when PurchaseTicketsCmd(orderId: OrderId("O-1"))
        then {
            saga compensated
            compensated [ProcessPayment, ReserveTickets]
        }
    }

    scenario "adapter de pagamento mockado registra a chamada" {
        mock PaymentRequest returns true
        given state { orderId: OrderId("O-1") }
        when PurchaseTicketsCmd(orderId: OrderId("O-1"))
        then { called PaymentRequest }
    }
}
`

// generateSagaTestFixtureProject roda o orquestrador COMPLETO
// (codegen.Generate) sobre o Program da fixture acima — mesmo padrão de
// generateWalletProject/generateSagaFixtureProject: precisamos do projeto
// INTEIRO (não só o arquivo de teste isolado) porque o cenário de sucesso
// exercita o Adapter PaymentRequest de verdade (io.go) e a Saga PurchaseTickets
// de verdade (sagas.go), ambos gerados por EmitAdapters/EmitSagas — o mesmo
// motivo de TestEmitTestsWalletRunsGreen precisar do wallet inteiro para o
// cenário de UseCase.
func generateSagaTestFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    sagaTestFixtureModDs,
		"domain.ds": sagaTestFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Test de Saga (H4, §22.3) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de Test de Saga: %v", err)
	}
	return files
}

// bookingTestGoPath é o caminho do arquivo de teste gerado para o módulo
// Booking — "<pkg>_test.go" ao lado dos demais arquivos do pacote (EmitTests,
// ver a doc do arquivo gentest.go), pkg = goname.PackageName("Booking") =
// "booking". codegen.File.Path usa path.Join (sempre "/", ver
// generateModuleFiles/codegen.go) — NUNCA filepath.Join aqui, que em Windows
// produziria "\\" e não bateria com a chave do map (filesToMap usa f.Path
// direto).
const bookingTestGoPath = "booking/booking_test.go"

// emitSagaTestFixture devolve só o conteúdo de booking/booking_test.go, do
// projeto inteiro gerado — helper para os testes de golden/determinismo, que
// não precisam do restante do projeto.
func emitSagaTestFixture(t *testing.T) []byte {
	t.Helper()
	files := filesToMap(generateSagaTestFixtureProject(t))
	got, ok := files[bookingTestGoPath]
	if !ok {
		t.Fatalf("esperava %q entre os arquivos gerados, não achei", bookingTestGoPath)
	}
	return got
}

// TestEmitSagaTestsGolden prova os elementos centrais do critério de
// conclusão desta fatia: os 3 func de cenário; o reset via
// purchaseTicketsStepsOriginal; a instalação do mock/fail-step ANTES do
// given/when; res := purchaseTicketsRunSteps(...) (RunSteps direto, não a
// entrada pública — ver a doc de emitSagaTestDecl); e as 3 formas de then
// (compensated vazio, saga compensated + compensated [...], called).
func TestEmitSagaTestsGolden(t *testing.T) {
	got := string(emitSagaTestFixture(t))
	for _, want := range []string{
		"package booking",
		"func TestPurchaseTickets_SemMockNemFailStepCompletaSemCompensacao(t *testing.T)",
		"func TestPurchaseTickets_FalhaDeInfraNaConfirmacaoCompensaPagamentoEReservaEmOrdemReversa(t *testing.T)",
		"func TestPurchaseTickets_AdapterDePagamentoMockadoRegistraAChamada(t *testing.T)",
		"var purchaseTicketsStepsOriginal = append([]runtime.Step[PurchaseTicketsState](nil), purchaseTicketsSteps...)",
		"purchaseTicketsSteps = append([]runtime.Step[PurchaseTicketsState](nil), purchaseTicketsStepsOriginal...)",
		"state := &PurchaseTicketsState{}",
		"state.OrderId = OrderId(\"O-1\")",
		"cmd := PurchaseTicketsCmd{",
		"state.OrderId = cmd.OrderId",
		"res := purchaseTicketsRunSteps(context.Background(), state)",
		"gotCompensated := append([]string{}, res.Compensated...)",
		"wantCompensated := []string{}",
		"wantCompensated := []string{\"ProcessPayment\", \"ReserveTickets\"}",
		"if res.FinalState() != runtime.SagaCompensated",
		"purchaseTicketsSteps[2].Up = func(ctx context.Context, state *PurchaseTicketsState) error",
		"return errors.New(\"fail step ConfirmPurchase: InfraError (simulado)\")",
		"sendPaymentRequestFn = func(ctx context.Context, n PaymentRequest) error",
		"paymentRequestCalled = true",
		"if !paymentRequestCalled",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q no Go gerado do Test PurchaseTickets (Saga), não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "tests_saga_purchasetickets.go.golden"), []byte(got))
}

// TestEmitSagaTestsDeterministic prova NFR-13: regerar duas vezes produz
// bytes idênticos — mesma forma de TestEmitTestsWalletDeterministic.
func TestEmitSagaTestsDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitSagaTestFixture(t)
	})
}

// TestEmitSagaTestsSmokeCompile prova NFR-14 sobre o projeto INTEIRO gerado
// (não só o arquivo de teste): compila e passa go vet num diretório isolado.
func TestEmitSagaTestsSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generateSagaTestFixtureProject(t)))
}

// TestEmitSagaTestsRunGreen prova o alvo de conclusão desta fatia de H4
// (mesmo espírito de TestEmitTestsWalletRunsGreen para Aggregate/UseCase):
// roda `go test ./...` de VERDADE sobre o projeto gerado — os 3 func
// TestPurchaseTickets_* gerados a partir do *.test.ds SÃO os testes que
// rodam aqui (não um teste escrito à mão CHAMANDO o gerado, como
// sagaBehaviorTest em decl_saga_test.go faz para F3) — a prova mais direta
// possível de que "mock"/"fail step"/"then { saga compensated / compensated
// [...] / called ... }" produzem Go que corretamente prova cada cenário.
func TestEmitSagaTestsRunGreen(t *testing.T) {
	runGeneratedTests(t, filesToMap(generateSagaTestFixtureProject(t)))
}
