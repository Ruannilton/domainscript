# Providers reais de infraestrutura ausentes (G-4) (ex-ISSUE-3)
- SPEC: codegen
- TASK: gaps.md §G-4 (Marcos F/G — providers reais de infraestrutura)
- DESCRIPTION: Tudo está atrás de seams limpos (NFR-12 respeitado), mas a única
  dependência externa real por categoria é sqlite — o sistema gerado hoje
  **não é implantável contra infraestrutura real** além disso. Categorias em
  aberto: Database (spec pede Postgres §12; só `"sqlite"` é adapter real,
  `"postgres"`/`"mongodb"` são rótulos decorativos — [sql_wiring.go](../../../codegen/sql_wiring.go));
  Canais (`grpc`/`http`/`stream` §11 → erro de geração; provider `rabbitmq`
  não existe, só `direct`/`queue` in-memory — [channel_test.go](../../../codegen/channel_test.go));
  Cache backend (`redis`/`layered` §15 → só in-memory); RateLimit backend
  (`redis` §16 → só in-memory); FileStorage (`"s3"` §12 → seam in-memory);
  Idempotency storage (`external` Redis/Dynamo §14 → só `same` in-memory,
  `codegen/rtsrc/idempotency.go.txt`); Outbox (durabilidade real §12 → in-
  memory). Fechar exige um provider real por vez, opt-in e isolado (padrão já
  existe: `codegen/sqlrt/`, `grpcrt/`, `otelrt/`). Postgres ou rabbitmq
  primeiro (validam os seams mais centrais). Nota: o seam `Dialect` (REQ-40,
  read-side/I7.0, `codegen/sqlrt/dialect.go.txt`) já reduz o custo da parte
  SQL — adicionar banco vira "implementar `Dialect` + entrada no registro"; o
  restante (driver real, migrations, type mapping) segue aberto.

  EM ANDAMENTO (spec criada): `../specs/infra-providers/` (Marco J,
  REQ-41..48 / NFR-21..24) tratou esta issue com **recorte de 5 providers** —
  Postgres, RabbitMQ, Redis (Cache+RateLimit), S3 e Outbox durável. As demais
  categorias de G-4 (outros bancos, gRPC-canal, Dynamo para idempotency
  `external`, backend `layered` de cache, GCS/Azure) ficam explicitamente fora
  do recorte, para ciclos futuros.

  FECHADA PARCIALMENTE (Marco J concluído, J7.1): as 5 categorias do recorte
  têm provider real — Postgres (J1, `codegen/pgrt` + [sql_wiring.go](../../../codegen/sql_wiring.go)),
  RabbitMQ (J3, [channel_rabbitmq.go](../../../codegen/channel_rabbitmq.go)), Redis Cache+RateLimit (J4,
  `codegen/redisrt`), S3 FileStorage (J5, `codegen/s3rt`), Outbox durável
  (J2, `runtime.DurableOutbox`/`sql_wiring.go:emitOutboxDatabaseWiring`) —
  todos opt-in, isolados atrás do seam existente, cobertos por golden +
  smoke compile (NFR-17) e determinismo (NFR-21, `infra_providers_
  determinism_test.go`). Ver [gaps.md](../specs/codegen/gaps.md) §G-4 para a
  tabela completa antes/depois por categoria.

  Residual que sobrou do Marco J (rastreado à parte, já resolvido): o lado
  PRODUTOR do Outbox→canal cross-service (REQ-42.6) publicava direto no
  commit em vez de enfileirar no outbox — isso foi fechado pelo Marco K
  (`../specs/correcoes-issues-9-10-11/`, ex-ISSUE-9, RESOLVED nos
  commits `1137ba9`/`e2f3ec9`/`9fd30f0`/`c580e1f`).

  Ainda em aberto (não fechado por nenhum ciclo até agora): a
  vendorização/build offline real (R10) nunca foi implementada — os smoke
  tests usam `go mod tidy` (rede), não `-mod=vendor` genuíno. As categorias
  explicitamente fora do recorte do Marco J (outros bancos, gRPC-canal,
  Dynamo, `layered` cache, GCS/Azure) continuam abertas para um ciclo
  futuro.
- SOLVED: FALSE
