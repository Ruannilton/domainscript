CODIGO: m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype
CATEGORIA: Ajuste de especificação

Issue original: [[docs/sdd/issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype]]

## Resumo da issue

A rota decidida em M1.1 — carimbar `ctx` uma única vez com o `aggregateType`, antes de `uow.Run(ctx, ...)` — só seria segura se uma única `Tx.Run()` nunca gravasse eventos de mais de um tipo de Aggregate. A issue prova, por leitura de código, que essa premissa é falsa: o front-end permite e o codegen já gera `UseCase` que despacham `Handle` em dois Aggregates de tipos diferentes dentro do mesmo `Run`, seja porque não há `Database` declarado, seja porque os dois compartilham o mesmo `Database`. Carimbar o `ctx` uma vez gravaria o tipo errado num dos dois streams.

## Evidencias

- [[docs/sdd/steerings/domainscript-spec-v7/19-transactions-sagas|19-transactions-sagas.md]]:7 — a tabela normativa condiciona commit local só ao `Database`, nunca ao tipo de Aggregate.
- `sema/rules_crossfile.go:168-209` (`checkTransactions`, REQ-5.9) só soma `Database` distintos; linha 186 é literalmente `continue // aggregate sem banco declarado: fora do alcance da regra` — nenhuma regra do front-end conta tipos de Aggregate.
- `codegen/decl_usecase.go:345-346` e `:330` desembocam no mesmo `handleDispatchCall` (`codegen/lower/stmt.go:2035-2085`), que emite `%s.Append(string(%s.id), events)` para qualquer Aggregate do módulo — inclusive dois tipos diferentes no mesmo `tx`.
- `usecase2PCPlan` (`codegen/decl_usecase.go:399-401`) só exige `len(seen) >= 2` bancos distintos; três Aggregates com dois no mesmo `Database` colapsam no mesmo `Tx`, reproduzindo o mesmo problema no caminho 2PC.
- `grep` de `aggregateType`/`WithAggregateType` sobre `*.go`/`*.txt`/`*.golden`: zero ocorrências — nada foi implementado ainda.
- [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]] §4.2.3 (revisão 2026-07-31) normatiza que `aggregateId` é derivado do emissor, com emissor único por `Event` — confirma normativamente que o tipo é propriedade da chamada, não da transação.

## Impacto no projeto

Se implementada como originalmente decidida, a rota gravaria `tenantStream.aggregateType` errado sempre que um `UseCase` tocar dois Aggregates no mesmo `Run` — e `ListStreams(ctx, "B")` deixaria de enxergar o stream de `B` (carimbado como `"A"`), corrompendo silenciosamente a leitura de streams por tipo. É exatamente a classe de miscompilação silenciosa que o NFR-33 do ciclo proíbe. Bloqueia o fechamento de M1.1 e da issue irmã [[docs/sdd/issues/m1-1-aggregatetype-nao-chega-a-eventstore-append|m1-1-aggregatetype-nao-chega-a-eventstore-append]].

## Soluçoes possíveis

### Solucão 1

Passar o `aggregateType` por CHAMADA em vez de por `ctx` de todo o `Run`: `Tx.Append` ganha um terceiro parâmetro (`Append(aggregateType, aggregateID string, events []Event) error`), e o `ctx` do [[docs/sdd/specs/correcoes-issues-6-8-12/design|design.md]] §5.1 é realocado de `decl_usecase.go` (uma vez por `Run`) para `memoryTx.Append` (uma vez por chamada, dentro de `codegen/rtsrc/uow.go.txt`). É a rota que a "Solução proposta" da própria issue recomenda, com raio de alcance mapeado (2 implementações de `runtime.Tx`, 16 call sites, 5 goldens) e um fatiamento em 4 sub-tasks (M1.1a–d).

### Solução 2

Restringir `checkTransactions` (REQ-5.9) para também barrar o caso heterogêneo (um `UseCase` só poderia tocar um tipo de Aggregate por transação local). Descartada pela própria análise: mudaria a semântica da linguagem (o que hoje compila deixaria de compilar), está fora do escopo de um ciclo de correções de `codegen`, e a [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|§4.2.3]] revisada confirma que a linguagem espera múltiplos emissores por transação — é o `Event` que tem emissor único, não a transação inteira.

## O que precisa ser resolvido antes

A decisão técnica já está tomada pela própria análise da issue (terceiro parâmetro em `Tx.Append`, preferido a um método irmão `AppendTyped`, por NFR-33) — a [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|§4.2.3]] da spec da linguagem também já licencia a rota (fixa que o valor a emitir é o Aggregate emissor). O que falta é trabalho editorial no ciclo `correcoes-issues-6-8-12`, antes de codificar:

1. Emendar o NFR-31 do ciclo — hoje promete "`wallet` e `shop` permanecem byte-idênticos", incompatível com qualquer rota que altere a linha de `Append` gerada.
2. Deixar explícito o escopo do NFR-32 — ele fala só de `runtime.EventStore`, mas `runtime.Tx` também muda de assinatura e isso precisa constar no design.
3. Registrar por escrito a escolha "terceiro parâmetro" vs. `AppendTyped` irmão (a análise já recomenda o primeiro).
4. Registrar as duas amarrações futuras já identificadas: semeadura tipada em `*.test.ds` (não bloqueia M1.1) e o acoplamento com REQ-59/M4.2 (staging precisa carregar o `aggregateType` junto do `aggregateID` no flush).

Feito isso, a implementação segue o fatiamento M1.1a→d já proposto na issue.
