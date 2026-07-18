package codegen

import (
	"errors"
	"testing"
)

// provider_runtime_test.go prova a DoD de J0.3 (REQ-46.3, §design 2.3):
// generateProviderRuntimeFiles é o gate genérico de cópia de fontes por
// categoria, espelhando generateSQLRuntimeFiles (sql_wiring.go) — só copia as
// fontes de uma providerDep quando providerSources tem uma entrada registrada
// para seu adapterDir; com providerSources vazio devolve sempre nil, e
// nenhum projeto gerado muda (NFR-21). "amqpruntime" (adapterDir de
// channelProviders["rabbitmq"]) parou de ser um exemplo de adapterDir NÃO
// registrado a partir de J3.1 — providerSources tem uma entrada real para
// ele agora (ver TestGenerateProviderRuntimeFilesCopiesRealAMQPRuntimeSources,
// abaixo); "redisruntime" segue não registrado até J4 popular Redis.

func TestGenerateProviderRuntimeFilesEmptyRegistryIsNoop(t *testing.T) {
	orig := providerSources
	defer func() { providerSources = orig }()
	providerSources = map[string]func() (map[string][]byte, error){}

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

// TestGenerateProviderRuntimeFilesCopiesRealAMQPRuntimeSources prova o
// registro real de J3.1 (REQ-43.1/46.3): a providerDep de
// channelProviders["rabbitmq"] tem seu adapterDir ("amqpruntime") resolvido
// contra o registro DE VERDADE de providerSources (nenhum monkey-patch aqui,
// ao contrário do teste acima) — copia exatamente os arquivos que
// amqprt.Sources() embute, prefixados por "amqpruntime/".
func TestGenerateProviderRuntimeFilesCopiesRealAMQPRuntimeSources(t *testing.T) {
	files, err := generateProviderRuntimeFiles([]providerDep{channelProviders["rabbitmq"]})
	if err != nil {
		t.Fatalf("generateProviderRuntimeFiles: erro inesperado: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("generateProviderRuntimeFiles: esperava ao menos um arquivo (amqpruntime/rabbitmq.go)")
	}
	found := false
	for _, f := range files {
		if f.Path == "amqpruntime/rabbitmq.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("generateProviderRuntimeFiles: esperava amqpruntime/rabbitmq.go entre os arquivos, veio %+v", files)
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

// TestGenerateProviderRuntimeFilesSameAdapterDirCopiedOnce prova a correção
// da revisão da PR #13: duas providerDep que compartilham o MESMO adapterDir
// mas têm ctor diferente (o caso real de redis em Cache E RateLimit,
// §design 3.4 — activeProviderDeps não as colapsa de propósito, R5) só
// materializam as fontes daquele adapterDir UMA vez — nunca arquivos
// duplicados na lista devolvida.
func TestGenerateProviderRuntimeFilesSameAdapterDirCopiedOnce(t *testing.T) {
	orig := providerSources
	defer func() { providerSources = orig }()

	calls := 0
	providerSources = map[string]func() (map[string][]byte, error){
		"redisruntime": func() (map[string][]byte, error) {
			calls++
			return map[string][]byte{"cache.go": []byte("package redisruntime\n")}, nil
		},
	}

	deps := []providerDep{
		{module: "github.com/redis/go-redis/v9", version: "v9.7.0", adapterDir: "redisruntime", ctor: "NewRedisQueryCache"},
		{module: "github.com/redis/go-redis/v9", version: "v9.7.0", adapterDir: "redisruntime", ctor: "NewRedisLimiter"},
	}

	files, err := generateProviderRuntimeFiles(deps)
	if err != nil {
		t.Fatalf("generateProviderRuntimeFiles: erro inesperado: %v", err)
	}
	if calls != 1 {
		t.Fatalf("generateProviderRuntimeFiles: esperava sources() chamada 1 vez, veio %d", calls)
	}
	if len(files) != 1 {
		t.Fatalf("generateProviderRuntimeFiles: esperava 1 arquivo (adapterDir copiado uma única vez), veio %d: %+v", len(files), files)
	}
}
