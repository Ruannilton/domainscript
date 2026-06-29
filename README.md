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

## Estado

Em desenvolvimento — fluxo spec-driven (`requirements` → `design` → `tasks`) em
`.claude/specs/`.
