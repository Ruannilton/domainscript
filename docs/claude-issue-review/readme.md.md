# Revisão das issues (2026-08-01)

Índice das 22 issues de [[docs/sdd/issues/open-issues.md|docs/sdd/issues]], cada uma
com um review próprio em `claude-reviwed-issues/` seguindo
[[docs/claude-issue-review/issue.template.md|issue.template.md]]. A maioria das
issues originais já carrega uma seção "Solução proposta" de uma sessão anterior
(Veredito/Causa raiz/Bloqueios/Fatiamento) — este índice não reabre essa
investigação, ele classifica o que já existe em uma de três rotas de ação.

**Critério de classificação:**

- **Correção de código** — a spec é clara (às vezes só ficou clara depois da
  revisão v7 de 2026-07-31); o que falta é implementação. Sem decisão pendente.
- **Ajuste de especificação** — a spec tem lacuna/contradição, mas já existe
  decisão do desenvolvedor (nota embutida na issue ou em
  [[docs/notes/Issues]]) que a resolve; falta só redigir o texto normativo +
  implementar.
- **Dependente de decisão do desenvolvedor** — a lacuna segue sem decisão:
  duas ou mais saídas em aberto, ou pergunta explícita sem resposta. **É aqui
  que você precisa agir primeiro.**

Uma nota lateral que apareceu repetidas vezes durante a revisão: quando o
defeito nasce de um exemplo ou fixture (`docs/examples/`, `testdata/projects/`)
fazendo algo que a spec não descreve, a issue nunca cai em "Correção de
código" — o exemplo não dita a spec (ver `CLAUDE.md`, seção "Examples vs.
fixtures"). Duas issues abaixo são exatamente esse padrão:
[[docs/claude-issue-review/claude-reviwed-issues/cache-ratelimit-backend-exige-string-contra-spec|cache-ratelimit-backend]]
tem a forma errada num fixture (`backend: "redis"` em vez de `backend: redis`)
mas isso *é* código puro porque a spec já é inequívoca; já
[[docs/claude-issue-review/claude-reviwed-issues/pizzeria-bloqueado-por-multiplos-defeitos-de-codegen|pizzeria]]
tem um fixture que só compila se a linguagem ganhar uma forma que a spec nunca
prometeu — por isso fica em "dependente de decisão".

---

# Dependente de decisão do desenvolvedor
*(9 issues — comece por aqui: nada nelas avança até você responder às perguntas listadas em cada review)*

- [[docs/claude-issue-review/claude-reviwed-issues/visibility-de-view-nao-implementado|visibility-de-view-nao-implementado]] — `visibility` de View é parseado e totalmente ignorado, vazando campos restritos; 5 perguntas de spec em aberto. Issue original: [[docs/sdd/issues/visibility-de-view-nao-implementado]]
- [[docs/claude-issue-review/claude-reviwed-issues/usecase-access-block-nao-parseado|usecase-access-block-nao-parseado]] — `access` em UseCase não parseia; sintaxe pronta, mas semântica de `caller`/cross-tenant falta. Issue original: [[docs/sdd/issues/usecase-access-block-nao-parseado]]
- [[docs/claude-issue-review/claude-reviwed-issues/usecase-idempotency-required-intestavel-test-ds|usecase-idempotency-required-intestavel-test-ds]] — §24 não define como um cenário fornece Idempotency-Key/contexto de chamada. Issue original: [[docs/sdd/issues/usecase-idempotency-required-intestavel-test-ds]]
- [[docs/claude-issue-review/claude-reviwed-issues/observabilidade-otel-parcial|observabilidade-otel-parcial]] — métricas/logs OTel não exportam; falta a spec nomear gauges, métricas e enums. Issue original: [[docs/sdd/issues/observabilidade-otel-parcial]]
- [[docs/claude-issue-review/claude-reviwed-issues/providers-reais-de-infraestrutura-ausentes|providers-reais-de-infraestrutura-ausentes]] — vazamento de pgx é código puro (pode andar já); catálogo de rótulos (RateLimit, §2.7, idempotency) segue em aberto. Issue original: [[docs/sdd/issues/providers-reais-de-infraestrutura-ausentes]]
- [[docs/claude-issue-review/claude-reviwed-issues/pizzeria-bloqueado-por-multiplos-defeitos-de-codegen|pizzeria-bloqueado-por-multiplos-defeitos-de-codegen]] — 5 defeitos empilham o pizzeria; rota para `list <Aggregate> as View` foi explicitamente adiada, não decidida. Issue original: [[docs/sdd/issues/pizzeria-bloqueado-por-multiplos-defeitos-de-codegen]]
- [[docs/claude-issue-review/claude-reviwed-issues/lacunas-nos-testes-gerados-test-ds|lacunas-nos-testes-gerados-test-ds]] — maioria das lacunas de `*.test.ds` já destravada pela spec; `released` e contexto de chamada seguem indefinidos. Issue original: [[docs/sdd/issues/lacunas-nos-testes-gerados-test-ds]]
- [[docs/claude-issue-review/claude-reviwed-issues/m4-1-shrinking-de-property-muda-golden-fora-de-target-files|m4-1-shrinking-de-property-muda-golden-fora-de-target-files]] — shrinking de property exige golden regenerado; rota de regeneração e definição de "mínimo" não decididas. Issue original: [[docs/sdd/issues/m4-1-shrinking-de-property-muda-golden-fora-de-target-files]]
- [[docs/claude-issue-review/claude-reviwed-issues/divergencias-menores-do-spec-em-evolucao|divergencias-menores-do-spec-em-evolucao]] — GDPR e agregações/FFI aguardam sintaxe que o spec ainda não define; cobertura por ramo é só análise pendente. Issue original: [[docs/sdd/issues/divergencias-menores-do-spec-em-evolucao]]

# Ajuste de especificação
*(7 issues — a decisão já foi tomada; falta redigir o texto normativo e, na sequência, implementar)*

- [[docs/claude-issue-review/claude-reviwed-issues/spec-v7-retrywithbackoff-sem-definicao|spec-v7-retrywithbackoff-sem-definicao]] — `RetryWithBackoff` indefinido; nota do dev já decide por `maxAttempts` + trigger de down, falta escrever na §19. Issue original: [[docs/sdd/issues/spec-v7-retrywithbackoff-sem-definicao]] · Nota: [[docs/notes/Issues/spec-v7-retrywithbackoff-sem-definicao]]
- [[docs/claude-issue-review/claude-reviwed-issues/spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos|spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos]] — `sum` sobre ValueObject e `focus` sem contrato contradizem §2.8; a própria análise resolve por eliminação técnica, falta redigir. Issue original: [[docs/sdd/issues/spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos]]
- [[docs/claude-issue-review/claude-reviwed-issues/features-spec-v6-nao-modeladas-pelo-frontend|features-spec-v6-nao-modeladas-pelo-frontend]] — TCP/UDP descartado e tenancy-por-agregado já decididos; faltam registrar/detalhar na spec. Issue original: [[docs/sdd/issues/features-spec-v6-nao-modeladas-pelo-frontend]]
- [[docs/claude-issue-review/claude-reviwed-issues/usecase-e-policy-no-mesmo-modulo-colisao-de-wire|usecase-e-policy-no-mesmo-modulo-colisao-de-wire]] — colisão de Wire já corrigida na prática; falta escrever o invariante de nomenclatura em design.md. Issue original: [[docs/sdd/issues/usecase-e-policy-no-mesmo-modulo-colisao-de-wire]]
- [[docs/claude-issue-review/claude-reviwed-issues/m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files|m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files]] — design.md §4.4 não decidia o caso só-Saga; gramática de nomes já decidida na issue-irmã. Issue original: [[docs/sdd/issues/m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files]]
- [[docs/claude-issue-review/claude-reviwed-issues/m1-1-aggregatetype-nao-chega-a-eventstore-append|m1-1-aggregatetype-nao-chega-a-eventstore-append]] — rota já decidida (ctx, granularidade por chamada); falta virar texto normativo em design.md + implementar. Issue original: [[docs/sdd/issues/m1-1-aggregatetype-nao-chega-a-eventstore-append]]
- [[docs/claude-issue-review/claude-reviwed-issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype|m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype]] — carimbar ctx uma vez por Run é inseguro; rota certa (tipo por chamada) já definida, falta emendar NFR-31/32. Issue original: [[docs/sdd/issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype]]

# Correção de código
*(6 issues — spec clara, sem decisão pendente; pode ir direto para `task-implementer`/issue de implementação)*

- [[docs/claude-issue-review/claude-reviwed-issues/cache-ratelimit-backend-exige-string-contra-spec|cache-ratelimit-backend-exige-string-contra-spec]] — gerador exige string entre aspas onde a §13 pede identificador nu; sem ambiguidade. Issue original: [[docs/sdd/issues/cache-ratelimit-backend-exige-string-contra-spec]]
- [[docs/claude-issue-review/claude-reviwed-issues/m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real|m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real]] — Query lê store em memória em vez do banco real do produtor durável; gap de codegen puro. Issue original: [[docs/sdd/issues/m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real]]
- [[docs/claude-issue-review/claude-reviwed-issues/spec-v7-identidade-implicita-do-aggregate|spec-v7-identidade-implicita-do-aggregate]] — spec v7 §4.3.1 já define `self.id` implícito como `ref T`; falta implementar em `types.Model`. Issue original: [[docs/sdd/issues/spec-v7-identidade-implicita-do-aggregate]] · Nota: [[docs/notes/Issues/spec-v7-identidade-implicita-do-aggregate]]
- [[docs/claude-issue-review/claude-reviwed-issues/spec-v7-metadata-implicito-de-event|spec-v7-metadata-implicito-de-event]] — spec v7 §4.2.3/§5.3 já define envelope de Event e `ApplicationEvent`; falta implementar. Issue original: [[docs/sdd/issues/spec-v7-metadata-implicito-de-event]] · Nota: [[docs/notes/Issues/spec-v7-metadata-implicito-de-event]]
- [[docs/claude-issue-review/claude-reviwed-issues/spec-v7-catalogo-de-metodos-embutidos|spec-v7-catalogo-de-metodos-embutidos]] — spec v7 §2.8 já é o catálogo fechado completo; falta implementar em `types.Model`/`goname`. Issue original: [[docs/sdd/issues/spec-v7-catalogo-de-metodos-embutidos]] · Nota: [[docs/notes/Issues/spec-v7-catalogo-de-metodos-embutidos]]
- [[docs/claude-issue-review/claude-reviwed-issues/spec-v7-adapter-sem-contrato-de-resposta|spec-v7-adapter-sem-contrato-de-resposta]] — spec v7 §9.4 já normatiza `-> T`/`response{}`; falta implementar call/mock com retorno. Issue original: [[docs/sdd/issues/spec-v7-adapter-sem-contrato-de-resposta]] · Nota: [[docs/notes/Issues/spec-v7-adapter-sem-contrato-de-resposta]]

---

## Notas da revisão

- **As quatro issues "spec-v7" do lote de 2026-07-31** (identidade, metadata,
  catálogo de métodos, adapter) foram reconferidas linha a linha contra o
  texto atual de
  [[docs/sdd/steerings/domainscript-spec-v7/02-type-system]],
  [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core]],
  [[docs/sdd/steerings/domainscript-spec-v7/05-application-layer]] e
  [[docs/sdd/steerings/domainscript-spec-v7/09-notifications-adapters]]: a
  spec já cobre as quatro por completo, sem lacuna residual. Migraram de
  "revisão de spec pendente" (como estavam em
  [[docs/sdd/issues/open-issues.md]]) para "correção de código" puro — é
  trabalho de implementação represado, não mais uma decisão em aberto.
- **`spec-v7-retrywithbackoff-sem-definicao`** é a quinta desse lote e ficou em
  "Ajuste de especificação", não "Correção de código": a nota do desenvolvedor
  decide a direção (`maxAttempts` + comando de down), mas o texto normativo da
  §19 ainda não foi escrito — ao contrário das outras quatro, aqui o texto
  normativo em si é o trabalho pendente.
- **`providers-reais-de-infraestrutura-ausentes`** é um caso misto: uma fatia
  (vazamento de dependência `pgx`) é correção de código sem bloqueio algum e
  pode avançar já; o resto da issue depende de decisões de catálogo ainda
  abertas. Fica listada inteira em "dependente de decisão" porque é onde está
  o grosso do valor residual, mas o review file detalha a fatia destravada.
- **`pizzeria-bloqueado-por-multiplos-defeitos-de-codegen`** tem uma anotação
  "decisão do usuário: não perseguir agora" — isso foi tratado como **adiar**,
  não como **resolver**: o garfo técnico (estender codegen vs. reescrever
  fixture) continua sem resposta, por isso a issue segue em "dependente de
  decisão" para essa sub-pergunta específica, mesmo com os outros três defeitos
  do mesmo arquivo já desbloqueados.
- **`spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos`** é a única
  classificação como "ajuste de especificação" sem nota explícita do
  desenvolvedor em [[docs/notes/Issues]] — a leitura foi que a própria análise
  técnica da issue fecha as duas perguntas por eliminação (intraduzibilidade
  para SQL descarta a alternativa de `sum` sobre VO; o precedente de `load`
  fixa a forma de `focus`). Vale conferir se essa leitura bate com sua
  intenção antes de redigir o texto na §2.8/§22.
