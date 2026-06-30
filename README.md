# DomainScript

Front-end do transpilador para **DomainScript** (spec v6.0): o estágio que vai do
texto-fonte até um veredito de validação. Recebe um ou mais arquivos DomainScript
e produz (a) uma AST validada e (b) um relatório de diagnósticos (erros e avisos)
com localização precisa. A geração de código Go é um estágio posterior, fora do
escopo deste projeto.

```
texto-fonte ─▶ LEXER ─▶ tokens ─▶ PARSER ─▶ AST ─▶ RESOLVER ─▶ CHECKER ─▶ AST validada
                                                                   │
                                                                   ▼
                                                           relatório de diagnósticos
```

## Princípios

- **Fail-fast:** todo erro detectável estaticamente é reportado na compilação.
- **Diagnósticos como produto:** mensagens com posição, severidade e texto acionável.
- **Cobertura sobre interrupção:** reporta o máximo de problemas reais por execução.
- **Separação de fases:** sintaxe e semântica são estritamente separadas; o
  contrato entre elas é `(AST, DiagnosticBag)`.

## Build e testes

```sh
go build ./...                      # compila todos os pacotes
go test ./...                       # roda toda a suíte
go vet ./...                        # checagens estáticas
gofmt -l .                          # lista arquivos não formatados
```

Ou via `Makefile`:

```sh
make build      # go build ./...
make test       # go test ./...
make lint       # gofmt -l . + go vet ./...
make fmt        # gofmt -w .
```

## Estrutura

O front-end é um pipeline de quatro estágios (lexer → parser → resolver → checker)
com um `DiagnosticBag` compartilhado e acumulativo. Um pacote por responsabilidade,
com dependências apontando "para baixo" (`driver → sema → resolver → parser →
lexer → ast/token/diag`). Ver `.claude/specs/` para requisitos, design e plano de
implementação.

## Uso

### CLI

```sh
go build -o dsc ./cmd/dsc        # compila a CLI

dsc caminho/para/arquivo.ds      # valida um único arquivo
dsc caminho/para/projeto         # valida um projeto inteiro (regras cross-file)
```

A CLI imprime o relatório de diagnósticos (`linha:coluna: severidade: mensagem`) e
retorna o exit code: `0` sem erros, `1` com erros de validação, `2` para erro de
uso ou de IO.

### API programática

```go
import "domainscript/driver"

// Arquivo único: léxico → sintaxe → resolução → regras locais.
file, bag := driver.CheckSource(src)

// Projeto inteiro: agrega os arquivos e habilita as regras cross-file.
prog, bag := driver.CheckProject(dir)

if bag.HasErrors() {
    fmt.Println(bag.Render())
}
```

`CheckSource` e `CheckProject` nunca devolvem AST `nil` e acumulam todos os
diagnósticos (léxico, sintaxe, semântica) num único `DiagnosticBag` determinístico.

## Estado

**Completo** — todas as fases do plano (`.claude/specs/tasks.md`) implementadas,
cobrindo o Definition of Done (`requirements.md` §5):

1. Todos os construtos do spec v6 reconhecidos pelo parser (REQ-2).
2. Toda regra ❌ da §23 detectada e reportada como erro (REQ-5).
3. Toda regra ⚠️ da §23 detectada e reportada como aviso (REQ-5).
4. Recovery do parser reporta múltiplos problemas por execução (REQ-3).
5. Cada fase e cada regra com cobertura de teste positivo e negativo (NFR-4),
   auditada em `sema/rules_audit_test.go`.
6. CLI processa um diretório de projeto com exit code coerente (REQ-8).
7. Nenhuma entrada causa crash ou laço infinito (NFR-2), incl. fuzzing leve.
