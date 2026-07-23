# Design — Correções de dívida técnica (ISSUE-6, ISSUE-7, ISSUE-8)

Marco L. Mesmo formato do Marco K: **análise de raiz** validada por leitura de
código, **fix na raiz**, **arquivos tocados** e **par de testes** (NFR-4). Onde
um item exige um ciclo de front-end ou definição de spec, o design **para na
fronteira** e documenta, em vez de inventar.

---

## 1. Visão geral e ordem

Três correções ortogonais (codegen / gentest+runtime / sema). Ordem **L1
(REQ-52) → L2 (REQ-53) → L3 (REQ-54)**: a primeira é a mais concreta e de maior
valor (desbloqueia o `pizzeria` inteiro e um item do CI); as outras são fatias
independentes. Nenhuma depende do resultado da outra.

---

## 2. REQ-52 — Wire unificado: UseCase + Policy no mesmo módulo (ISSUE-7)

### 2.1. Análise de raiz

`generateModuleFiles` (`codegen/codegen.go:502`) recusa de propósito:

```go
if hasUseCases && hasPolicies {
    return ..., fmt.Errorf("módulo %s: UseCase e Policy no mesmo módulo ainda não têm wiring combinado suportado ...", moduleName)
}
```

Motivo real (confirmado por leitura): os dois emissores produzem uma função com
o **mesmo nome** `Wire` no mesmo pacote Go:

- `emitUOWWireFunc` (`decl_usecase.go:85`): `func Wire(u runtime.UnitOfWork) {
  uow = u }` — injeta a var de pacote `uow`.
- `emitPolicyWireFunc` (`decl_policy.go:632`): `func Wire(d runtime.Dispatcher) {
  policyDispatcher = d; ...; o.Subscribe(...) }` — injeta `policyDispatcher` e
  assina as Policies.

Duas `func Wire` no mesmo pacote ⇒ "redeclared in this block". O `pizzeria`/
`Kitchen` é o primeiro módulo real com os dois.

**O próprio código já resolveu esse problema em outros lugares**, sempre com um
**nome próprio** em vez de um segundo `Wire`: `StartWorkers` (`decl_worker.go`),
`WireQueryCache` (`decl_query_cache.go`), `WireOutboxStore`/`StartOutboxRelay`/
`StartOutboxCleanup` (Marco J). O comentário de `decl_worker.go:84` diz isso
explicitamente. Ou seja: o padrão de resolução já existe; ISSUE-7 é só o ponto
que ainda não o aplicou.

### 2.2. Fix na raiz: um `Wire` combinado no módulo misto

Duas alternativas resolvem a colisão; o design **recomenda (A)** por dar um único
ponto de entrada coerente, mas ambas são aceitáveis (REQ-52.2):

- **(A) `Wire` unificado por módulo.** A assinatura passa a incluir os params que
  o módulo precisa: `UnitOfWork` sse tem UseCase, `Dispatcher` sse tem Policy.
  - só UseCase → `func Wire(u UnitOfWork)` (byte-idêntico a hoje).
  - só Policy → `func Wire(d Dispatcher)` (byte-idêntico a hoje).
  - misto → `func Wire(u UnitOfWork, d Dispatcher)` — corpo faz `uow = u` **e**
    `policyDispatcher = d` + os `o.Subscribe(...)` das Policies.
  Preserva byte-identidade dos casos puros por construção (o param extra e o
  corpo extra só aparecem no caso misto). Exige que UM emissor passe a coordenar
  os dois (ex. `generateModuleFiles` chama um `emitCombinedWireFunc` quando misto,
  senão os emissores atuais), para não haver dois `func Wire`.
- **(B) Nome próprio para a Policy no caso misto.** UseCase mantém `Wire(u)`;
  quando o módulo TAMBÉM tem UseCase, a Policy emite `WirePolicies(d)` (padrão
  `WireQueryCache`). `main.go` chama os dois. Menos unificado, mas mínimo.

Ponto comum às duas: `generateCmdMainFile` (`codegen.go`, call site
`%s.Wire(%s)` em ~1288) precisa, para o módulo misto, montar a lista de
argumentos com **a `UnitOfWork` e o `Dispatcher`** — que o main.go já constrói
quando `needsDispatcher` é true (o `uow := NewUnitOfWork(store, dispatcher)` já
existe nesse caminho). Então o `dispatcher` já está em escopo no main.go do
módulo misto; só falta passá-lo ao `Wire`.

**Interação com `needsDispatcher`:** um módulo com Policy local já força
`needsDispatcher=true` (codegen.go:1068), e um com UseCase que emite eventos
também usa o dispatcher como publisher da UoW. No caso misto, o MESMO
`dispatcher` serve aos dois lados (UoW publica → Policies assinam) — exatamente o
fluxo in-process que o Marco F já usa; nada de novo no runtime.

**Fora do orçamento (guarda pré-existente que continua valendo):**
`generateCmdMainFile` recusa combinar, no MESMO service, um módulo produtor de
canal de saída "queue" com um módulo que precisa de Dispatcher (F5/G3,
codegen.go:1121). O `Kitchen` do pizzeria é UseCase+Policy **local** (sem canal
de saída próprio), então não esbarra nessa guarda — confirmar na task, mas o
`pizzeria` foi desenhado para caber.

### 2.3. Arquivos e testes

- `codegen/codegen.go` — remover o guarda `hasUseCases && hasPolicies`; rotear
  para o wiring combinado (A) no caso misto; ajustar o call site em
  `generateCmdMainFile`.
- `codegen/decl_usecase.go` / `codegen/decl_policy.go` — extrair/parametrizar a
  emissão do `Wire` para permitir a forma combinada (ou o nome próprio, se (B)).
- `.github/workflows/ci.yml` — remover `pizzeria` de `KNOWN_UNGENERATABLE`.
- Testes (par NFR-4):
  - **positivo/desbloqueio:** fixture sintética com UseCase+Policy no mesmo
    módulo → gera; `gentest.SmokeCompile` builda; e/ou o `pizzeria` real via
    `GenerateProject` + smoke.
  - **byte-identidade:** um módulo só-UseCase e um só-Policy geram exatamente o
    Go de antes (guarda contra regressão de wallet/shop — NFR-28).

### 2.4. Alternativa rejeitada

Exportar as vars `uow`/`policyDispatcher` e deixar o main.go escrevê-las direto —
rejeitada pela mesma razão documentada em `decl_usecase.go:78` (não se pode
atribuir a var não-exportada de outro pacote, e exportá-las polui a superfície do
pacote). O `Wire`-como-setter é o padrão do código; mantê-lo.

---

## 3. REQ-53 — Semântica plena dos testes gerados (fatia de ISSUE-6)

### 3.1. Análise de raiz

O cabeçalho de `gentest.go` documenta cada redução. Categorizando por
tratabilidade (validado por leitura):

| Item (spec) | Hoje | Fecha em | Custo |
|---|---|---|---|
| `then state {}` §22.1 | erro de geração | codegen (replay + compara campos) | baixo |
| `emitted`/`released` em Saga §22.3 | erro de geração | codegen (coleta eventos do passo) | baixo |
| `mock returns X` §22.3 | mock sucede, `X` ignorado | codegen (X vira o retorno do mock) | médio |
| shrinking de property §22.5 | reporta sequência cheia | codegen (`gentest_property.go`) | médio |
| `rolledback` real §22.2 | só `err != nil` | **runtime** (staging na memory store) | médio |
| **acesso NEGADO** §22 | não expressável | **front-end (nova gramática)** | alto — DELIMITADO |

Os cinco primeiros ficam em codegen/runtime. O sexto (acesso NEGADO) exige uma
forma de gramática "como o caller X" que o léxico/parser não têm — mesmo tipo de
item de ISSUE-2, **fora deste ciclo** (REQ-53.7).

### 3.2. Fix por item

- **`then state {}` (§22.1):** onde hoje `gentest.go` erra, reconstruir o estado
  do Aggregate aplicando os eventos (o mesmo `Apply` que o codegen já gera) e
  emitir uma asserção por campo declarado (`if got.Campo != want { t.Errorf }`).
- **`emitted`/`released` em Saga (§22.3):** reusar a coleta de eventos que
  §22.4/Policy já faz (`gentest.go` "--- Policy/Query", reatribui
  `policyDispatcher` a um dispatcher de teste que acumula) — aplicar a mesma
  técnica ao passo de Saga que publica.
- **`mock returns X` (§22.3):** hoje o mock é um stub que sempre sucede. Fazer o
  valor `X` (já construído no gerado) ser o retorno do alvo mockado, para
  influenciar o passo seguinte. A auditoria de `emitSagaMock` decide se o mock é
  uma função-campo substituível (então `X` vira o corpo do stub) — a task começa
  por essa auditoria.
- **shrinking (§22.5):** `gentest_property.go` gera a sequência aleatória e, ao
  falhar, reporta a cheia. Implementar shrinking clássico (encolher por
  bissecção/remoção de passos, re-testando até o mínimo que ainda falha),
  **determinístico** (semente fixa) para não quebrar NFR-13.
- **`rolledback` real (§22.2):** a raiz é `memoryUnitOfWork`/`MemoryEventStore`
  sem staging (`rtsrc/uow.go.txt:73` documenta: "commit e rollback são no-op...
  eventos já são duráveis no ato de Append"). Dar **staging** à memory store:
  `memoryTx.Append` acumula num buffer e só aplica na store no commit (fn retorna
  nil); no erro, descarta. Assim `rolledback` prova que a store ficou intacta.
  Cuidado (NFR-30): `rtsrc/` puro, stdlib; e preservar a semântica
  "read-your-writes" dentro do MESMO `Run` (um `Load` depois de um `Append` no
  mesmo tx enxerga o buffer) que o adapter SQL já tem.

### 3.3. Arquivos e testes

- `codegen/gentest.go`, `codegen/gentest_property.go` (+ helpers de Saga/mock).
- `codegen/rtsrc/uow.go.txt` (+ `eventstore.go.txt` se o staging precisar de
  suporte da store) — só para `rolledback`.
- Cada item entrega o par NFR-4 (um cenário que passa, um que falha com mensagem
  clara), via `gentest.WriteFiles`/`RunTests` sobre o projeto gerado — o padrão
  dos testes de gentest existentes.
- **Guarda de não-regressão:** wallet/shop `*.test.ds` geram byte-idêntico
  (NFR-28) — nenhuma das mudanças toca uma forma que eles exercitam.

---

## 4. REQ-54 — Divergências menores: refino + reclassificação (ISSUE-8)

### 4.1. Análise de raiz e decisão por item

- **(b) Cobertura §22.7 (`checkHandleErrorCoverage`, `rules_warnings.go:208`,
  REQ-5.22):** hoje o warning dispara na granularidade **por Handle** ("o Handle
  não tem nenhum cenário de erro testado"). O refino pedido é "por ramo de regra
  de negócio". A task **começa pela análise de raiz**: o checker tem, por Handle,
  os `ensure ... else Error` (os ramos de regra)? Se sim, dá para cruzar cada ramo
  com os cenários de erro testados e apontar o ramo específico não coberto —
  fecha em `sema`. Se a informação de "qual cenário testa qual ramo" não existe
  sem cruzar `*.test.ds` × Handle (uma análise que o checker hoje não faz), o
  design **para** e reclassifica: mantém o warning por-Handle e registra o refino
  como um ciclo de sema dedicado, com o motivo preciso. A decisão sai da leitura,
  na primeira sub-task.
- **(a) Redação GDPR (§4.4)** e **(c) §25 (avg/min/max/group by, aritmética
  estendida, FFI):** o próprio spec da linguagem os marca "em evolução / a
  definir" (§25). O placeholder tipado de redação já existe (E4.3); o *gatilho*
  e as agregações exigem **sintaxe nova não definida no spec**. Sem definição, não
  há implementação estável — qualquer coisa que se fizesse seria adivinhação de
  semântica. **Decisão: reclassificar** (não implementar): movê-los de "dívida de
  codegen" para "aguardando definição no spec da linguagem", documentado em
  `gaps.md` e em ISSUE-8. Isso *fecha* a parte de ISSUE-8 que é deste lado (não há
  ação de codegen pendente para (a)/(c)).

### 4.2. Arquivos e testes

- `sema/rules_warnings.go` — só se (b) fechar em sema; então par NFR-4 (um Handle
  com um ramo de erro não testado → warning apontando o ramo; todos testados →
  silêncio).
- `.claude/specs/codegen/gaps.md` / `.claude/issues.md` — reclassificação de
  (a)/(c) (e de (b), se ficar para um ciclo dedicado).

---

## 5. Estratégia de testes (consolidada, NFR-4/NFR-29)

- REQ-52: fixture sintética misto + `pizzeria` real (gera+compila) + guarda de
  byte-identidade dos puros. É o item que mexe no CI (remove pizzeria de
  `KNOWN_UNGENERATABLE`).
- REQ-53: par por sub-item sobre o projeto gerado (`gentest.WriteFiles`/
  `RunTests`); `rolledback` também tem teste comportamental do staging da memory
  store.
- REQ-54: par em `sema` só se (b) fechar; caso contrário, a "prova" é a
  documentação da reclassificação.
- Fechamento: `.claude/issues.md`/`gaps.md`/`state.md` atualizados conforme o DoD
  (requirements §5) — ISSUE-7 resolvida; ISSUE-6 resolvida na fatia + resíduo
  apontado; ISSUE-8 fechada em (b) e reclassificada em (a)/(c).
