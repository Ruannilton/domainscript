package gentest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// SmokeCompile escreve files (caminho relativo → conteúdo) num diretório
// temporário e roda "go build ./..." e "go vet ./..." sobre a saída (NFR-14):
// o Go gerado a partir de um programa válido deve sempre compilar e passar
// vet. Falha o teste, com a saída do comando, se qualquer um dos dois falhar.
//
// Quando go.mod declara um bloco "require" (G1: um projeto com Database
// provider:"sqlite" — a única dependência externa opt-in deste gerador,
// NFR-12), roda "go mod tidy" primeiro, para resolver/baixar a dependência e
// escrever go.sum — o mesmo passo que um usuário real rodaria ao gerar um
// projeto assim pela primeira vez. Um go.mod SEM "require" (o caso comum,
// núcleo sem dep externa) nunca dispara isso, preservando o comportamento
// (e a velocidade) de antes de G1.
func SmokeCompile(t TB, files map[string][]byte) {
	t.Helper()
	dir := WriteFiles(t, files)

	if needsModTidy(files) {
		runGo(t, dir, "mod", "tidy")
	}

	runGo(t, dir, "build", "./...")
	runGo(t, dir, "vet", "./...")
}

// WriteFiles escreve files (caminho relativo → conteúdo) num diretório
// temporário e devolve o diretório — o passo compartilhado por SmokeCompile
// e por qualquer chamador que precise ACRESCENTAR arquivos próprios (ex. um
// teste comportamental de pacote, mesmo padrão de walletUseCaseBehaviorTest/
// meterUseCaseBehaviorTest em decl_usecase_test.go) antes de compilar/rodar.
func WriteFiles(t TB, files map[string][]byte) string {
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
	return dir
}

// RunTests roda "go test ./... -timeout <timeout>" em dir (já escrito via
// WriteFiles/SmokeCompile) — para quem precisa rodar testes comportamentais
// DE VERDADE sobre o Go gerado (G1: sqlruntime, database/sql real), não só
// compilar. timeout é repassado literal ao -timeout do "go test" interno
// (ex. "30s") — SEMPRE explícito, nunca o default do toolchain: a lição
// documentada em F5 é que um -timeout implícito (10min por pacote) pode
// mascarar uma trava de verdade por tempo demais antes de falhar; aqui o
// chamador escolhe um teto curto de propósito. runGo, por cima, ainda aplica
// seu próprio teto de subprocesso (runGoTimeout) como rede de segurança
// adicional caso o binário de teste em si trave de um jeito que nem o
// -timeout interno pegue (ex. travado ANTES de testing.M rodar).
//
// Roda "go mod tidy" primeiro quando dir/go.mod declara um bloco "require"
// (mesma detecção de SmokeCompile/needsModTidy, agora lendo o go.mod já
// escrito em disco — RunTests não recebe o map[string][]byte original,
// só dir, porque quem chama normalmente já ACRESCENTOU arquivos extras via
// WriteFiles antes de chamar isto, ex. um teste comportamental).
func RunTests(t TB, dir, timeout string) {
	t.Helper()
	if goMod, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && bytes.Contains(goMod, []byte("require ")) {
		runGo(t, dir, "mod", "tidy")
	}
	runGo(t, dir, "test", "./...", "-timeout", timeout)
}

// needsModTidy devolve true se o go.mod do conjunto de arquivos declara um
// bloco "require" (ver a doc de SmokeCompile).
func needsModTidy(files map[string][]byte) bool {
	mod, ok := files["go.mod"]
	return ok && bytes.Contains(mod, []byte("require "))
}

// runGoTimeout é o teto por invocação de "go ..." — generoso o bastante para
// "go mod tidy" baixar uma dependência nova (G1), mas ainda um LIMITE
// explícito: a lição documentada em F5 é que confiar só no -timeout do `go
// test` do processo inteiro pode mascarar uma trava por até o default do
// pacote — aqui cada subprocesso falha rápido e claro em vez de ficar
// pendurado até o timeout externo.
const runGoTimeout = 120 * time.Second

func runGo(t TB, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runGoTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("gentest: `go %s` excedeu %s em %q (possível trava) — saída parcial:\n%s", args[0], runGoTimeout, dir, out)
	}
	if err != nil {
		t.Fatalf("gentest: `go %s` falhou em %q: %v\n%s", args[0], dir, err, out)
	}
}
