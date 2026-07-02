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

// decl_aggregate_load_test.go prova os critérios de conclusão da task E6.2
// (§design codegen 3.7, REQ-19.4/19.5): a reconstrução Load<Nome> que
// decl_aggregate_load.go ACRESCENTA à emissão de decl_aggregate.go (E6.1).
//
// Duas fixtures:
//   - o Aggregate Wallet real (docs/examples/wallet), EventSourced sem
//     snapshot — o caminho testado contra o domínio de verdade.
//   - uma fixture SINTÉTICA (aggregateLoadFixtureSrc, abaixo) com um segundo
//     Aggregate StateStored (o wallet não usa essa estratégia) e um terceiro
//     EventSourced COM "snapshot every N events" (o wallet também não usa) —
//     nenhum dos dois é exercitado pelo domínio real, exatamente como
//     decl_event_version_test.go já fez para default/Upcast/redactable.

// --- Parte 1: EventSourced sobre o Aggregate Wallet real. ---

// emitWalletAggregateLoad gera o Go de Load<Nome> para o Aggregate Wallet
// real — o mesmo Model+SymbolTable+registry de decl_aggregate_test.go.
func emitWalletAggregateLoad(t *testing.T) []byte {
	t.Helper()
	prog := parseWalletProgram(t)
	agg := findAggregateDecl(t, prog, "Wallet")
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitAggregateLoad("wallet", agg, model, prog.Symbols, "Wallet")
	if err != nil {
		t.Fatalf("EmitAggregateLoad: erro inesperado: %v", err)
	}
	return got
}

// TestEmitAggregateLoadEventSourcedGolden gera LoadWallet e compara byte a
// byte com o artefato versionado.
func TestEmitAggregateLoadEventSourcedGolden(t *testing.T) {
	got := emitWalletAggregateLoad(t)
	gentest.Golden(t, filepath.Join("testdata", "aggregate_wallet_load.go.golden"), got)
}

// TestEmitAggregateLoadEventSourcedDeterministic prova NFR-13.
func TestEmitAggregateLoadEventSourcedDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWalletAggregateLoad(t)
	})
}

// walletAggregateLoadSmokeFiles estende aggregateSmokeFiles (decl_aggregate_
// test.go, E6.1) com o Go de LoadWallet desta task — mesmo conjunto de
// arquivos (go.mod, runtime real, VOs/Enum/Errors/Events do wallet,
// aggregate_wallet.go) mais aggregate_wallet_load.go.
func walletAggregateLoadSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := aggregateSmokeFiles(t)
	files[filepath.Join("wallet", "aggregate_wallet_load.go")] = emitWalletAggregateLoad(t)
	return files
}

// TestEmitAggregateLoadEventSourcedSmokeCompile prova NFR-14: LoadWallet,
// junto do restante do módulo wallet e do runtime vendorado real, compila e
// passa go vet num projeto isolado.
func TestEmitAggregateLoadEventSourcedSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, walletAggregateLoadSmokeFiles(t))
}

// walletAggregateLoadBehaviorTest roda dentro do projeto isolado gerado no
// smoke e prova, sobre o Go de fato gerado (não uma reimplementação): monta
// um runtime.EventStore in-memory de verdade, grava dois DepositPerformed via
// tx.Append (o wallet não tem Handle/Apply de criação — o replay começa
// direto de DepositPerformed, como o prompt da task antecipa), chama
// LoadWallet e confere que o state reconstruído soma os dois depósitos.
//
// Achado documentado (não é bug desta task, é uma lacuna do domínio wallet
// real — §design 6, "fixtures não são fonte de verdade"): LoadWallet começa
// SEMPRE de um *Wallet cujo state é o zero-value Go (w := &Wallet{id: id}) —
// não há Apply WalletCreated que estabeleça um Money inicial. Money.Add
// (Operator +, E3.2) exige currency == other.currency; a zero-value de Money
// tem Currency == "". Por isso os dois depósitos abaixo usam moeda "" (em vez
// de "BRL"): com uma moeda != "" no 1º depósito, o replay já falharia no
// PRIMEIRO Add com CurrencyMismatch — não uma falha do mecanismo de replay
// (Tx.Load + switch + applyX, o que esta task testa), mas do domínio de
// exemplo não modelar uma "criação" que fixasse a moeda antes do 1º Apply.
// Fora do escopo de E6.2 corrigir o domain.ds (não pedido pela task).
const walletAggregateLoadBehaviorTest = `package wallet

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func TestLoadWalletReplaysStreamAndSumsDeposits(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow := runtime.NewUnitOfWork(store)
	ctx := context.Background()

	// Moeda "" (não "BRL") de propósito — ver o comentário de
	// walletAggregateLoadBehaviorTest acima: casa com a moeda zero-value de
	// Money, que é o que o 1º Apply de um replay-do-zero sempre vê.
	amount1, err := NewMoney(mustDecimal(t, "10.00"), "")
	if err != nil {
		t.Fatal(err)
	}
	amount2, err := NewMoney(mustDecimal(t, "5.00"), "")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}

	err = uow.Run(ctx, func(tx runtime.Tx) error {
		return tx.Append("W1", []runtime.Event{
			&DepositPerformed{Id: WalletId("W1"), Amount: amount1, Description: desc},
			&DepositPerformed{Id: WalletId("W1"), Amount: amount2, Description: desc},
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	var w *Wallet
	err = uow.Run(ctx, func(tx runtime.Tx) error {
		loaded, err := LoadWallet(tx, WalletId("W1"))
		if err != nil {
			return err
		}
		w = loaded
		return nil
	})
	if err != nil {
		t.Fatalf("LoadWallet: %v", err)
	}

	if w.state.Id != WalletId("W1") {
		t.Fatalf("state.Id não sincronizado: got %v, want W1", w.state.Id)
	}
	want := mustDecimal(t, "15.00")
	if w.state.Balance.Amount.Cmp(want) != 0 {
		t.Fatalf("Balance reconstruído incorreto: got %s, want %s", w.state.Balance.Amount, want)
	}
	entries := w.state.Entries.Items()
	if len(entries) != 2 {
		t.Fatalf("esperava 2 entries reconstruídas via replay, got %d", len(entries))
	}
}

func TestLoadWalletUnknownStreamReturnsEmptyReconstruction(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow := runtime.NewUnitOfWork(store)

	var w *Wallet
	err := uow.Run(context.Background(), func(tx runtime.Tx) error {
		loaded, err := LoadWallet(tx, WalletId("desconhecido"))
		if err != nil {
			return err
		}
		w = loaded
		return nil
	})
	if err != nil {
		t.Fatalf("LoadWallet de stream desconhecido não deveria falhar: %v", err)
	}
	if w.state.Id != WalletId("desconhecido") {
		t.Fatalf("state.Id deveria estar sincronizado mesmo sem eventos: got %v", w.state.Id)
	}
	if len(w.state.Entries.Items()) != 0 {
		t.Fatalf("esperava 0 entries para stream desconhecido, got %d", len(w.state.Entries.Items()))
	}
}
`

// TestEmitAggregateLoadEventSourcedBehavior prova NFR-15 sobre o Go de fato
// gerado: escreve os arquivos do smoke mais o teste comportamental acima num
// diretório isolado e roda `go test ./...` de verdade. Reusa
// walletAggregateBehaviorTest (decl_aggregate_test.go, E6.1) para ganhar
// mustDecimal/stubCaller — mas como cada arquivo _test.go precisa compilar
// isoladamente dentro do MESMO pacote Go, basta escrever os dois arquivos de
// teste juntos (mustDecimal já existe em aggregate_behavior_test.go).
func TestEmitAggregateLoadEventSourcedBehavior(t *testing.T) {
	files := walletAggregateLoadSmokeFiles(t)
	files[filepath.Join("wallet", "aggregate_behavior_test.go")] = []byte(walletAggregateBehaviorTest)
	files[filepath.Join("wallet", "aggregate_load_behavior_test.go")] = []byte(walletAggregateLoadBehaviorTest)

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

// --- Parte 2: fixture sintética — StateStored + EventSourced com snapshot. ---

// aggregateLoadFixtureSrc declara dois Aggregates que o wallet real não
// exercita (E6.2 §"Testes", itens 2/3): Counter é StateStored (REQ-19.5, o
// padrão do spec quando "strategy" está ausente — aqui declarado explícito
// por clareza) e SnapCounter é EventSourced com "snapshot every 3 events"
// (REQ-19.4) — o runtime não tem SnapshotStore (decisão (b) documentada em
// decl_aggregate_load.go), então SnapCounter só prova que o replay COMPLETO
// continua correto mesmo com Snapshot declarado. Ambos evitam Operator de VO
// de propósito (Apply só faz atribuição direta): esta fixture testa
// Load<Nome>, não o dispatch de operador (§4.2), já coberto por E6.1/E3.2.
const aggregateLoadFixtureSrc = `
ValueObject EntityId(string) {
    Valid { value.length() > 0 }
}

ValueObject Count(integer) {
    Valid { ok }
}

Event CounterIncremented {
    id EntityId
    amount Count
}

Event SnapCounterIncremented {
    id EntityId
    amount Count
}

// Counter: StateStored (REQ-19.5) — Load<Nome> lê o state direto de um
// Repository, sem replay.
Aggregate Counter {
    strategy StateStored

    state {
        id EntityId
        count Count
    }

    access {
        Increment requires caller.authenticated
    }

    Handle Increment(amount Count) {
        emit CounterIncremented(self.id, amount)
    }

    Apply CounterIncremented {
        state.count = event.amount
    }
}

// SnapCounter: EventSourced com snapshot declarado (REQ-19.4) — cobre o
// caminho estrutural de "snapshot every N events" sem que o runtime precise
// de um SnapshotStore ainda (decisão (b), ver decl_aggregate_load.go).
Aggregate SnapCounter {
    strategy EventSourced
    snapshot every 3 events

    state {
        id EntityId
        count Count
    }

    access {
        Increment requires caller.authenticated
    }

    Handle Increment(amount Count) {
        emit SnapCounterIncremented(self.id, amount)
    }

    Apply SnapCounterIncremented {
        state.count = event.amount
    }
}
`

// aggregateLoadFixtureModDs declara o módulo e o banco que "gerencia" os dois
// Aggregates da fixture — mesma forma mínima de docs/examples/wallet/mod.ds.
const aggregateLoadFixtureModDs = `Module Counter {
    Database CounterDb {
        provider: "postgres"
        manages: [Counter, SnapCounter]
    }
}
`

// writeProjectDir escreve files (caminho relativo → conteúdo) num diretório
// temporário — usado para montar um projeto de verdade (mod.ds + domain.ds)
// que driver.CheckProject possa resolver (a fixture PRECISA de um projeto:
// driver.CheckSource não devolve a SymbolTable que EmitAggregateLoad exige,
// só CheckProject expõe prog.Symbols — mesma razão de decl_aggregate_test.go
// usar o wallet via CheckProject em vez de CheckSource).
func writeProjectDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("não consegui criar %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("não consegui escrever %q: %v", full, err)
		}
	}
	return dir
}

// parseAggregateLoadFixture monta o projeto sintético em disco e o resolve
// via driver.CheckProject — devolve o Program e os dois AggregateDecl.
func parseAggregateLoadFixture(t *testing.T) (*program.Program, *ast.AggregateDecl, *ast.AggregateDecl) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    aggregateLoadFixtureModDs,
		"domain.ds": aggregateLoadFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Load (E6.2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	counter := findAggregateDecl(t, prog, "Counter")
	snap := findAggregateDecl(t, prog, "SnapCounter")
	return prog, counter, snap
}

// --- Parte 2a: Counter (StateStored). ---

// emitCounterAggregateAndLoad gera EmitAggregate + EmitAggregateLoad de
// Counter (§design 3.7: Load<Nome> ESTENDE a emissão de EmitAggregate, então
// os dois arquivos precisam existir juntos no mesmo pacote).
func emitCounterAggregateAndLoad(t *testing.T) (aggGo, loadGo []byte) {
	t.Helper()
	prog, counter, _ := parseAggregateLoadFixture(t)
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	aggGo, err := codegen.EmitAggregate("counter", counter, model, prog.Symbols, "Counter", reg)
	if err != nil {
		t.Fatalf("EmitAggregate(Counter): erro inesperado: %v", err)
	}
	loadGo, err = codegen.EmitAggregateLoad("counter", counter, model, prog.Symbols, "Counter")
	if err != nil {
		t.Fatalf("EmitAggregateLoad(Counter): erro inesperado: %v", err)
	}
	return aggGo, loadGo
}

// TestEmitAggregateLoadStateStoredGolden gera LoadCounter (StateStored) e
// compara byte a byte com o artefato versionado.
func TestEmitAggregateLoadStateStoredGolden(t *testing.T) {
	_, loadGo := emitCounterAggregateAndLoad(t)
	gentest.Golden(t, filepath.Join("testdata", "aggregate_counter_load.go.golden"), loadGo)
}

// TestEmitAggregateLoadStateStoredDeterministic prova NFR-13.
func TestEmitAggregateLoadStateStoredDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		_, loadGo := emitCounterAggregateAndLoad(t)
		return loadGo
	})
}

// findValueObjectDecl/findEventDecl indexam os decls sintéticos da fixture
// por nome — espelham findAggregateDecl (decl_aggregate_test.go).
func findValueObjectDecl(t *testing.T, prog *program.Program, name string) *ast.ValueObjectDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if vo, ok := d.(*ast.ValueObjectDecl); ok && vo.Name == name {
				return vo
			}
		}
	}
	t.Fatalf("ValueObject %q não encontrado na fixture", name)
	return nil
}

func findEventDecl(t *testing.T, prog *program.Program, name string) *ast.EventDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if ev, ok := d.(*ast.EventDecl); ok && ev.Name == name {
				return ev
			}
		}
	}
	t.Fatalf("Event %q não encontrado na fixture", name)
	return nil
}

// aggregateLoadFixtureCommonFiles monta go.mod + runtime real + os
// ValueObjects/Events comuns às duas fixtures (Counter/SnapCounter) — usado
// pelos dois smoke compiles desta parte.
func aggregateLoadFixtureCommonFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog, _, _ := parseAggregateLoadFixture(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	entityID := findValueObjectDecl(t, prog, "EntityId")
	count := findValueObjectDecl(t, prog, "Count")
	for _, spec := range []struct {
		decl *ast.ValueObjectDecl
		file string
	}{
		{entityID, "entity_id.go"},
		{count, "count.go"},
	} {
		got, err := codegen.EmitValueObject("counter", spec.decl)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", spec.decl.Name, err)
		}
		files[filepath.Join("counter", spec.file)] = got
	}

	eventsGo, err := codegen.EmitEvents("counter", []*ast.EventDecl{
		findEventDecl(t, prog, "CounterIncremented"),
		findEventDecl(t, prog, "SnapCounterIncremented"),
	})
	if err != nil {
		t.Fatalf("EmitEvents: erro inesperado: %v", err)
	}
	files[filepath.Join("counter", "events.go")] = eventsGo

	return files
}

// counterSmokeFiles junta os arquivos comuns ao Aggregate Counter
// (StateStored) gerado por esta task.
func counterSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := aggregateLoadFixtureCommonFiles(t)
	aggGo, loadGo := emitCounterAggregateAndLoad(t)
	files[filepath.Join("counter", "aggregate_counter.go")] = aggGo
	files[filepath.Join("counter", "aggregate_counter_load.go")] = loadGo
	return files
}

// TestEmitAggregateLoadStateStoredSmokeCompile prova NFR-14 para o caminho
// StateStored.
func TestEmitAggregateLoadStateStoredSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, counterSmokeFiles(t))
}

// counterBehaviorTest roda dentro do projeto isolado gerado no smoke e prova,
// sobre o Go de fato gerado: LoadCounter devolve (nil, nil) quando o
// Repository não tem state salvo para o id (REQ-19.5: "não encontrado" não é
// erro de infra); e devolve o state salvo quando o Repository o tem — sem
// nenhum replay envolvido (StateStored).
const counterBehaviorTest = `package counter

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func TestLoadCounterReturnsNilNilWhenNotFound(t *testing.T) {
	repo := runtime.NewMemoryRepository[counterState]()
	got, err := LoadCounter(context.Background(), repo, EntityId("c1"))
	if err != nil {
		t.Fatalf("LoadCounter: erro inesperado: %v", err)
	}
	if got != nil {
		t.Fatalf("esperava (nil, nil) para id ausente, got %+v", got)
	}
}

func TestLoadCounterReturnsSavedState(t *testing.T) {
	repo := runtime.NewMemoryRepository[counterState]()
	ctx := context.Background()

	saved := counterState{Id: EntityId("c1"), Count: Count(42)}
	if err := repo.Save(ctx, "c1", saved); err != nil {
		t.Fatal(err)
	}

	got, err := LoadCounter(ctx, repo, EntityId("c1"))
	if err != nil {
		t.Fatalf("LoadCounter: erro inesperado: %v", err)
	}
	if got == nil {
		t.Fatal("esperava reconstrução não-nil")
	}
	if got.state.Count != 42 {
		t.Fatalf("Count incorreto: got %d, want 42", got.state.Count)
	}
	if got.state.Id != EntityId("c1") {
		t.Fatalf("Id não sincronizado: got %v, want c1", got.state.Id)
	}
}
`

// TestEmitAggregateLoadStateStoredBehavior prova NFR-15 sobre o Go de fato
// gerado para o caminho StateStored.
func TestEmitAggregateLoadStateStoredBehavior(t *testing.T) {
	files := counterSmokeFiles(t)
	files[filepath.Join("counter", "behavior_test.go")] = []byte(counterBehaviorTest)
	runGeneratedTests(t, files)
}

// --- Parte 2b: SnapCounter (EventSourced com snapshot declarado). ---

// emitSnapCounterAggregateAndLoad gera EmitAggregate + EmitAggregateLoad de
// SnapCounter.
func emitSnapCounterAggregateAndLoad(t *testing.T) (aggGo, loadGo []byte) {
	t.Helper()
	prog, _, snap := parseAggregateLoadFixture(t)
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	aggGo, err := codegen.EmitAggregate("counter", snap, model, prog.Symbols, "Counter", reg)
	if err != nil {
		t.Fatalf("EmitAggregate(SnapCounter): erro inesperado: %v", err)
	}
	loadGo, err = codegen.EmitAggregateLoad("counter", snap, model, prog.Symbols, "Counter")
	if err != nil {
		t.Fatalf("EmitAggregateLoad(SnapCounter): erro inesperado: %v", err)
	}
	return aggGo, loadGo
}

// TestEmitAggregateLoadSnapshotDeclaredCompilesFullReplay prova o item 2 da
// task (opção (b), documentada em decl_aggregate_load.go): decl.Snapshot !=
// nil não muda a FORMA de LoadSnapCounter — continua um replay completo — mas
// a função ainda precisa compilar e o comentário estrutural de snapshot
// precisa estar presente no Go gerado.
func TestEmitAggregateLoadSnapshotDeclaredCompilesFullReplay(t *testing.T) {
	_, loadGo := emitSnapCounterAggregateAndLoad(t)
	if !strings.Contains(string(loadGo), "SnapshotStore") {
		t.Fatalf("esperava um comentário apontando a ausência de SnapshotStore (decisão (b)) no Go gerado:\n%s", loadGo)
	}
}

// snapCounterSmokeFiles junta os arquivos comuns ao Aggregate SnapCounter
// (EventSourced com snapshot) gerado por esta task.
func snapCounterSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := aggregateLoadFixtureCommonFiles(t)
	aggGo, loadGo := emitSnapCounterAggregateAndLoad(t)
	files[filepath.Join("counter", "aggregate_snap_counter.go")] = aggGo
	files[filepath.Join("counter", "aggregate_snap_counter_load.go")] = loadGo
	return files
}

// TestEmitAggregateLoadSnapshotSmokeCompile prova NFR-14 para o caminho
// EventSourced com Snapshot declarado.
func TestEmitAggregateLoadSnapshotSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, snapCounterSmokeFiles(t))
}

// snapCounterBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado, que o replay COMPLETO continua correto
// mesmo com "snapshot every N events" declarado (item 2 da task, caminho (b)):
// grava 2 eventos via tx.Append e confere que LoadSnapCounter aplica os DOIS,
// em ordem (o 2º evento "vence" porque Apply faz atribuição direta, não soma —
// prova que o replay passou pelos dois, não só aplicou o primeiro).
const snapCounterBehaviorTest = `package counter

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func TestLoadSnapCounterFullReplayAppliesAllEventsInOrder(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow := runtime.NewUnitOfWork(store)
	ctx := context.Background()

	err := uow.Run(ctx, func(tx runtime.Tx) error {
		return tx.Append("sc1", []runtime.Event{
			&SnapCounterIncremented{Id: EntityId("sc1"), Amount: Count(1)},
			&SnapCounterIncremented{Id: EntityId("sc1"), Amount: Count(2)},
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	var got *SnapCounter
	err = uow.Run(ctx, func(tx runtime.Tx) error {
		loaded, err := LoadSnapCounter(tx, EntityId("sc1"))
		if err != nil {
			return err
		}
		got = loaded
		return nil
	})
	if err != nil {
		t.Fatalf("LoadSnapCounter: %v", err)
	}

	if got.state.Count != 2 {
		t.Fatalf("Count = %d, want 2 (o 2º evento do replay deveria ter sido aplicado por último)", got.state.Count)
	}
	if got.state.Id != EntityId("sc1") {
		t.Fatalf("Id não sincronizado: got %v, want sc1", got.state.Id)
	}
}
`

// TestEmitAggregateLoadSnapshotBehavior prova NFR-15: replay completo correto
// mesmo com Snapshot declarado (decisão (b) documentada).
func TestEmitAggregateLoadSnapshotBehavior(t *testing.T) {
	files := snapCounterSmokeFiles(t)
	files[filepath.Join("counter", "behavior_test.go")] = []byte(snapCounterBehaviorTest)
	runGeneratedTests(t, files)
}

// runGeneratedTests escreve files num diretório isolado e roda `go test
// ./...` de verdade — helper compartilhado pelos testes comportamentais desta
// parte (mesmo padrão de TestEmitAggregateBehavior, decl_aggregate_test.go).
func runGeneratedTests(t *testing.T, files map[string][]byte) {
	t.Helper()
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
