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

// decl_worker_test.go prova os critérios de conclusão da task F2 (§design
// codegen 3.10, REQ-23.2/23.3) — EmitWorker(s) não tem exemplo real no shop
// mínimo ("Fixture; sem Worker no shop mínimo", tasks.md) — coberta por uma
// fixture SINTÉTICA que exercita os 3 modos de schedule do spec §8: "every"
// (com concurrency/batchSize/maxRate/timeout/onError.retry), "cron" e
// "continuous" (com source/where). Mesmo padrão de decl_projection_test.go
// (E8.2): parseada e validada via driver.CheckProject (EmitWorkers exige um
// types.Model + symbols.SymbolTable resolvidos).

// workerFixtureModDs é o mínimo de mod.ds exigido pelo programa (mesma forma
// mínima de docs/examples/shop/shipping/mod.ds: "Module Nome { }" — este
// módulo não declara nenhum Aggregate/Database, Worker não precisa de
// nenhum).
const workerFixtureModDs = `Module Reservations { }
`

// workerFixtureSrc declara o domínio mínimo que os 3 Workers referenciam
// (QueueItemId/QueueItemStatus/QueueItem) e os 3 Workers em si:
//
//   - ProcessExpiredReservations (every): concurrency/batchSize/maxRate/
//     timeout/onError.retry — o corpo do critério de conclusão desta task.
//     "every 50ms" (não "every 1min", como o exemplo do spec) para que o
//     teste comportamental de disparo real (TestGenerateWorkerEveryFires
//     AndRunsBodyBehavior, abaixo) não precise esperar um minuto de verdade —
//     a forma Go gerada não depende do valor da duração, só golden/smoke se
//     importariam com "1min" literal, e nenhum dos dois compara a duração
//     bruta (ver TestEmitWorkersGolden).
//   - DailySettlement (cron): a forma mínima do spec, sem Settings.
//   - ProcessQueueItems (continuous): concurrency/batchSize/maxRate + source
//     com where (REQ-23.2) — o caso "ideal se der tempo" do prompt da task.
const workerFixtureSrc = `
ValueObject QueueItemId(string) {
    Valid { value.length() > 0 }
}

Enum QueueItemStatus : string {
    Pending    = "PENDING"
    Processed  = "PROCESSED"
}

ValueObject QueueItem {
    id     QueueItemId
    status QueueItemStatus

    Valid { ok }
}

Worker ProcessExpiredReservations {
    schedule every 50ms
    concurrency: 2
    batchSize: 10
    maxRate: 20
    timeout 5min
    onError { retry: { attempts: 3, backoff: "exponential" } }
    execute {
        log Info "worker tick"
    }
}

Worker DailySettlement {
    schedule cron "0 2 * * *"
    execute {
        return
    }
}

Worker ProcessQueueItems {
    schedule continuous
    concurrency: 3
    batchSize: 5
    maxRate: 50
    source { list QueueItem q where q.status == QueueItemStatus.Pending }
    execute(item) {
        log Info "processing item"
    }
}
`

// findWorkerDecl acha o *ast.WorkerDecl de nome name em qualquer arquivo do
// programa — espelha findPolicyDecl/findProjectionDecl.
func findWorkerDecl(t *testing.T, prog *program.Program, name string) *ast.WorkerDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if w, ok := d.(*ast.WorkerDecl); ok && w.Name == name {
				return w
			}
		}
	}
	t.Fatalf("Worker %q não encontrado na fixture — o exemplo mudou?", name)
	return nil
}

// parseWorkerFixture monta o projeto sintético em disco (mod.ds + domain.ds)
// e o resolve via driver.CheckProject — devolve o Program e os 3 WorkerDecl.
func parseWorkerFixture(t *testing.T) (prog *program.Program, every, cron, continuous *ast.WorkerDecl) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    workerFixtureModDs,
		"domain.ds": workerFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Worker (F2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	every = findWorkerDecl(t, prog, "ProcessExpiredReservations")
	cron = findWorkerDecl(t, prog, "DailySettlement")
	continuous = findWorkerDecl(t, prog, "ProcessQueueItems")
	return
}

// emitWorkerFixture gera o Go dos 3 Workers da fixture, num único arquivo
// (workers.go) — mesma forma de EmitPolicies sobre >1 Policy.
func emitWorkerFixture(t *testing.T) []byte {
	t.Helper()
	prog, every, cron, continuous := parseWorkerFixture(t)
	model := types.NewModel(prog.Symbols)
	reg := walletVOOperatorRegistryFromProgram(prog) // nenhum Operator na fixture, registry vazio é suficiente

	got, err := codegen.EmitWorkers("reservations", []*ast.WorkerDecl{every, cron, continuous}, model, prog.Symbols, "Reservations", reg)
	if err != nil {
		t.Fatalf("EmitWorkers: erro inesperado: %v", err)
	}
	return got
}

// --- Golden + determinismo -------------------------------------------------

// TestEmitWorkersGolden prova os elementos centrais do critério de conclusão
// da task: schedule every -> ticker + semáforo (concurrency) + rate limiter
// (maxRate) + retry (onError.retry, backoff exponencial) + timeout por tick;
// schedule cron -> runtime.ParseCron + CronSchedule.Next; schedule continuous
// -> runtime.Source[QueueItem] + pool de goroutines (concurrency) + canal
// (batchSize) + predicado do "where"; e StartWorkers (não "Wire" — ver a doc
// de decl_worker.go) iniciando os 3.
func TestEmitWorkersGolden(t *testing.T) {
	got := emitWorkerFixture(t)
	for _, want := range []string{
		// every
		"func ProcessExpiredReservations(ctx context.Context)",
		"sem := runtime.NewSemaphore(2)",
		"limiter := runtime.NewRateLimiter(20)",
		"retry := runtime.RetryPolicy{Attempts: 3, Backoff: runtime.BackoffExponential}",
		"ticker := time.NewTicker(time.Duration(50000000))",
		"execCtx, cancel := context.WithTimeout(ctx, time.Duration(300000000000))",
		"func processExpiredReservationsTick(ctx context.Context) error",
		`slog.Info("worker tick", "trace_id", runtime.TraceIDFrom(ctx))`,
		// cron
		"func DailySettlement(ctx context.Context)",
		`sched, err := runtime.ParseCron("0 2 * * *")`,
		"next := sched.Next(time.Now())",
		"func dailySettlementTick(ctx context.Context) error",
		// continuous
		"var processQueueItemsSource runtime.Source[QueueItem] = runtime.NewSliceSource[QueueItem](nil)",
		"func ProcessQueueItems(ctx context.Context)",
		"items := make(chan QueueItem, 5)",
		"for i := 0; i < 3; i++",
		"q, ok, err := processQueueItemsSource.Next(ctx)",
		"if !(q.Status == QueueItemStatusPending)",
		"func processQueueItemsExecute(ctx context.Context, item QueueItem) error",
		// StartWorkers, não Wire (ver a doc de decl_worker.go)
		"func StartWorkers(ctx context.Context)",
		"go ProcessExpiredReservations(ctx)",
		"go DailySettlement(ctx)",
		"go ProcessQueueItems(ctx)",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "func Wire(") {
		t.Errorf("Worker NÃO deveria emitir \"func Wire\" (ver a doc de decl_worker.go sobre a colisão de F1):\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "workers_reservations.go.golden"), got)
}

// TestEmitWorkersDeterministic prova NFR-13.
func TestEmitWorkersDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWorkerFixture(t)
	})
}

// --- Smoke compile -----------------------------------------------------------

// workerSmokeFiles monta os arquivos mínimos para compilar os 3 Workers
// junto do domínio que referenciam: go.mod + runtime real +
// reservations/value_objects.go (QueueItemId/QueueItem + Enum
// QueueItemStatus) + reservations/workers.go.
func workerSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog, _, _, _ := parseWorkerFixture(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	// Um arquivo Go por ValueObject/Enum — mesmo padrão de projectionSmokeFiles
	// (decl_projection_test.go): EmitValueObject/EmitEnum são só-singular (a
	// combinação num único value_objects.go é emitValueObjectsAndEnums, não
	// exportada — codegen.go, generateModuleFiles).
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			switch n := d.(type) {
			case *ast.ValueObjectDecl:
				got, err := codegen.EmitValueObject("reservations", n)
				if err != nil {
					t.Fatalf("EmitValueObject(%s): erro inesperado: %v", n.Name, err)
				}
				files[filepath.Join("reservations", strings.ToLower(n.Name)+".go")] = got
			case *ast.EnumDecl:
				got, err := codegen.EmitEnum("reservations", n)
				if err != nil {
					t.Fatalf("EmitEnum(%s): erro inesperado: %v", n.Name, err)
				}
				files[filepath.Join("reservations", strings.ToLower(n.Name)+".go")] = got
			}
		}
	}

	files[filepath.Join("reservations", "workers.go")] = emitWorkerFixture(t)
	return files
}

// TestEmitWorkersSmokeCompile prova NFR-14: o Go dos 3 Workers, junto do
// domínio que referenciam e do runtime vendorado real, compila e passa go
// vet num projeto isolado.
func TestEmitWorkersSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, workerSmokeFiles(t))
}

// --- Orquestrador completo (codegen.Generate) --------------------------------

// generateWorkerFixtureProject roda Generate sobre o Program da fixture
// sintética de Worker (mesmo padrão de generateWalletProject/
// generateShopProject: driver.CheckProject fresco + codegen.Generate — não
// reusa parseWorkerFixture porque este precisa do bag de CheckProject, que
// aquele helper descarta).
func generateWorkerFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    workerFixtureModDs,
		"domain.ds": workerFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Worker (F2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de Worker: %v", err)
	}
	return files
}

// TestGenerateWorkerFixtureWiresStartWorkersAndCompiles prova o item 3 do
// prompt da task ("Wire Workers into module generation... e into Wire,
// resolvendo o gap de colisão do F1"): rodando o orquestrador COMPLETO
// (codegen.Generate, não EmitWorkers isolado) sobre o projeto sintético,
// cmd/reservations/main.go (o único módulo, monólito implícito — ver
// defaultCmdDirName) chama "reservations.StartWorkers(workerCtx)" — nome
// PRÓPRIO, nunca "Wire" (o módulo desta fixture não declara UseCase nem
// Policy, então nem sequer existiria um "func Wire" para colidir; a garantia
// de que Worker NUNCA soma uma 2ª colisão mesmo quando UseCase/Policy
// existirem no mesmo módulo é estrutural — StartWorkers é sempre um nome
// distinto, ver a doc de decl_worker.go — não depende de um exemplo real
// combinando as três categorias, que nem o wallet nem o shop de hoje têm) —
// e o projeto gerado INTEIRO compila.
func TestGenerateWorkerFixtureWiresStartWorkersAndCompiles(t *testing.T) {
	files := generateWorkerFixtureProject(t)
	m := filesToMap(files)

	workersGo, ok := m["reservations/workers.go"]
	if !ok {
		t.Fatalf("esperava reservations/workers.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}
	if !strings.Contains(string(workersGo), "func StartWorkers(ctx context.Context)") {
		t.Errorf("esperava StartWorkers em reservations/workers.go, não achei:\n%s", workersGo)
	}

	mainGo, ok := m["cmd/reservations/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/reservations/main.go, não achei:\n%v", filePathsForTest(files))
	}
	for _, want := range []string{
		"workerCtx := context.Background()",
		"reservations.StartWorkers(workerCtx)",
	} {
		if !strings.Contains(string(mainGo), want) {
			t.Errorf("esperava %q em cmd/reservations/main.go, não achei:\n%s", want, mainGo)
		}
	}
	if strings.Contains(string(mainGo), "reservations.Wire(") {
		t.Errorf("NÃO esperava \"reservations.Wire(...)\" em cmd/reservations/main.go (o módulo não declara UseCase/Policy):\n%s", mainGo)
	}

	gentest.SmokeCompile(t, m)
}

// --- Comportamento -----------------------------------------------------------

// workerBehaviorTest roda dentro do projeto isolado gerado no smoke, no MESMO
// pacote Go dos Workers (permite chamar as funções privadas <nome>Tick/
// <nome>Execute diretamente) e prova, sobre o Go de fato gerado:
//
//  1. TestTickFunctionsRunLoweredBodyWithoutError chama processExpiredReserv
//     ationsTick/dailySettlementTick/processQueueItemsExecute diretamente
//     (sem depender de ticker/cron real — determinístico, sem risco de
//     flakiness) e confere que o corpo lowerizado roda sem erro.
//  2. TestProcessExpiredReservationsFiresViaRealTicker prova que o schedule
//     "every" de fato dispara sozinho: inicia a goroutine real (ProcessExpi
//     redReservations, o "func Nome(ctx)" gerado) com um slog.Handler que só
//     conta chamadas, espera (com folga generosa, sem apostar em timing
//     exato) pelo menos 1 disparo, e cancela — prova o ticker+semáforo+rate
//     limiter+retry+tick disparando de ponta a ponta, não só o texto Go.
const workerBehaviorTest = `package reservations

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestTickFunctionsRunLoweredBodyWithoutError(t *testing.T) {
	ctx := context.Background()

	if err := processExpiredReservationsTick(ctx); err != nil {
		t.Fatalf("processExpiredReservationsTick: erro inesperado: %v", err)
	}
	if err := dailySettlementTick(ctx); err != nil {
		t.Fatalf("dailySettlementTick: erro inesperado: %v", err)
	}
	if err := processQueueItemsExecute(ctx, QueueItem{Status: QueueItemStatusPending}); err != nil {
		t.Fatalf("processQueueItemsExecute: erro inesperado: %v", err)
	}
}

// countingHandler é um slog.Handler mínimo que só conta quantas vezes Handle
// é chamado — usado para provar que o ticker real de "every" disparou pelo
// menos uma vez, sem inspecionar o conteúdo do log (fora do escopo desta
// prova).
type countingHandler struct {
	count *atomic.Int64
}

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countingHandler) Handle(context.Context, slog.Record) error {
	h.count.Add(1)
	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }

func TestProcessExpiredReservationsFiresViaRealTicker(t *testing.T) {
	var count atomic.Int64
	prev := slog.Default()
	slog.SetDefault(slog.New(countingHandler{count: &count}))
	defer slog.SetDefault(prev)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ProcessExpiredReservations(ctx)

	deadline := time.After(2 * time.Second)
	for {
		if count.Load() > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("esperava que o Worker ProcessExpiredReservations (schedule every 50ms) disparasse pelo menos 1 vez em 2s")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
`

// TestEmitWorkersBehavior prova NFR-15 sobre o Go de fato gerado: escreve os
// mesmos arquivos do smoke mais o teste comportamental acima num diretório
// isolado e roda "go test ./..." de verdade.
func TestEmitWorkersBehavior(t *testing.T) {
	files := workerSmokeFiles(t)
	files[filepath.Join("reservations", "workers_behavior_test.go")] = []byte(workerBehaviorTest)

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
