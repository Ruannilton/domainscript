package codegen_test

import (
	"errors"
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
// (docs/examples/wallet, via driver.CheckProject — o pipeline completo que
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
		"runtime.NewUnitOfWork(store)",
		"wallet.Wire(uow)",
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
