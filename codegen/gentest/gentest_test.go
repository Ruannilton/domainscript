package gentest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestGoldenComparesFixedFile exercita Golden sobre um arquivo Go fixo — ainda
// sem nenhum emissor real (E0.2 só prova que o helper funciona).
func TestGoldenComparesFixedFile(t *testing.T) {
	got := []byte("package example\n\nfunc Hello() string { return \"hi\" }\n")
	Golden(t, filepath.Join("testdata", "example.go.golden"), got)
}

// TestGoldenFailsOnMismatch prova o lado negativo: conteúdo diferente do
// golden falha.
func TestGoldenFailsOnMismatch(t *testing.T) {
	failed := runFake(func(tb TB) {
		Golden(tb, filepath.Join("testdata", "example.go.golden"), []byte("package example\n\n// bytes diferentes\n"))
	})
	if !failed {
		t.Fatal("esperava que Golden falhasse sobre conteúdo diferente do golden")
	}
}

// TestDeterministicPassesOnStableGenerator prova que Deterministic aceita um
// gerador que devolve sempre os mesmos bytes.
func TestDeterministicPassesOnStableGenerator(t *testing.T) {
	Deterministic(t, func() []byte {
		return []byte("package example\n")
	})
}

// TestDeterministicFailsOnUnstableGenerator prova o lado negativo: um gerador
// que muda entre chamadas falha.
func TestDeterministicFailsOnUnstableGenerator(t *testing.T) {
	n := 0
	failed := runFake(func(tb TB) {
		Deterministic(tb, func() []byte {
			n++
			return fmt.Appendf(nil, "run %d", n)
		})
	})
	if !failed {
		t.Fatal("esperava que Deterministic falhasse sobre um gerador instável")
	}
}

// TestSmokeCompileBuildsTrivialProject prova que SmokeCompile escreve os
// arquivos e roda go build/go vet com sucesso sobre um projeto Go mínimo.
func TestSmokeCompileBuildsTrivialProject(t *testing.T) {
	SmokeCompile(t, map[string][]byte{
		"go.mod":  []byte("module example.com/smoke\n\ngo 1.22\n"),
		"main.go": []byte("package main\n\nfunc main() {}\n"),
	})
}

// TestSmokeCompileFailsOnBrokenGo prova o lado negativo: Go sintaticamente
// inválido falha o smoke compile.
func TestSmokeCompileFailsOnBrokenGo(t *testing.T) {
	failed := runFake(func(tb TB) {
		SmokeCompile(tb, map[string][]byte{
			"go.mod":  []byte("module example.com/smoke\n\ngo 1.22\n"),
			"main.go": []byte("package main\n\nfunc main( {\n"),
		})
	})
	if !failed {
		t.Fatal("esperava que SmokeCompile falhasse sobre Go inválido")
	}
}

// fakeTB é um TB mínimo para exercitar os caminhos de falha dos helpers sem
// que a falha vire uma falha real da suíte (testing.TB não pode ser
// implementada fora do pacote testing — daí a interface própria TB).
type fakeTB struct {
	failed  bool
	tmpDirs []string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
	runtime.Goexit()
}

func (f *fakeTB) TempDir() string {
	dir, err := os.MkdirTemp("", "gentest-fake-*")
	if err != nil {
		panic(err)
	}
	f.tmpDirs = append(f.tmpDirs, dir)
	return dir
}

func (f *fakeTB) cleanup() {
	for _, dir := range f.tmpDirs {
		os.RemoveAll(dir)
	}
}

// runFake roda fn com um fakeTB, numa goroutine própria (Fatalf chama
// runtime.Goexit, como o testing.T real faria), e devolve se fn sinalizou
// falha.
func runFake(fn func(TB)) bool {
	f := &fakeTB{}
	defer f.cleanup()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	<-done
	return f.failed
}
