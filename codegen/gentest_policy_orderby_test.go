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

// gentest_policy_orderby_test.go prova, de PONTA A PONTA (não só no nível de
// unidade que codegen/lower/orderby_test.go já cobre), o critério de
// conclusão de I2.1 (§design read-side 3.1/3.2, REQ-33.1/33.3): uma Policy
// cujo "execute" contém "list T i orderBy i.k [dir] skip N take M" gera Go
// que compila E se comporta como o spec descreve — ordenação + paginação de
// verdade sobre dados reais, não só o texto do predicado/Less.
//
// Fixture sintética NOVA (nem wallet, nem shop, nem PriceCheck/Refunds):
// Ranking. Item.score é um VO wrapper sobre integer (Score) — a linha "VO
// wrapper sobre primitivo ordenável" da tabela de comparabilidade (§design
// read-side 3.2), a mais simples de semear com dados reais sem depender de
// nenhum Operator de VO composto. FindTopItems ordena por score DESCENDENTE
// e pagina "skip 1 take 1" — o 2º colocado, exercitando orderBy+skip+take
// juntos na mesma query, o pedido explícito do prompt da task.
const policyOrderByFixtureModDs = `Module Ranking { }
`

const policyOrderByFixtureSrc = `
ValueObject ItemId(string) {
    Valid { value.length() > 0 }
}

ValueObject Score(integer) {
    Valid { value >= 0 }
}

ValueObject Item {
    id ItemId
    score Score
}

Event RankRequested {
    requestedBy ItemId
}

Event TopItemFound {
    id ItemId
}

Policy FindTopItems on RankRequested {
    delivery BestEffort
    execute {
        top = list Item i orderBy i.score descending skip 1 take 1
        for m in top {
            emit TopItemFound(id: m.id)
        }
    }
}
`

// generatePolicyOrderByFixtureProject roda o orquestrador COMPLETO
// (codegen.Generate) sobre o Program da fixture Ranking — mesmo padrão de
// generatePolicyPredicateFixtureProject (gentest_policy_predicate_test.go):
// o teste comportamental abaixo aciona FindTopItems de verdade, então
// precisa do projeto INTEIRO (itemCollection/policyDispatcher inclusos).
func generatePolicyOrderByFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    policyOrderByFixtureModDs,
		"domain.ds": policyOrderByFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de orderBy/skip/take (I2.1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de orderBy/skip/take: %v", err)
	}
	return files
}

// rankingPolicyFile localiza, entre os arquivos gerados, o que carrega a
// Policy FindTopItems (decl_policy.go emite um arquivo por módulo, pkg =
// goname.PackageName("Ranking") = "ranking").
func rankingPolicyFile(t *testing.T, files map[string][]byte) (path string, content []byte) {
	t.Helper()
	for p, c := range files {
		if strings.HasPrefix(p, "ranking/") && strings.HasSuffix(p, "policies.go") {
			return p, c
		}
	}
	var ks []string
	for k := range files {
		ks = append(ks, k)
	}
	t.Fatalf("esperava um arquivo \"ranking/*policies.go\" entre os arquivos gerados, não achei; arquivos: %v", ks)
	return "", nil
}

func emitPolicyOrderByBodyFixture(t *testing.T) []byte {
	t.Helper()
	_, content := rankingPolicyFile(t, filesToMap(generatePolicyOrderByFixtureProject(t)))
	return content
}

// TestEmitPolicyOrderByBodyGolden prova a forma EXATA do Select gerado:
// Query[Item] completo (Less + OrderField + OrderDesc + Skip + Take), na
// convenção de closure de uma linha só (sem hoisting: Score é um wrapper
// ordenável nativamente, skip/take são literais inteiros simples).
func TestEmitPolicyOrderByBodyGolden(t *testing.T) {
	got := string(emitPolicyOrderByBodyFixture(t))
	want := `itemCollection.Select(ctx, runtime.Query[Item]{Less: func(a, b Item) (bool, error) { return b.Score < a.Score, nil }, OrderField: "score", OrderDesc: true, Skip: 1, Take: 1})`
	if !strings.Contains(got, want) {
		t.Errorf("esperava %q no corpo gerado de FindTopItems, não achei:\n%s", want, got)
	}
	gentest.Golden(t, filepath.Join("testdata", "policy_ranking_orderby.go.golden"), []byte(got))
}

// TestEmitPolicyOrderByDeterministic prova NFR-13: regerar duas vezes
// produz bytes idênticos.
func TestEmitPolicyOrderByDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitPolicyOrderByBodyFixture(t)
	})
}

// TestEmitPolicyOrderBySmokeCompile prova que o projeto INTEIRO gerado
// compila e passa go vet num diretório isolado (NFR-14/NFR-17).
func TestEmitPolicyOrderBySmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generatePolicyOrderByFixtureProject(t)))
}

// policyOrderByHandwrittenTest é o harness ESCRITO À MÃO que aciona o código
// GERADO de verdade — mesma razão documentada em
// gentest_policy_predicate_test.go (emitPolicyScenarioBody, gentest.go, não
// cobre "given" com hoisting de VO composto; aqui o motivo é mais simples:
// esta fixture nem tem um bloco "Test" — a prova pedida pela task é
// comportamental sobre orderBy/skip/take, não sobre a máquina de *.test.ds).
// Semeia itemCollection com 3 Items de scores 10/30/20 (ordem de inserção
// deliberadamente EMBARALHADA em relação ao score, para provar que Less
// ordena de verdade, não que a ordem de inserção "por acaso" já bate):
// ordenado DESCENDENTE por score dá [30, 20, 10]; "skip 1 take 1" mantém só
// o item de score 20 (o 2º colocado) — a prova de que orderBy+skip+take
// compõem corretamente (§design read-side 3.1, a ordem semântica fixa
// where→orderBy→skip→take).
const policyOrderByHandwrittenTest = `package ranking

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func seedRankItem(t *testing.T, id string, score int64) Item {
	t.Helper()
	return Item{Id: ItemId(id), Score: Score(score)}
}

// TestPolicyOrderByHandwrittenRunGreen prova o critério de conclusão
// comportamental de I2.1: orderBy descending + skip 1 take 1 sobre dados
// reais devolve exatamente o 2º colocado por score, nunca o 1º nem o 3º.
func TestPolicyOrderByHandwrittenRunGreen(t *testing.T) {
	ctx := context.Background()

	itemCollection = runtime.NewMemoryCollection[Item]()
	// Ordem de inserção deliberadamente diferente da ordem por score.
	for _, seed := range []struct {
		id    string
		score int64
	}{
		{"I1", 10},
		{"I2", 30},
		{"I3", 20},
	} {
		if err := itemCollection.Add(ctx, seedRankItem(t, seed.id, seed.score)); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}

	var published []runtime.Event
	policyDispatcher = runtime.NewDispatcher()
	policyDispatcher.Subscribe("TopItemFound", func(ctx context.Context, ev runtime.Event) error {
		published = append(published, ev)
		return nil
	})

	ev := RankRequested{RequestedBy: ItemId("SYS")}
	if err := FindTopItems(ctx, &ev); err != nil {
		t.Fatalf("esperava sucesso, got: %v", err)
	}
	if len(published) != 1 {
		t.Fatalf("esperava 1 evento publicado (skip 1 take 1 == exatamente 1 item), got %d: %+v", len(published), published)
	}
	want := &TopItemFound{Id: ItemId("I3")}
	got, ok := published[0].(*TopItemFound)
	if !ok || *got != *want {
		t.Fatalf("esperava %+v (I3, score 20, o 2º colocado por score descendente), got %+v", want, published[0])
	}
}
`

// TestPolicyOrderByHandwrittenRunGreen roda `go test ./...` de VERDADE
// sobre o projeto gerado, com policyOrderByHandwrittenTest injetado ao lado
// dos demais arquivos de ranking/ — path.Join usa sempre "/" (NUNCA
// filepath.Join, que em Windows produziria "\\" — mesma nota de
// pricecheckTestGoPath em gentest_policy_predicate_test.go).
func TestPolicyOrderByHandwrittenRunGreen(t *testing.T) {
	files := filesToMap(generatePolicyOrderByFixtureProject(t))
	files["ranking/ranking_handwritten_test.go"] = []byte(policyOrderByHandwrittenTest)
	runGeneratedTests(t, files)
}
