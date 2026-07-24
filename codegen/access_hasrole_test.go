package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// access_hasrole_test.go prova a DoD de L1.3b (ISSUE-12 item 1,
// correcoes-issues-6-7-8 §design 2.2): lowerAccessCondition
// (codegen/decl_aggregate.go) reconhece "caller.hasRole(...)" como condição
// de access de primeira classe, tanto SOZINHA (um CallExpr puro, sem
// &&/||/== compondo — a forma que antes caía no fallback genérico
// Lowerer.Expr e era rejeitada, "CallExpr com Fn *ast.MemberExpr não
// suportado") quanto COMPOSTA com && (a forma que já era parcialmente
// suportada via o caso BinaryExpr/AND, mas cujo lado direito -- o próprio
// hasRole -- também caía no mesmo fallback rejeitado). Fixture SINTÉTICA
// mínima e dedicada, análoga em espírito a docs/examples/pizzeria/
// kitchen/domain.ds:90-93 ("access { Create requires
// caller.hasRole(\"system_sales\") }"), sem tocar o fixture do pizzeria em
// si (fora de escopo desta task).

const hasRoleModDs = `Module Store { }
`

const hasRoleDomainDs = `
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
        Create requires caller.hasRole("admin")
    }

    Handle Create() {
        emit ItemCreated(self.id)
    }

    Apply ItemCreated {
        state.id = event.id
    }
}
`

var hasRoleGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateHasRoleProject escreve a fixture em disco, resolve via
// driver.CheckProject e gera o projeto Go completo — mesmo padrão de
// generateMixedWireProject.
func generateHasRoleProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    hasRoleModDs,
		"domain.ds": hasRoleDomainDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética hasRole (L1.3b) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, hasRoleGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture hasRole: %v", err)
	}
	return files
}

// TestGenerateAccessConditionBareHasRoleCompiles prova o desbloqueio central
// de L1.3b: "access { Create requires caller.hasRole(\"admin\") }" SOZINHA
// (sem &&/||/== compondo) gera (antes falhava com "CallExpr com Fn
// *ast.MemberExpr não suportado em Lowerer.Expr") e o projeto Go inteiro
// compila.
func TestGenerateAccessConditionBareHasRoleCompiles(t *testing.T) {
	files := generateHasRoleProject(t)
	m := filesToMap(files)

	aggGo, ok := m["store/aggregate_item.go"]
	if !ok {
		t.Fatalf("esperava store/aggregate_item.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}

	if !strings.Contains(string(aggGo), "caller.HasRole(\"admin\")") {
		t.Errorf("esperava \"caller.HasRole(\\\"admin\\\")\" no Handle Create gerado, não achei:\n%s", aggGo)
	}
	if !strings.Contains(string(aggGo), "if !(caller.HasRole(\"admin\")) {") {
		t.Errorf("esperava o guard \"if !(caller.HasRole(\\\"admin\\\")) {\" no Handle Create gerado, não achei:\n%s", aggGo)
	}

	gentest.SmokeCompile(t, m)
}

// --- Forma composta: caller.authenticated && caller.hasRole(...) -----------

const hasRoleComposedDomainDs = `
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
        Create requires caller.authenticated and caller.hasRole("admin")
    }

    Handle Create() {
        emit ItemCreated(self.id)
    }

    Apply ItemCreated {
        state.id = event.id
    }
}
`

// generateHasRoleComposedProject é o par de generateHasRoleProject com a
// condição de access COMPOSTA ("caller.authenticated && caller.hasRole(...)")
// — prova que a recursão de lowerAccessCondition sobre AND/OR já cobre o novo
// caso sem mudança adicional (o CallExpr puro é tratado no MESMO ponto da
// função, alcançado tanto direto quanto como um dos dois lados de um AND).
func generateHasRoleComposedProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    hasRoleModDs,
		"domain.ds": hasRoleComposedDomainDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética hasRole composta (L1.3b) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, hasRoleGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture hasRole composta: %v", err)
	}
	return files
}

// TestGenerateAccessConditionComposedHasRoleCompiles prova que a forma
// composta ("caller.authenticated && caller.hasRole(...)") gera as duas
// chamadas (Authenticated() e HasRole(...)) unidas por "&&" e compila.
func TestGenerateAccessConditionComposedHasRoleCompiles(t *testing.T) {
	files := generateHasRoleComposedProject(t)
	m := filesToMap(files)

	aggGo, ok := m["store/aggregate_item.go"]
	if !ok {
		t.Fatalf("esperava store/aggregate_item.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}

	if !strings.Contains(string(aggGo), "caller.Authenticated() && caller.HasRole(\"admin\")") {
		t.Errorf("esperava \"caller.Authenticated() && caller.HasRole(\\\"admin\\\")\" no Handle Create gerado, não achei:\n%s", aggGo)
	}

	gentest.SmokeCompile(t, m)
}

// TestLowerAccessConditionHasRoleArityMismatchFailsExplicitly prova que uma
// aridade errada em caller.hasRole(...) (não exatamente 1 argumento) produz
// um erro de geração claro, em vez de cair silenciosamente no fallback
// genérico (que produziria a mensagem confusa "CallExpr com Fn
// *ast.MemberExpr não suportado").
func TestLowerAccessConditionHasRoleArityMismatchFailsExplicitly(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"mod.ds": hasRoleModDs,
		"domain.ds": `
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
        Create requires caller.hasRole()
    }

    Handle Create() {
        emit ItemCreated(self.id)
    }

    Apply ItemCreated {
        state.id = event.id
    }
}
`,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética hasRole/aridade (L1.3b) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	_, err := codegen.Generate(prog, model, prog.Symbols, bag, hasRoleGenerateOptions)
	if err == nil {
		t.Fatal("esperava erro de geração para caller.hasRole() sem o argumento (papel)")
	}
	if !strings.Contains(err.Error(), "hasRole espera exatamente 1 argumento") {
		t.Errorf("esperava um erro claro sobre aridade de hasRole, achei: %v", err)
	}
}
