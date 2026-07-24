package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/driver"
	"domainscript/types"
)

// mixed_wire_maincall_test.go prova a DoD de L1.2 (ISSUE-7/REQ-52.4, §design
// correcoes-issues-6-7-8 2.2): o CALL SITE em cmd/<service>/main.go
// (generateCmdMainFile, codegen.go) para o módulo misto (UseCase+Policy).
//
// L1.1 (mixed_wire_test.go, TestGenerateMixedModuleWiresCombinedWireAndCompiles)
// já prova "kitchen.Wire(uow, dispatcher)" em cmd/kitchen/main.go, mas como
// consequência incidental de SmokeCompile — o foco daquele teste é a emissão
// combinada em policies.go/usecases.go, não o call site em si. Este arquivo é
// o teste DEDICADO a essa parte: confirma (a) que o call site do módulo misto
// monta os argumentos com uow e dispatcher, NA ORDEM que casa com "func
// Wire(u UnitOfWork, d Dispatcher)" (decl_policy.go:661), e (b) por
// byte-identidade/regressão, que os call sites dos módulos PUROS continuam
// "<pkg>.Wire(uow)"/"<pkg>.Wire(dispatcher)" — um único argumento cada,
// exatamente como antes de L1.1/L1.2 (nenhuma mudança de código foi
// necessária no call site: generateCmdMainFile já montava os args
// corretamente a partir de wt.hasUseCases/wt.hasPolicies).
func TestGenerateMixedModuleMainCallSiteWiresBothArgs(t *testing.T) {
	files := generateMixedWireProject(t)
	m := filesToMap(files)

	mainGo, ok := m["cmd/kitchen/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/kitchen/main.go, não achei:\n%v", filePathsForTest(files))
	}
	// A ordem "uow, dispatcher" tem que casar com a assinatura combinada
	// "func Wire(u runtime.UnitOfWork, d runtime.Dispatcher)" que
	// emitCombinedWireFunc (decl_policy.go) emite — argumento invertido
	// quebraria a compilação.
	if !strings.Contains(string(mainGo), "kitchen.Wire(uow, dispatcher)") {
		t.Errorf("esperava \"kitchen.Wire(uow, dispatcher)\" em cmd/kitchen/main.go (call site do módulo misto, REQ-52.4), não achei:\n%s", mainGo)
	}
	// Guarda negativa: nenhuma forma de argumento único ou trocada deveria
	// aparecer para o alias "kitchen".
	if strings.Contains(string(mainGo), "kitchen.Wire(uow)") || strings.Contains(string(mainGo), "kitchen.Wire(dispatcher)") || strings.Contains(string(mainGo), "kitchen.Wire(dispatcher, uow)") {
		t.Errorf("cmd/kitchen/main.go tem uma forma de Wire inesperada (esperava só o combinado \"Wire(uow, dispatcher)\"):\n%s", mainGo)
	}
}

// TestGeneratePureModulesMainCallSiteUnchanged prova a metade "byte-idêntica"
// do par NFR-4/REQ-52.4: o call site de um módulo só-UseCase continua
// "<pkg>.Wire(uow)" (um argumento) e o de um módulo só-Policy continua
// "<pkg>.Wire(dispatcher)" (um argumento) — sem NENHUM traço do argumento
// combinado. Reusa as fixtures síncronas de mixed_wire_test.go
// (pureUseCaseModDs/Src, purePolicyModDs/Src) para não duplicar cenário.
func TestGeneratePureModulesMainCallSiteUnchanged(t *testing.T) {
	t.Run("só-UseCase", func(t *testing.T) {
		dir := writeProjectDir(t, map[string]string{
			"mod.ds":    pureUseCaseModDs,
			"domain.ds": pureUseCaseSrc,
		})
		prog, bag := driver.CheckProject(dir)
		if bag.HasErrors() {
			t.Fatalf("fixture só-UseCase tem diagnósticos de erro:\n%s", bag.Render())
		}
		model := types.NewModel(prog.Symbols)
		files, err := codegen.Generate(prog, model, prog.Symbols, bag, mixedWireGenerateOptions)
		if err != nil {
			t.Fatalf("Generate: erro inesperado sobre a fixture só-UseCase: %v", err)
		}
		m := filesToMap(files)

		mainGo, ok := m["cmd/orders/main.go"]
		if !ok {
			t.Fatalf("esperava cmd/orders/main.go, não achei:\n%v", filePathsForTest(files))
		}
		if !strings.Contains(string(mainGo), "orders.Wire(uow)") {
			t.Errorf("esperava \"orders.Wire(uow)\" (call site só-UseCase, inalterado) em cmd/orders/main.go, não achei:\n%s", mainGo)
		}
		if strings.Contains(string(mainGo), "orders.Wire(uow, dispatcher)") {
			t.Errorf("cmd/orders/main.go NÃO deveria ter o call site combinado (o módulo não tem Policy):\n%s", mainGo)
		}
	})

	t.Run("só-Policy", func(t *testing.T) {
		dir := writeProjectDir(t, map[string]string{
			"mod.ds":    purePolicyModDs,
			"domain.ds": purePolicySrc,
		})
		prog, bag := driver.CheckProject(dir)
		if bag.HasErrors() {
			t.Fatalf("fixture só-Policy tem diagnósticos de erro:\n%s", bag.Render())
		}
		model := types.NewModel(prog.Symbols)
		files, err := codegen.Generate(prog, model, prog.Symbols, bag, mixedWireGenerateOptions)
		if err != nil {
			t.Fatalf("Generate: erro inesperado sobre a fixture só-Policy: %v", err)
		}
		m := filesToMap(files)

		mainGo, ok := m["cmd/shipping/main.go"]
		if !ok {
			t.Fatalf("esperava cmd/shipping/main.go, não achei:\n%v", filePathsForTest(files))
		}
		if !strings.Contains(string(mainGo), "shipping.Wire(dispatcher)") {
			t.Errorf("esperava \"shipping.Wire(dispatcher)\" (call site só-Policy, inalterado) em cmd/shipping/main.go, não achei:\n%s", mainGo)
		}
		if strings.Contains(string(mainGo), "shipping.Wire(uow, dispatcher)") {
			t.Errorf("cmd/shipping/main.go NÃO deveria ter o call site combinado (o módulo não tem UseCase):\n%s", mainGo)
		}
	})
}
