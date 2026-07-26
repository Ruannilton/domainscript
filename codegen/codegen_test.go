package codegen_test

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/diag"
	"domainscript/driver"
	"domainscript/token"
	"domainscript/types"
)

// codegen_test.go prova os critérios de conclusão da task E9.1 (§design
// codegen 3.1/3.4/3.11, REQ-14.5, REQ-26.4): o orquestrador Generate,
// rodando pela primeira vez sobre o Program REAL do wallet inteiro (não
// fixtures individuais por construto como as demais *_test.go deste
// pacote) — o primeiro smoke fim-a-fim de verdade do Marco E, juntando TODOS
// os emissores (E3-E8) numa pipeline de geração de projeto completo.

// walletGenerateOptions usa o MESMO module path que todo o resto do pacote
// codegen assume implicitamente via RuntimeImportPath ("domainscript/
// generated/runtime", fixo desde E3.1/decl_value.go — ver a doc de
// domainModuleRoot em codegen.go): para o projeto gerado de fato compilar
// (TestGenerateWalletSmokeCompile), ModulePath precisa bater com essa raiz —
// o mesmo module path que TODOS os outros smoke tests deste pacote já usam
// (smokeGoMod, decl_value_test.go).
var walletGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateWalletProject roda Generate sobre o Program real do wallet
// (testdata/projects/wallet, via driver.CheckProject — o pipeline completo que
// um usuário final rodaria, não uma fixture reconstruída à mão como as
// demais *_test.go deste pacote precisam fazer para contornar o bug de
// gramática documentado em usecase_repair.go).
func generateWalletProject(t *testing.T) []codegen.File {
	t.Helper()
	prog, bag := driver.CheckProject(walletProjectDir)
	if bag.HasErrors() {
		t.Fatalf("wallet não deveria ter diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre o wallet real: %v", err)
	}
	return files
}

// filesToMap converte []codegen.File (o formato de Generate) para o
// map[string][]byte que gentest.SmokeCompile espera.
func filesToMap(files []codegen.File) map[string][]byte {
	m := make(map[string][]byte, len(files))
	for _, f := range files {
		m[f.Path] = f.Content
	}
	return m
}

// TestGenerateWalletFileInventory confirma o critério de conclusão #1 da
// task: a lista de arquivos gerados inclui go.mod, runtime/*.go, wallet/*.go
// (várias categorias) e cmd/<algo>/main.go — e NÃO inclui contracts/* (o
// wallet não declara nenhum PublicEvent, os 3 Events são todos privados) nem
// wallet/projections.go (o wallet não declara Projection).
func TestGenerateWalletFileInventory(t *testing.T) {
	files := generateWalletProject(t)

	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
		if len(f.Content) == 0 {
			t.Errorf("arquivo %q gerado com conteúdo vazio", f.Path)
		}
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("Generate não devolveu os arquivos ordenados por Path (NFR-13):\n%v", paths)
	}

	has := func(p string) bool {
		for _, got := range paths {
			if got == p {
				return true
			}
		}
		return false
	}
	hasPrefix := func(prefix string) bool {
		for _, got := range paths {
			if strings.HasPrefix(got, prefix) {
				return true
			}
		}
		return false
	}

	for _, want := range []string{
		"go.mod",
		"runtime/eventstore.go",
		"runtime/uow.go",
		"runtime/decimal.go",
		"wallet/value_objects.go",
		"wallet/errors.go",
		"wallet/events.go",
		"wallet/aggregate_wallet.go",
		"wallet/aggregate_wallet_load.go",
		"wallet/commands.go",
		"wallet/usecases.go",
		"wallet/views.go",
		"wallet/queries.go",
		"wallet/wallet_test.go", // H4, *.test.ds (wallet.test.ds declara Test Wallet)
	} {
		if !has(want) {
			t.Errorf("esperava %q na lista de arquivos gerados, não achei:\n%v", want, paths)
		}
	}

	if !hasPrefix("cmd/") {
		t.Errorf("esperava ao menos um cmd/<service>/main.go, não achei:\n%v", paths)
	}
	if hasPrefix("contracts/") {
		t.Errorf("NÃO esperava contracts/* (wallet não declara PublicEvent), achei:\n%v", paths)
	}
	if has("wallet/projections.go") {
		t.Errorf("NÃO esperava wallet/projections.go (wallet não declara Projection)")
	}
}

// TestGenerateWalletMainWiresUseCases confirma que cmd/wallet/main.go (o
// wallet é um monólito implícito — sem topology.ds, prog.Services fica
// vazio, então "1 módulo sem Service" vira 1 único cmd/, nomeado pelo
// próprio módulo — ver buildCmdGroups/defaultCmdDirName em codegen.go) de
// fato chama wallet.Wire(uow): o wallet declara UseCases (PerformDeposit/
// PerformWithdrawal), então o grupo default deveria wirar o pacote wallet.
func TestGenerateWalletMainWiresUseCases(t *testing.T) {
	files := generateWalletProject(t)

	var main []byte
	for _, f := range files {
		if f.Path == "cmd/wallet/main.go" {
			main = f.Content
		}
	}
	if main == nil {
		t.Fatalf("esperava cmd/wallet/main.go (monólito implícito, 1 módulo Wallet sem Service)")
	}
	for _, want := range []string{
		"runtime.NewMemoryEventStore()",
		// Desde que o exemplo wallet passou a declarar `Cache { backend:
		// "redis" }` e a Query GetWallet ganhou bloco `cache` (§15), o módulo
		// tem uma Query cacheada — o que força needsDispatcher (G3: o
		// WireQueryCache assina o MESMO runtime.Dispatcher), e a unit of work
		// passa a receber o dispatcher como publisher. Antes disso o wallet não
		// tinha nenhum assinante e a forma era "NewUnitOfWork(store)".
		"dispatcher := runtime.NewDispatcher()",
		"runtime.NewUnitOfWork(store, dispatcher)",
		"wallet.Wire(uow)",
		"wallet.WireQueryCache(dispatcher)",
		`fmt.Sprintf(":%s", port)`,
	} {
		if !strings.Contains(string(main), want) {
			t.Errorf("esperava %q em cmd/wallet/main.go, não achei:\n%s", want, main)
		}
	}
}

// TestGenerateWalletSmokeCompile é o primeiro smoke fim-a-fim de verdade do
// Marco E: escreve TODOS os arquivos gerados (go.mod + runtime/ + wallet/ +
// cmd/) num diretório temporário e roda "go build ./..."/"go vet ./..."
// sobre o projeto INTEIRO — não mais um construto isolado como as demais
// tasks E3-E8 (que só provavam cada emissor com o resto do módulo montado à
// mão nos testes).
func TestGenerateWalletSmokeCompile(t *testing.T) {
	files := generateWalletProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// TestGenerateWalletDeterministic prova NFR-13 no nível do orquestrador:
// duas gerações do mesmo Program produzem exatamente os mesmos arquivos,
// byte a byte, na mesma ordem.
func TestGenerateWalletDeterministic(t *testing.T) {
	first := generateWalletProject(t)
	second := generateWalletProject(t)

	if len(first) != len(second) {
		t.Fatalf("número de arquivos difere entre gerações: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Fatalf("Path[%d] difere entre gerações: %q vs %q", i, first[i].Path, second[i].Path)
		}
		if string(first[i].Content) != string(second[i].Content) {
			t.Fatalf("conteúdo de %q difere entre gerações", first[i].Path)
		}
	}
}

// TestGenerateRefusesDiagnostics prova REQ-14.1 no nível do orquestrador (o
// mesmo espírito de TestGenerateProjectRefusesInvalidProject em
// driver/generate_test.go, agora direto sobre codegen.Generate): um bag com
// diagnóstico de erro recusa a geração inteira, sem produzir nenhum
// arquivo.
func TestGenerateRefusesDiagnostics(t *testing.T) {
	prog, bag := driver.CheckProject(walletProjectDir)
	if bag.HasErrors() {
		t.Fatalf("wallet não deveria ter diagnósticos de erro: %s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	dirty := diag.New()
	dirty.Errorf(token.Pos{Line: 1, Col: 1}, "erro sintético para forçar a recusa")

	files, err := codegen.Generate(prog, model, prog.Symbols, dirty, walletGenerateOptions)
	if !errors.Is(err, codegen.ErrHasDiagnostics) {
		t.Fatalf("err = %v, quero codegen.ErrHasDiagnostics", err)
	}
	if files != nil {
		t.Fatalf("esperava nil files quando o bag tem diagnósticos de erro, got %d arquivos", len(files))
	}
}

// --- TASK-M1.5: wiring de service com múltiplos produtores e Dispatcher ---
//
// (REQ-55.7/55.8, §design 4.3/5.2) — as duas fixtures abaixo (m15DualProducer.../
// m15Mixed...) provam a queda das antigas guardas F5 (>1 módulo produtor de
// canal) e F5/G3 (produtor + Dispatcher no mesmo service): o mecanismo NOVO
// (fan-out via Dispatcher, §design 4.3 itens 1/2) substitui as duas recusas
// pelo MESMO wiring — cada canal de saída não-durável assina o Dispatcher em
// vez de ser o publisher direto da UoW compartilhada. m15DurableSelfDispatch...
// prova a combinação que GENUINAMENTE resta não suportada (item 4 de §4.3,
// fora do escopo desta task): um módulo AO MESMO TEMPO produtor durável e
// dono de Dispatcher local.

// m15DualProducerTopologyDs: 2 módulos produtores (Fizz, Buzz) no MESMO
// service, cada um com seu PRÓPRIO canal "queue" in-memory (sem provider —
// não-durável) para um consumidor num service SEPARADO — a forma mais
// simples que exercita a guarda F5 antiga (>1 produtor), sem envolver
// Dispatcher por nenhum outro motivo (nem Policy nem Query cacheada no
// service DualProducerSvc): o Dispatcher só entra em jogo aqui por CONTENÇÃO
// entre os 2 produtores pelo único slot de Publisher (§design 4.3 item 1).
const m15DualProducerTopologyDs = `Topology {
    services {
        DualProducerSvc { modules: [Fizz, Buzz] }
        FizzSinkSvc { modules: [FizzSink] }
        BuzzSinkSvc { modules: [BuzzSink] }
    }
    channels {
        Fizz -> FizzSink {
            via: queue
            orderBy: id
        }
        Buzz -> BuzzSink {
            via: queue
            orderBy: id
        }
    }
}
`

const m15FizzModDs = `Module Fizz { }
`

const m15FizzDomainDs = `
ValueObject FizzId(string) {
    Valid { value.length() > 0 }
}

PublicEvent FizzMade { id FizzId }

Aggregate FizzThing {
    strategy EventSourced

    state {
        id FizzId
    }

    access {
        Make requires caller.authenticated
    }

    Handle Make() {
        emit FizzMade(self.id)
    }

    Apply FizzMade {
        state.id = event.id
    }
}

Command MakeFizz {
    id ref FizzThing
}

UseCase MakeFizzUseCase handles MakeFizz {
    execute {
        thing = load FizzThing(cmd.id)
        thing.Make()
    }
}
`

const m15BuzzModDs = `Module Buzz { }
`

const m15BuzzDomainDs = `
ValueObject BuzzId(string) {
    Valid { value.length() > 0 }
}

PublicEvent BuzzMade { id BuzzId }

Aggregate BuzzThing {
    strategy EventSourced

    state {
        id BuzzId
    }

    access {
        Make requires caller.authenticated
    }

    Handle Make() {
        emit BuzzMade(self.id)
    }

    Apply BuzzMade {
        state.id = event.id
    }
}

Command MakeBuzz {
    id ref BuzzThing
}

UseCase MakeBuzzUseCase handles MakeBuzz {
    execute {
        thing = load BuzzThing(cmd.id)
        thing.Make()
    }
}
`

const m15FizzSinkModDs = `Module FizzSink { }
`

const m15FizzSinkPolicyDs = `Policy ReactToFizz on FizzMade {
    delivery AtLeastOnce
    execute { return }
}
`

const m15BuzzSinkModDs = `Module BuzzSink { }
`

const m15BuzzSinkPolicyDs = `Policy ReactToBuzz on BuzzMade {
    delivery AtLeastOnce
    execute { return }
}
`

// generateM15DualProducerProject monta e gera o projeto da fixture de
// múltiplos produtores (TEST-1) — devolve os arquivos gerados.
func generateM15DualProducerProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":        m15DualProducerTopologyDs,
		"fizz/mod.ds":        m15FizzModDs,
		"fizz/domain.ds":     m15FizzDomainDs,
		"buzz/mod.ds":        m15BuzzModDs,
		"buzz/domain.ds":     m15BuzzDomainDs,
		"fizzsink/mod.ds":    m15FizzSinkModDs,
		"fizzsink/policy.ds": m15FizzSinkPolicyDs,
		"buzzsink/mod.ds":    m15BuzzSinkModDs,
		"buzzsink/policy.ds": m15BuzzSinkPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de múltiplos produtores (M1.5, TEST-1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, shopGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de múltiplos produtores: %v", err)
	}
	return files
}

// TestGenerateMultipleProducersSharesDispatcherAndCompiles prova o critério
// de conclusão de TEST-1 (REQ-55.7): cmd/dualproducersvc/main.go constrói UM
// runtime.Dispatcher compartilhado, UMA UnitOfWork sobre ele, e cada canal
// (fizzChannel/buzzChannel) assina o Dispatcher para o(s) PublicEvent que
// carrega — em vez do erro de geração "mais de um módulo produtor..." (F5)
// que generateCmdMainFile devolvia antes desta task. O projeto INTEIRO
// (produtores + os 2 consumidores) compila.
func TestGenerateMultipleProducersSharesDispatcherAndCompiles(t *testing.T) {
	files := generateM15DualProducerProject(t)
	m := filesToMap(files)

	main, ok := m["cmd/dualproducersvc/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/dualproducersvc/main.go (service DualProducerSvc, módulos Fizz/Buzz), não achei:\n%v", filePathsForTest(files))
	}
	s := string(main)
	for _, want := range []string{
		"dispatcher := runtime.NewDispatcher()",
		"uow := runtime.NewUnitOfWork(store, dispatcher)",
		`dispatcher.Subscribe("FizzMade", fizzChannel.Publish)`,
		`dispatcher.Subscribe("BuzzMade", buzzChannel.Publish)`,
		"fizz.Wire(uow)",
		"buzz.Wire(uow)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("esperava %q em cmd/dualproducersvc/main.go, não achei:\n%s", want, s)
		}
	}

	gentest.SmokeCompile(t, m)
}

// TestGenerateMultipleProducersDeterministic prova NFR-13 sobre a fixture de
// múltiplos produtores: a ordem de Subscribe/Wire segue group.modules
// (alfabética: Buzz antes de Fizz), estável entre gerações.
func TestGenerateMultipleProducersDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		files := generateM15DualProducerProject(t)
		for _, f := range files {
			if f.Path == "cmd/dualproducersvc/main.go" {
				return f.Content
			}
		}
		t.Fatal("cmd/dualproducersvc/main.go não encontrado")
		return nil
	})
}

// TestFanOutDoesNotLoseEventsAcrossMultipleProducers é o comportamental de
// TEST-3 (segunda metade): reconstrói, FORA de main.go mas com a MESMA
// sequência de chamadas que cmd/dualproducersvc/main.go emite
// (runtime.NewDispatcher/NewUnitOfWork/Wire/Subscribe), a prova de que um
// PublicEvent emitido por CADA módulo produtor chega ao assinante certo do
// Dispatcher compartilhado — sem perda nem contaminação cruzada entre Fizz e
// Buzz. Roda via `go test` de verdade sobre o Go GERADO (gentest.RunTests),
// não uma reimplementação da lógica de fan-out.
func TestFanOutDoesNotLoseEventsAcrossMultipleProducers(t *testing.T) {
	files := filesToMap(generateM15DualProducerProject(t))
	files[filepath.Join("fizz", "fanout_test.go")] = []byte(m15FanOutBehaviorTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}

// m15FanOutBehaviorTest é um teste EXTERNO ao pacote "fizz" gerado (package
// fizz_test — só símbolos exportados: Wire/MakeFizzUseCase/MakeFizz/FizzId de
// fizz, os equivalentes de buzz, e runtime), embutido no projeto gerado antes
// de rodar `go test` (mesmo padrão de producerOutboxCrashSimulationTest,
// producer_outbox_test.go, K3.4). Substitui a construção real dos canais
// (runtime.NewQueueChannel, já provada em TestGenerateMultipleProducers
// SharesDispatcherAndCompiles/channel_test.go) por 2 spies inscritos
// DIRETAMENTE no MESMO runtime.Dispatcher — o que está sob teste aqui é
// exclusivamente o fan-out em si (2 produtores, 1 Dispatcher/UnitOfWork
// compartilhados), não a entrega do QueueChannel.
var m15FanOutBehaviorTest = `package fizz_test

import (
	"context"
	"testing"

	"domainscript/generated/buzz"
	"domainscript/generated/fizz"
	"domainscript/generated/runtime"
)

type m15FanOutCaller struct{ id string }

func (c m15FanOutCaller) Authenticated() bool { return true }
func (c m15FanOutCaller) ID() string          { return c.id }
func (c m15FanOutCaller) HasRole(string) bool { return false }

func TestFanOutDoesNotLoseEvents(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	dispatcher := runtime.NewDispatcher()
	uow := runtime.NewUnitOfWork(store, dispatcher)

	fizz.Wire(uow)
	buzz.Wire(uow)

	var fizzGot []string
	var buzzGot []string
	dispatcher.Subscribe("FizzMade", func(ctx context.Context, ev runtime.Event) error {
		fizzGot = append(fizzGot, string(ev.(*fizz.FizzMade).Id))
		return nil
	})
	dispatcher.Subscribe("BuzzMade", func(ctx context.Context, ev runtime.Event) error {
		buzzGot = append(buzzGot, string(ev.(*buzz.BuzzMade).Id))
		return nil
	})

	ctx := runtime.WithCaller(context.Background(), m15FanOutCaller{id: "caller-1"})
	if err := fizz.MakeFizzUseCase(ctx, fizz.MakeFizz{Id: fizz.FizzId("fz-1")}); err != nil {
		t.Fatalf("MakeFizzUseCase: %v", err)
	}
	if err := buzz.MakeBuzzUseCase(ctx, buzz.MakeBuzz{Id: buzz.BuzzId("bz-1")}); err != nil {
		t.Fatalf("MakeBuzzUseCase: %v", err)
	}

	if len(fizzGot) != 1 || fizzGot[0] != "fz-1" {
		t.Fatalf("fizzGot = %v, want [\"fz-1\"] (FizzMade não deveria se perder nem ser confundido com o assinante de Buzz)", fizzGot)
	}
	if len(buzzGot) != 1 || buzzGot[0] != "bz-1" {
		t.Fatalf("buzzGot = %v, want [\"bz-1\"] (BuzzMade não deveria se perder nem ser confundido com o assinante de Fizz)", buzzGot)
	}
}
`

// --- TEST-2: produtor + módulo que precisa de Dispatcher, MÓDULOS DIFERENTES ---
//
// m15MixedTopologyDs combina, no MESMO service, um módulo produtor
// não-durável (Producer) e um módulo com uma Query cacheada (Cached, G3 —
// "cache { ttl: ... }", SEM nenhuma Database — a Query cacheada por si só já
// força needsDispatcher, independente de qualquer infra real): a antiga
// guarda F5/G3 recusava exatamente esta combinação ("módulo com Policy/
// Query cacheada E módulo produtor de canal de saída no mesmo service").
// Deliberadamente sem Database "postgres"/"sqlite" real (ao contrário de
// cacheFixtureModDs, decl_query_cache_test.go): mantém o smoke compile desta
// fixture livre de dependência externa (NFR-12), o que não é o que TEST-2
// pede provar.

const m15MixedTopologyDs = `Topology {
    services {
        MixedSvc { modules: [Producer, Cached] }
        MixedSinkSvc { modules: [MixedSink] }
    }
    channels {
        Producer -> MixedSink {
            via: queue
            orderBy: id
        }
    }
}
`

const m15ProducerModDs = `Module Producer { }
`

const m15ProducerDomainDs = `
ValueObject GizmoId(string) {
    Valid { value.length() > 0 }
}

PublicEvent GizmoMade { id GizmoId }

Aggregate Gizmo {
    strategy EventSourced

    state {
        id GizmoId
    }

    access {
        Make requires caller.authenticated
    }

    Handle Make() {
        emit GizmoMade(self.id)
    }

    Apply GizmoMade {
        state.id = event.id
    }
}

Command MakeGizmo {
    id ref Gizmo
}

UseCase MakeGizmoUseCase handles MakeGizmo {
    execute {
        gizmo = load Gizmo(cmd.id)
        gizmo.Make()
    }
}
`

const m15MixedSinkModDs = `Module MixedSink { }
`

const m15MixedSinkPolicyDs = `Policy ReactToGizmo on GizmoMade {
    delivery AtLeastOnce
    execute { return }
}
`

// m15CachedModDs/m15CachedDomainDs: o módulo "Cached" da fixture de TEST-2 —
// um Aggregate mínimo com UseCase (exercita o item 3 de §design 4.3: um
// módulo com UseCase que NÃO é produtor também usa a UoW compartilhada) e uma
// Query com "cache { ttl: ... }" (G3), SEM nenhuma Database declarada — a
// invalidação por Dispatcher não depende de nenhum provider real.
const m15CachedModDs = `Module Cached { }
`

const m15CachedDomainDs = `
ValueObject CachedWidgetId(string) {
    Valid { value.length() > 0 }
}

Event CachedWidgetCreated {
    id CachedWidgetId
}

View CachedWidgetView {
    id CachedWidgetId
}

Aggregate CachedWidget {
    strategy EventSourced

    state {
        id CachedWidgetId
    }

    access {
        Create requires caller.authenticated
    }

    Handle Create() {
        emit CachedWidgetCreated(self.id)
    }

    Apply CachedWidgetCreated {
        state.id = event.id
    }
}

Command CreateCachedWidget {
    id ref CachedWidget
}

UseCase CreateCachedWidgetUseCase handles CreateCachedWidget {
    execute {
        widget = load CachedWidget(cmd.id)
        widget.Create()
    }
}

Query GetCachedWidget(id CachedWidgetId) -> CachedWidgetView {
    cache {
        ttl: 1h
    }
    return load CachedWidget(id) as CachedWidgetView
}
`

// generateM15MixedProject monta e gera o projeto da fixture produtor+
// Dispatcher em módulos diferentes (TEST-2).
func generateM15MixedProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":         m15MixedTopologyDs,
		"producer/mod.ds":     m15ProducerModDs,
		"producer/domain.ds":  m15ProducerDomainDs,
		"cached/mod.ds":       m15CachedModDs,
		"cached/domain.ds":    m15CachedDomainDs,
		"mixedsink/mod.ds":    m15MixedSinkModDs,
		"mixedsink/policy.ds": m15MixedSinkPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de produtor+Dispatcher em módulos diferentes (M1.5, TEST-2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, shopGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de produtor+Dispatcher: %v", err)
	}
	return files
}

// TestGenerateProducerAndDispatcherDifferentModulesCoexistAndCompile prova o
// critério de conclusão de TEST-2 (REQ-55.8): cmd/mixedsvc/main.go constrói o
// Dispatcher compartilhado (por causa da Query cacheada de Cached), o canal
// de Producer assina esse MESMO Dispatcher em vez de recusar a geração
// (antiga F5/G3), e ambos os módulos (Producer via UseCase, Cached via
// UseCase+WireQueryCache) usam a MESMA UnitOfWork compartilhada — o projeto
// INTEIRO compila.
func TestGenerateProducerAndDispatcherDifferentModulesCoexistAndCompile(t *testing.T) {
	files := generateM15MixedProject(t)
	m := filesToMap(files)

	main, ok := m["cmd/mixedsvc/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/mixedsvc/main.go (service MixedSvc, módulos Producer/Cached), não achei:\n%v", filePathsForTest(files))
	}
	s := string(main)
	for _, want := range []string{
		"dispatcher := runtime.NewDispatcher()",
		"uow := runtime.NewUnitOfWork(store, dispatcher)",
		`dispatcher.Subscribe("GizmoMade", producerChannel.Publish)`,
		"producer.Wire(uow)",
		"cached.Wire(uow)",
		"cached.WireQueryCache(dispatcher)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("esperava %q em cmd/mixedsvc/main.go, não achei:\n%s", want, s)
		}
	}
	// sharedUow só existe quando um produtor DURÁVEL já ocupa o nome "uow"
	// (§design 4.3 item 3) — nenhum dos 2 módulos aqui é durável (nenhum
	// declara Database real), então a variável compartilhada continua se
	// chamando "uow", como no caminho de hoje.
	if strings.Contains(s, "sharedUow") {
		t.Errorf("NÃO esperava \"sharedUow\" em cmd/mixedsvc/main.go (nenhum produtor durável neste service):\n%s", s)
	}

	gentest.SmokeCompile(t, m)
}

// --- TEST-3 (negativo): a combinação que GENUINAMENTE resta não suportada ---
//
// m15DurableSelfDispatchTopologyDs faz o MESMO módulo (DurableSelf) ser, ao
// mesmo tempo, produtor DURÁVEL (Database "postgres" real + canal
// provider:"rabbitmq", durableProducer) e dono de uma Query cacheada (G3,
// precisa de Dispatcher LOCAL) — a combinação que §design 4.3 item 4
// documenta como fora do escopo desta task (exigiria estender
// NewOutboxUnitOfWork, codegen/sqlrt/uow.go.txt, com um Publisher opcional).

const m15DurableSelfDispatchTopologyDs = `Topology {
    services {
        DurableSelfDispatchSvc { modules: [DurableSelf] }
        DurableSelfSinkSvc { modules: [DurableSelfSink] }
    }
    channels {
        DurableSelf -> DurableSelfSink {
            via: queue
            provider: "rabbitmq"
            connection: env("AMQP_URL")
            orderBy: id
        }
    }
}
`

const m15DurableSelfModDs = `Module DurableSelf {
    Database MainDb {
        provider: "postgres"
        manages: [Ticket]
    }
}
`

const m15DurableSelfDomainDs = `
ValueObject TicketId(string) {
    Valid { value.length() > 0 }
}

PublicEvent TicketMade { id TicketId }

View TicketView {
    id TicketId
}

Aggregate Ticket {
    strategy EventSourced

    state {
        id TicketId
    }

    access {
        Make requires caller.authenticated
    }

    Handle Make() {
        emit TicketMade(self.id)
    }

    Apply TicketMade {
        state.id = event.id
    }
}

Command MakeTicket {
    id ref Ticket
}

UseCase MakeTicketUseCase handles MakeTicket {
    execute {
        t = load Ticket(cmd.id)
        t.Make()
    }
}

Query GetTicket(ticketId TicketId) -> TicketView {
    cache {
        ttl: 1h
    }
    return load Ticket(ticketId) as TicketView
}
`

const m15DurableSelfSinkModDs = `Module DurableSelfSink { }
`

const m15DurableSelfSinkPolicyDs = `Policy ReactToTicket on TicketMade {
    delivery AtLeastOnce
    execute { return }
}
`

// TestGenerateDurableProducerWithLocalDispatcherIsGenerationError prova a
// metade negativa de TEST-3: um módulo AO MESMO TEMPO produtor durável e
// dono de Dispatcher local (Query cacheada) é um erro de geração CLARO —
// nunca uma miscompilação silenciosa (a UoW SQL do produtor, sem a extensão
// de Publisher opcional de §design 4.3 item 4, nunca alimentaria o
// Dispatcher com os eventos privados que a invalidação de cache precisa).
func TestGenerateDurableProducerWithLocalDispatcherIsGenerationError(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":               m15DurableSelfDispatchTopologyDs,
		"durableself/mod.ds":        m15DurableSelfModDs,
		"durableself/domain.ds":     m15DurableSelfDomainDs,
		"durableselfsink/mod.ds":    m15DurableSelfSinkModDs,
		"durableselfsink/policy.ds": m15DurableSelfSinkPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de produtor durável + Dispatcher local não deveria ter erros de front-end:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	_, err := codegen.Generate(prog, model, prog.Symbols, bag, shopGenerateOptions)
	if err == nil {
		t.Fatal("esperava erro de geração: DurableSelf é produtor durável E precisa de Dispatcher local (Query cacheada) — combinação ainda não suportada")
	}
	for _, want := range []string{"DurableSelf", "produtor durável", "Dispatcher"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("mensagem de erro não menciona %q: %v", want, err)
		}
	}
}

// --- TEST-4 (byte-identidade, NFR-31) ---
//
// TestGenerateWalletMainWiresUseCases (codegen_test.go, já existente) e
// TestGenerateShopPolicyRegistersSubscriberAndCompiles/TestGenerateChannel
// FixtureProducerAndConsumerCompile/TestProducerOutboxFixtureWiringAndSmoke
// Compile (decl_policy_test.go/channel_test.go/producer_outbox_test.go, não
// tocados por esta task) já fixam, literal, cada linha de wiring que
// continua sendo emitida em cada um desses services — nenhuma dessas
// asserções mudou nesta task, e permanecer verde É a prova de byte-
// identidade. A guarda abaixo é NEGATIVA e complementar: nenhum dos dois
// nomes que só existem sob contenção pelo Publisher (sharedUow,
// "dispatcher.Subscribe(") aparece em nenhum service de wallet/shop — os
// dois têm no máximo 1 candidato ao Publisher por service.
func TestGenerateM15ByteIdentityWalletAndShop(t *testing.T) {
	wallet := string(walletCmdMainFile(t))
	for _, notWant := range []string{"sharedUow", "dispatcher.Subscribe("} {
		if strings.Contains(wallet, notWant) {
			t.Errorf("cmd/wallet/main.go não deveria conter %q (wallet não tem produtor de canal): \n%s", notWant, wallet)
		}
	}

	for _, f := range generateShopProject(t) {
		if !strings.HasPrefix(f.Path, "cmd/") || !strings.HasSuffix(f.Path, "main.go") {
			continue
		}
		s := string(f.Content)
		for _, notWant := range []string{"sharedUow", "dispatcher.Subscribe("} {
			if strings.Contains(s, notWant) {
				t.Errorf("%s não deveria conter %q (shop não combina múltiplos produtores nem produtor+Dispatcher no mesmo service): \n%s", f.Path, notWant, s)
			}
		}
	}
}
