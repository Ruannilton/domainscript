// Package redisrt embute o código-fonte do adapter de Cache opt-in sobre
// Redis (Marco J, task J4.1, REQ-44.1/44.5, §design infra-providers 3.4):
// mesmo padrão de codegen/sqlrt/codegen/grpcrt/codegen/amqprt (arquivos
// .go.txt via //go:embed, para não compilar junto do compilador), num
// pacote SEPARADO só emitido quando o programa realmente declara
// `Cache { backend: "redis" }` — é o único lugar do gerado que importa o
// driver concreto (github.com/redis/go-redis/v9), mantendo o núcleo
// (runtime/, codegen/rtsrc) sem nenhuma dependência externa (NFR-12). O
// pacote gerado a partir daqui chama-se "redisruntime" (ver a doc de cada
// .go.txt) — mesma convenção de "sqlruntime"/"amqpruntime": nome
// deliberadamente distinto de "runtime" (o pacote core) para deixar o
// import claro em quem consome.
package redisrt
