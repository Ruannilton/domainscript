# `access { requires ... }` em UseCase não é parseado (§14)
- SPEC: [transpilador](../specs/transpilador/requirements.md)
- TASK: descoberto ao escrever os exemplos spec-first
  ([review-v7.md §A-9](../steerings/review-v7.md))
- DESCRIPTION: A [§14](../steerings/domainscript-spec-v7/14-multi-tenancy.md)
  (Multi-Tenancy) fecha com o exemplo canônico de acesso
  cross-tenant, e ele declara um bloco `access` **dentro do UseCase**:

  ```ds
  UseCase GenerateGlobalReport handles GlobalReportCmd {
      tenancy: cross_tenant
      access { requires caller.hasRole("super_admin") }
      execute {
          allWallets = list Wallet take 10000
      }
  }
  ```

  O parser só modela `access` em Aggregate (`ast.AggregateDecl.Access`);
  `ast.UseCaseDecl` não tem o campo, e `parseUseCase` rejeita o membro.
  Rodando o trecho acima verbatim com o `dsc` do HEAD:

  ```
  5:5:  error: membro de UseCase inesperado: IDENT
  5:14: error: membro de UseCase inesperado: IDENT
  5:29: error: membro de UseCase inesperado: .
  6:5:  error: esperava uma declaração de topo, encontrei IDENT
  ```

  Isolado: `tenancy: cross_tenant` sozinho passa limpo — é especificamente o
  bloco `access` que quebra.

  **Impacto.** A §14 exige que o opt-in cross-tenant venha acompanhado de role
  privilegiada ("Cross-tenant opt-in: `tenancy: cross_tenant` + role
  privilegiada + auditoria automática + warning"). A regra de compilação do
  opt-in existe
  ([`checkCrossTenantOptIn`](../../../sema/rules_crossfile.go#L106),
  [rules_crossfile.go](../../../sema/rules_crossfile.go)) e o warning de
  auditoria também (`checkCrossTenantAudit`), mas **a sintaxe que
  expressaria a exigência de role não existe** — hoje só dá para declarar o
  opt-in, não a proteção que a spec pede junto.

  **Escopo a confirmar ao implementar.** A spec mostra esta forma **uma única
  vez**, de passagem, no exemplo da §14; a
  [§5.2](../steerings/domainscript-spec-v7/05-application-layer.md)
  (UseCases) não menciona `access`. Então dá para implementar o que está
  escrito (`access { requires
  <condição> }` num UseCase, mesma gramática de condição do bloco `access` do
  Aggregate), mas vale checar se o alcance pretendido é maior — por exemplo, se
  Query também deveria aceitá-lo. Se a resposta não estiver no texto, é caso de
  issue de revisão de spec separada, não de palpite.

  **Não é lacuna do spec** para o que está escrito: a forma é inequívoca. É
  defeito de código.
- SOLVED: FALSE
