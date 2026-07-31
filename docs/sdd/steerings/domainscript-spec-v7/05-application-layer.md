# 5. Camada de Aplicação

## 5.1. Commands

ValueObjects/Enums obrigatórios. `ref` para Type Safety. **Idempotency key implícita** em todo Command que muta estado (seção 15).

```ds
Command DepositCmd {
    walletId ref Wallet
    amount Money
    description TransactionDescription
}

Command TransferCmd {
    fromWalletId ref Wallet
    toWalletId ref Wallet
    amount Money
    description TransactionDescription
}
```

## 5.2. UseCases

Unit of Work implícito. Timeout com herança módulo → UseCase.

```ds
UseCase PerformDeposit handles DepositCmd {
    timeout 5s
    execute {
        wallet = load Wallet(cmd.walletId)
        ensure wallet exists else WalletNotFound
        wallet.Deposit(cmd.amount, cmd.description)
    }
}

UseCase PerformTransfer handles TransferCmd {
    execute {
        from = load Wallet(cmd.fromWalletId)
        ensure from exists else WalletNotFound
        to = load Wallet(cmd.toWalletId)
        ensure to exists else WalletNotFound

        from.SendTransfer(cmd.toWalletId, cmd.amount, cmd.description)
        to.ReceiveTransfer(cmd.fromWalletId, cmd.amount, cmd.description)
        // mesmo Database → commit atômico
    }
}
```

## 5.3. Application Events

Um `Event` é fato **de uma instância**: nasce num `Handle`, pertence ao stream do Aggregate que o emitiu e reconstrói o estado dele ([§4.2.3](04-domain-core.md)). Nem todo fato da aplicação tem instância emissora. "A transferência entre W1 e W2 concluiu" não é fato de `W1` nem de `W2` — é fato do **procedimento** que coordenou as duas. Esse é o `ApplicationEvent`: fato de **escopo de requisição**, emitido onde há corpo executável e não há Aggregate.

Toda interação com a aplicação é uma requisição; o `ApplicationEvent` é o que essa requisição tem a dizer sobre si mesma. Não é fonte da verdade, não é replayável, não substitui evento de domínio ([§5.3.4](05-application-layer.md)).

**Declaração.** Duas grafias, na mesma divisão de [§4.2](04-domain-core.md):

```ds
ApplicationEvent TransferCompleted { from ref Wallet, to ref Wallet, amount Money }
PublicApplicationEvent DepositRejected { walletId ref Wallet, amount Money, reason RejectionReason }
```

| Grafia | Alcance | Onde se declara |
|--------|---------|-----------------|
| `ApplicationEvent` | Interno ao módulo | Qualquer `*.ds` do módulo |
| `PublicApplicationEvent` | Compartilhado entre módulos | `contracts/*.ds` ([§1.3](01-overview.md)), como `PublicEvent` |

Não há modificador de visibilidade: a grafia **é** a marcação, e introduzir um segundo mecanismo de compartilhamento contrariaria "Uma Forma Canônica" ([§1.1](01-overview.md)). Policy de outro módulo escutando `ApplicationEvent` privado → **erro de compilação**, a mesma regra que [§25](25-compilation-rules.md) já aplica a `Event`.

| Regra | Resultado |
|-------|-----------|
| **Regra de Ouro** ([§2.1](02-type-system.md)) nos campos declarados | ✅ Vale integralmente — é Write Side. `ApplicationEvent E { reason string }` → ❌ erro de compilação; embrulhe em ValueObject ou Enum |
| `ref T` em campo ([§2.7](02-type-system.md)) | ✅ Permitido em qualquer posição — é como um evento de escopo de requisição nomeia as instâncias que tocou |
| Nome já usado por `Event`, `PublicEvent`, Command, ValueObject, Enum ou Aggregate do módulo | ❌ Erro de compilação — espaço de nomes único |
| `Apply <ApplicationEvent>` em qualquer Aggregate | ❌ Erro de compilação ([§5.3.4](05-application-layer.md)) |
| Campo `redactable` ou `Upcast` sobre a declaração | ❌ Erro de compilação ([§5.3.6](05-application-layer.md)) |
| Declarado e nunca emitido | ⚠️ Warning — declaração morta |
| Emitido e sem consumidor algum (Policy, `Metric`, `refreshOn`) | ⚠️ Warning — canal de notificação sem ouvinte |

### 5.3.1. O Envelope

Payload e envelope se separam exatamente como em [§4.2.3](04-domain-core.md): o autor declara o payload, o compilador gera o envelope, o runtime o preenche. O envelope nunca é declarado, nunca é escrito, nunca é passado ao `emit`.

**Os quatro campos.**

| Campo | Tipo | Semântica | Quem preenche |
|-------|------|-----------|---------------|
| `eventId` | `EventId` | Idêntico a [§4.2.3](04-domain-core.md) — mesmo tipo opaco, mesmas operações, mesma unicidade global | Runtime, na emissão |
| `eventType` | `string` | Idêntico a [§4.2.3](04-domain-core.md) — nome qualificado da declaração, `"Carteira.TransferCompleted"` | Compilador — constante por declaração |
| `timestamp` | `datetime` | Idêntico a [§4.2.3](04-domain-core.md) — instante UTC da emissão | Runtime, na emissão |
| `procedureName` | `string` | Nome qualificado do **procedimento emissor** | Compilador — constante por **sítio de `emit`** |

Os três primeiros são os universais que [§4.2.3](04-domain-core.md) fixou; esta seção não os redefine e não os altera — semântica, tipos, operações permitidas, forma na borda e comportamento em `*.test.ds` são os de lá. O par `aggregateId`/`sequence` **não existe** aqui: sem instância emissora não há identidade a carregar nem stream em que ocupar posição. Ler `event.aggregateId` ou `event.sequence` de um `ApplicationEvent` → **erro de compilação**, como [§4.2.3](04-domain-core.md) já determina.

O conjunto é **fechado**: são exatamente estes quatro. **Não há campo de correlação de requisição.** [§4.2.3](04-domain-core.md) fechou a superfície da linguagem contra metadata de correlação, causação, tenant e trace, e um `requestId` seria um segundo mecanismo de correlação ao lado do que o runtime já carrega: trace context é propagado automaticamente inclusive através de canais ([§12](12-topology.md)), anexado a todo `log` ([§3.4](03-control-flow.md)) e exportado por [§20](20-observability.md). Rastreabilidade de requisição é assunto de [§20](20-observability.md); o que o envelope acrescenta é **qual procedimento** produziu o fato, que é o que a telemetria não sabe declarar.

**O que `procedureName` nomeia.** O procedimento executável que contém o `emit`, qualificado pelo módulo:

| Emissor | `procedureName` |
|---------|-----------------|
| UseCase `PerformTransfer` do módulo `Carteira` | `"Carteira.PerformTransfer"` |
| Policy `NotifyOnDeposit` do módulo `Carteira` | `"Carteira.NotifyOnDeposit"` |
| Worker `DailySettlement` do módulo `Carteira` | `"Carteira.DailySettlement"` |
| `up` do step `ProcessPayment` da Saga `PurchaseTickets` | `"Ingressos.PurchaseTickets.ProcessPayment.up"` |
| `down` do mesmo step | `"Ingressos.PurchaseTickets.ProcessPayment.down"` |

Saga qualifica por step **e por fase**: `up` e `down` são procedimentos distintos com semânticas opostas — avanço e compensação — e um consumidor que não os distingue não consegue auditar a compensação ([§19.3.3](19-transactions-sagas.md)).

**`procedureName` é `string`, não Enum.** Pela mesma razão que `eventType` é ([§4.2.3](04-domain-core.md)): não existe literal de tipo na linguagem, e um Enum gerado pelo compilador seria um tipo que ninguém declara, com pertencimento mudando a cada procedimento novo e entrando na exaustividade de `match` ([§3.2](03-control-flow.md)). A grafia não fica frouxa porque o compilador valida o literal — as mesmas regras de `eventType`:

| Forma | Resultado |
|-------|-----------|
| `event.procedureName == "Carteira.PerformTransfer"` | `boolean` |
| Literal que não nomeia procedimento declarado no programa | ❌ Erro de compilação |
| Nome sem o módulo (`"PerformTransfer"`), ou step de Saga sem a fase | ❌ Erro de compilação — uma forma canônica ([§1.1](01-overview.md)) |
| `<` `>` `<=` `>=`, `contains`, `startsWith` e demais métodos de `string` | ❌ Erro de compilação — compara por igualdade, não é texto manipulável |
| Comparação onde o compilador vê um único sítio de `emit` para o tipo | ⚠️ Warning — o resultado é constante |

**Múltiplos emissores são legais** — e é o contraste deliberado com [§4.2.3](04-domain-core.md). Lá, dois Aggregates emitindo o mesmo `Event` é erro, porque `aggregateId` é derivado pelo compilador do emissor único. Aqui `procedureName` é constante do **sítio**, não da declaração: `TransferCompleted` emitido pelo UseCase e pela Saga de compensação é um programa válido, e o envelope de cada emissão diz qual dos dois foi.

**Isenção da Regra de Ouro no envelope.** Como em [§4.2.3](04-domain-core.md), e pela mesma razão: metadata não é declarado. `timestamp datetime`, `eventType string` e `procedureName string` são primitivos legais, e a isenção vale **só** para estes quatro campos.

**Readonly e colisão.**

| Situação | Resultado |
|----------|-----------|
| Atribuir a qualquer campo do envelope | ❌ Erro de compilação |
| Declarar campo chamado `eventId`, `eventType`, `timestamp` ou `procedureName` | ❌ Erro de compilação — colide com o envelope |
| Declarar campo chamado `aggregateId` ou `sequence` | ❌ Erro de compilação — nomes reservados do envelope de evento, mesmo onde não se aplicam |
| Passar campo do envelope a `emit` | ❌ Erro de compilação — `emit` só recebe payload |
| Declarar campo de tipo `EventId` | ❌ Erro de compilação ([§4.2.3](04-domain-core.md)) |

**Forma na borda.** `eventId`, `eventType` e `timestamp` atravessam como em [§4.2.3](04-domain-core.md). `procedureName` → string JSON; gRPC `string`.

### 5.3.2. Onde se emite

O verbo é **`emit`** — o mesmo. Não há verbo próprio: seria uma segunda construção para a mesma operação ([§1.1](01-overview.md)). Quem decide a legalidade é o par (contexto, tipo declarado do nome emitido), e o compilador já resolve o nome antes de checar o contexto.

| Contexto | `emit <Event>` | `emit <ApplicationEvent>` |
|----------|----------------|---------------------------|
| `Handle` de Aggregate | ✅ | ❌ Erro de compilação |
| `Apply` | ❌ Erro | ❌ Erro — hermético ([§10.4](10-ffi.md)) |
| UseCase `execute` | ❌ Erro | ✅ |
| Policy `execute` | ❌ Erro | ✅ |
| Worker `execute`, nos três modos | ❌ Erro | ✅ |
| Saga, `up` de step | ❌ Erro | ✅ |
| Saga, `down` de step | ❌ Erro | ✅ |
| Corpo da Saga fora de um `step` | ❌ Erro | ❌ Erro — não há corpo executável ali |
| Query, `visibility` de View, `map` de Projection | ❌ Erro | ❌ Erro — leitura é pura ([§2.8.10](02-type-system.md)) |
| Adapter (`body`, `map`, `response`) | ❌ Erro | ❌ Erro — Adapter é mapeamento declarativo, não corpo executável ([§9.3](09-notifications-adapters.md)) |
| ValueObject (`Valid`, `Operator`, `coerce`) | ❌ Erro | ❌ Erro |
| `Upcast` | ❌ Erro | ❌ Erro |

**`emit` de `Event` de domínio fora de um Aggregate → ❌ erro de compilação.** É a resolução da linha "`E` emitido só fora de Aggregate" de [§4.2.3](04-domain-core.md): essa declaração não é um `Event` — é um `ApplicationEvent`, e o diagnóstico diz isso. Com essa regra, todo `Event` do programa tem exatamente um Aggregate emissor, que é a premissa de que [§4.2.3](04-domain-core.md) deriva `aggregateId`.

**`emit` de `ApplicationEvent` dentro de um Aggregate → ❌ erro de compilação.** Um `Handle` produz o fato da instância, e esse fato é o que o `Apply` reexecuta no replay; um evento sem `aggregateId` emitido ali teria instância emissora e nenhum lugar no stream dela — exatamente a ambiguidade de envelope que [§4.2.3](04-domain-core.md) recusa. Fato que o Aggregate conhece é `Event`; coordenação entre Aggregates é do procedimento que os coordena.

**`emit` não vincula receptor.** Emitir não traz `event` para o escopo. Numa Policy, `event` continua sendo o evento **consumido**, antes e depois do `emit` ([§4.2.3](04-domain-core.md)); num UseCase, Saga ou Worker não há `event` em escopo, e `event.*` continua sendo erro de compilação.

### 5.3.3. Quem consome

| Consumidor | ApplicationEvent |
|------------|------------------|
| Policy ([§7](07-policies.md)) — `Policy P on X` | ✅ Mesma sintaxe, mesmo `delivery`. É o consumidor canônico |
| `Metric M ... on X` ([§20](20-observability.md)) | ✅ Envelope legível em `value` e `labels` — os quatro |
| Projection, `refreshOn [X]` ([§6.4](06-read-side.md)) | ✅ Como **gatilho** de recomputação |
| Projection, bloco `map` / View | ❌ Erro de compilação — projetam estado de Aggregate; `event.*` não está em escopo ([§4.2.3](04-domain-core.md)) |
| Worker ([§8](08-workers.md)) | ❌ Não existe `on E` em Worker: Worker é agendado, reação a evento é Policy |
| `Apply` de Aggregate | ❌ Erro de compilação ([§5.3.4](05-application-layer.md)) |
| Policy de outro módulo, sobre `ApplicationEvent` privado | ❌ Erro de compilação |
| Policy de outro módulo, sobre `PublicApplicationEvent` | ✅ Pelo canal declarado em [§12](12-topology.md); sem canal → ❌ erro |

Alimentar uma projeção e disparar sua recomputação são coisas diferentes: `refreshOn` diz **quando** recomputar, `map` diz **de onde** vêm os campos, e os campos vêm sempre de `source`. Um `ApplicationEvent` faz a primeira, nunca a segunda.

Dentro do corpo do consumidor o receptor é `event` e o envelope legível é o de [§5.3.1](05-application-layer.md) — os quatro campos, `aggregateId` e `sequence` → ❌ erro de compilação.

### 5.3.4. Persistência, Transação e Replay

**Não é persistido no event store, e não é replayável.** Com todas as letras, porque é a diferença conceitual central em relação ao evento de domínio.

O event store é particionado por stream, e o stream é a instância ([§4.2.3](04-domain-core.md)). Um `ApplicationEvent` não tem instância: não tem stream sob o qual gravar, não tem `sequence` que ocupar, e um stream sintético "por procedimento" seria um contador quase-global, incompatível com [§12](12-topology.md), guardando fatos que não reconstroem estado nenhum.

| Questão | Resposta |
|---------|----------|
| Vai para o event store? | ❌ Não |
| Aparece em `events()` ([§4.3](04-domain-core.md))? | ❌ Não — `events()` é o stream da instância |
| `Apply <ApplicationEvent>` | ❌ Erro de compilação — não existe estado a aplicar |
| Entra no replay de `EventSourced`? | ❌ Não. Apagar todo `ApplicationEvent` já emitido não muda o estado de nenhum Aggregate |
| `snapshot`, `from:`/`to:`, `since:`/`until:` | Não se aplicam |
| Que durabilidade existe | A do **Outbox** ([§13](13-module-infra.md)): registro transitório, retido até a entrega, descartado depois. Nunca consultável, nunca reprocessável a pedido |

**Consequência normativa:** um `ApplicationEvent` nunca é a única testemunha de um fato. Fato que precisa sobreviver à entrega é `Event` de domínio, ou é estado de Aggregate. O `ApplicationEvent` é canal de notificação; o que ele carrega já é verdade em outro lugar.

**Transação.** A emissão é publicada quando a fronteira transacional que a contém confirma:

| Emissor | Fronteira | Publica | Se a fronteira não confirma |
|---------|-----------|---------|-----------------------------|
| UseCase | Unit of Work implícito ([§5.2](05-application-layer.md)) | No **commit** | Rollback → **nunca publicado**, como se não tivesse sido emitido |
| Policy, Worker | A execução do `execute` | Ao concluir sem erro | Não publica; a retentativa de entrega ([§7](07-policies.md)) reexecuta o corpo inteiro |
| Saga, `up`/`down` | O **step** — a Saga não tem transação global ([§19.1](19-transactions-sagas.md)) | Ao fim do `up`/`down` que contém o `emit` | Não publica. Publicação de step já concluído **sobrevive à compensação** — é o que permite auditar uma Saga `Compensated` |

Entre tentativas do mesmo step vale [§19.3.5](19-transactions-sagas.md): `emit` é efeito não revertido, o `up` reemite e precisa tolerar publicação duplicada.

**O consumidor nunca roda dentro da transação do emissor.** Consumo é posterior à publicação, que é posterior ao commit. Uma Policy sobre `ApplicationEvent` não pode desfazer, bloquear nem influenciar o UseCase que o emitiu.

### 5.3.5. Ordenação, Entrega e Idempotência

| Questão | Regra |
|---------|-------|
| Ordem entre emissões da mesma fronteira | Ordem de emissão, preservada |
| Ordem entre fronteiras distintas | **Nenhuma** — só `timestamp`, a mesma regra de [§4.2.3](04-domain-core.md) para streams distintos |
| Ordem relativa a `Event` de domínio | **Nenhuma**. Não há `sequence` com que comparar |
| Entrega | A do consumidor: `delivery AtLeastOnce` ou `BestEffort` ([§7](07-policies.md)) |
| Deduplicação em `AtLeastOnce` | Por `eventId`, feita pelo runtime ([§4.2.3](04-domain-core.md)) — o autor não a escreve |
| Cross-módulo | Pelo Outbox ([§13](13-module-infra.md)) e pelo canal de [§12](12-topology.md), como `notify` assíncrono ([§9.4.2](09-notifications-adapters.md)) |
| `orderBy: aggregateId` no canal ([§12](12-topology.md)) | Não se aplica: não há chave de partição. Canal cujo tráfego é só `PublicApplicationEvent` e que declara `orderBy` → ⚠️ Warning — declaração inerte |
| Idempotência de Command ([§15](15-idempotency.md)) | Command reenviado com a mesma chave devolve o resultado em cache e **não** reexecuta o UseCase: não reemite |

Duas defesas distintas contra duplicata, e ambas já existem na linguagem: idempotência de Command a impede na **borda**; deduplicação por `eventId` a impede no **consumidor**.

### 5.3.6. Versionamento e Redação

| Mecanismo | Vale? | Por quê |
|-----------|-------|---------|
| Campo novo com `default` ([§4.2.1](04-domain-core.md)) | ✅ | É a compatibilidade que importa aqui: emissor e consumidor de um `PublicApplicationEvent` podem estar em versões diferentes enquanto a mensagem está em voo |
| `Upcast` ([§4.2.1](04-domain-core.md)) | ❌ Erro de compilação | `Upcast` reinterpreta payload **já persistido**, lido de novo depois de o código mudar. Nada aqui é persistido nem lido de novo: a mensagem é consumida em voo e descartada. Não há versão histórica a resolver |
| `redactable` ([§4.2.2](04-domain-core.md)) | ❌ Erro de compilação | Redação existe para apagar PII **do store** sem quebrar replay. Sem store e sem replay, não há o que redigir — o direito ao esquecimento se exerce sobre o `Event` de domínio e sobre o estado |

Um `ApplicationEvent` cujo payload precise de redação está carregando dado que deveria estar num `Event` de domínio.

### 5.3.7. Em `*.test.ds`

A asserção é `emitted X(...)` **sem instância qualificadora** — e a distinção é justamente essa: `Wallet("W1") emitted E(...)` afirma evento de domínio no stream de `W1` ([§24.2](24-testing.md)); `emitted X(...)` afirma `ApplicationEvent`, porque não há instância a nomear. É a forma que [§24.4](24-testing.md) já usa.

| Situação | Resultado |
|----------|-----------|
| `emitted X(...)` em `then { }` | Casa por shape parcial, como todo `then` ([§24.2](24-testing.md)) |
| `emitted count N` | Conta `ApplicationEvent`s da forma não qualificada |
| `T("alias") emitted X(...)` com `X` sendo `ApplicationEvent` | ❌ Erro — não há instância emissora |
| `ApplicationEvent` em `given` | ❌ Erro — `given` é histórico de stream, e ele não tem stream |
| `when event X(...)` dirigindo uma Policy sobre `X` | ✅ Como em [§24.4](24-testing.md) |
| Nomear campo do envelope em `then` | ❌ Erro — `then` casa payload ([§4.2.3](04-domain-core.md)) |
| `emitted X(...)` num cenário que termina `rolledback` | Falha do teste — rollback não publica ([§5.3.4](05-application-layer.md)) |
| `eventId` e `timestamp` no cenário | Atribuídos pelo runner, deterministicamente ([§4.2.3](04-domain-core.md)) |
| `procedureName` no cenário | Atribuído pelo runner a partir do procedimento que executou a emissão |

### 5.3.8. Exemplo

```ds
ValueObject RejectionReason(string) { Valid { self.length > 0 } }

ApplicationEvent TransferCompleted { from ref Wallet, to ref Wallet, amount Money }
PublicApplicationEvent DepositRejected { walletId ref Wallet, amount Money, reason RejectionReason }

UseCase PerformTransfer handles TransferCmd {
    execute {
        ensure cmd.fromWalletId != cmd.toWalletId else SameWalletTransfer
        from = load Wallet(cmd.fromWalletId)
        ensure from exists else WalletNotFound
        to = load Wallet(cmd.toWalletId)
        ensure to exists else WalletNotFound

        from.SendTransfer(cmd.toWalletId, cmd.amount, cmd.description)   // Event, no stream de W1
        to.ReceiveTransfer(cmd.fromWalletId, cmd.amount, cmd.description) // Event, no stream de W2

        emit TransferCompleted(cmd.fromWalletId, cmd.toWalletId, cmd.amount)  // fato do procedimento
    }
}

Policy AuditTransfers on TransferCompleted {
    delivery AtLeastOnce
    execute {
        log info "transferência concluída" {
            eventId = event.eventId              // EventId, opaco
            by      = event.procedureName        // "Carteira.PerformTransfer"
            at      = event.timestamp            // datetime
            kind    = event.eventType            // "Carteira.TransferCompleted"
            amount  = event.amount               // payload
        }
    }
}

Test PerformTransfer {
    scenario "transferência publica o fato do procedimento" {
        given Wallet("W1") from [
            WalletCreated(id: "W1", holder: "João", email: "joao@x.com"),
            DepositPerformed(id: "W1", amount: Money(100, "BRL"), description: "init")
        ]
        given Wallet("W2") from [ WalletCreated(id: "W2", holder: "Maria", email: "maria@x.com") ]
        when TransferCmd(fromWalletId: "W1", toWalletId: "W2", amount: Money(30, "BRL"), description: "x")
        then {
            Wallet("W1") emitted TransferSent(amount: Money(30, "BRL"))   // domínio: instância
            emitted TransferCompleted(amount: Money(30, "BRL"))           // aplicação: sem instância
            committed
        }
    }
}
```

Compensação numa Saga, onde o `procedureName` distingue avanço de reversão:

```ds
ValueObject CancellationReason(string) { Valid { self.length > 0 } }

ApplicationEvent PurchaseAbandoned { orderId ref Order, reason CancellationReason }

step ConfirmPurchase {
    up { ... }
    down {
        order = load Order(state.orderId)
        order.Cancel("Compensação")
        emit PurchaseAbandoned(state.orderId, CancellationReason("compensacao"))
        // procedureName = "Ingressos.PurchaseTickets.ConfirmPurchase.down"
    }
}
```

Erros que os trechos acima recusariam: `emit TransferCompleted(...)` dentro de um `Handle` de `Wallet` (ApplicationEvent em Aggregate), `emit TransferSent(...)` dentro do UseCase (`Event` de domínio fora de Aggregate — declare-o `ApplicationEvent`), `Apply TransferCompleted` (não há estado a aplicar), `event.aggregateId` na Policy `AuditTransfers` (não existe no envelope), `ApplicationEvent E { reason string }` (primitivo declarado no Write Side), `ApplicationEvent E { timestamp datetime }` (colide com o envelope), `Upcast TransferCompleted v1 -> v2` (não é persistido), `from ref Wallet redactable` (não é redigível), `event.procedureName.startsWith("Carteira")` (compara por igualdade), `Wallet("W1") emitted TransferCompleted(...)` num teste (não há instância emissora).

