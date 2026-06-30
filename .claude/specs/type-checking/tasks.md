# Tasks — Resolução Completa de Nomes & Tipos

> Documento 3 de 3. Plano executável para `requirements.md` (REQ-9..13) via
> `design.md`. Mesmas convenções do `tasks.md` do front-end: ordem respeita
> dependências, fatiar **verticalmente**, cada task tem **critério de conclusão** e
> fecha em **commit** atômico (Conventional Commits, português imperativo). Só
> commitar com a árvore verde (`go build ./...` e `go test ./...`).
>
> **Esta spec ainda não foi iniciada** — todas as tasks começam `[ ]`.
>
> Escopos de commit novos: `types`. Reusa os existentes (`resolver`, `sema`,
> `ast`, `diag`, `repo`).

## Como cada bug do Wallet é fechado

- `amoun` (corpo) → **Fase A** (REQ-9).
- `Walle` (config) → **Fase B** (REQ-10).
- `self.i` (membro) → **Fase D** (REQ-12), apoiada na **Fase C** (REQ-11).

---

## Fase A — Resolução de Nomes em Corpos (REQ-9)

> Fecha o bug `amoun`. Maior fatia de valor isolado: pega typos em corpos sem
> precisar de tipos.

- [x] **A.0** Extrair a travessia genérica para um pacote neutro reusável.
  - Mover `forEachStmt`, `forEachExpr`, `forEachExprInBlock`, `stmtExprs`,
    `declBlocks`, `stateField`, `isIdent`, `headName` de `sema/walk.go` para
    `astutil/` (ou `ast/walk`); reapontar os usos no `sema/`.
  _(NFR-8, §design 5)_
  **Conclusão:** `go build ./...` e `go test ./...` verdes após a mudança; `sema/`
  passa a importar o novo pacote.
  **Commit:** `refactor(ast): extrai travessia genérica para astutil`

- [x] **A.1** Modelo de `Scope` com push/pop e `Binding`.
  _(REQ-9.2/9.5, §design 3.1)_
  **Conclusão:** testes unitários de `Define`/`Lookup`/`Child` (sombra de nome em
  binder aninhado, nome some ao sair do filho).
  **Commit:** `feat(resolver): modelo de Scope léxico`

- [x] **A.2** Tabela de receptores contextuais por construto.
  _(REQ-9.3, §design 3.2)_
  **Conclusão:** teste tabela construto→receptores (`Handle` tem `self`/`state`/
  `caller`; `Apply` tem `event`; `Valid` tem `value`/`ok`).
  **Commit:** `feat(resolver): receptores contextuais por construto`

- [x] **A.3** Passagem de resolução de corpos: percorre `declBlocks`, resolve
  `*ast.Ident` em posição de valor; distingue uso de definição (binder, nome de
  campo de `Arg`). _(REQ-9.1/9.4/9.6, §design 3.3)_
  **Conclusão (par de testes):** corpo com ident inexistente dispara erro
  localizado; corpo correto fica em silêncio. Inclui o caso `amoun` do Wallet.
  **Commit:** `feat(resolver): resolução de nomes em corpos executáveis`

---

## Fase B — Refs de Configuração (REQ-10)

> Fecha o bug `Walle`. Independente da Fase A; pode ir em paralelo.

- [x] **B.1** Catálogo declarativo config-ref → `Kind` esperado e extração dos nomes.
  _(REQ-10.1, §design 3.4)_
  **Conclusão:** teste do catálogo cobrindo `manages`, `Route.Target`,
  `ServiceDef.modules`, `ChannelDef.From/To`, `VersionRoute.Target`.
  **Commit:** `feat(resolver): catálogo de referências de configuração`

- [x] **B.2** Resolução das refs contra a tabela de símbolos: inexistente → erro;
  `Kind` divergente → erro esperado-vs-encontrado. _(REQ-10.2/10.3)_
  **Conclusão (par de testes):** `manages: [Inexistente]` dispara; `manages:
  [Aggregate válido]` silencia; `manages: [umEvent]` dispara erro de Kind. Inclui o
  caso `Walle` do Wallet.
  **Commit:** `feat(resolver): resolve referências de configuração`

---

## Fase C — Modelo de Tipos (REQ-11)

> Base para membro (D) e compatibilidade (E). Sem diagnósticos próprios ainda
> (exceto o tipo de erro sentinela).

- [x] **C.1** Pacote `types`: representação `Type` e variantes (`Primitive`,
  `VOType`, `EnumType`, `ShapeType`, `Generic`, `FuncType`, `errorType`).
  _(REQ-11.1, §design 3.5)_
  **Conclusão:** compila; teste de identidade/igualdade de tipos.
  **Commit:** `feat(types): representação de Type e variantes`

- [x] **C.2** `model.go`: `TypeOf(symbol)` e catálogo de membros por tipo
  (campos de VO/Aggregate/Event/Command, membros de Enum). _(REQ-11.1, §design 3.5)_
  **Conclusão:** teste que dado um Aggregate, `state`/`self` expõem seus campos.
  **Commit:** `feat(types): tipo de declaração e catálogo de membros`

- [x] **C.3** `infer.go`: `Infer(expr, scope)` com propagação de `errorType` sem
  cascata. _(REQ-11.2/11.3, §design 3.5)_
  **Conclusão:** testes de inferência (literal, construção de VO, acesso, operador);
  teste de que subexpressão de erro produz `errorType` e **não** emite diagnóstico.
  **Commit:** `feat(types): inferência de tipo de expressão`

---

## Fase D — Acesso a Membro (REQ-12)

> Fecha o bug `self.i`. Depende de C.

- [x] **D.1** Regra de acesso a membro em `sema/rules_typecheck.go`: valida
  `X.nome` contra o catálogo do tipo de `X`; pula `errorType`; sugere membro mais
  próximo. _(REQ-12.1/12.2/12.3/12.4, §design 3.6)_
  **Conclusão (par de testes):** `self.i` (campo inexistente) dispara erro
  localizado no membro com sugestão `id`; `self.id` silencia. Inclui o caso do
  Wallet. Cobre `event.<campo>` em `Apply`.
  **Commit:** `feat(sema): checagem de acesso a membro`

---

## Fase E — Compatibilidade de Tipos (REQ-13)

> Profundidade definida no design; coordena com REQ-5.1 para não duplicar.

- [x] **E.1** Relação de atribuibilidade e checagem em atribuição/argumento/
  operador/return. _(REQ-13.1/13.2, §design 3.6)_
  **Conclusão (par de testes):** uso incompatível dispara erro esperado-vs-
  encontrado; uso compatível silencia.
  **Commit:** `feat(sema): checagem de compatibilidade de tipos`

- [x] **E.2** Coordenação com a Regra de Ouro (REQ-5.1): uma só mensagem por causa.
  _(REQ-13.3)_
  **Conclusão:** teste de que primitivo no Write Side gera **um** diagnóstico, não
  dois.
  **Commit:** `fix(sema): evita diagnóstico duplicado entre tipos e write side`

---

## Fase F — Códigos de Diagnóstico e Fechamento (NFR-9/10)

- [x] **F.1** Preencher `diag.Code` para as novas famílias (`E100`+), conforme o
  catálogo do design §3.7. _(REQ-6, §design 3.7)_
  **Conclusão:** diagnósticos novos carregam código estável; teste de render com
  código.
  **Commit:** `feat(diag): códigos para diagnósticos de nome e tipo`

- [x] **F.2** Regressão do Wallet: corrigir `docs/examples/wallet` e fixar o teste
  de que (a) a versão com os 3 typos dispara exatamente 3 erros esperados e (b) a
  versão corrigida silencia. _(DoD §5.1–5.4)_
  **Conclusão:** `dsc docs/examples/wallet` (corrigido) → exit 0; teste de
  regressão verde.
  **Commit:** `test(sema): regressão dos três bugs do Wallet`

- [ ] **F.3** Revalidar determinismo e limite de cascata sobre as novas regras;
  atualizar `CLAUDE.md` (estado real do projeto) e os specs. _(NFR-9, NFR-1)_
  **Conclusão:** testes de determinismo/cascata cobrindo erros de nome/tipo; árvore
  verde; auditoria de par positivo+negativo por REQ-9..13 completa (NFR-10).
  **Commit:** `docs(repo): fecha resolução de nomes & tipos e atualiza estado`

---

## Mapa de Dependências

```
Fase A (corpos) ─┐
Fase B (config) ─┼─▶ (independentes entre si)
Fase C (tipos) ──┴─▶ Fase D (membro) ─▶ Fase E (compat) ─▶ Fase F (códigos + fechamento)
```

- A e B só dependem da resolução de símbolos já existente; podem ir em paralelo.
- D depende de C; E depende de D; F fecha sobre tudo.
- Entrega incremental: concluir **A + B** já elimina os bugs `amoun` e `Walle`
  (dois dos três) e entrega valor antes do modelo de tipos.

---

## Rastreabilidade REQ → Task

| Requisito | Tasks |
|---|---|
| REQ-9 | A.1, A.2, A.3 |
| REQ-10 | B.1, B.2 |
| REQ-11 | C.1, C.2, C.3 |
| REQ-12 | D.1 |
| REQ-13 | E.1, E.2 |
| NFR-8 | A.0 |
| NFR-9 | C.3, F.3 |
| NFR-10 | A.3, B.2, C.3, D.1, E.1, F.2, F.3 |
