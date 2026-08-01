# Exemplo: SaaS multi-tenant `pizzeria`

Um projeto com **dois módulos em bounded contexts isolados** (`Sales` e
`Kitchen`) rodando no mesmo monólito (`PizzeriaMonolith`), trocando eventos
por uma fila assíncrona. Modela a especificação do "Desafio SaaS Pizzaria"
(catálogo, pedidos delivery/balcão, KDS da cozinha) exercitando diretivas da
v6.0 que os outros dois exemplos não cobrem: multi-tenancy (`tenant { from:
subdomain }`, `tenancy: { strategy: row_level }`), `Idempotency`/`Cache` de
módulo, `cache` de Query, `idempotency` de UseCase, `visibility` de View,
`RateLimit` por rota e versionamento de API.

## Estrutura

```
pizzeria/
├── topology.ds            # §11 — service único (monólito) + 2 canais queue
├── sales/                 # módulo Sales — catálogo e pedidos (Postgres, decorativo)
│   ├── mod.ds              # §12 — Database (row_level) + Idempotency/Cache/RateLimit
│   ├── domain.ds            # §2/§4 — VOs, MenuItem (StateStored), Order (EventSourced)
│   ├── application.ds       # §5 — Commands + UseCases (PlaceOrder idempotente, ...)
│   ├── policy.ds             # §7 — reage a TicketFinished (Kitchen) e libera o pedido
│   ├── read.ds                # §6 — GetAvailableMenu (cache), GetActiveOrders (visibility)
│   └── interface.ds            # §10 — rotas, tenant, versioning, rate limits
└── kitchen/                # módulo Kitchen — esteira de preparo (MongoDB, decorativo)
    ├── mod.ds               # §12 — Database (row_level), sem vínculo com Sales
    ├── domain.ds             # §4 — KitchenTicket (StateStored): Claim/Finish
    ├── application.ds         # §5 — Commands + UseCases (Claim/Finish)
    ├── policy.ds               # §7 — reage a OrderPaid (Sales) e cria o ticket
    ├── read.ds                  # §6 — GetBoardTickets (FIFO, Pending/Preparing)
    └── interface.ds               # §10 — board/claim/finish
```

## O que o exemplo demonstra

- **Isolamento estrito por coreografia (critério de avaliação #1):** Sales e
  Kitchen nunca fazem `join`/`load` cruzando bancos — `Order` (Sales) e
  `KitchenTicket` (Kitchen) só se falam via `PublicEvent` (`OrderPaid`,
  `TicketFinished`) e `Policy` (REQ-5.8/5.10/5.11). O canal `via: queue` em
  `topology.ds` materializa a fila (RabbitMQ, decorativo) mesmo os dois
  módulos rodando no mesmo service/monólito (§11 permite isso explicitamente).
- **Anti-corrupção na fronteira entre bounded contexts:** nenhum ValueObject
  é compartilhado por nome entre módulos — só `PublicEvent` atravessa (REQ-7.4;
  `ValueObject` nunca é `Public`, ver `symbols/table.go`). `sales/policy.ds` e
  `kitchen/policy.ds` traduzem os valores de **domínio** do outro módulo
  construindo o VO local a partir do valor estrangeiro
  (`TicketItemName(line.name)`, `TicketItemQuantity(line.quantity)`, ...) — o
  checker de compatibilidade (REQ-13) silencia essa conversão porque o
  construtor de um VO-wrapper espera o primitivo subjacente
  (`sema/rules_compat.go`, "quando um dos lados é primitivo, a regra
  silencia"), o mesmo mecanismo que a linguagem já usa para não duplicar o
  diagnóstico da Regra de Ouro do Write Side. É a técnica usada aqui para
  expressar, em DomainScript, a camada anticorrupção que todo Bounded Context
  real precisa na fronteira.
  **A identidade é a exceção deliberada:** ela atravessa como `ref Order`, sem
  wrapper de tradução, porque a
  [§2.7](../../../docs/sdd/steerings/domainscript-spec-v7/02-type-system.md)
  permite `ref T` cross-módulo explicitamente — "carrega identidade, não
  estado". O antigo `ValueObject ExternalOrderRef` era exatamente o
  VO-wrapper-de-id que `ref T` existe para substituir.
- **Padrão Snapshot / Dependência Temporal (critério #3):** `Order` nunca
  referencia `MenuItem` ao vivo. `PlaceOrder` (`sales/application.ds`) copia
  nome, quantidade e preço unitário de cada item para um `OrderLineSnapshot`
  no momento da compra; alterações futuras de preço no cardápio não afetam
  pedidos já feitos.
- **Event Sourcing x State Stored (critério #2):** `MenuItem` e
  `KitchenTicket` são `StateStored` (só o estado atual importa); `Order` é
  `EventSourced` (histórico imutável de status/pagamento). A máquina de
  estados do pedido (`Placed -> Paid -> Preparing -> Ready|Delivered`) é
  validada rigidamente: cada `Handle` faz `ensure state.status == ... else
  InvalidOrderTransition`, então `Placed -> Preparing` direto é impossível.
- **ValueObjects e `ensure` (critério #2):** todo campo de Write Side é VO ou
  Enum (Regra de Ouro, REQ-5.1) — inclusive a quantidade (`Quantity`, composta
  com um campo nomeado `value` para permitir uso em range de `for`, como o
  próprio spec faz em `for i in 1..batch.quantity`). O preço nunca negativo é
  a invariante do próprio `Money` (`Valid { amount >= 0 }`), não uma checagem
  repetida no Handle.
- **Diretivas v6.0 (critério #4):** `tenant { from: subdomain }` (Interface,
  ambas as interfaces resolvem o tenant pelo subdomínio da requisição),
  `tenancy: { strategy: row_level, column: "tenant_id" }` (Database, ambos os
  módulos), `Idempotency`/`Cache`/`RateLimit` de módulo (`sales/mod.ds`),
  `idempotency { required: true, window: 1h }` em `PlaceOrder` (proteção
  contra clique duplo), `cache { ttl: 1h }` em `GetAvailableMenu` com
  invalidação **inferida** (nenhum `invalidateOn` explícito — o compilador já
  varre os `Apply` de `MenuItem` tocados pela Query), `visibility` em
  `OrderVW` (dados do cliente e total só para o dono do pedido ou `staff`),
  `Policy` cross-módulo com `delivery AtLeastOnce`, e `RateLimit` por rota
  (`rateLimit { perUser: 5/min }` em `POST /orders`, `perIp: 200/min` em
  `GET /menu`, `perIp: 60/min` global).

## Adaptações em relação ao enunciado literal (limitações conhecidas do back-end)

Estas substituições foram confirmadas lendo o código do `codegen` antes de
escrever o exemplo — não são tentativas às cegas:

1. **Estratégia de tenancy:** o enunciado pede `schema_per_tenant`.
   `codegen/codegen.go` (`rejectUnsupportedTenancyStrategies`) recusa
   `schema_per_tenant`/`database_per_tenant` na geração — só `row_level` tem
   implementação real (filtro + fail-closed). Os dois `Database` usam
   `tenancy: { strategy: row_level, column: "tenant_id" }`.
2. **Resolução de tenant por subdomínio:** ao contrário do que se poderia
   temer, **não precisou de substituição** — `tenant { from: subdomain }` é
   aceito pelo parser (`parser/parse_interface_test.go`) e tem wiring real no
   codegen (`codegen/http.go`, extrai o primeiro rótulo do `Host`). Usado tal
   como o enunciado pede.
3. **`provider` do Database (atualizado pelo ciclo infra-providers/Marco J,
   REQ-41):** `"postgres"` (Sales) **deixou de ser decorativo** —
   `codegen/sql_wiring.go` reconhece `"sqlite"` **e** `"postgres"` como
   providers reais desde J1.2; se a geração alcançasse o módulo Sales, ela
   puxaria `github.com/jackc/pgx/v5` para o `go.mod` e tentaria abrir uma
   conexão real. Isso não chega a acontecer hoje: a limitação
   UseCase+Policy no mesmo módulo (ver "Gerar o back-end" abaixo) barra
   `dsc gen` antes de qualquer wiring de provider — mas o rótulo em si não
   é mais só decoração, e `sales/mod.ds` não declara `connection: env(...)`
   (só é lida quando o módulo usa 2PC ou Outbox durável — nenhum dos dois é
   o caso aqui). `"mongodb"` (Kitchen) **continua decorativo** — fora do
   recorte de 5 providers do Marco J (só Postgres entre bancos reais; ver
   `.claude/specs/infra-providers/requirements.md`, "Fora de escopo").
4. **Canal `via: queue` (atualizado pelo ciclo infra-providers/Marco J,
   REQ-43):** `"rabbitmq"` **deixou de ser decorativo** —
   `codegen/channel_rabbitmq.go` reconhece `"rabbitmq"` como provider real
   desde J3.1; os dois canais aqui já declaram `connection:
   env("RABBITMQ_URL")` (a mesma chave que o wiring real espera), então SE
   a limitação de "Gerar o back-end" fosse resolvida, este exemplo passaria
   a puxar `github.com/rabbitmq/amqp091-go` e usar o transporte cross-
   process de verdade, em vez do `QueueChannel` in-memory — nenhuma mudança
   no `.ds` seria necessária. `via: grpc/http/stream` continuam erro de
   geração (fora do recorte de Marco J).
5. **`visibility` de View:** o bloco em `OrderVW` (sales/read.ds) é sintaxe
   válida e modela a intenção corretamente (campos sensíveis só para o
   cliente dono ou `staff`), mas é um **gap conhecido do back-end**
   (`.claude/specs/codegen/gaps.md`, G-5: "nenhum arquivo do codegen consome
   Visibility") — o Go gerado hoje NÃO redige esses campos em runtime.
6. **Item da linha do ticket (Kitchen):** o enunciado pede
   "itens (nome, tamanho, quantidade, observações)" no ticket da cozinha.
   Este exemplo modela nome e quantidade (`TicketItem { name, quantity }`),
   omitindo tamanho/observações — simplificação deliberada para manter o
   exemplo focado nas diretivas avaliadas (tenancy/cache/idempotency/Policy/
   snapshot) em vez de multiplicar VOs de tradução na fronteira; o mesmo
   padrão de conversão (item 2 acima) se estende trivialmente para campos
   adicionais.
7. **`Command` com o mesmo nome do `Handle` que aciona, `UseCase` com o nome
   exposto na interface** — mesma convenção do `wallet` (`Command Deposit` /
   `UseCase PerformDeposit`) e do `shop` (`Command Place` / `UseCase
   PlaceOrder`): o front-end rejeita um `UseCase` e um `Command` com o MESMO
   nome no mesmo módulo ("nome duplicado... já declarado como Command").

## Conformidade com a spec (identidade de Aggregate)

Esta fixture **não** usa mais `ValueObject <T>Id` como identidade de
Aggregate. Todo id de agregado é `ref T` — a identidade tipada da
[§2.7](../../../docs/sdd/steerings/domainscript-spec-v7/02-type-system.md):
`id ref MenuItem`, `id ref Order`, `id ref KitchenTicket`, em `state`, em
`Event`, em `Command` e em `View`. Os VOs `MenuItemId`/`OrderId`/`TicketId`
foram removidos; a §4.3.1 os classifica como redundantes (⚠️ warning) quando
existe o `Aggregate` homônimo.

A identidade implícita da
[§4.3.1](../../../docs/sdd/steerings/domainscript-spec-v7/04-domain-core.md)
está aplicada: **nenhum `state` declara campo `id`**, nenhum `Apply` semeia a
identidade (`state.id = event.id` foi removido de todos), e `self.id` é lido
diretamente no `Handle`, inclusive no de criação. Fora do agregado a
identidade se lê da instância (`menuItem.id`), nunca `menuItem.state.id`.
Os campos `id ref T` que permanecem em `Event` e `View` são **payload
declarado**, forma que a §4.2.3 chancela explicitamente ("`id` continua sendo
nome livre de campo declarado").

Continuam sendo ValueObject, corretamente, os ids que **não** são identidade
de agregado: `CustomerId`, o principal autenticado — a §4.3.1 isenta esse caso
("id de sistema externo, chave legada").

**Views na forma `From T` (§6.1).** As três Views (`MenuItemVW`, `OrderVW`,
`KitchenTicketVW`) projetam a **raiz** de um agregado — cada uma é alvo de um
`list <Aggregate> ... as <View>` —, então todas são `View X From T`, com os
campos auto-mapeados por flattening e **não** declarados. A §6.1 admite a
segunda forma, de shape explícito (o `StatementEntryVW` do exemplo da spec),
para projetar o que **não** é raiz — tipicamente um elemento de coleção do
`state`; nenhuma View daqui está nesse caso. Para `OrderVW` o `From` não é
estilo e sim exigência: a §4.3.1 define que dentro de `visibility` o `self` é
a instância projetada, e uma View sem `From` não tem instância a que se
referir. Efeito colateral esperado do auto-mapping: `OrderVW` passa a expor
também `lines`, que a forma explícita anterior omitia — a §6.1 não oferece
mecanismo de subconjunto com `From`, e é `visibility` que restringe o que
cada caller vê.

### Esta fixture NÃO passa em `dsc check` — e isso é o ponto

A fixture é escrita contra a **spec**, não contra o transpilador. Os 14 erros
de hoje são o transpilador atrasado em relação à v7, e cada um tem issue:

| Erro | Ocorrências | Causa |
|------|-------------|-------|
| `E102: membro inexistente: "id"` | 10 | Identidade implícita não implementada: sem `id` no `state`, `self.id` não resolve. [`spec-v7-identidade-implicita-do-aggregate`](../../../docs/sdd/issues/spec-v7-identidade-implicita-do-aggregate.md) |
| `tipo não declarado: "Order"` | 4 | `ref T` cross-módulo, que a §2.7 permite ("carrega identidade, não estado"), não resolve entre módulos |

Não existe hoje forma de escrever este domínio que seja **simultaneamente**
conforme e aceita: `id` no `state` é erro pela §4.3.1, e sem ele `self.id` é
erro pelo transpilador. Recuar para a forma aceita seria deixar a spec ser
ditada pela implementação — exatamente o que o `CLAUDE.md` proíbe. `pizzeria`
já está em `KNOWN_UNGENERATABLE`; o job `fixtures` da CI ainda exige `dsc
check` verde em todo projeto, então **esta fixture quebra a CI de propósito
até que as duas issues acima fechem**.

### Divergências que dependem de decisão de modelagem

Três pontos que a spec proíbe na forma atual mas cuja substituição ela **não**
determina — deixados sinalizados no código, não adivinhados:

1. **Como um UseCase cria uma instância.** Todo exemplo da spec (§5.2) é
   `load` de instância existente; o exemplo de Saga da §2.7 elide justamente a
   linha (`new_ref(Order)` seguido de `...`). `CreateMenuItem` e `PlaceOrder`
   seguem no idioma "load-then-create" (`Command Create { id ref MenuItem }` +
   `load` + `.Create(...)`), que sob `generation: system` contradiz "um campo
   `ref T` sem marcador endereça uma instância existente, nunca cria uma".
   Note que o `wallet` **evita** o assunto: seu README admite não modelar
   criação de ponta a ponta.
2. **Identidade do ticket derivada do pedido** (`load KitchenTicket(event.id)`,
   kitchen/policy.ds): passa `ref Order` onde a §2.7 exige `ref KitchenTicket`.
   É essa derivação que dá idempotência à Policy; sob identidade implícita ela
   deixa de existir e "achar o ticket deste pedido" precisa de outro mecanismo.
3. **`caller.id == self.customerId`** (`OrderVW.visibility`, sales/read.ds): a
   §4.3.1 só admite `caller.id` contra `ref T`, nunca contra ValueObject.
   Expressar "o dono do pedido" exigiria um `Aggregate Customer`, que este
   domínio não modela.

## Validar (front-end — contrato exigido)

A partir da raiz do repositório:

```sh
go build -o dsc ./cmd/dsc && ./dsc check testdata/projects/pizzeria
```

⚠️ **Falha hoje, de propósito** — ver "Esta fixture NÃO passa em `dsc check`"
acima. O job `fixtures` da CI exige check verde de todo projeto; esta fixture
ficará vermelha até a identidade implícita (§4.3.1) ser implementada.

## Gerar o back-end (bloqueado — limitação real do codegen)

```sh
./dsc gen testdata/projects/pizzeria -o /tmp/pizzeria-gen
```

Este passo **falha hoje**, e por isso `pizzeria` está em
`KNOWN_UNGENERATABLE` (`.github/workflows/ci.yml`). O erro atual é:

```
codegen: módulo Kitchen: queries.go: Query GetBoardTickets: return: forma não
suportada por EmitQuery: list ... em posição de expressão pura não é
suportado por Lowerer.Expr — precisa de hoisting em nível de statement
```

Ou seja: `list <Aggregate> ... as View` nunca foi implementado
(`tryEmitListVO` só reconhece a correlação via `AppendList<VO>`). A colisão
`UseCase` + `Policy` no mesmo módulo, que este README antes apontava como o
bloqueio, **já foi corrigida** (existe `Wire` combinado) e não aparece mais.

O inventário completo dos defeitos empilhados aqui está em
[`pizzeria-bloqueado-por-multiplos-defeitos-de-codegen`](../../../docs/sdd/issues/pizzeria-bloqueado-por-multiplos-defeitos-de-codegen.md);
a rota para `list <Aggregate> as View` é uma decisão em aberto, deliberadamente
adiada. Corrigir isso está fora do escopo desta fixture (não se mexe em
`codegen/` a partir daqui); reportado como achado, não escondido.
