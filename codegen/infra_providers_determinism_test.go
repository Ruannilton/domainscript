package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// infra_providers_determinism_test.go prova a DoD de J6.3 (NFR-21/23),
// fechando a Fase J6 e o plano infra-providers inteiro (REQ-41..48):
//
//   - (a) Determinismo: regenerar a fixture-âncora de J6.1 (os cinco
//     providers reais ativos ao mesmo tempo, em 3 services) duas vezes
//     produz bytes IDÊNTICOS — go.mod, imports, fontes de adapter
//     (redisruntime/s3runtime/amqpruntime/sqlruntime) e main.go de cada
//     service, tudo incluído (mesma técnica de
//     TestSharedCollectionTypeDeterministic/TestFileStorageDeterministic:
//     concatena Path+"\x00"+Content de cada arquivo, gera duas vezes,
//     compara byte a byte via gentest.Deterministic). Não cobre go.sum/
//     vendor/ — esses dois nunca chegaram a existir neste ciclo (desvio
//     registrado em J6.1, R10: vendorização real fica como follow-up
//     explícito, ver .claude/state.md/issues.md).
//   - (b) NFR-21 consolidado: um programa que NÃO declara NENHUM dos
//     cinco providers (nem Database real, nem canal `queue provider:
//     "rabbitmq"`, nem `Cache`/`RateLimit { backend: "redis" }`, nem
//     `FileStorage { provider: "s3" }`) gera um go.mod SEM bloco
//     "require" nenhum (núcleo puro, só stdlib + runtime vendorado) e
//     NENHUM arquivo sob os quatro diretórios de adapter opt-in
//     (sqlruntime/amqpruntime/redisruntime/s3runtime) — o núcleo
//     permanece intacto quando nenhuma categoria está ativa, a mesma
//     prova que cada task individual (J1-J5) já fez por categoria,
//     consolidada aqui numa única fixture que não toca NENHUMA das
//     cinco.

// TestAnchorFixtureDeterministic prova NFR-23 sobre a fixture-âncora
// inteira (os cinco providers ativos ao mesmo tempo, J6.1/anchor_fixture_
// test.go): regenerar duas vezes produz bytes idênticos em TODOS os
// arquivos (go.mod, cada main.go, cada pacote de módulo, e as fontes de
// adapter copiadas de redisrt/s3rt/amqprt/sqlrt).
func TestAnchorFixtureDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		files := generateAnchorProject(t)
		var buf []byte
		for _, f := range files {
			buf = append(buf, []byte(f.Path+"\x00")...)
			buf = append(buf, f.Content...)
		}
		return buf
	})
}

// --- NFR-21 consolidado: nenhum dos 5 providers declarado ⇒ núcleo intacto ---

const baselineDomainDs = `
ValueObject BaselineId(string) {
    Valid { value.length() > 0 }
}

Event BaselineCreated {
    id BaselineId
}

Aggregate BaselineAgg {
    strategy EventSourced

    state {
        id BaselineId
    }

    access {
        Create requires caller.authenticated
    }

    Handle Create() {
        emit BaselineCreated(self.id)
    }

    Apply BaselineCreated {
    }
}

Command CreateBaseline {
    id ref BaselineAgg
}

UseCase CreateBaselineUseCase handles CreateBaseline {
    execute {
        agg = load BaselineAgg(cmd.id)
        agg.Create()
    }
}
`

const baselineModDs = `Module Baseline { }
`

// baselineGenerateOptions espelha runErrorGenerateOptions/
// anchorGenerateOptions — o mesmo module path que RuntimeImportPath
// assume implicitamente em todo o pacote codegen.
var baselineGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// TestNoProviderDeclaredMeansCoreOnly prova NFR-21 consolidado: um
// programa que não declara NENHUM Database/Cache/RateLimit/FileStorage
// real nem canal rabbitmq (nem sequer um bloco Database/Cache/RateLimit/
// FileStorage/topology.ds — o caso mais simples possível) gera go.mod SEM
// bloco "require" (só "module .../go .../\n", a forma exata de antes de
// qualquer provider real existir, EmitGoMod/project.go) e NENHUM arquivo
// sob os 4 diretórios de adapter opt-in — o núcleo permanece intacto.
func TestNoProviderDeclaredMeansCoreOnly(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    baselineModDs,
		"domain.ds": baselineDomainDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture baseline (J6.3, NFR-21) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, baselineGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture baseline: %v", err)
	}

	goMod := fileContent(t, files, "go.mod")
	if strings.Contains(goMod, "require") {
		t.Fatalf("go.mod não deveria conter nenhum bloco \"require\" (nenhum provider declarado, NFR-21):\n%s", goMod)
	}
	wantGoMod := "module domainscript/generated\n\ngo 1.22\n"
	if goMod != wantGoMod {
		t.Fatalf("go.mod = %q, want %q (forma exata do núcleo sem provider nenhum)", goMod, wantGoMod)
	}

	for _, f := range files {
		for _, adapterDir := range []string{"sqlruntime/", "amqpruntime/", "redisruntime/", "s3runtime/"} {
			if strings.HasPrefix(f.Path, adapterDir) {
				t.Fatalf("arquivo %q não deveria existir (nenhum provider real declarado, NFR-21)", f.Path)
			}
		}
	}
}
