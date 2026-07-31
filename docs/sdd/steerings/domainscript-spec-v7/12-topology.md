# 12. Topologia (`topology.ds`)

Services agrupam módulos. Um service = monólito (implícito). Múltiplos = microsserviços. Canais entre módulos do mesmo service = monólito modular.

```ds
Topology {
    services {
        CarteiraService { modules: [Carteira] }
        NotificacoesService { modules: [Notificacoes] }
        PagamentosService { modules: [Pagamentos] }
    }
    channels {
        Carteira -> Notificacoes {
            via: queue
            provider: "rabbitmq"
            connection: env("RABBITMQ_URL")
            orderBy: aggregateId        // partição por stream; ver §4.2.3
            workers { concurrency: 5, maxRate: 100, batchSize: 10 }
        }
        Carteira -> Pagamentos {
            via: grpc
            connection: env("PAGAMENTOS_GRPC_URL")
            timeout: 10s
            circuitBreaker: { threshold: 5, cooldown: 30s }
        }
    }
}
```

Canais: `direct` (default), `queue`, `grpc`, `http`, `stream`. Mudar topologia não altera domínio; compilador revalida (cross-service sem Saga → erro). Tenant e trace context propagados automaticamente.

O payload publicado é **envelope + payload declarado** ([§4.2.3](04-domain-core.md), [§5.3.1](05-application-layer.md)). `orderBy: aggregateId` particiona por stream e por isso não se aplica a `PublicApplicationEvent`, que não tem chave de partição ([§5.3](05-application-layer.md)); canal que só carrega esse tráfego e declara `orderBy` → ⚠️ warning (inerte).

