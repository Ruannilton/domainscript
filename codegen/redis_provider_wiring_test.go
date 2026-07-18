package codegen_test

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/diag"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/token"
	"domainscript/types"
)

// redis_provider_wiring_test.go prova os critérios de conclusão de J4.3
// ("Seleção + wiring", REQ-44.4, R1/R3, §design infra-providers 3.4): que
// decl_query_cache.go/ratelimit.go trocam o construtor in-memory pelo
// backend Redis (J4.1/J4.2) quando (e só quando) o mod.ds declara
// `Cache { backend: "redis" }`/`RateLimit { backend: "redis" }`, com a URL
// de conexão resolvida via `env(...)` (R1) — e que, sem esse bloco, o
// caminho in-memory de sempre continua byte-idêntico (NFR-21/23, já provado
// pelos testes de decl_query_cache_test.go/ratelimit_test.go continuando
// verdes sem nenhuma alteração após esta task).

// --- R3: o bloco Cache/RateLimit de módulo aceita "connection: env(...)" ---

// redisWiringCacheModDs declara Cache{backend:"redis"} no mod.ds de um
// módulo já existente (cacheFixtureModDs/cacheFixtureSrc, decl_query_cache_
// test.go) — reusa a MESMA fixture de domínio (Widget, GetWidget/
// GetWidgetAutoInvalidate) para provar que o wiring redis se aplica às
// Queries cacheadas de sempre, sem duplicar a fixture inteira.
const redisWiringCacheModDs = `Module Widgets {
    Database WidgetsDb {
        provider: "postgres"
        manages: [Widget]
    }
    Cache {
        backend: "redis"
        connection: env("REDIS_URL")
    }
}
`

// parseCacheRedisFixture monta o projeto (mod.ds com Cache{backend:"redis"}
// + domain.ds de cacheFixtureSrc) e resolve via driver.CheckProject.
func parseCacheRedisFixture(t *testing.T) *program.Program {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    redisWiringCacheModDs,
		"domain.ds": cacheFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de cache Redis (J4.3) tem diagnósticos de erro:\n%s", bag.Render())
	}
	return prog
}

// TestCacheModuleBlockAcceptsEnvConnection prova a pré-condição R3 (§design
// infra-providers §7): uma entry "connection: env(\"REDIS_URL\")" dentro do
// bloco "Cache { ... }" do mod.ds chega intacta em ConfigBlock.Entries — o
// ÚNICO ponto que, não chegando, teria exigido mudança de front-end (nenhuma
// foi necessária: o parser já aceita config livre).
func TestCacheModuleBlockAcceptsEnvConnection(t *testing.T) {
	prog := parseCacheRedisFixture(t)
	mod := prog.Modules["Widgets"]
	if mod == nil || mod.Decl == nil {
		t.Fatal("esperava o módulo Widgets resolvido com Decl não nulo")
	}

	var block *ast.ConfigBlock
	for _, b := range mod.Decl.Blocks {
		if b.Kind == "Cache" {
			block = b
		}
	}
	if block == nil {
		t.Fatal("esperava um bloco \"Cache\" em mod.Decl.Blocks, não achei nenhum")
	}

	var backendEntry, connEntry *ast.ConfigEntry
	for i := range block.Entries {
		switch block.Entries[i].Key {
		case "backend":
			backendEntry = &block.Entries[i]
		case "connection":
			connEntry = &block.Entries[i]
		}
	}
	if backendEntry == nil {
		t.Fatal(`esperava a entry "backend" em Cache.Entries, não achei`)
	}
	if lit, ok := backendEntry.Value.(*ast.Literal); !ok || lit.Kind != token.STRING || lit.Value != "redis" {
		t.Fatalf(`esperava backend como literal STRING "redis", veio %#v`, backendEntry.Value)
	}
	if connEntry == nil {
		t.Fatal(`esperava a entry "connection" em Cache.Entries, não achei — R3 falharia (env(...) não chega em Decl.Entries)`)
	}
	call, ok := connEntry.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf(`esperava connection como CallExpr "env(...)", veio %T`, connEntry.Value)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != "env" {
		t.Fatalf(`esperava Fn "env", veio %#v`, call.Fn)
	}
	if len(call.Args) != 1 {
		t.Fatalf("esperava exatamente 1 argumento em env(...), veio %d", len(call.Args))
	}
	argLit, ok := call.Args[0].Value.(*ast.Literal)
	if !ok || argLit.Kind != token.STRING || argLit.Value != "REDIS_URL" {
		t.Fatalf(`esperava env("REDIS_URL"), veio %#v`, call.Args[0].Value)
	}
}

// --- Cache: seleção do backend redis (decl_query_cache.go) ---

// TestEmitQueryCacheRedisBackendGolden prova, sobre o Go de fato gerado, a
// seleção do backend Redis para as 2 Queries cacheadas da fixture: cada uma
// abre sua PRÓPRIA conexão (redisruntime.OpenClient sobre
// os.Getenv("REDIS_URL"), R1), falha o startup (panic) se a conexão falhar,
// registra seu tipo de retorno via encoding/gob (exigido por
// redisruntime.NewRedisQueryCache — ver a doc de redisrt/cache.go.txt) e usa
// redisruntime.NewRedisQueryCache no lugar de runtime.NewMemoryQueryCache.
func TestEmitQueryCacheRedisBackendGolden(t *testing.T) {
	prog := parseCacheRedisFixture(t)
	got := emitCacheQueries(t, prog)
	s := string(got)

	for _, want := range []string{
		`var getWidgetCacheRedisClient, getWidgetCacheRedisClientErr = redisruntime.OpenClient(os.Getenv("REDIS_URL"))`,
		"if getWidgetCacheRedisClientErr != nil {",
		"panic(getWidgetCacheRedisClientErr)",
		"gob.Register(WidgetView{})",
		`var getWidgetCache = redisruntime.NewRedisQueryCache(getWidgetCacheRedisClient, "GetWidget")`,
		`var getWidgetAutoInvalidateCacheRedisClient, getWidgetAutoInvalidateCacheRedisClientErr = redisruntime.OpenClient(os.Getenv("REDIS_URL"))`,
		`var getWidgetAutoInvalidateCache = redisruntime.NewRedisQueryCache(getWidgetAutoInvalidateCacheRedisClient, "GetWidgetAutoInvalidate")`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("esperava %q no Go gerado, não achei:\n%s", want, s)
		}
	}
	if strings.Contains(s, "runtime.NewMemoryQueryCache()") {
		t.Fatalf("com Cache{backend:\"redis\"}, não deveria mais usar runtime.NewMemoryQueryCache():\n%s", s)
	}
	// O wrapper público (Get/Coalesce/Set/SetErr) continua sendo o MESMO —
	// só a var de cache que ele referencia mudou de backend.
	if !strings.Contains(s, "func GetWidget(ctx context.Context, store runtime.EventStore, widgetId WidgetId) (WidgetView, error) {") {
		t.Fatalf("esperava o wrapper público GetWidget inalterado:\n%s", s)
	}
}

// TestGenerateCacheRedisBackendSmokeCompile prova que o PROJETO INTEIRO,
// gerado com Cache{backend:"redis"} no mod.ds (ativando cacheProviders
// ["redis"] via activeProviderDeps — go.mod ganha go-redis/v9,
// redisruntime/*.go é vendorado), COMPILA de verdade — mesma técnica de
// TestGenerateRabbitMQChannelFixtureProducerAndConsumerCompile
// (channel_rabbitmq_test.go): driver.CheckProject + codegen.Generate +
// gentest.SmokeCompile sobre os bytes de fato escritos, nenhuma conexão
// Redis real é aberta (SmokeCompile só builda).
func TestGenerateCacheRedisBackendSmokeCompile(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    redisWiringCacheModDs,
		"domain.ds": cacheFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de cache Redis (J4.3) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, rateLimitGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de cache Redis: %v", err)
	}
	gentest.SmokeCompile(t, filesToMap(files))
}

// --- RateLimit: seleção do backend redis (ratelimit.go) ---

const rlRedisModDs = `Module Orders {
    Database OrdersDb {
        provider: "pg"
        manages: [Order]
    }
    RateLimit {
        backend: "redis"
        connection: env("REDIS_URL")
    }
}
`

const rlRedisDomainDs = `
ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

ValueObject OrderTotal(integer) {
    Valid { ok }
}

Event OrderPlaced {
    id OrderId
    total OrderTotal
}

Aggregate Order {
    strategy EventSourced

    state {
        id OrderId
        total OrderTotal
    }

    access {
        Place requires caller.authenticated
    }

    Handle Place(total OrderTotal) {
        emit OrderPlaced(self.id, total)
    }

    Apply OrderPlaced {
        state.total = event.total
    }
}

Command PlaceOrderCmd {
    orderId ref Order
    total OrderTotal
}

UseCase PlaceOrder handles PlaceOrderCmd {
    execute {
        order = load Order(cmd.orderId)
        order.Place(cmd.total)
    }
}
`

const rlRedisInterfaceDs = `Interface HTTP {
    port: 8080

    POST "/orders/{id}" -> PlaceOrder {
        rateLimit { perIp: 5/min }
    }
}
`

// parseRateLimitRedisFixtureProgram monta o projeto sintético (1 módulo,
// RateLimit{backend:"redis"}) — mesmo padrão de
// parseRateLimitFixtureProgram (ratelimit_test.go), reduzido a 1 módulo/1
// rota porque só a seleção de backend está em jogo aqui (os 3 algoritmos/
// byTier/tiers já são cobertos, in-memory, por ratelimit_test.go).
func parseRateLimitRedisFixtureProgram(t *testing.T) (*program.Program, *diag.DiagnosticBag) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"orders/mod.ds":       rlRedisModDs,
		"orders/domain.ds":    rlRedisDomainDs,
		"orders/interface.ds": rlRedisInterfaceDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de rate limit Redis (J4.3) tem diagnósticos de erro:\n%s", bag.Render())
	}
	return prog, bag
}

// TestGenerateRateLimitRedisBackendGolden prova, sobre cmd/app/main.go de
// fato gerado, a seleção do backend Redis para o limitador perIp de
// PlaceOrder: conexão própria (redisruntime.OpenClient sobre
// os.Getenv("REDIS_URL"), R1), panic no startup em caso de falha, e
// redisruntime.NewRedisLimiter no lugar de runtime.NewLimiter — preservando
// o MESMO contrato de checks/headers/429 que ratelimit_test.go já prova
// para o caminho in-memory (esta task só troca o construtor do Limiter, não
// a lógica de borda, REQ-44.3).
func TestGenerateRateLimitRedisBackendGolden(t *testing.T) {
	prog, bag := parseRateLimitRedisFixtureProgram(t)
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, rateLimitGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de rate limit Redis: %v", err)
	}
	m := filesToMap(files)
	mainGo, ok := m["cmd/orders/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/orders/main.go, não achei:\n%v", filePathsForTest(files))
	}
	s := string(mainGo)
	for _, want := range []string{
		`var placeOrderPerIpLimiterRedisClient, placeOrderPerIpLimiterRedisClientErr = redisruntime.OpenClient(os.Getenv("REDIS_URL"))`,
		"if placeOrderPerIpLimiterRedisClientErr != nil {",
		"panic(placeOrderPerIpLimiterRedisClientErr)",
		`var placeOrderPerIpLimiter runtime.Limiter = redisruntime.NewRedisLimiter(placeOrderPerIpLimiterRedisClient, "placeOrderPerIpLimiter", "token_bucket", 5, time.Duration(60000000000), 0)`,
		"checks = append(checks, runtime.RateLimitCheck{Limiter: placeOrderPerIpLimiter, Key: rateLimitClientIP(r), FailOpen: true})",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("esperava %q em cmd/orders/main.go, não achei:\n%s", want, s)
		}
	}
	if strings.Contains(s, `runtime.NewLimiter("token_bucket"`) {
		t.Fatalf("com RateLimit{backend:\"redis\"}, não deveria mais usar runtime.NewLimiter:\n%s", s)
	}
}

// TestGenerateRateLimitRedisBackendSmokeCompile prova que o PROJETO INTEIRO
// COMPILA de verdade com o backend Redis selecionado para RateLimit — mesma
// técnica de TestGenerateCacheRedisBackendSmokeCompile acima.
func TestGenerateRateLimitRedisBackendSmokeCompile(t *testing.T) {
	prog, bag := parseRateLimitRedisFixtureProgram(t)
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, rateLimitGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de rate limit Redis: %v", err)
	}
	gentest.SmokeCompile(t, filesToMap(files))
}
