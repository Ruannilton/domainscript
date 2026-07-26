# Exemplo: projeto multi-módulo `shop`

Um projeto com **dois módulos em services diferentes**, ligados por um canal na
topologia. Onde o exemplo [`wallet/`](../wallet) mostra um módulo único e
completo, este foca no **diferencial do DomainScript**: as regras arquiteturais
*cross-file* que só fazem sentido vendo o programa inteiro.

## Estrutura

```
shop/
├── topology.ds            # §11 — services (Sales, Delivery) e o canal entre eles
├── orders/                # módulo Orders (service Sales)
│   ├── mod.ds             # §12 — Module + Database que persiste o Aggregate
│   ├── domain.ds          # §2/§4 — ValueObjects, PublicEvent, Aggregate Order
│   ├── application.ds     # §5 — Command + UseCase
│   └── interface.ds       # §10 — rota HTTP expondo o UseCase
└── shipping/              # módulo Shipping (service Delivery)
    ├── mod.ds             # §12 — Module
    └── policy.ds          # §7 — Policy que reage ao PublicEvent de Orders
```

## O que o exemplo demonstra

A peça central é a comunicação **cross-service**: o módulo `Orders` (no service
`Sales`) publica o `PublicEvent OrderPlaced`; o módulo `Shipping` (no service
`Delivery`) tem a `Policy NotifyShipping` que reage a esse evento. Como os dois
módulos rodam em services diferentes, essa reação cruza a rede — e o compilador
exige um **canal explícito** na topologia para isso:

- **Evento público para reação cross-módulo (REQ-5.8):** a Policy só pode reagir a
  um `PublicEvent`, não a um `Event` privado de outro módulo.
- **Canal obrigatório entre services (REQ-5.11):** sem o `Orders -> Shipping`
  declarado em `channels`, a reação cross-service é um erro.
- **Ordem total em canal de fila (REQ-5.16):** o canal `via: queue` carrega
  `orderBy: id` — sem isso seria um aviso.
- **Aggregate alvo de `manages` (REQ-10):** `Database.manages: [Order]` resolve ao
  Aggregate declarado no módulo.
- **Exposição (REQ-5.23):** o `UseCase PlaceOrder` está exposto numa rota HTTP, por
  isso não há aviso de operação inalcançável.

## Validar

A partir da raiz do repositório:

```sh
go build -o dsc ./cmd/dsc
./dsc docs/examples/shop      # sem saída e exit 0 = válido
```

Remova o canal `Orders -> Shipping` de `topology.ds` e a CLI passa a reportar o
erro cross-service, com posição e mensagem acionável (exit code `1`). Esse mesmo
contraste está fixado por regressão em `driver/shop_regression_test.go`.
