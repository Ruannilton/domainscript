# 21. Geração de Artefatos de Deploy

O compilador conhece toda a topologia e infraestrutura — a geração de deploy é derivada, não declarada. Suporte inicial: **Dockerfile** e **docker-compose**.

## 21.1. Fontes de Inferência

| Fonte | Informação derivada |
|-------|---------------------|
| `topology.ds` — services | Containers de aplicação (um binário Go por service) |
| `topology.ds` — channels | Message brokers (RabbitMQ, Kafka) |
| `mod.ds` — Database | Bancos (Postgres, Mongo) com healthchecks |
| `mod.ds` — Cache / RateLimit / Idempotency external | Redis/Memcached |
| `mod.ds` — FileStorage | MinIO (dev) / S3 (prod) |
| `mod.ds` — Telemetry | OpenTelemetry Collector |
| `interface.ds` | Portas expostas |

## 21.2. Dockerfile (um por service)

Multi-stage build, usuário não-root. Service worker-only (sem `interface.ds`) não expõe porta. FFI com dependência C → compilador habilita CGO e dependências no estágio de build.

```dockerfile
# Gerado: docker/carteira-service.Dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/carteira-service ./cmd/carteira-service

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=build /bin/carteira-service /bin/carteira-service
USER appuser
EXPOSE 8080
ENTRYPOINT ["/bin/carteira-service"]
```

## 21.3. docker-compose

Services da aplicação + infraestrutura inferida, com `depends_on` ordenado por healthcheck:

```yaml
services:
  carteira-service:
    build: { context: ., dockerfile: docker/carteira-service.Dockerfile }
    ports: ["8080:8080"]
    environment:
      DB_URL: postgres://user:pass@carteira-db:5432/carteira
      REDIS_URL: redis://cache:6379
      RABBITMQ_URL: amqp://rabbitmq:5672
      OTEL_EXPORTER_ENDPOINT: http://otel-collector:4317
    depends_on:
      carteira-db: { condition: service_healthy }
      rabbitmq: { condition: service_healthy }

  carteira-db:
    image: postgres:16-alpine
    volumes: [carteira-db-data:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U user"]
      interval: 5s
      retries: 5

  cache:
    image: redis:7-alpine

  rabbitmq:
    image: rabbitmq:3-management-alpine
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "ping"]

  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    volumes: [./docker/otel-collector-config.yaml:/etc/otel-collector-config.yaml]

volumes:
  carteira-db-data:
```

## 21.4. Regras de Geração

| Regra | Comportamento |
|-------|---------------|
| Deduplicação de infra | Por connection string: mesma URL = container compartilhado |
| Perfis | `--profile=dev` (tudo local: MinIO, ElasticMQ, containers de banco) / `--profile=prod` (referências externas) |
| Provider cloud sem equivalente local (dev) | ⚠️ Warning |
| Variáveis de ambiente | `.env.example` gerado de todos os `env(...)` do código, com defaults dev apontando para os containers |
| Configs auxiliares | OTEL Collector config (do bloco `Telemetry`), migrations SQL (do schema dos aggregates: state, snapshot, event store, outbox, idempotency) |
| Worker-only service | Sem `ports`/`EXPOSE` |

## 21.5. Comando e Estrutura de Saída

```
ds build --target=docker-compose --profile=dev
```

```
build/
├── cmd/<service>/main.go
├── docker/<service>.Dockerfile
├── docker/otel-collector-config.yaml
├── migrations/<module>/001_init.sql
├── docker-compose.yml
├── .env.example
└── go.mod
```

Resultado: `docker compose up` sobe o sistema completo — aplicação, bancos, filas, cache e observabilidade — sem o dev escrever uma linha de YAML.

