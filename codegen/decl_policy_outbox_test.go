package codegen_test

import (
	"fmt"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/diag"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// decl_policy_outbox_test.go prova a DoD de J2.5 (REQ-42.5/42.7, §design
// infra-providers 3.2): a seleção NewDurableOutbox vs. NewOutbox(d) — o
// wiring gerado troca de Outbox real (DurableOutbox) para memoryOutbox
// conforme o módulo declara ou não um Database real — e o Start(ctx) do
// relay/cleanup em cmd/<service>/main.go. Nem wallet nem shop exercitam essa
// combinação (shipping do shop, a única Policy AtLeastOnce real hoje, não
// tem Database próprio — ver a doc de moduleOutboxDatabaseName) — por isso
// esta é uma fixture sintética nova, no mesmo espírito de
// ratelimit_test.go/decl_metric_test.go (Marco G/H): 2 módulos sem
// topology.ds (canal "d" local sempre, nunca cross-service via queue).

const outboxOrdersModDs = `Module Orders {}`

const outboxOrdersDomainDs = `
ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

PublicEvent OrderPlaced { id OrderId }

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
    }
}
`

const outboxOrdersApplicationDs = `
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

const outboxOrdersInterfaceDs = `
Interface HTTP {
    POST "/orders" -> PlaceOrderUseCase
}
`

// outboxShippingModDsTemplate: %s é o provider ("sqlite" reconhecido, ou
// "pg" decorativo/não reconhecido — TestPolicyOutboxSelection exercita os
// dois lados da seleção sobre a MESMA fixture, só trocando isto).
const outboxShippingModDsTemplate = `Module Shipping {
    Database MainDb {
        provider: "%s"
        manages: []
    }
}
`

const outboxShippingPolicyDs = `
Policy NotifyShipping on OrderPlaced {
    delivery AtLeastOnce
    execute { return }
}
`

// parseOutboxFixtureProgram monta o projeto sintético (2 módulos, sem
// topology.ds — Policy sempre local, nunca cross-service via canal) com
// Shipping.Database.provider = provider, e o resolve via driver.CheckProject.
func parseOutboxFixtureProgram(t *testing.T, provider string) (*program.Program, *diag.DiagnosticBag) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"orders/mod.ds":         outboxOrdersModDs,
		"orders/domain.ds":      outboxOrdersDomainDs,
		"orders/application.ds": outboxOrdersApplicationDs,
		"orders/interface.ds":   outboxOrdersInterfaceDs,
		"shipping/mod.ds":       fmt.Sprintf(outboxShippingModDsTemplate, provider),
		"shipping/policy.ds":    outboxShippingPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de outbox (J2.5) tem diagnósticos de erro (provider=%q):\n%s", provider, bag.Render())
	}
	return prog, bag
}

var outboxGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

func generateOutboxProject(t *testing.T, provider string) []codegen.File {
	t.Helper()
	prog, bag := parseOutboxFixtureProgram(t, provider)
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, outboxGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado (provider=%q): %v", provider, err)
	}
	return files
}

func fileContent(t *testing.T, files []codegen.File, path string) string {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return string(f.Content)
		}
	}
	t.Fatalf("arquivo %q não encontrado na geração", path)
	return ""
}

// TestPolicyOutboxDurableWhenModuleHasRealDatabase prova REQ-42.5: com
// Shipping.Database.provider = "sqlite" (reconhecido, recognizedSQLProvider),
// policies.go promove "o" a var de pacote (*runtime.DurableOutbox),
// constrói via runtime.NewDurableOutbox(outboxStore, ...) dentro de Wire, e
// ganha WireOutboxStore/StartOutboxRelay/StartOutboxCleanup; main.go abre a
// conexão real, monta o sqlruntime.OutboxStore, chama WireOutboxStore ANTES
// de Wire, e sobe StartOutboxRelay/StartOutboxCleanup como goroutines.
func TestPolicyOutboxDurableWhenModuleHasRealDatabase(t *testing.T) {
	files := generateOutboxProject(t, "sqlite")

	policies := fileContent(t, files, "shipping/policies.go")
	for _, want := range []string{
		"var outboxStore runtime.OutboxStore",
		"func WireOutboxStore(store runtime.OutboxStore)",
		"var o *runtime.DurableOutbox",
		"o = runtime.NewDurableOutbox(outboxStore, map[string]runtime.EventFactory{",
		`"OrderPlaced": func() runtime.Event { return &contracts.OrderPlaced{} },`,
		"func StartOutboxRelay(ctx context.Context)",
		"o.Start(ctx)",
		"func StartOutboxCleanup(ctx context.Context)",
		"o.Cleanup(ctx, 7*24*time.Hour)",
	} {
		if !strings.Contains(policies, want) {
			t.Fatalf("shipping/policies.go não contém %q:\n%s", want, policies)
		}
	}
	if strings.Contains(policies, "runtime.NewOutbox(d)") {
		t.Fatalf("shipping/policies.go ainda usa o memoryOutbox stopgap apesar de Database real:\n%s", policies)
	}

	main := fileContent(t, files, "cmd/app/main.go")
	for _, want := range []string{
		"shippingOutboxDB, err := sqlruntime.Open(",
		"shippingOutboxStore := sqlruntime.NewOutboxStore(shippingOutboxDB, sqlruntime.SQLiteDialect())",
		"shipping.WireOutboxStore(shippingOutboxStore)",
		"go shipping.StartOutboxRelay(workerCtx)",
		"go shipping.StartOutboxCleanup(workerCtx)",
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("cmd/app/main.go não contém %q:\n%s", want, main)
		}
	}

	// WireOutboxStore precisa rodar ANTES de Wire (ver a doc de
	// emitPolicyWireFunc/emitOutboxDatabaseWiring) — Wire lê outboxStore ao
	// construir o DurableOutbox.
	wireIdx := strings.Index(main, "shipping.Wire(dispatcher)")
	storeIdx := strings.Index(main, "shipping.WireOutboxStore(")
	if wireIdx < 0 || storeIdx < 0 || storeIdx > wireIdx {
		t.Fatalf("WireOutboxStore não roda antes de Wire em cmd/app/main.go:\n%s", main)
	}

	gentest.SmokeCompile(t, filesToMap(files))
}

// TestPolicyOutboxMemoryWhenNoRealDatabase prova a metade NFR-21/23 de
// REQ-42.5: com Shipping.Database.provider = "pg" (rótulo decorativo, NÃO
// reconhecido por recognizedSQLProvider — mesma convenção usada por outras
// fixtures deste pacote), policies.go/main.go permanecem EXATAMENTE como
// antes desta task — "o := runtime.NewOutbox(d)" var LOCAL de Wire, nenhum
// WireOutboxStore/StartOutboxRelay/StartOutboxCleanup em lugar nenhum.
func TestPolicyOutboxMemoryWhenNoRealDatabase(t *testing.T) {
	files := generateOutboxProject(t, "pg")

	policies := fileContent(t, files, "shipping/policies.go")
	if !strings.Contains(policies, "o := runtime.NewOutbox(d)") {
		t.Fatalf("shipping/policies.go deveria manter o memoryOutbox stopgap (sem Database real):\n%s", policies)
	}
	for _, unwanted := range []string{"WireOutboxStore", "DurableOutbox", "StartOutboxRelay", "StartOutboxCleanup", "outboxStore"} {
		if strings.Contains(policies, unwanted) {
			t.Fatalf("shipping/policies.go não deveria conter %q (provider não reconhecido, NFR-21):\n%s", unwanted, policies)
		}
	}

	main := fileContent(t, files, "cmd/app/main.go")
	for _, unwanted := range []string{"WireOutboxStore", "StartOutboxRelay", "StartOutboxCleanup", "sqlruntime"} {
		if strings.Contains(main, unwanted) {
			t.Fatalf("cmd/app/main.go não deveria conter %q (provider não reconhecido, NFR-21):\n%s", unwanted, main)
		}
	}

	gentest.SmokeCompile(t, filesToMap(files))
}
