package codegen_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/goname"
	"domainscript/codegen/rtsrc"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/token"
	"domainscript/types"
)

// decl_usecase_test.go prova os critérios de conclusão da task E7.2 (§design
// codegen 3.8, REQ-20.2/20.3) sobre os UseCases reais do wallet
// (docs/examples/wallet/application.ds): golden, determinismo, smoke compile
// e dois testes comportamentais sobre o Go de fato gerado — mesmo padrão de
// decl_aggregate_test.go/decl_aggregate_load_test.go/decl_command_test.go.
//
// --- Achado importante, documentado aqui (ver também o resumo da task) ---
//
// A gramática do parser NÃO fecha statement por quebra de linha (documentado
// desde E5.3, codegen/lower/builtins_test.go): "load T(id)" aceita um
// IDENTIFICADOR opcional logo em seguida como "binding" (a mesma sintaxe de
// "list Ticket t where ..."). Isso significa que, ao parsear de verdade
// "wallet = load Wallet(cmd.walletId)\nwallet.Deposit(cmd.amount,
// cmd.description)" (o corpo real de application.ds), o parser consome
// "wallet" (da 2ª linha) como o BINDING da QueryExpr, e a chamada
// ".Deposit(...)" encadeia como postfix sobre a PRÓPRIA QueryExpr — o
// execute inteiro vira UM ÚNICO AssignStmt (confirmado nesta task: `go run`
// sobre driver.CheckProject("docs/examples/wallet") mostra
// len(uc.Execute.Stmts) == 1, não 2), não os dois statements que um leitor
// humano do .ds esperaria. Isso é um bug de gramática PRÉ-EXISTENTE do
// front-end (fora do escopo desta task de codegen — consertar exigiria mudar
// parser/parse_query.go, um componente do front-end "pronto" por CLAUDE.md,
// e arriscar regressão na sintaxe legítima "list T t where ..."), e ainda
// não tinha sido detectado porque nenhuma fase anterior precisava do FORMATO
// do corpo — só de nomes resolvidos (REQ-9) e tipos compatíveis (REQ-13,
// que devolve ErrorType para QueryExpr e por isso não valida essa forma).
//
// Esta task é a PRIMEIRA a precisar da forma exata do corpo (para reconhecer
// dispatch de Handle) e por isso é a primeira a expor o problema. Solução
// adotada aqui, mesmo precedente já usado por E5.3
// (TestStmt_Load_RealWalletUseCase_CompletionCriterion): reconstrução do
// Execute à mão (dois statements limpos — AssignStmt + ExprStmt) sobre o
// Model/SymbolTable/Aggregate REAIS do wallet (via driver.CheckProject),
// preservando Name/Handles/Timeout/Tenancy do UseCaseDecl de fato parseado.
// EmitUseCase/EmitUseCases em si operam sobre QUALQUER *ast.UseCaseDecl bem
// formado — não sabem nem precisam saber desta reconstrução; ela só existe
// nos testes desta task, para contornar o bug de gramática ao montar a
// fixture "real".

// ucIdent/ucMember/ucArg/ucCall são os mesmos helpers mínimos de
// construção manual de AST usados por codegen/lower/builtins_test.go
// (E5.3) para o mesmo propósito: montar, à mão, a forma que o parser
// DEVERIA produzir para "wallet = load Wallet(cmd.walletId)" +
// "wallet.Deposit(cmd.amount, cmd.description)", contornando a ambiguidade
// de gramática documentada acima.
func ucIdent(n string) *ast.Ident { return ast.NewIdent(n, ast.Span{}) }
func ucMember(x ast.Expr, n string) *ast.MemberExpr {
	return ast.NewMemberExpr(x, n, token.Pos{}, ast.Span{})
}
func ucArg(v ast.Expr) ast.Arg { return ast.Arg{Value: v} }
func ucCall(fn ast.Expr, args ...ast.Arg) *ast.CallExpr {
	return ast.NewCallExpr(fn, args, ast.Span{})
}

// handDispatchExecuteBlock monta, à mão, o corpo canônico de um UseCase
// simples: "localVar = load AggName(cmd.idField)" seguido de
// "localVar.HandleName(cmd.cmdArgFields[0], cmd.cmdArgFields[1], ...)" — a
// forma que o parser DEVERIA produzir (ver a doc do arquivo).
func handDispatchExecuteBlock(localVar, aggName, idField, handleName string, cmdArgFields ...string) *ast.Block {
	loadTarget := ucCall(ucIdent(aggName), ucArg(ucMember(ucIdent("cmd"), idField)))
	loadExpr := ast.NewQueryExpr("load", loadTarget, "", nil, ast.Span{})
	assign := ast.NewAssignStmt(ucIdent(localVar), loadExpr, ast.Span{})

	args := make([]ast.Arg, len(cmdArgFields))
	for i, f := range cmdArgFields {
		args[i] = ucArg(ucMember(ucIdent("cmd"), f))
	}
	dispatch := ast.NewExprStmt(ucCall(ucMember(ucIdent(localVar), handleName), args...), ast.Span{})
	return ast.NewBlock([]ast.Stmt{assign, dispatch}, ast.Span{})
}

// --- Parte 1: os 2 UseCases reais do wallet (PerformDeposit/PerformWithdrawal). ---

// parseWalletUseCaseMetas acha os UseCaseDecl reais de application.ds, na
// ordem de declaração (PerformDeposit, PerformWithdrawal) — só para extrair
// Name/Handles/Timeout/Tenancy (ver a doc do arquivo sobre por que Execute é
// reconstruído à mão).
func parseWalletUseCaseMetas(t *testing.T) []*ast.UseCaseDecl {
	t.Helper()
	prog := parseWalletProgram(t)

	paths := make([]string, 0, len(prog.Files))
	for path := range prog.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var out []*ast.UseCaseDecl
	for _, path := range paths {
		for _, d := range prog.Files[path].Decls {
			if u, ok := d.(*ast.UseCaseDecl); ok {
				out = append(out, u)
			}
		}
	}
	return out
}

// reconstructedWalletUseCases devolve os 2 UseCases reais do wallet com o
// MESMO Name/Handles/Timeout/Tenancy do parser, mas Execute reconstruído à
// mão (handDispatchExecuteBlock) — ver a doc do arquivo. Ambos os Handles
// reais (Deposit/Withdraw) têm a mesma forma de parâmetros
// (amount Money, description TransactionDescription), e Handles (o nome do
// Command) é idêntico ao nome do Handle correspondente no Aggregate (§5.1:
// "Command tem o mesmo nome do Handle que aciona").
func reconstructedWalletUseCases(t *testing.T) []*ast.UseCaseDecl {
	t.Helper()
	metas := parseWalletUseCaseMetas(t)
	if len(metas) != 2 {
		t.Fatalf("esperava 2 UseCases em wallet/application.ds, achei %d", len(metas))
	}
	out := make([]*ast.UseCaseDecl, len(metas))
	for i, m := range metas {
		execute := handDispatchExecuteBlock("wallet", "Wallet", "walletId", m.Handles, "amount", "description")
		out[i] = ast.NewUseCaseDecl(m.Name, m.Handles, m.Timeout, m.Idempotency, m.Tenancy, execute, m.Span())
	}
	return out
}

// emitWalletUseCases monta o Model+SymbolTable+aggregates sobre o programa
// real e gera o Go dos 2 UseCases do wallet.
func emitWalletUseCases(t *testing.T) []byte {
	t.Helper()
	prog := parseWalletProgram(t)
	agg := findAggregateDecl(t, prog, "Wallet")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)
	aggregates := map[string]*ast.AggregateDecl{"Wallet": agg}
	decls := reconstructedWalletUseCases(t)

	got, err := codegen.EmitUseCases("wallet", decls, aggregates, model, prog.Symbols, "Wallet", reg, nil)
	if err != nil {
		t.Fatalf("EmitUseCases: erro inesperado: %v", err)
	}
	return got
}

// TestEmitUseCasesGolden gera o Go dos 2 UseCases reais do wallet
// (PerformDeposit/PerformWithdrawal) num único arquivo e compara byte a byte
// com o artefato versionado — confirma, junto do golden, os 4 elementos do
// critério de conclusão da task (uow.Run, LoadWallet(tx, cmd.WalletId),
// wallet.Deposit(...)/wallet.Withdraw(...), tx.Append(...)).
func TestEmitUseCasesGolden(t *testing.T) {
	got := emitWalletUseCases(t)
	for _, want := range []string{
		"uow.Run(ctx, func(tx runtime.Tx) error",
		"LoadWallet(tx, cmd.WalletId)",
		"wallet.Deposit(caller, cmd.Amount, cmd.Description)",
		"wallet.Withdraw(caller, cmd.Amount, cmd.Description)",
		"tx.Append(string(wallet.id), events)",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "usecases_wallet.go.golden"), got)
}

// TestEmitUseCaseGoldenSingle gera o Go de um único UseCase (PerformDeposit)
// via EmitUseCase e compara com um segundo artefato versionado — prova que a
// forma "um de cada vez" também é suportada e estável (mesmo contrato de
// EmitCommand/EmitCommands, EmitEvent/EmitEvents).
func TestEmitUseCaseGoldenSingle(t *testing.T) {
	prog := parseWalletProgram(t)
	agg := findAggregateDecl(t, prog, "Wallet")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)
	aggregates := map[string]*ast.AggregateDecl{"Wallet": agg}
	decls := reconstructedWalletUseCases(t)

	got, err := codegen.EmitUseCase("wallet", decls[0], aggregates, model, prog.Symbols, "Wallet", reg, nil)
	if err != nil {
		t.Fatalf("EmitUseCase(PerformDeposit): erro inesperado: %v", err)
	}
	gentest.Golden(t, filepath.Join("testdata", "usecase_deposit.go.golden"), got)
}

// TestEmitUseCasesDeterministic prova NFR-13.
func TestEmitUseCasesDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWalletUseCases(t)
	})
}

// usecaseSmokeFiles estende walletAggregateLoadSmokeFiles (decl_aggregate_
// load_test.go, E6.2) com os Commands (E7.1) e os UseCases desta task — o
// conjunto completo do write side do wallet (VOs/Enum/Errors/Events/
// Aggregate/Load/Commands/UseCases), fechando o ciclo transacional do
// Marco E pela primeira vez.
func usecaseSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := walletAggregateLoadSmokeFiles(t)

	prog := parseWalletProgram(t)
	model := types.NewModel(prog.Symbols)

	cmdDecls := parseWalletCommands(t)
	cmdsGo, err := codegen.EmitCommands("wallet", cmdDecls, model, prog.Symbols, "Wallet")
	if err != nil {
		t.Fatalf("EmitCommands: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "commands.go")] = cmdsGo

	files[filepath.Join("wallet", "usecases.go")] = emitWalletUseCases(t)
	return files
}

// TestEmitUseCasesSmokeCompile prova NFR-14: o Go gerado dos 2 UseCases,
// junto de todo o restante do módulo wallet (VOs/Enum/Errors/Events/
// Aggregate/Load/Commands) e do runtime vendorado real, compila e passa go
// vet num projeto isolado — o critério de conclusão da task ("compila").
func TestEmitUseCasesSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, usecaseSmokeFiles(t))
}

// walletUseCaseBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado (não uma reimplementação), o CAMINHO DE
// FALHA de PerformDeposit — orquestração completa: uow.Run abre a
// transação, LoadWallet reconstrói (stream vazio ⇒ Wallet zero-value),
// wallet.Deposit despacha o Handle (access OK, caller autenticado como
// "W1"), o Handle falha em "ensure state.active == ActiveStatus(true) else
// InactiveWallet" ANTES de emitir — então PerformDeposit devolve
// ErrInactiveWallet e tx.Append nunca roda (confirmado: o stream de W1
// continua vazio depois da chamada).
//
// Achado documentado (gap do domínio de exemplo, não bug desta task — mesmo
// espírito da nota de Currency em decl_aggregate_load_test.go): NÃO existe,
// no wallet real, nenhum Apply que ligue state.active (nenhum "Apply
// WalletCreated" está declarado no Aggregate, apesar do Event WalletCreated
// existir) — LoadWallet SEMPRE reconstrói a partir de Active=zero-value
// (false), então o CAMINHO DE SUCESSO de PerformDeposit contra um wallet
// carregado por replay é estruturalmente inalcançável sobre o domínio real
// (qualquer sequência de DepositPerformed/WithdrawalPerformed jamais liga
// Active, porque nenhum dos dois Applies o toca). O caminho de sucesso do
// dispatch de Handle dentro de uma unit of work de verdade É testado — mas
// sobre uma fixture SINTÉTICA sem essa lacuna (TestEmitUseCasePerformIncrement
// abaixo), o mesmo padrão que decl_aggregate_load_test.go usou para
// StateStored/snapshot.
const walletUseCaseBehaviorTest = `package wallet

import (
	"context"
	"errors"
	"testing"

	"domainscript/generated/runtime"
)

type ucStubCaller struct {
	authenticated bool
	id            string
}

func (c ucStubCaller) Authenticated() bool      { return c.authenticated }
func (c ucStubCaller) ID() string                { return c.id }
func (c ucStubCaller) HasRole(role string) bool { return false }

func TestPerformDepositOnZeroValueWalletFailsInactiveAndAppendsNothing(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow = runtime.NewUnitOfWork(store)

	amount, err := NewMoney(mustDecimalUC(t, "10.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}
	cmd := Deposit{WalletId: WalletId("W1"), Amount: amount, Description: desc}

	ctx := runtime.WithCaller(context.Background(), ucStubCaller{authenticated: true, id: "W1"})

	err = PerformDeposit(ctx, cmd)
	if !errors.Is(err, ErrInactiveWallet) {
		t.Fatalf("esperava ErrInactiveWallet (gap documentado: nenhum Apply liga state.active), got %v", err)
	}

	events, loadErr := store.Load(context.Background(), "W1")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(events) != 0 {
		t.Fatalf("esperava 0 eventos persistidos (Handle falhou antes de emit; tx.Append nunca roda), got %d", len(events))
	}
}

func mustDecimalUC(t *testing.T, s string) runtime.Decimal {
	t.Helper()
	d, err := runtime.ParseDecimal(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
`

// TestEmitUseCasesBehaviorInactiveWalletPath prova NFR-15 sobre o caminho de
// falha do PerformDeposit real (ver a doc de walletUseCaseBehaviorTest).
func TestEmitUseCasesBehaviorInactiveWalletPath(t *testing.T) {
	files := usecaseSmokeFiles(t)
	files[filepath.Join("wallet", "usecases_behavior_test.go")] = []byte(walletUseCaseBehaviorTest)
	runGeneratedTests(t, files)
}

// --- Parte 2: fixture sintética — o CAMINHO DE SUCESSO completo. ---
//
// meterFixtureSrc declara um Aggregate cujo único Handle NÃO tem nenhuma
// precondição (ensure) — ao contrário de Wallet.Deposit, que exige
// state.active — para provar o caminho de sucesso completo do UseCase gerado
// (uow.Run abre, LoadMeter reconstrói do stream vazio, o dispatch de Handle
// sucede e emite, tx.Append persiste), sem esbarrar no gap documentado acima.

const meterFixtureSrc = `
ValueObject MeterId(string) {
    Valid { value.length() > 0 }
}

ValueObject Reading(integer) {
    Valid { ok }
}

Event MeterRead {
    id MeterId
    value Reading
}

Aggregate Meter {
    strategy EventSourced

    state {
        id MeterId
        value Reading
    }

    access {
        Record requires caller.authenticated
    }

    Handle Record(value Reading) {
        emit MeterRead(self.id, value)
    }

    Apply MeterRead {
        state.value = event.value
    }
}

Command RecordReading {
    meterId ref Meter
    value Reading
}
`

const meterFixtureModDs = `Module Meter {
    Database MeterDb {
        provider: "postgres"
        manages: [Meter]
    }
}
`

// parseMeterFixture monta o projeto sintético em disco e o resolve via
// driver.CheckProject — devolve o Program, o AggregateDecl e o CommandDecl.
// Nenhum UseCase é declarado no .ds (ver a doc do arquivo: qualquer corpo de
// UseCase real bateria na mesma ambiguidade de gramática do wallet) — o
// UseCaseDecl é montado à mão logo abaixo, sobre o Model/SymbolTable deste
// programa (writeProjectDir é de decl_aggregate_load_test.go, mesmo pacote).
func parseMeterFixture(t *testing.T) (*program.Program, *ast.AggregateDecl, *ast.CommandDecl) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    meterFixtureModDs,
		"domain.ds": meterFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de UseCase (E7.2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	agg := findAggregateDecl(t, prog, "Meter")
	var cmd *ast.CommandDecl
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if c, ok := d.(*ast.CommandDecl); ok && c.Name == "RecordReading" {
				cmd = c
			}
		}
	}
	if cmd == nil {
		t.Fatal("Command RecordReading não encontrado na fixture sintética")
	}
	return prog, agg, cmd
}

// meterUseCaseDecl monta à mão o UseCase PerformRecordReading — Timeout nil
// (item 1 da task: "se Timeout == nil, pule essa parte"), Handles
// "RecordReading" (o Command).
func meterUseCaseDecl() *ast.UseCaseDecl {
	execute := handDispatchExecuteBlock("meter", "Meter", "meterId", "Record", "value")
	return ast.NewUseCaseDecl("PerformRecordReading", "RecordReading", nil, nil, "", execute, ast.Span{})
}

// meterSmokeFiles monta o conjunto completo de arquivos da fixture Meter:
// go.mod, runtime real, os VOs/Events/Aggregate/Load/Command/UseCase.
func meterSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog, agg, cmdDecl := parseMeterFixture(t)
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

	meterID := findValueObjectDecl(t, prog, "MeterId")
	reading := findValueObjectDecl(t, prog, "Reading")
	for _, spec := range []struct {
		decl *ast.ValueObjectDecl
		file string
	}{
		{meterID, "meter_id.go"},
		{reading, "reading.go"},
	} {
		got, err := codegen.EmitValueObject("meter", spec.decl)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", spec.decl.Name, err)
		}
		files[filepath.Join("meter", spec.file)] = got
	}

	eventsGo, err := codegen.EmitEvents("meter", []*ast.EventDecl{findEventDecl(t, prog, "MeterRead")})
	if err != nil {
		t.Fatalf("EmitEvents: erro inesperado: %v", err)
	}
	files[filepath.Join("meter", "events.go")] = eventsGo

	aggGo, err := codegen.EmitAggregate("meter", agg, model, prog.Symbols, "Meter", reg)
	if err != nil {
		t.Fatalf("EmitAggregate: erro inesperado: %v", err)
	}
	files[filepath.Join("meter", "aggregate_meter.go")] = aggGo

	loadGo, err := codegen.EmitAggregateLoad("meter", agg, model, prog.Symbols, "Meter")
	if err != nil {
		t.Fatalf("EmitAggregateLoad: erro inesperado: %v", err)
	}
	files[filepath.Join("meter", "aggregate_meter_load.go")] = loadGo

	cmdGo, err := codegen.EmitCommand("meter", cmdDecl, model, prog.Symbols, "Meter")
	if err != nil {
		t.Fatalf("EmitCommand: erro inesperado: %v", err)
	}
	files[filepath.Join("meter", "commands.go")] = cmdGo

	aggregates := map[string]*ast.AggregateDecl{"Meter": agg}
	ucGo, err := codegen.EmitUseCase("meter", meterUseCaseDecl(), aggregates, model, prog.Symbols, "Meter", reg, nil)
	if err != nil {
		t.Fatalf("EmitUseCase(PerformRecordReading): erro inesperado: %v", err)
	}
	files[filepath.Join("meter", "usecases.go")] = ucGo

	return files
}

// TestEmitUseCaseMeterFixtureNoTimeoutCompiles prova o item 1 da task
// (Timeout == nil pula context.WithTimeout) e é o smoke compile da fixture
// sintética (NFR-14).
func TestEmitUseCaseMeterFixtureNoTimeoutCompiles(t *testing.T) {
	files := meterSmokeFiles(t)
	if strings.Contains(string(files[filepath.Join("meter", "usecases.go")]), "WithTimeout") {
		t.Fatalf("esperava NENHUM context.WithTimeout (Timeout == nil), achei:\n%s", files[filepath.Join("meter", "usecases.go")])
	}
	gentest.SmokeCompile(t, files)
}

// meterUseCaseBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado, o CAMINHO DE SUCESSO completo: uow.Run
// abre a transação, LoadMeter reconstrói (stream vazio ⇒ Meter zero-value —
// sem nenhuma precondição de estado, ao contrário de Wallet), o dispatch de
// Handle sucede e emite MeterRead, tx.Append persiste — confirmado lendo o
// EventStore diretamente depois da chamada (o "confirme que o evento foi de
// fato gravado no store" pedido pela task).
const meterUseCaseBehaviorTest = `package meter

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

type meterStubCaller struct{ authenticated bool }

func (c meterStubCaller) Authenticated() bool      { return c.authenticated }
func (c meterStubCaller) ID() string                { return "m1" }
func (c meterStubCaller) HasRole(role string) bool { return false }

func TestPerformRecordReadingSucceedsAndPersistsEvent(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	uow = runtime.NewUnitOfWork(store)

	ctx := runtime.WithCaller(context.Background(), meterStubCaller{authenticated: true})
	cmd := RecordReading{MeterId: MeterId("m1"), Value: Reading(42)}

	if err := PerformRecordReading(ctx, cmd); err != nil {
		t.Fatalf("PerformRecordReading: erro inesperado: %v", err)
	}

	events, err := store.Load(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento persistido (MeterRead), got %d", len(events))
	}
	ev, ok := events[0].(*MeterRead)
	if !ok {
		t.Fatalf("esperava *MeterRead, got %T", events[0])
	}
	if ev.Value != Reading(42) {
		t.Fatalf("Value incorreto: got %v, want %v", ev.Value, Reading(42))
	}
	if ev.Id != MeterId("m1") {
		t.Fatalf("Id incorreto: got %v, want m1", ev.Id)
	}
}
`

// TestEmitUseCasePerformRecordReadingBehavior prova NFR-15: o caminho de
// sucesso completo (uow.Run + LoadX + dispatch de Handle + tx.Append) sobre
// o Go de fato gerado.
func TestEmitUseCasePerformRecordReadingBehavior(t *testing.T) {
	files := meterSmokeFiles(t)
	files[filepath.Join("meter", "usecases_behavior_test.go")] = []byte(meterUseCaseBehaviorTest)
	runGeneratedTests(t, files)
}

// --- Testes defensivos. ---

// TestEmitUseCaseUnknownAggregateFallsThroughToBuiltinError prova que, se o
// receptor de uma chamada em ExprStmt não resolve a um Aggregate CONHECIDO
// (não está no mapa "aggregates" passado ao emissor), o gerador NÃO tenta um
// dispatch de Handle — cai no caminho de método embutido normal (E5.2/E5.3)
// e, sem um método embutido conhecido para o tipo, falha com um erro de
// geração claro (nunca panic, REQ-14.4).
func TestEmitUseCaseUnknownAggregateFallsThroughToBuiltinError(t *testing.T) {
	prog := parseWalletProgram(t)
	model := types.NewModel(prog.Symbols)

	// aggregates vazio: "Wallet" não é reconhecido, então
	// "wallet.Deposit(...)" não é dispatch de Handle nem método embutido.
	execute := handDispatchExecuteBlock("wallet", "Wallet", "walletId", "Deposit", "amount", "description")
	uc := ast.NewUseCaseDecl("PerformDeposit", "Deposit", nil, nil, "", execute, ast.Span{})

	if _, err := codegen.EmitUseCase("wallet", uc, map[string]*ast.AggregateDecl{}, model, prog.Symbols, "Wallet", goname.NewVOOperatorRegistry(), nil); err == nil {
		t.Fatal("esperava erro de geração: Aggregate desconhecido não deveria virar dispatch de Handle nem método embutido")
	}
}

// runGeneratedTests já está definido em decl_aggregate_load_test.go (mesmo
// pacote codegen_test) — reusado aqui.
