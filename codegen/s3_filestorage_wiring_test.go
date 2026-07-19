package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// s3_filestorage_wiring_test.go prova os critérios de conclusão de J5.2
// ("Seleção + wiring", REQ-45.4, R1/R2, §design infra-providers 3.5): que
// decl_filestorage.go/codegen.go trocam runtime.NewMemoryFileStorage() por
// s3runtime.NewS3FileStorage(...) quando (e só quando) a FileStorage do
// mod.ds declara `provider: "s3"`, com bucket/região resolvidos via
// `env(...)` (R1) — e que, sem esse provider, o caminho in-memory de sempre
// continua byte-idêntico (NFR-21/23, já provado por filestorage_test.go
// continuando verde sem nenhuma alteração após esta task).

// fileStorageS3ModDs é fileStorageModDs (filestorage_test.go) + o provider
// real (J5.1/J5.2) — mesmos Database/Aggregate/UseCase/Query da fixture
// Docs, só a FileStorage ganha `provider: "s3"` + bucket/região por
// env(...) (R1), provando que os DOIS caminhos (in-memory e s3) leem a
// MESMA declaração de FileStorage, só trocando o construtor.
const fileStorageS3ModDs = `Module Docs {
    Database DocsDb {
        provider: "postgres"
        manages: [Document]
    }
    FileStorage ContentStorage {
        provider: "s3"
        bucket: env("DOCUMENTS_BUCKET")
        region: env("AWS_REGION")
    }
}
`

// generateFileStorageS3Project monta o projeto Docs (fileStorageS3ModDs +
// as mesmas domain/application/read.ds da fixture in-memory) e gera o
// projeto Go completo.
func generateFileStorageS3Project(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         fileStorageS3ModDs,
		"domain.ds":      fileStorageDomainDs,
		"application.ds": fileStorageApplicationDs,
		"read.ds":        fileStorageReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture Docs (S3, J5.2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, fileStorageOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture Docs (S3): %v", err)
	}
	return files
}

// TestGenerateFileStorageS3BackendGolden prova, sobre cmd/docs/main.go de
// fato gerado, a seleção do backend S3 para ContentStorage: bucket/região
// de env(...) (R1), fail-closed via log.Fatal (mesmo padrão de
// emitXADatabaseWiring/sql_wiring.go — nenhum "run() error" ainda, J6.2) e
// s3runtime.NewS3FileStorage no lugar de runtime.NewMemoryFileStorage.
func TestGenerateFileStorageS3BackendGolden(t *testing.T) {
	files := generateFileStorageS3Project(t)
	got := fileStorageFileByPath(t, files, "cmd/docs/main.go")
	s := string(got)

	for _, want := range []string{
		`contentStorageFS, err := s3runtime.NewS3FileStorage(context.Background(), os.Getenv("DOCUMENTS_BUCKET"), os.Getenv("AWS_REGION"))`,
		"log.Fatal(err)",
		`docs.WireFileStorage("ContentStorage", contentStorageFS)`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("esperava %q em cmd/docs/main.go, não achei:\n%s", want, s)
		}
	}
	if strings.Contains(s, "runtime.NewMemoryFileStorage()") {
		t.Fatalf("com FileStorage{provider:\"s3\"}, não deveria mais usar runtime.NewMemoryFileStorage():\n%s", s)
	}
}

// TestGenerateFileStorageS3BackendSmokeCompile prova que o PROJETO INTEIRO,
// gerado com FileStorage{provider:"s3"} no mod.ds (ativando
// fileProviders["s3"] via activeProviderDeps — go.mod ganha
// aws-sdk-go-v2/service/s3 + aws-sdk-go-v2/config, s3runtime/*.go é
// vendorado), COMPILA de verdade — mesma técnica de
// TestGenerateCacheRedisBackendSmokeCompile (redis_provider_wiring_test.go):
// driver.CheckProject + codegen.Generate + gentest.SmokeCompile sobre os
// bytes de fato escritos, nenhuma chamada AWS real é feita.
func TestGenerateFileStorageS3BackendSmokeCompile(t *testing.T) {
	files := generateFileStorageS3Project(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// TestFileStorageUnrecognizedProviderStaysInMemory prova a metade NFR-21 de
// REQ-45.4: um provider declarado mas NÃO reconhecido (ex. "gcs", ainda não
// implementado neste ciclo) cai silenciosamente no caminho in-memory de
// sempre — nunca um erro de geração, nunca s3runtime.
func TestFileStorageUnrecognizedProviderStaysInMemory(t *testing.T) {
	modDs := strings.Replace(fileStorageS3ModDs, `provider: "s3"`, `provider: "gcs"`, 1)
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         modDs,
		"domain.ds":      fileStorageDomainDs,
		"application.ds": fileStorageApplicationDs,
		"read.ds":        fileStorageReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture Docs (provider gcs) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, fileStorageOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado (provider não reconhecido deveria cair no caminho in-memory): %v", err)
	}
	got := string(fileStorageFileByPath(t, files, "cmd/docs/main.go"))
	if !strings.Contains(got, "runtime.NewMemoryFileStorage()") {
		t.Fatalf("provider \"gcs\" (não reconhecido) deveria manter runtime.NewMemoryFileStorage():\n%s", got)
	}
	if strings.Contains(got, "s3runtime") {
		t.Fatalf("provider \"gcs\" (não reconhecido) não deveria referenciar s3runtime:\n%s", got)
	}
}
