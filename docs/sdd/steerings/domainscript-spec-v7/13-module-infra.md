# 13. Infraestrutura de Módulo (`mod.ds`)

Configuração completa de um módulo — cada bloco alimenta uma feature da linguagem e a geração de deploy (seção 21):

```ds
Module Carteira {
    timeout 30s

    Database WalletDb {
        provider: "Postgres"
        connection: env("DB_URL")
        supportsXA: true
        manages: [Wallet]
        retry: { attempts: 3, backoff: "exponential" }
        circuitBreaker: { threshold: 5, cooldown: 30s }
        tenancy: { strategy: row_level, column: "tenant_id" }
    }

    FileStorage DocumentStorage {
        provider: "s3"
        bucket: env("DOCUMENTS_BUCKET")
        region: env("AWS_REGION")
    }

    Idempotency {
        storage: same
        window: 24h
        required: true
        concurrentRetry: wait
    }

    Cache {
        backend: layered
        layers: [
            { type: memory, maxSize: 100MB, ttl: 30s },
            { type: redis, connection: env("REDIS_URL"), ttl: 5min }
        ]
        defaultTtl: 1min
        stampedeProtection: true
    }

    RateLimit {
        backend: redis
        connection: env("REDIS_URL")
        algorithm: token_bucket
        onBackendFailure: open
    }

    Outbox {
        pollInterval: 1s
        batchSize: 50
        concurrency: 3
    }

    Telemetry {
        exporter: "otlp"
        endpoint: env("OTEL_EXPORTER_ENDPOINT")
        traces { sampler: "parentbased_traceidratio", sampleRate: 0.1 }
        metrics { interval: 30s }
        logs { level: "info", format: "json" }
    }
}
```

