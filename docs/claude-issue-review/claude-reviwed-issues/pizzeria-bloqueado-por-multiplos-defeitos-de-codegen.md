CODIGO: pizzeria-bloqueado-por-multiplos-defeitos-de-codegen
CATEGORIA: Dependente de decisão do desenvolvedor

Issue original: [[docs/sdd/issues/pizzeria-bloqueado-por-multiplos-defeitos-de-codegen]]

## Resumo da issue

O fixture `testdata/projects/pizzeria` não gera ponta a ponta. A investigação encontrou pelo menos cinco defeitos independentes de codegen, empilhados um atrás do outro: a guarda F5/G3 (Policy+Query cacheada e módulo produtor de canal no mesmo service) realmente dispara para o módulo `Kitchen`; e, mais cedo no pipeline, quatro bloqueios adicionais e ortogonais impedem a geração de sequer chegar até essa guarda. Três desses quatro são bugs claros de implementação; o quarto — `list <Aggregate> ... as View` nunca foi implementado — é um gap de codegen genuíno cuja rota de correção o próprio desenvolvedor decidiu não escolher agora.

## Evidencias

- Guarda F5/G3 confirmada: `Kitchen` emite `TicketFinished` (`PublicEvent`) e é produtor do canal `Kitchen -> Sales` (`topology.ds`, `via: queue`) dentro do mesmo service `PizzeriaMonolith`; `Policy CreateTicketOnOrderPaid` (`kitchen/policy.ds`) força `needsDispatcher = true` (`codegen/codegen.go:1089`) — combinação exata que dispara `producerChannel != nil && needsDispatcher`.
- Erro real hoje ao rodar `dsc gen`: `Aggregate KitchenTicket: Handle Create: access: codegen: CallExpr com Fn *ast.MemberExpr não suportado em Lowerer.Expr` — causa: `lowerAccessCondition` (`codegen/decl_aggregate.go:341`) só trata `BinaryExpr`; uma condição que é só `caller.hasRole(...)` (um `CallExpr` puro) cai no fallback genérico e é rejeitada.
- `emitApply` (`codegen/decl_aggregate.go:274`) nunca chama `.WithBuiltins(...)` no `Lowerer`, ao contrário de `emitUseCasesBytes`/`emitPolicyExecute`/Saga/Query — qualquer builtin (`now()`) dentro de um `Apply` falha.
- `.add()` só está mapeado para campo `AppendList<T>` (`codegen/goname/types.go:111`); Kitchen declara `items List<TicketItem>` (List comum) — provável typo do próprio fixture (deveria ser `AppendList<TicketItem>`, como `wallet/domain.ds:88`).
- `list <Aggregate> ... as View`: `tryEmitListVO` (`codegen/decl_query.go`, ~linha 461) só reconhece `list <nome>` quando `<nome>` resolve a `*types.VOType` correlacionado via `AppendList<VO>` — um nome de Aggregate resolve a `*types.ShapeType`, então a checagem falha sempre, independente de provider (confirmado empiricamente ao trocar o provider de Kitchen para `"sqlite"`: o erro não mudou). `sales/read.ds` tem a mesma forma e nunca foi de fato exercitada — a geração sempre falha em Kitchen primeiro.
- Citação da decisão registrada na própria issue: "**Decisão do usuário (não perseguir agora):** em vez de (a) estender `tryEmitListVO`/`EmitQuery` ... ou (b') reescrever as Queries de Kitchen/Sales para a forma já suportada, o usuário optou por **parar aqui, registrar e seguir para as Fases L2/L3** ... L1.3d/L1.3e/L1.3f ficam **BLOQUEADAS**, sem tentativa adicional neste ciclo."

## Impacto no projeto

O único fixture do repositório que exercita topologia multi-service com canal de mensageria (`Kitchen -> Sales`, `via: queue`) permanece sem geração e sem `go build` provados — está em `KNOWN_UNGENERATABLE` no CI. Isso deixa sem regressão real a combinação Policy local + Query cacheada + módulo produtor de canal (F5/G3) e a forma `list <Aggregate> ... as View`, que também está latente e não provada em `sales/read.ds` (`wallet`/`shop` não a exercitam).

## Soluçoes possíveis

### Solucão 1

Estender `tryEmitListVO`/`EmitQuery` (`codegen/decl_query.go`) para suportar de fato `list <Aggregate> ... as View`, sem exigir correlação via `AppendList<VO>`. Rota "inevitável" segundo a própria análise, mas classificada como grande demais para ser feita de passagem — precisa de task própria.

### Solução 2

Reescrever as Queries de `Kitchen`/`Sales` para a forma já suportada hoje (correlação via `AppendList<VO>`), evitando tocar o codegen. Mais barata a curto prazo, mas empurra a limitação real de `list <Aggregate> ...` para o próximo programa que tentar essa forma — nenhuma das duas rotas foi escolhida pelo desenvolvedor até agora.

## O que precisa ser resolvido antes

1. Para `list <Aggregate> ... as View`: estender o codegen (rota a) ou reescrever as Queries dos fixtures (rota b')? Nenhuma foi decidida — o desenvolvedor optou explicitamente por adiar essa escolha.
2. A guarda F5/G3 (Policy+Query cacheada e módulo produtor de canal no mesmo service) precisa de um recorte de wiring combinado que a spec `correcoes-issues-6-8-12` não cobre — vale um ciclo/spec dedicado, ou uma fixture-alvo diferente do `pizzeria` para não misturar essa decisão com os outros quatro defeitos?

Os outros três defeitos (`lowerAccessCondition` sem suporte a `CallExpr` puro em `access`; `emitApply` sem `WithBuiltins`; `.add()` em `List` comum, provável typo do fixture) não dependem de nenhuma decisão — são bugs de implementação com causa raiz já identificada e podem ser corrigidos independentemente das duas questões acima.
