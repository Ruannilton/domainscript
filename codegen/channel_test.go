package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/goname"
	"domainscript/codegen/rtsrc"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// channel_test.go prova os critérios de conclusão da task F5 (§design
// codegen 3.11, REQ-25.3, REQ-26.1/26.5) que o shop real (decl_policy_test.go)
// não exercita: docs/examples/shop/topology.ds só declara "via: queue
// orderBy: id" — sem workers{}/timeout/circuitBreaker. Esta fixture SINTÉTICA
// (mesmo padrão de decl_worker_test.go/decl_io_test.go: shop não tem o
// suficiente, uma fixture cobre) declara um canal "queue" com TODOS os
// settings de REQ-26.5 juntos, e prova os caminhos de erro claro (via não
// suportado, múltiplos canais de saída, orderBy para um campo inexistente).

// channelFixtureTopologyDs declara duas services (Alpha/Beta) ligadas por um
// canal "queue" com orderBy + workers{concurrency,maxRate,batchSize} +
// timeout + circuitBreaker{threshold,cooldown} — o exemplo completo do spec
// §11, sem provider externo (in-memory, REQ-26.5).
const channelFixtureTopologyDs = `Topology {
    services {
        AlphaSvc { modules: [Alpha] }
        BetaSvc { modules: [Beta] }
    }
    channels {
        Alpha -> Beta {
            via: queue
            orderBy: id
            workers { concurrency: 5 maxRate: 100 batchSize: 10 }
            timeout: 10s
            circuitBreaker: { threshold: 5 cooldown: 30s }
        }
    }
}
`

// channelFixtureAlphaModDs/DomainDs declaram o módulo produtor: um Aggregate
// mínimo cujo Handle emite o PublicEvent que atravessa o canal — mesma forma
// de Orders/OrderPlaced no shop real (docs/examples/shop/orders/domain.ds).
const channelFixtureAlphaModDs = `Module Alpha {
    Database MainDb {
        provider: "postgres"
        manages: [Widget]
    }
}
`

const channelFixtureAlphaDomainDs = `
ValueObject WidgetId(string) {
    Valid { value.length() > 0 }
}

PublicEvent WidgetMade { id WidgetId }

Aggregate Widget {
    strategy EventSourced

    state {
        id WidgetId
    }

    access {
        Make requires caller.authenticated
    }

    Handle Make() {
        emit WidgetMade(self.id)
    }

    Apply WidgetMade {
        state.id = event.id
    }
}

Command Make {
    id ref Widget
}

UseCase MakeWidget handles Make {
    execute {
        widget = load Widget(cmd.id)
        widget.Make()
    }
}
`

// channelFixtureBetaModDs/PolicyDs declaram o módulo consumidor: só uma
// Policy cross-service (mesma forma de Shipping/NotifyShipping no shop real).
const channelFixtureBetaModDs = `Module Beta { }
`

const channelFixtureBetaPolicyDs = `Policy ReactToWidget on WidgetMade {
    delivery AtLeastOnce
    execute { return }
}
`

// parseChannelFixture monta o projeto sintético em disco e o resolve via
// driver.CheckProject — mesmo padrão de parseWorkerFixture
// (decl_worker_test.go). topologyDs é parametrizado para os testes de erro
// (via não suportado) reusarem o resto da fixture com só o canal mudado.
func parseChannelFixture(t *testing.T, topologyDs string) *program.Program {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":     topologyDs,
		"alpha/mod.ds":    channelFixtureAlphaModDs,
		"alpha/domain.ds": channelFixtureAlphaDomainDs,
		"beta/mod.ds":     channelFixtureBetaModDs,
		"beta/policy.ds":  channelFixtureBetaPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de canal (F5) tem diagnósticos de erro:\n%s", bag.Render())
	}
	return prog
}

// --- Golden: todos os settings de REQ-26.5 juntos (workers/timeout/circuitBreaker). ---

// TestEmitPoliciesChannelWithWorkersTimeoutCircuitBreakerGolden prova que
// EmitPolicies (lado consumidor) lê `workers{concurrency,maxRate,batchSize}`/
// `timeout`/`circuitBreaker{threshold,cooldown}` do canal REAL da topologia
// (não só os defaults que o shop exercita) e monta o
// runtime.QueueChannelConfig correspondente, com o KeyFunc extraindo o campo
// de orderBy ("id" -> "Id") do tipo concreto do evento.
func TestEmitPoliciesChannelWithWorkersTimeoutCircuitBreakerGolden(t *testing.T) {
	prog := parseChannelFixture(t, channelFixtureTopologyDs)
	model := types.NewModel(prog.Symbols)
	policy := findPolicyDecl(t, prog, "ReactToWidget")

	got, err := codegen.EmitPolicies("beta", []*ast.PolicyDecl{policy}, model, prog.Symbols, prog, "Beta", goname.NewVOOperatorRegistry(), nil, nil)
	if err != nil {
		t.Fatalf("EmitPolicies: erro inesperado: %v", err)
	}
	for _, want := range []string{
		"runtime.QueueChannelConfig{Concurrency: 5, MaxRate: 100, BatchSize: 10, Timeout: time.Duration(10000000000), BreakerThreshold: 5, BreakerCooldown: time.Duration(30000000000)}",
		"case *contracts.WidgetMade:",
		"return fmt.Sprint(e.Id)",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava %q no Go gerado, não achei:\n%s", want, got)
		}
	}
}

// TestEmitPoliciesChannelSettingsDeterministic prova NFR-13 sobre a fixture
// com todos os settings.
func TestEmitPoliciesChannelSettingsDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		prog := parseChannelFixture(t, channelFixtureTopologyDs)
		model := types.NewModel(prog.Symbols)
		policy := findPolicyDecl(t, prog, "ReactToWidget")
		got, err := codegen.EmitPolicies("beta", []*ast.PolicyDecl{policy}, model, prog.Symbols, prog, "Beta", goname.NewVOOperatorRegistry(), nil, nil)
		if err != nil {
			t.Fatalf("EmitPolicies: erro inesperado: %v", err)
		}
		return got
	})
}

// --- Smoke compile: consumidor isolado + o projeto INTEIRO (produtor+consumidor). ---

// channelFixtureSmokeFiles monta os arquivos mínimos para compilar
// ReactToWidget isoladamente — mesmo padrão de policySmokeFiles
// (decl_policy_test.go), agora sobre a fixture com workers/timeout/
// circuitBreaker.
func channelFixtureSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog := parseChannelFixture(t, channelFixtureTopologyDs)
	model := types.NewModel(prog.Symbols)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}
	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	widgetId := findValueObjectDecl(t, prog, "WidgetId")
	voGo, err := codegen.EmitValueObject("alpha", widgetId)
	if err != nil {
		t.Fatalf("EmitValueObject(WidgetId): erro inesperado: %v", err)
	}
	files[filepath.Join("alpha", "widget_id.go")] = voGo

	widgetMade := findEventDecl(t, prog, "WidgetMade")
	alphaEventsGo, err := codegen.EmitEvents("alpha", []*ast.EventDecl{widgetMade})
	if err != nil {
		t.Fatalf("EmitEvents(WidgetMade): erro inesperado: %v", err)
	}
	files[filepath.Join("alpha", "events.go")] = alphaEventsGo

	contractsGo, err := codegen.EmitPublicEvents([]*ast.EventDecl{widgetMade}, map[string]string{"WidgetMade": "Alpha"})
	if err != nil {
		t.Fatalf("EmitPublicEvents(WidgetMade): erro inesperado: %v", err)
	}
	files[filepath.Join("contracts", "events.go")] = contractsGo

	policy := findPolicyDecl(t, prog, "ReactToWidget")
	policyGo, err := codegen.EmitPolicies("beta", []*ast.PolicyDecl{policy}, model, prog.Symbols, prog, "Beta", goname.NewVOOperatorRegistry(), nil, nil)
	if err != nil {
		t.Fatalf("EmitPolicies(ReactToWidget): erro inesperado: %v", err)
	}
	files[filepath.Join("beta", "policies.go")] = policyGo
	return files
}

// TestEmitPoliciesChannelSettingsSmokeCompile prova NFR-14 sobre o canal com
// todos os settings de REQ-26.5 juntos — metade isolada (a outra metade, o
// projeto inteiro com o lado PRODUTOR, é
// TestGenerateChannelFixtureProducerAndConsumerCompile abaixo).
func TestEmitPoliciesChannelSettingsSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, channelFixtureSmokeFiles(t))
}

// TestGenerateChannelFixtureProducerAndConsumerCompile prova o lado PRODUTOR
// (codegen.go:generateCmdMainFile) sobre a MESMA fixture: cmd/alphasvc/
// main.go injeta seu PRÓPRIO runtime.NewQueueChannel (mesmos settings do
// canal) como publisher da unit of work, e o projeto INTEIRO (produtor +
// consumidor) compila — o par exato de TestGenerateShopPolicyRegisters
// SubscriberAndCompiles (decl_policy_test.go), agora com workers/timeout/
// circuitBreaker de verdade em vez dos defaults do shop.
func TestGenerateChannelFixtureProducerAndConsumerCompile(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":     channelFixtureTopologyDs,
		"alpha/mod.ds":    channelFixtureAlphaModDs,
		"alpha/domain.ds": channelFixtureAlphaDomainDs,
		"beta/mod.ds":     channelFixtureBetaModDs,
		"beta/policy.ds":  channelFixtureBetaPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de canal (F5) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, shopGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado: %v", err)
	}
	m := filesToMap(files)

	alphaMain, ok := m["cmd/alphasvc/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/alphasvc/main.go (service AlphaSvc, módulo Alpha), não achei:\n%v", filePathsForTest(files))
	}
	for _, want := range []string{
		"runtime.QueueChannelConfig{Concurrency: 5, MaxRate: 100, BatchSize: 10, Timeout: time.Duration(10000000000), BreakerThreshold: 5, BreakerCooldown: time.Duration(30000000000)}",
		"uow := runtime.NewUnitOfWork(store, alphaChannel)",
	} {
		if !strings.Contains(string(alphaMain), want) {
			t.Errorf("esperava %q em cmd/alphasvc/main.go, não achei:\n%s", want, alphaMain)
		}
	}

	gentest.SmokeCompile(t, m)
}

// --- Erros de geração claros (nunca fallback silencioso, REQ-26.5). ---

// channelFixtureUnsupportedViaTopologyDs troca "via: queue" por "via: grpc"
// — sintaticamente válido (o front-end aceita qualquer um dos 5 valores do
// spec §11: direct/queue/grpc/http/stream, REQ-5.16 só se aplica a
// queue/stream), mas o gerador (F5) só implementa direct/queue hoje.
const channelFixtureUnsupportedViaTopologyDs = `Topology {
    services {
        AlphaSvc { modules: [Alpha] }
        BetaSvc { modules: [Beta] }
    }
    channels {
        Alpha -> Beta {
            via: grpc
        }
    }
}
`

// TestEmitPoliciesUnsupportedChannelKindIsGenerationError prova que um
// `via` que REQ-26.5 nomeia mas este marco não implementa (grpc/http/
// stream) é um erro de geração CLARO do lado consumidor (decl_policy.go),
// nunca um fallback silencioso para despacho local — ver a doc de
// unsupportedChannelKindError (channel.go).
func TestEmitPoliciesUnsupportedChannelKindIsGenerationError(t *testing.T) {
	prog := parseChannelFixture(t, channelFixtureUnsupportedViaTopologyDs)
	model := types.NewModel(prog.Symbols)
	policy := findPolicyDecl(t, prog, "ReactToWidget")

	_, err := codegen.EmitPolicies("beta", []*ast.PolicyDecl{policy}, model, prog.Symbols, prog, "Beta", goname.NewVOOperatorRegistry(), nil, nil)
	if err == nil {
		t.Fatal("esperava erro de geração para via \"grpc\" (não suportado neste marco)")
	}
	for _, want := range []string{"grpc", "não é suportado"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("mensagem de erro não menciona %q: %v", want, err)
		}
	}
}

// TestGenerateUnsupportedChannelKindIsGenerationError prova o MESMO erro
// claro do lado PRODUTOR (codegen.go:producerChannelFor via
// generateCmdMainFile), sobre o orquestrador completo.
func TestGenerateUnsupportedChannelKindIsGenerationError(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":     channelFixtureUnsupportedViaTopologyDs,
		"alpha/mod.ds":    channelFixtureAlphaModDs,
		"alpha/domain.ds": channelFixtureAlphaDomainDs,
		"beta/mod.ds":     channelFixtureBetaModDs,
		"beta/policy.ds":  channelFixtureBetaPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture não deveria ter erros de front-end (via grpc é sintaticamente válido):\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	_, err := codegen.Generate(prog, model, prog.Symbols, bag, shopGenerateOptions)
	if err == nil {
		t.Fatal("esperava erro de geração para via \"grpc\" (não suportado neste marco)")
	}
	for _, want := range []string{"grpc", "não é suportado"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("mensagem de erro não menciona %q: %v", want, err)
		}
	}
}

// channelFixtureOrderByMismatchTopologyDs declara orderBy sobre um campo que
// WidgetMade não tem — erro de geração claro (channelEventHasField,
// channel.go), nunca um KeyFunc que silenciosamente sempre devolve "".
const channelFixtureOrderByMismatchTopologyDs = `Topology {
    services {
        AlphaSvc { modules: [Alpha] }
        BetaSvc { modules: [Beta] }
    }
    channels {
        Alpha -> Beta {
            via: queue
            orderBy: bogus
        }
    }
}
`

// TestEmitPoliciesOrderByFieldMismatchIsGenerationError prova o erro claro
// quando `orderBy` referencia um campo que o(s) PublicEvent(s) do canal não
// declaram.
func TestEmitPoliciesOrderByFieldMismatchIsGenerationError(t *testing.T) {
	prog := parseChannelFixture(t, channelFixtureOrderByMismatchTopologyDs)
	model := types.NewModel(prog.Symbols)
	policy := findPolicyDecl(t, prog, "ReactToWidget")

	_, err := codegen.EmitPolicies("beta", []*ast.PolicyDecl{policy}, model, prog.Symbols, prog, "Beta", goname.NewVOOperatorRegistry(), nil, nil)
	if err == nil {
		t.Fatal("esperava erro de geração: orderBy \"bogus\" não existe em WidgetMade")
	}
	for _, want := range []string{"bogus", "WidgetMade"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("mensagem de erro não menciona %q: %v", want, err)
		}
	}
}

// channelFixtureGammaModDs/PolicyDs é um TERCEIRO módulo consumidor, reagindo
// ao MESMO PublicEvent WidgetMade — usado só por
// TestGenerateMultipleOutgoingQueueChannelsIsGenerationError abaixo para
// forçar Alpha a ter DOIS canais de saída via queue (Alpha->Beta e
// Alpha->Gamma), o caso ainda não suportado (ver a doc de
// producerChannelFor, channel.go).
const channelFixtureGammaModDs = `Module Gamma { }
`

const channelFixtureGammaPolicyDs = `Policy ReactToWidgetInGamma on WidgetMade {
    delivery BestEffort
    execute { return }
}
`

const channelFixtureTwoOutgoingChannelsTopologyDs = `Topology {
    services {
        AlphaSvc { modules: [Alpha] }
        BetaSvc { modules: [Beta] }
        GammaSvc { modules: [Gamma] }
    }
    channels {
        Alpha -> Beta {
            via: queue
            orderBy: id
        }
        Alpha -> Gamma {
            via: queue
            orderBy: id
        }
    }
}
`

// TestGenerateMultipleOutgoingQueueChannelsIsGenerationError prova o erro de
// geração claro quando um módulo produtor tem MAIS DE UM canal de saída via
// queue (wiring combinado, mais de um transporte por unit of work, ainda não
// suportado — ver a doc de producerChannelFor, channel.go).
func TestGenerateMultipleOutgoingQueueChannelsIsGenerationError(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":     channelFixtureTwoOutgoingChannelsTopologyDs,
		"alpha/mod.ds":    channelFixtureAlphaModDs,
		"alpha/domain.ds": channelFixtureAlphaDomainDs,
		"beta/mod.ds":     channelFixtureBetaModDs,
		"beta/policy.ds":  channelFixtureBetaPolicyDs,
		"gamma/mod.ds":    channelFixtureGammaModDs,
		"gamma/policy.ds": channelFixtureGammaPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture não deveria ter erros de front-end:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	_, err := codegen.Generate(prog, model, prog.Symbols, bag, shopGenerateOptions)
	if err == nil {
		t.Fatal("esperava erro de geração: Alpha tem 2 canais de saída via queue (wiring combinado não suportado)")
	}
	if !strings.Contains(err.Error(), "mais de um canal de saída via queue") {
		t.Fatalf("mensagem de erro não menciona o motivo esperado: %v", err)
	}
}
