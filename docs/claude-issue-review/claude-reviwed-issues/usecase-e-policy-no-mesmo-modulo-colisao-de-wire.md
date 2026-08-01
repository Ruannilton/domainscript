CODIGO: usecase-e-policy-no-mesmo-modulo-colisao-de-wire
CATEGORIA: Ajuste de especificação
Issue original: [[docs/sdd/issues/usecase-e-policy-no-mesmo-modulo-colisao-de-wire]]

## Resumo da issue

Um módulo que combina `UseCase` e `Policy` costumava não gerar: os dois emitiam sua própria `func Wire(...)`, e duas funções com o mesmo nome no mesmo pacote Go colidem. A task L1.1 já corrigiu essa colisão especificamente — hoje existe um `Wire` combinado (`Wire(u UnitOfWork, d Dispatcher)`) e há testes cobrindo o caso misto. O que sobrou, e é o motivo de a issue continuar aberta, é que a *regra geral* por trás da correção nunca foi escrita: ninguém registrou por que `Wire` colide e os outros cinco construtos (`StartWorkers`, `WireQueryCache`, `WireMetrics`, `WireOutboxStore`, `WireFileStorage`) não — e por isso o próximo construto (Saga com `emit`, na issue irmã) bateu na mesma parede de novo.

## Evidencias

- O ramo misto existe e é o caminho normal: `codegen/codegen.go:511` (`mixed := hasUseCases && hasPolicies`), `codegen/codegen.go:626` (`emitUseCasesBytes(..., !mixed)`), `codegen/decl_policy.go:648-673` (`emitCombinedWireFunc`). `go test ./codegen/ -run TestGenerateMixedModuleWiresCombinedWireAndCompiles` passa; dois testes dedicados (`mixed_wire_test.go`, `mixed_wire_maincall_test.go`) cobrem o caso.
- A doc de `codegen/decl_policy.go:104-115` ainda afirma que `generateModuleFiles` "recusa esse caso HOJE com um erro de geração claro" — texto pré-L1.1, hoje falso.
- Rodando `dsc gen testdata/projects/pizzeria` hoje: a guarda de wiring combinado (que motivou originalmente esta issue) já não aparece nos erros — a geração avança até `Kitchen/queries.go` e para num defeito ortogonal (`list <Aggregate>` em posição de expressão pura), sem relação com wiring.
- Causa raiz identificada na análise: `Wire` é a única entrada da superfície de wiring cuja assinatura depende do conteúdo declarado (`Wire(u)`, `Wire(d)`, `Wire(u,d)`, montada posicionalmente em `codegen/codegen.go:1467-1476`) — um espaço de 2^n assinaturas conforme construtos se somam. Os cinco construtos que escolheram nome próprio nunca colidiram com nada.

## Impacto no projeto

Enquanto o invariante não estiver escrito, cada construto de back-end novo volta a "relitigar" a mesma decisão de nomenclatura — foi exatamente o que aconteceu com `[[docs/sdd/issues/m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files|m2-3]]`, que tentou pendurar o Dispatcher de Saga na `Wire` de Policy e ficou bloqueada por essa mesma lacuna de design. A spec v7 (`[[docs/sdd/steerings/domainscript-spec-v7/05-application-layer|05-application-layer.md]]` §5.3) também está prestes a somar quatro emissores novos de `ApplicationEvent` (UseCase, Policy, Worker, Saga) — sem a regra escrita, seriam quatro setters exportados disputando o mesmo objeto.

## Soluçoes possíveis

### Solucão 1

Registrar como invariante de design a gramática que o código já pratica: `Wire(...)` fica congelada nas três assinaturas de L1.1 (nunca ganha parâmetro novo); dependências novas usam nome próprio pela **forma** — `Wire<Dependência>` (injeção pura), `Wire<Concern>` (injeção + registro), `Start<Concern>` (goroutine de fundo) — com ordem de chamada fixa em `cmd/<service>/main.go`. Concretamente, um `WireDispatcher(d runtime.Dispatcher)` novo (`codegen/wiring.go`) vira o seam único de Dispatcher do módulo, substituindo o `policyDispatcher` de hoje e servindo de base para a Saga depois. Custo marginal de cada construto futuro cai para "um campo em `moduleMarks` + uma linha condicional em `main.go`".

### Solução 2

Unificar tudo numa única `Wire` com todas as dependências — por posição (`Wire(u, d, x, ...)`) ou por struct (`Wire(deps ModuleDeps)`). Descartada: a variante posicional é exatamente a explosão 2^n que causou a colisão original; a variante struct de fato unifica, mas reescreveria todos os 11 goldens de `Wire` e os 6 goldens de `main.go` de uma vez, para terminar com o mesmo custo marginal por construto (um campo no struct) que a Solução 1 já dá sem quebrar nada. Nome próprio *por construto* para o Dispatcher (`WireSagas`, `WireUseCaseEvents`, …) também foi descartado — funcionaria, mas multiplicaria setters para injetar o mesmo objeto assim que os quatro emissores de `ApplicationEvent` da `[[docs/sdd/steerings/domainscript-spec-v7/05-application-layer|05-application-layer.md]]` §5.3 chegarem.

## O que precisa ser resolvido antes

A decisão já foi tomada pelo desenvolvedor — nota embutida na própria issue, na seção Bloqueios: "descreva na spec regras para nomes de funções e campos gerados pelo compilador durante o build. Utilize a solução proposta: **Nomes próprios por concern, com contrato uniforme** — a uniformidade está na forma (uma função exportada por concern, uma dependência por chamada, ordem de chamada fixa em `main.go`), não numa função única."

O que falta é trabalho editorial + implementação, não mais decisão:

1. Escrever a tabela da gramática (Solução 1 acima) em `[[docs/sdd/specs/correcoes-issues-6-8-12/design.md|design.md]]` §4.4, e corrigir a doc estale de `codegen/decl_policy.go:104-115`.
2. Trocar os seis `map[string]bool` paralelos de `Generate`/`generateCmdMainFile` por um único `map[string]moduleMarks` (byte-idêntico por construção).
3. Implementar `WireDispatcher` (`codegen/wiring.go` novo) e migrar `policyDispatcher` para ele — primeiro cliente real do seam, com cobertura de teste já existente para adaptar.

O vínculo histórico com o `pizzeria` deixou de ser argumento para manter esta issue aberta: o bloqueio real remanescente daquele exemplo (`list <Aggregate>`) é ortogonal a wiring e não depende de nada aqui.
