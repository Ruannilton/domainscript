# 2. Sistema de Tipos

## 2.1. A Regra de Ouro

Primitivos (`integer`, `decimal`, `string`, `boolean`, `datetime`, `bytes`) são **proibidos no Write Side** (Aggregates, Commands, Events, ApplicationEvents). Permitidos dentro de ValueObjects/Enums e no Read Side (Views, Queries, Projections). Isentos: a identidade tipada `ref T` ([§2.7](02-type-system.md)), que não é primitivo, e o envelope implícito de evento ([§4.2.3](04-domain-core.md), [§5.3.1](05-application-layer.md)), que não é declarado.

## 2.2. ValueObjects

Blocos atômicos, imutáveis, auto-validáveis, com comportamento (operadores). Base de todos os exemplos seguintes:

```ds
ValueObject Email(string) {
    Valid { self.contains("@") }
}

ValueObject HolderName(string) {
    Valid { self.length >= 2 and self.length <= 120 }
}

ValueObject TransactionDescription(string) {
    Valid { self.length <= 256 }
}

ValueObject ActiveStatus(boolean) {
    Valid { true }
}

ValueObject Money {
    amount decimal
    currency string

    Valid { amount >= 0 }

    Operator +(other Money) -> Money {
        ensure self.currency == other.currency else CurrencyMismatchError
        return Money(amount: self.amount + other.amount, currency: self.currency)
    }

    Operator -(other Money) -> Money {
        ensure self.currency == other.currency else CurrencyMismatchError
        ensure self.amount >= other.amount else NegativeResultError
        return Money(amount: self.amount - other.amount, currency: self.currency)
    }

    Operator >=(other Money) -> boolean {
        ensure self.currency == other.currency else CurrencyMismatchError
        return self.amount >= other.amount
    }
}

ValueObject StatementEntry {
    type TransactionType
    amount Money
    description TransactionDescription
    date datetime
}
```

## 2.3. Enums

Conjunto fechado de valores nomeados sob namespace e tipo. Permitidos no Write Side. Syntactic sugar para VO com validação de pertencimento — o compilador emite warning quando um VO poderia ser Enum.

```ds
Enum TransactionType : string {
    Deposit     = "DEPOSIT"
    Withdrawal  = "WITHDRAWAL"
    TransferIn  = "TRANSFER_IN"
    TransferOut = "TRANSFER_OUT"
}

Enum TicketStatus : string {
    Available = "AVAILABLE"
    Reserved  = "RESERVED"
    Sold      = "SOLD"
    Cancelled = "CANCELLED"
    Used      = "USED"
}
```

**Coerção implícita (padrão):** conversão bidirecional automática na borda. Valor desconhecido → erro 422.

**Coerção explícita (`coerce`):** para aliases, legados, case-insensitive.

```ds
Enum PaymentMethod : string {
    CreditCard = "CREDIT_CARD"
    DebitCard  = "DEBIT_CARD"
    Pix        = "PIX"

    coerce from string {
        match self.uppercase() {
            "CREDIT_CARD", "CC" => CreditCard
            "DEBIT_CARD", "DC"  => DebitCard
            "PIX"               => Pix
            _ => InvalidPaymentMethodError
        }
    }
}
```

## 2.4. Tipos de Coleção

| Tipo | Descrição | Métodos |
|------|-----------|---------|
| `List<T>` | Lista mutável ordenada | [§2.8](02-type-system.md) |
| `AppendList<T>` | Append-only. Compilador otimiza storage e paginação | [§2.8](02-type-system.md) — só `add()` muta |
| `Set<T>` | Sem duplicatas | [§2.8](02-type-system.md) |
| `Map<K, V>` | Chave-valor | [§2.8](02-type-system.md) |

A lista autoritativa de assinaturas, retornos e semântica é [§2.8](02-type-system.md) — inclusive o que **não** existe (indexação, remoção por posição).

`AppendList<T>`: `remove()`/`clear()`/reatribuição → **erro de compilação**.

## 2.5. Tipo `File`

Arquivos são conceitualmente ValueObjects, mas operacionalmente especiais: bytes caros de carregar, storage dedicado, ciclo de vida próprio.

| Tipo | Função |
|------|--------|
| `File` | VO com bytes em memória. Commands e operações transitórias. |
| `FileStream` | Streaming chunk-a-chunk para arquivos grandes. |
| `FileRef` | Referência leve com metadata. Vive no Aggregate state. Nunca carrega bytes implicitamente. |

**Operações:**

| Operação                       | Comportamento                                               |
| ------------------------------ | ----------------------------------------------------------- |
| `store cmd.file`               | Upload ao storage, retorna `FileRef`                        |
| `load File(fileId)`            | Carrega bytes explicitamente                                |
| `signed_url(fileId, expires:)` | URL temporária do storage                                   |
| `delete file(fileId)`          | Remoção explícita — ciclo de vida é responsabilidade do dev |

```ds
Aggregate Person {
    storage {
        state: PersonDb
        document: DocumentStorage    // campo FileRef → FileStorage específico
    }

    state {
        name HolderName
        document FileRef
    }

    access {
        AttachDocument requires caller.id == self.id
    }

    Handle AttachDocument(file FileRef) {
        emit DocumentAttached(self.id, file)
    }

    Apply DocumentAttached {
        state.document = event.document
    }
}

Command UploadDocumentCmd {
    personId ref Person
    document File
}

UseCase UploadDocument handles UploadDocumentCmd {
    execute {
        person = load Person(cmd.personId)
        ensure person exists else PersonNotFound
        docId = store cmd.document
        person.AttachDocument(docId)
    }
}

Query GetDocumentUrl(personId ref Person) -> string {
    person = load Person(personId)
    ensure person exists else PersonNotFound
    return signed_url(person.state.document, expires: 15min)
}
```

Em domínios EventSourced/auditados, arquivos podem viver além do aggregate — por isso a remoção nunca é automática.

## 2.6. Funções Utilitárias (Built-in)

| Função                      | Retorno    | Descrição                     |
| --------------------------- | ---------- | ----------------------------- |
| `now()`                     | `datetime` | Timestamp atual               |
| `uuid()`                    | `string`   | UUID v7                       |
| `random(min, max)`          | `integer`  | Inteiro aleatório             |
| `random_str(length)`        | `string`   | String aleatória              |
| `signed_url(fileId, expires:)` | `string` | URL temporária de FileStorage |
| `new_ref(T)`                | `ref T`    | Nova identidade de Aggregate ([§2.7](02-type-system.md)) |

## 2.7. Tipos de Referência (`ref`)

Identidade de Aggregate é tipo, não primitivo embrulhado em convenção de nome. `ref <Aggregate>` é o tipo de primeira classe que a carrega: um wrapper type-safe sobre um primitivo, nominal, sem conversão implícita.

```ds
Command DepositCmd {
    walletId ref Wallet
    amount Money
    description TransactionDescription
}
```

`ref Wallet` e `ref Person` são **tipos distintos**. Atribuir, passar como argumento ou comparar um no lugar do outro → **erro de compilação**. `ref` sobre um tipo que não é Aggregate (`ref Money`, `ref WalletId`, `ref TicketStatus`) → **erro de compilação**. Não há unwrap: usar um `ref T` onde se espera `string`/`integer`/`uuid`, ou o contrário, → **erro de compilação**.

O membro implícito `id` de um Aggregate `T` tem tipo `ref T`. A declaração, atribuição e semeadura dessa identidade implícita estão em [§4.3.1](04-domain-core.md).

Não confundir com `FileRef` ([§2.5](02-type-system.md)): `FileRef` endereça um arquivo em um FileStorage; `ref T` endereça um Aggregate.

**Regra de Ouro.** `ref T` **não é primitivo** — é permitido no Write Side ([§2.1](02-type-system.md)) em qualquer posição. É exatamente o que torna a identidade tipada viável: um Command não precisa declarar `walletId string` (proibido) nem inventar um `ValueObject WalletId` só para atravessar a fronteira.

**Onde `ref T` pode aparecer:**

| Posição | Exemplo |
|---------|---------|
| Campo de Command | `walletId ref Wallet` |
| Campo de `Event`/`PublicEvent` | `id ref Wallet` |
| Parâmetro de `Handle` | `Handle SendTransfer(to ref Wallet, amount Money)` |
| Campo de `state` de Aggregate | `owner ref Person` |
| Campo de `state` de Saga | `orderId ref Order` |
| Argumento de `load` | `load Wallet(cmd.walletId)` |
| Campo de View/Projection, parâmetro e retorno de Query | `Query GetWalletSummary(walletId ref Wallet) -> WalletSummaryVW` |
| Campo de ValueObject | `ValueObject OrderLine { productId ref Product, amount Money }` |
| Parâmetro de Notification/Adapter/`Foreign` | marshalled como a representação subjacente ([§10](10-ffi.md)) |

Chave de `Map<ref T, V>` e elemento de `Set<ref T>` são permitidos. `ref T` cross-módulo é permitido — carrega identidade, não estado; o `load T(...)` correspondente continua sujeito a [§12](12-topology.md) e [§19](19-transactions-sagas.md) (cross-service sem Saga → erro).

**Representação subjacente.** Declarada no bloco `identity` do Aggregate, que configura a representação de `ref T` para todo o programa:

```ds
Aggregate Wallet {
    identity {
        type: uuid            // uuid (padrão) | string | integer
        generation: system    // system (padrão) | client
    }
    ...
}
```

Ausência do bloco ≡ `identity { type: uuid, generation: system }`. Não há forma abreviada.

| `type` | `generation` | Quem aloca | Ordem |
|--------|--------------|------------|-------|
| `uuid` | `system` | Runtime, antes do primeiro evento | UUID v7 — ordenável por tempo de criação |
| `uuid` | `client` | Cliente, na borda | Sem ordem |
| `integer` | `system` | Sequence do `Database` de `storage.state` ([§13](13-module-infra.md)) | Incremental — ordenável |
| `integer` | `client` | Cliente, na borda | Sem ordem |
| `string` | `client` | Cliente — chave natural ou legada | Sem ordem |

Não existe botão para escolher entre geração incremental e aleatória-ordenável: a ordem é **consequência da representação**, não configuração. `uuid` gerado pelo sistema é sempre UUID v7 (aleatório, ordenável no tempo); `integer` gerado pelo sistema é sempre uma sequence monotônica.

- `type: string` com `generation: system` → **erro de compilação**. Uma chave textual é natural ou legada; o sistema não inventa uma.
- `type: integer` com `generation: system` em Aggregate `strategy EventSourced` ([§4.3](04-domain-core.md)) → **erro de compilação**. A identidade precisa existir antes do primeiro evento e o event store não aloca sequence.
- `type: integer` com `generation: system` exige o `Database` declarado em `storage.state`; ausente → **erro de compilação**.

Mapeamento de storage ([§13](13-module-infra.md)): `uuid` → tipo nativo `uuid` quando o provider suporta, senão `char(36)`; `string` → `varchar(256)`; `integer` → inteiro de 64 bits, com sequence quando `generation: system`.

**Origem de um valor `ref`:**

| Origem | Como |
|--------|------|
| Alocada na criação | `generation: system` — o runtime aloca antes do primeiro evento; `self.id` já vale dentro do `Handle` de criação |
| Pré-alocada | `new_ref(T)` — quando o chamador precisa do id antes de o Aggregate existir (típico de Saga) |
| Recebida na borda | Campo `ref T` de Command ou path param, desserializado da representação ([§11](11-interface.md)) |
| Lida | Campo `ref T` no `state` de outro Aggregate, em View/Projection, ou retorno de Query |

`new_ref(T)` retorna `ref T` e só é legal quando a identidade de `T` é `type: uuid, generation: system`; nos demais casos → **erro de compilação** (com `client` a identidade é do cliente; com `integer` a sequence só aloca na escrita). Proibida em `Apply`, em `Valid`, em `coerce` e em Query → **erro de compilação** — mesma razão que barra FFI em `Apply` ([§25](25-compilation-rules.md)): replay determinístico e leitura pura.

`uuid()` ([§2.6](02-type-system.md)) continua retornando `string` e **não** produz identidade: atribuir `uuid()` a um `ref T` → **erro de compilação**.

**Igualdade e ordenação.**

| Operação | Resultado |
|----------|-----------|
| `==` / `!=` entre dois `ref T` | `boolean` |
| `==` / `!=` entre `ref T` e `ref U` (`T` ≠ `U`) | ❌ Erro de compilação |
| `==` / `!=` entre `ref T` e a representação subjacente | ❌ Erro de compilação |
| `<` `>` `<=` `>=` entre dois `ref T`, identidade `generation: system` | `boolean` — ordem de criação |
| `<` `>` `<=` `>=` entre dois `ref T`, identidade `generation: client` | ❌ Erro de compilação |

Ordenar por `ref` é ordenar por **criação** — só existe quando o sistema gera a identidade (UUID v7 ordenável no tempo, sequence incremental). Com `generation: client` os valores são arbitrários e a ordem não significa nada; `orderBy` sobre um campo `ref T` nessa configuração ([§6.3](06-read-side.md)) segue a mesma regra.

**Serialização na borda.** `ref T` trafega sempre como escalar, nunca como objeto: `uuid` e `string` → string JSON; `integer` → número JSON. Em gRPC ([§11](11-interface.md)): `uuid`/`string` → `string`; `integer` → `int64`. Path params (`/wallets/{walletId}`) são desserializados para `ref T` conforme a representação do Aggregate alvo.

| Situação na borda | Resultado |
|-------------------|-----------|
| Campo `ref` obrigatório ausente | 400 (fail-closed, como tenant ausente em [§14](14-multi-tenancy.md)) |
| Valor presente mas malformado para a representação (`uuid` fora do formato canônico, `string` vazia ou > 256, `integer` não inteiro ou ≤ 0) | 422 — mesma classe da coerção de Enum falha ([§2.3](02-type-system.md)); gRPC: `INVALID_ARGUMENT` |
| Valor bem-formado, Aggregate inexistente | **Não** é erro de borda: `load` não encontra e o domínio decide via `ensure ... exists else XNotFound` → `Error` de negócio, 4xx ([§23](23-error-classification.md)) |

Valor malformado é rejeitado antes do UseCase: nunca vira `Error` de domínio e nunca é declarável como tal ([§23](23-error-classification.md)).

**Interação com `load`.** `load <Aggregate>(x)` exige `x : ref <Aggregate>`. Passar `ref U`, um ValueObject de id, um primitivo ou o resultado de `uuid()` → **erro de compilação**. A forma `load File(fileId)` ([§2.5](02-type-system.md)) endereça o FileStorage e não usa `ref`.

**`ref` é palavra reservada.** Só ocorre em posição de tipo. Nomear variável, parâmetro, campo, ValueObject ou qualquer outra declaração como `ref` → **erro de sintaxe**.

**Identidades em `*.test.ds`.** Um literal `string`/`integer` em posição de `ref T` ([§24](24-testing.md)) denota um **alias de teste**: identidade simbólica estável que o runner materializa como um valor válido da representação declarada. O mesmo literal denota a mesma identidade dentro do cenário. Fora de `*.test.ds` não existe coerção de literal para `ref` → **erro de compilação**.

**Exemplo:**

```ds
Aggregate Wallet {
    identity { type: uuid, generation: system }

    state {
        balance Money
        holder HolderName
    }
    ...
}

Aggregate Ticket {
    identity { type: string, generation: client }   // código impresso no ingresso
    ...
}

Command TransferCmd {
    fromWalletId ref Wallet
    toWalletId ref Wallet
    amount Money
    description TransactionDescription
}

Event TransferSent { id ref Wallet, to ref Wallet, amount Money }

Error SameWalletTransfer { message "Origem e destino iguais." }

UseCase PerformTransfer handles TransferCmd {
    execute {
        ensure cmd.fromWalletId != cmd.toWalletId else SameWalletTransfer
        from = load Wallet(cmd.fromWalletId)     // exige ref Wallet
        ensure from exists else WalletNotFound
        to = load Wallet(cmd.toWalletId)
        ensure to exists else WalletNotFound
        from.SendTransfer(cmd.toWalletId, cmd.amount, cmd.description)
        to.ReceiveTransfer(cmd.fromWalletId, cmd.amount, cmd.description)
    }
}

Saga PurchaseTickets handles PurchaseTicketsCmd {
    mode await timeout 60s
    state { orderId ref Order, ticketIds List<ref Ticket> }

    step CreateOrder {
        up {
            state.orderId = new_ref(Order)       // id antes de o Aggregate existir
            ...
        }
        down { ... }
    }
}
```

Erros que os trechos acima recusariam: `load Wallet(cmd.toWalletId)` com `cmd.toWalletId ref Ticket` (`ref Ticket` ≠ `ref Wallet`), `cmd.fromWalletId == "W1"` (`ref` contra primitivo), `new_ref(Ticket)` (identidade `client`), `list Ticket t orderBy t.id` (identidade de `Ticket` é `client`, sem ordem), `Command Cmd { ref string }` (`ref` como nome).

## 2.8. Catálogo de Métodos Embutidos

Esta seção é a **autoridade** sobre o que se pode invocar em um valor. O catálogo é **fechado**: método que não esteja aqui não existe, e chamá-lo é **erro de compilação** detectado no `check` — nunca na geração. O usuário não declara métodos; a única extensão de um tipo é `Operator` em ValueObject ([§2.2](02-type-system.md)).

Substitui a coluna de operações da [§2.4](02-type-system.md) e complementa as funções livres da [§2.6](02-type-system.md): função livre é chamada sem receptor (`now()`), método é chamado sobre um valor (`self.length`).

### 2.8.1. Método, campo e operador

DomainScript **não tem propriedades**. Três formas, sem sobreposição:

| Forma | O que é | Onde existe |
|-------|---------|-------------|
| `x.nome` sobre tipo composto | **campo** declarado | ValueObject composto, `state` de Aggregate/Saga, Event, Command, View, Projection |
| `x.nome(...)` | **método embutido** deste catálogo | primitivos e coleções |
| `a + b`, `a >= b` | **operador** — embutido ou `Operator` de VO ([§2.2](02-type-system.md)) | conforme o tipo |

Primitivos e coleções não têm campos. Tipos compostos não têm métodos. Logo `x.nome` nunca é ambíguo, e é isso que torna a regra da [§2.8.2](02-type-system.md) segura.

- Invocar um `Operator` em forma de método (`a.plus(b)`, `a.gte(b)`) → **erro de compilação**. Operador se escreve como operador.
- Um ValueObject wrapper é **opaco por fora**: dentro de `Valid`, `Operator` e `coerce`, `self` **é** o valor base e expõe os métodos do primitivo; fora do corpo do VO a base não é acessível — `walletId.length`, `description.trim` → **erro de compilação**. Mesma regra de não-unwrap de [§2.7](02-type-system.md).
- Enum não tem métodos: ramificação é `match` ([§3.2](03-control-flow.md)), texto de borda é `coerce` ([§2.3](02-type-system.md)). Dentro de `coerce from string`, `self` é a `string` de entrada — por isso `self.uppercase()` é legal ali.
- `ref T` não tem métodos ([§2.7](02-type-system.md)): só `==`/`!=` e, com `generation: system`, ordenação. `walletId.toString` → **erro de compilação** (seria unwrap).
- `File`, `FileStream` e `FileRef` não têm métodos: as operações são `store`, `load File(...)`, `signed_url(...)` e `delete file(...)` ([§2.5](02-type-system.md)).

### 2.8.2. Parênteses opcionais

**Regra.** Método de aridade 0 pode ser invocado sem parênteses. `self.length` e `self.length()` são a **mesma** expressão — açúcar léxico, resolvido antes de qualquer análise semântica.

```ds
ValueObject TransactionDescription(string) {
    Valid { self.length <= 256 }        // ≡ self.length()
}
```

| Questão | Regra |
|---------|-------|
| Quais métodos | Todo método de aridade 0 **deste catálogo**, em qualquer tipo — não só primitivos |
| Métodos com parâmetros | ❌ `self.contains "@"` → erro de sintaxe. Parênteses obrigatórios |
| `Operator` de VO | ❌ Não é método; não tem forma de chamada ([§2.8.1](02-type-system.md)) |
| Encadeamento | ✅ `self.name.trim.uppercase` ≡ `self.name.trim().uppercase()`; grafias podem se misturar na mesma cadeia |
| À esquerda de `=` | ❌ **Erro de compilação**. `self.length = 3`, `state.tags.count = 0` — chamada nunca é alvo de atribuição, e não há propriedade para atribuir |
| Em posição de statement | ❌ **Erro de compilação**. Mutador invocado pelo efeito exige parênteses: `state.tags.clear()`, nunca `state.tags.clear` |
| Warning ao escolher uma grafia | Nenhum. As duas são canônicas |

**Exceção deliberada a "Uma Forma Canônica" ([§1.1](01-overview.md)).** É a única da linguagem, e é bem-comportada: §1.1 proíbe duas *construções* para a mesma operação; aqui há uma construção só — a chamada — com duas grafias que o front-end colapsa em uma antes de resolver nomes. Como a linguagem não tem propriedades, `self.length` não pode denotar outra coisa. O ganho é que a leitura de um `Valid` acompanha a frase de negócio ("a descrição tem no máximo 256 caracteres") sem introduzir um segundo conceito no sistema de tipos. Emitir warning por qualquer das grafias está **proibido** — recusaria os próprios exemplos desta spec (`self.length` na [§2.2](02-type-system.md), `availableTickets.count()` na [§19.2](19-transactions-sagas.md)).

### 2.8.3. Invariantes do catálogo

1. **Totalidade.** Nenhum método deste catálogo falha. Não há método parcial, não há índice fora de range, não há chave ausente, não há parse que possa não casar. Operação que seria parcial ou não está no catálogo, ou tem assinatura total (`get(k, default)`, `take(n)` saturante).
2. **Pureza e determinismo.** Nenhum método lê relógio, gera aleatoriedade, faz I/O, aloca identidade ou consulta storage. Mesmo receptor e mesmos argumentos → mesmo resultado, hoje e no replay. Por isso todo o catálogo é legal em `Apply`, que a [§10.4](10-ffi.md) descreve como dependente "só do evento e de built-ins". Utilitário não determinístico é função livre da [§2.6](02-type-system.md) (`now()`, `uuid()`, `random()`), **nunca** método — e continua sujeito à captura em evento.
3. **Imutabilidade dos valores.** Métodos de `string`, `bytes`, `integer`, `decimal`, `boolean` e `datetime` nunca mutam o receptor: devolvem valor novo. Só coleções têm mutadores.
4. **Mutador é statement.** `add`, `remove`, `clear`, `put` não devolvem valor. Usar em posição de expressão (`x = set.add(v)`, `ensure list.add(v)`) → **erro de compilação**. Presença se consulta com `contains`/`containsKey`, não pelo retorno da mutação.
5. **Ausência não é valor.** Não existe `null`, `Option`, tipo anulável nem valor sentinela na linguagem. Ausência é decidida no ponto de leitura — por `containsKey` antes, por `default` em `get`, ou por `ensure x exists else <Error>` no caso de `load`/`focus`.

Toda violação desta seção é **erro de compilação**. A seção não introduz nenhum warning.

### 2.8.4. `string`

Comprimento e recortes em **runes** (caracteres Unicode), nunca em bytes.

| Método                            | Retorno        | Semântica                                                                                          |
| --------------------------------- | -------------- | -------------------------------------------------------------------------------------------------- |
| `length`                          | `integer`      | Número de runes                                                                                    |
| `isEmpty`                         | `boolean`      | `length == 0`                                                                                      |
| `uppercase`                       | `string`       | Caixa alta, invariante de locale                                                                   |
| `lowercase`                       | `string`       | Caixa baixa, invariante de locale                                                                  |
| `trim`                            | `string`       | Remove espaço em branco das duas pontas                                                            |
| `contains(s string)`              | `boolean`      | Subcadeia; `s` vazia → `true`                                                                      |
| `startsWith(s string)`            | `boolean`      | Prefixo                                                                                            |
| `endsWith(s string)`              | `boolean`      | Sufixo                                                                                             |
| `split(sep string)`               | `List<string>` | `sep` vazio → **erro de compilação** quando literal, senão devolve a string inteira em um elemento |
| `replace(old string, new string)` | `string`       | **Todas** as ocorrências; `old` vazio → receptor inalterado                                        |
| `take(n integer)`                 | `string`       | Primeiras `n` runes; satura em `length`                                                            |
| `skip(n integer)`                 | `string`       | Descarta as `n` primeiras runes; satura em `length`                                                |

`take`/`skip` são o único recorte de `string`: não há `substring`, `indexOf` nem indexação — o par satura e por isso é total ([§2.8.3](02-type-system.md)), e reaproveita o vocabulário de paginação da [§6.3](06-read-side.md). Argumento literal negativo → **erro de compilação**; em runtime, negativo satura em `0`.

Conversão de texto para número **não** está no catálogo: `"12".toInteger` → **erro de compilação**. Parsing é coerção de borda ([§2.3](02-type-system.md)) ou `Foreign pure function` ([§10](10-ffi.md)) — só esses dois têm onde declarar a falha. `string` também não tem `toString`: seria a identidade, e duas grafias para o mesmo valor contrariam [§1.1](01-overview.md).

```ds
ValueObject Email(string) {
    Valid { self.contains("@") and self.trim.length == self.length }
}

ValueObject HolderName(string) {
    Valid { self.length >= 2 and self.length <= 120 }
}
```

### 2.8.5. `integer`, `decimal`, `boolean`

| Receptor | Método | Retorno | Semântica |
|----------|--------|---------|-----------|
| `integer` | `abs` | `integer` | Valor absoluto |
| `integer` | `min(other integer)` / `max(other integer)` | `integer` | Menor / maior |
| `integer` | `toDecimal` | `decimal` | Exata |
| `integer` | `toString` | `string` | Base 10, sem separador |
| `decimal` | `abs` | `decimal` | Valor absoluto |
| `decimal` | `min(other decimal)` / `max(other decimal)` | `decimal` | Menor / maior |
| `decimal` | `round(places integer)` | `decimal` | Meio-para-cima, afastando de zero. `places` literal negativo → erro de compilação |
| `decimal` | `floor` / `ceil` | `decimal` | Inteiro imediatamente abaixo / acima, como `decimal` |
| `decimal` | `toInteger` | `integer` | Trunca em direção a zero |
| `decimal` | `toString` | `string` | Decimal canônico, sem notação científica |
| `boolean` | `toString` | `string` | `"true"` / `"false"` |

`boolean` tem exatamente um método: negação e conjunção são operadores (`not`, `and`, `or`), não métodos.

### 2.8.6. `datetime`

`datetime` é um **instante em UTC**. Todo componente é lido em UTC; a linguagem não tem tipo de fuso, e apresentação local é responsabilidade da borda.

| Método | Retorno | Semântica |
|--------|---------|-----------|
| `year` / `month` / `day` | `integer` | Componentes de data, UTC. `month` 1–12, `day` 1–31 |
| `hour` / `minute` / `second` | `integer` | Componentes de hora, UTC |
| `startOfDay` | `datetime` | Meia-noite UTC do mesmo dia |
| `plus(d)` / `minus(d)` | `datetime` | Desloca por uma **duração literal** |
| `secondsSince(other datetime)` | `integer` | Segundos decorridos de `other` até o receptor; negativo se `other` for posterior |
| `epochSeconds` | `integer` | Segundos desde 1970-01-01T00:00:00Z |
| `toString` | `string` | RFC 3339 em UTC |

A duração de `plus`/`minus` é sempre **literal**, na mesma grafia de [§13](13-module-infra.md) e [§19.3.1](19-transactions-sagas.md) — `500ms`, `30s`, `15min`, `24h`, `7d`. Expressão não-literal nessa posição → **erro de compilação**. Não existe tipo `duration` de primeira classe.

Comparação de instantes é operador relacional (`<`, `<=`, `>`, `>=`, `==`), não método: `isBefore`/`isAfter` → **erro de compilação**.

```ds
Aggregate Ticket {
    ...
    Handle Reserve(orderId ref Order, holder HolderName) {
        emit TicketReserved(self.id, orderId, holder, now().plus(15min))   // reserva expira em 15min
    }
}
```

### 2.8.7. `bytes`

| Método | Retorno | Semântica |
|--------|---------|-----------|
| `length` | `integer` | Número de bytes |
| `isEmpty` | `boolean` | `length == 0` |
| `contains(b bytes)` | `boolean` | Subsequência |
| `take(n integer)` / `skip(n integer)` | `bytes` | Saturantes, como em `string` |
| `toBase64` | `string` | Base64 padrão, com padding |
| `toHex` | `string` | Hexadecimal minúsculo |

`bytes` → `string` textual não está no catálogo (exigiria decidir validade UTF-8): use `toBase64`/`toHex`, ou `Foreign pure function` ([§10](10-ffi.md)).

### 2.8.8. Coleções

**Ordem é determinística em todas as quatro.** `List<T>` e `AppendList<T>`: ordem de inserção/append. `Set<T>`: ordem de inserção (o conjunto não duplica, mas itera estável). `Map<K,V>`: ordem de inserção de chave, e `values` na mesma ordem de `keys`. Isso vale para `for` ([§3.3](03-control-flow.md)), para `keys`/`values` e para `skip`/`take`.

**Consultas — `List<T>`, `AppendList<T>`, `Set<T>`:**

| Método | Retorno | Semântica |
|--------|---------|-----------|
| `count` | `integer` | Cardinalidade |
| `isEmpty` | `boolean` | `count == 0` |
| `contains(v T)` | `boolean` | Pertencimento por igualdade de `T` |
| `any(f)` | `boolean` | Existe elemento que satisfaz `f`; coleção vazia → `false` |
| `all(f)` | `boolean` | Todos satisfazem `f`; coleção vazia → `true` |
| `sum(f)` | `integer` / `decimal` | Soma da projeção; coleção vazia → `0` |
| `skip(n integer)` / `take(n integer)` | `List<T>` | Recorte na ordem de iteração; saturantes |
| `focus(id)` | `T` | Carregamento pontual — semântica em [§22](22-smart-partial-loading.md) |

`f` é a forma `x => <expressão>` da [§22](22-smart-partial-loading.md). A projeção de `sum` deve ser `integer` ou `decimal`; projeção de outro tipo → **erro de compilação**. Para somar valor monetário, projete o campo numérico e reconstrua o VO.

**Mutadores:**

| Receptor | Método | Semântica |
|----------|--------|-----------|
| `List<T>` | `add(v T)` | Acrescenta ao fim |
| `List<T>` | `remove(v T)` | Remove a **primeira ocorrência por valor**; ausente → no-op |
| `List<T>` | `clear()` | Esvazia |
| `AppendList<T>` | `add(v T)` | Acrescenta ao fim. `remove`/`clear`/reatribuição → ❌ erro ([§2.4](02-type-system.md), [§25](25-compilation-rules.md)) |
| `Set<T>` | `add(v T)` | Insere; valor já presente → no-op silencioso |
| `Set<T>` | `remove(v T)` | Remove por valor; ausente → no-op |
| `Set<T>` | `clear()` | Esvazia |

**Remoção é sempre por valor ou por chave, nunca por posição** — não existe índice na linguagem. Nenhum mutador devolve valor ([§2.8.3](02-type-system.md)): `Set.add` não informa se inseriu, e o interesse legítimo por essa informação se escreve `set.contains(v)` antes.

**`Map<K, V>`:**

| Método | Retorno | Semântica |
|--------|---------|-----------|
| `count` | `integer` | Número de entradas |
| `isEmpty` | `boolean` | `count == 0` |
| `containsKey(k K)` | `boolean` | Presença da chave |
| `get(k K, default V)` | `V` | Valor associado; chave ausente → `default` |
| `keys` | `List<K>` | Chaves em ordem de inserção |
| `values` | `List<V>` | Valores na ordem de `keys` |
| `put(k K, v V)` | — | Insere ou sobrescreve |
| `remove(k K)` | — | Remove por chave; ausente → no-op |
| `clear()` | — | Esvazia |

`get` tem **dois** parâmetros, sempre. `m.get(k)` de um argumento → **erro de compilação**, com o `default` como forma indicada. É assim que a linguagem devolve um valor total sem `null` e sem `Option`: quem não tem default razoável testa `containsKey` antes e ramifica com `match` ([§3.2](03-control-flow.md)); quem lida com identidade de Aggregate não usa `Map`, usa `load`/`focus` e `ensure ... exists else <Error>` ([§2.7](02-type-system.md)).

Tipo de chave: `K` deve ser `string`, `integer`, `boolean`, `datetime`, Enum, `ref T` ([§2.7](02-type-system.md)) ou ValueObject wrapper sobre um desses. Qualquer outro → **erro de compilação**. `Map<K,V>` não é iterável diretamente: `for e in mapa` → **erro de compilação**; itere `keys` ou `values`.

```ds
Aggregate Wallet {
    Apply Deposited {
        state.balance = state.balance + event.amount
        state.entries.add(StatementEntry(
            type: TransactionType.Deposit,
            amount: event.amount,
            description: event.description,
            date: event.timestamp
        ))
    }
}

ValueObject Statement {
    entries List<StatementEntry>
    labels Map<TransactionType, string>

    Valid { entries.count <= 100 and labels.get(TransactionType.Deposit, "Depósito").length > 0 }
}
```

### 2.8.9. Indexação, aritmética e falhas de runtime

**Indexação não existe.** `l[0]`, `m["k"]`, `s[3]` → **erro de compilação** em qualquer coleção, `string` ou `bytes`. A razão é a [§23](23-error-classification.md): acesso fora de range não é erro de negócio (não é regra do domínio e não seria declarável como `Error`) nem erro de infraestrutura (não é falha de dependência externa, e retry ou circuit breaker não o consertam) — e a §23 admite exatamente duas classes. Em vez de abrir uma terceira, a linguagem **remove o modo de falha**: percorra com `for` ([§3.3](03-control-flow.md)), recorte com `skip`/`take`, teste com `contains`, acesse pontualmente com `focus` ([§22](22-smart-partial-loading.md)), leia mapa com `get(k, default)`. É a mesma disciplina que já eliminou `null`, `if` e `while`.

**Aritmética.** Operadores, não métodos — o nível de suporte segue [§27](27-evolving-features.md). O que a linguagem garante hoje:

| Situação | Garantia |
|----------|----------|
| `integer` | Inteiro com sinal de 64 bits — a mesma representação de [§2.7](02-type-system.md) para identidade. Overflow **não é verificado**: o resultado envolve, como no alvo de transpilação. Valor monetário nunca é `integer`; é `decimal` dentro de um VO como `Money` ([§2.2](02-type-system.md)) |
| `decimal` | Decimal de precisão fixa: 38 dígitos significativos, escala máxima de 9 casas. `+`, `-`, `*` exatos dentro desse envelope; `/` arredonda meio-para-cima na 9ª casa |
| Divisão por zero | Divisor **literal** `0` → ❌ erro de compilação. Em runtime, falha de infraestrutura (5xx, [§23](23-error-classification.md)): não é declarável como `Error` e não é capturável no domínio. Domínio que precise tratar o caso escreve `ensure divisor != 0 else <Error>` antes da divisão |
| Estouro de magnitude de `decimal` | Mesma classe da divisão por zero |
| Conversão implícita entre primitivos | Não existe. Só as conversões explícitas de [§2.8.5](02-type-system.md)–[§2.8.7](02-type-system.md), todas totais |

### 2.8.10. Onde os métodos podem ser chamados

| Contexto | Consultas | Mutadores de coleção |
|----------|-----------|----------------------|
| `Valid`, `Operator`, `coerce` de ValueObject | ✅ | ❌ Erro — VO é imutável |
| `Handle` | ✅ | ✅ sobre coleção local |
| `Apply` | ✅ | ✅ — é onde o `state` muta |
| UseCase, Saga (`up`/`down`), Policy, Worker | ✅ | ✅ |
| Query, `visibility` de View, Projection | ✅ | ❌ Erro — leitura é pura |
| `*.test.ds` ([§24](24-testing.md)) | ✅ | ✅ |

`Apply` aceita o catálogo **inteiro** porque o catálogo inteiro é determinístico ([§2.8.3](02-type-system.md)) — inclusive `split`, que aloca uma `List<string>`: alocar coleção é determinístico, alocar *identidade* não é, e por isso `new_ref` está barrado em `Apply` ([§2.7](02-type-system.md)) e nenhum método aloca identidade.

Quem muta `state` continua sendo decidido fora daqui: `state` de Aggregate só em `Apply` ([§4](04-domain-core.md)), `state` de Saga em `up`/`down` ([§19](19-transactions-sagas.md)). Este catálogo não afrouxa essas regras — apenas diz quais chamadas existem.

### 2.8.11. O que não existe

Chamar qualquer um destes é **erro de compilação**, reportado pelo `check`:

| Ausente | Forma canônica |
|---------|----------------|
| `filter`, `map`, `reduce`, `sort`, `groupBy`, `avg`, `min`/`max` de coleção | `Query`/`list` com `where`, `orderBy`, `skip`/`take` ([§6.3](06-read-side.md)); agregações adicionais em [§27](27-evolving-features.md) |
| `first`, `last`, `elementAt`, `indexOf` | `for` ([§3.3](03-control-flow.md)), `focus` ([§22](22-smart-partial-loading.md)) |
| `substring`, indexação de `string` | `take`/`skip` |
| `toInteger`/`toDecimal` sobre `string`, `toString` sobre `bytes` | `coerce` ([§2.3](02-type-system.md)) ou `Foreign pure function` ([§10](10-ffi.md)) |
| `isBefore`/`isAfter` de `datetime` | Operadores relacionais |
| Métodos sobre `ref T`, Enum, `File`/`FileRef`/`FileStream`, ValueObject | [§2.8.1](02-type-system.md) |
| Método declarado pelo usuário | `Operator` de VO ([§2.2](02-type-system.md)), `Foreign` ([§10](10-ffi.md)) |

Método inexistente (`value.frobnicate()`), método existente com aridade errada (`self.replace("a")`), método existente no tipo errado (`lista.length`, `texto.count`) e grafia sem parênteses em método com parâmetros: os quatro são **erro de compilação no `check`**. Nenhum deles pode chegar à geração de código — se compila, a chamada existe.

