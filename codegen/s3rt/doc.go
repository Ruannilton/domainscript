// Package s3rt embute o código-fonte do adapter de FileStorage opt-in sobre
// S3 (Marco J, task J5.1, REQ-45.1/45.2, §design infra-providers 3.5): mesmo
// padrão de codegen/sqlrt/codegen/redisrt/codegen/amqprt (arquivos .go.txt
// via //go:embed, para não compilar junto do compilador), num pacote
// SEPARADO só emitido quando o programa realmente declara `FileStorage {
// provider: "s3" }` (J5.2) — é o único lugar do gerado que importa o driver
// concreto (github.com/aws/aws-sdk-go-v2/...), mantendo o núcleo (runtime/,
// codegen/rtsrc) sem nenhuma dependência externa (NFR-12). O pacote gerado a
// partir daqui chama-se "s3runtime" (ver a doc de cada .go.txt) — mesma
// convenção de "sqlruntime"/"amqpruntime"/"redisruntime": nome
// deliberadamente distinto de "runtime" (o pacote core) para deixar o
// import claro em quem consome.
package s3rt
