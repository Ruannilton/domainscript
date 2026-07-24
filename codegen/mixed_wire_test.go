package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// mixed_wire_test.go prova a DoD de L1.1 (ISSUE-7/REQ-52.1/52.2/52.3, §design
// correcoes-issues-6-7-8 2.2): um módulo que declara UseCase E Policy ao mesmo
// tempo — antes recusado por generateModuleFiles com um erro de geração
// (usecases.go e policies.go emitiam DUAS "func Wire" no mesmo pacote Go,
// colisão) — agora gera um único Wire COMBINADO ("func Wire(u UnitOfWork, d
// Dispatcher)") e o projeto Go inteiro compila. Fixture SINTÉTICA mínima e
// dedicada (não a âncora de nenhum ciclo anterior): 1 módulo "Kitchen" com um
// UseCase (Claim de um Ticket via um Handle que emite TicketClaimed) e uma
// Policy local reagindo a esse MESMO TicketClaimed — o padrão exato do
// pizzeria/Kitchen (UseCase+Policy local, SEM canal de saída próprio), a
// forma mais simples que ativa o caminho misto sem esbarrar na guarda F5/G3
// (produtor-de-canal + Dispatcher no mesmo service).
//
// O par NFR-4 é fechado aqui pelas duas outras funções deste arquivo: os
// casos PUROS (só UseCase / só Policy) continuam com sua "func Wire"
// single-arg de sempre (byte-identidade por construção — seguem por
// EmitUseCases/EmitPolicies, inalterados), o que os testes golden/e2e de
// wallet/shop já provam byte a byte na suíte completa.

const mixedWireModDs = `Module Kitchen { }
`

const mixedWireDomainDs = `
ValueObject TicketId(string) {
    Valid { value.length() > 0 }
}

Event TicketClaimed { id TicketId }

Aggregate Ticket {
    strategy EventSourced

    state {
        id TicketId
    }

    access {
        Claim requires caller.authenticated
    }

    Handle Claim() {
        emit TicketClaimed(self.id)
    }

    Apply TicketClaimed {
        state.id = event.id
    }
}
`

const mixedWireApplicationDs = `
Command ClaimTicket {
    id ref Ticket
}

UseCase ClaimTicketUseCase handles ClaimTicket {
    execute {
        ticket = load Ticket(cmd.id)
        ticket.Claim()
    }
}

Policy NotifyOnClaim on TicketClaimed {
    execute { return }
}
`

var mixedWireGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateMixedWireProject escreve a fixture do módulo misto em disco, resolve
// via driver.CheckProject e gera o projeto Go completo — mesmo padrão de
// generateWorkerFixtureProject/generateProducerOutboxProject.
func generateMixedWireProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         mixedWireModDs,
		"domain.ds":      mixedWireDomainDs,
		"application.ds": mixedWireApplicationDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de módulo misto (L1.1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, mixedWireGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de módulo misto: %v", err)
	}
	return files
}

// TestGenerateMixedModuleWiresCombinedWireAndCompiles prova o desbloqueio de
// L1.1: um módulo UseCase+Policy gera (não é mais recusado), usecases.go NÃO
// tem "func Wire" próprio, policies.go tem o Wire COMBINADO de dois
// parâmetros, existe EXATAMENTE um "func Wire(" no pacote inteiro (nenhuma
// colisão), main.go chama Wire(uow, dispatcher), e o projeto Go inteiro
// compila (gentest.SmokeCompile).
func TestGenerateMixedModuleWiresCombinedWireAndCompiles(t *testing.T) {
	files := generateMixedWireProject(t)
	m := filesToMap(files)

	usecasesGo, ok := m["kitchen/usecases.go"]
	if !ok {
		t.Fatalf("esperava kitchen/usecases.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}
	policiesGo, ok := m["kitchen/policies.go"]
	if !ok {
		t.Fatalf("esperava kitchen/policies.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}

	// usecases.go ainda declara a var de pacote uow, mas NÃO emite mais seu
	// Wire próprio (o Wire combinado, em policies.go, é quem escreve uow).
	if !strings.Contains(string(usecasesGo), "var uow runtime.UnitOfWork") {
		t.Errorf("esperava \"var uow runtime.UnitOfWork\" em kitchen/usecases.go, não achei:\n%s", usecasesGo)
	}
	if strings.Contains(string(usecasesGo), "func Wire(") {
		t.Errorf("kitchen/usecases.go NÃO deveria emitir sua própria \"func Wire\" no caso misto (colidiria com a de policies.go):\n%s", usecasesGo)
	}

	// policies.go emite o Wire combinado, com "uow = u".
	if !strings.Contains(string(policiesGo), "func Wire(u runtime.UnitOfWork, d runtime.Dispatcher)") {
		t.Errorf("esperava a assinatura combinada \"func Wire(u runtime.UnitOfWork, d runtime.Dispatcher)\" em kitchen/policies.go, não achei:\n%s", policiesGo)
	}
	if !strings.Contains(string(policiesGo), "uow = u") {
		t.Errorf("esperava \"uow = u\" no corpo do Wire combinado (injeta a uow dos UseCases), não achei:\n%s", policiesGo)
	}
	if !strings.Contains(string(policiesGo), `d.Subscribe("TicketClaimed", NotifyOnClaim)`) {
		t.Errorf("esperava a assinatura da Policy (d.Subscribe) no Wire combinado, não achei:\n%s", policiesGo)
	}

	// EXATAMENTE uma "func Wire(" no pacote kitchen inteiro — a prova central
	// de que não há colisão de símbolo.
	total := strings.Count(string(usecasesGo), "func Wire(") + strings.Count(string(policiesGo), "func Wire(")
	if total != 1 {
		t.Errorf("esperava exatamente 1 \"func Wire(\" no pacote kitchen, achei %d\nusecases.go:\n%s\npolicies.go:\n%s", total, usecasesGo, policiesGo)
	}

	mainGo, ok := m["cmd/kitchen/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/kitchen/main.go, não achei:\n%v", filePathsForTest(files))
	}
	if !strings.Contains(string(mainGo), "kitchen.Wire(uow, dispatcher)") {
		t.Errorf("esperava \"kitchen.Wire(uow, dispatcher)\" em cmd/kitchen/main.go (call site do módulo misto), não achei:\n%s", mainGo)
	}

	gentest.SmokeCompile(t, m)
}

// --- Guardas de byte-identidade dos casos PUROS (NFR-4/NFR-28) ---------------

const pureUseCaseModDs = `Module Orders { }
`

const pureUseCaseSrc = `
ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

Event OrderPlaced { id OrderId }

Aggregate Order {
    strategy EventSourced

    state {
        id OrderId
    }

    access {
        Place requires caller.authenticated
    }

    Handle Place() {
        emit OrderPlaced(self.id)
    }

    Apply OrderPlaced {
        state.id = event.id
    }
}

Command PlaceOrder {
    id ref Order
}

UseCase PlaceOrderUseCase handles PlaceOrder {
    execute {
        order = load Order(cmd.id)
        order.Place()
    }
}
`

// TestGeneratePureUseCaseModuleKeepsSingleArgWire prova que um módulo SÓ com
// UseCase (nenhuma Policy) segue com sua "func Wire(u runtime.UnitOfWork)" de
// sempre — o Wire combinado de dois parâmetros NÃO aparece, e nenhuma
// policies.go é gerada. Guarda de não-regressão da byte-identidade dos casos
// puros (o caminho de EmitUseCases não mudou).
func TestGeneratePureUseCaseModuleKeepsSingleArgWire(t *testing.T) {
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

	usecasesGo, ok := m["orders/usecases.go"]
	if !ok {
		t.Fatalf("esperava orders/usecases.go, não achei:\n%v", filePathsForTest(files))
	}
	if !strings.Contains(string(usecasesGo), "func Wire(u runtime.UnitOfWork)") {
		t.Errorf("esperava a Wire single-arg \"func Wire(u runtime.UnitOfWork)\" em orders/usecases.go (caso puro inalterado), não achei:\n%s", usecasesGo)
	}
	if strings.Contains(string(usecasesGo), "func Wire(u runtime.UnitOfWork, d runtime.Dispatcher)") {
		t.Errorf("orders/usecases.go NÃO deveria ter o Wire combinado (o módulo não tem Policy):\n%s", usecasesGo)
	}
	if _, ok := m["orders/policies.go"]; ok {
		t.Errorf("orders/policies.go NÃO deveria existir (o módulo não tem Policy):\n%v", filePathsForTest(files))
	}
}

const purePolicyModDs = `Module Shipping { }
`

const purePolicySrc = `
ValueObject ShipmentId(string) {
    Valid { value.length() > 0 }
}

Event ShipmentReady { id ShipmentId }

Policy NotifyReady on ShipmentReady {
    execute { return }
}
`

// TestGeneratePurePolicyModuleKeepsSingleArgWire prova que um módulo SÓ com
// Policy (nenhum UseCase) segue com sua "func Wire(d runtime.Dispatcher)" de
// sempre — o Wire combinado NÃO aparece, e nenhuma usecases.go é gerada.
// Guarda simétrica à de cima (o caminho de EmitPolicies não mudou).
func TestGeneratePurePolicyModuleKeepsSingleArgWire(t *testing.T) {
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

	policiesGo, ok := m["shipping/policies.go"]
	if !ok {
		t.Fatalf("esperava shipping/policies.go, não achei:\n%v", filePathsForTest(files))
	}
	if !strings.Contains(string(policiesGo), "func Wire(d runtime.Dispatcher)") {
		t.Errorf("esperava a Wire single-arg \"func Wire(d runtime.Dispatcher)\" em shipping/policies.go (caso puro inalterado), não achei:\n%s", policiesGo)
	}
	if strings.Contains(string(policiesGo), "func Wire(u runtime.UnitOfWork, d runtime.Dispatcher)") {
		t.Errorf("shipping/policies.go NÃO deveria ter o Wire combinado (o módulo não tem UseCase):\n%s", policiesGo)
	}
	if _, ok := m["shipping/usecases.go"]; ok {
		t.Errorf("shipping/usecases.go NÃO deveria existir (o módulo não tem UseCase):\n%v", filePathsForTest(files))
	}
}
