package codegen

import (
	"strings"
	"testing"
)

// provider_registry_gomod_test.go prova a DoD de J0.2 (REQ-46.2, §design 2.2):
// EmitGoMod itera providerDeps (activeProviderDeps) além de sqlProviderKeys —
// cada dep ativa vira uma linha "require" (ordenada por módulo com as demais)
// e eleva o default de versão de Go para dep.minGo quando não-vazio. Vazio (o
// caso de hoje, sem nenhum registro populado) não muda o go.mod (NFR-21).

func TestEmitGoModWithProviderDepsAddsRequireAndBumpsVersion(t *testing.T) {
	deps := []providerDep{
		{module: "github.com/rabbitmq/amqp091-go", version: "v1.10.0", minGo: "1.23", adapterDir: "amqpruntime", ctor: "NewRabbitMQChannel"},
	}

	content := string(EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", nil, false, false, deps))

	if !strings.Contains(content, "require github.com/rabbitmq/amqp091-go v1.10.0") {
		t.Fatalf("esperava \"require github.com/rabbitmq/amqp091-go v1.10.0\" em go.mod, não achei:\n%s", content)
	}
	if !strings.Contains(content, "go 1.23") {
		t.Fatalf("esperava \"go 1.23\" (minGo da dep) em go.mod, não achei:\n%s", content)
	}
}

// TestEmitGoModWithProviderDepsOrderedWithSQL prova que providerDeps entra no
// MESMO bloco require, ordenado por módulo junto com os providers SQL —
// determinismo (NFR-13), sem depender de qual dos dois "chegou primeiro".
func TestEmitGoModWithProviderDepsOrderedWithSQL(t *testing.T) {
	deps := []providerDep{
		{module: "github.com/redis/go-redis/v9", version: "v9.7.0", adapterDir: "redisruntime", ctor: "NewRedisQueryCache"},
	}

	content := string(EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", []string{"sqlite"}, false, false, deps))

	sqliteIdx := strings.Index(content, "modernc.org/sqlite")
	redisIdx := strings.Index(content, "github.com/redis/go-redis/v9")
	if sqliteIdx < 0 || redisIdx < 0 {
		t.Fatalf("esperava as duas linhas require presentes, não achei:\n%s", content)
	}
	// "github.com/..." ordena antes de "modernc.org/..." lexicograficamente.
	if redisIdx > sqliteIdx {
		t.Fatalf("esperava require ordenado por módulo (github.com/... antes de modernc.org/...), veio:\n%s", content)
	}
}

// TestEmitGoModNoProviderDepsUnchanged prova NFR-21: providerDeps vazio (nil,
// o estado de hoje antes de J1..J5 popularem qualquer registro) produz
// EXATAMENTE o go.mod de antes desta task.
func TestEmitGoModNoProviderDepsUnchanged(t *testing.T) {
	withNil := EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", nil, false, false, nil)
	withEmpty := EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", nil, false, false, []providerDep{})

	want := "module domainscript/generated\n\ngo 1.22\n"
	if string(withNil) != want {
		t.Fatalf("esperava go.mod sem providerDeps == %q, veio %q", want, string(withNil))
	}
	if string(withEmpty) != want {
		t.Fatalf("esperava go.mod com providerDeps vazio == %q, veio %q", want, string(withEmpty))
	}
}
