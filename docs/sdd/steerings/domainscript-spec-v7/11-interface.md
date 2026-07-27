# 11. Interface (`interface.ds`)

Exposição via HTTP, gRPC, TCP, UDP. Hospeda tenant resolution, versionamento e rate limit. Domínio não sabe qual protocolo é usado.

```ds
Interface HTTP {
    port: env("HTTP_PORT")
    basePath: "/api"

    versioning {
        strategy: path
        current: v2
        supported: [v1, v2]
    }

    tenant {
        from: subdomain     // ou: header "X-Tenant-Id", jwt_claim "tenant_id", path
    }

    rateLimit {
        perIp: 1000/min
        perUser: 300/min
    }

    POST "/wallets"                       -> CreateWallet
    POST "/wallets/{walletId}/deposit"    -> PerformDeposit {
        rateLimit { perUser: 60/min, burst: 10 }
    }
    GET  "/wallets/{walletId}"            -> GetWalletSummary
    GET  "/wallets/{walletId}/statement"  -> GetStatement

    POST "/login"  -> Login       { tenancy: none, rateLimit: { perIp: 10/min, onBackendFailure: closed } }
    GET  "/health" -> HealthCheck { tenancy: none }
}

Interface GRPC {
    port: env("GRPC_PORT")
    service WalletService {
        rpc Deposit   -> PerformDeposit
        rpc GetWallet -> GetWalletSummary
    }
}
```

Mapeamento automático: path params → `ref`/parâmetro, query params → query string, body → JSON, status codes (`200/201/422/404/429/503`). Compilador gera `.proto` para gRPC.

