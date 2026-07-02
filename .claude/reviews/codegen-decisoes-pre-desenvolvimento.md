# Revisão pré-desenvolvimento do back-end (codegen)

> **Propósito.** Antes de escrever a primeira linha do gerador (task E0.1), há
> decisões que **moldam interfaces e layout** e são caras de retrofitar depois, e
> lacunas nos exemplos que afetam o "runnable" do Marco E. Este documento é um
> **registro de decisões**: cada item traz contexto, a evidência no código, as
> opções, uma **recomendação** e o custo de adiar.
>
> **Todas as decisões foram fechadas** e estão consolidadas em **§0**. Não há mais
> itens em aberto.
>
> Complementa — não substitui — `.claude/specs/codegen/{requirements,design,tasks}.md`.

## §0. Decisões já fechadas (não re-litigar)

| # | Decisão | Onde |
|---|---|---|
| 0.1 | `decimal` → `runtime.Decimal` **exato** (nunca `float64`) | design §3.3/§7; confirmado pelo dono |
| 0.2 | Exemplo wallet ganhou `Operator +,-,>=` em `Money` (+ Errors `CurrencyMismatch`/`NegativeResult`), na convenção real do front-end (`value`/campos nus, não `self`) | `docs/examples/wallet/domain.ds`; suíte verde |
| 0.3 | Inferência de tipos de locais (`load`/`list`/`match`/`lambda`) é estendida **dentro do `codegen`** via `TypeEnv`, não em `types` | design §3.6a; task E5.0 |
| 0.4 | Ops de arquivo (`store`/`signed_url`/`delete`/`load File`) saem do Marco E → Marco G (dependem de `FileStorage`) | requirements REQ-22.7; task G1a |
| 0.5 | Emissor de strings + `go/format`; runtime `.go.txt` + `//go:embed`; um pacote Go por módulo | design §1.3/§2/§6 |
| 0.6 | **Contexto ambiente:** `context.Context` threadado em **toda fronteira** do runtime desde E2.1 (`Store.Load(ctx,id)`, `uow.Run(ctx,fn)`, `repo.Count(ctx,…)`; `Handle` recebe `caller` explícito + `ctx` implícito). `caller`/`tenant`/idempotency-key/trace entram no `ctx` via **chaves tipadas do pacote `runtime`** (no-op até F/G/H) | era §1.1; runtime E2.1 |
| 0.7 | **Taxonomia de erros:** `runtime.BusinessError{Code, Msg}` com `Code` = nome do `Error`; **tudo que não for `BusinessError` é infra** (→ 503 + retry); a borda HTTP mapeia por `errors.As(&BusinessError)`; `ErrForbidden`/`ErrNotFound` são `BusinessError`s reservados (403/404) | era §1.2; design §3.5 |
| 0.8 | **`runtime.Decimal`:** decimal de **escala fixa** sobre `math/big.Int` (sem dependência externa), arredondamento **half-even** (bankers'), escala default **4**. `+`/`-`/`>=` triviais; `/` aplica a política de escala+arredondamento; `NewDecimalFromInt` para a coerção `integer`→`Decimal` na construção | era §1.3; design §3.3/§7 |
| 0.9 | **Event sourcing:** chave de stream = **id do aggregate emissor**, atribuída como **metadata no `append`** (junto de `sequence`/`timestamp`/`eventType`); o campo `id` do payload é dado de negócio, não a chave. `Apply` lê o payload; a reconstrução itera o stream por `aggregateId` | era §1.4; E2.1/E6.2 |
| 0.10 | **`PublicEvent`:** gerado num pacote compartilhado **`contracts/`** (espelha `contracts/*.ds`; quebra ciclos de import por construção); `Event` privado fica no pacote do módulo. O emissor suporta **refs qualificadas por pacote + imports cross-módulo desde E1.1** | era §1.5; layout E1.1/E9.1 |
| 0.11 | **`caller` na borda:** `runtime.Caller` com `Authenticated()`, `ID() string`, `HasRole(string)`; a borda do Marco E popula um **caller de dev** a partir de um header simples (placeholder até auth real). `caller.id == self.id` → `caller.ID() == string(self.id)` (id de VO wrapper de string vira string) | era §1.6; E6.1/E9.2 |
| 0.12 | **Smoke do Marco E:** `go build ./...` + `go vet ./...` + **teste comportamental gerado in-memory** (cria estado via eventos `given`, executa um UseCase, confere o evento emitido); sem subir socket. Um "sobe o servidor" fica como teste opcional. Teste de um evento isolado semeia via `given`; teste do fluxo completo usa a construção completa (0.17) | era §1.7; E0.2/E10.2 |
| 0.13 | **Read-model Marco E (in-memory):** `list <VO aninhado>` varre os aggregates conhecidos e concatena o campo `AppendList` daquele tipo ("read não-otimizado do Marco E"; substituído pelo backend `database/sql` em G) | era §2.2; E8.1 |
| 0.14 | **Desambiguação de nomes:** sufixo determinístico para colisão intra-pacote (ordem estável de declaração + sufixo numérico); `names.go` integra com `emit.Import` para produzir refs qualificadas por pacote | era §2.3; design §3.3 / E1.2 |
| 0.15 | **Métodos embutidos (Marco E):** `string.length()`→`len`, `AppendList.add`→`.Add`; `distinct`/`sum`/`focus` (§20, shop) entram no Marco F. Par `(tipo, método)` ausente = **erro de geração explícito** (nunca Go arbitrário) | era §2.4; E1.3/E5.3 |
| 0.16 | **Defaults:** `go.mod` GoVersion `1.22` (mín. p/ `ServeMux METHOD /path/{param}`, REQ-28); `env("X")`→`os.Getenv("X")` com porta default `8080`; `ModulePath` derivado do dir de saída (sobrescrevível por `opts.ModulePath`); tracing automático **fora do Marco E** (só `slog` explícito; trace no `ctx` desde E2.1); `cmd/dsc`: subcomando `gen` novo, `check` = comportamento atual, `dsc <path>` sem subcomando continua validando (retrocompat) | era §4 |
| 0.17 | **Fixtures de exemplo não são fonte de verdade.** O wallet foi escrito para exercitar a tokenização, não como domínio canônico — não precisa estar correto/completo nem ser "corrigido" para o codegen. Consequências: (a) **não** estender o wallet como verdade; (b) o smoke/teste do Marco E não depende dele — testar um evento isolado semeia o estado via `given`, testar tudo usa a **construção completa** (caminho de criação); (c) fica sem efeito a questão do `ListEntries` ignorar `id` — o read-model é definido por 0.13, o exemplo apenas o exercita | era §3.1/§3.2; E10.2 |
| 0.18 | **Semântica de `value`/`ok` no VO:** `value` = valor embrulhado (wrapper) / o receptor inteiro (composto); **`ok` = sentinela "validação passa"** → `NewX` não retorna erro | era §2.1; design §3.3 |

---

## Próximo passo

Não há decisões em aberto — §0 está completo e **já folheado** para dentro dos specs
de codegen (`design.md` §3.1a/3.3/3.4/3.5/3.7/3.9/3.12/§6/§7 e `tasks.md`
E1.2/E1.3/E2.1/E4.2/E6.2/E9.1/E9.2/E10.2). Este arquivo fica como o registro histórico
das decisões; a fonte de verdade operacional passa a ser os specs. **E0.1 pode começar.**
