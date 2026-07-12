# DomainScript

Transpilador de dois estágios para **DomainScript** (spec v6.0): um **front-end**
que vai do texto-fonte até um veredito de validação, e um **back-end** que, a
partir de um programa já validado, gera um projeto Go completo, legível e
compilável. Recebe um ou mais arquivos DomainScript e produz (a) uma AST
validada, (b) um relatório de diagnósticos (erros e avisos) com localização
precisa e, quando não há erros, (c) o código Go que realiza o domínio descrito.

```
                    ┌──────────────── FRONT-END ────────────────┐
texto-fonte ─▶ LEXER ─▶ tokens ─▶ PARSER ─▶ AST ─▶ RESOLVER ─▶ CHECKER ─▶ programa validado
                                                                   │            │
                                                                   ▼            ▼ sem erros
                                                           relatório de   ┌── BACK-END ──┐
                                                           diagnósticos   │  gerador Go  │──▶ projeto Go
                                                                          └──────────────┘   (go build ✓)
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
com um `DiagnosticBag` compartilhado e acumulativo. O back-end (`codegen/`,
orquestrado por `driver.GenerateProject`) roda só quando o `DiagnosticBag` não
tem erros, e consome exclusivamente o que o front-end já produziu
(`program.Program`, `symbols.SymbolTable`, `types.Model`) — nunca re-lexa,
re-parseia ou re-valida. Um pacote por responsabilidade, com dependências
apontando "para baixo" (`driver → sema → resolver → parser → lexer →
ast/token/diag`; `driver → codegen → codegen/{emit,lower,rtsrc,grpcrt,otelrt,
sqlrt}`). Ver `.claude/specs/` para requisitos, design e plano de implementação
de cada ciclo.

## Back-end (geração de Go)

`driver.GenerateProject` (e a CLI `dsc gen`) transformam um programa
DomainScript validado num projeto Go idiomático e compilável: `go.mod`, um
pacote `runtime/` vendorado (event store, dispatcher, unit of work,
idempotency store, tudo atrás de interfaces), um pacote Go por módulo de
domínio, `contracts/` para os `PublicEvent`s compartilhados entre módulos, e um
`cmd/<service>/main.go` por service da topologia (ou um único grupo default
quando não há `topology.ds`, como no exemplo Wallet).

Cada construto do spec (ValueObject, Enum, Error, Event, Aggregate, Command,
UseCase, View, Query, Projection, Policy, Worker, Saga, Notification, Adapter,
Foreign, Metric, Upcast) vira uma forma canônica de Go — a mesma entrada sempre
produz os mesmos bytes (determinismo, NFR-13). O núcleo transacional
(persistência in-memory, HTTP via `net/http`, canais de topologia in-process)
depende só da stdlib Go e do runtime vendorado; toda dependência externa é
**opt-in e isolada atrás de interface**, e só aparece no `go.mod` quando o
programa de fato a exige: um driver `database/sql` real para um `Database` com
provider reconhecido, `google.golang.org/grpc` para uma `Interface GRPC`,
OpenTelemetry para um bloco `Telemetry`. Testes declarativos (`*.test.ds`)
também são gerados como testes Go executáveis (`go test`), incluindo mocks de
Adapter, injeção de falha por step e testes baseados em propriedades.

Ver `.claude/specs/codegen/{requirements,design,tasks}.md` para o ciclo
completo (REQ-14..32, NFR-11..17).

## Uso

### CLI

```sh
go build -o dsc ./cmd/dsc            # compila a CLI

dsc caminho/para/arquivo.ds          # valida um único arquivo
dsc caminho/para/projeto             # valida um projeto inteiro (regras cross-file)
dsc check caminho/para/arquivo.ds    # forma explícita do subcomando (equivalente à de cima)
dsc gen caminho/para/projeto -o out  # valida e, se válido, gera o projeto Go em out/
```

`dsc <caminho>` (sem subcomando) e `dsc check <caminho>` são equivalentes — o
primeiro argumento só é tratado como subcomando quando é `check` ou `gen`;
qualquer outro valor é o caminho de `check`, para retrocompatibilidade. Ambos
aceitam arquivo ou diretório. A CLI imprime o relatório de diagnósticos
(`linha:coluna: severidade: mensagem`) e retorna o exit code: `0` sem erros,
`1` com erros de validação, `2` para erro de uso ou de IO. `dsc gen` recusa
gerar (exit 1) quando o programa tem erro, sem escrever nada em `out/`; a
escrita é idempotente (regenerar no mesmo `out/` produz os mesmos bytes e
limpa arquivos órfãos de declarações removidas).

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

// Geração de Go: só escreve em out se o programa em dir não tiver erros.
import "domainscript/codegen"

bag, err := driver.GenerateProject(dir, out, codegen.Options{})
```

`CheckSource` e `CheckProject` nunca devolvem AST `nil` e acumulam todos os
diagnósticos (léxico, sintaxe, semântica) num único `DiagnosticBag` determinístico.
`GenerateProject` devolve esse mesmo `DiagnosticBag`; se `bag.HasErrors()`, nada é
escrito em `out` e `err` é (ou envolve) `codegen.ErrHasDiagnostics`.

## Estado

**Completo** — front-end e back-end. Todas as fases dos três planos
(`.claude/specs/transpilador/tasks.md`, `.claude/specs/type-checking/tasks.md`,
`.claude/specs/codegen/tasks.md`) implementadas.

Front-end (`.claude/specs/transpilador/requirements.md` §5):

1. Todos os construtos do spec v6 reconhecidos pelo parser (REQ-2).
2. Toda regra ❌ da §23 detectada e reportada como erro (REQ-5).
3. Toda regra ⚠️ da §23 detectada e reportada como aviso (REQ-5).
4. Recovery do parser reporta múltiplos problemas por execução (REQ-3).
5. Cada fase e cada regra com cobertura de teste positivo e negativo (NFR-4),
   auditada em `sema/rules_audit_test.go`.
6. CLI processa um diretório de projeto com exit code coerente (REQ-8).
7. Nenhuma entrada causa crash ou laço infinito (NFR-2), incl. fuzzing leve.

Back-end (`.claude/specs/codegen/requirements.md` §5):

1. Todo construto do spec v6 modelado pelo front-end é gerado em Go idiomático
   (exceções documentadas em §1.3: exposição TCP/UDP, o receptor `tenant` em
   corpos, `provision tenant(id)`, acesso nativo `events()` — nenhuma modelada
   pelo front-end, então fora de escopo também no gerador).
2. O gerado a partir de `docs/examples/wallet` e `docs/examples/shop` compila
   (`go build ./...`) e passa os testes de fumaça.
3. Saída determinística: dois runs produzem bytes idênticos.
4. O núcleo transacional compila e roda sem nenhuma dependência externa.
5. Cada emissor tem golden test; dependências externas (DB, gRPC, OTel) isoladas
   atrás de interfaces e ausentes quando não usadas.
6. A CLI `dsc gen` gera um projeto válido e recusa (exit ≠ 0) programas com erros.
7. `go build ./...` e `go test ./...` do compilador permanecem verdes.
