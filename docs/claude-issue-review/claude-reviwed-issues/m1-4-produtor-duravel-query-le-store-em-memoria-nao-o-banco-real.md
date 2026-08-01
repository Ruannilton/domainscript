CODIGO: m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real
CATEGORIA: Correção de código
Issue original: [[docs/sdd/issues/m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real]]

## Resumo da issue

Quando um módulo grava eventos num banco real (produtor durável — `Database` com provider reconhecido + canal `queue`), a escrita vai corretamente para o banco, mas toda `Query` do serviço — inclusive as do próprio módulo produtor — continua lendo de uma `store` em memória global que nunca recebe esses eventos. O Go gerado compila e roda sem erro, mas devolve dado errado: na pior forma, um `load ... as View` devolve um registro **fabricado** com HTTP 200 (todos os campos zerados) em vez de 404; na forma mais comum, um `list` devolve lista vazia mesmo havendo dados reais no banco.

## Evidencias

- `codegen/codegen.go:1384` — `store := runtime.NewMemoryEventStore()`, incondicional, é a única store de leitura por *service*; `codegen/codegen.go:1583`/`:1591` a passam para `newGRPCServer`/`newMux`.
- `codegen/sql_wiring.go:388` (`emitSingleDatabaseWiring`) troca só a `UnitOfWork` (escrita) do módulo produtor para SQL real; a doc-comment em `:381-387` já documenta o mismatch como intencional.
- `codegen/testdata/aggregate_wallet_load.go.golden:14-33` — `Load<Agg>` gerado não trata stream vazio: devolve o agregado zero-value sem erro, o que produz a "leitura fabricada" quando combinado com `memoryEventStore.Load` de um id desconhecido devolvendo `([]Event{}, nil)` (`codegen/rtsrc/eventstore.go.txt:102-105`).
- Reproduz em `[[testdata/projects/pizzeria]]`: `Sales` é produtor durável (`Database MainDb { provider: "postgres" }` + canal `Sales -> Kitchen` `provider: "rabbitmq"`) e dono de `GetAvailableMenu`/`GetActiveOrders` sobre `MenuItem`/`Order` — Aggregates que ele mesmo grava só no Postgres.
- `ensureSchema` (DDL de `events`/`outbox`) só é chamada de `NewEventStore` (`codegen/sqlrt/eventstore.go.txt:49`), nunca do caminho de wiring do produtor durável — contra um Postgres virgem o primeiro `UseCase` falharia com `relation "events" does not exist`.

## Impacto no projeto

É miscompilação silenciosa de leitura (a classe que o NFR-33 do ciclo proíbe): o programa gerado compila, os testes gerados passam (porque reatribuem a store), e só em produção, contra um banco real, a Query devolve dado inconsistente ou fabricado. Hoje o defeito é **latente** — nenhum projeto que efetivamente gera hoje o exercita (`pizzeria` ainda não gera, por outro motivo: `list <Aggregate>` em posição de expressão, gap de M1.2/M1.3) — mas vira observável assim que esse outro gap fechar, e bloqueia M1.6 (REQ-55.9, `pizzeria` compilando de ponta a ponta) poder provar o que promete.

## Soluçoes possíveis

### Solucão 1

Simetria com o que a escrita já faz: um `readStoreByModule` ao lado do `uowVarByModule` existente. `emitSingleDatabaseWiring` passa a emitir também um `<mod>ReadStore := sqlruntime.NewEventStore(...)` (mesma conexão, mesma tabela `events` que a UoW já grava); `newMux`/`newGRPCServer` ganham um parâmetro extra só quando o grupo tem produtor durável com Query roteada; a rota HTTP/gRPC escolhe a store pelo módulo da Query. Como efeito colateral desejado, `ensureSchema` passa a rodar nesse caminho, fechando de graça o problema do schema nunca criado. `list <Aggregate>` sobre um módulo com store SQL cai no erro claro de REQ-55.5 (`*sqlruntime.EventStore` não implementa `StreamLister`) em vez de devolver lista vazia — troca miscompilação silenciosa por gap declarado, que é o padrão que NFR-33 pede.

### Solução 2

Dar ao `sqlrt` um `StreamLister` completo (coluna `aggregate_type` no DDL, `ListStreams` com `ORDER BY`) para que `list`/`GetAvailableMenu`/`GetActiveOrders` devolvam dado de verdade, não só um erro claro. Não é alternativa à Solução 1 — é a fatia seguinte (Mx.3 no fatiamento da issue), que depende de M1.1a de `[[docs/sdd/issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype|m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype]]` (o `Tx.Append` tipado que leva `aggregateType` até o `INSERT`). As alternativas descartadas pela própria análise — trocar a `store` do service inteiro pela SQL do produtor (quebra módulos in-memory do mesmo service), `newMux(map[string]EventStore)` (reescreve os 6 goldens sem ganho), dual-write memória+SQL (duas fontes de verdade sem transação comum) — não sobrevivem por razões estruturais, então a Solução 1 é o único caminho razoável para a parte que não precisa de decisão de escopo.

## O que precisa ser resolvido antes

Nenhuma — a spec da linguagem já é clara: nada em `[[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|13-module-infra.md]]` (§13) admite ler `X` de outro lugar que não o `Database` que o declara `manages`. O próprio texto original da issue confirma: "Não é decisão de design em aberto nem ambiguidade da spec da linguagem — é gap de implementação em codegen." A Solução 1 (Mx.1/Mx.2 no fatiamento da issue) pode ser implementada sem decisão pendente nenhuma.

Duas dependências, não bloqueios desta issue em si:

- `[[docs/sdd/issues/usecase-e-policy-no-mesmo-modulo-colisao-de-wire|usecase-e-policy-no-mesmo-modulo-colisao-de-wire]]` registra que M1.6 tem outro bloqueio independente: `codegen.go:1215-1219` ainda recusa um produtor durável que também precise de Dispatcher no mesmo módulo, caso que `Sales` do `pizzeria` também é.
- A fatia Mx.3/Mx.4 (Solução 2 acima, e o e2e completo do `pizzeria`) depende de uma decisão de escopo do ciclo ainda não tomada: se REQ-55.9 significa apenas "o projeto compila" (Mx.1/Mx.2 bastam) ou "as Queries do exemplo devolvem dado correto" (precisa também de `StreamLister` no `sqlrt`) — `[[docs/sdd/specs/correcoes-issues-6-8-12/requirements.md|requirements.md]]`/`[[docs/sdd/specs/correcoes-issues-6-8-12/design.md|design.md]]` do ciclo `correcoes-issues-6-8-12` ainda não decidem isso explicitamente.
