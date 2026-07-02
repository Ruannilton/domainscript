package gentest

import (
	"os"
	"path/filepath"
)

// Golden compara got com o conteúdo do arquivo de referência em path
// (tipicamente algo em "testdata/"). Com a variável de ambiente
// UPDATE_GOLDEN=1, regrava o arquivo em vez de comparar — o fluxo de
// atualização dos goldens versionados.
func Golden(t TB, path string, got []byte) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("gentest: não consegui criar %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("gentest: não consegui escrever o golden %q: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("gentest: não consegui ler o golden %q (rode com UPDATE_GOLDEN=1 para criá-lo): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("gentest: saída difere do golden %q\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
