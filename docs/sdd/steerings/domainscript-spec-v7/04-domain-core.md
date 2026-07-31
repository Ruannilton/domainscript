# 4. O Núcleo do Domínio (Write Side)

## 4.1. Errors

```ds
Error InsufficientBalance { message "Saldo insuficiente." }
Error InactiveWallet      { message "Carteira inativa." }
Error WalletNotFound      { message "Carteira não encontrada." }
```

## 4.2. Events

Todo evento carrega um **envelope** implícito e readonly — `eventId`, `eventType`, `timestamp`, `sequence`, `aggregateId` — gerado pelo compilador, preenchido pelo runtime e nunca declarado. Definição normativa em [§4.2.3](04-domain-core.md).

**Event vs PublicEvent** — interno ao módulo vs. compartilhado em `contracts/*.ds`. Policy cross-module escutando `Event` privado → erro de compilação.

```ds
Event WalletCreated { id ref Wallet, holder HolderName, email Email }
Event WithdrawalPerformed { id ref Wallet, amount Money, description TransactionDescription }
PublicEvent DepositPerformed { id ref Wallet, amount Money, description TransactionDescription }
```

### 4.2.1 Versionamento de Eventos

Campos novos com `default`. Transformações complexas com `Upcast`. Compilador valida caminho resolvível de toda versão histórica; emite warning quando Upcast é substituível por default.

```ds
Event DepositPerformed {
    id ref Wallet
    amount Money
    channel Channel = Channel("unknown")   // adicionado em versão posterior
}

Upcast TransferSent v1 -> v2 {
    fee = Money(amount: 0, currency: event.amount.currency)
}
```

### 4.2.2 Redação de Eventos (GDPR)

Para direito ao esquecimento (GDPR Art. 17) sem corromper replay, campos podem ser `redactable`. A redação substitui o campo por placeholder tipado — replay continua funcionando, estrutura preservada, PII removida.

```ds
Event WalletCreated {
    id ref Wallet
    holder HolderName redactable
    email Email redactable
}
```

(Mecanismo de gatilho — feature em evolução.)

### 4.2.3. Metadata Implícito

Um evento tem duas partes: **payload** — os campos que o autor declara — e **envelope** — o metadata que o compilador gera e o runtime preenche. O envelope é o que torna um evento localizável, ordenável, auditável e desserializável. Nunca é declarado, nunca é escrito, nunca é passado ao `emit`.

Esta seção define o envelope do **evento de domínio**: o emitido por `emit` dentro de um `Handle` de Aggregate ([§4.3](04-domain-core.md)).

**Os cinco campos.**

| Campo | Tipo | Semântica | Quem preenche |
|-------|------|-----------|---------------|
| `eventId` | `EventId` | Identidade do evento. UUID v7, única no programa, imutável | Runtime, na emissão |
| `eventType` | `string` | Nome qualificado da declaração — `"Carteira.DepositPerformed"`. É o que permite desserializar o payload | Compilador — constante por declaração |
| `timestamp` | `datetime` | Instante UTC em que o evento entrou no stream | Runtime, na emissão |
| `sequence` | `integer` | Posição no stream da instância emissora. Começa em `1`, monotônico, sem lacunas | Runtime, na emissão |
| `aggregateId` | `ref T` | Identidade da instância emissora ([§4.3.1](04-domain-core.md)) | Runtime, na emissão |

O conjunto é **fechado**: são exatamente estes cinco. Não existe metadata de correlação, de causação, de tenant nem de trace na superfície da linguagem — trace context é propagado pelo runtime ([§20](20-observability.md)) e tenant é contexto ambiente ([§14](14-multi-tenancy.md)); nenhum dos dois é campo de evento.

`eventId` **não se chama `id`**. `id` continua sendo nome livre de campo declarado, e é o que os eventos deste capítulo usam (`Event WalletCreated { id ref Wallet, ... }`): payload e envelope não colidem em nome nenhum. `event.id` é o campo declarado; `event.aggregateId` é o envelope. Coincidirem em valor é consequência de `emit WalletCreated(self.id, ...)`, não regra da linguagem.

**O tipo `EventId`.** Tipo embutido, **opaco**, sem representação exposta e sem conversão — mesma forma de `CallerId` ([§4.3.1](04-domain-core.md)), e pela mesma razão: identidade de evento não é identidade de Aggregate. `ref <Event>` **não existe** — `ref T` exige que `T` seja Aggregate ([§2.7](02-type-system.md)), e toda a maquinaria de `ref` (bloco `identity`, `generation`, `load T(...)`, `new_ref`, desserialização de path param) é vazia para um evento. `EventId` é nome reservado; declarar ValueObject, Enum ou Aggregate com esse nome → **erro de compilação**.

| Operação sobre `EventId` | Resultado |
|--------------------------|-----------|
| `==` / `!=` entre dois `EventId` | `boolean` |
| `<` `>` `<=` `>=` | ❌ Erro de compilação — ordem de evento é `sequence`, não identidade |
| `==` / `!=` contra `ref T`, primitivo, ValueObject, Enum ou `CallerId` | ❌ Erro de compilação |
| Métodos (`toString`, …) | ❌ Erro de compilação — catálogo fechado ([§2.8](02-type-system.md)), sem unwrap |
| Campo em `log` ([§3.4](03-control-flow.md)) | ✅ |
| Argumento de `Foreign`/`Adapter`/`Notification` | ✅ Marshalled como `string` ([§10](10-ffi.md)) |
| Campo declarado de tipo `EventId` — em Event, Command, `state`, View, ValueObject ou parâmetro de `Handle` | ❌ Erro de compilação |

Deduplicação de entrega `AtLeastOnce` ([§7](07-policies.md)) usa `eventId` e é do **runtime**: o autor não a escreve, e por isso `EventId` não precisa ser chave de `Map` nem elemento de `Set` ([§2.8.8](02-type-system.md)).

**Comparar `eventType`.** É `string`, e a comparação é contra literal de string — não existe literal de tipo na linguagem. O compilador valida o literal, então a grafia não é frouxa:

| Forma | Resultado |
|-------|-----------|
| `event.eventType == "Carteira.DepositPerformed"` | `boolean` |
| Literal que não nomeia um `Event`/`PublicEvent`/`ApplicationEvent`/`PublicApplicationEvent` declarado no programa | ❌ Erro de compilação |
| Nome simples, sem o módulo (`"DepositPerformed"`) | ❌ Erro de compilação — uma forma canônica ([§1.1](01-overview.md)) |
| `<` `>` `<=` `>=`, `contains`, `startsWith` e demais métodos de `string` sobre `event.eventType` | ❌ Erro de compilação — `eventType` compara por igualdade, não é texto manipulável |
| Comparação em contexto de tipo estaticamente único — `Apply E`, `Policy ... on E`, `Upcast E`, `Metric ... on E` | ⚠️ Warning — o resultado é constante |

Ramificar por tipo de evento no domínio **não** se escreve com `eventType`: se escreve `Apply E` e `Policy ... on E`, que já selecionam por tipo. `eventType` existe para a borda, para o log e para a auditoria.

**Como o compilador determina `T` em `aggregateId`.** Pelo **emissor**, não pela declaração. Um `Event` é declarado no topo do módulo; o vínculo vem dos sítios de `emit` e `Apply`, que o compilador já resolve globalmente.

| Situação | Resultado |
|----------|-----------|
| `E` emitido por `emit E(...)` em `Handle` de exatamente um Aggregate `T` | `aggregateId : ref T` |
| `E` emitido por dois Aggregates distintos | ❌ Erro de compilação — evento de domínio tem um emissor só |
| `Apply E` em `T` e `emit E` em `U`, com `T ≠ U` | ❌ Erro de compilação |
| `E` emitido dentro de Aggregate **e** fora dele (UseCase, Saga, Policy, Worker) | ❌ Erro de compilação — o envelope seria ambíguo |
| `E` emitido só fora de Aggregate | Evento de escopo de requisição — ver o gancho no fim desta seção |
| `E` declarado e nunca emitido nem aplicado | ⚠️ Warning — declaração morta. `event.aggregateId` e `event.sequence` sobre ele → ❌ erro de compilação |

O vínculo é fato do **programa inteiro** ([§12](12-topology.md)), não do módulo: um `PublicEvent` de `contracts/*.ds` consumido por Policy de outro módulo continua expondo `aggregateId : ref Wallet` mesmo onde `Aggregate Wallet` é privado. É exatamente o que a [§2.7](02-type-system.md) já licencia — `ref T` cross-módulo carrega identidade, não estado. O `load Wallet(event.aggregateId)` correspondente continua sujeito a [§12](12-topology.md) e [§19](19-transactions-sagas.md) (cross-service sem Saga → erro).

**Isenção da Regra de Ouro.** A [§2.1](02-type-system.md) alcança o que o autor **declara**. Metadata não é declarado: é envelope gerado, readonly, e nunca entra pela borda. Portanto `timestamp datetime`, `sequence integer` e `eventType string` são **primitivos legais no Write Side**, isentos por esta regra — e a isenção vale **só** para estes cinco campos. Um campo declarado `occurredAt datetime` num Event continua sendo ❌ erro de compilação.

**Readonly.**

| Situação | Resultado |
|----------|-----------|
| Atribuir a qualquer campo do envelope (`event.timestamp = now()`, `event.sequence = 0`) | ❌ Erro de compilação |
| Declarar campo de `Event`/`PublicEvent` chamado `eventId`, `eventType`, `timestamp`, `sequence` ou `aggregateId` | ❌ Erro de compilação — colide com o envelope |
| Passar campo do envelope como argumento de `emit` (`emit E(eventId: ...)`) | ❌ Erro de compilação — `emit` só recebe payload |
| Declarar campo de Event chamado `id` | ✅ Permitido — `id` não é nome de envelope |

**Onde o envelope é legível.** Só onde existe um receptor `event` em escopo.

| Contexto | Envelope |
|----------|----------|
| `Apply` ([§4.3](04-domain-core.md)) | ✅ Os cinco |
| `Upcast` ([§4.2.1](04-domain-core.md)) | ✅ Leitura; escrita → ❌ erro |
| Policy ([§7](07-policies.md)) — `event.*` é o receptor | ✅ Os cinco |
| `Metric ... on E` ([§20](20-observability.md)), em `value` e `labels` | ✅ Os cinco |
| Elemento de `events()` ([§4.3](04-domain-core.md)), inclusive em Query | ✅ Os cinco — é o que torna `from:`/`to:` e `since:`/`until:` expressáveis |
| `log` ([§3.4](03-control-flow.md)) dentro de um contexto acima | ✅ |
| `Handle` | ❌ Erro de compilação — não há `event` em escopo: o evento ainda não existe, e o envelope é atribuído **na** emissão |
| UseCase, Saga (`up`/`down`), Worker | ❌ Erro de compilação — o receptor ali é `cmd`; não há `event` |
| `visibility` de View, Projection ([§6](06-read-side.md)) | ❌ Erro de compilação — projetam estado de Aggregate, não evento |
| `Valid`, `Operator`, `coerce` de ValueObject ([§2.2](02-type-system.md)) | ❌ Erro de compilação |

**Metadata em `Apply` é determinístico.** Não é óbvio e é a regra que sustenta o exemplo canônico da [§4.3](04-domain-core.md). A [§2.7](02-type-system.md) barra `new_ref` em `Apply` e a [§2.8.3](02-type-system.md) exige pureza de todo o catálogo ali, ambas por causa do replay. Ler envelope não viola nenhuma das duas: o valor foi **persistido junto com o evento**, não é computado na leitura. `event.timestamp` num `Apply` devolve o instante da emissão original, idêntico em toda reexecução — é leitura do evento, e "`Apply` é hermético: depende só do evento e de built-ins" ([§10.4](10-ffi.md)) continua valendo ao pé da letra. `now()` não tem essa propriedade e por isso continua exigindo captura em evento.

**Interação com `redactable` ([§4.2.2](04-domain-core.md)).** O envelope **nunca é redigido**. Redigir `aggregateId` ou `sequence` desanexaria o evento do stream e quebraria exatamente o replay que a §4.2.2 promete preservar; redigir `timestamp` ou `eventType` tornaria o evento indesserializável. Marcar metadata como `redactable` não é sequer expressável — o campo não é declarável, e a tentativa cai na regra de colisão de nome acima (❌ erro de compilação). A redação de um campo declarado substitui **só** aquele campo pelo placeholder tipado: o evento redigido mantém `eventId`, `timestamp`, `sequence` e `aggregateId` e ocupa a mesma posição do stream. Esquecer a instância inteira não é redação de evento — é remoção do stream, fora do escopo da linguagem.

**Interação com `Upcast` ([§4.2.1](04-domain-core.md)).** `Upcast` reinterpreta o **payload** de um evento já persistido; não produz evento novo.

| Questão | Resposta |
|---------|----------|
| O envelope sobrevive ao upcast? | ✅ Íntegro — os cinco campos atravessam inalterados |
| `eventId` muda? | ❌ Não. É o mesmo evento, lido de outra forma |
| `Upcast` pode ler o envelope? | ✅ Sim, e é determinístico pela mesma razão que em `Apply` |
| `Upcast` pode escrever o envelope? | ❌ Erro de compilação |
| `eventType` muda de v1 para v2? | ❌ Não. `eventType` identifica a **declaração**, não a versão. A versão do payload é registrada pelo store ao lado dele e não é legível no domínio — o domínio só vê o evento já upcastado para a versão corrente |

**Ordem e unicidade.**

| Questão | Regra |
|---------|-------|
| Escopo de `sequence` | **Por stream de instância** — nunca global. Uma sequence global exigiria um contador único no programa inteiro, incompatível com [§12](12-topology.md) |
| Monotonicidade | Estrita, `+1` por evento, começando em `1`. Sem lacunas, sem reuso, mesmo após redação |
| `strategy` | Irrelevante: `EventSourced` e `StateStored` gravam o stream igualmente. `strategy` decide se o **estado** é reconstruído dele, não se ele existe |
| `events(from: 100, to: 200)` | Limites de `sequence`, **inclusivos** nos dois extremos |
| `events(since: d1, until: d2)` | Limites de `timestamp`, **inclusivos** nos dois extremos |
| `snapshot every 50 events` | Snapshot tirado quando `sequence % 50 == 0`; o replay retoma no `sequence` seguinte ao do snapshot |
| Ordem entre streams distintos | Só por `timestamp`. `sequence` de instâncias diferentes não é comparável, e compará-lo não é erro — é sem sentido |
| Unicidade de `eventId` | Global no programa. Dois eventos nunca compartilham `eventId`, nem entre módulos, nem entre tenants |

**Serialização e fronteira de módulo.** O envelope atravessa a fronteira junto com todo `PublicEvent` ([§12](12-topology.md)), como envelope do payload publicado — nunca misturado a ele:

| Campo | Forma na borda |
|-------|----------------|
| `eventId` | String JSON (UUID v7); gRPC `string` |
| `eventType` | String JSON — nome qualificado |
| `timestamp` | String JSON RFC 3339 em UTC, a mesma grafia de `datetime.toString` ([§2.8.6](02-type-system.md)) |
| `sequence` | Número JSON; gRPC `int64` |
| `aggregateId` | A representação subjacente de `ref T` ([§2.7](02-type-system.md)): `uuid`/`string` → string JSON, `integer` → número JSON |

`eventType` é o que fecha o ciclo: sem ele o payload publicado é bytes sem tipo, e o consumidor não tem como escolher o desserializador. Por isso é **qualificado** — dois módulos podem declarar `Event Created`, e `"Carteira.Created"` desambigua. Um `Event` privado nunca cruza fronteira de módulo, mas é persistido com o mesmo envelope: o formato do store é uniforme e o replay de um stream antigo não depende de o evento algum dia ter sido público. No canal com `orderBy: aggregateId` ([§12](12-topology.md)) a partição é o stream, e dentro dela a entrega segue `sequence`.

**Em `*.test.ds` ([§24](24-testing.md)).** O envelope é readonly também no teste: o cenário nunca o fornece, o runner sempre o atribui — é o que faz um `Apply` que lê `event.timestamp` rodar num cenário determinístico.

| Situação | Resultado |
|----------|-----------|
| Fornecer campo do envelope em `given` ou `when` | ❌ Erro de compilação — readonly |
| Nomear campo do envelope em `then` | ❌ Erro de compilação — `then` casa payload |
| `aggregateId` atribuído pelo runner | O alias de teste do cenário ([§2.7](02-type-system.md)) — `given Wallet("W1") from [...]` dá `ref Wallet` de `"W1"` a todos os eventos da lista |
| `sequence` atribuído pelo runner | `1..n`, na ordem em que os eventos aparecem no `given`, continuando pelos eventos produzidos pelo `when` |
| `timestamp` atribuído pelo runner | Relógio virtual determinístico, monotônico e reprodutível entre execuções |
| `eventId` atribuído pelo runner | Derivado deterministicamente do cenário e da posição |

**Eventos emitidos fora de um Aggregate.** Esta seção define o envelope do evento de domínio. Um evento emitido em UseCase, Saga, Policy ou Worker não tem instância emissora e portanto **não tem `aggregateId` nem `sequence`** — é evento de escopo de requisição, e seu envelope próprio, que troca esse par por identificação do procedimento emissor, está definido em [§5](05-application-layer.md). Os três campos universais — `eventId`, `eventType`, `timestamp` — valem para ambos, com semântica, tipos e forma de borda idênticos aos desta seção. Ler `event.aggregateId` ou `event.sequence` de um evento sem Aggregate emissor → **erro de compilação**.

```ds
PublicEvent DepositPerformed { id ref Wallet, amount Money, description TransactionDescription }

Aggregate Wallet {
    strategy EventSourced

    Handle Deposit(amount Money, description TransactionDescription) {
        ensure state.active == ActiveStatus(true) else InactiveWallet
        emit DepositPerformed(self.id, amount, description)   // envelope não se passa
    }

    Apply DepositPerformed {
        state.balance = state.balance + event.amount          // payload
        state.entries.add(StatementEntry(
            type: TransactionType.Deposit,
            amount: event.amount,
            description: event.description,
            date: event.timestamp                             // envelope — determinístico no replay
        ))
    }
}

Policy AuditDeposits on DepositPerformed {
    delivery AtLeastOnce
    execute {
        wallet = load Wallet(event.aggregateId)               // ref Wallet, do envelope
        log info "depósito auditado" {
            eventId  = event.eventId                          // EventId, opaco
            position = event.sequence                         // integer
            at       = event.timestamp                        // datetime
            kind     = event.eventType                        // "Carteira.DepositPerformed"
            holder   = wallet.state.holder
        }
    }
}
```

Erros que o trecho acima recusaria: `Event E { timestamp datetime }` (colide com o envelope), `Event E { occurredAt datetime }` (primitivo declarado — a isenção não se estende), `event.sequence = 1` num `Apply` (readonly), `emit DepositPerformed(eventId: ...)` (`emit` só recebe payload), `event.timestamp` dentro de um `Handle` (não há `event` em escopo), `event.eventId < other.eventId` (`EventId` não ordena), `state { last EventId }` (`EventId` não é declarável como campo), `ref DepositPerformed` (`ref` só sobre Aggregate), `emit DepositPerformed(...)` num `Handle` de `Wallet` **e** num UseCase (envelope ambíguo).

## 4.3. Aggregates

`StateStored` (padrão) ou `EventSourced`. Snapshot opt-in. Event stream nativo via `events()`. Bloco `access` closed-by-default. Bloco `storage` mapeia state e campos `FileRef`.

```ds
Aggregate Wallet {
    identity { type: uuid, generation: system }
    strategy EventSourced
    snapshot every 50 events

    state {
        balance Money
        active ActiveStatus
        holder HolderName
        email Email
        entries AppendList<StatementEntry>
    }

    access {
        Create          requires caller.authenticated
        Deposit         requires caller.authenticated
        Withdraw        requires caller.id == self.id or caller.hasRole("admin")
        SendTransfer    requires caller.id == self.id
        ReceiveTransfer requires caller.authenticated
    }

    Handle Create(holder HolderName, email Email) {
        emit WalletCreated(self.id, holder, email)
    }

    Apply WalletCreated {
        state.holder = event.holder
        state.email = event.email
        state.balance = Money(amount: 0, currency: "BRL")
        state.active = ActiveStatus(true)
    }

    Handle Deposit(amount Money, description TransactionDescription) {
        ensure state.active == ActiveStatus(true) else InactiveWallet
        emit DepositPerformed(self.id, amount, description)
    }

    Apply DepositPerformed {
        state.balance = state.balance + event.amount
        state.entries.add(StatementEntry(
            type: TransactionType.Deposit,
            amount: event.amount,
            description: event.description,
            date: event.timestamp
        ))
    }

    Handle Withdraw(amount Money, description TransactionDescription) {
        ensure state.active == ActiveStatus(true) else InactiveWallet
        ensure state.balance >= amount else InsufficientBalance
        emit WithdrawalPerformed(self.id, amount, description)
    }

    Apply WithdrawalPerformed {
        state.balance = state.balance - event.amount
        state.entries.add(StatementEntry(
            type: TransactionType.Withdrawal,
            amount: event.amount,
            description: event.description,
            date: event.timestamp
        ))
    }
}
```

**Event stream nativo:**

```ds
wallet.events()
wallet.events(from: 100, to: 200)
wallet.events(since: someDate, until: anotherDate)
```

`events()` é o log técnico; `AppendList` é a visão de negócio (ex: extrato). Coexistem com propósitos distintos.

### 4.3.1. Identidade Implícita

Todo Aggregate `T` tem um membro implícito `id` de tipo `ref T` ([§2.7](02-type-system.md)). É a identidade da instância: existe sempre, é readonly, e **nunca é declarada**. Identidade não é modelagem — é a condição para haver instância.

**Declaração e forma.**

| Regra | Resultado |
|-------|-----------|
| Campo chamado `id` no bloco `state`, de qualquer tipo | ❌ Erro de compilação — colide com o membro implícito |
| `state.id`, em leitura ou escrita | ❌ Erro de compilação — `id` não é campo de `state`; o membro é `self.id` |
| Atribuir a `self.id` (inclusive em `Apply`) | ❌ Erro de compilação — readonly |
| `id` implícito no Write Side | ✅ `ref T` não é primitivo ([§2.1](02-type-system.md)); a Regra de Ouro não o alcança |

A identidade não é estado: em `StateStored` é a chave primária da tabela mapeada em `storage.state` ([§13](13-module-infra.md)); em `EventSourced` é a chave do stream. Snapshot não a serializa como campo. Em módulo multi-tenant a unicidade é dentro do tenant ([§14](14-multi-tenancy.md)) — dois tenants podem carregar a mesma identidade sem colisão.

**O bloco `identity`** é um bloco de configuração do Aggregate, ao lado de `strategy`, `snapshot`, `storage`, `state` e `access`. Seu conteúdo (`type`, `generation`, defaults, representação subjacente, erros de combinação) está definido em [§2.7](02-type-system.md) e não se repete aqui.

| Regra | Resultado |
|-------|-----------|
| Ordem entre blocos de configuração | Livre |
| Bloco de configuração repetido no mesmo Aggregate | ❌ Erro de compilação |
| `identity` ausente | ≡ `identity { type: uuid, generation: system }` |
| `identity` em construto que não é Aggregate (Saga, ValueObject, View, …) | ❌ Erro de compilação |

**Onde `self.id` é legível.**

| Contexto | `self.id` |
|----------|-----------|
| `Handle`, inclusive o de criação | ✅ Legível |
| `Apply` | ✅ Legível — a identidade é anterior ao evento; replay a vincula do stream |
| `access` do Aggregate ([§4.3](04-domain-core.md)) | ✅ Legível — única posição, com `visibility`, em que `caller.id` compara |
| `visibility` de View sobre `T` ([§6.2](06-read-side.md)) | ✅ Legível — `self` é a instância projetada |
| `log` ([§3.4](03-control-flow.md)) dentro de `Handle`/`Apply` | ✅ Legível |
| Instância carregada, fora do Aggregate — `wallet.id`, `o.id`, `ticket.id` | ✅ Legível em UseCase, Saga, Policy, Worker, Query e Projection |
| `Valid` de ValueObject, `Operator`, `coerce` | ❌ Erro de compilação — ali `self` é o ValueObject ([§2.2](02-type-system.md)), não há Aggregate em escopo |
| UseCase, Policy, Saga, Worker, Query sem instância carregada | ❌ Erro de compilação — não existe `self` de Aggregate nesses corpos |

Fora do Aggregate a identidade se lê **da instância**, nunca de `self`: `load Wallet(cmd.walletId)` devolve uma instância cujo `.id` é `ref Wallet`. É o que faz `join Order o on t.orderId == o.id` ([§6.3](06-read-side.md)) type-checar — `ref Order` dos dois lados.

**Como a identidade é atribuída.**

| `generation` | Quando passa a existir | De onde vem |
|--------------|------------------------|-------------|
| `system` | Antes do primeiro evento da instância, alocada pelo runtime | Runtime ([§2.7](02-type-system.md)); ou pré-alocada com `new_ref(T)` |
| `client` | No mesmo instante — não há alocação | É o próprio valor `ref T` com que a instância foi endereçada |

Em ambos os casos `self.id` **já vale dentro do `Handle` de criação** — o `Handle` que produz o primeiro evento da instância. `emit WalletCreated(self.id, ...)` é legal ali, e nenhum `Apply` semeia a identidade: quando o primeiro `Apply` roda, ela já existe. Uma vez estabelecida, é imutável pelo resto da vida da instância.

Com `generation: client` a identidade entra pela borda, no Command que dispara o Handle de criação, no campo marcado **`identity`** — marcador posfixo, como `redactable` ([§4.2](04-domain-core.md)):

```ds
Command IssueTicketCmd {
    code ref Ticket identity      // identidade escolhida pelo cliente
    holder HolderName
}
```

| Regra | Resultado |
|-------|-----------|
| Campo marcado `identity` de tipo diferente de `ref T` | ❌ Erro de compilação |
| Mais de um campo `identity` no mesmo Command | ❌ Erro de compilação |
| Command que cria instância de `T` com `generation: client` sem campo `identity` | ❌ Erro de compilação |
| Campo `identity` endereçando `T` com `generation: system` | ❌ Erro de compilação — a identidade é do sistema, o cliente não a escolhe |
| Marcador `identity` fora de campo de Command | ❌ Erro de sintaxe |

Não há convenção de nome nem de posição: o marcador é a única forma. Um campo `ref T` sem marcador endereça uma instância existente ([§5.1](05-application-layer.md)), nunca cria uma.

**`caller.id`.** `caller` é contexto ambiente ([§14](14-multi-tenancy.md)) e não é Aggregate do programa: o principal autenticado vem da borda, não do domínio. `caller.id` tem tipo **`CallerId`** — tipo embutido, **opaco**, sem representação exposta e sem conversão. `CallerId` é nome reservado; declarar ValueObject, Enum ou Aggregate com esse nome → **erro de compilação**.

Opaco não significa inútil: `CallerId` tem exatamente um operador, a **comparação de vínculo** contra uma identidade de Aggregate.

| Operação | Resultado |
|----------|-----------|
| `caller.id == <expr : ref T>` / `!=`, em `access` ou `visibility` | `boolean` — comparação de vínculo |
| `caller.id` em qualquer outro contexto (`Handle`, `Apply`, UseCase, Query, Policy, Saga, `log`) | ❌ Erro de compilação |
| `caller.id` comparado a primitivo, ValueObject, Enum ou outro `CallerId` | ❌ Erro de compilação |
| `caller.id` atribuído a variável, passado a `load`, guardado em `state` ou emitido em evento | ❌ Erro de compilação |
| `<` `>` `<=` `>=` sobre `CallerId` | ❌ Erro de compilação |

`caller.id == x`, com `x : ref T`, avalia `true` sse o caller está autenticado e o subject autenticado, desserializado para a representação declarada em `identity` de `T`, é igual a `x`. Caller anônimo ou subject malformado para essa representação → `false`, **fail-closed** — nunca erro de execução, nunca 422; a negativa é negativa de `access`, que já é closed-by-default. `T` é lido do próprio operando: `caller.id == self.id` num `Aggregate Wallet` compara contra a representação de `Wallet`; `caller.id == self.owner`, com `owner ref Person`, compara contra a de `Person`. A nominalidade de `ref` fica intacta — não se introduz nenhuma comparação `ref T` × `ref U`, nem `ref T` × primitivo: `CallerId` é um tipo distinto com um operador próprio.

**ValueObjects de id.** Um `ValueObject WalletId(string)` continua sendo um ValueObject legal — e **não é identidade de Aggregate**. Não há como torná-lo uma: `state` não aceita campo `id`, e `load T(...)` exige `ref T` ([§2.7](02-type-system.md)). Passá-lo onde se espera `ref T` → **erro de compilação**.

| Situação | Resultado |
|----------|-----------|
| `ValueObject <T>Id` declarado num programa que declara `Aggregate <T>` | ⚠️ Warning — redundante com `ref <T>`; use o tipo de referência |
| `ValueObject XId` sem `Aggregate X` (id de sistema externo, chave legada) | ✅ Sem diagnóstico |

O warning é por casamento exato do nome (`<T>` seguido de `Id`). Não é erro: o VO pode carregar validação de formato usada na borda de um sistema de terceiros.

**Identidade em `*.test.ds`.** Um cenário fornece a identidade por **alias de teste** ([§2.7](02-type-system.md)): o literal em posição `ref T` — `Wallet("W1")`, `given Wallet("W1") from [...]`, `TransferCmd(fromWalletId: "W1", ...)` — denota uma identidade simbólica estável, materializada pelo runner num valor válido da representação declarada. Vale também para `generation: system`: dentro do cenário `self.id` é a identidade vinculada ao alias, e o mesmo literal denota a mesma instância ([§24](24-testing.md)). O runner nunca aloca identidade por conta própria para um alias já visto.

**Metadata de evento.** O metadata implícito de `Event` ([§4.2](04-domain-core.md)) é definido à parte e nada aqui o antecipa. Os campos `id ref Wallet` dos eventos deste capítulo são campos declarados como quaisquer outros.

```ds
Aggregate Wallet {
    identity { type: uuid, generation: system }   // ≡ ausência do bloco
    strategy EventSourced

    state {
        balance Money
        holder HolderName          // sem campo id: ele é implícito
    }

    access {
        Create   requires caller.authenticated
        Withdraw requires caller.id == self.id or caller.hasRole("admin")
    }

    Handle Create(holder HolderName, email Email) {
        emit WalletCreated(self.id, holder, email)   // self.id já vale aqui
    }

    Apply WalletCreated {
        state.holder = event.holder                  // nenhum Apply semeia a identidade
        state.balance = Money(amount: 0, currency: "BRL")
    }
}
```

Erros que o trecho acima recusaria: `state { id ref Wallet }` (colide com o membro implícito), `state.id = event.id` (`state` não tem `id`), `self.id = new_ref(Wallet)` (readonly), `caller.id == "u-1"` (`CallerId` contra primitivo), `walletId = caller.id` dentro de um `Handle` (`caller.id` só em `access`/`visibility`), `Command CreateWalletCmd { walletId ref Wallet identity }` (a identidade de `Wallet` é `system`).

