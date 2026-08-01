CODIGO: cache-ratelimit-backend-exige-string-contra-spec
CATEGORIA: Correção de código

Issue original: [[docs/sdd/issues/cache-ratelimit-backend-exige-string-contra-spec]]

## Resumo da issue

A spec escreve o valor de `backend:` em `Cache`/`RateLimit` como identificador
nu (`backend: redis`, `backend: layered`), coerente com o resto do bloco, que só
aspa valores opacos de produto. O gerador, porém, aceita **só** a forma com
aspas e falha em geração na forma que a spec de fato usa. É um defeito de
código puro: o front-end já aceita a sintaxe correta, e nenhuma regra semântica
depende disso — só o leitor de config do back-end pede o tipo errado de token.

## Evidencias

```
$ dsc gen <mod com "Cache { backend: memory }"> -o out
dsc: codegen: módulo M: queries.go: codegen: Query Q:
     mod.ds Cache.backend: backend: esperava um literal string, veio *ast.Ident
```

- `codegen/decl_telemetry.go:207-219` (`configStringLitEntry`) exige
  `*ast.Literal` com `Kind == token.STRING`.
- `codegen/decl_query_cache.go:309` (`cacheBackendKind`) e
  `codegen/ratelimit.go:223` (`rateLimitBackendKind`) chamam esse helper.
- Achado adicional da análise, fora do relato original: mais duas leituras da
  mesma chave em `codegen/provider_registry.go:166,173`
  (`activeProviderDeps`) — se só as duas primeiras forem corrigidas, `backend:
  redis` passa a gerar chamada a `redisruntime.NewRedisQueryCache(...)` sem que
  `redisruntime/` seja copiado nem `github.com/redis/go-redis/v9` entre no
  `go.mod` (projeto gerado não compila).
- Contraste interno já confirmado: `algorithm` (`ratelimit.go:197`),
  `onBackendFailure` (`ratelimit.go:275`) e `concurrentRetry`
  (`usecase_idempotency.go:201`) já usam `configIdentEntry` — `backend` é o
  único enumerado do bloco lido como rótulo opaco.
- `testdata/projects/wallet/mod.ds` usa a forma com aspas (gera, mas diverge da
  spec); `testdata/projects/pizzeria/sales/mod.ds` usa a forma da spec e está
  latentemente quebrado — hoje mascarado porque a geração do pizzeria já falha
  antes por outro motivo.

## Impacto no projeto

Um programa escrito exatamente como a
[[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|§13]] e a
[[docs/sdd/steerings/domainscript-spec-v7/16-cache|§16]] documentam falha na
geração com uma mensagem que soa como erro de configuração do usuário, quando
na verdade é o gerador rejeitando a sintaxe correta. Isso bloqueia qualquer
exemplo/projeto que siga a spec ao pé da letra para Cache/RateLimit com backend
distribuído, e mascara um segundo defeito mais sério (o acoplamento silencioso
de `provider_registry.go` descrito acima), que faria um projeto compilar sem o
driver Redis necessário.

## Soluçoes possíveis

### Solucão 1

Trocar as quatro leituras (`cacheBackendKind`, `rateLimitBackendKind`, e as
duas em `activeProviderDeps`) de `configStringLitEntry` para
`configIdentEntry` — helper que já existe e já é usado pelos outros enumerados
do mesmo bloco. Sem tolerância às duas grafias: a forma com aspas passa a ser
erro de geração (efeito exigido, não um bug a esconder). As quatro leituras
precisam mudar no mesmo commit — corrigir só as duas citadas no relato original
geraria projeto que não compila (o acoplamento silencioso do item acima).

### Solução 2

Aceitar as duas grafias (`Ident` ou `STRING` no mesmo helper). Descartada
explicitamente: o CLAUDE.md do repositório proíbe "aceitar as duas formas" como
solução de compromisso — deixaria metade da superfície não especificada. Uma
variante também descartada seria alargar `configStringLitEntry` para aceitar
`Ident`, o que contaminaria `provider:`/`exporter:`/`sampler:`, aspados de
propósito pela mesma
[[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|§13]].

## O que precisa ser resolvido antes

Nenhuma — a spec já é clara e o front-end já aceita a forma correta; o defeito
é 100% de codegen. A correção deve vir acompanhada, no mesmo commit, da
atualização de `testdata/projects/wallet/mod.ds` (`backend: redis` sem aspas) e
dos testes que hoje montam `ast.Literal{Kind: token.STRING}` à mão
(`codegen/redis_provider_wiring_test.go:95`,
`codegen/provider_registry_test.go`).

Separadamente — e não bloqueando esta correção — a análise identificou uma
segunda pergunta ainda sem resposta normativa: qual valor de `Cache { backend:
}` seleciona um backend distribuído real (a
[[docs/sdd/steerings/domainscript-spec-v7/16-cache|§16]] enumera só
`memory`/`distributed`/`layered`, sem `redis`). Já existe uma nota do
desenvolvedor sobre isso, embutida na seção Bloqueios da própria
[[docs/sdd/issues/cache-ratelimit-backend-exige-string-contra-spec|issue original]]
(não há arquivo separado em `docs/notes/Issues/` para esta decisão) —
"distributed quer dizer cache externo, no caso será
o cache definido pelo module, se o module define como redis será redis, se o
module define como memcached será memcached e por ai vai" —, mas ainda precisa
virar texto normativo na
[[docs/sdd/steerings/domainscript-spec-v7/16-cache|§16]] e ser tratada como
issue própria (é o item T4 do fatiamento sugerido, também discutido em
[[docs/sdd/issues/providers-reais-de-infraestrutura-ausentes]]).
