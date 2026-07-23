# Requirements — Correções de dívida técnica (ISSUE-6, ISSUE-7, ISSUE-8)

> Ciclo de **manutenção** (Marco L), no mesmo molde do Marco K
> (`correcoes-issues-9-10-11`): cada requisito fecha uma issue já registrada em
> `.claude/issues.md`, com análise de raiz validada contra o código e o par de
> testes positivo/negativo que o CLAUDE.md exige (NFR-4). Onde um item exige um
> ciclo maior (nova gramática de front-end ou definição no próprio spec da
> linguagem), o requisito **delimita explicitamente** o que fecha agora e o que
> fica documentado para um ciclo futuro — não promete o que não cabe no escopo
> de back-end/sema.

## 1. Introdução

### 1.1. Objetivo

Fechar três dívidas do back-end (`codegen`), cada uma corrigida na causa-raiz
até onde o escopo de codegen/runtime/sema alcança:

- **REQ-52 (ISSUE-7, `codegen/`):** um módulo que combina `UseCase` **E**
  `Policy` não gera — os dois emitem `func Wire(...)` no mesmo pacote Go e
  colidem. É a dívida mais concreta e de maior valor: **bloqueia a geração do
  exemplo `pizzeria`** (hoje na lista `KNOWN_UNGENERATABLE` do CI). Fechar
  desbloqueia o exemplo inteiro.
- **REQ-53 (ISSUE-6, `codegen/gentest*`, `codegen/rtsrc/`):** vários cenários de
  teste de `*.test.ds` (spec §22) têm semântica reduzida hoje. Fecha a **fatia
  tratável** (asserção de estado, `emitted`/`released` em Saga, `mock returns`,
  shrinking de property, `rolledback` com reversão real) e **delimita** o item
  que exige nova gramática (cenário de acesso NEGADO).
- **REQ-54 (ISSUE-8, `sema/`):** divergências menores marcadas "em evolução"
  pelo próprio spec. Fecha a **fatia tratável em `sema`** (refino do relatório de
  cobertura, §22.7) e **reclassifica** os itens que dependem de definição no
  spec da linguagem (gatilho de redação GDPR §4.4; agregações §25 avg/min/max/
  group by, aritmética estendida, marshalling FFI) — que saem da dívida de
  codegen e passam a "aguardando definição de spec".

Os três são **ortogonais** (arquivos/pacotes distintos, sem dependência de
ordem). A ordem de execução (L1 → L2 → L3) põe a de maior valor e mais concreta
(ISSUE-7) primeiro.

### 1.2. Baseline (o que já existe — não é trabalho deste ciclo)

- O back-end completo do Marco E–J: geração de módulos
  (`codegen/codegen.go:generateModuleFiles`), o wiring de UseCase
  (`emitUOWWireFunc`, `decl_usecase.go` → `func Wire(u UnitOfWork)`) e de Policy
  (`emitPolicyWireFunc`, `decl_policy.go` → `func Wire(d Dispatcher)`), e os
  pontos de entrada com **nome próprio** que já evitam colidir com `Wire`:
  `StartWorkers` (`decl_worker.go`), `WireQueryCache` (`decl_query_cache.go`),
  `WireOutboxStore`/`StartOutboxRelay`/`StartOutboxCleanup` (Marco J). Ou seja,
  **o padrão de resolução da colisão já existe no código** — ISSUE-7 é o único
  ponto que ainda erra de propósito, com guarda em `generateModuleFiles`.
- A geração de testes `*.test.ds` → Go (`codegen/gentest*.go`, Marco H4),
  cobrindo §22.1–22.6 no caminho feliz, com as reduções de semântica de ISSUE-6
  documentadas no cabeçalho de `gentest.go` (caller sempre autenticado, `then`
  de estado/`emitted` em Saga como erro de geração, `rolledback` como só
  `err != nil`, property sem shrinking).
- A `memoryUnitOfWork`/`MemoryEventStore` (`codegen/rtsrc/uow.go.txt`/
  `eventstore.go.txt`): commit/rollback são no-op porque a store não tem
  staging — a razão de `rolledback` (§22.2) não provar reversão real hoje.
- O warning de cobertura `checkHandleErrorCoverage`
  (`sema/rules_warnings.go`, REQ-5.22, §22.7), na granularidade **por Handle**.
- O exemplo `pizzeria` (`docs/examples/pizzeria`): valida limpo (`dsc check`),
  mas `dsc gen` falha no módulo `Kitchen` (UseCase `Claim`/`Finish` via HTTP +
  Policy reativa sobre `OrderPaid`) — a fixture-âncora natural de REQ-52.

### 1.3. Escopo

Dentro:

- **REQ-52:** unificar o wiring de um módulo com UseCase+Policy, e provar com o
  `pizzeria` gerando + compilando. Remover `pizzeria` da lista
  `KNOWN_UNGENERATABLE` do CI (`.github/workflows/ci.yml`).
- **REQ-53:** a fatia de ISSUE-6 tratável em codegen/runtime, item a item.
- **REQ-54:** o refino §22.7 em `sema`, e a reclassificação documentada de §4.4/
  §25.

Fora (documentado, para ciclos futuros — não reabrir estas issues por eles):

- **Cenário de acesso NEGADO em teste** (ISSUE-6): a gramática de §22 não tem
  "como o caller X" — fechar exige um ciclo de front-end (léxico→parser→
  resolver→sema), fora do escopo de codegen. Mesma natureza dos itens de
  ISSUE-2.
- **Gatilho de redação GDPR** (§4.4) e **agregações/aritmética/FFI de §25**
  (ISSUE-8 a/c): o próprio spec da linguagem os marca "em evolução / a definir".
  Sem definição no spec, não há o que implementar de forma estável — a ação é
  reclassificá-los como "aguardando spec", não codegen.

## 2. Requisitos Funcionais

### REQ-52 — Wire unificado: UseCase + Policy no mesmo módulo (ISSUE-7)

**User story:** Como autor de DomainScript, quero declarar `UseCase` e `Policy`
no mesmo módulo (ex. o `Kitchen` do `pizzeria`: `Claim`/`Finish` via HTTP e
criação reativa via Policy sobre `OrderPaid`) e ter o back-end gerado, em vez de
`dsc gen` falhar com "wiring combinado ainda não suportado".

1. THE SYSTEM SHALL gerar o back-end de um módulo que declara **UseCase e Policy
   ao mesmo tempo**, sem o erro de `generateModuleFiles`
   (`codegen/codegen.go:503`) — o guarda `if hasUseCases && hasPolicies` é
   removido e substituído por wiring que não colide.
2. THE SYSTEM SHALL emitir, para esse módulo, um wiring **sem colisão de
   símbolo** no pacote Go: um único `Wire` por módulo que injeta tanto a
   `UnitOfWork` dos UseCases quanto o `Dispatcher` que as Policies assinam
   (assinatura combinada `Wire(u UnitOfWork, d Dispatcher)` — ver §design 2.2), OU
   um `Wire` de UseCase mais um ponto de entrada de Policy com **nome próprio**
   (padrão de `WireQueryCache`/`WireOutboxStore` já no código). A decisão fica no
   design; qualquer das duas satisfaz "não colidir".
3. THE SYSTEM SHALL manter **byte-idêntico** o wiring de um módulo que tem
   **só** UseCase (wallet/shop) ou **só** Policy (shop/shipping) — a mudança
   afeta exclusivamente o caso misto (NFR-28).
4. THE SYSTEM SHALL ajustar `cmd/<service>/main.go` (`generateCmdMainFile`,
   `codegen.go`) para chamar o wiring combinado do módulo misto com os
   argumentos corretos (a `UnitOfWork` e o `Dispatcher` que ele já constrói
   quando `needsDispatcher`) — sem quebrar os call sites dos módulos puros.
5. THE SYSTEM SHALL provar o fechamento com o exemplo **`pizzeria`**: `dsc gen
   docs/examples/pizzeria` sai com código 0 e o projeto Go gerado **compila**
   (`go build`/`go vet`), além de um teste de codegen pareado (fixture sintética
   com UseCase+Policy no mesmo módulo → gera e compila; um módulo só-UseCase e um
   só-Policy → byte-idênticos).
6. THE SYSTEM SHALL remover `pizzeria` da lista `KNOWN_UNGENERATABLE`
   (`.github/workflows/ci.yml`, job `examples`) — a partir daí o CI passa a
   **gerar e compilar** o `pizzeria` como os demais exemplos.
7. WHERE a geração do `pizzeria` revelar um bloqueio **adicional** e independente
   (fora da colisão de `Wire`), THE SYSTEM SHALL registrá-lo como uma nova issue
   em `.claude/issues.md` em vez de ampliar silenciosamente o escopo de REQ-52.

### REQ-53 — Semântica plena dos testes gerados (fatia tratável de ISSUE-6)

**User story:** Como autor de `*.test.ds`, quero que os cenários do spec §22
gerem asserções com semântica real, não reduzida, para os casos que não dependem
de nova gramática.

1. THE SYSTEM SHALL gerar a asserção de estado `then state { campo = valor }`
   (§22.1, Aggregate): em vez do erro de geração de hoje, reconstruir o estado do
   Aggregate (replay dos eventos do `given`/`when`) e comparar cada campo
   declarado — par de testes (um `then state` que passa; um que falha com
   diferença clara).
2. THE SYSTEM SHALL gerar `emitted Evento(...)` / `released` a partir de um passo
   de **Saga** (§22.3), hoje um erro de geração — coletando os eventos
   publicados no passo e afirmando sua presença (mesmo espírito da coleta que
   §22.4/Policy já faz).
3. THE SYSTEM SHALL fazer `mock Target returns X` (§22.3) **influenciar o
   fluxo**: o valor `X` construído passa a ser o retorno efetivo do alvo mockado
   (hoje o mock sempre sucede e `X` é ignorado) — par de testes onde `X` desvia o
   resultado observável do passo seguinte.
4. THE SYSTEM SHALL encolher (**shrinking**) o contra-exemplo de uma `property`
   (§22.5): ao falhar, reportar a **menor** sequência que ainda reproduz a falha,
   não a sequência completa — par de testes (property que falha → contra-exemplo
   mínimo estável e determinístico).
5. THE SYSTEM SHALL fazer `rolledback` (§22.2) provar **reversão real**: a
   `memoryUnitOfWork`/`MemoryEventStore` (`codegen/rtsrc/`) ganha **staging** —
   escritas de um `Run` que retorna erro **não** ficam visíveis na store (hoje
   `Append` é durável no ato e commit/rollback são no-op) — par de testes (um
   `Run` com erro não deixa evento na store; um `Run` OK deixa). Esta é a única
   sub-parte que toca o runtime vendorizado; NFR-27 (núcleo sem deps externas)
   vale — é só stdlib.
6. THE SYSTEM SHALL manter **byte-idêntica** a saída de qualquer `*.test.ds` que
   já gerava (wallet/shop) — cada nova semântica só muda a saída de um cenário
   que HOJE é erro de geração (2/1) ou de granularidade reduzida (3/4/5) e que
   nenhum exemplo real exercita (NFR-28). Cada sub-parte entrega o par
   positivo/negativo (NFR-4).
7. THE SYSTEM SHALL **delimitar** o cenário de acesso NEGADO (§22): fica fora
   deste ciclo por exigir nova gramática ("como o caller X") — documentado no
   próprio spec e mantido em ISSUE-6 como resíduo apontado para um ciclo de
   front-end (não fechar ISSUE-6 enquanto este item existir; ver §5).

### REQ-54 — Divergências menores: refino tratável + reclassificação (ISSUE-8)

**User story:** Como mantenedor, quero que as divergências menores de ISSUE-8
fiquem ou fechadas (quando cabem em `sema`) ou reclassificadas com precisão
(quando dependem do spec da linguagem), para a issue deixar de ser um saco
indefinido.

1. THE SYSTEM SHALL refinar o relatório de cobertura semântica (§22.7,
   `sema/rules_warnings.go:checkHandleErrorCoverage`, REQ-5.22) para a
   granularidade **por ramo de regra de negócio** (não só "o Handle tem algum
   cenário de erro"), OU — se a análise de raiz mostrar que isso exige
   informação que o checker não tem sem re-arquitetura — registrar o motivo
   preciso e manter o warning por-Handle, reclassificando o refino como item de
   um ciclo de sema dedicado. A decisão sai da análise de raiz (§design 4).
2. THE SYSTEM SHALL **reclassificar** o gatilho de redação GDPR (§4.4) e os
   itens de §25 (avg/min/max/group by, aritmética estendida, marshalling FFI
   detalhado) como **"aguardando definição no spec da linguagem"** — não são
   dívida de codegen: o placeholder tipado de redação (E4.3) já existe, e as
   agregações/aritmética exigem sintaxe nova que o spec ainda não definiu. A ação
   é documentá-los como bloqueados em spec (em `gaps.md`/ISSUE-8), removendo-os
   da expectativa de "codegen a fazer".
3. THE SYSTEM SHALL, se REQ-54.1 fechar o refino de §22.7, entregar o par de
   testes (NFR-4) para a nova granularidade (um Handle com um ramo de erro não
   testado → warning; todos os ramos testados → silêncio).

## 3. Requisitos Não-Funcionais

### NFR-28 — Sem regressão; determinismo; byte-identidade dos exemplos reais

- `wallet` e `shop` permanecem **byte-idênticos** (determinismo NFR-13/21).
- `pizzeria` **passa a gerar** (REQ-52) — é a mudança de comportamento esperada;
  sua saída gerada é nova (não havia golden antes, porque não gerava), então não
  há "regressão de golden", só cobertura nova.
- REQ-53/54 só alteram a saída de formas de cenário que hoje são erro de geração
  ou warning de granularidade reduzida — nenhum `*.test.ds` real existente muda.

### NFR-29 — Par de testes positivo/negativo por correção (reafirma NFR-4)

Cada sub-parte fechada entrega o par exigido pelo CLAUDE.md. Escopo de teste por
task (não a suíte inteira) — CI roda o resto.

### NFR-30 — Núcleo do runtime sem deps externas (reafirma NFR-12)

O staging de `rolledback` (REQ-53.5) é `rtsrc/` puro (stdlib) — não introduz
nenhuma dependência nova nem import de `database/sql` no núcleo.

## 4. Rastreabilidade

| Issue | REQ | Pacote/arquivo-raiz | Natureza |
|---|---|---|---|
| ISSUE-7 | REQ-52 | `codegen/codegen.go`, `decl_usecase.go`, `decl_policy.go` | fix concreto (desbloqueia pizzeria) |
| ISSUE-6 | REQ-53 | `codegen/gentest*.go`, `codegen/rtsrc/{uow,eventstore}.go.txt` | fatia tratável + 1 item delimitado |
| ISSUE-8 | REQ-54 | `sema/rules_warnings.go` (+ docs) | refino tratável + reclassificação |

## 5. Critérios de Pronto (Definition of Done)

O ciclo está completo quando:

1. Um módulo com UseCase+Policy gera e o Go compila; `dsc gen
   docs/examples/pizzeria` sai 0 e o projeto builda; `pizzeria` sai da lista
   `KNOWN_UNGENERATABLE` do CI; wallet/shop byte-idênticos (REQ-52) — testes
   pareados verdes.
2. As sub-partes tratáveis de ISSUE-6 (state assertion, saga emitted/released,
   mock returns, shrinking, rolledback com staging) fechadas com par de testes;
   o item de acesso NEGADO delimitado e documentado (REQ-53).
3. O refino de §22.7 fechado em `sema` **ou** reclassificado com motivo preciso;
   §4.4/§25 reclassificados como "aguardando spec" (REQ-54).
4. `go build ./...` / `go vet ./...` / `gofmt -l .` limpos; testes de escopo de
   cada task verdes; wallet/shop sem regressão; `pizzeria` gera e compila no CI.
5. `.claude/issues.md`: **ISSUE-7** → `RESOLVED (commit <hash>)`; **ISSUE-6** →
   `RESOLVED` para a fatia fechada, com o resíduo "acesso NEGADO" apontado para um
   ciclo de front-end (não marcada totalmente resolvida enquanto esse item
   existir); **ISSUE-8** → fechada para (b) e **reclassificada** para (a)/(c)
   ("aguardando spec"). `.claude/state.md` reflete o Marco L; `gaps.md` §G-7/§G-6/
   §G-baixo atualizados.
