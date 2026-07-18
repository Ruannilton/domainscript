package codegen

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
	"domainscript/codegen/rtsrc"
	"domainscript/codegen/s3rt"
)

// s3_filestorage_test.go prova a DoD de J5.1.c (REQ-45.1/45.2, §design
// infra-providers 3.5): montagem de key/metadata/layout do adapter
// s3runtime (codegen/s3rt/filestorage.go.txt) — SEM abrir nenhuma conexão
// de rede real com S3. Como filestorage.go.txt não é compilado diretamente
// por este módulo (só embutido como texto), a única forma de exercitá-lo de
// verdade é compilar e rodar um pacote s3runtime real dentro de um projeto
// Go efêmero (mesmo padrão de redis_cache_test.go/J4.1.c). O teste embutido
// é `package s3runtime` (white-box, não `_test`): newS3FileStorage/
// s3PutObjectAPI/s3GetObjectAPI/s3DeleteObjectAPI/s3PresignAPI não são
// exportados de propósito (só o wiring gerado, task J5.2, chama
// NewS3FileStorage de fora). Este arquivo é `package codegen` (interno, não
// `codegen_test`) para poder ler fileProviders["s3"] direto (não exportado)
// ao montar o go.mod da fixture — mesmo padrão de redis_cache_test.go.
//
// *s3.Client é uma struct concreta, não uma interface — não dá para
// substituir o client inteiro por um dublê de teste. Em vez disso,
// filestorage.go.txt já isola sua superfície de chamadas atrás de
// s3PutObjectAPI/s3GetObjectAPI/s3DeleteObjectAPI/s3PresignAPI (as únicas
// operações que o adapter usa) — o teste embutido implementa esses mesmos
// formatos com um fakeS3 em memória (mapa + mutex), permitindo verificar
// key/metadata/layout de forma determinística, sem nenhuma rede.
const s3FileStorageTest = `package s3runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"domainscript/generated/runtime"
)

// fakeS3 implementa s3PutObjectAPI/s3GetObjectAPI/s3DeleteObjectAPI/
// s3PresignAPI inteiramente em memória (mapa + mutex) — prova "client fake
// injetado" (task J5.1.c) sem abrir nenhuma conexão real.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]fakeObject
}

type fakeObject struct {
	body        []byte
	contentType string
	metadata    map[string]string
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: make(map[string]fakeObject)}
}

func (f *fakeS3) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	body, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[aws.ToString(params.Key)] = fakeObject{
		body:        body,
		contentType: aws.ToString(params.ContentType),
		metadata:    params.Metadata,
	}
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.mu.Lock()
	obj, ok := f.objects[aws.ToString(params.Key)]
	f.mu.Unlock()
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(obj.body)),
		ContentType:   aws.String(obj.contentType),
		ContentLength: aws.Int64(int64(len(obj.body))),
		Metadata:      obj.metadata,
	}, nil
}

func (f *fakeS3) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, aws.ToString(params.Key))
	return &s3.DeleteObjectOutput{}, nil
}

type fakePresigner struct {
	err error
}

func (p fakePresigner) PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4PresignedHTTPRequest, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &v4PresignedHTTPRequest{URL: "https://example-bucket.s3.amazonaws.com/" + aws.ToString(params.Key) + "?X-Amz-Signature=fake"}, nil
}

// TestS3FileStorage_StoreThenLoadRoundTrips prova o layout: Store grava
// Name/Metadata como object metadata (mergeMetadataWithName) preservando o
// Content-Type/bytes, e Load reconstrói o File inteiro a partir da resposta
// (nunca dos campos do FileRef de entrada) — Content-Length lido de volta
// via GetObjectOutput, não do File.Size original.
func TestS3FileStorage_StoreThenLoadRoundTrips(t *testing.T) {
	fake := newFakeS3()
	fs := newS3FileStorage(fake, fake, fake, fakePresigner{}, "my-bucket")
	ctx := context.Background()

	ref, err := fs.Store(ctx, runtime.File{
		Name:        "report.pdf",
		ContentType: "application/pdf",
		Size:        4,
		Metadata:    map[string]string{"team": "billing"},
		Buffer:      []byte("data"),
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if ref.ID == "" {
		t.Fatal("Store: esperava um FileRef.ID não vazio (UUID v4)")
	}
	if ref.Name != "report.pdf" || ref.ContentType != "application/pdf" {
		t.Fatalf("Store: FileRef inesperado: %+v", ref)
	}

	got, err := fs.Load(ctx, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != "report.pdf" {
		t.Fatalf("Load: Name = %q, want %q", got.Name, "report.pdf")
	}
	if got.ContentType != "application/pdf" {
		t.Fatalf("Load: ContentType = %q, want %q", got.ContentType, "application/pdf")
	}
	if got.Size != 4 {
		t.Fatalf("Load: Size = %d, want 4 (lido de GetObjectOutput.ContentLength)", got.Size)
	}
	if string(got.Buffer) != "data" {
		t.Fatalf("Load: Buffer = %q, want %q", got.Buffer, "data")
	}
	if got.Metadata["team"] != "billing" {
		t.Fatalf("Load: Metadata[\"team\"] = %q, want %q", got.Metadata["team"], "billing")
	}
	if _, reserved := got.Metadata[s3NameMetadataKey]; reserved {
		t.Fatalf("Load: Metadata não deveria vazar a chave reservada %q: %+v", s3NameMetadataKey, got.Metadata)
	}
}

// TestS3FileStorage_StoreTwiceProducesDistinctKeys prova key ÚNICA por
// Store (UUID v4 novo a cada chamada, nunca determinística por conteúdo,
// mesmo com bytes idênticos) — §design infra-providers 3.5.
func TestS3FileStorage_StoreTwiceProducesDistinctKeys(t *testing.T) {
	fake := newFakeS3()
	fs := newS3FileStorage(fake, fake, fake, fakePresigner{}, "my-bucket")
	ctx := context.Background()

	ref1, err := fs.Store(ctx, runtime.File{Name: "a.txt", Buffer: []byte("same")})
	if err != nil {
		t.Fatalf("Store (1ª): %v", err)
	}
	ref2, err := fs.Store(ctx, runtime.File{Name: "a.txt", Buffer: []byte("same")})
	if err != nil {
		t.Fatalf("Store (2ª): %v", err)
	}
	if ref1.ID == ref2.ID {
		t.Fatalf("esperava keys distintas para 2 Store, veio a mesma: %q", ref1.ID)
	}
}

// TestS3FileStorage_LoadUnknownRefFailsWithErrNotFound prova o mapeamento
// types.NoSuchKey -> runtime.ErrNotFound (mesmo contrato de
// memoryFileStorage.Load).
func TestS3FileStorage_LoadUnknownRefFailsWithErrNotFound(t *testing.T) {
	fake := newFakeS3()
	fs := newS3FileStorage(fake, fake, fake, fakePresigner{}, "my-bucket")
	ctx := context.Background()

	_, err := fs.Load(ctx, runtime.FileRef{ID: "never-stored"})
	if !errors.Is(err, runtime.ErrNotFound) {
		t.Fatalf("Load de um ref desconhecido: err = %v, want runtime.ErrNotFound", err)
	}
}

// TestS3FileStorage_DeleteIsIdempotent prova Delete idempotente (S3 não
// erra numa key inexistente, mesmo contrato de memoryFileStorage.Delete).
func TestS3FileStorage_DeleteIsIdempotent(t *testing.T) {
	fake := newFakeS3()
	fs := newS3FileStorage(fake, fake, fake, fakePresigner{}, "my-bucket")
	ctx := context.Background()

	ref, err := fs.Store(ctx, runtime.File{Name: "a.txt", Buffer: []byte("x")})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := fs.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete (1ª): %v", err)
	}
	if err := fs.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete (2ª, idempotente): %v", err)
	}
	if _, err := fs.Load(ctx, ref); !errors.Is(err, runtime.ErrNotFound) {
		t.Fatalf("Load depois de Delete: err = %v, want runtime.ErrNotFound", err)
	}
}

// TestS3FileStorage_SignedURLHasURLShape prova SignedURL sobre o caminho
// feliz (presign real via fakePresigner.URL).
func TestS3FileStorage_SignedURLHasURLShape(t *testing.T) {
	fake := newFakeS3()
	fs := newS3FileStorage(fake, fake, fake, fakePresigner{}, "my-bucket")
	ctx := context.Background()

	url := fs.SignedURL(ctx, runtime.FileRef{ID: "some-id"}, 15*time.Minute)
	if url == "" {
		t.Fatal("SignedURL: esperava uma URL não vazia no caminho feliz")
	}
}

// TestS3FileStorage_SignedURLReturnsEmptyOnPresignFailure prova o contrato
// "sem error" da interface (ver a doc de filestorage.go.txt): uma falha do
// presigner nunca propaga um erro nem pânico, só devolve "" (logado via
// slog).
func TestS3FileStorage_SignedURLReturnsEmptyOnPresignFailure(t *testing.T) {
	fake := newFakeS3()
	fs := newS3FileStorage(fake, fake, fake, fakePresigner{err: errors.New("boom")}, "my-bucket")
	ctx := context.Background()

	url := fs.SignedURL(ctx, runtime.FileRef{ID: "some-id"}, 15*time.Minute)
	if url != "" {
		t.Fatalf("SignedURL numa falha de presign: esperava \"\", veio %q", url)
	}
}
`

// buildS3RuntimeProjectFiles monta o material MÍNIMO que codegen.Generate
// escreveria para QUALQUER programa com uma FileStorage `provider: "s3"`
// (go.mod + runtime/*.go + s3runtime/*.go, J5.1) — sem passar por
// driver.CheckProject/Generate sobre nenhum programa .ds (mesmo espírito de
// buildRedisRuntimeProjectFiles, redis_cache_test.go). O go.mod inclui os
// dois requires (aws-sdk-go-v2/service/s3 + aws-sdk-go-v2/config, via
// EmitGoMod/fileProviders["s3"]) — gentest.RunTests roda "go mod tidy" a
// partir dele (nenhuma chamada de rede além da resolução do módulo: o teste
// embutido nunca abre uma conexão S3 de verdade).
func buildS3RuntimeProjectFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	files["go.mod"] = EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", nil, false, false, []providerDep{fileProviders["s3"]})

	rtSrcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources: %v", err)
	}
	for name, content := range rtSrcs {
		files[path.Join("runtime", name)] = content
	}

	s3Srcs, err := s3rt.Sources()
	if err != nil {
		t.Fatalf("s3rt.Sources: %v", err)
	}
	for name, content := range s3Srcs {
		files[path.Join("s3runtime", name)] = content
	}
	return files
}

// TestS3FileStorageAdapter roda s3FileStorageTest de verdade sobre um
// projeto Go mínimo (runtime + s3runtime vendorados) — prova item c da task
// J5.1: montagem de key/metadata/layout por client fake injetado.
func TestS3FileStorageAdapter(t *testing.T) {
	files := buildS3RuntimeProjectFiles(t)
	files[path.Join("s3runtime", "filestorage_adapter_test.go")] = []byte(s3FileStorageTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
