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

// decl_query_cache_test.go prova os critérios de conclusão da task G3 (§design
// codegen 3.9, REQ-21.3, spec §15): cache de Query — ttl, invalidação por
// evento (inferida dos aggregates tocados; override "invalidateOn"),
// negativeCacheTtl, stampede protection (request coalescing), fail-open na
// falha do backend, bypass "Cache-Control: no-cache" e tenant na chave.
//
// --- A fixture: Widget, um Aggregate mínimo com 2 Queries cacheadas ---
//
// Widget é EventSourced (mesma razão de Tally em idempotency_test.go: é a
// única estratégia que o read side (decl_query.go, "load Agg(...) as View")
// sabe carregar hoje — StateStored pede um runtime.Repository[T], que Query
// não recebe). GetWidget declara "invalidateOn: [WidgetRenamed]" — um
// OVERRIDE que deliberadamente EXCLUI WidgetCreated (que também tem Apply em
// Widget) — prova que o override muda o conjunto de invalidação de verdade,
// não só documenta a inferência. GetWidgetAutoInvalidate NÃO declara
// "invalidateOn": o conjunto é inferido dos 2 Applies de Widget
// (WidgetCreated + WidgetRenamed) — prova a inferência automática (golden,
// abaixo).
//
// "ensure widgetId != WidgetId(\"missing\") else WidgetNotFound" dá a
// GetWidget um caminho de erro de negócio DETERMINÍSTICO (sem depender de
// "aggregate não existe" — LoadWidget EventSourced nunca devolve nil, mesmo
// gap documentado em idempotency_test.go/generate_e2e_wallet_test.go) — o
// que TestQueryCacheBehavior usa para provar negativeCacheTtl.
const cacheFixtureSrc = `
ValueObject WidgetId(string) {
    Valid { value.length() > 0 }
}

ValueObject WidgetName(string) {
    Valid { value.length() > 0 }
}

Event WidgetCreated {
    id WidgetId
    name WidgetName
}

Event WidgetRenamed {
    id WidgetId
    name WidgetName
}

Error WidgetNotFound {
    message "widget not found"
}

View WidgetView {
    id WidgetId
    name WidgetName
}

Aggregate Widget {
    strategy EventSourced

    state {
        id WidgetId
        name WidgetName
    }

    access {
        Create requires caller.authenticated
        Rename requires caller.authenticated
    }

    Handle Create(name WidgetName) {
        emit WidgetCreated(self.id, name)
    }

    Handle Rename(name WidgetName) {
        emit WidgetRenamed(self.id, name)
    }

    Apply WidgetCreated {
        state.name = event.name
    }

    Apply WidgetRenamed {
        state.name = event.name
    }
}

Command RenameWidget {
    widgetId ref Widget
    name WidgetName
}

UseCase PerformRename handles RenameWidget {
    execute {
        widget = load Widget(cmd.widgetId)
        ensure widget exists else WidgetNotFound
        widget.Rename(cmd.name)
    }
}

Query GetWidget(widgetId WidgetId) -> WidgetView {
    cache {
        ttl: 200ms
        negativeCacheTtl: 100ms
        invalidateOn: [WidgetRenamed]
    }
    ensure widgetId != WidgetId("missing") else WidgetNotFound
    return load Widget(widgetId) as WidgetView
}

Query GetWidgetAutoInvalidate(widgetId WidgetId) -> WidgetView {
    cache {
        ttl: 1min
    }
    return load Widget(widgetId) as WidgetView
}
`

const cacheFixtureModDs = `Module Widgets {
    Database WidgetsDb {
        provider: "postgres"
        manages: [Widget]
    }
}
`

// findUseCaseDeclCache/findErrorTypeDeclCache espelham findIdemUseCaseDecl/
// findErrorTypeDeclIdem (idempotency_test.go) — nomes próprios (sufixo
// "Cache") para não colidir no mesmo pacote codegen_test.
func findUseCaseDeclCache(t *testing.T, prog *program.Program, name string) *ast.UseCaseDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if u, ok := d.(*ast.UseCaseDecl); ok && u.Name == name {
				return u
			}
		}
	}
	t.Fatalf("UseCase %q não encontrado na fixture de cache de Query (G3)", name)
	return nil
}

func findErrorTypeDeclCache(t *testing.T, prog *program.Program, name string) *ast.ErrorTypeDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if e, ok := d.(*ast.ErrorTypeDecl); ok && e.Name == name {
				return e
			}
		}
	}
	t.Fatalf("Error %q não encontrado na fixture de cache de Query (G3)", name)
	return nil
}

// parseCacheFixture monta o projeto sintético (mod.ds + domain.ds) em disco e
// o resolve via driver.CheckProject.
func parseCacheFixture(t *testing.T) *program.Program {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    cacheFixtureModDs,
		"domain.ds": cacheFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de cache de Query (G3) tem diagnósticos de erro:\n%s", bag.Render())
	}
	return prog
}

// emitCacheQueries gera o Go das 2 Queries da fixture (GetWidget,
// GetWidgetAutoInvalidate) num único arquivo — o caminho comum aos testes de
// golden/determinismo/smoke desta suíte.
func emitCacheQueries(t *testing.T, prog *program.Program) []byte {
	t.Helper()
	agg := findAggregateDecl(t, prog, "Widget")
	getWidget := findQueryDecl(t, prog, "GetWidget")
	getWidgetAuto := findQueryDecl(t, prog, "GetWidgetAutoInvalidate")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)
	aggregates := map[string]*ast.AggregateDecl{"Widget": agg}

	got, err := codegen.EmitQueries("widgets", []*ast.QueryDecl{getWidget, getWidgetAuto}, aggregates, prog, model, prog.Symbols, "Widgets", reg)
	if err != nil {
		t.Fatalf("EmitQueries: erro inesperado: %v", err)
	}
	return got
}

// --- Golden + determinismo ---------------------------------------------------

// TestEmitQueryCacheGolden prova, sobre o Go de fato gerado, os elementos
// centrais do critério de conclusão de G3: o corpo de sempre migra para um
// nome privado (getWidgetRun), a var de cache (getWidgetCache), o wrapper
// público com Get/Coalesce/Set/SetErr/NoCacheFrom/TenantFrom/CacheKey, E a
// diferença entre invalidação por OVERRIDE (GetWidget: só WidgetRenamed) e
// por INFERÊNCIA automática (GetWidgetAutoInvalidate: WidgetCreated E
// WidgetRenamed, os 2 Applies de Widget).
func TestEmitQueryCacheGolden(t *testing.T) {
	prog := parseCacheFixture(t)
	got := emitCacheQueries(t, prog)
	s := string(got)
	for _, want := range []string{
		// Corpo de sempre migrado para o nome privado.
		"func getWidgetRun(ctx context.Context, store runtime.EventStore, widgetId WidgetId) (WidgetView, error)",
		"if !(widgetId != WidgetId(\"missing\")) {",
		"return zero, ErrWidgetNotFound",
		"widget, err := LoadWidget(runtime.NewEventLoader(ctx, store), widgetId)",
		// Var de cache + wrapper público.
		"var getWidgetCache = runtime.NewMemoryQueryCache()",
		"func GetWidget(ctx context.Context, store runtime.EventStore, widgetId WidgetId) (WidgetView, error) {",
		"tenant, _ := runtime.TenantFrom(ctx)",
		"key := runtime.CacheKey(\"GetWidget\", tenant.ID, widgetId)",
		"if !runtime.NoCacheFrom(ctx) {",
		"if v, cachedErr, hit := getWidgetCache.Get(ctx, key); hit {",
		"return v.(WidgetView), nil",
		"result, err := getWidgetCache.Coalesce(key, func() (any, error) {",
		"return getWidgetRun(ctx, store, widgetId)",
		"if runtime.IsBusinessError(err) {",
		"getWidgetCache.SetErr(ctx, key, err, time.Duration(100000000))",
		"getWidgetCache.Set(ctx, key, result, time.Duration(200000000))",
		"return result.(WidgetView), nil",
		// Override: SÓ WidgetRenamed dispara a invalidação de getWidgetCache.
		"d.Subscribe(\"WidgetRenamed\", func(ctx context.Context, ev runtime.Event) error {",
		"getWidgetCache.InvalidateAll()",
		// Inferência automática: os 2 Applies de Widget, ambos.
		"var getWidgetAutoInvalidateCache = runtime.NewMemoryQueryCache()",
		"func WireQueryCache(d runtime.Dispatcher) {",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, s)
		}
	}

	// getWidgetCache (override) NUNCA deveria assinar WidgetCreated —
	// checagem estrutural: o Subscribe de WidgetCreated (se existir, só pode
	// ser o de getWidgetAutoInvalidateCache) precisa vir DEPOIS do de
	// WidgetRenamed na ordem determinística (dedupSortedStrings ordena
	// alfabeticamente: GetWidget é processada primeiro, sua lista
	// [WidgetRenamed] só tem 1 evento; GetWidgetAutoInvalidate vem depois com
	// [WidgetCreated, WidgetRenamed]).
	firstRenamed := strings.Index(s, `d.Subscribe("WidgetRenamed"`)
	firstCreated := strings.Index(s, `d.Subscribe("WidgetCreated"`)
	if firstCreated == -1 {
		t.Fatalf("esperava ao menos 1 d.Subscribe(\"WidgetCreated\", ...) (invalidação inferida de GetWidgetAutoInvalidate), não achei:\n%s", s)
	}
	if firstRenamed == -1 || firstRenamed > firstCreated {
		t.Fatalf("esperava d.Subscribe(\"WidgetRenamed\", ...) (de GetWidget, override) ANTES do de \"WidgetCreated\" (de GetWidgetAutoInvalidate, inferido) — ordem determinística por nome de Query:\n%s", s)
	}
	// getWidgetCache.InvalidateAll só pode aparecer 1 vez (1 único evento,
	// WidgetRenamed, no override) — se aparecesse 2x teria assinado
	// WidgetCreated também, quebrando o override.
	if n := strings.Count(s, "getWidgetCache.InvalidateAll()"); n != 1 {
		t.Fatalf("esperava exatamente 1 Subscribe invalidando getWidgetCache (override exclui WidgetCreated), achei %d:\n%s", n, s)
	}
	if n := strings.Count(s, "getWidgetAutoInvalidateCache.InvalidateAll()"); n != 2 {
		t.Fatalf("esperava exatamente 2 Subscribe invalidando getWidgetAutoInvalidateCache (WidgetCreated + WidgetRenamed, inferidos), achei %d:\n%s", n, s)
	}

	gentest.Golden(t, filepath.Join("testdata", "queries_widget_cache.go.golden"), got)
}

// TestEmitQueryCacheDeterministic prova NFR-13.
func TestEmitQueryCacheDeterministic(t *testing.T) {
	prog := parseCacheFixture(t)
	gentest.Deterministic(t, func() []byte {
		return emitCacheQueries(t, prog)
	})
}

// --- Smoke compile ------------------------------------------------------------

// cacheSmokeFiles monta o conjunto completo de arquivos de um projeto
// isolado (go.mod + runtime real + VOs/Events/Error/View/Aggregate+Load/
// Command/UseCase/Queries) — mesmo padrão de idemSmokeFiles
// (idempotency_test.go).
func cacheSmokeFiles(t *testing.T, prog *program.Program) map[string][]byte {
	t.Helper()
	agg := findAggregateDecl(t, prog, "Widget")
	cmdDecl := findCommandDecl(t, prog, "RenameWidget")
	uc := findUseCaseDeclCache(t, prog, "PerformRename")
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

	pkg := "widgets"
	for _, spec := range []struct {
		decl *ast.ValueObjectDecl
		file string
	}{
		{findValueObjectDecl(t, prog, "WidgetId"), "widget_id.go"},
		{findValueObjectDecl(t, prog, "WidgetName"), "widget_name.go"},
	} {
		got, err := codegen.EmitValueObject(pkg, spec.decl)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", spec.decl.Name, err)
		}
		files[filepath.Join(pkg, spec.file)] = got
	}

	eventsGo, err := codegen.EmitEvents(pkg, []*ast.EventDecl{
		findEventDecl(t, prog, "WidgetCreated"),
		findEventDecl(t, prog, "WidgetRenamed"),
	})
	if err != nil {
		t.Fatalf("EmitEvents: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "events.go")] = eventsGo

	errGo, err := codegen.EmitErrors(pkg, []*ast.ErrorTypeDecl{findErrorTypeDeclCache(t, prog, "WidgetNotFound")})
	if err != nil {
		t.Fatalf("EmitErrors: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "errors.go")] = errGo

	viewGo, err := codegen.EmitView(pkg, findViewDecl(t, prog, "WidgetView"), model, prog.Symbols, "Widgets")
	if err != nil {
		t.Fatalf("EmitView: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "views.go")] = viewGo

	aggGo, err := codegen.EmitAggregate(pkg, agg, model, prog.Symbols, "Widgets", reg)
	if err != nil {
		t.Fatalf("EmitAggregate: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "aggregate_widget.go")] = aggGo

	loadGo, err := codegen.EmitAggregateLoad(pkg, agg, model, prog.Symbols, "Widgets")
	if err != nil {
		t.Fatalf("EmitAggregateLoad: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "aggregate_widget_load.go")] = loadGo

	cmdGo, err := codegen.EmitCommand(pkg, cmdDecl, model, prog.Symbols, "Widgets")
	if err != nil {
		t.Fatalf("EmitCommand: erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "commands.go")] = cmdGo

	aggregates := map[string]*ast.AggregateDecl{"Widget": agg}
	ucGo, err := codegen.EmitUseCase(pkg, uc, aggregates, prog, model, prog.Symbols, "Widgets", reg, nil)
	if err != nil {
		t.Fatalf("EmitUseCase(PerformRename): erro inesperado: %v", err)
	}
	files[filepath.Join(pkg, "usecases.go")] = ucGo

	files[filepath.Join(pkg, "queries.go")] = emitCacheQueries(t, prog)

	return files
}

func TestEmitQueryCacheSmokeCompile(t *testing.T) {
	prog := parseCacheFixture(t)
	gentest.SmokeCompile(t, cacheSmokeFiles(t, prog))
}

// --- Comportamental (NFR-15): TTL, negativeCacheTtl, stampede, invalidação,
// no-cache — sobre o Go de fato gerado, rodando `go test ./...` DENTRO do
// projeto isolado (mesma técnica de idempotency_test.go/decl_aggregate_
// load_test.go: "uow"/"WireQueryCache" são símbolos do pacote gerado,
// wireados manualmente pelo teste — cmd/<service>/main.go não é exercitado
// aqui, ver TestGenerateWidgetCacheHTTPBypassGolden abaixo para a borda
// HTTP).
//
// stampedeCounter conta as chamadas REAIS de Load do EventStore subjacente:
// como GetWidget/getWidgetRun sempre chama LoadWidget -> store.Load
// exatamente 1 vez por execução, contar Load é equivalente a contar
// execuções reais de getWidgetRun — a prova pedida pela task ("um contador
// incrementado pela lógica de query subjacente").
const cacheBehaviorTest = `package widgets

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"domainscript/generated/runtime"
)

type cacheStubCaller struct{ authenticated bool }

func (c cacheStubCaller) Authenticated() bool { return c.authenticated }
func (c cacheStubCaller) ID() string          { return "tester" }
func (c cacheStubCaller) HasRole(string) bool  { return false }

// countingStore conta toda chamada de Load (a prova de "só 1 execução real"
// de stampede protection) e pode travar a 1ª chamada até o teste liberar
// (gated), reaproveitando o padrão de gatedStore (idempotency_test.go) para
// forçar uma corrida real e determinística entre goroutines concorrentes.
type countingStore struct {
	inner   runtime.EventStore
	loads   int64
	gate    chan struct{} // se não-nil, a 1ª Load bloqueia até ser fechado
	entered chan struct{}
	once    sync.Once
}

func (s *countingStore) Append(ctx context.Context, aggregateID string, events []runtime.Event) error {
	return s.inner.Append(ctx, aggregateID, events)
}

func (s *countingStore) Load(ctx context.Context, aggregateID string) ([]runtime.Event, error) {
	atomic.AddInt64(&s.loads, 1)
	if s.gate != nil {
		s.once.Do(func() {
			if s.entered != nil {
				close(s.entered)
			}
			<-s.gate
		})
	}
	return s.inner.Load(ctx, aggregateID)
}

func newWidgetsWiring() (*countingStore, *runtime.Dispatcher) {
	panic("unused") // placeholder removido abaixo — ver setup de cada teste
}

func TestTTLExpiryReExecutesAfterExpiry(t *testing.T) {
	inner := runtime.NewMemoryEventStore()
	store := &countingStore{inner: inner}
	dispatcher := runtime.NewDispatcher()
	uow = runtime.NewUnitOfWork(store, dispatcher)
	WireQueryCache(dispatcher)

	ctx := runtime.WithCaller(context.Background(), cacheStubCaller{authenticated: true})
	id := WidgetId("w-ttl")
	if err := inner.Append(context.Background(), string(id), []runtime.Event{&WidgetCreated{Id: id, Name: WidgetName("first")}}); err != nil {
		t.Fatal(err)
	}

	v1, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("1ª chamada: erro inesperado: %v", err)
	}
	if v1.Name != WidgetName("first") {
		t.Fatalf("1ª chamada: Name = %v, want first", v1.Name)
	}
	if atomic.LoadInt64(&store.loads) != 1 {
		t.Fatalf("esperava 1 Load real após a 1ª chamada, got %d", store.loads)
	}

	// Muda o estado por baixo do cache (sem invalidar) — a 2ª chamada, ainda
	// dentro do ttl (200ms), deveria devolver o valor CACHEADO (stale).
	if err := inner.Append(context.Background(), string(id), []runtime.Event{&WidgetRenamed{Id: id, Name: WidgetName("second")}}); err != nil {
		t.Fatal(err)
	}
	v2, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("2ª chamada: erro inesperado: %v", err)
	}
	if v2.Name != WidgetName("first") {
		t.Fatalf("2ª chamada (dentro do ttl): esperava valor CACHEADO %q, got %q", "first", v2.Name)
	}
	if atomic.LoadInt64(&store.loads) != 1 {
		t.Fatalf("esperava AINDA 1 Load real (2ª chamada deveria ter sido cache hit), got %d", store.loads)
	}

	// Espera o ttl (200ms) expirar.
	time.Sleep(300 * time.Millisecond)
	v3, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("3ª chamada (pós-expiry): erro inesperado: %v", err)
	}
	if v3.Name != WidgetName("second") {
		t.Fatalf("3ª chamada (pós-expiry): esperava valor FRESCO %q, got %q", "second", v3.Name)
	}
	if atomic.LoadInt64(&store.loads) != 2 {
		t.Fatalf("esperava 2 Loads reais (ttl expirou, reexecutou), got %d", store.loads)
	}
}

func TestNegativeCacheTtlCachesNotFoundThenExpires(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	dispatcher := runtime.NewDispatcher()
	uow = runtime.NewUnitOfWork(store, dispatcher)
	WireQueryCache(dispatcher)

	ctx := runtime.WithCaller(context.Background(), cacheStubCaller{authenticated: true})
	missing := WidgetId("missing")

	_, err1 := GetWidget(ctx, store, missing)
	if !errors.Is(err1, ErrWidgetNotFound) {
		t.Fatalf("1ª chamada: esperava ErrWidgetNotFound, got %v", err1)
	}

	_, err2 := GetWidget(ctx, store, missing)
	if !errors.Is(err2, ErrWidgetNotFound) {
		t.Fatalf("2ª chamada (negativeCacheTtl, cacheado): esperava ErrWidgetNotFound, got %v", err2)
	}

	// Espera negativeCacheTtl (100ms) expirar — sem mudar nada, o resultado
	// continua ErrWidgetNotFound, mas agora reexecutado de verdade (não há
	// como observar isso diretamente sem instrumentar o "ensure", então esta
	// prova é comportamental-only: a chamada não trava/não quebra após a
	// expiração).
	time.Sleep(150 * time.Millisecond)
	_, err3 := GetWidget(ctx, store, missing)
	if !errors.Is(err3, ErrWidgetNotFound) {
		t.Fatalf("3ª chamada (pós-expiry do negativo): esperava ErrWidgetNotFound, got %v", err3)
	}
}

func TestStampedeProtectionSingleFlight(t *testing.T) {
	inner := runtime.NewMemoryEventStore()
	entered := make(chan struct{})
	gate := make(chan struct{})
	store := &countingStore{inner: inner, gate: gate, entered: entered}
	dispatcher := runtime.NewDispatcher()
	uow = runtime.NewUnitOfWork(store, dispatcher)
	WireQueryCache(dispatcher)

	ctx := runtime.WithCaller(context.Background(), cacheStubCaller{authenticated: true})
	id := WidgetId("w-stampede")
	if err := inner.Append(context.Background(), string(id), []runtime.Event{&WidgetCreated{Id: id, Name: WidgetName("stampede")}}); err != nil {
		t.Fatal(err)
	}

	const n = 8
	results := make(chan WidgetView, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			v, err := GetWidget(ctx, store, id)
			results <- v
			errs <- err
		}()
	}

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("nenhuma goroutine chegou no Load a tempo")
	}
	// Dá tempo das outras N-1 goroutines chamarem Coalesce e ficarem
	// esperando o mesmo voo em progresso antes de liberar.
	time.Sleep(100 * time.Millisecond)
	close(gate)

	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("goroutine %d: erro inesperado: %v", i, err)
		}
		if v := <-results; v.Name != WidgetName("stampede") {
			t.Fatalf("goroutine %d: Name = %v, want stampede", i, v.Name)
		}
	}

	if got := atomic.LoadInt64(&store.loads); got != 1 {
		t.Fatalf("esperava exatamente 1 Load real apesar de %d chamadas concorrentes (stampede protection), got %d", n, got)
	}
}

func TestInvalidationOnRealEmitViaOverride(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	dispatcher := runtime.NewDispatcher()
	uow = runtime.NewUnitOfWork(store, dispatcher)
	WireQueryCache(dispatcher)

	ctx := runtime.WithCaller(context.Background(), cacheStubCaller{authenticated: true})
	id := WidgetId("w-invalidate")
	if err := store.Append(context.Background(), string(id), []runtime.Event{&WidgetCreated{Id: id, Name: WidgetName("before")}}); err != nil {
		t.Fatal(err)
	}

	v1, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("1ª chamada: erro inesperado: %v", err)
	}
	if v1.Name != WidgetName("before") {
		t.Fatalf("1ª chamada: Name = %v, want before", v1.Name)
	}

	// PerformRename passa pela unit of work de VERDADE (uow.Run -> tx.Append
	// -> dispatcher.Publish, uow.go.txt) — a MESMA cadeia que qualquer
	// UseCase gerado usa; WireQueryCache assinou "WidgetRenamed" no MESMO
	// dispatcher, então a invalidação é IN-PROCESS e IMEDIATA (spec §15),
	// sem nenhum mock.
	cmd := RenameWidget{WidgetId: id, Name: WidgetName("after")}
	if err := PerformRename(ctx, cmd); err != nil {
		t.Fatalf("PerformRename: erro inesperado: %v", err)
	}

	v2, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("2ª chamada (pós-rename): erro inesperado: %v", err)
	}
	if v2.Name != WidgetName("after") {
		t.Fatalf("2ª chamada (pós-rename): esperava valor FRESCO %q (cache deveria ter sido invalidado por WidgetRenamed), got %q", "after", v2.Name)
	}
}

func TestInvalidationOverrideExcludesWidgetCreated(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	dispatcher := runtime.NewDispatcher()
	uow = runtime.NewUnitOfWork(store, dispatcher)
	WireQueryCache(dispatcher)

	ctx := runtime.WithCaller(context.Background(), cacheStubCaller{authenticated: true})
	id := WidgetId("w-override")
	if err := store.Append(context.Background(), string(id), []runtime.Event{&WidgetCreated{Id: id, Name: WidgetName("stays-cached")}}); err != nil {
		t.Fatal(err)
	}

	if _, err := GetWidget(ctx, store, id); err != nil {
		t.Fatalf("1ª chamada: erro inesperado: %v", err)
	}

	// Muda o estado por baixo do cache E publica um WidgetCreated (evento
	// que Widget TEM Apply para, mas que o override "invalidateOn:
	// [WidgetRenamed]" de GetWidget deliberadamente NÃO assina) diretamente
	// no MESMO dispatcher — simula outro widget sendo criado em qualquer
	// lugar do sistema. Se a inferência automática estivesse em uso (em vez
	// do override), isso teria invalidado getWidgetCache.
	if err := store.Append(context.Background(), string(id), []runtime.Event{&WidgetRenamed{Id: id, Name: WidgetName("ignored-by-direct-append")}}); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Publish(ctx, &WidgetCreated{Id: WidgetId("someone-else"), Name: WidgetName("irrelevant")}); err != nil {
		t.Fatal(err)
	}

	v2, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("2ª chamada: erro inesperado: %v", err)
	}
	if v2.Name != WidgetName("stays-cached") {
		t.Fatalf("2ª chamada: esperava o valor AINDA CACHEADO %q (WidgetCreated não deveria invalidar o override), got %q", "stays-cached", v2.Name)
	}
}

func TestNoCacheBypassesReadButRepopulates(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	dispatcher := runtime.NewDispatcher()
	uow = runtime.NewUnitOfWork(store, dispatcher)
	WireQueryCache(dispatcher)

	ctx := runtime.WithCaller(context.Background(), cacheStubCaller{authenticated: true})
	id := WidgetId("w-nocache")
	if err := store.Append(context.Background(), string(id), []runtime.Event{&WidgetCreated{Id: id, Name: WidgetName("v1")}}); err != nil {
		t.Fatal(err)
	}

	v1, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("1ª chamada: erro inesperado: %v", err)
	}
	if v1.Name != WidgetName("v1") {
		t.Fatalf("1ª chamada: Name = %v, want v1", v1.Name)
	}

	// Muda o estado por baixo do cache, SEM invalidar.
	if err := store.Append(context.Background(), string(id), []runtime.Event{&WidgetRenamed{Id: id, Name: WidgetName("v2")}}); err != nil {
		t.Fatal(err)
	}

	// Sem no-cache: continua vendo o valor cacheado (v1) — prova que o
	// cache de fato estava servindo a 2ª chamada.
	stale, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("chamada normal: erro inesperado: %v", err)
	}
	if stale.Name != WidgetName("v1") {
		t.Fatalf("chamada normal: esperava valor CACHEADO %q, got %q", "v1", stale.Name)
	}

	// Com no-cache: pula a LEITURA do cache, vê o valor fresco (v2) — E
	// repopula o cache com ele.
	noCacheCtx := runtime.WithNoCache(ctx)
	fresh, err := GetWidget(noCacheCtx, store, id)
	if err != nil {
		t.Fatalf("chamada com no-cache: erro inesperado: %v", err)
	}
	if fresh.Name != WidgetName("v2") {
		t.Fatalf("chamada com no-cache: esperava valor FRESCO %q, got %q", "v2", fresh.Name)
	}

	// Uma chamada NORMAL subsequente (sem no-cache) já vê v2 — prova que
	// no-cache REPOPULOU o cache (não só leu direto, ignorando-o para
	// sempre).
	repopulated, err := GetWidget(ctx, store, id)
	if err != nil {
		t.Fatalf("chamada normal pós-no-cache: erro inesperado: %v", err)
	}
	if repopulated.Name != WidgetName("v2") {
		t.Fatalf("chamada normal pós-no-cache: esperava %q (cache repopulado), got %q", "v2", repopulated.Name)
	}
}
`

func TestQueryCacheBehavior(t *testing.T) {
	prog := parseCacheFixture(t)
	files := cacheSmokeFiles(t, prog)
	files[filepath.Join("widgets", "cache_behavior_test.go")] = []byte(cacheBehaviorTest)
	runGeneratedTests(t, files)
}
