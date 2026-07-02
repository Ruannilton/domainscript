package rtsrc

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"domainscript/codegen/gentest"
)

// runtimeGoMod is the go.mod written for the isolated runtime project: no
// require block (the runtime depends only on stdlib — REQ-16.2).
const runtimeGoMod = "module runtimetest\n\ngo 1.22\n"

// TestSourcesNotEmptyAndWellFormed sanity-checks Sources() itself: it must
// find every expected file, with the ".go.txt" suffix stripped down to
// ".go", and no directories.
func TestSourcesNotEmptyAndWellFormed(t *testing.T) {
	srcs, err := Sources()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) == 0 {
		t.Fatal("Sources() não devolveu nenhum arquivo")
	}
	for name, content := range srcs {
		if filepath.Ext(name) != ".go" {
			t.Errorf("nome inesperado (sem sufixo .go): %q", name)
		}
		if len(content) == 0 {
			t.Errorf("%q veio vazio", name)
		}
	}
	// Peças-chave do §design 3.1a/3.7/3.8 têm que estar presentes.
	for _, want := range []string{
		"event.go", "eventstore.go", "repository.go", "dispatcher.go",
		"uow.go", "idempotency.go", "caller.go", "errors.go", "decimal.go",
		"contextkeys.go", "util.go", "appendlist.go",
	} {
		if _, ok := srcs[want]; !ok {
			t.Errorf("Sources() não contém %q", want)
		}
	}
}

// TestSourcesSmokeCompileAndVet copia Sources() para um projeto Go isolado
// (go.mod sem require) e roda "go build ./..." e "go vet ./..." sobre ele —
// prova que o runtime vendorado compila e é limpo, isolado do compilador
// (conclusão literal de E2.1).
func TestSourcesSmokeCompileAndVet(t *testing.T) {
	srcs, err := Sources()
	if err != nil {
		t.Fatal(err)
	}

	files := map[string][]byte{"go.mod": []byte(runtimeGoMod)}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}
	gentest.SmokeCompile(t, files)
}

// TestSourcesBehavioralTestsPass roda "go test ./..." sobre o mesmo projeto
// isolado: runtime_test.go.txt vai junto (embutido como qualquer outro
// arquivo) e exercita o comportamento de cada peça-chave do runtime
// (BusinessError+errors.Is, Decimal, AppendList, EventStore, Dispatcher,
// UnitOfWork, ...). SmokeCompile só roda build/vet; aqui rodamos test à
// parte para não misturar as duas responsabilidades.
func TestSourcesBehavioralTestsPass(t *testing.T) {
	srcs, err := Sources()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(runtimeGoMod), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range srcs {
		if err := os.WriteFile(filepath.Join(runtimeDir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("`go test ./...` falhou em %q: %v\n%s", dir, err, out)
	}
}

// TestSourcesDeterministic prova que reler Sources() (a "geração" do
// runtime, que é sempre o mesmo template estável — REQ-16.4) produz bytes
// idênticos entre execuções.
func TestSourcesDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		srcs, err := Sources()
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(srcs))
		for name := range srcs {
			names = append(names, name)
		}
		sort.Strings(names)

		var buf bytes.Buffer
		for _, name := range names {
			buf.WriteString("=== " + name + " ===\n")
			buf.Write(srcs[name])
		}
		return buf.Bytes()
	})
}

// TestNamesIsSortedAndMatchesSources prova que Names() devolve exatamente
// as chaves de Sources(), ordenadas.
func TestNamesIsSortedAndMatchesSources(t *testing.T) {
	srcs, err := Sources()
	if err != nil {
		t.Fatal(err)
	}
	names, err := Names()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(srcs) {
		t.Fatalf("Names() tem %d itens, Sources() tem %d", len(names), len(srcs))
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("Names() não está ordenado: %v", names)
	}
	for _, name := range names {
		if _, ok := srcs[name]; !ok {
			t.Errorf("Names() devolveu %q, ausente em Sources()", name)
		}
	}
}
