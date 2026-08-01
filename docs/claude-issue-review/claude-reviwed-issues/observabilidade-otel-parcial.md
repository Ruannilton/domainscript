CODIGO: observabilidade-otel-parcial
CATEGORIA: Dependente de decisão do desenvolvedor

Issue original: [[docs/sdd/issues/observabilidade-otel-parcial]]

## Resumo da issue

A [[docs/sdd/steerings/domainscript-spec-v7/20-observability|§20]] promete
"instrumentação OpenTelemetry automática" para os três sinais (traces,
métricas, logs), mas só traces são exportados de fato hoje. Métricas
declaradas com `Metric` vivem num registry in-memory próprio, sem exportador;
logs usam `slog` correlacionado por trace id, mas nunca viram OTLP; e não há
métricas automáticas nem propagação de contexto W3C entre serviços. Boa parte
do trabalho de código é implementável direto, mas três pontas de nomenclatura
e enumeração de valores nunca foram normatizadas na spec, e sem elas qualquer
escolha do gerador seria invenção.

## Evidencias

- `codegen/decl_telemetry.go:44-51` confirma no próprio comentário do código:
  `logs { ... }` e `metrics { ... }` "são aceitos pelo parser mas IGNORADOS
  por este gerador"; `resolveTelemetryPlan` (144-185) só lê `exporter`,
  `endpoint` e `traces{sampler,sampleRate}`.
- `codegen/otelrt/observer.go.txt:82-102` (`NewObserver`) monta só um
  `sdktrace.TracerProvider` — nenhum `MeterProvider`, nenhum
  `LoggerProvider`, nenhum `propagation.TextMapPropagator`.
- `codegen/rtsrc/metrics.go.txt:54-85,104-154`: `Counter`/`Histogram` são
  structs concretas sobre `map` interno, sem nenhum seam de exportação; a
  decisão de não exportar é documentada como consciente em
  `codegen/decl_metric.go:59-73`, não esquecimento.
- Grep vazio em todo `codegen/**` para `slog.SetDefault`/`NewJSONHandler` e
  para `traceparent`/`TextMapPropagator` — nenhuma configuração de log
  estruturado nem propagação cross-service existe.
- `codegen/http.go:586` e `codegen/grpc.go:390,426` **mintam** um trace id novo
  em toda requisição de entrada, sem nunca ler um header/metadata existente.
- Tabela completa "§20 promete × o que existe" na
  [[docs/sdd/issues/observabilidade-otel-parcial]] mostra a extensão real:
  zero span próprio em Aggregate/Handle/Saga/Worker/Channel/Adapter; zero
  propagação cross-service; zero métrica automática; `Telemetry { metrics {
  interval } logs { level, format } }`
  ([[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|§13]]) config
  declarada e inerte.
- Nenhum projeto em `testdata/projects/` declara `Telemetry` — todo o caminho
  OTel é exercitado só por `codegen/otel_test.go`, fora do job `fixtures`.

## Impacto no projeto

Um programa que declara `Telemetry { metrics {...} logs {...} }` tem essa
declaração silenciosamente ignorada — nem lida, nem rejeitada, nem avisada —
o que é o mesmo padrão de "controle aceito e inerte" já registrado em outras
issues como problema de segurança/observabilidade enganosa. Em produção, isso
significa nenhuma visibilidade real de métricas/logs via OTel apesar da spec
prometer os três sinais, e nenhuma correlação de trace entre serviços (cada
processo abre uma trace raiz própria), o que inutiliza rastreamento
distribuído em qualquer topologia com mais de um serviço.

## Soluçoes possíveis
### Solucão 1
Seguir o fatiamento de 5 tasks que a análise propõe, espelhando para métrica e
log o padrão que trace já usa (interface stdlib-only no `rtsrc`, `Set*`
chamado uma vez em `main.go`, implementação única no `otelrt` opt-in),
preservando NFR-12 (nenhuma dependência OTel entra em `go.mod` sem
`Telemetry` declarado): (1) `logs{level,format}` deixa de ser inerte via
`slog.SetDefault`, sem dependência nova; (2) `MeterProvider` OTLP no `otelrt`
+ métricas automáticas de graça pelo seam de `RecordSpan` que já existe,
sem mudar nenhuma linha do Go emitido por módulo; (3) `MetricSink` para
exportar `Metric` de negócio, com `metrics.go` de cada módulo permanecendo
byte-idêntico; (4) handler próprio de logs OTLP (não o bridge `contrib`, pela
mesma disciplina de manter o grafo de módulos enxuto que motivou preferir
`otlptracehttp` a `otlptracegrpc`); (5) propagação W3C ponta a ponta via
`Extract`/`Inject` no `runtime.Observer`, injeção incondicional com no-op
default (não requer `Telemetry` declarado). Tasks 1 e 2 dependem dos
bloqueios de nomenclatura listados abaixo; tasks 3 e 5 não dependem de
nenhum bloqueio e podem avançar já.
### Solução 2
Alternativas descartadas pela própria análise, registradas para mostrar por
que a Solução 1 venceu: `Counter`/`Histogram` do `rtsrc` virarem wrappers
diretos do SDK OTel (mataria NFR-12 na raiz, forçando OTel em todo projeto
com `Metric` mesmo sem `Telemetry`, e quebraria `Value`/`Snapshot` de que
testes gerados dependem); endpoint `/metrics` Prometheus por pull em vez de
OTLP por push (a
[[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|§13]] declara
`exporter: "otlp"`, superfície que a spec não descreve); métricas automáticas
alimentando só o registry in-memory sem
exportador (instrumentação invisível, custo no hot path sem consumidor).

## O que precisa ser resolvido antes

- "gauges" ([[docs/sdd/steerings/domainscript-spec-v7/20-observability|§20]])
  não existe em lugar nenhum da linguagem — `Metric` só documenta `counter` e
  `histogram` (§20:9-21). A spec precisa dizer se `type gauge` é grafia
  válida de `Metric`, ou se "gauges" se refere só a métricas automáticas — e
  nesse caso, qual construto produz um gauge (fila de Channel? inflight de
  Worker?).
- [[docs/sdd/steerings/domainscript-spec-v7/20-observability|§20]] não nomeia
  as métricas automáticas: sem nome, unidade e conjunto de labels definidos,
  qualquer escolha do gerador é invenção. A spec precisa fixar o esquema
  (ex.: `<construto>.duration` em segundos, labels `name`/`status`) ou
  declarar explicitamente que a nomeação é detalhe de implementação do
  back-end.
- [[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|§13]]:59-60
  mostra `metrics { interval: 30s }` e `logs { level: "info", format: "json"
  }` sem enumerar valores válidos: quais níveis (`debug|info|warn|error`)?
  quais formatos (`json|text`)? `interval` aceita qualquer `DURATION`? Sem
  isso o gerador ou aceita tudo (falhando só em runtime) ou inventa a
  enumeração.
- Item não bloqueante mas que condiciona o trabalho:
  [[docs/sdd/steerings/domainscript-spec-v7/05-application-layer|§5.3]]
  (revisão de 2026-07-31) diz que `Metric M ... on X` consome
  `ApplicationEvent`, que ainda não existe na implementação (grep zero em
  `ast/`, `parser/`, `sema/`, `codegen/`) — o mesmo `ApplicationEvent` que
  [[docs/sdd/issues/spec-v7-metadata-implicito-de-event]] cobre do lado da
  spec de Event — motivo a mais para esta issue não mexer no gatilho de
  `Metric`, só na exportação do valor, quando essa outra peça entrar.
