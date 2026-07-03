package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/rtsrc"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// decl_saga_test.go prova os critérios de conclusão da task F3 (§design
// codegen 3.10, REQ-24): um exemplo de Saga compila (golden + smoke) e um
// teste de compensação prova que, numa falha, "down" roda em ordem REVERSA,
// respeitando "down { unrecoverable }" (REQ-24.2).
//
// --- A fixture: PurchaseTickets, do spec (§18.2) ---
//
// docs/domainscript-spec-v6.md §18.2 tem um "Saga PurchaseTickets handles
// PurchaseTicketsCmd" real, com "mode await timeout 60s", exatamente os 3
// campos de state usados aqui (orderId/ticketIds/paymentId) e exatamente os
// 3 passos (ReserveTickets/ProcessPayment/ConfirmPurchase, na mesma ordem) —
// mas TODO corpo de up/down é elidido no spec ("up { ... }", "down { ... }");
// só "down { unrecoverable }" (o exemplo do parser, parser/parse_saga_test.go,
// TestSaga, reaproveita literalmente o nome "ConfirmPurchase" para esse
// passo) e "onInfraError { RetryWithBackoff(3) }" (não lowereizável — não é
// uma built-in reconhecida, ver codegen/lower/builtins.go) têm texto
// concreto, e mesmo esse texto não compila (RetryWithBackoff não é uma
// função declarada nem uma built-in). Esta fixture reproduz a ESTRUTURA real
// do spec (nome, Handles, mode/timeout, os 3 campos de state, os 3 passos na
// mesma ordem, "down { unrecoverable }" em ConfirmPurchase) e SINTETIZA
// corpos concretos e lowerizáveis nos lugares elididos — usando só formas que
// codegen/lower já sabe traduzir (atribuição a campo de state, "add" de
// AppendList, construção de VO a partir de um literal, ensure/else Error).
// Literais de string fixos (ex. TicketId("T1")) fazem esse papel — em vez de
// now()/uuid() (REQ-22.7(a)) — deliberadamente: nenhum programa DomainScript
// jamais exercitou essas built-ins de FUNÇÃO através da resolução de nomes
// (REQ-9) até este momento (codegen/lower/builtins.go as trata só na
// lowering); usá-las aqui arriscaria misturar, no diff desta task, um reparo
// de um gap não relacionado a Saga com o trabalho de F3. Fica fora de escopo,
// documentado aqui em vez de silenciosamente contornado.
//
// Um 4º passo, NotifyCustomer, é acrescentado ALÉM dos 3 do spec: nenhum dos
// 3 originais tem um jeito natural de falhar deterministicamente sem um
// mecanismo de mock/"fail step" (H4, fora do escopo de F3) — e sem uma falha
// de verdade não há como provar compensação em ordem reversa (o critério de
// conclusão literal desta task). NotifyCustomer falha exatamente quando
// cmd.orderId é o sentinela "FAIL-TRIGGER" (ensure state.orderId !=
// OrderId("FAIL-TRIGGER") else NotificationFailed) — determinístico sem
// mock, e ainda permite um caminho de SUCESSO real com qualquer outro
// orderId (TestPurchaseTicketsHappyPathCompletesAllStepsWithoutCompensation,
// abaixo). ConfirmPurchase continua ANTES de NotifyCustomer e com down
// unrecoverable — exatamente a forma do spec/parser — então a falha de
// NotifyCustomer exercita compensar ProcessPayment/ReserveTickets em ordem
// reversa PULANDO ConfirmPurchase (unrecoverable), a prova mais forte
// possível com uma única fixture.

const sagaFixtureModDs = `Module Tickets { }
`

const sagaFixtureSrc = `
ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

ValueObject TicketId(string) {
    Valid { value.length() > 0 }
}

ValueObject PaymentId(string) {
    Valid { value.length() > 0 }
}

Error NotificationFailed { message "falha ao notificar o cliente" }

Command PurchaseTicketsCmd {
    orderId OrderId
}

Saga PurchaseTickets handles PurchaseTicketsCmd {
    mode await timeout 2s
    state { orderId OrderId, ticketIds AppendList<TicketId>, paymentId PaymentId, compensationLog AppendList<string> }

    step ReserveTickets {
        up {
            state.ticketIds.add(TicketId("T1"))
            state.ticketIds.add(TicketId("T2"))
        }
        down {
            state.compensationLog.add("ReserveTickets")
        }
    }

    step ProcessPayment {
        up {
            state.paymentId = PaymentId("PAY-1")
        }
        down {
            state.compensationLog.add("ProcessPayment")
        }
        onInfraError {
            log Warn "infra ao processar pagamento"
        }
    }

    step ConfirmPurchase {
        up {
            return
        }
        down { unrecoverable }
    }

    step NotifyCustomer {
        up {
            ensure state.orderId != OrderId("FAIL-TRIGGER") else NotificationFailed
        }
    }
}
`

// findSagaDecl acha o *ast.SagaDecl de nome name em qualquer arquivo do
// programa — espelha findWorkerDecl/findPolicyDecl.
func findSagaDecl(t *testing.T, prog *program.Program, name string) *ast.SagaDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if s, ok := d.(*ast.SagaDecl); ok && s.Name == name {
				return s
			}
		}
	}
	t.Fatalf("Saga %q não encontrada na fixture — o exemplo mudou?", name)
	return nil
}

// parseSagaFixture monta o projeto sintético em disco (mod.ds + domain.ds) e
// o resolve via driver.CheckProject — devolve o Program e a SagaDecl.
func parseSagaFixture(t *testing.T) (prog *program.Program, saga *ast.SagaDecl) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    sagaFixtureModDs,
		"domain.ds": sagaFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Saga (F3) tem diagnósticos de erro:\n%s", bag.Render())
	}
	saga = findSagaDecl(t, prog, "PurchaseTickets")
	return
}

// emitSagaFixture gera o Go da Saga da fixture.
func emitSagaFixture(t *testing.T) []byte {
	t.Helper()
	prog, saga := parseSagaFixture(t)
	model := types.NewModel(prog.Symbols)
	reg := walletVOOperatorRegistryFromProgram(prog) // nenhum Operator na fixture, registry vazio é suficiente

	got, err := codegen.EmitSaga("tickets", saga, model, prog.Symbols, "Tickets", reg)
	if err != nil {
		t.Fatalf("EmitSaga: erro inesperado: %v", err)
	}
	return got
}

// --- Golden + determinismo -------------------------------------------------

// TestEmitSagaGolden prova os elementos centrais do critério de conclusão da
// task: o struct de state; up/down/onInfraError lowerizados por passo;
// down { unrecoverable } SEM função Down; a tabela de runtime.Step na ordem
// declarada; RunSteps sobre runtime.RunSaga; e a entrada "await" (bloqueante
// + timeout, sem "func Wire" — ver a doc do arquivo decl_saga.go).
func TestEmitSagaGolden(t *testing.T) {
	got := emitSagaFixture(t)
	for _, want := range []string{
		"type PurchaseTicketsState struct",
		`json:"orderId"`,
		"runtime.AppendList[TicketId]",
		`json:"ticketIds"`,
		`json:"paymentId"`,
		"runtime.AppendList[string]",
		`json:"compensationLog"`,
		// passos: up sempre, down só quando não-unrecoverable, onInfraError só quando presente
		"func purchaseTicketsReserveTicketsUp(ctx context.Context, state *PurchaseTicketsState) error",
		`state.TicketIds.Add(TicketId("T1"))`,
		"func purchaseTicketsReserveTicketsDown(ctx context.Context, state *PurchaseTicketsState) error",
		"state.CompensationLog.Add(\"ReserveTickets\")",
		"func purchaseTicketsProcessPaymentUp(ctx context.Context, state *PurchaseTicketsState) error",
		`state.PaymentId = PaymentId("PAY-1")`,
		"func purchaseTicketsProcessPaymentDown(ctx context.Context, state *PurchaseTicketsState) error",
		"func purchaseTicketsProcessPaymentOnInfraError(ctx context.Context, state *PurchaseTicketsState) error",
		"func purchaseTicketsConfirmPurchaseUp(ctx context.Context, state *PurchaseTicketsState) error",
		"func purchaseTicketsNotifyCustomerUp(ctx context.Context, state *PurchaseTicketsState) error",
		// ConfirmPurchase não deveria ganhar uma função Down (down { unrecoverable })
		// a tabela de steps
		"var purchaseTicketsSteps = []runtime.Step[PurchaseTicketsState]{",
		`{Name: "ReserveTickets", Up: purchaseTicketsReserveTicketsUp, Down: purchaseTicketsReserveTicketsDown, OnInfraError: nil, Unrecoverable: false}`,
		`{Name: "ProcessPayment", Up: purchaseTicketsProcessPaymentUp, Down: purchaseTicketsProcessPaymentDown, OnInfraError: purchaseTicketsProcessPaymentOnInfraError, Unrecoverable: false}`,
		`{Name: "ConfirmPurchase", Up: purchaseTicketsConfirmPurchaseUp, Down: nil, OnInfraError: nil, Unrecoverable: true}`,
		`{Name: "NotifyCustomer", Up: purchaseTicketsNotifyCustomerUp, Down: nil, OnInfraError: nil, Unrecoverable: false}`,
		"func purchaseTicketsRunSteps(ctx context.Context, state *PurchaseTicketsState) runtime.SagaResult",
		"return runtime.RunSaga(ctx, state, purchaseTicketsSteps)",
		// entrada await: bloqueante + timeout, devolve (*State, error)
		"func PurchaseTickets(ctx context.Context, cmd PurchaseTicketsCmd) (*PurchaseTicketsState, error)",
		"state := &PurchaseTicketsState{}",
		"state.OrderId = cmd.OrderId",
		"done := make(chan runtime.SagaResult, 1)",
		"done <- purchaseTicketsRunSteps(context.Background(), state)",
		"ctx, cancel = context.WithTimeout(ctx, time.Duration(2000000000))",
		"case res := <-done:",
		"return state, res.Err",
		"case <-ctx.Done():",
		"return nil, ctx.Err()",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "func purchaseTicketsConfirmPurchaseDown") {
		t.Errorf("NÃO esperava função Down para ConfirmPurchase (down { unrecoverable }):\n%s", got)
	}
	if strings.Contains(string(got), "func Wire(") {
		t.Errorf("Saga NÃO deveria emitir \"func Wire\" (ver a doc de decl_saga.go):\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "saga_purchase_tickets.go.golden"), got)
}

// TestEmitSagaDeterministic prova NFR-13.
func TestEmitSagaDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitSagaFixture(t)
	})
}

// --- Smoke compile -----------------------------------------------------------

// sagaSmokeFiles monta os arquivos mínimos para compilar a Saga junto do
// domínio que ela referencia: go.mod + runtime real + tickets/value_objects.go
// (as 3 VOs) + tickets/errors.go (NotificationFailed) + tickets/commands.go
// (PurchaseTicketsCmd) + tickets/sagas.go.
func sagaSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog, _ := parseSagaFixture(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	model := types.NewModel(prog.Symbols)
	var vos []*ast.ValueObjectDecl
	var errs []*ast.ErrorTypeDecl
	var cmds []*ast.CommandDecl
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			switch n := d.(type) {
			case *ast.ValueObjectDecl:
				vos = append(vos, n)
			case *ast.ErrorTypeDecl:
				errs = append(errs, n)
			case *ast.CommandDecl:
				cmds = append(cmds, n)
			}
		}
	}
	for _, vo := range vos {
		got, err := codegen.EmitValueObject("tickets", vo)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", vo.Name, err)
		}
		files[filepath.Join("tickets", strings.ToLower(vo.Name)+".go")] = got
	}
	for _, errDecl := range errs {
		got, err := codegen.EmitError("tickets", errDecl)
		if err != nil {
			t.Fatalf("EmitError(%s): erro inesperado: %v", errDecl.Name, err)
		}
		files[filepath.Join("tickets", strings.ToLower(errDecl.Name)+".go")] = got
	}
	for _, cmd := range cmds {
		got, err := codegen.EmitCommand("tickets", cmd, model, prog.Symbols, "Tickets")
		if err != nil {
			t.Fatalf("EmitCommand(%s): erro inesperado: %v", cmd.Name, err)
		}
		files[filepath.Join("tickets", strings.ToLower(cmd.Name)+".go")] = got
	}

	files[filepath.Join("tickets", "sagas.go")] = emitSagaFixture(t)
	return files
}

// TestEmitSagaSmokeCompile prova NFR-14: o Go da Saga, junto do domínio que
// ela referencia e do runtime vendorado real, compila e passa go vet num
// projeto isolado.
func TestEmitSagaSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, sagaSmokeFiles(t))
}

// --- Orquestrador completo (codegen.Generate) --------------------------------

// generateSagaFixtureProject roda Generate sobre o Program da fixture
// sintética de Saga (mesmo padrão de generateWorkerFixtureProject).
func generateSagaFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    sagaFixtureModDs,
		"domain.ds": sagaFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Saga (F3) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de Saga: %v", err)
	}
	return files
}

// TestGenerateSagaFixtureCompilesWithoutWireCollision prova o item 4 do
// prompt da task (evitar o problema de colisão do Wire, precedente de F1/F2):
// rodando o orquestrador COMPLETO (codegen.Generate) sobre o projeto
// sintético, tickets/sagas.go existe, NÃO tem "func Wire" (Saga não precisa
// de nenhum wiring — ver a doc de decl_saga.go), cmd/tickets/main.go não
// referencia a Saga (nenhuma mudança em generateCmdMainFile era necessária) e
// o projeto gerado INTEIRO compila.
func TestGenerateSagaFixtureCompilesWithoutWireCollision(t *testing.T) {
	files := generateSagaFixtureProject(t)
	m := filesToMap(files)

	sagasGo, ok := m["tickets/sagas.go"]
	if !ok {
		t.Fatalf("esperava tickets/sagas.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}
	if strings.Contains(string(sagasGo), "func Wire(") {
		t.Errorf("NÃO esperava \"func Wire\" em tickets/sagas.go:\n%s", sagasGo)
	}

	mainGo, ok := m["cmd/tickets/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/tickets/main.go, não achei:\n%v", filePathsForTest(files))
	}
	if strings.Contains(string(mainGo), "PurchaseTickets") {
		t.Errorf("NÃO esperava cmd/tickets/main.go referenciar a Saga (nenhum wiring é necessário):\n%s", mainGo)
	}

	gentest.SmokeCompile(t, m)
}

// --- Comportamento: compensação em ordem reversa (Conclusão literal de F3) --

// sagaBehaviorTest roda dentro do projeto isolado gerado no smoke, no MESMO
// pacote Go da Saga (permite chamar purchaseTicketsRunSteps diretamente,
// mesmo padrão de decl_worker_test.go chamando as funções privadas
// <nome>Tick/<nome>Execute) e prova, sobre o Go de fato gerado:
//
//  1. TestPurchaseTicketsHappyPathCompletesAllStepsWithoutCompensation: com
//     um orderId que não dispara o gatilho de falha, os 4 passos completam e
//     nenhuma compensação roda.
//  2. TestPurchaseTicketsFailureCompensatesInReverseOrderRespectingUnrecoverable
//     (o critério de conclusão LITERAL de F3): com o orderId-gatilho, o step
//     NotifyCustomer falha; ReserveTickets e ProcessPayment (já completados)
//     são compensados em ordem REVERSA — prova dupla, tanto pelo
//     state.CompensationLog (populado pelos próprios Down lowerizados) quanto
//     por runtime.SagaResult.Compensated — e ConfirmPurchase (unrecoverable)
//     é pulado sem panics, aparecendo em Unrecoverable.
const sagaBehaviorTest = `package tickets

import (
	"context"
	"testing"
)

func TestPurchaseTicketsHappyPathCompletesAllStepsWithoutCompensation(t *testing.T) {
	state := &PurchaseTicketsState{OrderId: OrderId("O1")}
	res := purchaseTicketsRunSteps(context.Background(), state)

	if !res.Ok() {
		t.Fatalf("esperava sucesso, got Err=%v", res.Err)
	}
	wantCompleted := []string{"ReserveTickets", "ProcessPayment", "ConfirmPurchase", "NotifyCustomer"}
	if len(res.Completed) != len(wantCompleted) {
		t.Fatalf("Completed = %v, want %v", res.Completed, wantCompleted)
	}
	for i, name := range wantCompleted {
		if res.Completed[i] != name {
			t.Fatalf("Completed[%d] = %q, want %q (%v)", i, res.Completed[i], name, res.Completed)
		}
	}
	if len(res.Compensated) != 0 || len(res.Unrecoverable) != 0 {
		t.Fatalf("não esperava compensação sobre sucesso total: %+v", res)
	}
	if len(state.TicketIds.Items()) != 2 {
		t.Fatalf("esperava 2 tickets reservados, got %d", len(state.TicketIds.Items()))
	}
	if state.PaymentId == PaymentId("") {
		t.Fatal("esperava paymentId semeado por ProcessPayment.up")
	}
}

func TestPurchaseTicketsFailureCompensatesInReverseOrderRespectingUnrecoverable(t *testing.T) {
	state := &PurchaseTicketsState{OrderId: OrderId("FAIL-TRIGGER")}
	res := purchaseTicketsRunSteps(context.Background(), state)

	if res.Ok() {
		t.Fatal("esperava falha (NotifyCustomer deveria disparar NotificationFailed)")
	}
	if res.FullyCompensated() {
		t.Fatal("não esperava FullyCompensated (ConfirmPurchase é unrecoverable)")
	}

	wantCompensated := []string{"ProcessPayment", "ReserveTickets"}
	if len(res.Compensated) != len(wantCompensated) {
		t.Fatalf("Compensated = %v, want %v (ordem REVERSA)", res.Compensated, wantCompensated)
	}
	for i, name := range wantCompensated {
		if res.Compensated[i] != name {
			t.Fatalf("Compensated[%d] = %q, want %q (ordem REVERSA, %v)", i, res.Compensated[i], name, res.Compensated)
		}
	}
	if len(res.Unrecoverable) != 1 || res.Unrecoverable[0] != "ConfirmPurchase" {
		t.Fatalf("Unrecoverable = %v, want [ConfirmPurchase]", res.Unrecoverable)
	}

	// Prova independente, via o side-effect que os próprios Down lowerizados
	// (state.compensationLog.add(...)) gravam em state — não só via
	// runtime.SagaResult, mas no Go de fato GERADO a partir do DomainScript.
	gotLog := state.CompensationLog.Items()
	wantLog := []string{"ProcessPayment", "ReserveTickets"}
	if len(gotLog) != len(wantLog) {
		t.Fatalf("state.CompensationLog = %v, want %v (ordem REVERSA)", gotLog, wantLog)
	}
	for i, name := range wantLog {
		if gotLog[i] != name {
			t.Fatalf("state.CompensationLog[%d] = %q, want %q (ordem REVERSA, %v)", i, gotLog[i], name, gotLog)
		}
	}
}

func TestPurchaseTicketsAwaitEntryReturnsStateAndError(t *testing.T) {
	got, err := PurchaseTickets(context.Background(), PurchaseTicketsCmd{OrderId: OrderId("O2")})
	if err != nil {
		t.Fatalf("esperava sucesso, got %v", err)
	}
	if got == nil || got.OrderId != OrderId("O2") {
		t.Fatalf("esperava state com OrderId semeado de cmd, got %+v", got)
	}

	_, err = PurchaseTickets(context.Background(), PurchaseTicketsCmd{OrderId: OrderId("FAIL-TRIGGER")})
	if err == nil {
		t.Fatal("esperava erro propagado (NotificationFailed) após compensação")
	}
}
`

// TestEmitSagaBehavior prova NFR-15 sobre o Go de fato gerado: escreve os
// mesmos arquivos do smoke mais o teste comportamental acima num diretório
// isolado e roda "go test ./..." de verdade — a prova mais forte do critério
// de conclusão literal de F3 ("teste de compensação executa down em ordem
// reversa").
func TestEmitSagaBehavior(t *testing.T) {
	files := sagaSmokeFiles(t)
	files[filepath.Join("tickets", "sagas_behavior_test.go")] = []byte(sagaBehaviorTest)

	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("não consegui criar %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("não consegui escrever %q: %v", path, err)
		}
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("`go test ./...` falhou em %q: %v\n%s", dir, err, out)
	}
}
