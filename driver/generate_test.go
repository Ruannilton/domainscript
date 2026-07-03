package driver

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"domainscript/codegen"
)

// REQ-14.1: um programa com diagnóstico de erro é recusado — GenerateProject
// devolve ErrHasDiagnostics e o bag carrega os diagnósticos para o chamador
// reportar. REQ-32.2: nada é escrito em out nesse caso.
func TestGenerateProjectRefusesInvalidProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.ds"), []byte(`Aggregate Account { state { balance integer } }`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")

	bag, err := GenerateProject(dir, out, codegen.Options{})
	if !errors.Is(err, codegen.ErrHasDiagnostics) {
		t.Fatalf("err = %v, quero codegen.ErrHasDiagnostics", err)
	}
	if !bag.HasErrors() {
		t.Fatalf("esperava o bag com os diagnósticos do programa inválido:\n%s", bag.Render())
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("esperava que nada fosse escrito em %q (programa inválido)", out)
	}
}

// REQ-32.1/32.2: um projeto válido gera de fato — GenerateProject escreve
// go.mod, o runtime vendorado e um pacote por módulo de domínio em out, sem
// erro e sem diagnósticos.
func TestGenerateProjectWritesRealProject(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")

	bag, err := GenerateProject(walletExampleDir, out, codegen.Options{})
	if err != nil {
		t.Fatalf("GenerateProject: erro inesperado sobre o wallet real: %v", err)
	}
	if bag.HasErrors() {
		t.Fatalf("wallet não deveria ter diagnósticos de erro:\n%s", bag.Render())
	}

	for _, rel := range []string{
		"go.mod",
		"runtime/eventstore.go",
		"wallet/value_objects.go",
		"wallet/commands.go",
		"wallet/usecases.go",
		"cmd/wallet/main.go",
	} {
		p := filepath.Join(out, filepath.FromSlash(rel))
		info, statErr := os.Stat(p)
		if statErr != nil {
			t.Errorf("esperava %q em %s, não achei: %v", rel, out, statErr)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%q foi escrito vazio", rel)
		}
	}
}

// NFR-13/REQ-32.3: rodar GenerateProject duas vezes sobre o MESMO projeto no
// MESMO out produz exatamente os mesmos arquivos, byte a byte — e a segunda
// rodada não toca o mtime de um arquivo cujo conteúdo não mudou (prova que a
// escrita idempotente de fato pula o WriteFile, não só produz bytes iguais
// por acaso).
func TestGenerateProjectIdempotentSameBytes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")

	if _, err := GenerateProject(walletExampleDir, out, codegen.Options{}); err != nil {
		t.Fatalf("1ª geração: erro inesperado: %v", err)
	}
	first := snapshotDir(t, out)

	goModPath := filepath.Join(out, "go.mod")
	infoBefore, err := os.Stat(goModPath)
	if err != nil {
		t.Fatalf("não consegui statar go.mod após a 1ª geração: %v", err)
	}

	// Garante uma resolução de mtime observável caso o arquivo seja
	// (incorretamente) reescrito na 2ª geração.
	time.Sleep(10 * time.Millisecond)

	if _, err := GenerateProject(walletExampleDir, out, codegen.Options{}); err != nil {
		t.Fatalf("2ª geração: erro inesperado: %v", err)
	}
	second := snapshotDir(t, out)

	if len(first) != len(second) {
		t.Fatalf("número de arquivos difere entre gerações: %d vs %d\n1ª: %v\n2ª: %v", len(first), len(second), keys(first), keys(second))
	}
	for rel, content := range first {
		got, ok := second[rel]
		if !ok {
			t.Fatalf("%q sumiu na 2ª geração", rel)
		}
		if string(got) != string(content) {
			t.Fatalf("conteúdo de %q difere entre gerações", rel)
		}
	}

	infoAfter, err := os.Stat(goModPath)
	if err != nil {
		t.Fatalf("não consegui statar go.mod após a 2ª geração: %v", err)
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Fatalf("go.mod foi reescrito na 2ª geração sem mudança de conteúdo: mtime %v -> %v", infoBefore.ModTime(), infoAfter.ModTime())
	}
}

// REQ-32.3: remover uma declaração do .ds e regenerar no MESMO out apaga o
// artefato órfão que ela tinha gerado — sem tocar os arquivos de outras
// declarações que continuam de pé. Usa dois módulos (cada um com seu próprio
// value_objects.go) para também provar que o diretório do módulo que fica
// vazio é limpo junto (removeEmptyDirs).
func TestGenerateProjectRemovesOrphanFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "moda/mod.ds", `Module Moda { }`)
	write(t, dir, "moda/domain.ds", `ValueObject Email(string) { Valid { ok } }`)
	write(t, dir, "modb/mod.ds", `Module Modb { }`)
	modbDomain := write(t, dir, "modb/domain.ds", `ValueObject Money(string) { Valid { ok } }`)

	out := filepath.Join(t.TempDir(), "out")
	if _, err := GenerateProject(dir, out, codegen.Options{}); err != nil {
		t.Fatalf("1ª geração: erro inesperado: %v", err)
	}

	modaPath := filepath.Join(out, "moda", "value_objects.go")
	modbPath := filepath.Join(out, "modb", "value_objects.go")
	modaContentBefore, err := os.ReadFile(modaPath)
	if err != nil {
		t.Fatalf("esperava %q após a 1ª geração: %v", modaPath, err)
	}
	if _, err := os.Stat(modbPath); err != nil {
		t.Fatalf("esperava %q após a 1ª geração: %v", modbPath, err)
	}

	// Remove o módulo Modb inteiro (a única declaração dele) e regenera.
	if err := os.Remove(modbDomain); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateProject(dir, out, codegen.Options{}); err != nil {
		t.Fatalf("2ª geração: erro inesperado: %v", err)
	}

	if _, err := os.Stat(modbPath); !os.IsNotExist(err) {
		t.Fatalf("esperava que %q tivesse sido removido (órfão), stat err = %v", modbPath, err)
	}
	if _, err := os.Stat(filepath.Join(out, "modb")); !os.IsNotExist(err) {
		t.Fatalf("esperava que o diretório %q (vazio) tivesse sido removido, stat err = %v", filepath.Join(out, "modb"), err)
	}

	modaContentAfter, err := os.ReadFile(modaPath)
	if err != nil {
		t.Fatalf("%q não deveria ter sumido (módulo Moda continua de pé): %v", modaPath, err)
	}
	if string(modaContentAfter) != string(modaContentBefore) {
		t.Fatalf("conteúdo de %q mudou sem motivo", modaPath)
	}
}

// snapshotDir lê recursivamente todo arquivo regular sob dir (exceto o
// manifesto interno, manifestName, que é um detalhe de implementação de
// writeProject e não faz parte do projeto Go gerado) e devolve um mapa de
// caminho relativo ("/"-separado) para conteúdo.
func snapshotDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() == manifestName {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		out[filepath.ToSlash(rel)] = content
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotDir(%q): %v", dir, err)
	}
	return out
}

func keys(m map[string][]byte) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// write escreve src em dir/rel (criando diretórios pais) e devolve o caminho
// absoluto escrito.
func write(t *testing.T, dir, rel, src string) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
