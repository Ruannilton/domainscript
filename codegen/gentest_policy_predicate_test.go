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

// gentest_policy_predicate_test.go prova o critério de conclusão de I1.1
// (§design read-side 3.3, REQ-36, fecha G-8) que gentest_policy_test.go NÃO
// cobre: aquela fixture (Refunds) só exercita "where" com uma comparação
// NATIVA (campos wrapper/primitivos, "t.eventId == event.id") — o próprio
// caso que hoistQueryPredicate JÁ suportava antes desta task. O que faltava
// provar de ponta a ponta (Go GERADO e EXECUTADO de verdade, não só o texto
// que TestStmt_List_Where_NeedsHoistingFailsExplicitly, em
// codegen/lower/builtins_test.go, agora prova no nível de unidade) é: (1)
// uma condição de "where" que precisa HOISTEAR uma construção de VO
// composto E um operador de VO falível compila e roda; (2) quando esse
// hoisting FALHA em tempo de execução para um item específico (aqui, um
// Money com moeda diferente da comparada — Operator >= de Money "ensure
// currency == other.currency else CurrencyMismatch", mesma regra do Money
// do wallet real, docs/examples/wallet/domain.ds), o Select/Count inteiro
// aborta com ESSE erro (REQ-36.1) — nunca um item silenciosamente pulado.
//
// Fixture sintética NOVA (nem wallet, nem shop, nem Refunds): PriceCheck.
// Item.price é um Money (VO composto, campo de outro VO composto, mesmo
// formato de StatementEntry.amount no wallet), e FlagExpensiveItems filtra
// itens cujo preço é >= um limiar construído INLINE no "where" ("Money(
// amount: 10, currency: \"BRL\")" — a construção de VO composto com args
// nomeados que needsHoistVOConstruct sempre marca como precisando de
// hoisting).
//
// --- Por que esta fixture NÃO tem um bloco "Test" (§22.4) ---
//
// Um "Test PolicyName { given items [ Item(...) { price: Money(...) } ] ...
// }" (a forma de gentest_policy_test.go) semeia o Collection[T] via
// emitPolicyGivenEntity (gentest.go), que hoisteia CADA campo do overlay
// (ExprHoisted) diretamente no corpo do func de cenário — igual a
// hoistQueryPredicate, mas SEM o "var err error" que emitUseCaseScenarioBody
// já usa antes de "err = %s(ctx, %s)" (ver o comentário ali: "um arg hoisted
// já pode ter declarado err via tmpN, err := ... — ':=' com 'err' sozinho a
// esquerda falharia"). emitPolicyScenarioBody NUNCA precisou desse cuidado
// até hoje porque nenhuma fixture de Policy dava um campo de VO COMPOSTO
// (que sempre hoisteia) num "given" — a única de H4, Refunds, só usa VOs
// wrapper de 1 campo (EventId/OrderId), que nunca hoisteiam. Uma fixture com
// "given items [ Item(...) { price: Money(...) } ]" REVELA esse gap
// pré-existente (não relacionado a hoistQueryPredicate — mora em
// emitPolicyScenarioBody, gentest.go): "err := <Policy>(ctx, &ev)" sempre
// usa ":=", que falha ("no new variables on left side of :=") quando o
// "given" já declarou "err" via hoisting. Consertar esse gap é FORA do
// escopo de I1.1 (mora numa camada diferente — a emissão de *.test.ds, não
// o hoisting de "where"); em vez de arriscar uma mudança mais ampla em
// gentest.go só para provar esta task, a prova comportamental abaixo
// (TestPolicyPredicateHandwrittenRunGreen) semeia itemCollection e chama
// FlagExpensiveItems a partir de um _test.go ESCRITO À MÃO, injetado no
// projeto GERADO de verdade antes de "go test" — o código sob teste (Money,
// NewMoney, Item, FlagExpensiveItems, itemCollection, ErrCurrencyMismatch)
// é 100% gerado a partir do DomainScript acima; só o HARNESS que o aciona
// não passa pela máquina de *.test.ds, evitando o gap sem mascará-lo (ele
// fica documentado aqui para quem for atacar I3+/uma limpeza futura de
// gentest.go).
const policyPredicateFixtureModDs = `Module PriceCheck { }
`

const policyPredicateFixtureSrc = `
ValueObject ItemId(string) {
    Valid { value.length() > 0 }
}

ValueObject Money {
    amount decimal
    currency string

    Valid { amount >= 0 }

    Operator >=(other Money) -> boolean {
        ensure currency == other.currency else CurrencyMismatch
        return amount >= other.amount
    }
}

Error CurrencyMismatch { message "As moedas comparadas não coincidem." }

Enum StockStatus : string {
    Available = "AVAILABLE"
}

ValueObject Item {
    id ItemId
    status StockStatus
    price Money
}

Event StockChecked {
    checkedBy ItemId
}

Event ExpensiveItemFound {
    id ItemId
}

Policy FlagExpensiveItems on StockChecked {
    delivery BestEffort
    execute {
        matches = list Item i where i.price >= Money(amount: 10, currency: "BRL")
        for m in matches {
            emit ExpensiveItemFound(id: m.id)
        }
    }
}
`

// generatePolicyPredicateFixtureProject roda o orquestrador COMPLETO
// (codegen.Generate) sobre o Program da fixture acima — mesmo padrão de
// generatePolicyTestFixtureProject (gentest_policy_test.go): precisamos do
// projeto INTEIRO (não só o arquivo da Policy) porque o teste comportamental
// abaixo aciona FlagExpensiveItems de verdade (pricecheck.go, incl. o var de
// pacote itemCollection e policyDispatcher que decl_policy.go declara).
func generatePolicyPredicateFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    policyPredicateFixtureModDs,
		"domain.ds": policyPredicateFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de predicado falível (I1.1, G-8) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de predicado falível: %v", err)
	}
	return files
}

// pricecheckPolicyGoPath localiza, entre os arquivos gerados, o que carrega
// a Policy FlagExpensiveItems em si (decl_policy.go emite um arquivo por
// módulo, pkg = goname.PackageName("PriceCheck") = "pricecheck").
func pricecheckPolicyFile(t *testing.T, files map[string][]byte) (path string, content []byte) {
	t.Helper()
	for p, c := range files {
		if strings.HasPrefix(p, "pricecheck/") && strings.HasSuffix(p, "policies.go") {
			return p, c
		}
	}
	var ks []string
	for k := range files {
		ks = append(ks, k)
	}
	t.Fatalf("esperava um arquivo \"pricecheck/*policies.go\" entre os arquivos gerados, não achei; arquivos: %v", ks)
	return "", nil
}

func emitPolicyPredicateBodyFixture(t *testing.T) []byte {
	t.Helper()
	_, content := pricecheckPolicyFile(t, filesToMap(generatePolicyPredicateFixtureProject(t)))
	return content
}

// TestEmitPolicyPredicateBodyGolden prova a forma EXATA do predicado em
// bloco dentro do corpo da Policy (a lacuna real que I1.1 fecha, G-8): a
// temporária hoisted da construção de Money, seu "if err != nil { return
// false, err }", o dispatch do Operator >= (também hoisted, com seu próprio
// "if err != nil { return false, err }"), e o "return <cond>, nil" final —
// tudo dentro de "func(i Item) (bool, error) { ... }", sem nenhum
// "runtime.Infallible(...)" ao redor (a ponte de I0.1, removida por esta
// task — ver a doc de queryLiteral, codegen/lower/builtins.go).
func TestEmitPolicyPredicateBodyGolden(t *testing.T) {
	got := string(emitPolicyPredicateBodyFixture(t))
	for _, want := range []string{
		"itemCollection.Select(ctx, runtime.Query[Item]{Where: func(i Item) (bool, error) {",
		"if err != nil {",
		"return false, err",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q no corpo gerado de FlagExpensiveItems (predicado em bloco, I1.1), não achei:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Infallible") {
		t.Errorf("NÃO esperava \"Infallible\" no corpo gerado — a ponte de I0.1 devia ter sido removida por esta task, got:\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "policy_pricecheck.go.golden"), []byte(got))
}

// TestEmitPolicyPredicateDeterministic prova NFR-13: regerar duas vezes
// produz bytes idênticos — mesma forma de TestEmitPolicyTestsDeterministic.
func TestEmitPolicyPredicateDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitPolicyPredicateBodyFixture(t)
	})
}

// TestEmitPolicyPredicateSmokeCompile prova NFR-14 sobre o projeto INTEIRO
// gerado: compila e passa go vet num diretório isolado — a prova de
// "compila" exigida pelo critério de conclusão de I1.1 (não só o texto).
func TestEmitPolicyPredicateSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generatePolicyPredicateFixtureProject(t)))
}

// policyPredicateHandwrittenTest é o harness ESCRITO À MÃO que aciona o
// código GERADO de verdade (ver a nota "Por que esta fixture NÃO tem um
// bloco Test" acima sobre por que não passa pela máquina de *.test.ds).
// Semeia itemCollection com 3 itens (dois BRL — um caro, um barato — e um
// USD) e prova as DUAS metades de REQ-36.1 na mesma chamada: um item cuja
// comparação de moeda falha (USD contra o limiar BRL do "where") aborta o
// Select INTEIRO com ErrCurrencyMismatch — mesmo o item caro em BRL, que
// passaria o filtro, nunca aparece no resultado (a prova de que não é um
// "item ruim pulado, resto ok", e sim a chamada inteira abortada). Um
// segundo cenário, sem o item USD, prova a metade "sucesso": o item caro
// passa, o barato é filtrado pelo bool do Operator >=.
const policyPredicateHandwrittenTest = `package pricecheck

import (
	"context"
	"errors"
	"testing"

	"domainscript/generated/runtime"
)

func seedItem(t *testing.T, id, status string, amount int64, currency string) Item {
	t.Helper()
	price, err := NewMoney(runtime.NewDecimalFromInt(amount), currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q): %v", amount, currency, err)
	}
	item := Item{Id: ItemId(id), Status: StockStatus(status), Price: price}
	return item
}

// TestPolicyPredicateHandwrittenRunGreen prova o critério de conclusão
// comportamental de I1.1 (REQ-36.1) sobre o Go GERADO de verdade: um "where"
// com hoisting real (construção de VO composto + operador de VO falível)
// filtra corretamente quando não há erro, e ABORTA A CHAMADA INTEIRA (não
// pula silenciosamente o item) quando o operador falível falha para um item.
func TestPolicyPredicateHandwrittenRunGreen(t *testing.T) {
	ctx := context.Background()

	t.Run("filtra por preco quando todas as moedas batem", func(t *testing.T) {
		itemCollection = runtime.NewMemoryCollection[Item]()
		expensive := seedItem(t, "I1", "AVAILABLE", 20, "BRL")
		cheap := seedItem(t, "I2", "AVAILABLE", 5, "BRL")
		if err := itemCollection.Add(ctx, expensive); err != nil {
			t.Fatalf("seed I1: %v", err)
		}
		if err := itemCollection.Add(ctx, cheap); err != nil {
			t.Fatalf("seed I2: %v", err)
		}

		var published []runtime.Event
		policyDispatcher = runtime.NewDispatcher()
		policyDispatcher.Subscribe("ExpensiveItemFound", func(ctx context.Context, ev runtime.Event) error {
			published = append(published, ev)
			return nil
		})

		ev := StockChecked{CheckedBy: ItemId("SYS")}
		if err := FlagExpensiveItems(ctx, &ev); err != nil {
			t.Fatalf("esperava sucesso, got: %v", err)
		}
		if len(published) != 1 {
			t.Fatalf("esperava 1 evento publicado (só o item caro), got %d: %+v", len(published), published)
		}
		want := &ExpensiveItemFound{Id: ItemId("I1")}
		got, ok := published[0].(*ExpensiveItemFound)
		if !ok || *got != *want {
			t.Fatalf("esperava %+v, got %+v", want, published[0])
		}
	})

	t.Run("moeda diferente aborta o Select inteiro com CurrencyMismatch", func(t *testing.T) {
		itemCollection = runtime.NewMemoryCollection[Item]()
		expensiveMatchingCurrency := seedItem(t, "I1", "AVAILABLE", 20, "BRL")
		mismatched := seedItem(t, "I3", "AVAILABLE", 20, "USD")
		if err := itemCollection.Add(ctx, expensiveMatchingCurrency); err != nil {
			t.Fatalf("seed I1: %v", err)
		}
		if err := itemCollection.Add(ctx, mismatched); err != nil {
			t.Fatalf("seed I3: %v", err)
		}

		var published []runtime.Event
		policyDispatcher = runtime.NewDispatcher()
		policyDispatcher.Subscribe("ExpensiveItemFound", func(ctx context.Context, ev runtime.Event) error {
			published = append(published, ev)
			return nil
		})

		ev := StockChecked{CheckedBy: ItemId("SYS")}
		err := FlagExpensiveItems(ctx, &ev)
		if !errors.Is(err, ErrCurrencyMismatch) {
			t.Fatalf("esperava errors.Is(err, ErrCurrencyMismatch), got: %v", err)
		}
		// A prova central de REQ-36.1: a chamada abortou ANTES de publicar
		// nada — nem o item I1 (que teria passado no filtro) foi emitido.
		// Um item cujo predicado falha NUNCA é "silenciosamente pulado com o
		// resto processado normalmente" — o erro propaga e descarta o
		// resultado parcial inteiro (ver SelectSlice, rtsrc/collection.go.txt).
		if len(published) != 0 {
			t.Fatalf("esperava 0 eventos publicados (Select deveria ter abortado ANTES de qualquer emit), got %d: %+v", len(published), published)
		}
	})
}
`

// TestPolicyPredicateHandwrittenRunGreen roda `go test ./...` de VERDADE
// sobre o projeto gerado, com policyPredicateHandwrittenTest injetado ao
// lado dos demais arquivos de pricecheck/ — ver a doc de
// policyPredicateHandwrittenTest sobre por que o harness é escrito à mão em
// vez de vir de um bloco "Test" (§22.4).
func TestPolicyPredicateHandwrittenRunGreen(t *testing.T) {
	files := filesToMap(generatePolicyPredicateFixtureProject(t))
	// "pricecheck/pricecheck_handwritten_test.go" — irmão de "pricecheck/
	// policies.go" (ver pricecheckPolicyFile); path.Join usa sempre "/" (ver a
	// nota de refundsTestGoPath/pricecheckTestGoPath em gentest_policy_test.go
	// sobre NUNCA usar filepath.Join aqui, que em Windows produziria "\\").
	files["pricecheck/pricecheck_handwritten_test.go"] = []byte(policyPredicateHandwrittenTest)
	runGeneratedTests(t, files)
}
