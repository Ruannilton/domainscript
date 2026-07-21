package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// run_error_test.go prova a DoD de J6.2 (R1, REQ-47.2/47.3, §design
// infra-providers 3.6): quando cmd/<service>/main.go abre 2+ recursos reais
// fallíveis no MESMO service (aqui: o Database do outbox durável de
// RunShipping + a FileStorage s3 de RunDocs), o corpo migra para "func run()
// error" com "defer Close()" por recurso closeable, e "func main()" vira só
// "if err := run(); err != nil { log.Fatal(err) }" — o log.Fatal ÚNICO do
// programa, nunca no meio do wiring (onde pularia o defer de um recurso já
// aberto). A fixture é sintética (3 módulos, SEM topology.ds — grupo
// default único, "app"): RunOrders (UseCase-only) emite o PublicEvent
// RunOrderPlaced; RunShipping (Policy-only) reage a ele localmente
// ("delivery AtLeastOnce", Database postgres própria — 1º recurso, o
// Outbox durável, mesmo padrão de decl_policy_outbox_test.go); RunDocs
// (UseCase-only) declara uma FileStorage `provider: "s3"` (2º recurso) sem
// nenhuma Policy/canal envolvidos — nenhum dos dois hits a limitação
// pré-existente "produtor de canal + Dispatcher no mesmo service" (não há
// canal nenhum aqui, só Dispatcher local).

const runOrdersModDs = `Module RunOrders { }
`

const runOrdersDomainDs = `
ValueObject RunOrderId(string) {
    Valid { value.length() > 0 }
}

PublicEvent RunOrderPlaced {
    id RunOrderId
}

Aggregate RunOrder {
    strategy EventSourced

    state {
        id RunOrderId
    }

    access {
        Place requires caller.authenticated
    }

    Handle Place() {
        emit RunOrderPlaced(self.id)
    }

    Apply RunOrderPlaced {
    }
}
`

const runOrdersApplicationDs = `
Command PlaceRunOrder {
    orderId ref RunOrder
}

UseCase PlaceRunOrderUseCase handles PlaceRunOrder {
    execute {
        order = load RunOrder(cmd.orderId)
        order.Place()
    }
}
`

const runShippingModDs = `Module RunShipping {
    Database ShippingDb {
        provider: "postgres"
        connection: env("SHIPPING_PG_URL")
        manages: []
    }
}
`

const runShippingPolicyDs = `Policy NotifyRunShipping on RunOrderPlaced {
    delivery AtLeastOnce
    execute { return }
}
`

const runDocsModDs = `Module RunDocs {
    FileStorage RunDocsStorage {
        provider: "s3"
        bucket: env("RUN_DOCS_BUCKET")
        region: env("AWS_REGION")
    }
}
`

const runDocsDomainDs = `
ValueObject RunDocId(string) {
    Valid { value.length() > 0 }
}

Event RunDocRegistered {
    id RunDocId
    attachment FileRef
}

Aggregate RunDoc {
    strategy EventSourced

    storage {
        attachment: RunDocsStorage
    }

    state {
        id RunDocId
        attachment FileRef
    }

    access {
        Register requires caller.authenticated
    }

    Handle Register(attachment FileRef) {
        emit RunDocRegistered(self.id, attachment)
    }

    Apply RunDocRegistered {
        state.attachment = event.attachment
    }
}
`

const runDocsApplicationDs = `
Command RegisterRunDoc {
    docId ref RunDoc
    attachment File
}

UseCase RegisterRunDocUseCase handles RegisterRunDoc {
    execute {
        doc = load RunDoc(cmd.docId)
        doc.Register(store cmd.attachment)
    }
}
`

var runErrorGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateRunErrorProject monta a fixture (3 módulos, sem topology.ds — 1
// grupo default "app") e gera o projeto Go completo.
func generateRunErrorProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"orders/mod.ds":         runOrdersModDs,
		"orders/domain.ds":      runOrdersDomainDs,
		"orders/application.ds": runOrdersApplicationDs,
		"shipping/mod.ds":       runShippingModDs,
		"shipping/policy.ds":    runShippingPolicyDs,
		"docs/mod.ds":           runDocsModDs,
		"docs/domain.ds":        runDocsDomainDs,
		"docs/application.ds":   runDocsApplicationDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de run() error (J6.2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, runErrorGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de run() error: %v", err)
	}
	return files
}

// TestGenerateRunErrorMainUsesRunErrorForm prova a forma "func run() error"
// + "defer Close()" (outbox) + "func main()" chamando run() uma única vez,
// quando 2+ recursos reais coexistem no mesmo service (REQ-47.2/47.3).
func TestGenerateRunErrorMainUsesRunErrorForm(t *testing.T) {
	files := generateRunErrorProject(t)
	main := fileContent(t, files, "cmd/app/main.go")

	for _, want := range []string{
		"func run() error {",
		"runShippingOutboxDB, err := sqlruntime.OpenPostgres(",
		"defer runShippingOutboxDB.Close()",
		"if err != nil {\n\t\treturn err\n\t}",
		"rundocsRunDocsStorageFS, err := s3runtime.NewS3FileStorage(",
		"return server.ListenAndServe()",
		"func main() {",
		"if err := run(); err != nil {",
		"log.Fatal(err)",
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("cmd/app/main.go não contém %q:\n%s", want, main)
		}
	}

	// func main() precisa ficar CURTO — só chama run() e falha fechado, o
	// log.Fatal ÚNICO do programa; nenhum wiring direto ali.
	mainIdx := strings.Index(main, "func main() {")
	if mainIdx < 0 {
		t.Fatal("esperava \"func main() {\" em cmd/app/main.go")
	}
	mainBody := main[mainIdx:]
	if strings.Contains(mainBody, "NewMemoryEventStore") {
		t.Fatalf("func main() não deveria conter wiring direto (isso pertence a run()):\n%s", mainBody)
	}

	// run() precisa vir ANTES de main() no arquivo (main chama run(), Go não
	// exige isso, mas a ordem de leitura natural é run() primeiro).
	runIdx := strings.Index(main, "func run() error {")
	if runIdx < 0 || runIdx > mainIdx {
		t.Fatalf("esperava \"func run() error\" ANTES de \"func main()\" em cmd/app/main.go:\n%s", main)
	}
}

// TestGenerateRunErrorSmokeCompile prova que o projeto inteiro, com a forma
// "func run() error" ativa, compila e vet-limpa de verdade.
func TestGenerateRunErrorSmokeCompile(t *testing.T) {
	files := generateRunErrorProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// --- Canal produtor rabbitmq + defer Close() seguro (achado da revisão da
// PR #33) ---
//
// A fixture acima (outbox + S3) nunca exercita emitDeferChannelClose — o
// ÚNICO recurso closeable ali é o Database do outbox (*sql.DB). Esta
// segunda fixture (2 services, via topology.ds) força um canal PRODUTOR
// rabbitmq a coexistir com uma FileStorage s3 no MESMO service (RunChan):
// 2 recursos reais (canal + S3) no mesmo main.go, o canal sendo o
// closeable — prova que a asserção de tipo segura (comma-ok, o fix da
// revisão) realmente compila e fecha o canal via defer.

const runChanModDs = `Module RunChan {
    FileStorage RunChanStorage {
        provider: "s3"
        bucket: env("RUN_CHAN_BUCKET")
        region: env("AWS_REGION")
    }
}
`

const runChanDomainDs = `
ValueObject RunChanId(string) {
    Valid { value.length() > 0 }
}

PublicEvent RunChanSent {
    id RunChanId
}

Aggregate RunChanAgg {
    strategy EventSourced

    state {
        id RunChanId
    }

    access {
        Send requires caller.authenticated
    }

    Handle Send() {
        emit RunChanSent(self.id)
    }

    Apply RunChanSent {
    }
}
`

const runChanApplicationDs = `
Command SendRunChan {
    id ref RunChanAgg
}

UseCase SendRunChanUseCase handles SendRunChan {
    execute {
        agg = load RunChanAgg(cmd.id)
        agg.Send()
    }
}
`

const runChanReceiverModDs = `Module RunChanReceiver { }
`

const runChanReceiverPolicyDs = `Policy NotifyRunChanReceiver on RunChanSent {
    delivery AtLeastOnce
    execute { return }
}
`

const runChanTopologyDs = `Topology {
    services {
        RunChanSvc { modules: [RunChan] }
        RunChanReceiverSvc { modules: [RunChanReceiver] }
    }
    channels {
        RunChan -> RunChanReceiver {
            via: queue
            provider: "rabbitmq"
            connection: env("RUN_CHAN_AMQP_URL")
        }
    }
}
`

// generateRunErrorChannelProject monta a fixture (2 services, canal
// produtor rabbitmq + FileStorage s3 no MESMO service) e gera o projeto Go
// completo.
func generateRunErrorChannelProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":         runChanTopologyDs,
		"chan/mod.ds":         runChanModDs,
		"chan/domain.ds":      runChanDomainDs,
		"chan/application.ds": runChanApplicationDs,
		"receiver/mod.ds":     runChanReceiverModDs,
		"receiver/policy.ds":  runChanReceiverPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de run() error + canal (J6.2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, runErrorGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de run() error + canal: %v", err)
	}
	return files
}

// TestGenerateRunErrorChannelUsesSafeDeferClose prova que o canal produtor
// rabbitmq, quando runMode está ativo (aqui: canal + FileStorage s3 no
// mesmo service RunChanSvc), fecha via asserção de tipo SEGURA (comma-ok,
// achado da revisão da PR #33) — nunca a forma direta/insegura que
// panicaria se o tipo subjacente não implementasse Close().
func TestGenerateRunErrorChannelUsesSafeDeferClose(t *testing.T) {
	files := generateRunErrorChannelProject(t)
	main := fileContent(t, files, "cmd/runchansvc/main.go")

	for _, want := range []string{
		"func run() error {",
		"amqpruntime.NewRabbitMQChannel(",
		"if closeable, ok := runChanChannel.(interface{ Close() error }); ok {",
		"defer closeable.Close()",
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("cmd/runchansvc/main.go não contém %q:\n%s", want, main)
		}
	}
	if strings.Contains(main, "runChanChannel.(interface{ Close() error }).Close()") {
		t.Fatalf("cmd/runchansvc/main.go não deveria usar a asserção de tipo INSEGURA:\n%s", main)
	}
}

// TestGenerateRunErrorChannelSmokeCompile prova que esse projeto (canal
// produtor + defer Close() seguro) compila e vet-limpa de verdade.
func TestGenerateRunErrorChannelSmokeCompile(t *testing.T) {
	files := generateRunErrorChannelProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}
