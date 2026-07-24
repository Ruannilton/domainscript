package codegen

import (
	"testing"

	"domainscript/ast"
	"domainscript/program"
	"domainscript/token"
)

// durable_producer_test.go prova a DoD de K3.1 (§design
// correcoes-issues-9-10-11 4.1, REQ-51 condição de ativação):
// durableProducer é um predicado puro sobre *program.Program — nenhuma
// emissão, nenhum arquivo gerado. Construído diretamente sobre o modelo
// program.Program (mesmo padrão leve de sql_wiring_test.go), sem passar pelo
// driver/parser inteiro — o predicado não depende de nada além do grafo já
// resolvido.

// rabbitmqChannelDecl monta um *ast.ChannelDef com `provider: "<provider>"`
// (ou nenhuma entrada "provider" quando provider == "", reproduzindo a
// QueueChannel in-memory que o `shop` de fato produz — sem a chave, não um
// valor vazio, para bater com channelProviderKind/channelProvider de
// verdade, ver a nota do design sobre "a forma do shop").
func rabbitmqChannelDecl(provider string) *ast.ChannelDef {
	var entries []ast.ConfigEntry
	if provider != "" {
		entries = append(entries, ast.ConfigEntry{
			Key:   "provider",
			Value: ast.NewLiteral(token.STRING, provider, ast.Span{}),
		})
	}
	return ast.NewChannelDef("Orders", "Shipping", entries, ast.Span{})
}

// durableProducerFixture monta um *program.Program com um único módulo
// "Orders": dbProviders vira um Database real por entrada (nome DBn,
// provider dbProviders[n]); outbound, se != "", vira o único canal de saída
// Orders -> Shipping via "queue" com o provider dado ("" reproduz a
// QueueChannel in-memory sem `provider:`, "" mesmo assim ainda é `via:
// queue` — só não tem um provider real).
func durableProducerFixture(dbProviders []string, hasChannel bool, channelProvider string) *program.Program {
	dbs := make(map[string]*program.Database, len(dbProviders))
	for i, p := range dbProviders {
		name := "DB" + string(rune('0'+i))
		dbs[name] = &program.Database{Name: name, Module: "Orders", Provider: p, Manages: []string{"Order"}}
	}

	p := &program.Program{
		Modules: map[string]*program.Module{
			"Orders": {Name: "Orders", Databases: dbs},
		},
	}
	if hasChannel {
		p.Channels = append(p.Channels, &program.Channel{
			From: "Orders",
			To:   "Shipping",
			Via:  "queue",
			Decl: rabbitmqChannelDecl(channelProvider),
		})
	}
	return p
}

// TestDurableProducerPostgresPlusRabbitMQ prova o caso positivo do §4.1: 1
// Database postgres real + 1 canal de saída via queue provider:"rabbitmq"
// ⇒ true.
func TestDurableProducerPostgresPlusRabbitMQ(t *testing.T) {
	prog := durableProducerFixture([]string{"postgres"}, true, "rabbitmq")
	got, err := durableProducer(prog, "Orders")
	if err != nil {
		t.Fatalf("durableProducer: erro inesperado: %v", err)
	}
	if !got {
		t.Fatal("durableProducer(postgres + rabbitmq) = false, want true")
	}
}

// TestDurableProducerInMemoryChannelIsNotDurable prova o "gotcha" central do
// §4.1 — a forma real do `shop`: 1 Database postgres real, mas o canal de
// saída é `via: queue` SEM `provider:` (a QueueChannel in-memory, mesmo
// processo) ⇒ false. Não basta checar a presença de `via: queue`.
func TestDurableProducerInMemoryChannelIsNotDurable(t *testing.T) {
	prog := durableProducerFixture([]string{"postgres"}, true, "")
	got, err := durableProducer(prog, "Orders")
	if err != nil {
		t.Fatalf("durableProducer: erro inesperado: %v", err)
	}
	if got {
		t.Fatal("durableProducer(postgres + canal in-memory sem provider) = true, want false (não é transporte durável)")
	}
}

// TestDurableProducerUnrecognizedProviderIsNotDurable é a variação "provider
// declarado mas não reconhecido" do mesmo gotcha (ex. "kafka", ainda não
// implementado) — channelProviderKind já normaliza isso para "" (NFR-21),
// então durableProducer também deve devolver false.
func TestDurableProducerUnrecognizedChannelProviderIsNotDurable(t *testing.T) {
	prog := durableProducerFixture([]string{"postgres"}, true, "kafka")
	got, err := durableProducer(prog, "Orders")
	if err != nil {
		t.Fatalf("durableProducer: erro inesperado: %v", err)
	}
	if got {
		t.Fatal("durableProducer(postgres + canal provider:\"kafka\" não reconhecido) = true, want false")
	}
}

// TestDurableProducerNoRealDatabase prova a metade "sem Database real" do
// §4.1: zero Databases, ou só providers não reconhecidos (ex. "mongodb"),
// mesmo com um canal rabbitmq de verdade ⇒ false.
func TestDurableProducerNoRealDatabase(t *testing.T) {
	cases := []struct {
		name string
		dbs  []string
	}{
		{"sem-database", nil},
		{"provider-nao-reconhecido", []string{"mongodb"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := durableProducerFixture(c.dbs, true, "rabbitmq")
			got, err := durableProducer(prog, "Orders")
			if err != nil {
				t.Fatalf("durableProducer: erro inesperado: %v", err)
			}
			if got {
				t.Fatalf("durableProducer(%s + rabbitmq) = true, want false (sem Database real)", c.name)
			}
		})
	}
}

// TestDurableProducerNoOutboundChannel prova a metade "sem canal" do §4.1: 1
// Database real, mas nenhum canal de saída a partir do módulo (a forma do
// `wallet`) ⇒ false.
func TestDurableProducerNoOutboundChannel(t *testing.T) {
	prog := durableProducerFixture([]string{"postgres"}, false, "")
	got, err := durableProducer(prog, "Orders")
	if err != nil {
		t.Fatalf("durableProducer: erro inesperado: %v", err)
	}
	if got {
		t.Fatal("durableProducer(postgres, sem canal de saída) = true, want false")
	}
}

// TestDurableProducerTwoRealDatabasesIsNotSingleDatabaseProducer prova a
// decisão documentada no comentário de durableProducer: 2+ Database reais no
// MESMO módulo é a forma XA/2PC (usecase2PCPlan/emitXADatabaseWiring,
// pré-existente) — não a forma "banco único, não-2PC" que REQ-51 endereça.
// durableProducer devolve false para não colidir/duplicar o reconhecimento
// já feito pelo caminho 2PC existente, mesmo com um canal rabbitmq de
// verdade presente.
func TestDurableProducerTwoRealDatabasesIsNotSingleDatabaseProducer(t *testing.T) {
	prog := durableProducerFixture([]string{"postgres", "sqlite"}, true, "rabbitmq")
	got, err := durableProducer(prog, "Orders")
	if err != nil {
		t.Fatalf("durableProducer: erro inesperado: %v", err)
	}
	if got {
		t.Fatal("durableProducer(2 Databases reais + rabbitmq) = true, want false (forma XA/2PC, fora do escopo de REQ-51)")
	}
}
