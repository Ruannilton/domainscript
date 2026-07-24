package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// apply_builtins_test.go prova a DoD de L1.3c (ISSUE-12 item 2,
// correcoes-issues-6-7-8): emitApply (codegen/decl_aggregate.go) anexa um
// BuiltinLowerer, tornando as built-ins de FUNÇÃO (now()/uuid()/random(...)/
// random_str(...)) utilizáveis dentro de um corpo de Apply — antes emitApply
// era o ÚNICO emissor de corpo executável sem WithBuiltins, e qualquer
// built-in num Apply falhava com "CallExpr sobre \"now\" não é construção de
// VO/Event/Command conhecida". Fixtures SINTÉTICAS mínimas e dedicadas,
// análogas em espírito a docs/examples/pizzeria/kitchen/domain.ds:104
// ("Apply TicketCreated { state.createdAt = CreatedAt(now()) }"), sem tocar
// o fixture do pizzeria em si (fora de escopo desta task).

var applyBuiltinsGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

const applyBuiltinsModDs = `Module Store { }
`

// applyNowDomainDs: um Apply que chama now() dentro da construção de um VO
// (o padrão exato de kitchen/domain.ds:104). now() é o built-in que precisa
// de ctx (runtime.Now(ctx)), e um Apply não tem parâmetro ctx próprio — daí
// a sutileza que L1.3c resolve importando "context" só quando now() aparece.
const applyNowDomainDs = `
ValueObject ItemId(string) {
    Valid { value.length() > 0 }
}

ValueObject CreatedAt(datetime) {
    Valid { true }
}

Event ItemCreated { id ItemId }

Aggregate Item {
    strategy EventSourced

    state {
        id ItemId
        createdAt CreatedAt
    }

    access {
        Create requires caller.authenticated
    }

    Handle Create() {
        emit ItemCreated(self.id)
    }

    Apply ItemCreated {
        state.id = event.id
        state.createdAt = CreatedAt(now())
    }
}
`

func generateApplyProject(t *testing.T, domainDs string) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    applyBuiltinsModDs,
		"domain.ds": domainDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Apply/built-in (L1.3c) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, applyBuiltinsGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de Apply/built-in: %v", err)
	}
	return files
}

// TestGenerateApplyWithNowCompiles prova o desbloqueio central de L1.3c:
// now() dentro de um Apply gera (antes falhava "CallExpr sobre \"now\" não é
// construção de VO/Event/Command conhecida"), emite exatamente
// "runtime.Now(context.Background())" (a forma que resolve a ausência de ctx
// num Apply) e o projeto Go inteiro compila.
func TestGenerateApplyWithNowCompiles(t *testing.T) {
	files := generateApplyProject(t, applyNowDomainDs)
	m := filesToMap(files)

	aggGo, ok := m["store/aggregate_item.go"]
	if !ok {
		t.Fatalf("esperava store/aggregate_item.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}

	if !strings.Contains(string(aggGo), "runtime.Now(context.Background())") {
		t.Errorf("esperava \"runtime.Now(context.Background())\" no Apply gerado, não achei:\n%s", aggGo)
	}

	gentest.SmokeCompile(t, m)
}

// applyUuidDomainDs: um Apply que chama uuid() — o caso mais simples (uuid()
// NÃO lê ctxGoName), que prova que o wiring de WithBuiltins funciona
// independente da sutileza do ctx que now() exige.
const applyUuidDomainDs = `
ValueObject ItemId(string) {
    Valid { value.length() > 0 }
}

ValueObject Token(string) {
    Valid { value.length() > 0 }
}

Event ItemCreated { id ItemId }

Aggregate Item {
    strategy EventSourced

    state {
        id ItemId
        token Token
    }

    access {
        Create requires caller.authenticated
    }

    Handle Create() {
        emit ItemCreated(self.id)
    }

    Apply ItemCreated {
        state.id = event.id
        state.token = Token(uuid())
    }
}
`

// TestGenerateApplyWithUuidCompiles prova que uuid() dentro de um Apply gera
// "runtime.UUID()" e compila — e, crucialmente, que "context" NÃO é importado
// (uuid() não precisa de ctx), confirmando que só now() dispara o import.
func TestGenerateApplyWithUuidCompiles(t *testing.T) {
	files := generateApplyProject(t, applyUuidDomainDs)
	m := filesToMap(files)

	aggGo, ok := m["store/aggregate_item.go"]
	if !ok {
		t.Fatalf("esperava store/aggregate_item.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}

	if !strings.Contains(string(aggGo), "runtime.UUID()") {
		t.Errorf("esperava \"runtime.UUID()\" no Apply gerado, não achei:\n%s", aggGo)
	}
	if strings.Contains(string(aggGo), "\"context\"") {
		t.Errorf("um Apply que só usa uuid() NÃO deve importar \"context\" (só now() precisa de ctx):\n%s", aggGo)
	}

	gentest.SmokeCompile(t, m)
}

// applyNoBuiltinDomainDs: o caso comum (todo Apply de wallet/shop hoje) — um
// Apply que muta state a partir de event sem chamar built-in nenhum.
const applyNoBuiltinDomainDs = `
ValueObject ItemId(string) {
    Valid { value.length() > 0 }
}

Event ItemCreated { id ItemId }

Aggregate Item {
    strategy EventSourced

    state {
        id ItemId
    }

    access {
        Create requires caller.authenticated
    }

    Handle Create() {
        emit ItemCreated(self.id)
    }

    Apply ItemCreated {
        state.id = event.id
    }
}
`

// TestGenerateApplyWithoutBuiltinDoesNotImportContext prova a não-regressão de
// L1.3c: um Apply sem built-in nenhum NÃO importa "context" e não referencia
// o runtime por nenhuma built-in — anexar o BuiltinLowerer não muda a saída
// quando o corpo não invoca nenhuma built-in (o caso comum, wallet/shop). A
// byte-identidade global é provada pelos e2e de wallet/shop
// (driver.TestGenerateWalletE2E*/TestGenerateShopE2E*), que passam inalterados.
func TestGenerateApplyWithoutBuiltinDoesNotImportContext(t *testing.T) {
	files := generateApplyProject(t, applyNoBuiltinDomainDs)
	m := filesToMap(files)

	aggGo, ok := m["store/aggregate_item.go"]
	if !ok {
		t.Fatalf("esperava store/aggregate_item.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}

	if strings.Contains(string(aggGo), "\"context\"") {
		t.Errorf("um Apply sem built-in NÃO deve importar \"context\":\n%s", aggGo)
	}
	if strings.Contains(string(aggGo), "runtime.Now(") {
		t.Errorf("um Apply sem built-in NÃO deve emitir runtime.Now(...):\n%s", aggGo)
	}

	gentest.SmokeCompile(t, m)
}
