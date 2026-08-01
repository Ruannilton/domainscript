CODIGO: m1-1-aggregatetype-nao-chega-a-eventstore-append
CATEGORIA: Ajuste de especificação

Issue original: [[docs/sdd/issues/m1-1-aggregatetype-nao-chega-a-eventstore-append]]

## Resumo da issue

A M1.1 precisa fazer o `aggregateType` chegar até `EventStore.Append`, mas nenhuma
das duas rotas prescritas pela task (derivar de um registro já existente, ou usar
um seam que `Event`/`EventMeta` já ofereça) existe hoje no código. A única rota
viável é threadar o valor via `ctx`, igual ao que já se faz com `tenantID` — só
que carimbado uma vez por `Run` seria errado, porque um mesmo `UseCase` pode
gravar eventos de mais de um tipo de Aggregate no mesmo `Run` (issue irmã
[[docs/sdd/issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype]]).

## Evidencias

- `runtime.EventStore` continua com dois métodos, ambos por `aggregateID`
  (`codegen/rtsrc/eventstore.go.txt:27-38`); `tenantStream` só guarda
  `tenantID`/`events` (`:45-48`); `EventMeta` só tem
  `AggregateID`/`Sequence`/`Timestamp` e é o próprio `Append` quem o constrói
  (`:81-89`).
- `grep -rn "AggregateType|aggregateType"` sobre `*.go`/`*.txt`/`*.golden`
  devolve zero ocorrências; `WithAggregateType` também zero.
- `EventType()` emite o nome simples do evento
  (`codegen/decl_event.go:181`, `func (*%s) EventType() string { return %q }`),
  sem prefixo do Aggregate; `eventRegistry` mapeia nome→construtor, por módulo
  (`:201-211`), não Aggregate→tipo, e vive fora de `codegen/rtsrc`.
- A rota já decidida no `SOLVED` original (carimbar `ctx` uma vez antes de
  `uow.Run`) é insegura: `sema/rules_crossfile.go:168-209` restringe
  transações por `Database`/service, nunca por tipo de Aggregate.
- O ponto único onde o gerador emite `Append` já conhece estaticamente o tipo:
  `codegen/lower/stmt.go:2035-2085`, `shape.Name` na linha 2039-2046.

## Impacto no projeto

Sem o `aggregateType` carimbado no stream, `StreamLister`/`ListStreams`
(exigidos pelo [[docs/sdd/specs/correcoes-issues-6-8-12/design|design.md §4.1]]
do ciclo `correcoes-issues-6-8-12`) não têm como
filtrar streams por tipo de Aggregate — a funcionalidade que a M1.1 existe para
entregar simplesmente não pode ser implementada com o estado atual do runtime.
Enquanto isso não fecha, a M1.1 fica bloqueada e qualquer tentativa de
implementá-la sem essa peça arrisca ficar pela metade (o `Tx` recebe o tipo mas
não tem como repassá-lo ao `EventStore.Append`, cuja assinatura não pode mudar
por NFR-32).

## Soluçoes possíveis

### Solucão 1

Passar o `aggregateType` por chamada, não por `Run`: `Tx.Append` ganha um
terceiro parâmetro (`Append(aggregateType, aggregateID string, events []Event)
error`); `memoryTx.Append` deriva o `ctx` por chamada
(`WithAggregateType(tx.ctx, aggregateType)`) e `memoryEventStore.Append` lê o
valor do `ctx` para carimbar `tenantStream.aggregateType`. É a mesma direção já
escolhida pelo dono (`ctx`, mesmo mecanismo de `tenantID`), só realocada da
granularidade "uma vez por `Run`" (errada) para "uma vez por chamada de
`Append`" (correta) — o que resolve a issue irmã sem abandonar a decisão
original. Custo: 16 call sites em 7 arquivos, todos mecânicos, e a segunda
implementação de `runtime.Tx` (`codegen/sqlrt/uow.go.txt`) precisa acompanhar a
assinatura mesmo sem implementar `StreamLister` neste ciclo.

### Solução 2

`aggregateID` prefixado (`"<AggregateType>:<id>"`), estampado pelo caller e
desempacotado por `ListStreams`/`Append`. Descartada: muda o formato do dado
persistido — afeta bytes de toda Query já suportada (REQ-55.6) e todo `given
Subject from [...]` de `*.test.ds` — e é irreversível sobre dados já gravados,
custo muito maior que a mudança de seam da Solução 1.

## O que precisa ser resolvido antes

A decisão de fundo (threadar via `ctx`, mesmo padrão de `tenantID`) já foi
tomada pelo dono e está registrada no `SOLVED` original da issue. O que falta é
trabalho editorial + implementação:

- Emendar NFR-31 do ciclo `correcoes-issues-6-8-12`: "wallet/shop permanecem
  byte-idênticos" é incompatível com qualquer rota que carregue o tipo a partir
  do código gerado (a linha de `Append` de todo `UseCase` muda de bytes).
- Explicitar o escopo de NFR-32: a interface que muda é `runtime.Tx`, não
  `runtime.EventStore` (que fica intocada).
- Registrar formalmente "terceiro parâmetro em `Append`" em vez de um método
  irmão `AppendTyped` — recomendação já justificada na análise (NFR-33, evitar
  caminho não tipado que grava stream invisível a `ListStreams`).
- Atualizar `target_files`/`design.md`/`requirements.md` de
  [[docs/sdd/specs/correcoes-issues-6-8-12/requirements|correcoes-issues-6-8-12]]
  e a task [[docs/sdd/specs/correcoes-issues-6-8-12/tasks/M1.1|M1.1]] com o
  fatiamento sugerido (M1.1a–d) antes de retomar a implementação.

Ver também a issue irmã
[[docs/sdd/issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype]], que
compartilha causa raiz e solução.
