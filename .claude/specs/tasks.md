# Tasks — Front-end do Transpilador DomainScript

> Documento 3 de 3 do fluxo spec-driven (`requirements` → `design` → `tasks`).
> Plano de implementação executável. Cada tarefa referencia os requisitos
> (`requirements.md`) e o componente de design (`design.md`) que realiza.

## Como ler este plano

- Todas as tarefas começam `[ ]` (pendentes). **Nada está implementado ainda** —
  o projeto parte do zero, incluindo o setup do repositório.
- A ordem respeita dependências. Dentro de uma fase, fatie **verticalmente**: um
  construto completo (lexer→parser→semântica→teste) antes de alargar.
- Cada tarefa tem **critério de conclusão** verificável (em geral "testes X
  passam") e termina com uma etapa de **commit** do git.
- `(REQ-n)` = requisito atendido; `(§design x)` = seção de design correspondente.

## Convenção de commits

Cada tarefa fecha com um commit atômico em [Conventional Commits](https://www.conventionalcommits.org):

```
<tipo>(<escopo>): <descrição no imperativo>
```

- **tipos:** `feat` (novo comportamento), `test` (só testes), `refactor`,
  `chore` (setup/infra), `docs`, `fix`.
- **escopos usados aqui:** `lexer`, `parser`, `ast`, `diag`, `sema`, `resolver`,
  `symbols`, `program`, `cli`, `repo`.
- Regra: **só commitar com a árvore verde** (`go build ./...` e `go test ./...`
  passando). Um commit por tarefa concluída.
- Sugestão: trabalhar cada tarefa num branch curto e fazer merge ao concluir, ou
  commitar direto em `main` se o fluxo for trunk-based. O comando de commit em
  cada tarefa assume trunk-based; adapte se usar branches.

---

## Fase 0 — Setup do Projeto

> Cria o repositório, o módulo Go e a estrutura de diretórios antes de qualquer
> código de produção. Sem dependências.

- [x] **0.1** Inicializar o repositório git e o módulo Go.
  - `git init`
  - `go mod init domainscript`
  - Criar `.gitignore` (binários, `*.out`, `/dist`, caches de IDE, `*.test`).
  - Criar `README.md` raiz com nome, propósito (front-end do transpilador
    DomainScript) e instruções de build (`go build ./...`, `go test ./...`).
  _(§design 1.3, §design 2)_
  **Conclusão:** `go version` e `go mod verify` ok; `git status` limpo após o
  commit inicial.
  **Commit:** `chore(repo): init go module, git e .gitignore`

- [x] **0.2** Criar a árvore de diretórios de pacotes (vazia, com placeholders).
  - `cmd/dsc/`, `token/`, `diag/`, `lexer/`, `ast/`, `parser/`, `symbols/`,
    `resolver/`, `sema/`, `program/`, `driver/`.
  - Em cada pacote, um arquivo `doc.go` com o comentário de pacote (`// Package x …`)
    para o diretório existir no git e o pacote compilar vazio.
  _(§design 2)_
  **Conclusão:** `go build ./...` compila todos os pacotes vazios sem erro.
  **Commit:** `chore(repo): scaffold da árvore de pacotes`

- [x] **0.3** Configurar tooling de qualidade e CI mínimo.
  - `Makefile` (ou `Taskfile`) com alvos `build`, `test`, `lint`, `fmt`.
  - Hook/etapa de `gofmt`/`go vet`.
  - (Opcional) workflow de CI que roda `go test ./...` em cada push.
  _(NFR-4)_
  **Conclusão:** `make test` roda a suíte (ainda vazia) com sucesso.
  **Commit:** `chore(repo): Makefile, go vet e CI de testes`

---

## Fase 1 — Fundações de Código

> Estruturas compartilhadas por todas as fases. Sem lógica de linguagem ainda.

- [x] **1.1** Pacote `token`: `TokenKind` (enum), `Token`, `Pos`, mapa de keywords,
  `String()` para mensagens. _(REQ-1.1/1.4, §design 3.1)_
  **Conclusão:** testes de `String()` e de lookup de keyword passam.
  **Commit:** `feat(token): TokenKind, Token, Pos e keywords`

- [x] **1.2** Pacote `diag`: `Severity`, `Diagnostic`, `DiagnosticBag` (dedup, teto
  configurável, ordenação estável por posição, `Render`). Reservar campo de
  **código de diagnóstico** (`E001`/`W014`) sem preencher. _(REQ-6.1/6.3/6.4/6.5/6.6,
  §design 3.4)_
  **Conclusão:** testes de dedup, teto e ordenação passam.
  **Commit:** `feat(diag): DiagnosticBag com dedup, teto e ordenação`

- [x] **1.3** Pacote `ast`: interfaces `Node`/`Decl`/`Stmt`/`Expr`, tipo `Span`
  (início+fim) e os nós de erro `ErrorDecl`/`ErrorStmt`/`ErrorExpr`. _(REQ-2.6/2.7,
  §design 3.3)_
  **Conclusão:** interfaces compilam; nós de erro satisfazem as interfaces; teste de
  `Span()` passa.
  **Commit:** `feat(ast): interfaces base, Span e nós de erro`

---

## Fase 2 — Léxico Completo (REQ-1)

> Implementa toda a tokenização do spec. Fatiar por categoria de token.

- [x] **2.1** Lexer single-pass: identificadores, keywords, inteiros, decimais,
  strings, booleanos; trivia (espaços) e comentários `//` descartados. _(REQ-1.2/1.5,
  §design 3.2)_
  **Conclusão:** tabela de testes (fonte→tokens) cobre cada caso.
  **Commit:** `feat(lexer): identificadores, literais e trivia`

- [x] **2.2** Pontuação e operadores: `{ } ( ) [ ] , . :` e `-> + - * / == != > < >= <= =`.
  _(REQ-1.3)_
  **Conclusão:** testes de cada operador, incl. os de 2 chars, passam.
  **Commit:** `feat(lexer): pontuação e operadores`

- [x] **2.3** Strings com escapes (`\n \t \" \\`) e detecção de não-terminada
  (linha/EOF) com diagnóstico localizado. _(REQ-1.7/1.8)_
  **Conclusão:** testes de escape e de string aberta passam.
  **Commit:** `feat(lexer): escapes e detecção de string não terminada`

- [x] **2.4** Literais compostos do domínio como kinds dedicados: duração
  (`5s`,`48h`), taxa (`300/min`), tamanho (`100MB`), `version_id` (`v1`). _(REQ-1.2,
  §design 3.1/3.2)_
  **Conclusão:** lexer emite `DURATION`/`RATE`/`SIZE`/`VERSION`; testes por unidade
  passam.
  **Commit:** `feat(lexer): durações, taxas, tamanhos e version_id`

- [x] **2.5** Caractere inválido emite diagnóstico e garante progresso (consome ≥1
  rune). _(REQ-1.6, NFR-2)_
  **Conclusão:** teste de char inválido + ausência de laço infinito.
  **Commit:** `feat(lexer): recovery de caractere inválido`

---

## Fase 3 — Infra de Parsing e Recovery (REQ-2, REQ-3)

> O mecanismo de recovery, antes de qualquer construto. É a base reutilizada por
> toda a Fase 4.

- [x] **3.1** Cursor do parser e `expect` com inserção virtual + deleção de token
  único. _(REQ-3.2/3.3, §design 3.5)_
  **Conclusão:** testes unitários de `expect` nos três caminhos.
  **Commit:** `feat(parser): cursor e expect com recovery`

- [x] **3.2** `synchronize` que nunca consome o token de parada nem `}`/EOF;
  arquivo `sync_sets.go` com `topLevelStop` (todas as keywords de topo) e sets por
  nível. _(REQ-3.4/3.7, §design 3.5)_
  **Conclusão:** teste de sincronização que não come o `}` de fechamento.
  **Commit:** `feat(parser): synchronize e sync sets por nível`

- [x] **3.3** Janela de silêncio anti-cascata + garantia de progresso em loops.
  _(REQ-3.5/3.6, NFR-1/2)_
  **Conclusão:** teste em que dois erros adjacentes não viram 5+; teste de não-laço.
  **Commit:** `feat(parser): janela de silêncio e garantia de progresso`

---

## Fase 4 — Parser e AST do Núcleo de Domínio (REQ-2)

> O grosso da gramática `*.ds`. Cada tarefa é uma **fatia vertical**: nós de AST +
> parser + teste happy-path + ≥1 teste de recovery, e fecha em commit.

### 4A. Expressões e controle de fluxo (base para os construtos)
- [x] **4A.1** Cadeia de precedência `or`→`and`→igualdade→relacional→aditivo→
  multiplicativo→unário→pós-fixo; membro/método/chamada/construção; named args.
  _(REQ-2.5, §design 3.3)_
  **Conclusão:** teste de precedência (`a + b * c >= d`) monta a árvore certa.
  **Commit:** `feat(parser): expressões com precedência e pós-fixo`
- [x] **4A.2** `match` como **statement** e como **expressão**: braços de valor,
  guards (`when`), wildcard `_`. _(REQ-2.4)_
  **Commit:** `feat(parser): match statement e expressão`
- [x] **4A.3** Range (`1..n`) e lambdas (`t => t.x`). _(REQ-2.5)_
  **Commit:** `feat(parser): range e lambdas`
- [x] **4A.4** Operações embutidas em expressão: `load`, `list`, `count`, `store`,
  `call`, `signed_url`, `exists` com cláusulas estilo SQL (`where`/`join`/`orderBy`/
  `skip`/`take`). _(REQ-2.4/2.5)_
  **Commit:** `feat(parser): operações de domínio em expressão`
- [x] **4A.5** `ensure … else …` (ações por contexto: Error/Nop/break/break all/
  continue), `return`, `for … in …`, `log`, `emit`, controles de laço. _(REQ-2.4)_
  **Commit:** `feat(parser): controle de fluxo (ensure, for, log, emit)`

### 4B. Declarações de domínio (uma fatia vertical cada)
- [x] **4B.1** `ValueObject` (wrapper `Email(string)` e composto `Money`), bloco
  `Valid`, `Operator` com parâmetro/retorno/corpo. _(REQ-2.1)_
  **Commit:** `feat(parser): declaração ValueObject`
- [x] **4B.2** `Enum` (base string/integer/VO, membros, bloco `coerce` com `match`).
  _(REQ-2.1)_
  **Commit:** `feat(parser): declaração Enum com coerce`
- [x] **4B.3** `Error`, `Event`/`PublicEvent` (campos, `redactable`, defaults).
  _(REQ-2.1)_
  **Commit:** `feat(parser): Error e Event/PublicEvent`
- [x] **4B.4** `Aggregate` (`strategy`, `snapshot`, `storage`, `state`, `access`,
  `Handle`, `Apply`). _(REQ-2.1)_
  **Commit:** `feat(parser): declaração Aggregate`
- [x] **4B.5** `Command` (campos com `ref`). _(REQ-2.1)_
  **Commit:** `feat(parser): declaração Command`
- [x] **4B.6** `UseCase` (`handles`, `timeout`, `idempotency`, `tenancy`,
  `execute`). _(REQ-2.1)_
  **Commit:** `feat(parser): declaração UseCase`
- [x] **4B.7** `View` (incl. `From` e `visibility`), `Projection`, `Query` (com
  `cache`). _(REQ-2.1)_
  **Commit:** `feat(parser): View, Projection e Query`
- [x] **4B.8** `Policy` (`on`, `delivery`, `execute`). _(REQ-2.1)_
  **Commit:** `feat(parser): declaração Policy`
- [x] **4B.9** `Worker` (`every`/`cron`/`continuous`, `scope`, `onError`,
  `source`). _(REQ-2.1)_
  **Commit:** `feat(parser): declaração Worker`
- [x] **4B.10** `Notification`, `Adapter` (HTTP e FFI), `Foreign` (FFI geral).
  _(REQ-2.1)_
  **Commit:** `feat(parser): Notification, Adapter e Foreign`
- [x] **4B.11** `Saga` (`mode async/await`, `state`, `step` com `up`/`down`/
  `onInfraError`, `unrecoverable`). _(REQ-2.1)_
  **Commit:** `feat(parser): declaração Saga`
- [x] **4B.12** `Metric` e `Upcast`. _(REQ-2.1)_
  **Commit:** `feat(parser): Metric e Upcast`
- [x] **4B.13** `parseFile`: `switch` na keyword de topo reunindo todos os
  construtos; recovery reancora na próxima declaração. _(REQ-2.1/3.7)_
  **Conclusão:** arquivo `.ds` multi-declaração com erros reporta cada um.
  **Commit:** `feat(parser): roteamento de declarações de topo`

---

## Fase 5 — Arquivos de Configuração e Teste (REQ-2.2/2.3)

- [x] **5.1** `config_entry` genérico (literal/`env`/duração/taxa/lista/objeto),
  reusado pelos arquivos de infra. _(REQ-2.2, §design 3.5)_
  **Commit:** `feat(parser): config_entry genérico de infraestrutura`
- [x] **5.2** `mod.ds`: `Module` + `Database`, `FileStorage`, `Idempotency`,
  `Cache`, `RateLimit`, `Outbox`, `Telemetry`. _(REQ-2.2)_
  **Commit:** `feat(parser): mod.ds (Module e blocos de infra)`
- [x] **5.3** `interface.ds`: `Interface HTTP` (rotas, `versioning`, `tenant`,
  `rateLimit`), `Interface GRPC`, `RateLimitTier`. _(REQ-2.2)_
  **Commit:** `feat(parser): interface.ds (HTTP, gRPC, tiers)`
- [x] **5.4** `topology.ds`: `Topology` com `services` e `channels`. _(REQ-2.2)_
  **Commit:** `feat(parser): topology.ds (services e channels)`
- [x] **5.5** `contracts/*.ds` (`PublicEvent`) e `versions/*.ds` (`Version` com
  `upcast`/`downcast`/`route`, `deprecated`/`sunset`). _(REQ-2.2)_
  **Commit:** `feat(parser): contracts e versions`
- [x] **5.6** `*.test.ds`: `Test`, `scenario`, `given`/`when`/`then`, `mock`,
  `fail step`, `property`, `Fixture`. _(REQ-2.3, §design 3.5)_
  **Conclusão:** o driver roteia o parser pelo nome do arquivo; testes por tipo.
  **Commit:** `feat(parser): arquivos *.test.ds`

---

## Fase 6 — Símbolos e Resolução de Nomes (REQ-4)

- [x] **6.1** `SymbolTable` com escopo por módulo + nível público; flag
  `Event`/`PublicEvent`. _(REQ-4.1/7.4, §design 3.6)_
  **Commit:** `feat(symbols): SymbolTable com escopo por módulo`
- [x] **6.2** Passagem de coleta: registra toda declaração; duplicada → erro.
  _(REQ-4.1/4.3, §design 3.7)_
  **Commit:** `feat(resolver): coleta de símbolos e duplicatas`
- [x] **6.3** Passagem de resolução: liga `ref`, `handles`, `on`, tipos de campo e
  parâmetro; não resolvida → erro; pula subárvores de erro. _(REQ-4.2/4.4/4.5)_
  **Conclusão:** testes de símbolo inexistente (dispara), duplicado (dispara) e
  programa correto (silêncio).
  **Commit:** `feat(resolver): resolução de referências`

---

## Fase 7 — Agregação de Programa (REQ-7)

> Necessária antes das regras semânticas cross-file.

- [x] **7.1** `Program`: agrega ASTs de um diretório num modelo unificado. _(REQ-7.1,
  §design 3.8)_
  **Commit:** `feat(program): agregação de arquivos do projeto`
- [x] **7.2** Grafo módulo→service→canal de `topology.ds`+`mod.ds`; mapear
  aggregates→Database→módulo→service. _(REQ-7.2/7.3)_
  **Conclusão:** dado um diretório, `Program` expõe símbolos globais e o grafo.
  **Commit:** `feat(program): grafo de topologia e mapeamento de aggregates`

---

## Fase 8 — Validação Semântica: Regras Locais (REQ-5, ❌/⚠️ por-arquivo)

> Cada tarefa = uma regra = **teste positivo + negativo** (NFR-4) + commit.

- [x] **8.1** Primitivo no Write Side. _(REQ-5.1, §design rules_types)_
  **Commit:** `feat(sema): proíbe primitivo no Write Side`
- [x] **8.2** `remove()`/`clear()` em `AppendList<T>`. _(REQ-5.4)_
  **Commit:** `feat(sema): proíbe remove/clear em AppendList`
- [x] **8.3** `match` exaustivo; `_` proibido/obrigatório conforme guards. _(REQ-5.5,
  §design 4.2)_
  **Commit:** `feat(sema): exaustividade de match`
- [x] **8.4** `Nop` em `Handle`/`UseCase`. _(REQ-5.6)_
  **Commit:** `feat(sema): proíbe Nop em Handle/UseCase`
- [x] **8.5** `break`/`continue`/`break all` fora de `for`. _(REQ-5.7)_
  **Commit:** `feat(sema): controle de laço fora de for`
- [x] **8.6** `Handle` sem entrada no `access`. _(REQ-5.2, §design rules_domain)_
  **Commit:** `feat(sema): Handle exige entrada em access`
- [x] **8.7** `Notification` sem `Adapter`. _(REQ-5.3)_
  **Commit:** `feat(sema): Notification exige Adapter`
- [x] **8.8** Upcast de versão com campo obrigatório sem default. _(REQ-5.13)_
  **Commit:** `feat(sema): upcast exige default em campo obrigatório`
- [x] **8.9** Validações de `*.test.ds`: evento/comando inexistente, shape errada,
  `fail step` inexistente, mock com tipo errado. _(REQ-5.14)_
  **Commit:** `feat(sema): validação de arquivos de teste`
- [x] **8.10** Assinatura incompatível em `Foreign`/`Adapter`. _(REQ-5.15)_
  **Commit:** `feat(sema): assinatura FFI/Adapter`
- [x] **8.11** ⚠️ ValueObject que poderia ser Enum. _(REQ-5.19)_
  **Commit:** `feat(sema): aviso VO que poderia ser Enum`
- [x] **8.12** ⚠️ Canal `queue`/`stream` sem `orderBy`. _(REQ-5.16)_
  **Commit:** `feat(sema): aviso canal sem orderBy`
- [x] **8.13** ⚠️ Cache em listagem de alta cardinalidade. _(REQ-5.20)_
  **Commit:** `feat(sema): aviso cache de alta cardinalidade`
- [x] **8.14** ⚠️ `Handle` sem cenário de erro testado (cobertura). _(REQ-5.22)_
  **Commit:** `feat(sema): aviso de cobertura de erro por Handle`

---

## Fase 9 — Validação Semântica: Regras Cross-File (REQ-5 + REQ-7)

> Dependem da Fase 7 (Program). Cada tarefa = par de testes + commit.

- [x] **9.1** Cross-database sem XA → erro; cross-service sem Saga → erro. _(REQ-5.9,
  §design 4.3)_
  **Commit:** `feat(sema): transações cross-database e cross-service`
- [x] **9.2** `JOIN` cross-database → erro. _(REQ-5.10)_
  **Commit:** `feat(sema): proíbe JOIN cross-database`
- [x] **9.3** Módulos em services diferentes sem canal → erro. _(REQ-5.11)_
  **Commit:** `feat(sema): exige canal entre services`
- [x] **9.4** Acesso cross-tenant sem opt-in → erro. _(REQ-5.12)_
  **Commit:** `feat(sema): exige opt-in cross-tenant`
- [x] **9.5** `Policy` cross-module escutando `Event` privado → erro. _(REQ-5.8)_
  **Commit:** `feat(sema): Policy cross-module exige PublicEvent`
- [x] **9.6** ⚠️ Saga `await` sobre canal `queue`. _(REQ-5.17)_
  **Commit:** `feat(sema): aviso Saga await sobre queue`
- [x] **9.7** ⚠️ UseCase cross-tenant declarado (auditoria). _(REQ-5.21)_
  **Commit:** `feat(sema): aviso auditoria cross-tenant`
- [x] **9.8** ⚠️ UseCase/Query não exposto em nenhuma interface. _(REQ-5.23)_
  **Conclusão de fase:** todas as regras ❌/⚠️ da §23 cobertas com par de testes.
  **Commit:** `feat(sema): aviso UseCase/Query não exposto`

---

## Fase 10 — Relatório, Driver e CLI (REQ-6, REQ-8)

- [x] **10.1** API pública `CheckSource` e `CheckProject` no pacote `driver`.
  _(REQ-8.1, §design 3.10)_
  **Commit:** `feat(driver): API CheckSource e CheckProject`
- [x] **10.2** CLI `dsc` (`cmd/dsc`): arquivo **ou** diretório; roteia parser por
  nome de arquivo; exit code coerente com `HasErrors`. _(REQ-8.2/8.3/8.4, REQ-6.7)_
  **Commit:** `feat(cli): dsc para arquivo e diretório`
- [x] **10.3** Revisar mensagens acionáveis (esperado vs. encontrado) em todos os
  caminhos de erro. _(REQ-6.8, NFR-1)_
  **Commit:** `refactor(diag): mensagens acionáveis em todo o pipeline`

---

## Fase 11 — Robustez, Determinismo e Fechamento (NFR-2/3/4)

- [x] **11.1** Testes de robustez: entradas truncadas, adversárias e fuzzing leve;
  ausência de crash e laço. _(NFR-2)_
  **Commit:** `test(parser): robustez e fuzzing leve`
- [x] **11.2** Testes de determinismo: mesma entrada → diagnósticos idênticos em
  ordem. _(NFR-3)_
  **Commit:** `test(diag): determinismo da saída`
- [x] **11.3** Verificar limite de cascata: erro de sintaxe não gera mais que um
  pequeno número fixo de diagnósticos derivados. _(NFR-1, REQ-3.5)_
  **Commit:** `test(parser): limite de cascata`
- [x] **11.4** Auditoria de cobertura: par de testes (positivo+negativo) para
  **cada** regra da §23. _(NFR-4)_ A auditoria revelou e fechou a lacuna da regra
  REQ-5.18 (Upcast substituível por default).
  **Commit:** `test(sema): auditoria de cobertura da §23`
- [x] **11.5** Revisão final contra o **Definition of Done** (`requirements.md` §5);
  atualizar `README.md` com estado e exemplos de uso.
  **Commit:** `docs(repo): fechamento e Definition of Done`

---

## Mapa de Dependências (resumo)

```
Fase 0 (setup) ─▶ Fase 1 (fundações) ─▶ Fase 2 (lexer) ─▶ Fase 3 (recovery)
   │
   └─▶ Fase 4 (parser domínio) ─▶ Fase 5 (config/test) ─▶ Fase 6 (símbolos)
                                                              │
                                                              ▼
                                          Fase 7 (programa) ─┬─▶ Fase 8 (sema local) ─┐
                                                             └─▶ Fase 9 (sema cross) ─┤
                                                                                      ▼
                                                                  Fase 10 (CLI) ─▶ Fase 11 (fechamento)
```

- Fases 8 e 9 dependem de 6 (símbolos); 9 também de 7 (programa).
- A API de arquivo único (10.1) pode adiantar após a Fase 4 para um validador
  parcial; a CLI de projeto (10.2) usa a Fase 7.
- Fase 11 fecha sobre tudo.

---

## Estratégia de Entrega Incremental

A ordem permite **valor utilizável cedo**, validando verticalmente. Cada marco é
demonstrável e mantém `go test ./...` verde:

1. **Marco A — "valida ValueObject e Enum"** (Fases 0–3, 4A, 4B.1–4B.2, 6 parcial,
   10.1 single-file): primeiro validador real, com erros de nome.
2. **Marco B — "valida um módulo de domínio completo"** (Fase 4 inteira, Fases 5–6,
   Fase 8): cobre Aggregate/UseCase/Query/Policy e as regras ❌ por-arquivo.
3. **Marco C — "valida um projeto multi-módulo"** (Fases 7, 9, 10): habilita as
   regras arquiteturais cross-file — o diferencial do DomainScript.
4. **Marco D — "pronto para produção"** (Fase 11): robustez, determinismo e
   cobertura total da §23.
