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
