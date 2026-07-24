package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// gentest_thenstate_test.go prova L2.1 (REQ-53.1, ISSUE-6, §22.1): a forma
// "then state { ... }" — a asserção de estado de um Aggregate StateStored.
//
// Nota de escopo (a premissa original da task estava errada): antes desta
// task "then state { ... }" nem PARSEAVA ("esperava '[', 'error' ou '{' após
// then, encontrei IDENT") — a lacuna era de front-end (ast.ThenClause não
// tinha o campo State, parser.parseThen não tinha o caso "state"), não só de
// codegen. Estes testes cobrem a ponta do back-end; o par de parser está em
// parser/parse_testfile_test.go (TestTestThenState/
// TestTestThenFormsUnaffectedByState).
//
// --- Por que uma fixture sintética ---
//
// Nem o wallet nem o shop têm um Aggregate StateStored com *.test.ds (o
// wallet é EventSourced e usa "then [eventos]"/"then error"), e nenhum dos
// dois usa "then state" — mesmo precedente de cada fatia anterior de H4 que
// precisou de uma forma que os exemplos reais não exercitam (ver a doc de
// gentest_policy_test.go, `.claude/specs/codegen/design.md` §6 "fixtures de
// exemplo não são fonte de verdade").
//
// --- O que a fixture prova, exatamente ---
//
// O "given state" semeia count = 1; o "when Increment(amount: Count(5))"
// dispara o Handle, que só EMITE CounterIncremented — um Handle nunca muta o
// state por si só (Apply é um método separado, ver emitHandle/emitApply em
// decl_aggregate.go). É o replay dos eventos devolvidos (emitApplyDispatch,
// reusado de §22.5) que leva count de 1 a 5. Um "then state" que se limitasse
// a ler o receiver logo depois do Handle veria count == 1 e falharia — ou
// seja, este cenário é exatamente o que prova que o replay acontece.

const thenStateFixtureModDs = `Module Counter {
    Database CounterDb {
        provider: "pg"
        manages: [Counter]
    }
}
`

// thenStateFixtureSrc é o domínio comum aos dois cenários (par NFR-4). O
// "%s" é o corpo do "then state" — preenchido com o valor que BATE ou com o
// que DIVERGE.
const thenStateFixtureSrc = `
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

// O "when" de um cenário §22.1 nomeia um Handle, mas o front-end (REQ-5.14)
// exige que o nome resolva a um símbolo declarado — a convenção dos exemplos
// reais é um Command homônimo do Handle (ver docs/examples/wallet/
// application.ds).
Command Increment {
    amount Count
}

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

Test Counter {
    scenario "incremento leva o state ao valor esperado" {
        given state { id: EntityId("C1"), count: Count(1) }
        when Increment(amount: Count(5))
        then state { id: EntityId("C1"), count: %s }
    }
}
`

// counterTestGoPath é o caminho do arquivo de teste gerado para o módulo
// Counter — path.Join ("/") sempre, nunca filepath.Join (mesma nota de
// refundsTestGoPath, gentest_policy_test.go).
const counterTestGoPath = "counter/counter_test.go"

// generateThenStateProject gera o projeto inteiro da fixture com o valor
// esperado want no "then state".
func generateThenStateProject(t *testing.T, want string) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    thenStateFixtureModDs,
		"domain.ds": strings.Replace(thenStateFixtureSrc, "%s", want, 1),
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de \"then state\" (§22.1, L2.1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de \"then state\": %v", err)
	}
	return files
}

// emitThenStateFixture devolve só o counter/counter_test.go gerado.
func emitThenStateFixture(t *testing.T, want string) []byte {
	t.Helper()
	files := filesToMap(generateThenStateProject(t, want))
	got, ok := files[counterTestGoPath]
	if !ok {
		t.Fatalf("esperava %q entre os arquivos gerados, não achei", counterTestGoPath)
	}
	return got
}

// TestEmitThenStateGolden prova a FORMA do Go gerado: o replay dos eventos
// devolvidos pelo Handle (switch ev := ev.(type) / applyCounterIncremented) e
// uma asserção reflect.DeepEqual por campo declarado, com t.Errorf nomeando o
// campo (diff claro, exigência da task).
func TestEmitThenStateGolden(t *testing.T) {
	got := string(emitThenStateFixture(t, `Count(5)`))
	for _, want := range []string{
		"package counter",
		"func TestCounter_IncrementoLevaOStateAoValorEsperado(t *testing.T)",
		"c := &Counter{}",
		`c.state.Id = EntityId("C1")`,
		"c.state.Count = Count(1)",
		"events, err := c.Increment(",
		"if err != nil {",
		"for _, ev := range events {",
		"switch ev := ev.(type) {",
		"case *CounterIncremented:",
		"c.applyCounterIncremented(*ev)",
		`wantState0 := EntityId("C1")`,
		"if !reflect.DeepEqual(c.state.Id, wantState0) {",
		`t.Errorf("state.id: got %+v, want %+v", c.state.Id, wantState0)`,
		"wantState1 := Count(5)",
		"if !reflect.DeepEqual(c.state.Count, wantState1) {",
		`t.Errorf("state.count: got %+v, want %+v", c.state.Count, wantState1)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q no Go gerado de \"then state\", não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "tests_thenstate_counter.go.golden"), []byte(got))
}

// TestEmitThenStateDeterministic prova NFR-13 sobre o novo caminho.
func TestEmitThenStateDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitThenStateFixture(t, `Count(5)`)
	})
}

// TestEmitThenStateSmokeCompile prova NFR-14 sobre o projeto inteiro gerado.
func TestEmitThenStateSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generateThenStateProject(t, `Count(5)`)))
}

// TestEmitThenStateRunGreen é a metade POSITIVA do par NFR-4: o "then state"
// bate com o estado real depois do when — `go test ./...` sobre o projeto
// gerado passa.
func TestEmitThenStateRunGreen(t *testing.T) {
	runGeneratedTests(t, filesToMap(generateThenStateProject(t, `Count(5)`)))
}

// TestEmitThenStateRunRedOnDivergence é a metade NEGATIVA do par NFR-4: o
// mesmo cenário com um valor esperado DIFERENTE do real (Count(99) contra os
// Count(5) que o Apply produz) faz o teste GERADO falhar, com uma mensagem
// que nomeia o campo e mostra got/want — a "falha com diff claro" que a task
// exige. Prova, de quebra, que a asserção não é vacuamente verdadeira.
func TestEmitThenStateRunRedOnDivergence(t *testing.T) {
	out := runGeneratedTestsExpectingFailure(t, filesToMap(generateThenStateProject(t, `Count(99)`)))
	for _, want := range []string{
		"TestCounter_IncrementoLevaOStateAoValorEsperado",
		"state.count: got",
		"want 99",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("esperava %q na saída de `go test` do projeto gerado, não achei:\n%s", want, out)
		}
	}
}

// runGeneratedTestsExpectingFailure é o inverso de runGeneratedTests: escreve
// files num diretório isolado, roda `go test ./...` e exige que ele FALHE
// (exit != 0), devolvendo a saída para inspeção. Um `go test` verde aqui é
// erro — significaria que a asserção gerada não afirma nada.
func runGeneratedTestsExpectingFailure(t *testing.T, files map[string][]byte) string {
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

	if goMod, ok := files["go.mod"]; ok && strings.Contains(string(goMod), "require ") {
		tidy := exec.Command("go", "mod", "tidy")
		tidy.Dir = dir
		if out, err := tidy.CombinedOutput(); err != nil {
			t.Fatalf("`go mod tidy` falhou em %q: %v\n%s", dir, err, out)
		}
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("`go test ./...` passou em %q, mas o \"then state\" diverge do estado real — a asserção gerada não está afirmando nada:\n%s", dir, out)
	}
	return string(out)
}
