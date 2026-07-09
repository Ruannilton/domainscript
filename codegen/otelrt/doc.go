// Package otelrt embute o código-fonte do adapter de observabilidade OTel
// opt-in (H2, REQ-30.2, §design codegen 3.13): mesmo padrão de codegen/sqlrt
// (G1) e codegen/grpcrt (H1) — arquivos .go.txt via //go:embed, para não
// compilar junto do compilador — mas para a dependência
// go.opentelemetry.io/otel{,/sdk,/trace} e o exporter OTLP-sobre-HTTP em vez
// de database/sql ou google.golang.org/grpc: é o único lugar do gerado que
// importa o SDK do OTel diretamente, mantendo o núcleo (runtime/,
// codegen/rtsrc) sem nenhuma dependência externa (NFR-12). O pacote gerado a
// partir daqui chama-se "otelruntime" (ver a doc de cada .go.txt) — nome
// deliberadamente distinto de "runtime" (o pacote core), "sqlruntime" (G1) e
// "grpcedge" (H1), para deixar o import claro em quem consome. Emitido só
// quando ao menos um mod.ds declara "Telemetry { ... }" (ver
// codegen/decl_telemetry.go, programNeedsOTel); ausente em qualquer outro
// caso.
package otelrt
