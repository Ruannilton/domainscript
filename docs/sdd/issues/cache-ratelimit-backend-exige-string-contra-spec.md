# `Cache`/`RateLimit` `backend:` exige string literal, contra a forma da §13
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: descoberto ao auditar `docs/examples/` contra a v7
  ([review-v7.md §A-8](../steerings/review-v7.md))
- DESCRIPTION: A [§13](../steerings/domainscript-spec-v7/13-module-infra.md)
  (`mod.ds`) escreve o valor de `backend:` como **identificador nu**, não
  como string: `Cache { backend: layered }`, `RateLimit { backend: redis }`
  — coerente com o resto do bloco, que só aspa valores opacos
  (`provider: "Postgres"`, `backoff: "exponential"`, `exporter: "otlp"`) e
  deixa nus os valores enumerados (`storage: same`, `algorithm:
  token_bucket`, `onBackendFailure: open`, `strategy: row_level`). A
  [§16](../steerings/domainscript-spec-v7/16-cache.md) reforça, listando os
  backends como `memory`, `distributed`, `layered`.
  O gerador aceita **apenas** a forma com aspas e falha na forma do spec:

  ```
  $ dsc gen <mod com "Cache { backend: memory }"> -o out
  dsc: codegen: módulo M: queries.go: codegen: Query Q:
       mod.ds Cache.backend: backend: esperava um literal string, veio *ast.Ident

  $ dsc gen <mod com "RateLimit { backend: redis }"> -o out
  dsc: codegen: cmd/m: cmd/m/main.go: rota POST "/w" -> U: UseCase U:
       mod.ds RateLimit.backend: backend: esperava um literal string, veio *ast.Ident
  ```

  Reproduzido com o `dsc` do HEAD em dois módulos mínimos (um com Query com
  bloco `cache`, outro com rota sob `rateLimit`); os demais campos enumerados
  do mesmo bloco (`algorithm`, `onBackendFailure`, `storage`, `strategy`)
  aceitam identificador nu normalmente — o defeito é específico de `backend`.
  Onde mora: os leitores de config de [decl_query_cache.go](../../../codegen/decl_query_cache.go)
  (`cacheBackendKind` e vizinhança, ~l.295-352) e [ratelimit.go](../../../codegen/ratelimit.go)
  (~l.264), que exigem `*ast.Literal` de kind STRING.

  **Impacto nos exemplos.** `testdata/projects/wallet/mod.ds` usa a forma com
  aspas (`backend: "redis"`) — gera, mas diverge do spec.
  `testdata/projects/pizzeria/sales/mod.ds` usa a forma do spec
  (`backend: memory`) — está correto e é **latentemente quebrado**: hoje o
  erro fica mascarado porque a geração do pizzeria já falha antes, na Query
  `GetBoardTickets`. Ou seja: os dois exemplos não podem estar simultaneamente
  conformes e funcionais enquanto isto não fechar, e por isso nenhum dos dois
  foi "corrigido" na auditoria dos exemplos.

  **Não é lacuna do spec** — o spec é claro e internamente consistente aqui.
  É defeito de código: a correção é aceitar o identificador nu (e, se quiser
  tolerância, aceitar as duas formas **só** se o spec passar a descrever as
  duas — hoje ele descreve uma).
- SOLVED: FALSE

# Solução proposta

## Veredito

**Real e não resolvida** — reconferida contra o HEAD de hoje (`go build ./...`
verde antes de qualquer mudança).

- [decl_telemetry.go:207-219](../../../codegen/decl_telemetry.go) —
  `configStringLitEntry` exige `*ast.Literal` com `Kind == token.STRING` e
  devolve exatamente `"%s: esperava um literal string, veio %T"`, a mensagem
  do relato.
- [decl_query_cache.go:309](../../../codegen/decl_query_cache.go) —
  `cacheBackendKind` a chama e embrulha em `"mod.ds Cache.backend: %w"`.
- [ratelimit.go:223](../../../codegen/ratelimit.go) — `rateLimitBackendKind`,
  embrulhada em `"mod.ds RateLimit.backend: %w"`.
- **Não citado na issue e decisivo para a correção:**
  [provider_registry.go:166 e :173](../../../codegen/provider_registry.go) —
  `activeProviderDeps` lê a MESMA chave pela MESMA função (ali engolindo o
  erro, `err == nil && ok`) para decidir o `go.mod` e a cópia de
  `redisruntime/`. São **quatro** leituras de `backend:`, não duas.

O front-end já aceita a forma da spec: [parse_module_test.go:53-60](../../../parser/parse_module_test.go)
parseia `Cache { backend: layered, layers: [...] }` limpo, e nada em
`resolver`/`sema` inspeciona `backend`. O defeito é 100% de `codegen`.
Confirmado também o contraste interno que a issue aponta: `algorithm`
([ratelimit.go:197](../../../codegen/ratelimit.go)), `onBackendFailure`
([ratelimit.go:275](../../../codegen/ratelimit.go)) e `concurrentRetry`
([usecase_idempotency.go:201](../../../codegen/usecase_idempotency.go)) já usam
`configIdentEntry` — `backend` é o único enumerado lido como rótulo opaco.

O "latentemente quebrado" do pizzeria também se sustenta:
[sales/mod.ds:24,28](../../../testdata/projects/pizzeria/sales/mod.ds) na forma
da spec, com [sales/read.ds:23](../../../testdata/projects/pizzeria/sales/read.ds)
(`cache { ttl: 1h }`) e [sales/interface.ds:19,26,31](../../../testdata/projects/pizzeria/sales/interface.ds)
(`rateLimit`) exercitando os dois caminhos — hoje mascarado porque pizzeria está
em `KNOWN_UNGENERATABLE` ([ci.yml:69](../../../.github/workflows/ci.yml)).

## Causa raiz

Ao introduzir o registro de providers (Marco J/`providerDep`), reusou-se o
leitor de `provider:` — chave cujo valor a §13 aspa de propósito, por ser
rótulo opaco de produto (`"Postgres"`, `"s3"`, `"rabbitmq"`) — para `backend:`,
cujo valor é **enumerado pela linguagem** e por isso a §13/§16 escrevem nu. Um
enumerado foi tratado como rótulo.

## Solução proposta

Trocar as quatro leituras de `configStringLitEntry` para `configIdentEntry`
([usecase_idempotency.go:95](../../../codegen/usecase_idempotency.go)) — o
helper já existe, já é o usado pelos outros enumerados dos mesmos blocos, e já
produz o erro simétrico (`"backend: esperava um identificador nu, veio
*ast.Literal"`). Nenhum helper novo, nenhuma tolerância às duas grafias: a
forma com aspas passa a ser erro de geração, que é o efeito exigido.

1. `cacheBackendKind` ([decl_query_cache.go:309](../../../codegen/decl_query_cache.go))
   e `rateLimitBackendKind` ([ratelimit.go:223](../../../codegen/ratelimit.go)).
2. **Junto, no mesmo commit**, as duas leituras de `activeProviderDeps`
   ([provider_registry.go:166,173](../../../codegen/provider_registry.go)). Se
   só o par de wiring mudar, um `backend: redis` emite
   `redisruntime.NewRedisQueryCache(...)`/`NewRedisLimiter(...)` **sem** que
   `redisruntime/` seja copiado nem `github.com/redis/go-redis/v9` entre no
   `go.mod` — projeto gerado que não compila. É o acoplamento silencioso que a
   issue não enxergou.
3. `strings.ToLower` sobre o nome do ident pode ficar como está (coerente com
   `activeSQLProviders`); é tolerância de caixa, não de forma.
4. **Não** mexer em `configStringLitEntry` nem nas outras chaves: `provider:`
   de Channel/FileStorage ([channel_rabbitmq.go:82](../../../codegen/channel_rabbitmq.go),
   [decl_filestorage.go:62](../../../codegen/decl_filestorage.go),
   [provider_registry.go:148,189](../../../codegen/provider_registry.go)) e
   `exporter:`/`sampler:` de Telemetry ([decl_telemetry.go:146,167](../../../codegen/decl_telemetry.go))
   são aspados na §13 por decisão da própria spec.
5. **Não** introduzir validação de conjunto fechado para `backend` (recusar
   `layered`, por exemplo): nenhuma regra da
   [§25](../steerings/domainscript-spec-v7/25-compilation-rules.md) exige isso,
   e a queda silenciosa para in-memory é o comportamento de hoje — e o único
   possível enquanto `layered` estiver fora de escopo ([gaps.md:115](../specs/codegen/gaps.md)).
6. Polimento opcional dentro da mesma task: o embrulho duplica a chave
   (`"mod.ds Cache.backend: backend: ..."`, visível no próprio relato acima).
   `resolveRateLimitAlgorithm` já faz certo com `"mod.ds RateLimit.%w"`
   ([ratelimit.go:199](../../../codegen/ratelimit.go)); alinhar os dois
   `backendKind` a esse padrão. Nenhum teste ancora esse texto (verificado).

## Alternativas descartadas

- **Aceitar as duas grafias** (um `configEnumEntry` que engole `Ident` ou
  `STRING`): é a rota mais barata — e por isso a tentação real —, mas
  explicitamente proibida pelo CLAUDE.md ("nunca aceitar as duas"); deixaria
  metade da superfície não especificada.
- **Alargar `configStringLitEntry` para aceitar `Ident`**: contamina
  `provider:`/`exporter:`/`sampler:`, que a §13 aspa de propósito — inverteria
  o defeito em vez de corrigi-lo.
- **Normalizar no front-end** (parser ou `program` convertendo `Ident` em
  literal STRING nas entries de `Cache`/`RateLimit`): quebra o split
  sintaxe/semântica (NFR-6), obriga o parser a conhecer chaves de
  infraestrutura, e apaga justamente a distinção enumerado/rótulo que a §13 usa.
- **Corrigir só as duas linhas que a issue cita**: gera projeto que não compila
  (ver item 2 acima).

## Raio de alcance

- **Produção:** 4 linhas em 3 arquivos.
- **Bytes gerados: inalterados.** Só muda *como* o valor é lido;
  `cacheBackendKind`/`rateLimitBackendKind` continuam devolvendo `"redis"`.
  Logo `codegen/testdata/queries_wallet.go.golden`,
  `query_get_wallet.go.golden`, `cmd_wallet_main.go.golden` e todo o resto de
  `codegen/testdata/` **não mudam** — qualquer golden que mexa é sinal de que a
  mudança extrapolou o escopo (bom alarme, NFR-13).
- **Fixture, obrigatória no mesmo commit:**
  [wallet/mod.ds:36,43](../../../testdata/projects/wallet/mod.ds) →
  `backend: redis`. Sem isso o job `fixtures` fica vermelho entre commits.
- **Testes que quebram e precisam mudar:**
  [redis_provider_wiring_test.go:40,200](../../../codegen/redis_provider_wiring_test.go)
  (fontes `.ds` inline) e, sobretudo, **:95**, um assert explícito
  `lit.Kind != token.STRING` que precisa virar `*ast.Ident`;
  [anchor_fixture_test.go:177,181](../../../codegen/anchor_fixture_test.go)
  (`anchorCatalogModDs`);
  [provider_registry_test.go:35,38,88,91,129,132](../../../codegen/provider_registry_test.go),
  que montam `ast.ConfigEntry` na mão com `&ast.Literal{Kind: token.STRING}` e
  passam a `&ast.Ident{Name: ...}`.
- **Não mexer:** [pizzeria/sales/mod.ds](../../../testdata/projects/pizzeria/sales/mod.ds)
  já está na forma da spec (o ganho é remover o bloqueio latente, não
  desbloquear pizzeria — ela segue em `KNOWN_UNGENERATABLE` por outros
  defeitos); [docs/examples/07-infra-e-topologia/mod.ds:42,52](../../examples/07-infra-e-topologia/mod.ds)
  idem, e nenhum job de CI o valida ([ci.yml:41-46](../../../.github/workflows/ci.yml)).
- **CI:** job `test` (goldens + unit) e job `fixtures` (`dsc check` + `dsc gen`
  + `go mod tidy` + `go build`/`go vet` sobre `testdata/projects/*`). O wallet
  passa a exercitar a forma nova de ponta a ponta, incluindo o `go-redis` sendo
  puxado — é a prova real de que as quatro leituras ficaram coerentes.
- **Comentários de doc** que ainda citam `backend: "redis"`
  ([decl_query_cache.go:296,395](../../../codegen/decl_query_cache.go),
  [ratelimit.go:213,636](../../../codegen/ratelimit.go),
  [provider_registry.go:74](../../../codegen/provider_registry.go),
  [decl_query_test.go:123](../../../codegen/decl_query_test.go),
  [infra_providers_determinism_test.go:29](../../../codegen/infra_providers_determinism_test.go),
  [redis_cache_test.go:474](../../../codegen/redis_cache_test.go)): cosméticos.
  **Cuidado com** `codegen/redisrt/cache.go.txt:25`, `ratelimit.go.txt:22` e
  `open.go.txt:19` — os `.txt` são copiados verbatim para o projeto gerado,
  então editar seus comentários **muda bytes do projeto gerado** (nenhum golden
  os cobre, mas quebra a idempotência byte-a-byte contra árvores já geradas na
  primeira regeneração, NFR-13). `redisrt/doc.go:6` não é copiado.

## Bloqueios

**Nenhum para a correção da forma** — a §13 é inequívoca e o front-end já a
aceita. Mas a correção **expõe, sem resolver**, uma segunda divergência que só
a spec da linguagem pode decidir, e a task de correção não deve tentar resolver:

- **Qual valor de `Cache { backend: }` seleciona um backend distribuído real?**
  A [§16](../steerings/domainscript-spec-v7/16-cache.md) e o glossário
  ([§26:26](../steerings/domainscript-spec-v7/26-glossary.md)) enumeram
  exatamente três — `memory`, `distributed`, `layered`. **`redis` não está
  entre eles**: a §13 usa `redis` como `layers[].type` dentro do `layered`, e
  como `backend` do **RateLimit** (onde é legítimo e atestado). O gerador,
  porém, seleciona o adapter Redis por `cacheProviders["redis"]`
  ([provider_registry.go:79-81](../../../codegen/provider_registry.go)) — valor
  que a spec nunca descreve para `Cache`; [gaps.md:115](../specs/codegen/gaps.md)
  chega a citar a spec errado ("`memory`/`redis`/`layered`"). A
  [§21.1:12](../steerings/domainscript-spec-v7/21-deploy.md) só diz que um
  Cache/RateLimit "external" vira "Redis/Memcached" no deploy, sem dizer qual
  valor de `backend` escolhe qual. **A spec precisa decidir uma de:** (a)
  acrescentar `redis` (e afins) ao catálogo de backends de Cache da §16; ou (b)
  manter os três e definir como `distributed` resolve o produto concreto (por
  `connection:`? por um `provider:` próprio? por `layers[].type`?). Até lá, a
  correção mantém `cacheProviders["redis"]` intocado — status quo, não uma
  divergência nova — e o wallet passa a escrever `Cache { backend: redis }`:
  forma correta, valor ainda não catalogado. **Não renomear para `distributed`
  por conta própria** — seria inventar a semântica que a spec não deu.
- Menor, sem decisão de spec e fora do escopo: `Cache { backend: layered }` — a
  forma literal da §13 — cai silenciosamente no in-memory; a spec não diz se um
  backend declarado e não implementado é erro, warning ou degradação silenciosa
  ([gaps.md:115,147](../specs/codegen/gaps.md)).
Nota do desenvolvedor: distributed quer dizer cache externo, no caso será o cache definido pelo module, se o module define como redis será redis, se o module define como memcached será memcached e por ai vai

## Fatiamento sugerido

1. **T1 — `backend:` lido como identificador nu (atômica, obrigatoriamente
   completa).** Trocar `configStringLitEntry` por `configIdentEntry` nas quatro
   leituras, migrar a fixture do wallet e ajustar os testes que embutem a
   grafia com aspas. Precisa ser um único commit: qualquer subconjunto deixa a
   CI vermelha ou gera projeto que não compila. Verificação alvo:
   `go test ./codegen/ -run TestCacheModuleBlockAcceptsEnvConnection`.
   `target_files`: `codegen/decl_query_cache.go`, `codegen/ratelimit.go`,
   `codegen/provider_registry.go`, `testdata/projects/wallet/mod.ds`,
   `codegen/provider_registry_test.go`, `codegen/redis_provider_wiring_test.go`,
   `codegen/anchor_fixture_test.go`.
2. **T2 — par positivo/negativo fixando a forma (NFR-4).** Hoje nenhum teste
   ancora *qual* grafia é a exigida, então a próxima refatoração reintroduz o
   `configStringLitEntry` sem alarme. Positivo: `Cache`/`RateLimit
   { backend: redis }` nu selecionando `redisruntime` + a dep em `go.mod`;
   segundo positivo: `backend: memory` (a forma do pizzeria) gerando in-memory
   **sem erro**; negativo: `backend: "redis"` produzindo erro de geração com a
   mensagem de identificador nu. `target_files`:
   `codegen/redis_provider_wiring_test.go`, `codegen/provider_registry_test.go`.
3. **T3 — comentários de doc alinhados à grafia da spec.** Inclui a decisão
   explícita sobre `codegen/redisrt/*.txt` (aceitar a mudança de bytes no
   projeto gerado, ou deixá-los fora e documentar por quê). `target_files`:
   `codegen/decl_query_cache.go`, `codegen/ratelimit.go`,
   `codegen/provider_registry.go`, `codegen/redisrt/doc.go`,
   `codegen/redisrt/cache.go.txt`, `codegen/redisrt/ratelimit.go.txt`,
   `codegen/redisrt/open.go.txt`.
4. **T4 — issue de revisão de spec: catálogo de `Cache.backend`** (independente
   das anteriores, via `issue-generator`): a §16/§26 enumeram
   `memory`/`distributed`/`layered` e o gerador seleciona por `redis`; corrigir
   de passagem a citação errada em `gaps.md:115`. `target_files`:
   `docs/sdd/issues/<nova-issue>.md`, `docs/sdd/issues/open-issues.md`.
