package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen/gentest"
)

// http_test.go prova os critérios de conclusão da task E9.2 (§design codegen
// 3.12, REQ-28.1/28.2) sobre a borda HTTP real do wallet
// (docs/examples/wallet/interface.ds): golden do cmd/wallet/main.go gerado
// (newMux + as 4 rotas + devCaller/writeBusinessError), determinismo, smoke
// compile do projeto inteiro, e um teste comportamental que roda `go test
// ./...` de verdade sobre o projeto gerado — não uma reimplementação — para
// provar, via httptest contra newMux(store), exatamente o que a "Conclusão"
// da task pede: "POST /wallets/{id}/deposit" roteia para PerformDeposit,
// "GET /wallets/{id}" roteia para GetWallet.
//
// generateWalletProject/filesToMap/walletGenerateOptions (codegen_test.go,
// E9.1) e runGeneratedTests (decl_aggregate_load_test.go) já existem no
// pacote codegen_test — reusados aqui, mesmo padrão dos demais *_test.go.

// walletCmdMainFile devolve o conteúdo de cmd/wallet/main.go dentre os
// arquivos gerados por Generate sobre o wallet real: o wallet é um monólito
// implícito (1 módulo sem Service, buildCmdGroups/defaultCmdDirName em
// codegen.go), então o grupo default vira cmd/wallet — mesma premissa de
// TestGenerateWalletMainWiresUseCases (codegen_test.go).
func walletCmdMainFile(t *testing.T) []byte {
	t.Helper()
	files := generateWalletProject(t)
	for _, f := range files {
		if f.Path == "cmd/wallet/main.go" {
			return f.Content
		}
	}
	t.Fatal("cmd/wallet/main.go não encontrado entre os arquivos gerados")
	return nil
}

// TestGenerateWalletHTTPRoutesGolden prova o critério de conclusão da task
// (golden): cmd/wallet/main.go registra as 4 rotas de interface.ds — incl.
// as 2 citadas explicitamente pela task, "POST /wallets/{id}/deposit" ->
// PerformDeposit e "GET /wallets/{id}" -> GetWallet — junto da correlação de
// path param (WalletId via "id" -> o único campo "ref Wallet"), do caller de
// dev (X-Caller-Id), do carrier de Idempotency-Key e do mapeamento de erro
// (writeBusinessError).
func TestGenerateWalletHTTPRoutesGolden(t *testing.T) {
	got := walletCmdMainFile(t)
	for _, want := range []string{
		"Handler: newMux(store)",
		"func newMux(store runtime.EventStore) *http.ServeMux",
		`mux.HandleFunc("POST /wallets/{id}/deposit", func(w http.ResponseWriter, r *http.Request)`,
		`mux.HandleFunc("POST /wallets/{id}/withdraw", func(w http.ResponseWriter, r *http.Request)`,
		`mux.HandleFunc("GET /wallets/{id}", func(w http.ResponseWriter, r *http.Request)`,
		`mux.HandleFunc("GET /wallets/{id}/entries", func(w http.ResponseWriter, r *http.Request)`,
		"idVal, err := wallet.NewWalletId(r.PathValue(\"id\"))",
		"cmd.WalletId = idVal",
		`ctx, ucSpanEnd := runtime.RecordSpan(ctx, "UseCase.PerformDeposit")`,
		"ucErr := wallet.PerformDeposit(ctx, cmd)",
		"ucSpanEnd(ucErr)",
		"if ucErr != nil",
		`ctx, ucSpanEnd := runtime.RecordSpan(ctx, "UseCase.PerformWithdrawal")`,
		"ucErr := wallet.PerformWithdrawal(ctx, cmd)",
		`ctx, qSpanEnd := runtime.RecordSpan(ctx, "Query.GetWallet")`,
		"result, err := wallet.GetWallet(ctx, store, idVal)",
		"qSpanEnd(err)",
		`ctx, qSpanEnd := runtime.RecordSpan(ctx, "Query.ListEntries")`,
		"result, err := wallet.ListEntries(ctx, store, idVal)",
		`caller := devCallerFromRequest(r)`,
		"ctx := runtime.WithCaller(r.Context(), caller)",
		"ctx = runtime.WithTrace(ctx, runtime.NewTraceID())",
		`r.Header.Get("X-Caller-Id")`,
		`if key := r.Header.Get("Idempotency-Key"); key != ""`,
		"func writeBusinessError(w http.ResponseWriter, err error)",
		"case errors.Is(err, runtime.ErrForbidden):",
		"http.Error(w, be.Msg, http.StatusForbidden)",
		"case errors.Is(err, runtime.ErrNotFound):",
		"http.Error(w, be.Msg, http.StatusNotFound)",
		"http.Error(w, be.Msg, http.StatusUnprocessableEntity)",
		`http.Error(w, "internal error", http.StatusServiceUnavailable)`,
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava %q em cmd/wallet/main.go, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "cmd_wallet_main.go.golden"), got)
}

// TestGenerateWalletHTTPRoutesDeterministic prova NFR-13 escopado a
// cmd/wallet/main.go — mesmo padrão de TestEmitUseCasesDeterministic
// (decl_usecase_test.go): duas gerações da borda HTTP produzem bytes
// idênticos.
func TestGenerateWalletHTTPRoutesDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return walletCmdMainFile(t)
	})
}

// TestGenerateWalletHTTPSmokeCompile prova NFR-14 para a borda HTTP
// especificamente: o projeto gerado inteiro (incl. cmd/wallet/main.go com
// newMux/devCaller/writeBusinessError desta task) compila e passa go vet —
// o critério de conclusão da task ("o router do wallet compila"). Redundante
// com TestGenerateWalletSmokeCompile (codegen_test.go, E9.1) no que
// compila, mas mantido separado para rastrear o critério desta task
// isoladamente, seguindo o padrão dos demais *_test.go do pacote (cada task
// tem seu próprio SmokeCompile).
func TestGenerateWalletHTTPSmokeCompile(t *testing.T) {
	files := generateWalletProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// walletHTTPRouteBehaviorTest roda DENTRO do diretório cmd/wallet do
// projeto isolado gerado (package main, mesmo pacote de main.go — newMux é
// não-exportado) e exercita newMux(store) via httptest.NewRecorder, sem
// subir nenhum socket real — prova, sobre o Go de fato gerado (não uma
// reimplementação), o critério de conclusão literal da task:
//
//   - POST /wallets/{id}/deposit roteia para PerformDeposit: com um caller
//     autenticado (X-Caller-Id), o dispatch chega ao Handle Deposit, que
//     falha em "ensure state.active == ActiveStatus(true) else
//     InactiveWallet" (gap de domínio já documentado em
//     decl_usecase_test.go/decl_query_test.go: nenhum Apply liga
//     state.active sobre um stream vazio) — o erro de negócio mapeia para
//     422 via writeBusinessError. Sem caller, o "access { Deposit requires
//     caller.authenticated }" barra ANTES do Handle, com runtime.ErrForbidden
//     -> 403 — a prova de que devCallerFromRequest/X-Caller-Id de fato
//     alimenta o caller usado pelo access check.
//   - GET /wallets/{id} roteia para GetWallet: sucesso completo (Query não
//     tem access nem precondição de estado), 200 + JSON com o Id
//     sincronizado a partir do path param — mesmo caminho de sucesso
//     provado por TestGetWalletUnknownStreamReturnsZeroValueSyncedView
//     (decl_query_test.go), agora sobre a borda HTTP.
const walletHTTPRouteBehaviorTest = `package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/wallet"
)

// newTestMux replica o wiring que func main() faz antes de subir o server
// (store -> uow -> wallet.Wire(uow), codegen.go/generateCmdMainFile) — sem
// isso, o pacote wallet fica com sua var de pacote "uow" (runtime.UnitOfWork)
// zero-value, e PerformDeposit/PerformWithdrawal (que abrem uow.Run) sofrem
// nil pointer dereference. newMux em si não faz esse wiring (não é
// responsabilidade dela — main() e newMux são funções separadas de
// propósito, ver a doc de generateCmdMainFile em codegen.go); GetWallet não
// precisa disso porque Query não usa unit of work (LoadWallet com
// runtime.NewEventLoader direto).
func newTestMux(store runtime.EventStore) *http.ServeMux {
	wallet.Wire(runtime.NewUnitOfWork(store))
	return newMux(store)
}

func TestNewMuxRoutesDepositToPerformDepositWithAuthenticatedCaller(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	body := bytes.NewBufferString(` + "`" + `{"amount":{"amount":"10.00","currency":"BRL"},"description":"compra"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/wallets/W1/deposit", body)
	req.Header.Set("X-Caller-Id", "W1")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (InactiveWallet — Handle alcançado, gap de domínio documentado); body: %s", rec.Code, rec.Body.String())
	}
}

func TestNewMuxRoutesDepositToPerformDepositWithoutCallerIsForbidden(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	body := bytes.NewBufferString(` + "`" + `{"amount":{"amount":"10.00","currency":"BRL"},"description":"compra"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/wallets/W1/deposit", body)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (access requires caller.authenticated, sem X-Caller-Id); body: %s", rec.Code, rec.Body.String())
	}
}

func TestNewMuxRoutesGetWalletToGetWallet(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newMux(store)

	req := httptest.NewRequest(http.MethodGet, "/wallets/W1", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var got struct {
		Id string ` + "`json:\"id\"`" + `
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal: %v\nbody: %s", err, rec.Body.String())
	}
	if got.Id != "W1" {
		t.Fatalf("Id = %q, want W1 (path param /wallets/{id} deveria ter sincronizado a View)", got.Id)
	}
}
`

// TestGenerateWalletHTTPBehavior prova NFR-15 sobre a borda HTTP: roda `go
// test ./...` de verdade sobre o projeto isolado gerado, com
// walletHTTPRouteBehaviorTest escrito em cmd/wallet (mesmo pacote de
// newMux) — o critério de conclusão da task, comportamentalmente.
func TestGenerateWalletHTTPBehavior(t *testing.T) {
	files := filesToMap(generateWalletProject(t))
	files[filepath.Join("cmd", "wallet", "main_route_test.go")] = []byte(walletHTTPRouteBehaviorTest)
	runGeneratedTests(t, files)
}
