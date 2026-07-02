package lower

import (
	"os"
	"path/filepath"
	"testing"

	"domainscript/ast"
	"domainscript/diag"
	"domainscript/program"
	"domainscript/sema"
	"domainscript/types"
)

// env_test.go prova os critérios de conclusão da task E5.0 (§design codegen
// 3.6a) sobre o domain.ds real do wallet (docs/examples/wallet) — não ASTs
// puramente sintéticas, seguindo a mesma convenção de
// codegen/decl_value_test.go: a fixture é o programa de verdade.

// walletExampleDir é o exemplo de referência empacotado no repositório,
// relativo a este pacote (codegen/lower).
var walletExampleDir = filepath.Join("..", "..", "docs", "examples", "wallet")

// buildWalletEnv carrega o projeto wallet completo (todos os .ds do
// diretório), monta o types.Model sobre a SymbolTable resolvida e devolve o
// Program (para achar declarações concretas) junto com um TypeEnv raiz para o
// módulo Wallet.
//
// Monta o programa via program.Build + sema.CheckProgram diretamente (em vez
// de driver.CheckProject) DE PROPÓSITO: driver importa codegen (driver/
// generate.go, GenerateProject), e codegen importa codegen/lower (E6.1+) — se
// os testes DESTE pacote (internos, "package lower") importassem driver,
// formaria um ciclo de import só ao compilar o binário de teste (lower_test
// precisaria de driver, que precisa de codegen, que precisa de lower de
// volta). program e sema não importam codegen, então este caminho fica livre
// do ciclo — mesma direção de dependência "para baixo" de §design 2.
func buildWalletEnv(t *testing.T) (*program.Program, *TypeEnv) {
	t.Helper()
	bag := diag.New()
	prog, err := program.Build(walletExampleDir, bag)
	if err != nil {
		t.Fatalf("program.Build(%q): %v", walletExampleDir, err)
	}
	sema.CheckProgram(prog, bag)
	if bag.HasErrors() {
		t.Fatalf("wallet não deveria ter diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)
	env := New(model, prog.Symbols, "Wallet")
	return prog, env
}

// findAggregate acha o *ast.AggregateDecl de nome name em qualquer arquivo do
// programa.
func findAggregate(t *testing.T, prog *program.Program, name string) *ast.AggregateDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if agg, ok := d.(*ast.AggregateDecl); ok && agg.Name == name {
				return agg
			}
		}
	}
	t.Fatalf("Aggregate %q não encontrado no wallet — o exemplo mudou?", name)
	return nil
}

// findUseCase acha o *ast.UseCaseDecl de nome name em qualquer arquivo do
// programa.
func findUseCase(t *testing.T, prog *program.Program, name string) *ast.UseCaseDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if uc, ok := d.(*ast.UseCaseDecl); ok && uc.Name == name {
				return uc
			}
		}
	}
	t.Fatalf("UseCase %q não encontrado no wallet — o exemplo mudou?", name)
	return nil
}

// findHandle acha o *ast.HandleDecl de nome name dentro de um Aggregate.
func findHandle(t *testing.T, agg *ast.AggregateDecl, name string) *ast.HandleDecl {
	t.Helper()
	for _, h := range agg.Handlers {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("Handle %q não encontrado em %s — o exemplo mudou?", name, agg.Name)
	return nil
}

// shapeName devolve o nome de t se for um *types.ShapeType, senão "".
func shapeName(t types.Type) string {
	if s, ok := t.(*types.ShapeType); ok {
		return s.Name
	}
	return ""
}

// --- Critério 1: wallet = load Wallet(id) ⇒ tipo Wallet (o Aggregate). ---

// TestInferAssignRHS_LoadFromRealUseCase monta à mão a QueryExpr do RHS de
// "wallet = load Wallet(cmd.walletId)" — a forma autorizada pela task quando
// o statement de verdade não isola de forma limpa a QueryExpr (o parser atual
// dá ao `load` um binding textual opcional, igual a `list T t`; sem um
// separador de statement baseado em token, o `wallet` que abre a PRÓXIMA linha
// de PerformDeposit.execute — "wallet.Deposit(...)" — é consumido como esse
// binding, e a chamada `.Deposit(...)` acaba encadeada por cima da própria
// QueryExpr. É uma ambiguidade pré-existente do parser, fora do escopo desta
// task; o teste evita depender dela e prova InferAssignRHS sobre o Model/
// SymbolTable REAIS do wallet, exatamente como pedido pela task ("ou só
// chamando InferAssignRHS direto sobre um QueryExpr montado à mão").
func TestInferAssignRHS_LoadFromRealUseCase(t *testing.T) {
	prog, env := buildWalletEnv(t)
	uc := findUseCase(t, prog, "PerformDeposit")
	env.SeedUseCaseExecute(uc.Handles) // cmd = Deposit (tipo real do Command)

	walletIdArg := ast.NewMemberExpr(ast.NewIdent("cmd", ast.Span{}), "walletId", ast.Span{}.Start, ast.Span{})
	target := ast.NewCallExpr(ast.NewIdent("Wallet", ast.Span{}), []ast.Arg{{Value: walletIdArg}}, ast.Span{})
	qe := ast.NewQueryExpr("load", target, "", nil, ast.Span{})

	got, err := env.InferAssignRHS(qe)
	if err != nil {
		t.Fatalf("InferAssignRHS não deveria falhar: %v", err)
	}
	if name := shapeName(got); name != "Wallet" {
		t.Fatalf("esperava tipo Wallet (ShapeType), obtive %s (%T)", got.String(), got)
	}
}

// --- Critério 2: for e in state.entries ⇒ e resolve para StatementEntry
// (state.entries é AppendList<StatementEntry> no Wallet real). ---

func TestChildForIter_StateEntriesFromRealAggregate(t *testing.T) {
	prog, env := buildWalletEnv(t)
	agg := findAggregate(t, prog, "Wallet")

	// Semeia o escopo raiz igual a um Apply real (state = tipo do Aggregate).
	apply := agg.Appliers[0] // Apply DepositPerformed
	env.SeedApply(agg.Name, apply.Event)

	// state.entries — o campo real do state do Wallet (AppendList<StatementEntry>).
	stateEntries := ast.NewMemberExpr(ast.NewIdent("state", ast.Span{}), "entries", ast.Span{}.Start, ast.Span{})
	iterType, err := env.InferAssignRHS(stateEntries)
	if err != nil {
		t.Fatalf("inferir state.entries não deveria falhar: %v", err)
	}
	g, ok := iterType.(*types.Generic)
	if !ok || g.Ctor != "AppendList" {
		t.Fatalf("esperava state.entries: AppendList<...>, obtive %s (%T)", iterType.String(), iterType)
	}

	child := env.ChildForIter("e", iterType)
	elem, ok := child.LookupType("e")
	if !ok {
		t.Fatal("esperava \"e\" vinculado no filho de ChildForIter")
	}
	if name := shapeName(elem); name != "StatementEntry" {
		// StatementEntry é um VO composto, não um Aggregate — confere via VOType.
		if vo, ok := elem.(*types.VOType); !ok || vo.Name != "StatementEntry" {
			t.Fatalf("esperava \"e\": StatementEntry, obtive %s (%T)", elem.String(), elem)
		}
	}
}

// --- Critério 3: x = count … ⇒ integer. ---

func TestInferAssignRHS_Count(t *testing.T) {
	env := New(types.NewModel(nil), nil, "Wallet")
	qe := ast.NewQueryExpr("count", ast.NewIdent("Wallet", ast.Span{}), "", nil, ast.Span{})

	got, err := env.InferAssignRHS(qe)
	if err != nil {
		t.Fatalf("InferAssignRHS(count) não deveria falhar: %v", err)
	}
	p, ok := got.(*types.Primitive)
	if !ok || p.Name != "integer" {
		t.Fatalf("esperava integer, obtive %s (%T)", got.String(), got)
	}
}

// --- Critério 4: um nó realmente desconhecido ⇒ erro explícito, nunca um
// tipo arbitrário. ---

func TestInferAssignRHS_UnknownQueryOpFailsExplicitly(t *testing.T) {
	env := New(types.NewModel(nil), nil, "Wallet")
	qe := ast.NewQueryExpr("bogus", ast.NewIdent("Wallet", ast.Span{}), "", nil, ast.Span{})

	got, err := env.InferAssignRHS(qe)
	if err == nil {
		t.Fatalf("esperava erro explícito para QueryExpr.Op desconhecido, obtive tipo %v", got)
	}
	if got != nil {
		t.Fatalf("no caminho de erro, o tipo devolvido deveria ser nil, obtive %v", got)
	}
}

// TestInferAssignRHS_EmptyMatchFailsExplicitly cobre o outro caso de "nó
// realmente desconhecido" citado pela task: um MatchExpr sem braços.
func TestInferAssignRHS_EmptyMatchFailsExplicitly(t *testing.T) {
	env := New(types.NewModel(nil), nil, "Wallet")
	me := ast.NewMatchExpr(ast.NewIdent("x", ast.Span{}), nil, ast.Span{})

	got, err := env.InferAssignRHS(me)
	if err == nil {
		t.Fatalf("esperava erro explícito para MatchExpr sem braços, obtive tipo %v", got)
	}
	if got != nil {
		t.Fatalf("no caminho de erro, o tipo devolvido deveria ser nil, obtive %v", got)
	}
}

// --- Critério 5: Bind/Child/LookupType — um filho enxerga o pai, o pai não
// enxerga o filho (escopo léxico normal). ---

func TestBindChildLookupTypeLexicalScope(t *testing.T) {
	root := New(types.NewModel(nil), nil, "Wallet")
	intT := &types.Primitive{Name: "integer"}
	strT := &types.Primitive{Name: "string"}

	root.Bind("x", intT)
	child := root.Child()
	child.Bind("y", strT)

	// O filho enxerga o próprio "y" e o "x" herdado do pai.
	if got, ok := child.LookupType("y"); !ok || got != strT {
		t.Fatalf("esperava que o filho enxergasse \"y\" == string, obtive %v, %v", got, ok)
	}
	if got, ok := child.LookupType("x"); !ok || got != intT {
		t.Fatalf("esperava que o filho enxergasse \"x\" herdado do pai == integer, obtive %v, %v", got, ok)
	}

	// O pai NÃO enxerga "y" (declarado só no filho).
	if got, ok := root.LookupType("y"); ok {
		t.Fatalf("esperava que o pai não enxergasse \"y\" do filho, obtive %v", got)
	}
	// Um nome desconhecido em ambos falha em ambos.
	if _, ok := root.LookupType("z"); ok {
		t.Fatal("esperava LookupType(\"z\") == false no pai")
	}
	if _, ok := child.LookupType("z"); ok {
		t.Fatal("esperava LookupType(\"z\") == false no filho")
	}
}

// --- Critério 6: Handle/Access reais do wallet — self/state resolvem pro
// tipo do Aggregate Wallet; um parâmetro de Handle Deposit(amount Money, ...)
// resolve pro tipo Money. ---

func TestSeedHandle_RealWalletDeposit(t *testing.T) {
	prog, env := buildWalletEnv(t)
	agg := findAggregate(t, prog, "Wallet")
	deposit := findHandle(t, agg, "Deposit")

	env.SeedHandle(agg.Name, deposit.Params)

	selfT, ok := env.LookupType("self")
	if !ok || shapeName(selfT) != "Wallet" {
		t.Fatalf("esperava self: Wallet, obtive %v, ok=%v", selfT, ok)
	}
	stateT, ok := env.LookupType("state")
	if !ok || shapeName(stateT) != "Wallet" {
		t.Fatalf("esperava state: Wallet, obtive %v, ok=%v", stateT, ok)
	}
	if selfT != stateT {
		t.Fatalf("esperava self e state com o MESMO tipo (mesmo shape do Aggregate), obtive %v != %v", selfT, stateT)
	}

	amountT, ok := env.LookupType("amount")
	if !ok {
		t.Fatal("esperava o parâmetro \"amount\" de Handle Deposit vinculado")
	}
	vo, ok := amountT.(*types.VOType)
	if !ok || vo.Name != "Money" {
		t.Fatalf("esperava amount: Money, obtive %v (%T)", amountT, amountT)
	}

	// caller nunca é tipado (decisão documentada em env.go): não deve estar no
	// escopo, mesmo sendo um receptor válido de Handle em
	// resolver/receivers.go.
	if _, ok := env.LookupType("caller"); ok {
		t.Fatal("esperava que \"caller\" NÃO fosse semeado com um tipo (decisão do front-end/design §3.6a)")
	}
}

func TestSeedAccess_RealWalletAggregate(t *testing.T) {
	prog, env := buildWalletEnv(t)
	agg := findAggregate(t, prog, "Wallet")

	env.SeedAccess(agg.Name)

	selfT, ok := env.LookupType("self")
	if !ok || shapeName(selfT) != "Wallet" {
		t.Fatalf("esperava self: Wallet, obtive %v, ok=%v", selfT, ok)
	}
	if _, ok := env.LookupType("caller"); ok {
		t.Fatal("esperava que \"caller\" NÃO fosse semeado com um tipo em Access")
	}
}

// TestNew_SanityOverRealProject só confirma que New não exige um tab não-nil
// para o caminho degenerado usado pelos testes acima com Model(nil) — smoke
// test de que a fixture do wallet realmente existe no caminho esperado (evita
// um t.Fatal enganoso nos outros testes se o diretório mudar de lugar).
func TestNew_SanityOverRealProject(t *testing.T) {
	if _, err := os.Stat(filepath.Join(walletExampleDir, "domain.ds")); err != nil {
		t.Fatalf("docs/examples/wallet/domain.ds não encontrado a partir de codegen/lower: %v", err)
	}
}
