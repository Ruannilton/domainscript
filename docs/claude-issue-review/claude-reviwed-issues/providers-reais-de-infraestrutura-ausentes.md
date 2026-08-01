CODIGO: providers-reais-de-infraestrutura-ausentes
CATEGORIA: Dependente de decisão do desenvolvedor

Issue original: [[docs/sdd/issues/providers-reais-de-infraestrutura-ausentes]]

## Resumo da issue

O projeto gerado só é implantável de verdade contra sqlite; as demais
categorias de infraestrutura (outros bancos, canais gRPC/HTTP/stream, cache e
rate limit distribuídos, storage externo de idempotência) ficam atrás de
rótulos decorativos ou seams sem implementação. O Marco J já fechou um recorte
de 5 providers reais (Postgres, RabbitMQ, Redis para Cache+RateLimit, S3,
Outbox durável), todos opt-in e cobertos por golden + smoke compile. O que
sobra é: um defeito de isolamento não registrado (vazamento de dependência),
um catálogo de rótulos incompleto/silencioso, e uma exigência nova da spec
([[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.7]], mapeamento
de storage por identidade) que ainda não tem decisão de design.

## Evidencias

- **Defeito novo e mais grave do resíduo — vazamento de pgx (quebra NFR-12).**
  `generateSQLRuntimeFiles()` copia **todos** os `*.go.txt` de
  `sqlrt.Sources()` sempre que qualquer provider SQL está ativo
  (`codegen/sql_wiring.go:212-228`); `sqlruntime/open_postgres.go` importa
  `github.com/jackc/pgx/v5/stdlib` (`codegen/sqlrt/open_postgres.go.txt:16`),
  mas `EmitGoMod` só emite `require` para providers **ativos**
  (`codegen/project.go:226`). Um projeto que declare só `provider: "sqlite"`
  emite um arquivo que importa pgx sem `require` correspondente — `go build`
  puro quebra; o `go mod tidy` do job `fixtures` mascara isso puxando uma
  dependência que o programa nunca declarou.
- **Rótulo desconhecido cai em silêncio.** `provider: "mongodb"` (usado em
  `testdata/projects/pizzeria/kitchen/mod.ds:10`) faz
  `programNeedsSQLAdapter` devolver `false` e o projeto inteiro cair em
  memória, sem diagnóstico (`codegen/sql_wiring.go:113-115`). Mesmo padrão em
  FileStorage (`codegen/decl_filestorage.go:84`).
- **Idempotency `storage: external`** é aceito e descartado sem ruído: o
  bloco `Idempotency` do `mod.ds` nunca é lido pelo codegen — só
  `uc.Idempotency` do `UseCase` (`codegen/codegen.go:619`,
  `codegen/usecase_idempotency.go:241`).
- **Cache Redis selecionado por rótulo que a spec não enumera**:
  `codegen/decl_query_cache.go:309-317,412-425` casa `backend == "redis"`;
  [[docs/sdd/steerings/domainscript-spec-v7/16-cache|§16]] só lista
  `memory`/`distributed`/`layered`.

## Impacto no projeto

O risco dominante é perda silenciosa de dado em produção: um `mod.ds` que
declara `provider: "mongodb"` (parece deliberadamente configurado) gera um
serviço que roda "no ar" persistindo em memória, sem qualquer aviso — o mesmo
perfil de risco de outras issues de severidade alta do projeto, e mais
provável de acontecer porque nada sinaliza o engano. Separadamente, o
vazamento de pgx é uma quebra silenciosa da promessa de dependências opt-in
(NFR-12): um projeto puramente sqlite passa a depender implicitamente de
Postgres sem que o programa tenha declarado isso. Por fim, o adapter Redis
para Cache/RateLimit já existe e funciona, mas é **inalcançável a partir de
uma fonte conforme à spec**, porque o rótulo que o gerador espera não é o
rótulo que a spec documenta.

## Soluçoes possíveis

### Solucão 1

Fechar primeiro o que não depende de nenhuma decisão de spec (prioridade P0 da
análise): (a) filtrar as fontes de `sqlrt` por provider ativo — mesma mecânica
que `generateProviderRuntimeFiles` já usa por `adapterDir`
(`codegen/provider_runtime.go:37-48`) — fechando o vazamento de pgx; isso
exige primeiro separar a interface `Dialect` da implementação `sqliteDialect`
em `codegen/sqlrt/dialect.go.txt` (hoje no mesmo arquivo); (b) emitir um
warning de geração (via o `*diag.DiagnosticBag` que `Generate` já recebe e
nunca usa para isso) para todo rótulo de provider não reconhecido em Database,
FileStorage, Cache, RateLimit e `Idempotency.storage`. Warning em vez de erro
como primeiro passo, para não quebrar `dsc gen pizzeria` por um motivo novo.
Trade-off: resolve o risco de maior severidade e o defeito de NFR-12 sem
esperar nenhuma decisão de spec, mas não desbloqueia o adapter Redis nem
adiciona nenhum provider novo.

### Solução 2

Avançar direto para alinhar o seletor de Cache/RateLimit ao catálogo da
[[docs/sdd/steerings/domainscript-spec-v7/16-cache|§16]] (prioridade P1) —
maior desbloqueio por unidade de trabalho, porque o driver Redis já existe e
só o rótulo de seleção está errado. Trade-off: bloqueado por uma decisão de
spec ainda parcialmente em aberto (ver abaixo); implementar antes da decisão
arrisca reescrever o seletor duas vezes.

## O que precisa ser resolvido antes

Uma pergunta já tem resposta parcial via nota do desenvolvedor encontrada na
issue irmã [[docs/sdd/issues/cache-ratelimit-backend-exige-string-contra-spec]]:
"distributed quer dizer cache externo, no caso será o cache definido pelo
module, se o module define como redis será redis, se o module define como
memcached será memcached e por ai vai" — isso resolve o rótulo de `Cache` (P1
do lado Cache), mas ainda precisa virar texto normativo na
[[docs/sdd/steerings/domainscript-spec-v7/16-cache|§16]]. Também há uma nota
do desenvolvedor específica desta issue sobre a direção de longo prazo dos
adapters SQL: "vamos utilizar o GORM internamente para diminuir a complexidade
de desenvolvimento e ao mesmo tempo ter suporte a vários bancos de dados" —
relevante para P4 (adicionar MySQL/SQL Server, hoje adiado), mas não resolve
nenhum dos bloqueios abaixo por si só.

Perguntas que continuam sem resposta:

1. O mesmo catálogo de `backend:` distribuído vale para `RateLimit`, ou
   [[docs/sdd/steerings/domainscript-spec-v7/17-rate-limiting|§17]] (que não
   enumera backends em lugar nenhum, só um exemplo com `backend: redis`)
   segue regra própria?
2. Para a exigência nova de
   [[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.7]] (mapeamento
   de storage por identidade: `uuid` nativo quando o provider suporta, senão
   `char(36)`; `integer` com sequence quando `generation: system`) — quais
   capacidades definem um provider "apto" (`uuid` nativo? sequence
   monotônica?), e o que acontece quando o provider declarado não as tem: erro
   de compilação, erro de geração, ou degradação silenciosa? Sem isso, todo
   dialeto `Dialect` novo nasce com data de reabertura.
3. [[docs/sdd/steerings/domainscript-spec-v7/15-idempotency|§15]] diz
   "`external` (Redis/Dynamo)" em prosa, mas não define qual chave do
   `mod.ds` escolhe entre os dois — decisão necessária antes de implementar
   `Idempotency.storage: external`.

Enquanto essas perguntas seguem abertas, a Solução 1 (P0: filtro de `sqlrt` por
provider ativo + warning de rótulo desconhecido) pode avançar imediatamente,
sem esperar nenhuma decisão — é trabalho de código puro, sem ambiguidade de
spec envolvida.
