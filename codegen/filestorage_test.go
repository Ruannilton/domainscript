package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// filestorage_test.go prova a task G1a (FileStorage e operações de arquivo,
// §2.5, REQ-22.7(b)/REQ-25/REQ-26): nem o wallet nem o shop (as duas
// fixtures reais deste repositório) declaram um bloco storage{} de Aggregate
// nem uma FileStorage em mod.ds (o inventário do Marco E lista explicitamente
// "Sem ... Adapter/..." e o shop mínimo do Marco F também não usa arquivo) —
// então esta task precisa de uma fixture SINTÉTICA para exercitar o
// caminho de verdade, o mesmo padrão que ledgerDomainDs (G1, sql_adapter_test.go)
// já usou para o adapter database/sql.
//
// A fixture, módulo "Docs": um Aggregate EventSourced (Document) cujo state
// guarda um campo FileRef ("content"), roteado por um bloco storage{} para
// uma FileStorage declarada em mod.ds ("ContentStorage") — o mesmo padrão do
// exemplo "Person" do spec §2.5. Dois Commands/UseCases exercitam
// store/delete file (UploadContentUC: "doc.AttachContent(store cmd.content)"
// — store inline como argumento, o padrão que o spec de fato usa e que evita
// a ambiguidade de gramática pré-existente documentada em usecase_repair.go;
// DeleteContentUC: "delete file(doc.state.content)" como último statement do
// bloco). Duas Queries exercitam signed_url/load File(ref) (GetContentUrl:
// "return signed_url(doc.state.content, expires: 15min)"; DownloadContent:
// "file = load File(doc.state.content); return file").

const fileStorageDomainDs = `
ValueObject DocId(string) {
    Valid { value.length() > 0 }
}

Error DocumentNotFound { message "Document não encontrado." }

Event ContentAttached {
    id DocId
    content FileRef
}

Event ContentRemoved {
    id DocId
}

Aggregate Document {
    strategy EventSourced

    storage {
        state: DocsDb
        content: ContentStorage
    }

    state {
        id DocId
        content FileRef
    }

    access {
        AttachContent requires caller.authenticated
        RemoveContent requires caller.authenticated
    }

    Handle AttachContent(fileRef FileRef) {
        emit ContentAttached(self.id, fileRef)
    }

    Apply ContentAttached {
        state.content = event.content
    }

    Handle RemoveContent() {
        emit ContentRemoved(self.id)
    }

    Apply ContentRemoved {
    }
}
`

const fileStorageApplicationDs = `
Command UploadContent {
    docId ref Document
    content File
}

Command DeleteContent {
    docId ref Document
}

UseCase UploadContentUC handles UploadContent {
    execute {
        doc = load Document(cmd.docId)
        ensure doc exists else DocumentNotFound
        doc.AttachContent(store cmd.content)
    }
}

UseCase DeleteContentUC handles DeleteContent {
    execute {
        doc = load Document(cmd.docId)
        ensure doc exists else DocumentNotFound
        doc.RemoveContent()
        delete file(doc.state.content)
    }
}
`

const fileStorageReadDs = `
Query GetContentUrl(id DocId) -> string {
    doc = load Document(id)
    ensure doc exists else DocumentNotFound
    return signed_url(doc.state.content, expires: 15min)
}

Query DownloadContent(id DocId) -> File {
    doc = load Document(id)
    ensure doc exists else DocumentNotFound
    file = load File(doc.state.content)
    return file
}
`

const fileStorageModDs = `Module Docs {
    Database DocsDb {
        provider: "postgres"
        manages: [Document]
    }
    FileStorage ContentStorage {
    }
}
`

// fileStorageOptions é o Options do projeto Docs — mesma convenção de
// walletGenerateOptions/ledgerOptions.
var fileStorageOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateFileStorageProject escreve a fixture Docs em disco, resolve via
// driver.CheckProject e gera o projeto Go completo — mesmo padrão de
// generateLedgerProject (sql_adapter_test.go).
func generateFileStorageProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         fileStorageModDs,
		"domain.ds":      fileStorageDomainDs,
		"application.ds": fileStorageApplicationDs,
		"read.ds":        fileStorageReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética Docs (G1a) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, fileStorageOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture Docs: %v", err)
	}
	return files
}

func fileStorageFileByPath(t *testing.T, files []codegen.File, p string) []byte {
	t.Helper()
	for _, f := range files {
		if f.Path == p {
			return f.Content
		}
	}
	t.Fatalf("arquivo %q não encontrado nos gerados", p)
	return nil
}

// --- 1. Golden: as 4 formas de lowering (store/delete file/signed_url/load
// File(ref)) na saída de verdade. ---

func TestFileStorageGoldenUseCases(t *testing.T) {
	files := generateFileStorageProject(t)
	got := fileStorageFileByPath(t, files, "docs/usecases.go")
	for _, want := range []string{
		`tmp2, err := fileStorages["ContentStorage"].Store(ctx, cmd.Content)`,
		`events, err := doc.AttachContent(caller, tmp2)`,
		`if err := fileStorages["ContentStorage"].Delete(ctx, doc.state.Content); err != nil {`,
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava %q em usecases.go, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "filestorage_usecases.go.golden"), got)
}

func TestFileStorageGoldenQueries(t *testing.T) {
	files := generateFileStorageProject(t)
	got := fileStorageFileByPath(t, files, "docs/queries.go")
	for _, want := range []string{
		`return fileStorages["ContentStorage"].SignedURL(ctx, doc.state.Content, time.Duration(900000000000)), nil`,
		`tmp2, err := fileStorages["ContentStorage"].Load(ctx, doc.state.Content)`,
		"return file, nil",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava %q em queries.go, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "filestorage_queries.go.golden"), got)
}

func TestFileStorageWiringFileEmitted(t *testing.T) {
	files := generateFileStorageProject(t)
	got := fileStorageFileByPath(t, files, "docs/filestorage.go")
	for _, want := range []string{
		"var fileStorages = map[string]runtime.FileStorage{}",
		"func WireFileStorage(name string, fs runtime.FileStorage)",
		"fileStorages[name] = fs",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava %q em filestorage.go, não achei:\n%s", want, got)
		}
	}
}

func TestFileStorageMainWiresMemoryBackend(t *testing.T) {
	files := generateFileStorageProject(t)
	got := fileStorageFileByPath(t, files, "cmd/docs/main.go")
	want := `docs.WireFileStorage("ContentStorage", runtime.NewMemoryFileStorage())`
	if !strings.Contains(string(got), want) {
		t.Fatalf("esperava %q em cmd/docs/main.go, não achei:\n%s", want, got)
	}
}

// --- 2. Determinismo (NFR-13) e smoke compile (NFR-14). ---

func TestFileStorageDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		files := generateFileStorageProject(t)
		var buf []byte
		for _, f := range files {
			buf = append(buf, []byte(f.Path+"\x00")...)
			buf = append(buf, f.Content...)
		}
		return buf
	})
}

func TestFileStorageSmokeCompile(t *testing.T) {
	files := generateFileStorageProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// --- 3. Comportamental: store→FileRef→load reproduz os mesmos bytes,
// signed_url tem forma de URL, delete remove (load subsequente falha limpo). ---

const fileStorageBehaviorTest = `package docs

import (
	"context"
	"strings"
	"testing"

	"domainscript/generated/runtime"
)

type docsStubCaller struct{}

func (docsStubCaller) Authenticated() bool      { return true }
func (docsStubCaller) ID() string                { return "u1" }
func (docsStubCaller) HasRole(role string) bool { return false }

func TestUploadSignedURLDownloadAndDeleteBehavior(t *testing.T) {
	WireFileStorage("ContentStorage", runtime.NewMemoryFileStorage())
	store := runtime.NewMemoryEventStore()
	uow := runtime.NewUnitOfWork(store)
	Wire(uow)

	ctx := runtime.WithCaller(context.Background(), docsStubCaller{})

	docId, err := NewDocId("doc-1")
	if err != nil {
		t.Fatal(err)
	}

	uploadCmd := UploadContent{
		DocId:   docId,
		Content: runtime.File{Name: "a.txt", ContentType: "text/plain", Size: 5, Buffer: []byte("hello")},
	}
	if err := UploadContentUC(ctx, uploadCmd); err != nil {
		t.Fatalf("UploadContentUC: %v", err)
	}

	url, err := GetContentUrl(ctx, store, docId)
	if err != nil {
		t.Fatalf("GetContentUrl: %v", err)
	}
	if !strings.HasPrefix(url, "memfile://") {
		t.Fatalf("esperava uma URL com o formato do backend in-memory, got %q", url)
	}

	downloaded, err := DownloadContent(ctx, store, docId)
	if err != nil {
		t.Fatalf("DownloadContent: %v", err)
	}
	if string(downloaded.Buffer) != "hello" {
		t.Fatalf("esperava bytes %q, got %q", "hello", downloaded.Buffer)
	}

	deleteCmd := DeleteContent{DocId: docId}
	if err := DeleteContentUC(ctx, deleteCmd); err != nil {
		t.Fatalf("DeleteContentUC: %v", err)
	}
	if _, err := DownloadContent(ctx, store, docId); err == nil {
		t.Fatal("esperava erro ao baixar conteúdo já removido (delete file limpou o storage)")
	}
}
`

func TestFileStorageBehavior(t *testing.T) {
	files := generateFileStorageProject(t)
	filesMap := filesToMap(files)
	filesMap[filepath.Join("docs", "filestorage_behavior_test.go")] = []byte(fileStorageBehaviorTest)
	dir := gentest.WriteFiles(t, filesMap)
	gentest.RunTests(t, dir, "30s")
}
