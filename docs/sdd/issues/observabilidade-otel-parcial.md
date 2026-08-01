# Observabilidade OTel parcial (métricas e logs não exportados) (ex-ISSUE-5)
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: [gaps.md §G-6](../specs/codegen/gaps.md) (Observabilidade OTel
  parcial, Marcos H2/H3)
- DESCRIPTION: Traces OTel reais e opt-in via `Telemetry` (H2) funcionam, mas o
  adapter **não exporta métricas nem logs OTel**: `Metric` vive num registry
  in-memory próprio ([`rtsrc/metrics.go.txt`](../../../codegen/rtsrc/metrics.go.txt),
  H3) e logs são `slog` com trace id, não OTLP. Documentado no cabeçalho de
  [decl_telemetry.go](../../../codegen/decl_telemetry.go). O spec (§21/§1.1,
  hoje
  [`20-observability.md`](../steerings/domainscript-spec-v7/20-observability.md)/
  [`01-overview.md`](../steerings/domainscript-spec-v7/01-overview.md))
  promete "instrumentação OpenTelemetry automática" para os três sinais.
  Oportunista: fechar quando telemetria for tocada de novo.
- SOLVED: FALSE

# Solução proposta

## Veredito

Real, e mais larga do que o título sugere. Verificado hoje contra a árvore em
`main` (`go build ./...` passa; os arquivos citados abaixo foram lidos, os
números de linha conferem):

- [`codegen/decl_telemetry.go:44-51`](../../../codegen/decl_telemetry.go) — `logs { ... }` e
  `metrics { ... }` do bloco `Telemetry` "são aceitos pelo parser mas
  IGNORADOS por este gerador". Confirmado: `resolveTelemetryPlan`
  (linhas 144-185) só lê `exporter`, `endpoint` e `traces{sampler,sampleRate}`;
  `telemetryPlan` (107-116) não tem campo algum para os outros dois blocos.
- [`codegen/decl_telemetry.go:283-304`](../../../codegen/decl_telemetry.go) (`emitOTelWiring`) — o único
  efeito de `Telemetry` em `cmd/<service>/main.go` é
  `otelruntime.NewObserver(...)` + `runtime.SetObserver(...)`. Visível
  literalmente no golden
  [`codegen/testdata/cmd_telemetrydemo_main.go.golden:19-33`](../../../codegen/testdata/cmd_telemetrydemo_main.go.golden).
- [`codegen/otelrt/observer.go.txt:82-102`](../../../codegen/otelrt/observer.go.txt) (`NewObserver`) — monta
  **só** um `sdktrace.TracerProvider`. Nenhum `MeterProvider`, nenhum
  `LoggerProvider`, nenhum `propagation.TextMapPropagator`.
- [`codegen/project.go:140-144`](../../../codegen/project.go) — as 4 dependências OTel que
  `EmitGoMod` sabe acrescentar são `otel`, `otel/sdk`, `otel/trace` e
  `otlptrace/otlptracehttp`. Todas do sinal *trace*.
- [`codegen/rtsrc/metrics.go.txt:54-85,104-154`](../../../codegen/rtsrc/metrics.go.txt) — `Counter`/`Histogram`
  são structs concretas com `map` interno; a única leitura é
  `Value`/`Snapshot`, "para que um teste verifique o que um Metric registrou".
  Não há seam de exportação nenhum.
- [`codegen/decl_metric.go:59-73`](../../../codegen/decl_metric.go) — a decisão de não exportar é
  consciente e documentada, não esquecimento.
- Nenhum `slog.SetDefault`, `slog.NewJSONHandler` ou `slog.HandlerOptions` em
  todo o gerador (grep vazio em `codegen/**`): o `log/slog` emitido por
  [`codegen/lower/stmt.go:2315-2340`](../../../codegen/lower/stmt.go) usa o handler default do
  processo — texto, `stderr`, nível `Info` — declare-se `logs { level:
  "info", format: "json" }` ou não.
- Nenhum `traceparent`/`TextMapPropagator` em lugar algum (grep vazio fora de
  comentários). [`codegen/http.go:586`](../../../codegen/http.go) e [`codegen/grpc.go:390,426`](../../../codegen/grpc.go)
  **mintam** um id novo (`runtime.WithTrace(ctx, runtime.NewTraceID())`) em
  toda requisição/RPC de entrada, sem nunca olhar um header/metadata de
  entrada. [`codegen/amqprt/rabbitmq.go.txt`](../../../codegen/amqprt/rabbitmq.go.txt) não escreve nem lê
  nenhum header de trace.

## Inventário: o que a §20 promete × o que existe

| §20 promete | Existe hoje | Onde |
|---|---|---|
| **Traces automáticos para todo construto** | **Parcial.** Span real em 3 pontos: borda HTTP (`UseCase.X`/`Query.X`, [http.go:812,959](../../../codegen/http.go)), borda gRPC ([grpc.go:394,428](../../../codegen/grpc.go)) e cada handler de Policy no Dispatcher núcleo ([rtsrc/dispatcher.go.txt:54](../../../codegen/rtsrc/dispatcher.go.txt)). **Sem span próprio:** Aggregate/Handle, Saga e cada passo, Worker, Channel, Adapter — escopo documentado em [rtsrc/observer.go.txt:15-25](../../../codegen/rtsrc/observer.go.txt) | seam `runtime.Observer` |
| **Propagação cross-service (W3C em grpc/queue/stream/http)** | **Zero.** Cada processo abre um trace raiz próprio; o id de 128 bits de `NewTraceID` tem o *tamanho* de um trace id W3C e nada mais ([rtsrc/observer.go.txt:118-123](../../../codegen/rtsrc/observer.go.txt)). Nem extração na entrada (HTTP header, gRPC metadata, header AMQP) nem injeção na saída (Adapter HTTP em [decl_io.go:296](../../../codegen/decl_io.go), publish AMQP) | — |
| **Métricas automáticas (duration, counters, gauges) por UseCase, Aggregate, Saga, Policy, Worker, Channel, Adapter** | **Zero.** Nenhuma métrica automática de nenhum construto. `runtime.NewCounter`/`NewHistogram` só são instanciados por `Metric` declarado ([decl_metric.go:278,291](../../../codegen/decl_metric.go)); não há tipo *gauge* em lugar nenhum | — |
| **`Metric` de negócio declarativa** | **Funciona, mas não sai do processo.** counter/histogram, `on Evento` (subscriber no Dispatcher, [decl_metric.go:300-359](../../../codegen/decl_metric.go)) e `on Saga.completed` (duração, hook em `decl_saga.go`), `value`/`labels`/`buckets` — tudo em memória, só legível por `Value`/`Snapshot` de dentro do próprio processo. `type gauge` é erro de geração ([decl_metric.go:151-160](../../../codegen/decl_metric.go)) | registry in-memory |
| **Logs automáticos** | **Zero como "por construto".** Os `slog.*` do gerador ([decl_usecase.go:240](../../../codegen/decl_usecase.go), [decl_policy.go:823](../../../codegen/decl_policy.go), [decl_worker.go:649](../../../codegen/decl_worker.go), [decl_io.go:451](../../../codegen/decl_io.go), [decl_query_cache.go:452](../../../codegen/decl_query_cache.go), [ratelimit.go:667](../../../codegen/ratelimit.go), [sql_wiring.go:484](../../../codegen/sql_wiring.go), [usecase_idempotency.go:265](../../../codegen/usecase_idempotency.go)) são avisos pontuais de falha/bypass, não instrumentação de entrada/saída de construto | — |
| **`log` explícito** | **Funciona**, com `trace_id` correlacionado ([lower/stmt.go:2303-2340](../../../codegen/lower/stmt.go)) | `log/slog` |
| **Exportação OTLP dos 3 sinais** | **Só trace.** | [otelrt/observer.go.txt](../../../codegen/otelrt/observer.go.txt) |
| **`Telemetry { metrics { interval } logs { level, format } }`** ([§13](../steerings/domainscript-spec-v7/13-module-infra.md):55-61) | **Config declarada e inerte** — nem lida, nem rejeitada, nem avisada | [decl_telemetry.go:144-185](../../../codegen/decl_telemetry.go) |

Nota de cobertura de CI: **nenhum** projeto em `testdata/projects/` declara
`Telemetry` (grep vazio). Todo o caminho OTel é exercitado só por
`codegen/otel_test.go` (fixture inline `telemetrydemo`), fora do job
`fixtures`.

## Causa raiz

O seam de observabilidade do runtime núcleo foi desenhado para **um** sinal:
`runtime.Observer` tem exatamente dois métodos, `RecordSpan` e `TraceID`
([rtsrc/observer.go.txt:44-57](../../../codegen/rtsrc/observer.go.txt)). Métrica e log nasceram por
caminhos que não passam por ele — `Metric` num registry concreto sem ponto de
extensão, `log` num `slog` sem handler configurável — então "ligar o
exportador" não é adicionar um provider ao `otelrt`, é **abrir seam onde não
existe**, sem deixar OTel vazar para o core (NFR-12).

## Solução proposta

Espelhar, para métrica e log, exatamente o padrão que trace já usa: interface
stdlib-only no `rtsrc`, `Set*` chamado uma vez em `main.go`, implementação
única no `otelrt` opt-in.

1. **`logs { level, format }` deixa de ser inerte — sem dependência nova.**
   `telemetryPlan` ganha `logLevel`/`logFormat`; `resolveTelemetryPlan` os lê
   com `configStringLitEntry`; `emitOTelWiring` (ou uma `emitLogHandlerWiring`
   irmã, chamada no mesmo ponto) emite
   `slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout,
   &slog.HandlerOptions{Level: slog.LevelInfo})))`. É `log/slog` puro: entra
   em `main.go`, não no core, e não acrescenta uma linha a `go.mod`.
2. **`MeterProvider` no `otelrt` + métricas automáticas de graça pelo seam que
   já existe.** `otelrt.Config` ganha `MetricInterval` (de `metrics {
   interval }`); `NewObserver` monta também um `sdkmetric.MeterProvider` com
   `PeriodicReader(otlpmetrichttp, WithInterval(...))`, e o `observer.RecordSpan`
   passa a cronometrar: no callback de fechamento, `duration.Record(elapsed,
   attrs)` + `calls.Add(1, status=ok|error)`. **Nenhuma linha do Go gerado
   muda** — a cobertura das métricas automáticas passa a ser exatamente a
   cobertura de `RecordSpan` (UseCase, Query, Policy), e ampliá-la vira o
   mesmo trabalho de ampliar a cobertura de span.
3. **Seam de exportação para `Metric` de negócio.** `rtsrc/metrics.go.txt`
   ganha `type MetricSink interface { Counter(name string) CounterHandle;
   Histogram(name string, buckets []float64) HistogramHandle }`, um
   `SetMetricSink(s)` e um registro de pacote alimentado por
   `NewCounter`/`NewHistogram` — `SetMetricSink` percorre o registro e liga um
   handle em cada instrumento já criado (as vars de `metrics.go` do módulo são
   `var` de pacote, inicializadas **antes** de `main()`, então bind na
   instalação é obrigatório; consultar o sink dentro de `Add`/`Observe` seria
   um lookup por chamada no hot path). `Add`/`Observe` continuam alimentando o
   `map` in-memory (os testes gerados dependem de `Value`/`Snapshot`) e, quando
   o handle é não-nil, encaminham. `otelrt` implementa o sink sobre o
   `Meter` do item 2 (`Float64Counter`, `Float64Histogram` com
   `WithExplicitBucketBoundaries(buckets)`); `emitOTelWiring` acrescenta uma
   linha `runtime.SetMetricSink(otelSink)`. **`metrics.go` de cada módulo fica
   byte-idêntico** — só `cmd/<service>/main.go` muda, e só quando há
   `Telemetry`.
4. **Logs OTLP.** `otelrt` ganha um `slog.Handler` próprio (~80 linhas) que
   traduz `slog.Record` para o `LoggerProvider` do SDK de logs OTel, encadeado
   com o handler do item 1; `emitOTelWiring` passa a instalar esse handler em
   vez do JSON puro quando `Telemetry` está declarado. Handler próprio em vez
   do bridge `contrib/bridges/otelslog` pela mesma razão já registrada em
   [otelrt/observer.go.txt:5-13](../../../codegen/otelrt/observer.go.txt) para preferir
   `otlptracehttp` a `otlptracegrpc`: manter o grafo de módulos do caminho OTel
   o mais enxuto possível. Decidir por probe real do grafo, como a task H2 fez.
5. **Propagação W3C.** `runtime.Observer` ganha `Extract(ctx, carrier)
   context.Context` e `Inject(ctx, carrier)` sobre um carrier stdlib-only
   (`type TextCarrier interface { Get(string) string; Set(k, v string) }` —
   `http.Header` e `amqp.Table` recebem adaptadores de 5 linhas no `rtsrc`); o
   no-op default devolve `ctx` e não escreve nada. A borda chama
   `runtime.ExtractTrace` **antes** de `WithTrace` ([http.go:586](../../../codegen/http.go),
   [grpc.go:390,426](../../../codegen/grpc.go)); a saída chama `runtime.InjectTrace` no
   `NewRequestWithContext` do Adapter ([decl_io.go:296](../../../codegen/decl_io.go)) e no
   publish AMQP. `otelrt` implementa com `propagation.TraceContext{}` — que
   vem do módulo `go.opentelemetry.io/otel`, **já presente**, então este item
   não acrescenta dependência nenhuma.

## Alternativas descartadas

- **`Counter`/`Histogram` do `rtsrc` viram wrappers diretos do SDK OTel.**
  Mata NFR-12 na raiz (OTel no `go.mod` de todo projeto com `Metric`, mesmo sem
  `Telemetry`) e quebra `Value`/`Snapshot`, de que os testes gerados dependem.
- **`decl_metric.go` emite chamadas a `otelruntime` direto no `metrics.go` do
  módulo.** Torna o conteúdo de `metrics.go` condicional a `Telemetry` — um
  segundo eixo de variação por módulo, churn em todo golden de `metrics.go`, e
  o gerador passa a saber de OTel onde hoje só sabe de `runtime`.
- **Endpoint `/metrics` Prometheus (pull) em vez de OTLP (push).** [§13](../steerings/domainscript-spec-v7/13-module-infra.md)
  declara `exporter: "otlp"` e nenhuma rota `/metrics`; inventaria superfície
  que a spec não descreve.
- **Métricas automáticas alimentando o registry in-memory, sem exportador.**
  Entrega instrumentação invisível: custo no hot path do core, nenhum
  consumidor, gap não fechado.
- **`otelslog` do repo `contrib` como bridge de logs.** Menos código nosso,
  mas puxa um módulo inteiro a mais; só ganha se o probe mostrar árvore
  comparável à do handler próprio.

## Raio de alcance

- **Goldens.** Itens 1-4 tocam só
  [`codegen/testdata/cmd_telemetrydemo_main.go.golden`](../../../codegen/testdata/cmd_telemetrydemo_main.go.golden) (mais linhas dentro do
  bloco de wiring de `Telemetry`). O item 5 é o caro: a linha de extração entra
  em **toda** rota HTTP e **todo** handler gRPC, logo todo
  `codegen/testdata/cmd_*_main.go.golden` (wallet, shop, billing, notes,
  ratelimit, grpcdemo, telemetrydemo) muda — inclusive de projetos **sem**
  `Telemetry`, porque a chamada é incondicional por design (o no-op absorve).
- **`go.mod` do projeto gerado.** Item 2 acrescenta `otel/metric`,
  `otel/sdk/metric`, `otlpmetric/otlpmetrichttp`; item 4 acrescenta
  `otel/log`, `otel/sdk/log`, `otlplog/otlploghttp`. Todas fixas por versão
  (NFR-13, mesma disciplina de [project.go:128-149](../../../codegen/project.go)) e todas
  **atrás de `programNeedsOTel`**. `otelMinGoVersion` pode subir — `EmitGoMod`
  já resolve o máximo. A ordenação por caminho de módulo do bloco `require`
  precisa continuar valendo: os novos módulos entram no meio da lista.
- **Testes que provam NFR-12** — `TestGenerateWalletProjectHasNoOTelArtifacts`
  ([otel_test.go:331](../../../codegen/otel_test.go)) e
  `TestGenerateWalletProjectHasNoMetricsArtifacts`
  ([decl_metric_test.go:428](../../../codegen/decl_metric_test.go)) — têm de continuar verdes sem
  ajuste. Se algum precisar de ajuste, o desenho está errado.
- **NFR-13.** Nada aqui introduz iteração de mapa nem timestamp na saída; o
  risco é só a ordenação do `require`.
- **CI.** O job `fixtures` não cobre nada disto (nenhum `testdata/projects/*`
  declara `Telemetry`). A cobertura continua sendo
  `TestGenerateTelemetryDemoSmokeCompile`, cujo `go build` passa a baixar o SDK
  de métricas e o de logs — tempo de teste do pacote `codegen` sobe. Criar um
  fixture com `Telemetry` para o job `fixtures` é uma decisão à parte (mesmo
  custo, em outro job).

## Bloqueios

Precisam de decisão da spec **antes** dos itens 1 e 2 (os itens 3 e 5 não
dependem de nenhuma):

1. **"gauges" ([§20](../steerings/domainscript-spec-v7/20-observability.md):4) não existe em lugar nenhum da
   linguagem.** `Metric` só documenta `counter` e `histogram`
   ([§20](../steerings/domainscript-spec-v7/20-observability.md):9-21). A spec precisa dizer se `type gauge`
   é grafia válida de `Metric`, ou se "gauges" se refere só a métricas
   automáticas — e nesse caso, qual construto produz um gauge (fila de
   Channel? inflight de Worker?).
2. **[§20](../steerings/domainscript-spec-v7/20-observability.md) não nomeia as métricas automáticas.** Sem
   nome, unidade e conjunto de labels definidos, qualquer escolha do gerador é
   invenção — o que a regra "nada que a spec não descreve" proíbe. A spec
   precisa fixar o esquema (ex.: `<construto>.duration` em segundos, labels
   `name`/`status`) **ou** declarar explicitamente que a nomeação é detalhe de
   implementação do back-end.
3. **[§13](../steerings/domainscript-spec-v7/13-module-infra.md):59-60 mostra `metrics { interval: 30s }` e
   `logs { level: "info", format: "json" }` sem enumerar os valores válidos.**
   Quais níveis (`debug|info|warn|error`)? Quais formatos (`json|text`)?
   `interval` aceita qualquer DURATION? Sem isso o gerador ou aceita tudo (e
   falha em runtime) ou inventa a enumeração.

Não bloqueia, mas condiciona: [§5.3](../steerings/domainscript-spec-v7/05-application-layer.md):165 (revisão de
2026-07-31) diz que `Metric M ... on X` consome `ApplicationEvent`, e
`ApplicationEvent` **não existe** na implementação (grep zero em `ast/`,
`parser/`, `sema/`, `codegen/`). Isso vai mudar `resolveMetricOn`
([decl_metric.go:114-140](../../../codegen/decl_metric.go)) quando entrar — motivo a mais para
esta issue não mexer no **gatilho** de `Metric`, só na **exportação** do valor.

## Fatiamento sugerido

| # | Task | O que entrega | `target_files` |
|---|---|---|---|
| **1** | `logs { level, format }` deixa de ser inerte | `telemetryPlan` ganha `logLevel`/`logFormat`; `main.go` chama `slog.SetDefault` com `JSONHandler`/`TextHandler` no nível declarado. Zero dependência nova. **Depende do bloqueio 3.** | `codegen/decl_telemetry.go`, `codegen/otel_test.go`, `codegen/testdata/cmd_telemetrydemo_main.go.golden` |
| **2** | `MeterProvider` OTLP + `metrics { interval }` | `otelrt.Config.MetricInterval`; `NewObserver` devolve também um `Meter`; `EmitGoMod` ganha os 3 módulos de métrica. Ainda sem instrumento nenhum registrado — só o provider e o shutdown, verificáveis por smoke compile | `codegen/otelrt/observer.go.txt`, `codegen/otelrt/observer_test.go.txt`, `codegen/project.go`, `codegen/decl_telemetry.go`, `codegen/testdata/cmd_telemetrydemo_main.go.golden` |
| **3** | `MetricSink` + `Metric` de negócio exportada | Seam no `rtsrc` + registro + bind em `SetMetricSink`; implementação no `otelrt` sobre o `Meter` da task 2; `runtime.SetMetricSink` no `main.go`. Goldens de `metrics.go` de módulo **inalterados** | `codegen/rtsrc/metrics.go.txt`, `codegen/rtsrc/metrics_test.go.txt`, `codegen/otelrt/observer.go.txt`, `codegen/decl_telemetry.go`, `codegen/decl_metric_test.go`, `codegen/testdata/cmd_telemetrydemo_main.go.golden` |
| **4** | Métricas automáticas de duração/erro via `RecordSpan` | `observer.RecordSpan` cronometra e registra duration+calls com o `Meter` da task 2. Nenhuma linha de Go gerado muda. **Depende dos bloqueios 1 e 2.** | `codegen/otelrt/observer.go.txt`, `codegen/otelrt/observer_test.go.txt`, `codegen/otel_test.go` |
| **5** | Propagação W3C ponta a ponta | `Observer.Extract`/`Inject` + `TextCarrier` no `rtsrc`; extração em HTTP/gRPC/AMQP de entrada, injeção em Adapter HTTP e publish AMQP; `propagation.TraceContext` no `otelrt` (sem dependência nova). **Churn largo de goldens** | `codegen/rtsrc/observer.go.txt`, `codegen/otelrt/observer.go.txt`, `codegen/http.go`, `codegen/grpc.go`, `codegen/decl_io.go`, `codegen/channel_rabbitmq.go`, `codegen/amqprt/rabbitmq.go.txt`, todos os `codegen/testdata/cmd_*_main.go.golden` |

Logs OTLP (item 4 da solução) fica **fora** deste fatiamento de propósito: os
módulos `otel/log`/`sdk/log`/`otlploghttp` vivem em `v0.x` com skew próprio em
relação a `otelVersion = v1.44.0` ([project.go:144](../../../codegen/project.go)) e exigem um
probe de compatibilidade real antes de fixar versão — mesma disciplina da task
H2. Vira uma task 6 depois do probe. Ampliar a cobertura de **span** para
Aggregate/Saga/Worker/Channel/Adapter (a outra metade do gap de trace) também
fica fora: é churn em 5+ `decl_*.go` com goldens próprios, e merece entrada de
gap separada — a task 4 acima faz a cobertura de métrica automática herdar,
automaticamente, o que quer que a cobertura de span venha a ser.
