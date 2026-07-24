package codegen_test

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/goname"
	"domainscript/driver"
	"domainscript/types"
)

// channel_rabbitmq_test.go prova os critérios de conclusão de J3.4 (REQ-43.2/
// 43.7, R1/R2, §design infra-providers 3.3): seleção real do provider
// "rabbitmq" no wiring gerado, nos dois lados (produtor/consumidor) do MESMO
// canal — reusa a fixture multi-service de channel_test.go (F5,
// parseChannelFixture/channelFixtureAlphaModDs/channelFixtureBetaPolicyDs)
// acrescentando só `provider: "rabbitmq"` + `connection: env("AMQP_URL")`
// ao canal da topologia; channelFixtureTopologyDs (sem provider, a fixture
// original) continua passando pelos MESMOS testes de channel_test.go
// inalterada — a prova viva de NFR-21 (o mesmo programa sem provider ⇒ nada
// muda).

// channelFixtureRabbitMQTopologyDs é channelFixtureTopologyDs (F5) + o
// provider real (J3.1-J3.4) e a connection por env(...) (R1) — mesmos
// workers/timeout/circuitBreaker, para provar que os DOIS caminhos
// (in-memory e rabbitmq) leem a MESMA config de canal, só trocando o
// construtor.
const channelFixtureRabbitMQTopologyDs = `Topology {
    services {
        AlphaSvc { modules: [Alpha] }
        BetaSvc { modules: [Beta] }
    }
    channels {
        Alpha -> Beta {
            via: queue
            provider: "rabbitmq"
            connection: env("AMQP_URL")
            orderBy: id
            workers { concurrency: 5 maxRate: 100 batchSize: 10 }
            timeout: 10s
            circuitBreaker: { threshold: 5 cooldown: 30s }
        }
    }
}
`

// TestEmitPoliciesRabbitMQChannelGolden prova o lado CONSUMIDOR
// (decl_policy.go:emitPolicyWireFunc, via emitChannelTransportVar): com
// provider "rabbitmq", Wire constrói amqpruntime.NewRabbitMQChannel (não
// runtime.NewQueueChannel) — RabbitMQConfig com Exchange/Queue nomeados
// "Alpha-Beta", Concurrency de workers.concurrency, MaxAttempts/RetryTTL de
// circuitBreaker.threshold/.cooldown, KeyFunc extraindo "Id" (mesmo
// mecanismo de orderBy do caminho in-memory) — SEM ConsumeDisabled (o lado
// consumidor consome de verdade) — e um registry inline com a factory de
// WidgetMade (R8/REQ-43.5, montado estaticamente, nunca via
// contracts.EventRegistry()/EventRegistry() — ver a doc do arquivo
// channel_rabbitmq.go).
func TestEmitPoliciesRabbitMQChannelGolden(t *testing.T) {
	prog := parseChannelFixture(t, channelFixtureRabbitMQTopologyDs)
	model := types.NewModel(prog.Symbols)
	policy := findPolicyDecl(t, prog, "ReactToWidget")

	got, err := codegen.EmitPolicies("beta", []*ast.PolicyDecl{policy}, model, prog.Symbols, prog, "Beta", goname.NewVOOperatorRegistry(), nil, nil)
	if err != nil {
		t.Fatalf("EmitPolicies: erro inesperado: %v", err)
	}
	gotStr := string(got)
	for _, want := range []string{
		"amqpruntime.NewRabbitMQChannel(os.Getenv(\"AMQP_URL\"), amqpruntime.RabbitMQConfig{",
		`Exchange: "Alpha-Beta"`,
		`Queue: "Alpha-Beta"`,
		"Concurrency: 5",
		"MaxAttempts: 5",
		"RetryTTL: time.Duration(30000000000)",
		"case *contracts.WidgetMade:",
		"return fmt.Sprint(e.Id)",
		`"WidgetMade": func() runtime.Event { return &contracts.WidgetMade{} }`,
	} {
		if !strings.Contains(gotStr, want) {
			t.Fatalf("esperava %q no Go gerado, não achei:\n%s", want, gotStr)
		}
	}
	if strings.Contains(gotStr, "ConsumeDisabled") {
		t.Fatalf("lado consumidor não deveria ter ConsumeDisabled (consome de verdade):\n%s", gotStr)
	}
	if strings.Contains(gotStr, "runtime.NewQueueChannel") {
		t.Fatalf("com provider \"rabbitmq\", não deveria mais usar runtime.NewQueueChannel:\n%s", gotStr)
	}
}

// TestGenerateRabbitMQChannelFixtureProducerAndConsumerCompile prova o lado
// PRODUTOR (codegen.go:generateCmdMainFile, via emitChannelTransportVar) e o
// projeto INTEIRO (produtor+consumidor) — mesmo par de
// TestGenerateChannelFixtureProducerAndConsumerCompile (channel_test.go),
// agora com provider "rabbitmq": cmd/alphasvc/main.go injeta seu PRÓPRIO
// amqpruntime.NewRabbitMQChannel com ConsumeDisabled: true (achado desta
// task — ver a doc de RabbitMQConfig.ConsumeDisabled, amqprt/rabbitmq.go.txt:
// o lado produtor só publica, nunca deve competir por mensagens reais com o
// consumidor do outro service) como publisher da unit of work.
//
// Desde K3.2 (ISSUE-9/REQ-51.5, §design correcoes-issues-9-10-11 4.2-P1):
// Alpha (Database postgres real + canal de saída provider:"rabbitmq")
// também satisfaz a condição de ativação de durableProducer (K3.1) — assim
// como a fixture-âncora de J6 (AnchorOrders, anchor_fixture_test.go), então
// sua UnitOfWork TAMBÉM troca de "runtime.NewUnitOfWork(store, alphaChannel)"
// para "sqlruntime.NewUnitOfWork(alphaDB, alpha.EventRegistry(),
// sqlruntime.PostgresDialect(), alphaChannel)" — mudança DELIBERADA desta
// asserção, não uma regressão: "alphaChannel" continua o publisher,
// inalterado (a troca de publisher/enqueue no outbox durável é K3.3, fora
// deste escopo). Prova smoke compile de verdade (importa amqp091-go real).
func TestGenerateRabbitMQChannelFixtureProducerAndConsumerCompile(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":     channelFixtureRabbitMQTopologyDs,
		"alpha/mod.ds":    channelFixtureAlphaModDs,
		"alpha/domain.ds": channelFixtureAlphaDomainDs,
		"beta/mod.ds":     channelFixtureBetaModDs,
		"beta/policy.ds":  channelFixtureBetaPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de canal rabbitmq (J3.4) tem diagnósticos de erro:\n%s", bag.Render())
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
	alphaMainStr := string(alphaMain)
	for _, want := range []string{
		"amqpruntime.NewRabbitMQChannel(os.Getenv(\"AMQP_URL\"), amqpruntime.RabbitMQConfig{",
		`Exchange: "Alpha-Beta"`,
		"ConsumeDisabled: true",
		"sqlruntime.OpenPostgres(",
		"uow := sqlruntime.NewUnitOfWork(alphaDB, alpha.EventRegistry(), sqlruntime.PostgresDialect(), alphaChannel)",
	} {
		if !strings.Contains(alphaMainStr, want) {
			t.Errorf("esperava %q em cmd/alphasvc/main.go, não achei:\n%s", want, alphaMainStr)
		}
	}
	if strings.Contains(alphaMainStr, "runtime.NewQueueChannel") {
		t.Errorf("cmd/alphasvc/main.go com provider \"rabbitmq\" não deveria usar runtime.NewQueueChannel:\n%s", alphaMainStr)
	}
	// K3.2: a UnitOfWork do produtor NÃO deve mais rodar sobre a store em
	// memória (a pré-condição do outbox durável de fato trocou a store).
	if strings.Contains(alphaMainStr, "runtime.NewUnitOfWork(store, alphaChannel)") {
		t.Errorf("cmd/alphasvc/main.go ainda constrói a UnitOfWork do produtor sobre a store em memória (pré-condição K3.2 não aplicada):\n%s", alphaMainStr)
	}

	gentest.SmokeCompile(t, m)
}

// TestChannelRabbitMQUnrecognizedProviderStaysInMemory prova a metade
// NFR-21 de REQ-43.7: um provider declarado mas NÃO reconhecido (ex.
// "kafka", ainda não implementado neste ciclo) cai silenciosamente no
// caminho in-memory de sempre — nunca um erro de geração, nunca
// NewRabbitMQChannel.
func TestChannelRabbitMQUnrecognizedProviderStaysInMemory(t *testing.T) {
	topologyDs := strings.Replace(channelFixtureRabbitMQTopologyDs, `provider: "rabbitmq"`, `provider: "kafka"`, 1)
	prog := parseChannelFixture(t, topologyDs)
	model := types.NewModel(prog.Symbols)
	policy := findPolicyDecl(t, prog, "ReactToWidget")

	got, err := codegen.EmitPolicies("beta", []*ast.PolicyDecl{policy}, model, prog.Symbols, prog, "Beta", goname.NewVOOperatorRegistry(), nil, nil)
	if err != nil {
		t.Fatalf("EmitPolicies: erro inesperado (provider não reconhecido deveria cair no caminho in-memory): %v", err)
	}
	gotStr := string(got)
	if !strings.Contains(gotStr, "runtime.NewQueueChannel") {
		t.Fatalf("provider \"kafka\" (não reconhecido) deveria manter runtime.NewQueueChannel:\n%s", gotStr)
	}
	if strings.Contains(gotStr, "amqpruntime") {
		t.Fatalf("provider \"kafka\" (não reconhecido) não deveria referenciar amqpruntime:\n%s", gotStr)
	}
}
