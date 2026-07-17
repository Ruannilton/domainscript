package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// versioning_test.go prova os critérios de conclusão da task G6 (§design
// codegen 3.12, REQ-28.4, spec §17): versionamento de API na borda —
// "versioning { strategy/current/supported }" (interface.ds) + "Version vN {
// deprecated/sunset/upcast/downcast/route }" (versions/*.ds). Nem o wallet
// nem o shop declaram "versioning" (confirmado antes de escrever esta task —
// TestGenerateWalletHTTPRoutesGolden/TestGenerateShopPolicyRegistersSubscriberAndCompiles
// continuam verdes, byte a byte, depois desta mudança), então a fixture é
// sintética: um módulo "Billing" mínimo, no mesmo espírito de "Notes" (G5,
// tenancy_test.go).
//
// --- A fixture ---
//
// Account (EventSourced) com um único Handle Charge; Command ChargeCmd usa
// um campo "accountId AccountId" NU (não "ref Account") de propósito: o
// upcast de v1 precisa reatribuir esse campo a partir de uma shape legada
// (REQ-5.13/sema.checkVersionUpcastDefaults exige que TODO campo obrigatório
// do Command apareça no bloco "to" do upcast — um campo "ref" não ganha
// nenhuma exceção dessa regra), e a rota usa "{accountId}" (correlação por
// NOME EXATO — a forma mais simples, sem depender da heurística "id" + único
// campo ref). Três versões:
//
//   - v0: só deprecated/sunset, ambos no PASSADO -> sempre 410 Gone (prova
//     "sunset", handler nunca roda).
//   - v1: deprecated no passado, sunset num futuro distante -> ainda funciona,
//     mas ganha os headers Deprecation/Sunset (prova "deprecated"); declara
//     upcast ChargeCmd (request legada com campos "legacyXxx" -> Money/Note
//     atuais, EXERCITA o mecanismo de hoisting — construção de VO COMPOSTO
//     com args nomeados, "Money(amount: ..., currency: ...)", o exemplo
//     canônico do spec §17) e downcast AccountSummaryVW (response atual ->
//     shape legada "accountId"/"legacyTotal").
//   - v2 (current): nenhuma Version a declara (não precisa — é o default) ->
//     nenhum upcast/downcast se aplica; junto da AUSÊNCIA de qualquer Version
//     para a versão corrente, prova o versionamento ESPARSO: o Go gerado
//     para as duas rotas não ganha NENHUM upcast/downcast quando resolveAPIVersion
//     devolve "v2" (ou qualquer versão desconhecida) — cai direto no "default"
//     do switch, idêntico ao handler pré-G6.

const billingDomainDs = `
ValueObject AccountId(string) {
    Valid { value.length() > 0 }
}

ValueObject Money {
    amount   decimal
    currency string

    Valid { amount >= 0 }
}

ValueObject Note(string) {
    Valid { ok }
}

Event AmountCharged {
    id     AccountId
    amount Money
    note   Note
}

Aggregate Account {
    strategy EventSourced

    state {
        id      AccountId
        balance Money
    }

    access {
        Charge requires caller.authenticated
        Adjust requires caller.authenticated
    }

    Handle Charge(amount Money, note Note) {
        emit AmountCharged(self.id, amount, note)
    }

    // Adjust é o alvo do VersionRoute de v1 (spec §17: "mudança de
    // comportamento" — um UseCase DISTINTO, nunca tradução de shape): um
    // "touch" sem parâmetro nenhum, reemitindo o balance JÁ CARREGADO com uma
    // nota fixa — o comportamento legado que v1 preservava.
    Handle Adjust() {
        emit AmountCharged(self.id, state.balance, Note("legacy-adjust"))
    }

    Apply AmountCharged {
        state.balance = event.amount
    }
}
`

const billingApplicationDs = `
Command ChargeCmd {
    accountId AccountId
    amount    Money
    note      Note
}

UseCase ChargeAccount handles ChargeCmd {
    execute {
        account = load Account(cmd.accountId)
        account.Charge(cmd.amount, cmd.note)
    }
}

// AdjustCmd/LegacyAdjustAccount: o alvo de um VersionRoute (v1, spec §17) —
// shape DIFERENTE de ChargeCmd (só "accountId", sem "amount"/"note"), prova
// que a rota de versão troca de UseCase inteiro, não de tradução.
Command AdjustCmd {
    accountId AccountId
}

UseCase LegacyAdjustAccount handles AdjustCmd {
    execute {
        account = load Account(cmd.accountId)
        account.Adjust()
    }
}
`

const billingReadDs = `
View AccountSummaryVW {
    id      AccountId
    balance Money
}

Query GetAccount(accountId AccountId) -> AccountSummaryVW {
    return load Account(accountId) as AccountSummaryVW
}
`

const billingModDs = `Module Billing {
    Database BillingDb {
        provider: "pg"
        manages: [Account]
    }
}
`

const billingInterfaceDs = `Interface HTTP {
    versioning {
        strategy: header("Api-Version")
        current: v2
        supported: [v0, v1, v2]
    }

    POST "/accounts/{accountId}/charge" -> ChargeAccount
    POST "/accounts/{accountId}/adjust" -> ChargeAccount
    GET  "/accounts/{accountId}"        -> GetAccount
}
`

// billingVersionV0Ds: sunset no passado (2016) -> sempre 410, sem
// upcast/downcast/route nenhum — só lifecycle.
const billingVersionV0Ds = `Version v0 {
    deprecated: "2015-01-01"
    sunset: "2016-01-01"
}
`

// billingVersionV1Ds: deprecated no passado, sunset num futuro distante ->
// ainda funciona (com headers) + upcast/downcast de verdade.
const billingVersionV1Ds = `Version v1 {
    deprecated: "2020-01-01"
    sunset: "2099-01-01"

    upcast ChargeCmd {
        from { legacyAccountId string, legacyAmount decimal, legacyCurrency string, legacyNote string }
        to {
            accountId = AccountId(legacyAccountId)
            amount    = Money(amount: legacyAmount, currency: legacyCurrency)
            note      = Note(legacyNote)
        }
    }

    downcast AccountSummaryVW {
        to {
            accountId   = self.id
            legacyTotal = self.balance
        }
    }

    // VersionRoute (spec §17): "/accounts/{accountId}/adjust" muda de
    // COMPORTAMENTO em v1 — vai para LegacyAdjustAccount (AdjustCmd, sem
    // "amount") em vez do alvo base ChargeAccount (ChargeCmd) — não é
    // tradução de shape, é um UseCase inteiramente diferente.
    route "/accounts/{accountId}/adjust" -> LegacyAdjustAccount
}
`

// billingGenerateOptions espelha walletGenerateOptions/notesGenerateOptions —
// mesmo module path que RuntimeImportPath assume implicitamente.
var billingGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateBillingProject escreve a fixture Billing em disco e gera o
// projeto Go completo via driver.CheckProject + codegen.Generate — mesmo
// padrão de generateWalletProject/generateNotesProject.
func generateBillingProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         billingModDs,
		"domain.ds":      billingDomainDs,
		"application.ds": billingApplicationDs,
		"read.ds":        billingReadDs,
		"interface.ds":   billingInterfaceDs,
		"versions/v0.ds": billingVersionV0Ds,
		"versions/v1.ds": billingVersionV1Ds,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética Billing (G6) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, billingGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture Billing: %v", err)
	}
	return files
}

func billingFileByPath(t *testing.T, files []codegen.File, p string) []byte {
	t.Helper()
	for _, f := range files {
		if f.Path == p {
			return f.Content
		}
	}
	t.Fatalf("%s não encontrado entre os arquivos gerados", p)
	return nil
}

func billingCmdMainFile(t *testing.T) []byte {
	t.Helper()
	return billingFileByPath(t, generateBillingProject(t), "cmd/billing/main.go")
}

// --- 1. Golden/determinismo/smoke — os critérios NFR-13/14 de sempre. ---

// TestGenerateBillingVersioningGolden prova, sobre o Go de fato gerado em
// cmd/billing/main.go: a resolução de versão por header, o gate de sunset
// (410, map "v0"), o gate de deprecated (headers, maps "v0"/"v1"), e a
// chamada QUALIFICADA (billing.Upcast.../billing.Downcast...) às funções que
// emitModuleAPIVersions gerou no pacote de domínio (ver
// TestGenerateBillingAPIVersionsGolden, abaixo).
func TestGenerateBillingVersioningGolden(t *testing.T) {
	got := string(billingCmdMainFile(t))
	for _, want := range []string{
		// resolveAPIVersion (estratégia header, fixture).
		`func resolveAPIVersion(r *http.Request) string`,
		`v := r.Header.Get("Api-Version")`,
		`return "v2"`,
		// Lifecycle: sunset (v0) e deprecated (v0, v1).
		`var apiVersionSunset = map[string]time.Time{`,
		`"v0": mustParseAPIVersionDate("2016-01-01"),`,
		`var apiVersionDeprecated = map[string]time.Time{`,
		`"v0": mustParseAPIVersionDate("2015-01-01"),`,
		`"v1": mustParseAPIVersionDate("2020-01-01"),`,
		// apiVersionGate: 410 antes de qualquer outra coisa, headers depois.
		`func apiVersionGate(w http.ResponseWriter, r *http.Request) (string, bool)`,
		`http.Error(w, "API version sunset (spec §17)", http.StatusGone)`,
		`return "", false`,
		`w.Header().Set("Deprecation", "true")`,
		`w.Header().Set("Sunset", apiVersionSunsetHeader(sunset))`,
		// Cada rota chama o gate ANTES de emitCallerAndIdempotency/decode.
		`apiVersion, verOK := apiVersionGate(w, r)`,
		`if !verOK {`,
		// Upcast (v1): decode da shape legada QUALIFICADO pelo pacote billing.
		`switch apiVersion {`,
		`case "v1":`,
		`var legacy billing.ChargeCmdV1Request`,
		`cmd, uerr = billing.UpcastChargeCmdV1(legacy)`,
		// Downcast (v1): encode QUALIFICADO pelo pacote billing.
		`billing.DowncastAccountSummaryVWV1(result)`,
		// VersionRoute (v1): "/adjust" despacha INTEIRAMENTE para
		// LegacyAdjustAccount/AdjustCmd (auto-contido: seu próprio caller/
		// decode/path-param/dispatch) e retorna — NUNCA chama ChargeAccount
		// nesse case.
		`mux.HandleFunc("POST /accounts/{accountId}/adjust", func(w http.ResponseWriter, r *http.Request) {`,
		`var cmd billing.AdjustCmd`,
		`if err := billing.LegacyAdjustAccount(ctx, cmd); err != nil {`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q em cmd/billing/main.go, não achei:\n%s", want, got)
		}
	}

	// O case "v1" de "/adjust" (do "case \"v1\":" ao "return" que fecha o
	// override) NUNCA menciona ChargeAccount/ChargeCmd — prova que o
	// VersionRoute troca o alvo INTEIRO, não injeta upcast num Command
	// diferente do que a rota base usa.
	adjustIdx := strings.Index(got, `mux.HandleFunc("POST /accounts/{accountId}/adjust"`)
	if adjustIdx < 0 {
		t.Fatal(`rota "POST /accounts/{accountId}/adjust" não encontrada`)
	}
	v1CaseIdx := strings.Index(got[adjustIdx:], `case "v1":`)
	if v1CaseIdx < 0 {
		t.Fatal(`case "v1" não encontrado dentro do handler de "/adjust"`)
	}
	v1CaseStart := adjustIdx + v1CaseIdx
	returnIdx := strings.Index(got[v1CaseStart:], "\n\t\t\treturn\n")
	if returnIdx < 0 {
		t.Fatal(`"return" de fechamento do override v1 não encontrado`)
	}
	v1CaseBlock := got[v1CaseStart : v1CaseStart+returnIdx]
	for _, notWant := range []string{"ChargeAccount", "ChargeCmd"} {
		if strings.Contains(v1CaseBlock, notWant) {
			t.Fatalf("case \"v1\" do override de /adjust não deveria mencionar %q:\n%s", notWant, v1CaseBlock)
		}
	}

	gentest.Golden(t, "testdata/cmd_billing_main.go.golden", []byte(got))
}

// TestGenerateBillingAPIVersionsGolden prova, sobre billing/api_versions.go
// (o pacote de domínio — ver a nota de arquitetura em httpVersioningEnv,
// versioning.go): a struct+função Upcast<Cmd>V1/Downcast<View>V1 de verdade,
// SEM nenhum alias (mesmo pacote de ChargeCmd/Money/AccountId/
// AccountSummaryVW), incl. a construção de VO COMPOSTO via hoisting
// ("tmp1, err := NewMoney(...)", o exemplo canônico do spec §17).
func TestGenerateBillingAPIVersionsGolden(t *testing.T) {
	got := string(billingFileByPath(t, generateBillingProject(t), "billing/api_versions.go"))
	for _, want := range []string{
		`type ChargeCmdV1Request struct`,
		`LegacyAccountId string `,
		`func UpcastChargeCmdV1(raw ChargeCmdV1Request) (ChargeCmd, error)`,
		// Money é um VO composto com args nomeados -> hoisting (NewMoney + erro).
		`tmp1, err := NewMoney(raw.LegacyAmount, raw.LegacyCurrency)`,
		`if err != nil {`,
		`accountId := AccountId(raw.LegacyAccountId)`,
		`amount := tmp1`,
		`note := Note(raw.LegacyNote)`,
		`return ChargeCmd{AccountId: accountId, Amount: amount, Note: note}, nil`,
		`type AccountSummaryVWV1Response struct`,
		`func DowncastAccountSummaryVWV1(v AccountSummaryVW) AccountSummaryVWV1Response`,
		`return AccountSummaryVWV1Response{AccountId: v.Id, LegacyTotal: v.Balance}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q em billing/api_versions.go, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, "testdata/billing_api_versions.go.golden", []byte(got))
}

// TestGenerateBillingVersioningDeterministic prova NFR-13 escopado a
// cmd/billing/main.go.
func TestGenerateBillingVersioningDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return billingCmdMainFile(t)
	})
}

// TestGenerateBillingVersioningSmokeCompile prova NFR-14: o projeto inteiro
// (incl. as funções Upcast/Downcast geradas) compila e passa go vet.
func TestGenerateBillingVersioningSmokeCompile(t *testing.T) {
	files := generateBillingProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// --- 2. Comportamento de verdade (o coração da task). ---
//
// billingHTTPVersioningBehaviorTest roda DENTRO de cmd/billing (mesmo pacote
// de newMux) e prova, sobre o mux de fato gerado:
//
//  1. v0 (sunset) -> 410 Gone imediato, e o Handle NUNCA roda: uma consulta
//     subsequente (versão atual) sobre a MESMA conta mostra saldo zero.
//  2. v1 (deprecated, não sunset) -> ainda funciona (204), com os headers
//     Deprecation/Sunset presentes, E o upcast de fato traduz a shape legada
//     (campos "legacyXxx") para o Command atual.
//  3. v1 GET -> 200, com Deprecation/Sunset, E o corpo na shape legada
//     ("accountId"/"legacyTotal"), refletindo o charge do passo 2.
//  4. v2 (corrente, sem header nenhum) GET -> 200, SEM Deprecation, corpo na
//     shape ATUAL ("id"/"balance") — o mesmo estado do passo 2/3, provando
//     que upcast/downcast só traduzem a REPRESENTAÇÃO, nunca o dado.
//  5. v2 POST com a shape ATUAL (sem upcast nenhum) -> 204 direto —
//     versionamento esparso: a MESMA rota, sem tradução, para a versão
//     corrente.
const billingHTTPVersioningBehaviorTest = `package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"domainscript/generated/billing"
	"domainscript/generated/runtime"
)

func newTestMux(store runtime.EventStore) *http.ServeMux {
	billing.Wire(runtime.NewUnitOfWork(store))
	return newMux(store)
}

func TestSunsetVersionReturns410AndNeverRunsHandle(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	body := bytes.NewBufferString(` + "`" + `{"amount":{"amount":"99.00","currency":"BRL"},"note":"nao deveria acontecer"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/accounts/A0/charge", body)
	req.Header.Set("X-Caller-Id", "u1")
	req.Header.Set("Api-Version", "v0")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410 (v0 sunset); body: %s", rec.Code, rec.Body.String())
	}

	// Confirma que o Handle NUNCA rodou: consultando a MESMA conta pela
	// versão corrente, o saldo continua zero (nenhum AmountCharged emitido).
	getReq := httptest.NewRequest(http.MethodGet, "/accounts/A0", nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /accounts/A0 (v2) status = %d, want 200; body: %s", getRec.Code, getRec.Body.String())
	}
	var view struct {
		Id      string ` + "`json:\"id\"`" + `
		Balance struct {
			Amount   string ` + "`json:\"amount\"`" + `
			Currency string ` + "`json:\"currency\"`" + `
		} ` + "`json:\"balance\"`" + `
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &view); err != nil {
		t.Fatalf("json.Unmarshal: %v; body: %s", err, getRec.Body.String())
	}
	if view.Balance.Amount != "" && view.Balance.Amount != "0" && view.Balance.Amount != "0.0000" {
		t.Fatalf("balance.amount = %q, want zero value (Handle nunca deveria ter rodado sob v0 sunset)", view.Balance.Amount)
	}
}

func TestDeprecatedVersionUpcastsRequestAndStillSucceeds(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	legacyBody := bytes.NewBufferString(` + "`" + `{"legacyAccountId":"A1","legacyAmount":"30.00","legacyCurrency":"BRL","legacyNote":"legado"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/accounts/A1/charge", legacyBody)
	req.Header.Set("X-Caller-Id", "u1")
	req.Header.Set("Api-Version", "v1")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (v1 deprecated, mas ainda funciona); body: %s", rec.Code, rec.Body.String())
	}
	if dep := rec.Header().Get("Deprecation"); dep != "true" {
		t.Fatalf("Deprecation header = %q, want \"true\"", dep)
	}
	if sunset := rec.Header().Get("Sunset"); sunset == "" {
		t.Fatal("Sunset header ausente para uma versão deprecated com sunset declarado")
	}
}

func TestDeprecatedVersionDowncastsResponseToLegacyShape(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	// Semeia a conta A1 via o MESMO caminho v1 (upcast) do teste anterior —
	// cada teste usa seu próprio store isolado.
	legacyBody := bytes.NewBufferString(` + "`" + `{"legacyAccountId":"A1","legacyAmount":"30.00","legacyCurrency":"BRL","legacyNote":"legado"}` + "`" + `)
	chargeReq := httptest.NewRequest(http.MethodPost, "/accounts/A1/charge", legacyBody)
	chargeReq.Header.Set("X-Caller-Id", "u1")
	chargeReq.Header.Set("Api-Version", "v1")
	chargeRec := httptest.NewRecorder()
	mux.ServeHTTP(chargeRec, chargeReq)
	if chargeRec.Code != http.StatusNoContent {
		t.Fatalf("setup (charge v1) status = %d, want 204; body: %s", chargeRec.Code, chargeRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/accounts/A1", nil)
	req.Header.Set("Api-Version", "v1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /accounts/A1 (v1) status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if dep := rec.Header().Get("Deprecation"); dep != "true" {
		t.Fatalf("Deprecation header = %q, want \"true\" (GET também versionado)", dep)
	}

	var legacy struct {
		AccountId   string ` + "`json:\"accountId\"`" + `
		LegacyTotal struct {
			Amount   string ` + "`json:\"amount\"`" + `
			Currency string ` + "`json:\"currency\"`" + `
		} ` + "`json:\"legacyTotal\"`" + `
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("json.Unmarshal (shape legada v1): %v; body: %s", err, rec.Body.String())
	}
	if legacy.AccountId != "A1" {
		t.Fatalf("accountId = %q, want A1", legacy.AccountId)
	}
	if legacy.LegacyTotal.Currency != "BRL" {
		t.Fatalf("legacyTotal.currency = %q, want BRL", legacy.LegacyTotal.Currency)
	}

	// A MESMA conta, pela versão CORRENTE (sem header nenhum) — shape atual
	// ("id"/"balance"), nunca "accountId"/"legacyTotal": upcast/downcast só
	// traduzem REPRESENTAÇÃO, nunca o dado subjacente.
	currentReq := httptest.NewRequest(http.MethodGet, "/accounts/A1", nil)
	currentRec := httptest.NewRecorder()
	mux.ServeHTTP(currentRec, currentReq)
	if currentRec.Code != http.StatusOK {
		t.Fatalf("GET /accounts/A1 (v2, corrente) status = %d, want 200; body: %s", currentRec.Code, currentRec.Body.String())
	}
	if dep := currentRec.Header().Get("Deprecation"); dep != "" {
		t.Fatalf("Deprecation header presente na versão CORRENTE (%q) — não deveria", dep)
	}
	var current struct {
		Id      string ` + "`json:\"id\"`" + `
		Balance struct {
			Amount   string ` + "`json:\"amount\"`" + `
			Currency string ` + "`json:\"currency\"`" + `
		} ` + "`json:\"balance\"`" + `
	}
	if err := json.Unmarshal(currentRec.Body.Bytes(), &current); err != nil {
		t.Fatalf("json.Unmarshal (shape atual v2): %v; body: %s", err, currentRec.Body.String())
	}
	if current.Id != "A1" {
		t.Fatalf("id = %q, want A1", current.Id)
	}
	if current.Balance.Currency != "BRL" {
		t.Fatalf("balance.currency = %q, want BRL", current.Balance.Currency)
	}
}

func TestUnchangedRouteChargeWithCurrentShapePassesThroughDirectly(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	// Sem "Api-Version" (cai na corrente, v2) e sem NENHUM upcast aplicável —
	// versionamento esparso: a shape ATUAL vai direto ao Command, sem
	// tradução nenhuma (a MESMA forma de antes de G6 existir).
	body := bytes.NewBufferString(` + "`" + `{"amount":{"amount":"50.00","currency":"BRL"},"note":"pagamento"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/accounts/A2/charge", body)
	req.Header.Set("X-Caller-Id", "u1")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (rota inalterada, versionamento esparso); body: %s", rec.Code, rec.Body.String())
	}
	if dep := rec.Header().Get("Deprecation"); dep != "" {
		t.Fatalf("Deprecation header presente sem NENHUM header Api-Version — não deveria (%q)", dep)
	}
}

func TestVersionRouteDispatchesToOverrideUseCaseForV1AndBaseForCurrent(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	mux := newTestMux(store)

	// v1 -> LegacyAdjustAccount/AdjustCmd: SEM corpo nenhum (AdjustCmd só tem
	// "accountId", preenchido pelo path param) — se estivesse (incorretamente)
	// caindo no alvo base (ChargeAccount/ChargeCmd), o Go gerado seria
	// idêntico (mesmo shape mínima aceitável), mas o teste comportamental
	// abaixo (TestGenerateBillingVersioningGolden) já confirma, sobre o texto
	// gerado, que o case "v1" NUNCA menciona ChargeAccount/ChargeCmd — a
	// prova de exclusividade mora ali; este teste prova que o override de
	// fato RESPONDE com sucesso ponta a ponta.
	v1Req := httptest.NewRequest(http.MethodPost, "/accounts/A3/adjust", nil)
	v1Req.Header.Set("X-Caller-Id", "u1")
	v1Req.Header.Set("Api-Version", "v1")
	v1Rec := httptest.NewRecorder()
	mux.ServeHTTP(v1Rec, v1Req)
	if v1Rec.Code != http.StatusNoContent {
		t.Fatalf("POST /accounts/A3/adjust (v1, override) status = %d, want 204; body: %s", v1Rec.Code, v1Rec.Body.String())
	}

	// Versão corrente (sem header) -> alvo BASE (ChargeAccount/ChargeCmd) —
	// a MESMA rota, sem override, continua funcionando com a shape atual.
	body := bytes.NewBufferString(` + "`" + `{"amount":{"amount":"10.00","currency":"BRL"},"note":"ajuste"}` + "`" + `)
	currentReq := httptest.NewRequest(http.MethodPost, "/accounts/A3/adjust", body)
	currentReq.Header.Set("X-Caller-Id", "u1")
	currentRec := httptest.NewRecorder()
	mux.ServeHTTP(currentRec, currentReq)
	if currentRec.Code != http.StatusNoContent {
		t.Fatalf("POST /accounts/A3/adjust (corrente, sem override) status = %d, want 204; body: %s", currentRec.Code, currentRec.Body.String())
	}
}
`

// TestGenerateBillingVersioningBehavior prova NFR-15 sobre o versionamento
// de API na borda: roda ` + "`go test ./...`" + ` de verdade sobre o projeto
// isolado gerado — o critério de conclusão da task G6, comportamentalmente
// (sunset -> 410 sem rodar o Handle, deprecated -> headers + ainda funciona,
// upcast/downcast traduzindo de fato, e versionamento esparso).
func TestGenerateBillingVersioningBehavior(t *testing.T) {
	files := filesToMap(generateBillingProject(t))
	files["cmd/billing/main_versioning_behavior_test.go"] = []byte(billingHTTPVersioningBehaviorTest)
	runGeneratedTests(t, files)
}
