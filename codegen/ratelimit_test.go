package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/diag"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// ratelimit_test.go prova os critérios de conclusão da task G4 (spec §16,
// §design codegen, REQ-28.4): dimensões (perIp/perUser/perTenant/perApiKey/
// global, múltiplas → todas precisam passar), os três algoritmos
// (token_bucket com burst, sliding_window, fixed_window), tiers de
// RateLimitTier via "rateLimit: byTier"/tenant.tier, resposta 429 +
// Retry-After + X-RateLimit-*, onBackendFailure (open/closed, override por
// endpoint) e a integração com G2 ("retry idempotente não consome cota").
//
// --- A fixture: 3 módulos sintéticos, sem topology.ds (1 grupo default) ---
//
// Billing (token_bucket, o default do mod.ds — sem bloco RateLimit
// nenhum): ChargeCard (idempotency + rateLimit multi-dimensão perIp+global,
// burst) e CloseAccount (idempotency + rateLimit perIp isolado, para o
// teste de replay não consumir cota sem interferência de outro teste).
// Catalog (sliding_window, onBackendFailure: closed no mod.ds — GetProduct
// sobrescreve para "open" no endpoint). Search (fixed_window; SearchEvents
// usa "rateLimit: byTier" contra os RateLimitTier Free/Pro declarados em
// billing/interface.ds). Todos os 3 módulos caem no MESMO grupo default
// (nenhum topology.ds — buildCmdGroups/codegen.go), então share 1 único
// cmd/app/main.go com 1 única Interface HTTP (findGroupInterface só acha a
// PRIMEIRA — por isso todas as rotas vivem num único arquivo,
// billing/interface.ds).
const rateLimitBillingModDs = `Module Billing {
    Database BillingDb {
        provider: "pg"
        manages: [Account]
    }
}
`

const rateLimitBillingDomainDs = `
ValueObject AccountId(string) {
    Valid { value.length() > 0 }
}

ValueObject Amount(integer) {
    Valid { ok }
}

Event Charged {
    id AccountId
    amount Amount
}

Event Closed {
    id AccountId
}

Error AccountNotFound {
    message "account not found"
}

Aggregate Account {
    strategy EventSourced

    state {
        id AccountId
        total Amount
    }

    access {
        Charge requires caller.authenticated
        Close requires caller.authenticated
    }

    Handle Charge(amount Amount) {
        emit Charged(self.id, amount)
    }

    Handle Close() {
        emit Closed(self.id)
    }

    Apply Charged {
        state.total = event.amount
    }

    Apply Closed {
        state.total = Amount(0)
    }
}

Command ChargeCardCmd {
    accountId ref Account
    amount Amount
}

Command CloseAccountCmd {
    accountId ref Account
}

UseCase ChargeCard handles ChargeCardCmd {
    idempotency { required: true, window: 1h }
    execute {
        account = load Account(cmd.accountId)
        ensure account exists else AccountNotFound
        account.Charge(cmd.amount)
    }
}

UseCase CloseAccount handles CloseAccountCmd {
    idempotency { required: true, window: 1h }
    execute {
        account = load Account(cmd.accountId)
        ensure account exists else AccountNotFound
        account.Close()
    }
}
`

const rateLimitBillingInterfaceDs = `
RateLimitTier Free {
    perUser: 100/min
    perTenant: 1000/min
}

RateLimitTier Pro {
    perUser: 1000/min
    perTenant: 20000/min
}

Interface HTTP {
    port: 8080

    POST "/accounts/{id}/charge" -> ChargeCard {
        rateLimit { perIp: 5/min, global: 1000/min, burst: 2 }
    }

    POST "/accounts/{id}/close" -> CloseAccount {
        rateLimit { perIp: 1/min }
    }

    GET "/products/{id}" -> GetProduct {
        rateLimit { perUser: 10/min, onBackendFailure: open }
    }

    GET "/products/{id}/peek" -> PeekProduct {
        rateLimit { perUser: 10/min }
    }

    GET "/search/{id}" -> SearchEvents {
        rateLimit: byTier
    }
}
`

const rateLimitCatalogModDs = `Module Catalog {
    Database CatalogDb {
        provider: "pg"
        manages: [Product]
    }

    RateLimit {
        algorithm: sliding_window
        onBackendFailure: closed
    }
}
`

const rateLimitCatalogDomainDs = `
ValueObject ProductId(string) {
    Valid { value.length() > 0 }
}

ValueObject ProductName(string) {
    Valid { value.length() > 0 }
}

Event ProductRegistered {
    id ProductId
    name ProductName
}

View ProductVW {
    id ProductId
    name ProductName
}

Aggregate Product {
    strategy EventSourced

    state {
        id ProductId
        name ProductName
    }

    access {
        Register requires caller.authenticated
    }

    Handle Register(name ProductName) {
        emit ProductRegistered(self.id, name)
    }

    Apply ProductRegistered {
        state.name = event.name
    }
}

Query GetProduct(id ProductId) -> ProductVW {
    return load Product(id) as ProductVW
}

Query PeekProduct(id ProductId) -> ProductVW {
    return load Product(id) as ProductVW
}
`

const rateLimitSearchModDs = `Module Search {
    Database SearchDb {
        provider: "pg"
        manages: [SearchIndex]
    }

    RateLimit {
        algorithm: fixed_window
    }
}
`

const rateLimitSearchDomainDs = `
ValueObject QueryTermId(string) {
    Valid { value.length() > 0 }
}

ValueObject HitCount(integer) {
    Valid { ok }
}

Event SearchIndexed {
    id QueryTermId
    hits HitCount
}

View SearchResultVW {
    id QueryTermId
    hits HitCount
}

Aggregate SearchIndex {
    strategy EventSourced

    state {
        id QueryTermId
        hits HitCount
    }

    access {
        Index requires caller.authenticated
    }

    Handle Index(hits HitCount) {
        emit SearchIndexed(self.id, hits)
    }

    Apply SearchIndexed {
        state.hits = event.hits
    }
}

Query SearchEvents(id QueryTermId) -> SearchResultVW {
    return load SearchIndex(id) as SearchResultVW
}
`

// rateLimitGenerateOptions espelha walletGenerateOptions/shopGenerateOptions
// (codegen_test.go/decl_policy_test.go) — o mesmo module path que
// RuntimeImportPath assume implicitamente em todo o pacote codegen.
var rateLimitGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// parseRateLimitFixtureProgram monta o projeto sintético (3 módulos, sem
// topology.ds) em disco e o resolve via driver.CheckProject — devolve o bag
// (sem erros, já checado) junto, para Generate reusar (mesmo padrão de
// generateWalletProject, codegen_test.go).
func parseRateLimitFixtureProgram(t *testing.T) (*program.Program, *diag.DiagnosticBag) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"billing/mod.ds":       rateLimitBillingModDs,
		"billing/domain.ds":    rateLimitBillingDomainDs,
		"billing/interface.ds": rateLimitBillingInterfaceDs,
		"catalog/mod.ds":       rateLimitCatalogModDs,
		"catalog/domain.ds":    rateLimitCatalogDomainDs,
		"search/mod.ds":        rateLimitSearchModDs,
		"search/domain.ds":     rateLimitSearchDomainDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de rate limiting (G4) tem diagnósticos de erro:\n%s", bag.Render())
	}
	return prog, bag
}

// generateRateLimitProject roda Generate sobre o Program da fixture —
// mesmo padrão de generateWalletProject (codegen_test.go): G4 mexe em
// codegen/http.go/codegen.go (compartilhados), então só é exercitado de
// verdade pelo pipeline Generate completo, não por uma EmitX singular.
func generateRateLimitProject(t *testing.T) []codegen.File {
	t.Helper()
	prog, bag := parseRateLimitFixtureProgram(t)
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, rateLimitGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de rate limiting: %v", err)
	}
	return files
}

// rateLimitCmdMainFile devolve o conteúdo de cmd/app/main.go — os 3 módulos
// da fixture não declaram Service (sem topology.ds), então caem no grupo
// default; defaultCmdDirName (codegen.go) escolhe "app" quando o grupo tem
// mais de 1 módulo.
func rateLimitCmdMainFile(t *testing.T) []byte {
	t.Helper()
	files := generateRateLimitProject(t)
	for _, f := range files {
		if f.Path == "cmd/app/main.go" {
			return f.Content
		}
	}
	t.Fatal("cmd/app/main.go não encontrado entre os arquivos gerados")
	return nil
}

// --- Golden + determinismo ---------------------------------------------

// TestGenerateRateLimitMainGolden prova, sobre o Go de fato gerado, os
// elementos centrais do critério de conclusão de G4: os três algoritmos
// (token_bucket/sliding_window/fixed_window) com os parâmetros certos
// (contagem/período/burst), FailOpen resolvido corretamente (módulo
// default + override por endpoint), o switch de tiers (byTier) e o peek de
// idempotência gating a checagem de rate limit.
func TestGenerateRateLimitMainGolden(t *testing.T) {
	got := rateLimitCmdMainFile(t)
	s := string(got)
	for _, want := range []string{
		// Billing: token_bucket (default do mod.ds), multi-dimensão
		// perIp+global com burst=2, FailOpen=true (sem bloco RateLimit no
		// mod.ds -> default "open").
		`runtime.NewLimiter("token_bucket", 5, time.Duration(60000000000), 2)`,
		`runtime.NewLimiter("token_bucket", 1000, time.Duration(60000000000), 2)`,
		"chargeCardPerIpLimiter",
		"chargeCardGlobalLimiter",
		"FailOpen: true",
		// CloseAccount: rateLimit isolado (perIp: 1/min, sem burst
		// explícito -> burst 0 repassado, NewLimiter resolve = limit).
		`runtime.NewLimiter("token_bucket", 1, time.Duration(60000000000), 0)`,
		"closeAccountPerIpLimiter",
		// Catalog: sliding_window, onBackendFailure: closed no módulo,
		// override para "open" no endpoint de GetProduct.
		`runtime.NewLimiter("sliding_window", 10, time.Duration(60000000000), 0)`,
		"getProductPerUserLimiter",
		"peekProductPerUserLimiter",
		"checks = append(checks, runtime.RateLimitCheck{Limiter: getProductPerUserLimiter, Key: caller.ID(), FailOpen: true})",
		"checks = append(checks, runtime.RateLimitCheck{Limiter: peekProductPerUserLimiter, Key: caller.ID(), FailOpen: false})",
		// Search: fixed_window, byTier (Free/Pro).
		`runtime.NewLimiter("fixed_window",`,
		"searchEventsFreePerUserLimiter",
		"searchEventsFreePerTenantLimiter",
		"searchEventsProPerUserLimiter",
		"searchEventsProPerTenantLimiter",
		"tenant, ok := runtime.TenantFrom(ctx)",
		`case "Free":`,
		`case "Pro":`,
		// Headers/status 429 (spec §16).
		`w.Header().Set("X-RateLimit-Limit"`,
		`w.Header().Set("X-RateLimit-Remaining"`,
		`w.Header().Set("X-RateLimit-Reset"`,
		`w.Header().Set("Retry-After"`,
		"http.StatusTooManyRequests",
		// Identidade perIp/perApiKey.
		`r.Header.Get("X-Forwarded-For")`,
		`r.Header.Get("X-Api-Key")`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("esperava %q em cmd/app/main.go, não achei:\n%s", want, s)
		}
	}
	// O peek de idempotência (IsReplay) precisa gatear ANTES do dispatch —
	// verificado à parte porque o alias de pacote (chargeCardAlias) é
	// derivado do nome do módulo goname.PackageName("Billing")=="billing".
	if !strings.Contains(s, "billing.ChargeCardIsReplay(ctx, cmd)") {
		t.Fatalf("esperava a checagem de replay ANTES do rate limit para ChargeCard, não achei:\n%s", s)
	}
	if !strings.Contains(s, "billing.CloseAccountIsReplay(ctx, cmd)") {
		t.Fatalf("esperava a checagem de replay ANTES do rate limit para CloseAccount, não achei:\n%s", s)
	}
	gentest.Golden(t, filepath.Join("testdata", "cmd_app_ratelimit_main.go.golden"), got)
}

// TestGenerateRateLimitMainDeterministic prova NFR-13.
func TestGenerateRateLimitMainDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return rateLimitCmdMainFile(t)
	})
}

// TestGenerateRateLimitSmokeCompile prova NFR-14: o projeto inteiro
// (3 módulos + o cmd/app com rate limiting) compila e passa go vet.
func TestGenerateRateLimitSmokeCompile(t *testing.T) {
	files := generateRateLimitProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// --- Comportamental (NFR-15) --------------------------------------------
//
// rateLimitBehaviorTest roda DENTRO do diretório cmd/app do projeto isolado
// gerado (package main, mesmo pacote de main.go — newMux/os limitadores são
// não-exportados) — mesmo padrão de walletHTTPRouteBehaviorTest
// (http_test.go). Prova, sobre o Go de fato gerado (não uma
// reimplementação):
//
//  1. Multi-dimensão AND (ChargeCard, perIp+global, token_bucket burst=2):
//     as 2 primeiras chamadas passam (consomem o burst), a 3ª é negada com
//     429 + Retry-After + X-RateLimit-* — E os headers de sucesso também
//     aparecem nas 2 primeiras.
//  2. Retry idempotente não consome cota (CloseAccount, perIp limit=1,
//     G2+G4): a 1ª chamada consome o único token; a 2ª, com a MESMA
//     Idempotency-Key (um replay conhecido), sucede MESMO com a cota
//     esgotada — nunca chega a chamar runtime.CheckRateLimits; uma 3ª
//     chamada com uma chave NOVA (não é replay) já leva 429, porque a cota
//     de fato não foi reposta pela 1ª chamada.
//  3. onBackendFailure override por endpoint (Catalog: módulo default
//     "closed", GetProduct sobrescreve para "open", PeekProduct herda o
//     default): com os dois limitadores trocados por um Limiter quebrado
//     (erro simulado), GetProduct passa (fail-open) e PeekProduct é negado
//     (fail-closed) — a MESMA falha de backend, dois resultados opostos,
//     conforme a config resolvida por rota.
//  4. Tier resolution (SearchEvents, "rateLimit: byTier", fixed_window):
//     sem tenant no ctx (o caso de hoje, G5 ainda não aterrissou — mesmo
//     placeholder documentado de G3), a rota passa direto, sem nenhuma
//     dimensão aplicada; com um Tenant{Tier: "Pro"} simulado via
//     runtime.WithTenant (a resolução de verdade na borda é G5 — aqui só
//     provamos que, uma vez presente, o tier CERTO é escolhido e aplicado),
//     o limite reportado é o do tier Pro (1000); um tier desconhecido
//     ("Enterprise") faz o mesmo "graceful skip" de tenant ausente.
const rateLimitBehaviorTest = `package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"domainscript/generated/billing"
	"domainscript/generated/runtime"
)

// newTestMux replica o wiring que func main() faz antes de subir o server
// (store -> uow -> billing.Wire(uow), codegen.go/generateCmdMainFile) — sem
// isso, a var de pacote "uow" (runtime.UnitOfWork) do módulo billing fica
// zero-value e ChargeCard/CloseAccount (que abrem uow.Run) sofrem nil
// pointer dereference. Catalog/Search não usam UseCase (só Query), então
// não precisam de Wire nenhum.
func newTestMux(store runtime.EventStore) *http.ServeMux {
	billing.Wire(runtime.NewUnitOfWork(store))
	return newMux(store)
}

func TestChargeCardMultiDimensionRateLimitAndBurst(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	makeReq := func(accountID string) *httptest.ResponseRecorder {
		body := bytes.NewBufferString(` + "`" + `{"amount":10}` + "`" + `)
		req := httptest.NewRequest(http.MethodPost, "/accounts/"+accountID+"/charge", body)
		req.Header.Set("X-Caller-Id", "tester")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	for i, accountID := range []string{"a1", "a2"} {
		rec := makeReq(accountID)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("chamada %d (%s): NÃO esperava 429 dentro do burst; body: %s", i+1, accountID, rec.Body.String())
		}
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Fatalf("chamada %d: esperava o header X-RateLimit-Limit também no caminho de sucesso", i+1)
		}
	}

	rec := makeReq("a3")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3ª chamada: status = %d, want 429 (burst=2 esgotado em perIp/global); body: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("esperava o header Retry-After no 429 (spec §16)")
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("X-RateLimit-Remaining = %q, want \"0\"", rec.Header().Get("X-RateLimit-Remaining"))
	}
}

func TestCloseAccountIdempotentReplaySkipsQuota(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	makeReq := func(idemKey string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/accounts/b1/close", nil)
		req.Header.Set("X-Caller-Id", "tester")
		req.Header.Set("Idempotency-Key", idemKey)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec1 := makeReq("close-key-1")
	if rec1.Code == http.StatusTooManyRequests {
		t.Fatalf("1ª chamada NÃO deveria ter sido negada (1º uso da cota); body: %s", rec1.Body.String())
	}

	// MESMA chave + MESMO Command -> replay conhecido (G2). NÃO deveria ser
	// negada mesmo com perIp (limit=1) já esgotado pela 1ª — spec §14/§16:
	// "retry idempotente não consome cota".
	rec2 := makeReq("close-key-1")
	if rec2.Code == http.StatusTooManyRequests {
		t.Fatalf("2ª chamada (replay idempotente, MESMA chave) NÃO deveria ter sido negada por rate limit; body: %s", rec2.Body.String())
	}

	// Chave NOVA (fresh, não é replay) do MESMO IP -> a cota real (1/min)
	// já foi consumida pela 1ª chamada e nunca foi reposta -> negada.
	rec3 := makeReq("close-key-2")
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("3ª chamada (chave NOVA, cota já esgotada pela 1ª): status = %d, want 429; body: %s", rec3.Code, rec3.Body.String())
	}
}

// brokenLimiter simula um backend de rate limit indisponível — todo Allow
// devolve erro, exercitando onBackendFailure (spec §16).
type brokenLimiter struct{}

func (brokenLimiter) Allow(ctx context.Context, key string) (bool, runtime.RateLimitResult, error) {
	return false, runtime.RateLimitResult{}, errors.New("rate limit backend indisponível (simulado)")
}

func TestOnBackendFailureOverridePerEndpoint(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	origGet, origPeek := getProductPerUserLimiter, peekProductPerUserLimiter
	defer func() {
		getProductPerUserLimiter = origGet
		peekProductPerUserLimiter = origPeek
	}()
	getProductPerUserLimiter = brokenLimiter{}
	peekProductPerUserLimiter = brokenLimiter{}

	get := httptest.NewRequest(http.MethodGet, "/products/p1", nil)
	get.Header.Set("X-Caller-Id", "tester")
	recGet := httptest.NewRecorder()
	mux.ServeHTTP(recGet, get)
	if recGet.Code == http.StatusTooManyRequests {
		t.Fatalf("GetProduct (onBackendFailure: open, override por endpoint) NÃO deveria ter sido bloqueado por um backend quebrado; body: %s", recGet.Body.String())
	}

	peek := httptest.NewRequest(http.MethodGet, "/products/p1/peek", nil)
	peek.Header.Set("X-Caller-Id", "tester")
	recPeek := httptest.NewRecorder()
	mux.ServeHTTP(recPeek, peek)
	if recPeek.Code != http.StatusTooManyRequests {
		t.Fatalf("PeekProduct (onBackendFailure: closed, default do módulo Catalog) DEVERIA ter sido bloqueado por um backend quebrado; status = %d, body: %s", recPeek.Code, recPeek.Body.String())
	}
}

func TestSearchEventsByTierResolution(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	// Sem tenant/tier resolvido (o caso de hoje — G5 ainda não aterrissou):
	// nenhuma dimensão de tier se aplica, a requisição passa direto.
	reqNoTenant := httptest.NewRequest(http.MethodGet, "/search/term1", nil)
	reqNoTenant.Header.Set("X-Caller-Id", "tester")
	recNoTenant := httptest.NewRecorder()
	mux.ServeHTTP(recNoTenant, reqNoTenant)
	if recNoTenant.Code == http.StatusTooManyRequests {
		t.Fatalf("sem tenant/tier resolvido, a rota byTier NUNCA deveria bloquear; body: %s", recNoTenant.Body.String())
	}

	// Tenant com tier "Pro" (simulado via runtime.WithTenant — a resolução
	// de verdade na borda é G5; aqui só provamos que, uma vez presente, o
	// tier CERTO é escolhido e aplicado).
	reqPro := httptest.NewRequest(http.MethodGet, "/search/term2", nil)
	reqPro.Header.Set("X-Caller-Id", "tester")
	reqPro = reqPro.WithContext(runtime.WithTenant(reqPro.Context(), runtime.Tenant{ID: "tenant1", Tier: "Pro"}))
	recPro := httptest.NewRecorder()
	mux.ServeHTTP(recPro, reqPro)
	if recPro.Code == http.StatusTooManyRequests {
		t.Fatalf("tier Pro (limite generoso) NÃO deveria ter bloqueado a 1ª chamada; body: %s", recPro.Body.String())
	}
	if got := recPro.Header().Get("X-RateLimit-Limit"); got != "1000" {
		t.Fatalf("X-RateLimit-Limit = %q, want \"1000\" (RateLimitTier Pro, perUser: 1000/min)", got)
	}

	// Tier desconhecido ("Enterprise" não é nem Free nem Pro) -> mesmo
	// "graceful skip" de tenant ausente, nunca um erro.
	reqUnknown := httptest.NewRequest(http.MethodGet, "/search/term3", nil)
	reqUnknown.Header.Set("X-Caller-Id", "tester")
	reqUnknown = reqUnknown.WithContext(runtime.WithTenant(reqUnknown.Context(), runtime.Tenant{ID: "tenant2", Tier: "Enterprise"}))
	recUnknown := httptest.NewRecorder()
	mux.ServeHTTP(recUnknown, reqUnknown)
	if recUnknown.Code == http.StatusTooManyRequests {
		t.Fatalf("tier desconhecido deveria também passar direto (graceful skip); body: %s", recUnknown.Body.String())
	}
}
`

// TestGenerateRateLimitBehavior prova NFR-15 sobre o rate limiting de
// verdade: roda `go test ./...` sobre o projeto isolado gerado, com
// rateLimitBehaviorTest escrito em cmd/app (mesmo pacote de newMux).
func TestGenerateRateLimitBehavior(t *testing.T) {
	files := filesToMap(generateRateLimitProject(t))
	files[filepath.Join("cmd", "app", "main_ratelimit_behavior_test.go")] = []byte(rateLimitBehaviorTest)
	runGeneratedTests(t, files)
}
