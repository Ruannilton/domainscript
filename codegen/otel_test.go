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

// otel_test.go prova os critérios de conclusão da task H2 (§design codegen
// 3.13, REQ-30.1/30.2): observabilidade — log/slog + trace context por
// padrão (REQ-30.1, coberto por decl_worker_test.go/decl_saga_test.go/
// decl_io_test.go/http_test.go, que já exercitam "trace_id" nos logs de
// UseCase/Policy/Worker/Saga e o trace id mintado na borda HTTP/gRPC) e o
// adapter OTel opt-in atrás de runtime.Observer quando "Telemetry" é
// declarado (REQ-30.2, o foco deste arquivo). Nem docs/examples/wallet nem
// docs/examples/shop declaram "Telemetry" (confirmado antes de escrever esta
// task, grep em docs/examples/*/mod.ds) — a fixture é sintética, no mesmo
// espírito de GrpcDemo (grpc_test.go, H1)/Notes (tenancy_test.go, G5)/
// Billing (versioning_test.go, G6): um módulo "TelemetryDemo" mínimo com um
// Aggregate (Item), um UseCase (TouchItemUseCase, escrita) e uma Query
// (GetItem, leitura), expostos por Interface HTTP — e um mod.ds que declara
// "Telemetry { ... }" seguindo o exemplo do spec §12.

const telemetryDemoModDs = `Module TelemetryDemo {
    Database TelemetryDemoDb {
        provider: "postgres"
        manages: [Item]
    }

    Telemetry {
        exporter: "otlp"
        endpoint: env("OTEL_EXPORTER_ENDPOINT")
        traces { sampler: "parentbased_traceidratio", sampleRate: 0.1 }
    }
}
`

const telemetryDemoDomainDs = `
ValueObject ItemId(string) {
    Valid { value.length() > 0 }
}

ValueObject ItemNote(string) {
    Valid { value.length() <= 200 }
}

Event ItemTouched {
    id   ItemId
    note ItemNote
}

Aggregate Item {
    strategy EventSourced

    state {
        id   ItemId
        note ItemNote
    }

    access {
        Touch requires caller.authenticated
    }

    Handle Touch(note ItemNote) {
        emit ItemTouched(self.id, note)
    }

    Apply ItemTouched {
        state.note = event.note
    }
}
`

const telemetryDemoApplicationDs = `
Command TouchItem {
    itemId ref Item
    note   ItemNote
}

UseCase TouchItemUseCase handles TouchItem {
    execute {
        item = load Item(cmd.itemId)
        item.Touch(cmd.note)
    }
}
`

const telemetryDemoReadDs = `
View ItemView {
    id   ItemId
    note ItemNote
}

Query GetItem(id ItemId) -> ItemView {
    return load Item(id) as ItemView
}
`

const telemetryDemoInterfaceDs = `Interface HTTP {
    POST "/items/{id}/touch" -> TouchItemUseCase
    GET  "/items/{id}"       -> GetItem
}
`

// telemetryDemoGenerateOptions espelha grpcDemoGenerateOptions/
// walletGenerateOptions — mesmo module path que RuntimeImportPath assume
// implicitamente.
var telemetryDemoGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateTelemetryDemoProject escreve a fixture TelemetryDemo em disco e
// gera o projeto Go completo via driver.CheckProject + codegen.Generate —
// mesmo padrão de generateGRPCDemoProject/generateBillingProject.
func generateTelemetryDemoProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         telemetryDemoModDs,
		"domain.ds":      telemetryDemoDomainDs,
		"application.ds": telemetryDemoApplicationDs,
		"read.ds":        telemetryDemoReadDs,
		"interface.ds":   telemetryDemoInterfaceDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética TelemetryDemo (H2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, telemetryDemoGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture TelemetryDemo: %v", err)
	}
	return files
}

func telemetryDemoFileByPath(t *testing.T, files []codegen.File, p string) []byte {
	t.Helper()
	for _, f := range files {
		if f.Path == p {
			return f.Content
		}
	}
	t.Fatalf("%s não encontrado entre os arquivos gerados", p)
	return nil
}

func telemetryDemoCmdMainFile(t *testing.T) []byte {
	t.Helper()
	return telemetryDemoFileByPath(t, generateTelemetryDemoProject(t), "cmd/telemetrydemo/main.go")
}

// --- 1. Golden/determinismo/smoke — os critérios NFR-13/14 de sempre. ---

// TestGenerateTelemetryDemoMainGolden prova, sobre o Go de fato gerado em
// cmd/telemetrydemo/main.go: o adapter OTel real é construído (otelruntime.
// NewObserver) a partir do bloco "Telemetry" do mod.ds — endpoint via
// env(...) (os.Getenv), sampler/sampleRate de "traces", ServiceName = nome
// do grupo — e instalado via runtime.SetObserver ANTES de qualquer outro
// wiring (store/uow/servidor).
func TestGenerateTelemetryDemoMainGolden(t *testing.T) {
	got := string(telemetryDemoCmdMainFile(t))
	for _, want := range []string{
		"func main() {",
		"otelObserver, otelShutdown, err := otelruntime.NewObserver(context.Background(), otelruntime.Config{",
		`Endpoint:    os.Getenv("OTEL_EXPORTER_ENDPOINT"),`,
		`ServiceName: "telemetrydemo",`,
		`Sampler:     "parentbased_traceidratio",`,
		"SampleRate:  0.1,",
		"if err != nil {",
		"log.Fatal(err)",
		"defer otelShutdown(context.Background())",
		"runtime.SetObserver(otelObserver)",
		"store := runtime.NewMemoryEventStore()",
		`ctx, ucSpanEnd := runtime.RecordSpan(ctx, "UseCase.TouchItemUseCase")`,
		`ctx, qSpanEnd := runtime.RecordSpan(ctx, "Query.GetItem")`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q em cmd/telemetrydemo/main.go, não achei:\n%s", want, got)
		}
	}
	// A construção do Observer precisa vir ANTES de "store :=" — a ordem que
	// emitOTelWiring garante (ver a doc de generateCmdMainFile, codegen.go).
	if strings.Index(got, "otelObserver, otelShutdown, err :=") > strings.Index(got, "store := runtime.NewMemoryEventStore()") {
		t.Fatalf("esperava o wiring do Observer ANTES de \"store :=\", ordem inesperada:\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "cmd_telemetrydemo_main.go.golden"), []byte(got))
}

// TestGenerateTelemetryDemoDeterministic prova NFR-13 escopado a
// cmd/telemetrydemo/main.go — mesmo padrão de TestGenerateGRPCDemoDeterministic.
func TestGenerateTelemetryDemoDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return telemetryDemoCmdMainFile(t)
	})
}

// TestGenerateTelemetryDemoSmokeCompile prova NFR-14: o projeto gerado
// inteiro — incl. otelruntime/*.go (vendorado, com o observer_test.go
// embutido, ver codegen/otelrt/observer_test.go.txt) e go.mod com "require
// go.opentelemetry.io/..." — compila e passa go vet. gentest.SmokeCompile
// detecta o bloco "require" e roda `go mod tidy` primeiro (precisa de acesso
// à rede ao proxy de módulos Go; já confirmado disponível neste ambiente
// antes de escrever esta task — um probe isolado com os 4 módulos OTel
// pinados em project.go resolveu, compilou e passou go vet/go test).
func TestGenerateTelemetryDemoSmokeCompile(t *testing.T) {
	files := generateTelemetryDemoProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// TestGenerateTelemetryDemoRuntimeBehavior roda `go test ./...` de verdade
// sobre o projeto isolado gerado (gentest.RunTests, mesmo padrão de
// TestGenerateGRPCDemoBehavior): otelruntime/observer_test.go (embutido
// verbatim a partir de codegen/otelrt/observer_test.go.txt) prova, sobre um
// in-memory tracetest.SpanRecorder, que RecordSpan produz um span de
// verdade (nome + status) e que NewObserver constrói/desliga sem exigir um
// collector real escutando — a prova comportamental do DoD #6 (adapter OTel
// produz span), sem precisar de infraestrutura externa no teste.
func TestGenerateTelemetryDemoRuntimeBehavior(t *testing.T) {
	files := filesToMap(generateTelemetryDemoProject(t))
	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}

// --- 2. Comportamento de verdade: RecordSpan na borda HTTP com um Observer
// fake instalado (prova a FIAÇÃO em http.go, sem depender do SDK real do
// OTel nem de rede). ---
//
// telemetryDemoObserverBehaviorTest roda DENTRO de cmd/telemetrydemo (mesmo
// pacote de newMux, "package main") e instala um runtime.Observer fake via
// runtime.SetObserver ANTES de exercitar newMux via httptest — prova que
// emitUseCaseRoute/emitQueryRoute (http.go, H2) de fato chamam
// runtime.RecordSpan com o nome esperado ("UseCase.TouchItemUseCase",
// "Query.GetItem") e propagam o ctx atualizado ao dispatch, sem precisar de
// um Observer OTel real nem de um collector.
const telemetryDemoObserverBehaviorTest = `package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/telemetrydemo"
)

// fakeObserver grava os nomes de span iniciados (protegido por mutex — os
// handlers HTTP podem, em tese, ser exercitados concorrentemente).
type fakeObserver struct {
	mu    sync.Mutex
	names []string
}

func (f *fakeObserver) RecordSpan(ctx context.Context, name string) (context.Context, func(err error)) {
	f.mu.Lock()
	f.names = append(f.names, name)
	f.mu.Unlock()
	return ctx, func(error) {}
}

func (f *fakeObserver) TraceID(ctx context.Context) (string, bool) { return "", false }

func (f *fakeObserver) recorded(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.names {
		if n == name {
			return true
		}
	}
	return false
}

func TestHTTPEdgeRecordsSpansViaInstalledObserver(t *testing.T) {
	obs := &fakeObserver{}
	runtime.SetObserver(obs)
	defer runtime.SetObserver(nil)

	store := runtime.NewMemoryEventStore()
	telemetrydemo.Wire(runtime.NewUnitOfWork(store))
	mux := newMux(store)

	body := bytes.NewBufferString(` + "`" + `{"note":"hello"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/items/I1/touch", body)
	req.Header.Set("X-Caller-Id", "tester")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /items/{id}/touch: status = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}
	if !obs.recorded("UseCase.TouchItemUseCase") {
		t.Fatalf("esperava um span \"UseCase.TouchItemUseCase\" registrado, achei: %v", obs.names)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/items/I1", nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /items/{id}: status = %d, want 200; body: %s", getRec.Code, getRec.Body.String())
	}
	if !obs.recorded("Query.GetItem") {
		t.Fatalf("esperava um span \"Query.GetItem\" registrado, achei: %v", obs.names)
	}
}
`

// TestGenerateTelemetryDemoObserverBehavior escreve o projeto gerado +
// telemetryDemoObserverBehaviorTest em cmd/telemetrydemo, e roda `go test
// ./...` de verdade sobre ele (gentest.RunTests — o go.mod tem "require",
// precisa de `go mod tidy`).
func TestGenerateTelemetryDemoObserverBehavior(t *testing.T) {
	files := filesToMap(generateTelemetryDemoProject(t))
	files[filepath.Join("cmd", "telemetrydemo", "main_observer_behavior_test.go")] = []byte(telemetryDemoObserverBehaviorTest)
	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}

// --- 3. Regressão: sem "Telemetry", nada muda (REQ-30.2, NFR-12). ---

// TestGenerateWalletProjectHasNoOTelArtifacts prova que um programa sem
// nenhuma "Telemetry { ... }" (o wallet real) continua sem NENHUM artefato
// de H2: nenhum arquivo otelruntime/*.go e go.mod sem
// "go.opentelemetry.io/..." — mesmo espírito de
// TestGenerateWalletProjectHasNoGRPCArtifacts (grpc_test.go, H1).
func TestGenerateWalletProjectHasNoOTelArtifacts(t *testing.T) {
	files := generateWalletProject(t)
	for _, f := range files {
		if strings.HasPrefix(f.Path, "otelruntime/") {
			t.Fatalf("NFR-12: wallet não deveria gerar nenhum arquivo otelruntime/* (sem Telemetry), achei %q", f.Path)
		}
	}
	goMod := grpcDemoFileByPathOrNil(files, "go.mod")
	if goMod == nil {
		t.Fatal("esperava go.mod entre os arquivos gerados do wallet")
	}
	if strings.Contains(string(goMod), "go.opentelemetry.io") {
		t.Fatalf("NFR-12: wallet não deveria ter \"go.opentelemetry.io\" em go.mod (sem Telemetry), achei:\n%s", goMod)
	}
}
