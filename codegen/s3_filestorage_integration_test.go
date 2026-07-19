//go:build integration

package codegen_test

import (
	"os"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// s3_filestorage_integration_test.go prova J5.2.b (REQ-45, REQ-48.3, NFR-22/
// 24, §design infra-providers 3.5): um teste comportamental DE VERDADE
// contra um bucket S3 real, atrás da build tag "integration" — NUNCA entra
// no caminho de "go test ./..." default (nem sequer compila sem
// -tags=integration) — e, além disso, guardado por env: sem S3_BUCKET
// definida, pula (t.Skip), nunca falha (REQ-48.3/NFR-24). Mesmo padrão de
// sql_postgres_integration_test.go/channel_rabbitmq_integration_test.go/
// redis_ratelimit_integration_test.go. Rodar de propósito: "S3_BUCKET=meu-
// bucket-de-teste AWS_REGION=us-east-1 go test -tags=integration ./codegen/
// -run TestS3FileStorageIntegration" (credenciais pela cadeia AWS padrão).
//
// fileStorageS3IntegrationModDs usa uma região LITERAL ("us-east-1", não
// env(...)) — ao contrário do fixture golden (s3_filestorage_wiring_test.go,
// que prova env(...) nas DUAS chaves, R1) — para que só S3_BUCKET precise
// estar no ambiente para este teste rodar (region: env("AWS_REGION") exigiria
// as DUAS variáveis, e o guard desta task é só S3_BUCKET); bucket continua
// por env("S3_BUCKET") — REQ-45.4/R1 continuam provados pelo teste golden,
// que não precisa de infra viva.
const fileStorageS3IntegrationModDs = `Module Docs {
    Database DocsDb {
        provider: "postgres"
        manages: [Document]
    }
    FileStorage ContentStorage {
        provider: "s3"
        bucket: env("S3_BUCKET")
        region: "us-east-1"
    }
}
`

func generateFileStorageS3IntegrationProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         fileStorageS3IntegrationModDs,
		"domain.ds":      fileStorageDomainDs,
		"application.ds": fileStorageApplicationDs,
		"read.ds":        fileStorageReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture Docs (S3, integração) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, fileStorageOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture Docs (S3, integração): %v", err)
	}
	return files
}

// fileStorageS3ParityBehaviorTest roda a MESMA sequência Store->Load->
// SignedURL->Delete->Load DUAS vezes — uma contra runtime.
// NewMemoryFileStorage() (o baseline), outra contra um bucket S3 real
// (s3runtime.NewS3FileStorage) — e compara o resultado observável (Name/
// ContentType/bytes lidos de volta, SignedURL não vazia, ErrNotFound depois
// do Delete) — a prova de NFR-22. Skip (não Fail) quando S3_BUCKET não está
// no ambiente: REQ-48.3/NFR-24.
const fileStorageS3ParityBehaviorTest = `package docs

import (
	"context"
	"os"
	"testing"
	"time"

	"domainscript/generated/runtime"
	"domainscript/generated/s3runtime"
)

// runFileStorageRoundTrip executa Store->Load->SignedURL->Delete->Load sobre
// fs e devolve o File lido de volta (do primeiro Load, antes do Delete) — a
// mesma sequência é rodada contra o backend in-memory e o S3 real, e o
// resultado dos dois é comparado pelo chamador.
func runFileStorageRoundTrip(t *testing.T, fs runtime.FileStorage) runtime.File {
	t.Helper()
	ctx := context.Background()

	ref, err := fs.Store(ctx, runtime.File{
		Name:        "relatório.pdf",
		ContentType: "application/pdf",
		Metadata:    map[string]string{"origem": "integração"},
		Buffer:      []byte("conteúdo de teste"),
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := fs.Load(ctx, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	url := fs.SignedURL(ctx, ref, 15*time.Minute)
	if url == "" {
		t.Fatal("SignedURL: esperava uma URL não vazia")
	}

	if err := fs.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := fs.Load(ctx, ref); err != runtime.ErrNotFound {
		t.Fatalf("Load depois de Delete: err = %v, want runtime.ErrNotFound", err)
	}

	return got
}

func TestS3FileStorageIntegrationParityWithMemory(t *testing.T) {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		t.Skip("S3_BUCKET não definida — pulando teste de integração S3 (REQ-48.3/NFR-24)")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	memGot := runFileStorageRoundTrip(t, runtime.NewMemoryFileStorage())

	s3FS, err := s3runtime.NewS3FileStorage(context.Background(), bucket, region)
	if err != nil {
		t.Fatalf("NewS3FileStorage: %v", err)
	}
	s3Got := runFileStorageRoundTrip(t, s3FS)

	if s3Got.Name != memGot.Name {
		t.Fatalf("paridade quebrada (NFR-22): memory Name=%q, s3 Name=%q", memGot.Name, s3Got.Name)
	}
	if s3Got.ContentType != memGot.ContentType {
		t.Fatalf("paridade quebrada (NFR-22): memory ContentType=%q, s3 ContentType=%q", memGot.ContentType, s3Got.ContentType)
	}
	if string(s3Got.Buffer) != string(memGot.Buffer) {
		t.Fatalf("paridade quebrada (NFR-22): memory Buffer=%q, s3 Buffer=%q", memGot.Buffer, s3Got.Buffer)
	}
	if s3Got.Metadata["origem"] != memGot.Metadata["origem"] {
		t.Fatalf("paridade quebrada (NFR-22): memory Metadata[origem]=%q, s3 Metadata[origem]=%q", memGot.Metadata["origem"], s3Got.Metadata["origem"])
	}
}
`

// TestS3FileStorageIntegration gera o projeto Docs (S3, bucket via
// env("S3_BUCKET")), acrescenta fileStorageS3ParityBehaviorTest ao pacote
// "docs" gerado, e roda "go test" DE VERDADE sobre ele (gentest.RunTests) —
// o subprocesso herda o ambiente do processo pai, então S3_BUCKET/
// AWS_REGION (checadas aqui e de novo dentro do teste gerado) chegam ao
// teste comportamental sem nenhuma passagem explícita.
func TestS3FileStorageIntegration(t *testing.T) {
	if os.Getenv("S3_BUCKET") == "" {
		t.Skip("S3_BUCKET não definida — pulando teste de integração S3 (REQ-48.3/NFR-24)")
	}

	files := filesToMap(generateFileStorageS3IntegrationProject(t))
	files["docs/s3_parity_test.go"] = []byte(fileStorageS3ParityBehaviorTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
