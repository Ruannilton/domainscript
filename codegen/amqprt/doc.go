// Package amqprt embute o código-fonte do adapter de canal cross-process
// opt-in sobre RabbitMQ (Marco J, task J3.1, REQ-43.1, §design
// infra-providers 3.3): mesmo padrão de codegen/sqlrt/codegen/grpcrt
// (arquivos .go.txt via //go:embed, para não compilar junto do compilador),
// num pacote SEPARADO só emitido quando o programa realmente declara um
// canal `via: queue provider: "rabbitmq"` — é o único lugar do gerado que
// importa o driver concreto (github.com/rabbitmq/amqp091-go), mantendo o
// núcleo (runtime/, codegen/rtsrc) sem nenhuma dependência externa (NFR-12).
// O pacote gerado a partir daqui chama-se "amqpruntime" (ver a doc de cada
// .go.txt) — mesma convenção de "sqlruntime"/"grpcedge": nome deliberadamente
// distinto de "runtime" (o pacote core) para deixar o import claro em quem
// consome.
package amqprt
