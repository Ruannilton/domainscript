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
	"domainscript/codegen/goname"
	"domainscript/codegen/rtsrc"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// decl_io_test.go prova os critérios de conclusão da task F4 (§design
// codegen 3.13/decl_io, REQ-25) — Notification/Adapter/Foreign não têm
// exemplo real no wallet nem no shop mínimo (nenhum dos dois declara
// Notification/Adapter/Foreign — checado antes de escrever esta fixture),
// coberta por uma fixture SINTÉTICA que reproduz literalmente os DOIS
// exemplos canônicos do spec (§9.1/9.3): "Notification DepositNotification"
// + "Adapter DepositNotification" (HTTP declarativo, mode async — parser
// test TestAdapterHTTP, parser/parse_adapter_test.go) e "Notification
// PaymentRequest" + "Adapter PaymentRequest" (FFI, mode sync — parser test
// TestAdapterFFI). Os campos usam tipos primitivos (em vez de VO/composto
// como Email/Money do spec) deliberadamente: o foco de F4 é a fronteira de
// saída (contrato + transporte + notify/call), não a profundidade de
// composição de VO — já coberta por E3.
//
// Um Policy (SendDepositNotification on OrderPlaced) invoca a Notification
// HTTP como ExprStmt solto — o padrão "notify" (REQ-25.3): construção +
// despacho assíncrono, erro nunca propagado. Um UseCase (Pay handles
// MakePayment) invoca a Notification FFI da mesma forma — o padrão "call"
// (REQ-25.3): erro propagado ao chamador. Nenhuma sintaxe nova: "Xxx(args)"
// já é construção de shape válida desde E5.1; só o LOWERING (F4,
// lower/stmt.go:notifyOrCallStmt) passa a reconhecer essa forma quando Xxx é
// uma Notification com Adapter parceiro anexado via WithNotifyAdapters — ver
// a doc de codegen/decl_io.go para a análise completa de por que não existe
// (nem precisa existir) um par de keywords "notify"/"call" na gramática.

const ioFixtureModDs = `Module Reactions { }
`

const ioFixtureSrc = `
ValueObject PaymentId(string) {
    Valid { value.length() > 0 }
}

ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

Event OrderPlaced {
    orderId OrderId
}

Notification DepositNotification {
    to string
    amount integer
}

Adapter DepositNotification {
    mode async
    http POST env("SENDGRID_URL")
    headers { "Authorization" = env("SENDGRID_KEY") }
    body { to = notification.to, amount = notification.amount }
}

Notification PaymentRequest {
    paymentId PaymentId
    amount integer
}

Adapter PaymentRequest {
    mode sync
    foreign "go" from "adapters/payment_gateway"
    function "ProcessPayment"
    map { paymentId = notification.paymentId, amount = notification.amount }
}

Policy SendDepositNotification on OrderPlaced {
    delivery BestEffort
    execute {
        DepositNotification(to: "customer@example.com", amount: 42)
    }
}

Command MakePayment {
    paymentId PaymentId
}

UseCase Pay handles MakePayment {
    execute {
        log Info "pagamento solicitado" { caller = caller.id }
        PaymentRequest(paymentId: cmd.paymentId, amount: 100)
    }
}
`

// findNotificationDecl/findAdapterDecl/findUseCaseDeclIO acham a declaração
// de nome name em qualquer arquivo do programa — mesmo padrão de
// findPolicyDecl/findWorkerDecl/findSagaDecl.
func findNotificationDecl(t *testing.T, prog *program.Program, name string) *ast.NotificationDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if n, ok := d.(*ast.NotificationDecl); ok && n.Name == name {
				return n
			}
		}
	}
	t.Fatalf("Notification %q não encontrada na fixture — o exemplo mudou?", name)
	return nil
}

func findAdapterDecl(t *testing.T, prog *program.Program, name string) *ast.AdapterDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if a, ok := d.(*ast.AdapterDecl); ok && a.Name == name {
				return a
			}
		}
	}
	t.Fatalf("Adapter %q não encontrado na fixture — o exemplo mudou?", name)
	return nil
}

func findUseCaseDeclIO(t *testing.T, prog *program.Program, name string) *ast.UseCaseDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if u, ok := d.(*ast.UseCaseDecl); ok && u.Name == name {
				return u
			}
		}
	}
	t.Fatalf("UseCase %q não encontrado na fixture — o exemplo mudou?", name)
	return nil
}

// parseIOFixture monta o projeto sintético em disco (mod.ds + domain.ds) e o
// resolve via driver.CheckProject.
func parseIOFixture(t *testing.T) *program.Program {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    ioFixtureModDs,
		"domain.ds": ioFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Notification/Adapter (F4) tem diagnósticos de erro:\n%s", bag.Render())
	}
	return prog
}

// adaptersByNameIO indexa todos os Adapter da fixture por nome — o registry
// que EmitPolicies/EmitUseCases (F4) esperam para reconhecer notify/call.
func adaptersByNameIO(prog *program.Program) map[string]*ast.AdapterDecl {
	m := make(map[string]*ast.AdapterDecl)
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if a, ok := d.(*ast.AdapterDecl); ok {
				m[a.Name] = a
			}
		}
	}
	return m
}

// --- 1. Notification: contrato de saída (golden). ---

func TestEmitNotificationGolden(t *testing.T) {
	prog := parseIOFixture(t)
	decl := findNotificationDecl(t, prog, "DepositNotification")

	got, err := codegen.EmitNotification("reactions", decl)
	if err != nil {
		t.Fatalf("EmitNotification: erro inesperado: %v", err)
	}
	for _, want := range []string{
		"type DepositNotification struct",
		`To     string ` + "`json:\"to\"`",
		`Amount int64  ` + "`json:\"amount\"`",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("esperava %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "notification_deposit.go.golden"), got)
}

// --- 2. Adapter HTTP declarativo (golden) — REQ-25.1, notify (async). ---

func TestEmitAdapterHTTPGolden(t *testing.T) {
	prog := parseIOFixture(t)
	notif := findNotificationDecl(t, prog, "DepositNotification")
	adapter := findAdapterDecl(t, prog, "DepositNotification")
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitAdapter("reactions", adapter, notif, model, prog.Symbols, "Reactions", goname.NewVOOperatorRegistry())
	if err != nil {
		t.Fatalf("EmitAdapter(DepositNotification): erro inesperado: %v", err)
	}
	for _, want := range []string{
		"func sendDepositNotification(ctx context.Context, n DepositNotification) error",
		`payload := map[string]any{"to": n.To, "amount": n.Amount}`,
		"bodyBytes, err := json.Marshal(payload)",
		`req, err := http.NewRequestWithContext(ctx, "POST", os.Getenv("SENDGRID_URL"), bytes.NewReader(bodyBytes))`,
		`req.Header.Set("Authorization", os.Getenv("SENDGRID_KEY"))`,
		"resp, err := http.DefaultClient.Do(req)",
		"defer resp.Body.Close()",
		"if resp.StatusCode >= 400",
		// notify (async): sem retorno, erro só logado — REQ-25.3
		"func NotifyDepositNotification(ctx context.Context, n DepositNotification)",
		"if err := sendDepositNotification(ctx, n); err != nil",
		"slog.Default().ErrorContext(ctx",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("esperava %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "func CallDepositNotification(") {
		t.Errorf("Mode async NÃO deveria gerar Call<Nome> (só Notify<Nome>):\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "adapter_deposit_notification.go.golden"), got)
}

// --- 3. Adapter FFI (golden) — REQ-25.2, call (sync). ---

func TestEmitAdapterFFIGolden(t *testing.T) {
	prog := parseIOFixture(t)
	notif := findNotificationDecl(t, prog, "PaymentRequest")
	adapter := findAdapterDecl(t, prog, "PaymentRequest")
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitAdapter("reactions", adapter, notif, model, prog.Symbols, "Reactions", goname.NewVOOperatorRegistry())
	if err != nil {
		t.Fatalf("EmitAdapter(PaymentRequest): erro inesperado: %v", err)
	}
	for _, want := range []string{
		"func callPaymentRequestForeign(ctx context.Context, n PaymentRequest) error",
		"return payment_gateway.ProcessPayment(ctx, string(n.PaymentId), n.Amount)",
		// call (sync): erro propagado — REQ-25.3
		"func CallPaymentRequest(ctx context.Context, n PaymentRequest) error",
		"return callPaymentRequestForeign(ctx, n)",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("esperava %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "func NotifyPaymentRequest(") {
		t.Errorf("Mode sync NÃO deveria gerar Notify<Nome> (só Call<Nome>):\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "adapter_payment_request.go.golden"), got)
}

// --- 4. Foreign geral (§9.4, golden) — REQ-25.2. ---

const foreignFixtureModDs = `Module Crypto { }
`

const foreignFixtureSrc = `
Foreign "go" from "internal/crypto" {
    function ComputeMerkleRoot(items List<bytes>) -> bytes
}
`

func findForeignDecl(t *testing.T, prog *program.Program) *ast.ForeignDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.ForeignDecl); ok {
				return fd
			}
		}
	}
	t.Fatalf("Foreign não encontrado na fixture — o exemplo mudou?")
	return nil
}

func TestEmitForeignGolden(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    foreignFixtureModDs,
		"domain.ds": foreignFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Foreign (F4) tem diagnósticos de erro:\n%s", bag.Render())
	}
	decl := findForeignDecl(t, prog)

	got, err := codegen.EmitForeign("crypto", decl)
	if err != nil {
		t.Fatalf("EmitForeign: erro inesperado: %v", err)
	}
	for _, want := range []string{
		"func ComputeMerkleRoot(items [][]byte) ([]byte, error)",
		"return crypto.ComputeMerkleRoot(items)",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("esperava %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "foreign_crypto.go.golden"), got)
}

func TestEmitForeignDeterministic(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    foreignFixtureModDs,
		"domain.ds": foreignFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Foreign (F4) tem diagnósticos de erro:\n%s", bag.Render())
	}
	decl := findForeignDecl(t, prog)

	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitForeign("crypto", decl)
		if err != nil {
			t.Fatalf("EmitForeign: erro inesperado: %v", err)
		}
		return got
	})
}

// --- 5. Determinismo de Notification/Adapter (NFR-13). ---

func TestEmitAdaptersDeterministic(t *testing.T) {
	prog := parseIOFixture(t)
	notif := findNotificationDecl(t, prog, "DepositNotification")
	adapter := findAdapterDecl(t, prog, "DepositNotification")
	model := types.NewModel(prog.Symbols)

	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitAdapter("reactions", adapter, notif, model, prog.Symbols, "Reactions", goname.NewVOOperatorRegistry())
		if err != nil {
			t.Fatalf("EmitAdapter: erro inesperado: %v", err)
		}
		return got
	})
}

// --- 6. notify em Policy (golden) — REQ-25.3. ---

func TestEmitPolicyNotifyGolden(t *testing.T) {
	prog := parseIOFixture(t)
	policy := findPolicyDecl(t, prog, "SendDepositNotification")
	model := types.NewModel(prog.Symbols)
	reg := goname.NewVOOperatorRegistry()

	got, err := codegen.EmitPolicy("reactions", policy, model, prog.Symbols, "Reactions", reg, adaptersByNameIO(prog))
	if err != nil {
		t.Fatalf("EmitPolicy(SendDepositNotification): erro inesperado: %v", err)
	}
	for _, want := range []string{
		`NotifyDepositNotification(ctx, DepositNotification{To: "customer@example.com", Amount: 42})`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("esperava %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	// notify: NENHUMA linha de checagem de erro para essa chamada (REQ-25.3 —
	// a própria FORMA do Go gerado distingue notify de call).
	if strings.Contains(string(got), "CallDepositNotification") {
		t.Errorf("Policy não deveria referenciar Call<Nome> (só Notify<Nome>, mode async):\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "policy_notify_deposit.go.golden"), got)
}

// --- 7. call em UseCase (golden) — REQ-25.3. ---

func TestEmitUseCaseCallGolden(t *testing.T) {
	prog := parseIOFixture(t)
	uc := findUseCaseDeclIO(t, prog, "Pay")
	model := types.NewModel(prog.Symbols)
	reg := goname.NewVOOperatorRegistry()

	got, err := codegen.EmitUseCase("reactions", uc, map[string]*ast.AggregateDecl{}, model, prog.Symbols, "Reactions", reg, adaptersByNameIO(prog))
	if err != nil {
		t.Fatalf("EmitUseCase(Pay): erro inesperado: %v", err)
	}
	for _, want := range []string{
		`if err := CallPaymentRequest(ctx, PaymentRequest{PaymentId: cmd.PaymentId, Amount: 100}); err != nil {`,
		"return err",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("esperava %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "NotifyPaymentRequest") {
		t.Errorf("UseCase não deveria referenciar Notify<Nome> (só Call<Nome>, mode sync):\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "usecase_call_payment.go.golden"), got)
}

// --- 8. Smoke compile + comportamento --------------------------------------

// paymentGatewayStub é o pacote HAND-WRITTEN esperado em
// adapters/payment_gateway (ver a doc de codegen/decl_io.go): NUNCA gerado
// por "dsc gen" — só escrito aqui, no teste, simulando o que um usuário real
// escreveria. Grava cada chamada (para o teste comportamental verificar
// marshalling) e devolve o erro configurado em ProcessPaymentErr (para o
// teste comportamental provar propagação de erro em "call").
const paymentGatewayStub = `package payment_gateway

import "context"

type ProcessPaymentCall struct {
	PaymentId string
	Amount    int64
}

var ProcessPaymentCalls []ProcessPaymentCall
var ProcessPaymentErr error

// ProcessPayment é a assinatura hand-written esperada pelo Adapter
// PaymentRequest (§9.3 Nível 2 — FFI, ver codegen/decl_io.go).
func ProcessPayment(ctx context.Context, paymentId string, amount int64) error {
	ProcessPaymentCalls = append(ProcessPaymentCalls, ProcessPaymentCall{paymentId, amount})
	return ProcessPaymentErr
}
`

// ioSmokeFiles monta os arquivos mínimos para compilar Notification/Adapter/
// Policy/UseCase da fixture junto do domínio que referenciam: go.mod +
// runtime real + o stub hand-written de adapters/payment_gateway (mesmo
// padrão de sagaSmokeFiles/workerSmokeFiles). Policy e UseCase vão para
// PACOTES SEPARADOS ("notify"/"payments") — mesma restrição estrutural que
// generateModuleFiles (codegen.go) já documenta e aplica no orquestrador
// real: EmitPolicies e EmitUseCases emitem, cada um, "func Wire(...)" com a
// MESMA assinatura de nome — coexistir no mesmo pacote Go colidiria (erro de
// compilação real, não um artefato deste teste). O wallet/shop de hoje nunca
// combinam as duas categorias no mesmo módulo pela mesma razão; esta fixture
// segue a mesma convenção, só demonstrando cada categoria (notify/call)
// isoladamente.
func ioSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog := parseIOFixture(t)
	model := types.NewModel(prog.Symbols)
	reg := goname.NewVOOperatorRegistry()

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	notif := findNotificationDecl(t, prog, "DepositNotification")
	adapter := findAdapterDecl(t, prog, "DepositNotification")
	orderId := findValueObjectDecl(t, prog, "OrderId")
	event := findEventDecl(t, prog, "OrderPlaced")
	policy := findPolicyDecl(t, prog, "SendDepositNotification")

	notifyAdapters := map[string]*ast.AdapterDecl{adapter.Name: adapter}

	orderIdGo, err := codegen.EmitValueObject("notify", orderId)
	if err != nil {
		t.Fatalf("EmitValueObject(OrderId): erro inesperado: %v", err)
	}
	files[filepath.Join("notify", "value_object_orderid.go")] = orderIdGo

	eventGo, err := codegen.EmitEvent("notify", event)
	if err != nil {
		t.Fatalf("EmitEvent(OrderPlaced): erro inesperado: %v", err)
	}
	files[filepath.Join("notify", "events.go")] = eventGo

	notifGo, err := codegen.EmitNotification("notify", notif)
	if err != nil {
		t.Fatalf("EmitNotification(DepositNotification): erro inesperado: %v", err)
	}
	files[filepath.Join("notify", "notification_deposit.go")] = notifGo

	adapterGo, err := codegen.EmitAdapter("notify", adapter, notif, model, prog.Symbols, "Reactions", reg)
	if err != nil {
		t.Fatalf("EmitAdapter(DepositNotification): erro inesperado: %v", err)
	}
	files[filepath.Join("notify", "adapter_deposit.go")] = adapterGo

	policyGo, err := codegen.EmitPolicy("notify", policy, model, prog.Symbols, "Reactions", reg, notifyAdapters)
	if err != nil {
		t.Fatalf("EmitPolicy(SendDepositNotification): erro inesperado: %v", err)
	}
	files[filepath.Join("notify", "policies.go")] = policyGo

	// --- pacote "payments": PaymentId/MakePayment/PaymentRequest/Pay (call) ---

	payNotif := findNotificationDecl(t, prog, "PaymentRequest")
	payAdapter := findAdapterDecl(t, prog, "PaymentRequest")
	paymentId := findValueObjectDecl(t, prog, "PaymentId")
	command := findCommandDecl(t, prog, "MakePayment")
	uc := findUseCaseDeclIO(t, prog, "Pay")

	payAdapters := map[string]*ast.AdapterDecl{payAdapter.Name: payAdapter}

	paymentIdGo, err := codegen.EmitValueObject("payments", paymentId)
	if err != nil {
		t.Fatalf("EmitValueObject(PaymentId): erro inesperado: %v", err)
	}
	files[filepath.Join("payments", "value_object_paymentid.go")] = paymentIdGo

	commandGo, err := codegen.EmitCommand("payments", command, model, prog.Symbols, "Reactions")
	if err != nil {
		t.Fatalf("EmitCommand(MakePayment): erro inesperado: %v", err)
	}
	files[filepath.Join("payments", "commands.go")] = commandGo

	payNotifGo, err := codegen.EmitNotification("payments", payNotif)
	if err != nil {
		t.Fatalf("EmitNotification(PaymentRequest): erro inesperado: %v", err)
	}
	files[filepath.Join("payments", "notification_payment.go")] = payNotifGo

	payAdapterGo, err := codegen.EmitAdapter("payments", payAdapter, payNotif, model, prog.Symbols, "Reactions", reg)
	if err != nil {
		t.Fatalf("EmitAdapter(PaymentRequest): erro inesperado: %v", err)
	}
	files[filepath.Join("payments", "adapter_payment.go")] = payAdapterGo

	aggregates := map[string]*ast.AggregateDecl{}
	usecaseGo, err := codegen.EmitUseCase("payments", uc, aggregates, model, prog.Symbols, "Reactions", reg, payAdapters)
	if err != nil {
		t.Fatalf("EmitUseCase(Pay): erro inesperado: %v", err)
	}
	files[filepath.Join("payments", "usecases.go")] = usecaseGo

	files[filepath.Join("adapters", "payment_gateway", "payment_gateway.go")] = []byte(paymentGatewayStub)

	return files
}

func findCommandDecl(t *testing.T, prog *program.Program, name string) *ast.CommandDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if c, ok := d.(*ast.CommandDecl); ok && c.Name == name {
				return c
			}
		}
	}
	t.Fatalf("Command %q não encontrado na fixture — o exemplo mudou?", name)
	return nil
}

func TestEmitIOSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, ioSmokeFiles(t))
}

// notifyBehaviorTestGo roda dentro do pacote "notify" do projeto isolado
// gerado no smoke (mesmo padrão de decl_saga_test.go/decl_worker_test.go):
// prova, sobre o Go de fato GERADO, que NotifyDepositNotification (notify,
// REQ-25.3) NÃO devolve nada ao chamador (a própria assinatura não permite
// propagar — nem precisa de um servidor falhando para provar isso) e que a
// requisição HTTP de fato chega com método/URL(env)/headers(env)/body
// corretos — o Adapter HTTP declarativo roda de verdade.
const notifyBehaviorTestGo = `package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNotifyDoesNotPropagateHTTPFailure(t *testing.T) {
	var gotAuth, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	os.Setenv("SENDGRID_URL", srv.URL)
	os.Setenv("SENDGRID_KEY", "test-key")

	// Fire-and-forget: a assinatura de Notify não devolve erro algum — se
	// isso compilasse com um retorno, o teste nem precisaria rodar para
	// provar a distinção (REQ-25.3); o que resta a provar é que a chamada
	// HTTP de fato acontece com a forma declarada, mesmo o servidor
	// devolvendo 500 (o "fogo e esqueça" não impede a tentativa real).
	NotifyDepositNotification(context.Background(), DepositNotification{To: "a@b.com", Amount: 7})

	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotAuth != "test-key" {
		t.Errorf("Authorization = %q, want %q (env(\"SENDGRID_KEY\") resolvido)", gotAuth, "test-key")
	}
	if gotBody["to"] != "a@b.com" {
		t.Errorf("body.to = %v, want a@b.com", gotBody["to"])
	}
	if gotBody["amount"] != float64(7) {
		t.Errorf("body.amount = %v, want 7", gotBody["amount"])
	}
}
`

// paymentsBehaviorTestGo roda dentro do pacote "payments": prova que
// CallPaymentRequest (call, REQ-25.3) devolve EXATAMENTE o erro que
// adapters/payment_gateway.ProcessPayment devolve — sucesso e falha — e que
// os argumentos chegam marshalados corretamente (PaymentId convertido para
// string nativo via conversão de tipo Go, Amount passado direto — ver
// ffiArgGo, codegen/decl_io.go).
const paymentsBehaviorTestGo = `package payments

import (
	"context"
	"testing"

	"domainscript/generated/adapters/payment_gateway"
	"domainscript/generated/runtime"
)

func TestCallPropagatesForeignSuccess(t *testing.T) {
	payment_gateway.ProcessPaymentCalls = nil
	payment_gateway.ProcessPaymentErr = nil

	err := CallPaymentRequest(context.Background(), PaymentRequest{PaymentId: PaymentId("PAY-1"), Amount: 100})
	if err != nil {
		t.Fatalf("esperava sucesso, got %v", err)
	}
	if len(payment_gateway.ProcessPaymentCalls) != 1 {
		t.Fatalf("esperava 1 chamada marshalada, got %d", len(payment_gateway.ProcessPaymentCalls))
	}
	call := payment_gateway.ProcessPaymentCalls[0]
	if call.PaymentId != "PAY-1" || call.Amount != 100 {
		t.Fatalf("chamada marshalada incorreta: %+v", call)
	}
}

func TestCallPropagatesForeignError(t *testing.T) {
	payment_gateway.ProcessPaymentCalls = nil
	payment_gateway.ProcessPaymentErr = context.DeadlineExceeded

	err := CallPaymentRequest(context.Background(), PaymentRequest{PaymentId: PaymentId("PAY-2"), Amount: 50})
	if err == nil {
		t.Fatal("esperava erro propagado de adapters/payment_gateway, veio nil")
	}
}

// fakeCaller implementa runtime.Caller minimamente — o corpo de Pay lê
// caller.id (§design 3.1a); sem injetar um no ctx, runtime.CallerFrom
// devolve (nil, false) e a chamada de método sobre a interface nil panica
// (comportamento do próprio runtime, não desta task) — daí WithCaller
// abaixo, o mesmo mecanismo real que a borda HTTP usa (codegen/http.go,
// devCallerFromRequest).
type fakeCaller struct{}

func (fakeCaller) Authenticated() bool      { return true }
func (fakeCaller) ID() string               { return "test-caller" }
func (fakeCaller) HasRole(role string) bool { return false }

func TestPayUseCasePropagatesForeignError(t *testing.T) {
	payment_gateway.ProcessPaymentCalls = nil
	payment_gateway.ProcessPaymentErr = context.DeadlineExceeded
	// unit of work in-memory real (§design 3.8) — o corpo desta fixture não
	// carrega/despacha nenhum Aggregate, mas uow.Run precisa de uma instância
	// de verdade (nil causaria panic, não um erro controlado).
	Wire(runtime.NewUnitOfWork(runtime.NewMemoryEventStore()))

	ctx := runtime.WithCaller(context.Background(), fakeCaller{})
	err := Pay(ctx, MakePayment{PaymentId: PaymentId("PAY-3")})
	if err == nil {
		t.Fatal("esperava erro propagado do UseCase Pay (call síncrono), veio nil")
	}
}
`

// TestEmitIOBehavior prova NFR-15 sobre o Go de fato gerado: escreve os
// mesmos arquivos do smoke mais os testes comportamentais acima (um por
// pacote) num diretório isolado e roda "go test ./..." de verdade.
func TestEmitIOBehavior(t *testing.T) {
	files := ioSmokeFiles(t)
	files[filepath.Join("notify", "notify_behavior_test.go")] = []byte(notifyBehaviorTestGo)
	files[filepath.Join("payments", "payments_behavior_test.go")] = []byte(paymentsBehaviorTestGo)

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

// TestEmitIODeterministicFullPipeline garante que gerar os arquivos do
// smoke duas vezes produz exatamente os mesmos bytes (NFR-13) — mesmo
// espírito de TestEmitSagaDeterministic, agora sobre o conjunto inteiro de
// arquivos que compõem o smoke desta task.
func TestEmitIODeterministicFullPipeline(t *testing.T) {
	first := ioSmokeFiles(t)
	second := ioSmokeFiles(t)
	if len(first) != len(second) {
		t.Fatalf("quantidade de arquivos difere entre gerações: %d vs %d", len(first), len(second))
	}
	for path, content := range first {
		other, ok := second[path]
		if !ok {
			t.Fatalf("arquivo %q presente na 1ª geração, ausente na 2ª", path)
		}
		if string(content) != string(other) {
			t.Fatalf("arquivo %q difere entre gerações", path)
		}
	}
}
