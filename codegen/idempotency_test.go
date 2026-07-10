package codegen_test

import (
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

// idempotency_test.go prova os critérios de conclusão da task G2 (§design
// codegen 3.8, REQ-20.4, spec §14): idempotência real de Command (chave do
// cliente, cache de sucesso/erro de negócio, conflito, corrida da mesma
// chave) e fecha os dois pontos que E7.2 deixou explicitamente adiados —
// "ensure ... exists" (aqui, "ensure tally exists else TallyNotFound", um
// Execute real parseado do .ds, não reconstruído à mão) e o mapeamento
// estável Idempotency-Key -> sagaId de uma Saga async (F3).
//
// --- A fixture: Tally, um Aggregate mínimo com idempotência (§14) ---
//
// Tally é EventSourced (a única estratégia que E7.2/G1 já wireiam através de
// um UseCase — StateStored nunca foi ligado a um UseCase, ver
// decl_aggregate_load_test.go: LoadX StateStored pede um
// runtime.Repository[T], não o runtime.Tx que o dispatch de Handle usa; um
// gap pré-existente, fora do escopo de G2). Uma consequência DOCUMENTADA:
// LoadTally (EventSourced) NUNCA devolve nil — um stream vazio ainda
// reconstrói um *Tally zero-value (mesmo gap do wallet, ver
// generate_e2e_wallet_test.go) — então "ensure tally exists else
// TallyNotFound" never actually FIRES sobre esta fixture; o que esta
// suite prova é que a tradução ("if !(tally != nil) { return
// ErrTallyNotFound }") COMPILA de verdade a partir do .ds real (o
// TestEmitUseCaseIdempotencyWaitGolden abaixo), fechando o "adiado" de
// E7.2 no nível de lowering — o mesmo nível em que o resto do gerador já
// documenta esse gap de LoadX EventSourced como pré-existente, não uma
// falha desta task.
//
// O "erro de negócio" cacheável (REQ-20.4) vem do `access` de Increment
// (`caller.authenticated`) em vez de uma regra de domínio nova: chamar sem
// caller autenticado sempre falha com runtime.ErrForbidden — determinístico,
// sem precisar de nenhum mecanismo de mock (H4 é fora de escopo aqui).
const idemFixtureSrc = `
ValueObject TallyId(string) {
    Valid { value.length() > 0 }
}

ValueObject Amount(integer) {
    Valid { ok }
}

Event Incremented {
    id TallyId
    amount Amount
}

Error TallyNotFound {
    message "tally not found"
}

Aggregate Tally {
    strategy EventSourced

    state {
        id TallyId
        total Amount
    }

    access {
        Increment requires caller.authenticated
    }

    Handle Increment(amount Amount) {
        emit Incremented(self.id, amount)
    }

    Apply Incremented {
        state.total = event.amount
    }
}

Command IncrementTally {
    tallyId ref Tally
    amount Amount
}

UseCase PerformIncrement handles IncrementTally {
    idempotency { required: true, window: 1h }
    execute {
        tally = load Tally(cmd.tallyId)
        ensure tally exists else TallyNotFound
        tally.Increment(cmd.amount)
    }
}
`

// idemFixtureModDsWait é o mod.ds padrão (sem bloco Idempotency{} — todo
// default do gerador vale: concurrentRetry "wait", concurrentTimeout 30s).
const idemFixtureModDsWait = `Module Tally {
    Database TallyDb {
        provider: "postgres"
        manages: [Tally]
    }
}
`

// idemFixtureModDsReject espelha idemFixtureModDsWait, sobrescrevendo
// concurrentRetry para "reject" (spec §14) — usado pelos testes que provam
// o outro lado da política de corrida.
const idemFixtureModDsReject = `Module Tally {
    Database TallyDb {
        provider: "postgres"
        manages: [Tally]
    }

    Idempotency {
        concurrentRetry: reject
    }
}
`

// findIdemUseCaseDecl acha o *ast.UseCaseDecl de nome name — espelha
// findUseCaseDeclIO (decl_io_test.go), com nome próprio para não colidir.
func findIdemUseCaseDecl(t *testing.T, prog *program.Program, name string) *ast.UseCaseDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if u, ok := d.(*ast.UseCaseDecl); ok && u.Name == name {
				return u
			}
		}
	}
	t.Fatalf("UseCase %q não encontrado na fixture de idempotência (G2)", name)
	return nil
}

// findErrorTypeDeclIdem acha o *ast.ErrorTypeDecl de nome name.
func findErrorTypeDeclIdem(t *testing.T, prog *program.Program, name string) *ast.ErrorTypeDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if e, ok := d.(*ast.ErrorTypeDecl); ok && e.Name == name {
				return e
			}
		}
	}
	t.Fatalf("Error %q não encontrado na fixture de idempotência (G2)", name)
	return nil
}

// parseIdemFixture monta o projeto sintético (mod.ds + domain.ds) em disco e
// o resolve via driver.CheckProject — devolve o Program e os decls que os
// emissores desta suíte precisam.
func parseIdemFixture(t *testing.T, modDs string) (*program.Program, *ast.AggregateDecl, *ast.CommandDecl, *ast.UseCaseDecl) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    modDs,
		"domain.ds": idemFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de idempotência (G2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	agg := findAggregateDecl(t, prog, "Tally")
	cmd := findCommandDecl(t, prog, "IncrementTally")
	uc := findIdemUseCaseDecl(t, prog, "PerformIncrement")
	return prog, agg, cmd, uc
}

// emitIdemUseCase gera o Go de PerformIncrement — o caminho comum aos testes
// de golden/determinismo desta suíte.
func emitIdemUseCase(t *testing.T, pkg, modDs string) []byte {
	t.Helper()
	prog, agg, _, uc := parseIdemFixture(t, modDs)
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)
	aggregates := map[string]*ast.AggregateDecl{"Tally": agg}

	got, err := codegen.EmitUseCase(pkg, uc, aggregates, prog, model, prog.Symbols, "Tally", reg, nil)
	if err != nil {
		t.Fatalf("EmitUseCase(PerformIncrement): erro inesperado: %v", err)
	}
	return got
}

// --- Golden + determinismo (modo wait, o default) ---------------------------

// TestEmitUseCaseIdempotencyWaitGolden prova, sobre o Go de fato gerado, os
// elementos centrais do critério de conclusão de G2: o corpo de sempre migra
// para um nome privado (performIncrementRun), "ensure tally exists" traduz
// para "if !(tally != nil)" (o "adiado" de E7.2, ver a doc do arquivo), e o
// wrapper público (PerformIncrement) tem a chave/fingerprint/Begin/switch de
// outcomes/Complete/Release completos.
func TestEmitUseCaseIdempotencyWaitGolden(t *testing.T) {
	got := emitIdemUseCase(t, "tally", idemFixtureModDsWait)
	s := string(got)
	for _, want := range []string{
		"func performIncrementRun(ctx context.Context, cmd IncrementTally) error",
		"if !(tally != nil) {",
		"return ErrTallyNotFound",
		"func PerformIncrement(ctx context.Context, cmd IncrementTally) error {",
		"key, hasKey := runtime.IdempotencyKeyFrom(ctx)",
		"return runtime.ErrIdempotencyKeyRequired",
		"fingerprint := runtime.IdempotencyFingerprint(cmd)",
		"begin, err := idem.Begin(ctx, key, fingerprint)",
		"case runtime.BeginConflict:",
		"return runtime.ErrIdempotencyKeyConflict",
		"case runtime.BeginCached:",
		"return begin.Cached.Err()",
		"case runtime.BeginInFlight:",
		"idem.Wait(waitCtx, key)",
		"runErr := performIncrementRun(ctx, cmd)",
		"!runtime.IsBusinessError(runErr)",
		"_ = idem.Release(ctx, key)",
		"_ = idem.Complete(ctx, key, runtime.NewCompletedResult(runErr)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, s)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "usecase_increment_idempotent.go.golden"), got)
}

// TestEmitUseCaseIdempotencyDeterministic prova NFR-13.
func TestEmitUseCaseIdempotencyDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitIdemUseCase(t, "tally", idemFixtureModDsWait)
	})
}

// TestEmitUseCaseIdempotencyRejectGolden prova a forma "reject" (mod.ds
// Idempotency.concurrentRetry, spec §14): a corrida da mesma chave falha
// direto com ErrIdempotencyInFlight, sem NUNCA chamar idem.Wait.
func TestEmitUseCaseIdempotencyRejectGolden(t *testing.T) {
	got := emitIdemUseCase(t, "tallyreject", idemFixtureModDsReject)
	s := string(got)
	if !strings.Contains(s, "return runtime.ErrIdempotencyInFlight") {
		t.Fatalf("esperava \"return runtime.ErrIdempotencyInFlight\" no modo reject, não achei:\n%s", s)
	}
	if strings.Contains(s, "idem.Wait(") {
		t.Fatalf("modo reject NUNCA deveria chamar idem.Wait (deveria falhar rápido):\n%s", s)
	}
	gentest.Golden(t, filepath.Join("testdata", "usecase_increment_idempotent_reject.go.golden"), got)
}

// TestEmitUseCaseIdempotencyRejectDeterministic prova NFR-13 para o modo reject.
func TestEmitUseCaseIdempotencyRejectDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitIdemUseCase(t, "tallyreject", idemFixtureModDsReject)
	})
}

// --- Smoke compile + comportamental (NFR-14/15) -----------------------------

// idemSmokeFiles monta o conjunto completo de arquivos de um projeto isolado
// (go.mod + runtime real + VOs/Event/Error/Aggregate/Load/Command/UseCase) —
// mesmo padrão de meterSmokeFiles (decl_usecase_test.go)/counterSmokeFiles
// (decl_aggregate_load_test.go). pkg permite gerar duas variantes
// independentes (wait/reject) sem colidirem no mesmo diretório de teste.
func idemSmokeFiles(t *testing.T, pkg, modDs string) map[string][]byte {
	t.Helper()
	prog, agg, cmdDecl, uc := parseIdemFixture(t, modDs)
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}
	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	tallyID := findValueObjectDecl(t, prog, "TallyId")
	amount := findValueObjectDecl(t, prog, "Amount")
	for _, spec := range []struct {
		decl *ast.ValueObjectDecl
		file string
	}{
		{tallyID, "tally_id.go"},
		{amount, "amount.go"},
	} {
		got, err := codegen.EmitValueObject(pkg, spec.decl)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", spec.decl.Name, err)
		}
		files[filepath.Join(pkg, spec.file)] = got
	}

	eventsGo, err := codegen.EmitEvents(pkg, []*ast.EventDecl{findEventDecl(t, prog, "Incremented")})
	if err != nil {
		t.Fatalf("EmitEvents: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "events.go")] = eventsGo

	errGo, err := codegen.EmitErrors(pkg, []*ast.ErrorTypeDecl{findErrorTypeDeclIdem(t, prog, "TallyNotFound")})
	if err != nil {
		t.Fatalf("EmitErrors: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "errors.go")] = errGo

	aggGo, err := codegen.EmitAggregate(pkg, agg, model, prog.Symbols, "Tally", reg)
	if err != nil {
		t.Fatalf("EmitAggregate: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "aggregate_tally.go")] = aggGo

	loadGo, err := codegen.EmitAggregateLoad(pkg, agg, model, prog.Symbols, "Tally")
	if err != nil {
		t.Fatalf("EmitAggregateLoad: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "aggregate_tally_load.go")] = loadGo

	cmdGo, err := codegen.EmitCommand(pkg, cmdDecl, model, prog.Symbols, "Tally")
	if err != nil {
		t.Fatalf("EmitCommand: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "commands.go")] = cmdGo

	aggregates := map[string]*ast.AggregateDecl{"Tally": agg}
	ucGo, err := codegen.EmitUseCase(pkg, uc, aggregates, prog, model, prog.Symbols, "Tally", reg, nil)
	if err != nil {
		t.Fatalf("EmitUseCase(PerformIncrement): erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "usecases.go")] = ucGo

	return files
}

func TestEmitUseCaseIdempotencyWaitSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, idemSmokeFiles(t, "tally", idemFixtureModDsWait))
}

func TestEmitUseCaseIdempotencyRejectSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, idemSmokeFiles(t, "tallyreject", idemFixtureModDsReject))
}

// idemStubCallerType/flakyStore/gatedStore (dentro das strings abaixo) rodam
// DENTRO do pacote isolado gerado (mesmo padrão de e2eCaller/ucStubCaller —
// decl_aggregate_load_test.go/decl_usecase_test.go): "uow"/"idem" são vars
// de pacote não-exportadas, reatribuídas diretamente pelo teste para isolar
// cada caso (mesma técnica de "uow = runtime.NewUnitOfWork(store)" já usada
// em todo o pacote codegen_test).
//
// flakyStore simula um erro de INFRAESTRUTURA transitório (falha o 1º
// Append, sucede depois) — prova REQ-20.4 "erro de infra permite retry"
// (NÃO cacheado). gatedStore trava o 1º Append até o teste liberar,
// permitindo forçar uma corrida real e determinística entre duas goroutines
// (concurrentRetry: wait/reject) sem depender de timing por sorte.
const idemWaitBehaviorTest = `package tally

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"domainscript/generated/runtime"
)

type idemStubCaller struct{ authenticated bool }

func (c idemStubCaller) Authenticated() bool { return c.authenticated }
func (c idemStubCaller) ID() string          { return "tester" }
func (c idemStubCaller) HasRole(string) bool  { return false }

// flakyStore falha a 1ª chamada de Append (erro de infra simulado) e sucede
// em todas as chamadas seguintes.
type flakyStore struct {
	inner    runtime.EventStore
	mu       sync.Mutex
	failNext bool
}

func (s *flakyStore) Append(ctx context.Context, aggregateID string, events []runtime.Event) error {
	s.mu.Lock()
	if s.failNext {
		s.failNext = false
		s.mu.Unlock()
		return errors.New("infra: armazenamento indisponível (simulado)")
	}
	s.mu.Unlock()
	return s.inner.Append(ctx, aggregateID, events)
}

func (s *flakyStore) Load(ctx context.Context, aggregateID string) ([]runtime.Event, error) {
	return s.inner.Load(ctx, aggregateID)
}

// gatedStore trava a 1ª chamada de Append até o teste liberar via proceed.
type gatedStore struct {
	inner   runtime.EventStore
	entered chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (s *gatedStore) Append(ctx context.Context, aggregateID string, events []runtime.Event) error {
	s.once.Do(func() {
		close(s.entered)
		<-s.proceed
	})
	return s.inner.Append(ctx, aggregateID, events)
}

func (s *gatedStore) Load(ctx context.Context, aggregateID string) ([]runtime.Event, error) {
	return s.inner.Load(ctx, aggregateID)
}

func TestIdempotencyKeyRequiredWhenMissing(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow = runtime.NewUnitOfWork(store)
	idem = runtime.NewMemoryIdempotencyStore()

	ctx := runtime.WithCaller(context.Background(), idemStubCaller{authenticated: true})
	cmd := IncrementTally{TallyId: TallyId("t7"), Amount: Amount(1)}

	err := PerformIncrement(ctx, cmd)
	if !errors.Is(err, runtime.ErrIdempotencyKeyRequired) {
		t.Fatalf("esperava ErrIdempotencyKeyRequired sem Idempotency-Key, got %v", err)
	}
}

func TestIdempotencySuccessCachedNotRerun(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow = runtime.NewUnitOfWork(store)
	idem = runtime.NewMemoryIdempotencyStore()

	ctx := runtime.WithCaller(context.Background(), idemStubCaller{authenticated: true})
	ctx = runtime.WithIdempotencyKey(ctx, "success-key")
	cmd := IncrementTally{TallyId: TallyId("t1"), Amount: Amount(3)}

	if err := PerformIncrement(ctx, cmd); err != nil {
		t.Fatalf("1ª chamada: erro inesperado: %v", err)
	}
	if err := PerformIncrement(ctx, cmd); err != nil {
		t.Fatalf("2ª chamada (mesma chave/comando): erro inesperado: %v", err)
	}

	events, err := store.Load(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento (2ª chamada deveria ter sido cacheada, sem reexecutar), got %d", len(events))
	}
}

func TestIdempotencyBusinessErrorCachedEvenAfterEnvironmentChanges(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow = runtime.NewUnitOfWork(store)
	idem = runtime.NewMemoryIdempotencyStore()

	unauth := runtime.WithCaller(context.Background(), idemStubCaller{authenticated: false})
	keyCtx := runtime.WithIdempotencyKey(unauth, "biz-key")
	cmd := IncrementTally{TallyId: TallyId("t2"), Amount: Amount(1)}

	err := PerformIncrement(keyCtx, cmd)
	if !errors.Is(err, runtime.ErrForbidden) {
		t.Fatalf("1ª chamada (caller não autenticado): esperava ErrForbidden, got %v", err)
	}

	// Ambiente muda: MESMA chave, agora com um caller AUTENTICADO — se o
	// corpo rodasse de novo, teria sucesso. Cacheado de verdade continua
	// devolvendo o ErrForbidden da 1ª vez.
	authCtx := runtime.WithIdempotencyKey(runtime.WithCaller(context.Background(), idemStubCaller{authenticated: true}), "biz-key")
	err = PerformIncrement(authCtx, cmd)
	if !errors.Is(err, runtime.ErrForbidden) {
		t.Fatalf("2ª chamada (mesma chave, caller agora autenticado): esperava ErrForbidden CACHEADO, got %v", err)
	}
	events, _ := store.Load(context.Background(), "t2")
	if len(events) != 0 {
		t.Fatalf("esperava 0 eventos (o corpo nunca deveria ter rodado de verdade), got %d", len(events))
	}

	// Chave NOVA, caller autenticado: roda de verdade e sucede — prova que
	// o cache era por CHAVE, não um bloqueio permanente.
	freshCtx := runtime.WithIdempotencyKey(runtime.WithCaller(context.Background(), idemStubCaller{authenticated: true}), "biz-key-fresh")
	if err := PerformIncrement(freshCtx, cmd); err != nil {
		t.Fatalf("chamada com chave nova: erro inesperado: %v", err)
	}
	events, _ = store.Load(context.Background(), "t2")
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento (chave nova rodou de verdade), got %d", len(events))
	}
}

func TestIdempotencyInfraErrorNotCachedRetryReruns(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	flaky := &flakyStore{inner: store}
	uow = runtime.NewUnitOfWork(flaky)
	idem = runtime.NewMemoryIdempotencyStore()

	ctx := runtime.WithCaller(context.Background(), idemStubCaller{authenticated: true})
	keyCtx := runtime.WithIdempotencyKey(ctx, "infra-key")
	cmd := IncrementTally{TallyId: TallyId("t3"), Amount: Amount(2)}

	flaky.failNext = true
	err := PerformIncrement(keyCtx, cmd)
	if err == nil {
		t.Fatal("1ª chamada: esperava erro de infra (Append simulado falhando), veio nil")
	}
	var be runtime.BusinessError
	if errors.As(err, &be) {
		t.Fatalf("1ª chamada: esperava erro de INFRA (não BusinessError), got %v", err)
	}

	if err := PerformIncrement(keyCtx, cmd); err != nil {
		t.Fatalf("2ª chamada (retry pós erro de infra): erro inesperado: %v", err)
	}

	events, _ := store.Load(context.Background(), "t3")
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento (retry reexecutou de verdade após o erro de infra não cacheado), got %d", len(events))
	}
}

func TestIdempotencyConflictOnDifferentPayload(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow = runtime.NewUnitOfWork(store)
	idem = runtime.NewMemoryIdempotencyStore()

	ctx := runtime.WithCaller(context.Background(), idemStubCaller{authenticated: true})
	keyCtx := runtime.WithIdempotencyKey(ctx, "conflict-key")

	cmd1 := IncrementTally{TallyId: TallyId("t4"), Amount: Amount(1)}
	if err := PerformIncrement(keyCtx, cmd1); err != nil {
		t.Fatalf("1ª chamada: erro inesperado: %v", err)
	}

	cmd2 := IncrementTally{TallyId: TallyId("t4"), Amount: Amount(2)}
	err := PerformIncrement(keyCtx, cmd2)
	if !errors.Is(err, runtime.ErrIdempotencyKeyConflict) {
		t.Fatalf("2ª chamada (payload diferente, mesma chave): esperava ErrIdempotencyKeyConflict, got %v", err)
	}

	events, _ := store.Load(context.Background(), "t4")
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento (o conflito não deveria ter rodado o corpo), got %d", len(events))
	}
}

func TestIdempotencyConcurrentWaitBlocksThenReturnsSameCachedResult(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	gated := &gatedStore{inner: store, entered: make(chan struct{}), proceed: make(chan struct{})}
	uow = runtime.NewUnitOfWork(gated)
	idem = runtime.NewMemoryIdempotencyStore()

	ctx := runtime.WithCaller(context.Background(), idemStubCaller{authenticated: true})
	key := "wait-key"
	cmd := IncrementTally{TallyId: TallyId("t5"), Amount: Amount(5)}

	resA := make(chan error, 1)
	go func() {
		resA <- PerformIncrement(runtime.WithIdempotencyKey(ctx, key), cmd)
	}()

	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine A não chegou no Append a tempo")
	}

	resB := make(chan error, 1)
	go func() {
		resB <- PerformIncrement(runtime.WithIdempotencyKey(ctx, key), cmd)
	}()

	// dá tempo da goroutine B chamar idem.Begin e observar BeginInFlight
	// antes de liberar A.
	time.Sleep(50 * time.Millisecond)
	close(gated.proceed)

	var errA, errB error
	select {
	case errA = <-resA:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine A não terminou")
	}
	select {
	case errB = <-resB:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine B não terminou (wait não desbloqueou)")
	}
	if errA != nil || errB != nil {
		t.Fatalf("esperava as duas chamadas sem erro, got A=%v B=%v", errA, errB)
	}

	events, err := store.Load(context.Background(), "t5")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento (B deveria ter esperado e recebido o resultado cacheado, não reexecutado), got %d", len(events))
	}
}
`

func TestIdempotencyWaitBehavior(t *testing.T) {
	files := idemSmokeFiles(t, "tally", idemFixtureModDsWait)
	files[filepath.Join("tally", "idempotency_behavior_test.go")] = []byte(idemWaitBehaviorTest)
	runGeneratedTests(t, files)
}

// idemRejectBehaviorTest prova o outro lado de concurrentRetry (spec §14):
// sob "reject", a 2ª chamada concorrente falha IMEDIATAMENTE com
// ErrIdempotencyInFlight, sem esperar a 1ª terminar.
const idemRejectBehaviorTest = `package tallyreject

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"domainscript/generated/runtime"
)

type idemStubCaller struct{ authenticated bool }

func (c idemStubCaller) Authenticated() bool { return c.authenticated }
func (c idemStubCaller) ID() string          { return "tester" }
func (c idemStubCaller) HasRole(string) bool  { return false }

type gatedStore struct {
	inner   runtime.EventStore
	entered chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (s *gatedStore) Append(ctx context.Context, aggregateID string, events []runtime.Event) error {
	s.once.Do(func() {
		close(s.entered)
		<-s.proceed
	})
	return s.inner.Append(ctx, aggregateID, events)
}

func (s *gatedStore) Load(ctx context.Context, aggregateID string) ([]runtime.Event, error) {
	return s.inner.Load(ctx, aggregateID)
}

func TestIdempotencyConcurrentRejectFailsFast(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	gated := &gatedStore{inner: store, entered: make(chan struct{}), proceed: make(chan struct{})}
	uow = runtime.NewUnitOfWork(gated)
	idem = runtime.NewMemoryIdempotencyStore()

	ctx := runtime.WithCaller(context.Background(), idemStubCaller{authenticated: true})
	key := "reject-key"
	cmd := IncrementTally{TallyId: TallyId("t1"), Amount: Amount(1)}

	resA := make(chan error, 1)
	go func() {
		resA <- PerformIncrement(runtime.WithIdempotencyKey(ctx, key), cmd)
	}()

	select {
	case <-gated.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine A não chegou no Append a tempo")
	}

	start := time.Now()
	errB := PerformIncrement(runtime.WithIdempotencyKey(ctx, key), cmd)
	elapsed := time.Since(start)
	if !errors.Is(errB, runtime.ErrIdempotencyInFlight) {
		t.Fatalf("esperava ErrIdempotencyInFlight (reject), got %v", errB)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("reject deveria falhar rápido, sem esperar A; levou %v", elapsed)
	}

	close(gated.proceed)
	if err := <-resA; err != nil {
		t.Fatalf("goroutine A: erro inesperado: %v", err)
	}

	events, err := store.Load(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento (A completou; B foi rejeitada sem rodar o corpo), got %d", len(events))
	}
}
`

func TestIdempotencyRejectBehavior(t *testing.T) {
	files := idemSmokeFiles(t, "tallyreject", idemFixtureModDsReject)
	files[filepath.Join("tallyreject", "idempotency_behavior_test.go")] = []byte(idemRejectBehaviorTest)
	runGeneratedTests(t, files)
}

// --- Saga async: mapeamento estável Idempotency-Key -> sagaId (F3 + G2) ----

// idemSagaFixtureSrc é uma Saga async mínima (1 passo trivial) — só para
// exercitar a entrada (GenerateReport) e o SagaStore (GenerateReportStatus);
// a orquestração de passos/compensação em si já está coberta por F3
// (decl_saga_test.go).
const idemSagaFixtureSrc = `
ValueObject ReportId(string) {
    Valid { value.length() > 0 }
}

Command GenerateReportCmd {
    reportId ReportId
}

Saga GenerateReport handles GenerateReportCmd {
    mode async
    state { reportId ReportId }

    step Build {
        up {
            return
        }
    }
}
`

const idemSagaFixtureModDs = `Module Reports { }
`

// parseIdemSagaFixture monta e resolve a fixture da Saga async.
func parseIdemSagaFixture(t *testing.T) (*program.Program, *ast.SagaDecl) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    idemSagaFixtureModDs,
		"domain.ds": idemSagaFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de Saga async (G2, mapeamento sagaId) tem diagnósticos de erro:\n%s", bag.Render())
	}
	saga := findSagaDecl(t, prog, "GenerateReport")
	return prog, saga
}

// emitIdemSaga gera o Go da Saga GenerateReport.
func emitIdemSaga(t *testing.T) []byte {
	t.Helper()
	prog, saga := parseIdemSagaFixture(t)
	model := types.NewModel(prog.Symbols)
	reg := walletVOOperatorRegistryFromProgram(prog)

	got, err := codegen.EmitSaga("reports", saga, model, prog.Symbols, "Reports", reg, nil, nil)
	if err != nil {
		t.Fatalf("EmitSaga(GenerateReport): erro inesperado: %v", err)
	}
	return got
}

// TestEmitSagaAsyncIdempotencyKeyMappingGolden prova, sobre o Go de fato
// gerado, que a entrada async deriva o sagaId da Idempotency-Key (quando
// presente) e consulta o SagaStore ANTES de iniciar um novo run — o
// mapeamento estável exigido por G2 (spec §14: "Idempotency-Key mapeia para
// sagaId de forma estável").
func TestEmitSagaAsyncIdempotencyKeyMappingGolden(t *testing.T) {
	got := emitIdemSaga(t)
	s := string(got)
	for _, want := range []string{
		"sagaID := runtime.UUID()",
		"if key, ok := runtime.IdempotencyKeyFrom(ctx); ok {",
		"sagaID = runtime.SagaIDFromIdempotencyKey(key)",
		"if _, found, _ := generateReportSagaStore.Get(ctx, sagaID); found {",
		"return sagaID",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("esperava %q no Go gerado da Saga async, não achei:\n%s", want, s)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "saga_generate_report_async.go.golden"), got)
}

// TestEmitSagaAsyncIdempotencyKeyMappingDeterministic prova NFR-13.
func TestEmitSagaAsyncIdempotencyKeyMappingDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitIdemSaga(t)
	})
}

// idemSagaSmokeFiles monta o projeto isolado da fixture da Saga async.
func idemSagaSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog, _ := parseIdemSagaFixture(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}
	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	reportID := findValueObjectDecl(t, prog, "ReportId")
	voGo, err := codegen.EmitValueObject("reports", reportID)
	if err != nil {
		t.Fatalf("EmitValueObject(ReportId): erro inesperado: %v", err)
	}
	files[filepath.Join("reports", "report_id.go")] = voGo

	cmdDecl := findCommandDecl(t, prog, "GenerateReportCmd")
	cmdGo, err := codegen.EmitCommand("reports", cmdDecl, types.NewModel(prog.Symbols), prog.Symbols, "Reports")
	if err != nil {
		t.Fatalf("EmitCommand: erro inesperado: %v", err)
	}
	files[filepath.Join("reports", "commands.go")] = cmdGo

	files[filepath.Join("reports", "sagas.go")] = emitIdemSaga(t)
	return files
}

func TestEmitSagaAsyncIdempotencyKeyMappingSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, idemSagaSmokeFiles(t))
}

// idemSagaBehaviorTest prova, sobre o Go de fato gerado: a MESMA
// Idempotency-Key sempre devolve o MESMO sagaId (e o status fica
// consultável); chaves diferentes devolvem sagaIds diferentes; SEM nenhuma
// chave, cada chamada continua gerando um sagaId aleatório novo (F3
// inalterado quando idempotência não está em jogo).
const idemSagaBehaviorTest = `package reports

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func TestGenerateReportSameIdempotencyKeyReturnsSameSagaID(t *testing.T) {
	ctx := context.Background()
	key := "report-key-1"
	cmd := GenerateReportCmd{ReportId: ReportId("r1")}

	id1 := GenerateReport(runtime.WithIdempotencyKey(ctx, key), cmd)
	id2 := GenerateReport(runtime.WithIdempotencyKey(ctx, key), cmd)
	if id1 != id2 {
		t.Fatalf("esperava o MESMO sagaId para a mesma Idempotency-Key: %q != %q", id1, id2)
	}

	if _, found := GenerateReportStatus(id1); !found {
		t.Fatal("esperava um status registrado para o sagaId")
	}

	id3 := GenerateReport(runtime.WithIdempotencyKey(ctx, "report-key-2"), cmd)
	if id3 == id1 {
		t.Fatalf("chaves diferentes deveriam produzir sagaIds diferentes: %q", id1)
	}
}

func TestGenerateReportWithoutIdempotencyKeyUsesRandomSagaID(t *testing.T) {
	ctx := context.Background()
	cmd := GenerateReportCmd{ReportId: ReportId("r2")}

	id1 := GenerateReport(ctx, cmd)
	id2 := GenerateReport(ctx, cmd)
	if id1 == id2 {
		t.Fatalf("sem Idempotency-Key, cada chamada deveria gerar um sagaId aleatório novo — got o mesmo %q duas vezes", id1)
	}
}
`

func TestEmitSagaAsyncIdempotencyKeyMappingBehavior(t *testing.T) {
	files := idemSagaSmokeFiles(t)
	files[filepath.Join("reports", "sagas_behavior_test.go")] = []byte(idemSagaBehaviorTest)
	runGeneratedTests(t, files)
}
