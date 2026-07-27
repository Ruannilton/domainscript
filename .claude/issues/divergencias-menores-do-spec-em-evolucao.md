# Divergências menores do spec (§25, "em evolução") (ex-ISSUE-8)
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: [gaps.md §G-baixo](../specs/codegen/gaps.md) (§25 — em evolução no próprio spec)
- DESCRIPTION: Divergências menores, a maioria marcada como planejada/"em
  evolução" pelo próprio spec (§25) — registradas para rastreio, sem ação
  urgente: (a) **Redação GDPR** (§4.4) — placeholder tipado implementado
  (E4.3), mas o *gatilho* de redação não (spec o marca como em evolução);
  (b) **Cobertura semântica** (§22.7) — o warning "Handle sem cenário de erro
  testado" existe ([`sema/rules_warnings.go`](../../sema/rules_warnings.go):`checkHandleErrorCoverage`,
  REQ-5.22), mas o relatório fino "por ramo de regra de negócio" fica na
  granularidade por Handle; (c) **itens §25** (avg/min/max/group by, aritmética
  estendida, marshalling FFI detalhado) — declarados planejado/a definir pelo
  spec, sem ação pendente deste lado.

  EM ANDAMENTO (spec encerrada e sucedida): a spec `correcoes-issues-6-7-8`
  (Marco L, REQ-54 / §design 4) que abriu esta frente foi fechada — ver
  [`correcoes-issues-6-8-12`](../specs/correcoes-issues-6-8-12/requirements.md)
  (Marco M) para o que foi retomado. Decisão por item: (b) cobertura §22.7 —
  a task L3.1 começa pela análise de raiz de `checkHandleErrorCoverage`; se
  o checker consegue cruzar os ramos `ensure ... else Error` com os cenários
  de erro testados, refina o warning para o ramo específico (fecha em
  `sema`); senão, mantém por-Handle e reclassifica como ciclo de sema
  dedicado, com o motivo. (a) redação GDPR (§4.4) e (c) §25 (agregações/
  aritmética/FFI) — **reclassificados** de "dívida de codegen" para
  "aguardando definição no spec da linguagem" (exigem sintaxe nova não
  definida; não há ação de codegen pendente). Fecha (b) e reclassifica
  (a)/(c) quando o Marco L fechar. Conforme `.claude/state.md`, o Marco L
  ainda está in-progress (task corrente: L2.5), então esta issue segue
  aberta.
- SOLVED: FALSE
