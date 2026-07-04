package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// --- 3. Comportamento de verdade (o coração da task). ---
//
// notesHTTPBehaviorTest roda DENTRO do diretório cmd/notes (mesmo padrão de
// walletHTTPRouteBehaviorTest, http_test.go): confirma, sobre o mux de fato
// gerado (newMux + newTestMux, que replica o wiring que main() faz — store,
// dispatcher, uow, Wire, WireQueryCache, o mesmo padrão que
// TestGenerateRateLimitBehavior/TestGenerateWalletHTTPBehavior já usam):
//
//  1. tenant ausente numa rota que o exige -> 400 fail-closed (§13.4), ANTES
//     de qualquer dispatch.
//  2. mesmo tenant, criar e espiar (CreateNote -> PeekNoteView) -> sucesso.
//  3. tenant DIFERENTE espiando o MESMO id -> 404 (nunca 403 — evita
//     enumeração, §13.2) — o filtro row_level em ação.
//  4. G3 (spec §15): a MESMA chamada de novo pelo tenant DONO ainda synonym
//     200 com o mesmo valor (cache não foi invalidado/corrompido pela
//     tentativa cross-tenant do passo 3) — prova que a chave de cache
//     (runtime.CacheKey inclui tenant.ID, ver notes/queries.go) isola os dois
//     tenants: se não isolasse, o passo 3 teria vazado o valor cacheado do
//     dono em vez de 404.
//  5. G4 (spec §16, perTenant: 2/min): a essa altura o tenant dono já
//     consumiu 2 unidades de cota em "/notes/{id}/view" (passos 2 e 4) — uma
//     3ª chamada dele bloqueia (429); o tenant cross-tenant do passo 3 só
//     consumiu 1 até aqui — uma 2ª chamada dele NÃO bloqueia, confirmando
//     contadores independentes por tenant (perTenant de verdade, não uma
//     cota global disfarçada).
//  6. "/ping" ("tenancy: none") funciona sem NENHUM header de tenant.
const notesHTTPBehaviorTest = `package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"domainscript/generated/notes"
	"domainscript/generated/runtime"
)

// newTestMux replica o wiring que main() faz antes de subir o server (store
// -> dispatcher -> uow -> notes.Wire(uow)/notes.WireQueryCache(dispatcher),
// codegen.go/generateCmdMainFile) — sem isso, "var uow"/o cache de
// PeekNoteView não têm dispatcher/uow reais.
func newTestMux(store runtime.EventStore) *http.ServeMux {
	dispatcher := runtime.NewDispatcher()
	uow := runtime.NewUnitOfWork(store, dispatcher)
	notes.Wire(uow)
	notes.WireQueryCache(dispatcher)
	return newMux(store)
}

func TestTenantRequiredRouteFailsClosedWithoutHeader(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	body := bytes.NewBufferString(` + "`" + `{"text":"sem tenant"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/notes/n1", body)
	req.Header.Set("X-Caller-Id", "u1")
	// SEM "X-Tenant-Id".
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (fail-closed, §13.4, tenant ausente); body: %s", rec.Code, rec.Body.String())
	}
}

func TestRowLevelTenancyIsolatesAndCachesPerTenant(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	create := func(tenant, id, text string) int {
		body := bytes.NewBufferString(` + "`" + `{"text":"` + "`" + ` + text + ` + "`" + `"}` + "`" + `)
		req := httptest.NewRequest(http.MethodPost, "/notes/"+id, body)
		req.Header.Set("X-Caller-Id", "u1")
		req.Header.Set("X-Tenant-Id", tenant)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	view := func(tenant, id string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, "/notes/"+id+"/view", nil)
		req.Header.Set("X-Caller-Id", "u1")
		req.Header.Set("X-Tenant-Id", tenant)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// 2. Tenant "acme" cria e espia sua própria note.
	if code := create("acme", "n1", "hello"); code != http.StatusNoContent {
		t.Fatalf("CreateNote (tenant acme) status = %d, want 204", code)
	}
	code, viewBody := view("acme", "n1")
	if code != http.StatusOK {
		t.Fatalf("PeekNoteView (tenant acme, dono) status = %d, want 200; body: %s", code, viewBody)
	}
	var got struct {
		Id   string ` + "`json:\"id\"`" + `
		Text string ` + "`json:\"text\"`" + `
	}
	if err := json.Unmarshal([]byte(viewBody), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v; body: %s", err, viewBody)
	}
	if got.Id != "n1" || got.Text != "hello" {
		t.Fatalf("view = %+v, want {Id:n1 Text:hello}", got)
	}

	// 3. Tenant DIFERENTE ("other") tentando espiar a MESMA note -> 404, não
	// 403 (row_level: acesso a aggregate de outro tenant é indistinguível de
	// "nunca existiu", §13.2).
	code, otherBody := view("other", "n1")
	if code != http.StatusNotFound {
		t.Fatalf("PeekNoteView (tenant other, cross-tenant) status = %d, want 404 (nunca 403 — evita enumeração); body: %s", code, otherBody)
	}

	// 4. O dono ainda enxerga o valor certo — a tentativa cross-tenant do
	// passo 3 não vazou nem corrompeu o cache do dono (prova que a chave de
	// cache é isolada por tenant, spec §15/G3).
	code, viewBody2 := view("acme", "n1")
	if code != http.StatusOK {
		t.Fatalf("PeekNoteView (tenant acme, 2ª chamada) status = %d, want 200; body: %s", code, viewBody2)
	}
	if viewBody2 != viewBody {
		t.Fatalf("2ª leitura do dono difere da 1ª (cache corrompido pela tentativa cross-tenant?): %s vs %s", viewBody2, viewBody)
	}

	// 5. perTenant: 2/min (spec §16, G4) — "acme" já consumiu 2 unidades
	// (passos 2 e 4 acima) sobre "/notes/{id}/view": uma 3ª bloqueia.
	code, _ = view("acme", "n1")
	if code != http.StatusTooManyRequests {
		t.Fatalf("3ª chamada de \"acme\" a /notes/{id}/view status = %d, want 429 (perTenant: 2/min esgotado)", code)
	}
	// "other" só usou a cota 1 vez (passo 3) — uma 2ª chamada dela NÃO
	// deveria bloquear: contador SEPARADO do de "acme" (perTenant de
	// verdade, não uma cota global disfarçada).
	code, _ = view("other", "n1")
	if code == http.StatusTooManyRequests {
		t.Fatal("2ª chamada de \"other\" a /notes/{id}/view NÃO deveria bloquear (cota perTenant independente da de \"acme\")")
	}
}

func TestTenancyNoneRouteWorksWithoutAnyTenantHeader(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ping (tenancy: none) status = %d, want 200 (sem nenhum header de tenant); body: %s", rec.Code, rec.Body.String())
	}
}
`

// --- 4. cross_tenant: bypass + auditoria, chamando o UseCase gerado direto. ---
//
// notesCrossTenantBehaviorTest roda DENTRO do pacote "notes" (mesmo padrão de
// ledgerSingleDatabaseBehaviorTest, sql_adapter_test.go: chama a função
// gerada direto, contornando a borda HTTP/devCallerFromRequest — necessário
// aqui porque devCallerFromRequest nunca simula HasRole/papéis, ver a doc do
// arquivo).
const notesCrossTenantBehaviorTest = `package notes

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"domainscript/generated/runtime"
)

type tenancyBehaviorCaller struct{ id string }

func (c tenancyBehaviorCaller) Authenticated() bool      { return true }
func (c tenancyBehaviorCaller) ID() string                { return c.id }
func (c tenancyBehaviorCaller) HasRole(role string) bool { return false }

func TestCrossTenantOptInBypassesFilterAndAudits(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	Wire(runtime.NewUnitOfWork(store))

	// "acme" cria a note n2.
	ownerCtx := runtime.WithTenant(runtime.WithCaller(context.Background(), tenancyBehaviorCaller{id: "u1"}), runtime.Tenant{ID: "acme"})
	if err := CreateNote(ownerCtx, CreateNoteCmd{NoteId: NoteId("n2"), Text: NoteText("secreto")}); err != nil {
		t.Fatalf("CreateNote (setup): %v", err)
	}

	// Sem o opt-in, um tenant DIFERENTE tentando tocar a MESMA note continua
	// bloqueado (ErrNotFound) — a mesma garantia de row_level provada via
	// HTTP em TestRowLevelTenancyIsolatesAndCachesPerTenant, agora direto
	// sobre o UseCase de escrita.
	intruderCtx := runtime.WithTenant(runtime.WithCaller(context.Background(), tenancyBehaviorCaller{id: "u2"}), runtime.Tenant{ID: "intruder"})
	err := CreateNote(intruderCtx, CreateNoteCmd{NoteId: NoteId("n2"), Text: NoteText("hackeado")})
	if !errors.Is(err, runtime.ErrNotFound) {
		t.Fatalf("CreateNote (sem cross_tenant, tenant diferente) = %v, want errors.Is(_, runtime.ErrNotFound)", err)
	}

	// Captura a trilha de auditoria (REQ-27.3) via um slog.Handler de teste.
	var logBuf bytes.Buffer
	origLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(origLogger)

	// COM tenancy: cross_tenant, o MESMO cenário (ctx de um tenant diferente
	// do dono) SUCEDE — o filtro row_level foi deliberadamente suspenso.
	if err := AdminTouchNote(intruderCtx, AdminTouchNoteCmd{NoteId: NoteId("n2"), Text: NoteText("tocado pelo admin")}); err != nil {
		t.Fatalf("AdminTouchNote (cross_tenant) não deveria falhar: %v", err)
	}

	logged := logBuf.String()
	for _, want := range []string{"cross-tenant access", "usecase=AdminTouchNote", "tenant=intruder", "caller=u2"} {
		if !bytes.Contains([]byte(logged), []byte(want)) {
			t.Fatalf("trilha de auditoria não contém %q; log completo: %s", want, logged)
		}
	}

	// Confirma que o AdminTouch de fato tocou a note (o bypass permitiu o
	// LOAD; o Handle rodou de verdade, não só "passou por cima" sem efeito).
	view, err := PeekNoteView(ownerCtx, store, NoteId("n2"))
	if err != nil {
		t.Fatalf("PeekNoteView pós AdminTouch: %v", err)
	}
	if view.Text != NoteText("tocado pelo admin") {
		t.Fatalf("Text = %q, want %q (AdminTouch deveria ter sobrescrito via o MESMO Apply de Create)", view.Text, "tocado pelo admin")
	}
}
`

// TestGenerateNotesTenancyBehavior prova NFR-15 sobre multi-tenancy de
// verdade: roda ` + "`go test ./...`" + ` sobre o projeto isolado gerado, com
// notesHTTPBehaviorTest (cmd/notes) e notesCrossTenantBehaviorTest (notes) —
// as DUAS metades comportamentais desta task, no MESMO projeto gerado.
func TestGenerateNotesTenancyBehavior(t *testing.T) {
	files := filesToMap(generateNotesProject(t))
	files["cmd/notes/main_tenancy_behavior_test.go"] = []byte(notesHTTPBehaviorTest)
	files["notes/cross_tenant_behavior_test.go"] = []byte(notesCrossTenantBehaviorTest)
	runGeneratedTests(t, files)
}

// tenancy_test.go prova os critérios de conclusão da task G5 (§design codegen
// 3.12, REQ-27, spec §13): multi-tenancy — tenant real extraído na borda HTTP
// (codegen/http.go), filtro row_level uniforme no runtime.EventStore
// in-memory (codegen/rtsrc/eventstore.go.txt) e no adapter SQL
// (codegen/sqlrt/eventstore.go.txt), acesso a aggregate de outro tenant -> 404
// (nunca 403 — evita enumeração), tenant ausente numa rota que o exige ->
// fail-closed 400, e o opt-in `tenancy: cross_tenant` (bypass do filtro +
// trilha de auditoria via slog). sql_adapter_test.go (G1) tem sua PRÓPRIA
// prova direta do filtro row_level sobre SQLite real (TestSQLEventStoreRowLevelTenancy).
//
// --- A fixture: módulo "Notes" ---
//
// Note é EventSourced (mesma razão de Widget/Ledger em decl_query_cache_test.go/
// sql_adapter_test.go: é a única estratégia que o read side sabe carregar
// hoje). Database NotesDb declara "tenancy: { strategy: row_level, column:
// "tenant_id" }" — a Interface HTTP declara "tenant { from: header("X-Tenant-Id") }"
// (a estratégia mais simples de testar via httptest, sem depender de
// resolução de subdomínio) + "rateLimit { perTenant: 2/min }" (default de
// TODAS as rotas, para provar G4 diferenciando 2 tenants reais).
//
//   - CreateNote (UseCase): caminho de escrita comum, tenant-filtrado
//     (row_level) — o Handle Create dispatch usa "caller", então esta
//     precisa ser mesmo um UseCase (uma "peek"-only sem dispatch de Handle
//     deixaria "caller"/o loaded local declarados e nunca usados — Go não
//     compila; por isso toda leitura desta fixture é uma Query, abaixo).
//   - PeekNoteView (Query com "cache") + View NoteView: prova tanto o
//     row_level (cross-tenant -> 404) quanto G3 (cache chaveado por tenant)
//     — se o cache NÃO diferenciasse por tenant, um tenant enxergaria o
//     resultado cacheado de outro (bug de segurança); o teste comportamental
//     confirma que NÃO enxerga.
//   - AdminTouchNote: `tenancy: cross_tenant` — dispara o Handle AdminTouch,
//     gated por `access { requires caller.authenticated }` (E7.2/§23,
//     inalterado por esta task). REQ-27.3 pede também uma role privilegiada
//     (`caller.hasRole(...)`) — mas essa FORMA de regra de access (chamada
//     de método sobre `caller` num "requires") não é lowerizada pelo
//     codegen hoje (gap pré-existente de E7.2/decl_aggregate.go, não
//     introduzido nem fechado por G5 — só `caller.authenticated` e
//     `caller.id == self.id` são suportados). G5 entrega integralmente sua
//     PRÓPRIA parte do REQ-27.3 (bypass do filtro + trilha de auditoria); o
//     requisito de role fica condicionado a essa lacuna de access ser
//     fechada em outra task — documentado aqui, não escondido. O teste
//     comportamental chama a função gerada AdminTouchNote DIRETO (mesmo
//     padrão de sql_adapter_test.go: behaviorCaller/twoPCCaller chamando
//     PerformDebit/TransferAndPost direto, sem passar pela borda HTTP), o
//     que também contorna devCallerFromRequest (o caller de dev da borda,
//     que nem tenta simular papéis — HasRole sempre false).
//   - "/ping" com "{ tenancy: none }": prova que uma rota pode optar por
//     ficar de fora da exigência de tenant mesmo com a Interface inteira
//     declarando tenancy (§13.1, "rotas sem tenant").
const notesDomainDs = `
ValueObject NoteId(string) {
    Valid { value.length() > 0 }
}

ValueObject NoteText(string) {
    Valid { value.length() > 0 }
}

Event NoteCreated { id NoteId, text NoteText }

Aggregate Note {
    strategy EventSourced

    state {
        id   NoteId
        text NoteText
    }

    access {
        Create     requires caller.authenticated
        AdminTouch requires caller.authenticated
    }

    Handle Create(text NoteText) {
        emit NoteCreated(self.id, text)
    }

    Handle AdminTouch(text NoteText) {
        emit NoteCreated(self.id, text)
    }

    Apply NoteCreated {
        state.text = event.text
    }
}
`

const notesApplicationDs = `
Command CreateNoteCmd {
    noteId ref Note
    text   NoteText
}

Command AdminTouchNoteCmd {
    noteId ref Note
    text   NoteText
}

UseCase CreateNote handles CreateNoteCmd {
    execute {
        note = load Note(cmd.noteId)
        note.Create(cmd.text)
    }
}

UseCase AdminTouchNote handles AdminTouchNoteCmd {
    tenancy: cross_tenant
    execute {
        note = load Note(cmd.noteId)
        note.AdminTouch(cmd.text)
    }
}
`

const notesReadDs = `
View NoteView {
    id   NoteId
    text NoteText
}

Query PeekNoteView(id NoteId) -> NoteView {
    cache {
        ttl: 1min
    }
    return load Note(id) as NoteView
}

Query Ping() -> NoteText {
    return NoteText("pong")
}
`

const notesModDs = `Module Notes {
    Database NotesDb {
        provider: "postgres"
        manages: [Note]
        tenancy: { strategy: row_level, column: "tenant_id" }
    }
}
`

const notesInterfaceDs = `Interface HTTP {
    tenant { from: header("X-Tenant-Id") }

    rateLimit { perTenant: 2/min }

    POST "/notes/{id}"             -> CreateNote
    GET  "/notes/{id}/view"        -> PeekNoteView
    POST "/notes/{id}/admin-touch" -> AdminTouchNote
    GET  "/ping"                   -> Ping { tenancy: none }
}
`

// notesGenerateOptions espelha walletGenerateOptions/rateLimitGenerateOptions
// (codegen_test.go/ratelimit_test.go) — mesmo module path que
// RuntimeImportPath assume implicitamente em todo o pacote codegen.
var notesGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateNotesProject escreve a fixture Notes em disco, resolve via
// driver.CheckProject e gera o projeto Go completo — mesmo padrão de
// generateWalletProject/generateRateLimitProject: G5 mexe em codegen/http.go/
// codegen.go/decl_usecase.go (compartilhados), então só é exercitado de
// verdade pelo pipeline Generate completo, não por uma EmitX singular.
func generateNotesProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         notesModDs,
		"domain.ds":      notesDomainDs,
		"application.ds": notesApplicationDs,
		"read.ds":        notesReadDs,
		"interface.ds":   notesInterfaceDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética Notes (G5) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, notesGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture Notes: %v", err)
	}
	return files
}

// notesFileByPath devolve o conteúdo do arquivo gerado em p (caminho com "/",
// a forma de codegen.File.Path — nunca filepath.Join, que usaria "\" no
// Windows) — mesmo padrão de ledgerFileByPath (sql_adapter_test.go).
func notesFileByPath(t *testing.T, files []codegen.File, p string) []byte {
	t.Helper()
	for _, f := range files {
		if f.Path == p {
			return f.Content
		}
	}
	t.Fatalf("%s não encontrado entre os arquivos gerados", p)
	return nil
}

func notesCmdMainFile(t *testing.T) []byte {
	t.Helper()
	return notesFileByPath(t, generateNotesProject(t), "cmd/notes/main.go")
}

// --- 1. Golden/determinismo/smoke — os critérios NFR-13/14 de sempre. ---

// TestGenerateNotesTenancyGolden prova, sobre o Go de fato gerado: a
// resolução de tenant materializada em tempo de geração (tenantIDFromRequest
// via header "X-Tenant-Id", requireTenant), o gate fail-closed 400 em toda
// rota que não declara "tenancy: none", e "/ping" (tenancy: none) sem ele.
// TestGenerateNotesCrossTenantBypassGolden (abaixo) prova o bypass +
// auditoria em notes/usecases.go, um arquivo separado.
func TestGenerateNotesTenancyGolden(t *testing.T) {
	got := string(notesCmdMainFile(t))
	for _, want := range []string{
		// tenantIDFromRequest resolve por header (estratégia da fixture).
		`func tenantIDFromRequest(r *http.Request) string`,
		`return r.Header.Get("X-Tenant-Id")`,
		// requireTenant: fail-closed (§13.4).
		`func requireTenant(ctx context.Context, r *http.Request) (context.Context, bool)`,
		`id := tenantIDFromRequest(r)`,
		`if id == ""`,
		`return ctx, false`,
		`return runtime.WithTenant(ctx, runtime.Tenant{ID: id, Tier: r.Header.Get("X-Tenant-Tier")}), true`,
		// Toda rota SEM "tenancy: none" ganha o gate, ANTES do corpo/rate limit.
		`mux.HandleFunc("POST /notes/{id}", func(w http.ResponseWriter, r *http.Request) {`,
		`ctx, tenantOK := requireTenant(ctx, r)`,
		`if !tenantOK {`,
		`http.Error(w, "tenant required", http.StatusBadRequest)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q em cmd/notes/main.go, não achei:\n%s", want, got)
		}
	}

	// "/ping" (tenancy: none) NÃO deve ganhar o gate — a rota inteira,
	// isolada, não deve conter "requireTenant".
	pingIdx := strings.Index(got, `mux.HandleFunc("GET /ping"`)
	if pingIdx < 0 {
		t.Fatal(`rota "GET /ping" não encontrada`)
	}
	pingBlock := got[pingIdx:]
	if nextRouteIdx := strings.Index(got[pingIdx+1:], `mux.HandleFunc(`); nextRouteIdx >= 0 {
		pingBlock = got[pingIdx : pingIdx+1+nextRouteIdx]
	} else if endIdx := strings.Index(got[pingIdx:], "\n\treturn mux"); endIdx >= 0 {
		// Ping é a última rota registrada — sem outro "mux.HandleFunc("
		// depois dela, o bloco vai até o fim de newMux (linha "return mux"),
		// nunca até o fim do ARQUIVO (que incluiria devCallerFromRequest/
		// requireTenant/etc. — todos mencionam "requireTenant" nalgum
		// comentário/definição, o que produziria um falso positivo aqui).
		pingBlock = got[pingIdx : pingIdx+endIdx]
	}
	if strings.Contains(pingBlock, "requireTenant") {
		t.Fatalf(`rota "/ping" (tenancy: none) não deveria conter "requireTenant":\n%s`, pingBlock)
	}

	gentest.Golden(t, "testdata/cmd_notes_main.go.golden", []byte(got))
}

// TestGenerateNotesTenancyDeterministic prova NFR-13 escopado a
// cmd/notes/main.go — mesmo padrão de TestGenerateWalletHTTPRoutesDeterministic.
func TestGenerateNotesTenancyDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return notesCmdMainFile(t)
	})
}

// TestGenerateNotesTenancySmokeCompile prova NFR-14: o projeto gerado inteiro
// (incl. o filtro row_level em runtime/sqlruntime e a borda de tenancy)
// compila e passa go vet.
func TestGenerateNotesTenancySmokeCompile(t *testing.T) {
	files := generateNotesProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// TestGenerateNotesCrossTenantBypassGolden prova, sobre notes/usecases.go de
// fato gerado: o UseCase AdminTouchNote (tenancy: cross_tenant, REQ-27.3)
// emite a auditoria via log/slog e marca ctx com runtime.WithCrossTenantBypass
// — enquanto CreateNote (sem o opt-in) não ganha NENHUMA das duas linhas.
func TestGenerateNotesCrossTenantBypassGolden(t *testing.T) {
	got := string(notesFileByPath(t, generateNotesProject(t), "notes/usecases.go"))
	for _, want := range []string{
		`func AdminTouchNote(ctx context.Context, cmd AdminTouchNoteCmd) error`,
		`caller, _ := runtime.CallerFrom(ctx)`,
		`tenant, _ := runtime.TenantFrom(ctx)`,
		`slog.Warn("cross-tenant access (spec §13.3)", "usecase", "AdminTouchNote", "tenant", tenant.ID, "caller", caller.ID())`,
		`ctx = runtime.WithCrossTenantBypass(ctx)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q em notes/usecases.go, não achei:\n%s", want, got)
		}
	}

	createIdx := strings.Index(got, "func CreateNote(")
	if createIdx < 0 {
		t.Fatal("func CreateNote não encontrada")
	}
	createBlock := got[createIdx:]
	if nextFuncIdx := strings.Index(got[createIdx+1:], "\nfunc "); nextFuncIdx >= 0 {
		createBlock = got[createIdx : createIdx+1+nextFuncIdx]
	}
	for _, notWant := range []string{"WithCrossTenantBypass", "cross-tenant access"} {
		if strings.Contains(createBlock, notWant) {
			t.Fatalf("CreateNote (sem tenancy: cross_tenant) não deveria conter %q:\n%s", notWant, createBlock)
		}
	}
}

// TestGenerateRejectsUnsupportedTenancyStrategies prova a postura fail-closed
// documentada em program.Database.Tenancy: "schema_per_tenant"/
// "database_per_tenant" (§13.1) não têm nenhum caminho real neste gerador
// (exigiriam "provision tenant(id)", §13.4, fora do escopo deste ciclo) — em
// vez de silenciosamente gerar um adapter sem isolamento nenhum, Generate
// recusa com um erro claro, ANTES de escrever qualquer arquivo. "row_level"
// (a fixture Notes de sempre) e "" (nenhuma tenancy) continuam aceitos.
func TestGenerateRejectsUnsupportedTenancyStrategies(t *testing.T) {
	for _, strategy := range []string{"schema_per_tenant", "database_per_tenant"} {
		t.Run(strategy, func(t *testing.T) {
			modDs := strings.Replace(notesModDs, "row_level", strategy, 1)
			dir := writeProjectDir(t, map[string]string{
				"mod.ds":         modDs,
				"domain.ds":      notesDomainDs,
				"application.ds": notesApplicationDs,
				"read.ds":        notesReadDs,
				"interface.ds":   notesInterfaceDs,
			})
			prog, bag := driver.CheckProject(dir)
			if bag.HasErrors() {
				t.Fatalf("fixture (tenancy %q) tem diagnósticos de erro inesperados:\n%s", strategy, bag.Render())
			}
			model := types.NewModel(prog.Symbols)
			_, err := codegen.Generate(prog, model, prog.Symbols, bag, notesGenerateOptions)
			if err == nil {
				t.Fatalf("Generate deveria recusar tenancy.strategy %q (não implementada — §13.4/provision tenant fora do escopo), mas não devolveu erro", strategy)
			}
			if !strings.Contains(err.Error(), strategy) {
				t.Fatalf("erro de Generate não menciona a estratégia recusada %q: %v", strategy, err)
			}
		})
	}
}
