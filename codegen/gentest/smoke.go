package gentest

import (
	"os"
	"os/exec"
	"path/filepath"
)

// SmokeCompile escreve files (caminho relativo → conteúdo) num diretório
// temporário e roda "go build ./..." e "go vet ./..." sobre a saída (NFR-14):
// o Go gerado a partir de um programa válido deve sempre compilar e passar
// vet. Falha o teste, com a saída do comando, se qualquer um dos dois falhar.
func SmokeCompile(t TB, files map[string][]byte) {
	t.Helper()

	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("gentest: não consegui criar %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("gentest: não consegui escrever %q: %v", path, err)
		}
	}

	runGo(t, dir, "build", "./...")
	runGo(t, dir, "vet", "./...")
}

func runGo(t TB, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gentest: `go %s` falhou em %q: %v\n%s", args[0], dir, err, out)
	}
}
