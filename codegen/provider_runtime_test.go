package codegen

import (
	"errors"
	"testing"
)

// provider_runtime_test.go prova a DoD de J0.3 (REQ-46.3, §design 2.3):
// generateProviderRuntimeFiles é o gate genérico de cópia de fontes por
// categoria, espelhando generateSQLRuntimeFiles (sql_wiring.go) — só copia as
// fontes de uma providerDep quando providerSources tem uma entrada registrada
// para seu adapterDir; com providerSources vazio (o estado de hoje, antes de
// J1..J5 registrarem qualquer adapter real) devolve sempre nil, e nenhum
// projeto gerado muda (NFR-21).

func TestGenerateProviderRuntimeFilesEmptyRegistryIsNoop(t *testing.T) {
	deps := []providerDep{
		{module: "github.com/rabbitmq/amqp091-go", version: "v1.10.0", adapterDir: "amqpruntime", ctor: "NewRabbitMQChannel"},
		{module: "github.com/redis/go-redis/v9", version: "v9.7.0", adapterDir: "redisruntime", ctor: "NewRedisQueryCache"},
	}

	files, err := generateProviderRuntimeFiles(deps)
	if err != nil {
		t.Fatalf("generateProviderRuntimeFiles: erro inesperado: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("generateProviderRuntimeFiles: esperava nenhum arquivo (providerSources vazio), veio %+v", files)
	}
}

func TestGenerateProviderRuntimeFilesNilDepsIsNoop(t *testing.T) {
	files, err := generateProviderRuntimeFiles(nil)
	if err != nil {
		t.Fatalf("generateProviderRuntimeFiles: erro inesperado: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("generateProviderRuntimeFiles: esperava nenhum arquivo com deps nil, veio %+v", files)
	}
}

// TestGenerateProviderRuntimeFilesCopiesRegisteredSources prova o caminho
// positivo com uma fonte fake registrada em providerSources (monkey-patch,
// restaurado via defer — mesmo padrão de provider_registry_test.go): copia
// cada arquivo devolvido por sources() para adapterDir/<nome>, ordenado por
// caminho (determinismo, NFR-13).
func TestGenerateProviderRuntimeFilesCopiesRegisteredSources(t *testing.T) {
	orig := providerSources
	defer func() { providerSources = orig }()

	providerSources = map[string]func() (map[string][]byte, error){
		"fakeruntime": func() (map[string][]byte, error) {
			return map[string][]byte{
				"z.go": []byte("package fakeruntime\n// z\n"),
				"a.go": []byte("package fakeruntime\n// a\n"),
			}, nil
		},
	}

	deps := []providerDep{
		{module: "github.com/example/fake", version: "v1.0.0", adapterDir: "fakeruntime", ctor: "NewFake"},
	}

	files, err := generateProviderRuntimeFiles(deps)
	if err != nil {
		t.Fatalf("generateProviderRuntimeFiles: erro inesperado: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("generateProviderRuntimeFiles: esperava 2 arquivos, veio %d: %+v", len(files), files)
	}
	if files[0].Path != "fakeruntime/a.go" || files[1].Path != "fakeruntime/z.go" {
		t.Fatalf("generateProviderRuntimeFiles: esperava ordem [fakeruntime/a.go, fakeruntime/z.go], veio %+v", files)
	}
	if string(files[0].Content) != "package fakeruntime\n// a\n" {
		t.Fatalf("generateProviderRuntimeFiles: conteúdo inesperado para a.go: %q", files[0].Content)
	}
}

// TestGenerateProviderRuntimeFilesUnregisteredAdapterDirSkipped prova que uma
// dep ativa cujo adapterDir NÃO está em providerSources é ignorada em
// silêncio (não é um erro) — o caso normal enquanto uma categoria ainda não
// implementou seu adapter real.
func TestGenerateProviderRuntimeFilesUnregisteredAdapterDirSkipped(t *testing.T) {
	deps := []providerDep{
		{module: "github.com/example/unregistered", adapterDir: "unregisteredruntime", ctor: "NewUnregistered"},
	}

	files, err := generateProviderRuntimeFiles(deps)
	if err != nil {
		t.Fatalf("generateProviderRuntimeFiles: erro inesperado: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("generateProviderRuntimeFiles: esperava nenhum arquivo (adapterDir não registrado), veio %+v", files)
	}
}

// TestGenerateProviderRuntimeFilesPropagatesSourcesError prova que um erro
// devolvido por sources() é propagado (envolvido com contexto do adapterDir),
// em vez de silenciado.
func TestGenerateProviderRuntimeFilesPropagatesSourcesError(t *testing.T) {
	orig := providerSources
	defer func() { providerSources = orig }()

	wantErr := errors.New("boom")
	providerSources = map[string]func() (map[string][]byte, error){
		"brokenruntime": func() (map[string][]byte, error) { return nil, wantErr },
	}

	deps := []providerDep{
		{module: "github.com/example/broken", adapterDir: "brokenruntime", ctor: "NewBroken"},
	}

	_, err := generateProviderRuntimeFiles(deps)
	if err == nil {
		t.Fatal("generateProviderRuntimeFiles: esperava erro propagado de sources(), veio nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("generateProviderRuntimeFiles: esperava erro envolvendo %v, veio %v", wantErr, err)
	}
}
