# Identity aplicado ao `pizzeria` — cliente, gerente e cozinheiro

> Status: **exemplo de proposta**. Aplica [[Identity]] e [[Identity-API]] ao
> fixture `testdata/projects/pizzeria`. **Não é uma alteração do fixture**:
> `testdata/projects/` é escrito contra o que o transpilador aceita *hoje*, e
> nada disto compila. Os trechos abaixo são o "depois" hipotético, lado a lado
> com o "antes" real.

## 1. Ponto de partida — o que o fixture faz hoje

Autorização existe, autenticação não. Os `access` já citam papéis:

```ds
// sales/domain.ds — hoje
access {
    Create      requires caller.hasRole("store_manager")
    UpdatePrice requires caller.hasRole("store_manager")
}
access {
    Place               requires caller.authenticated
    ConfirmPayment      requires caller.hasRole("system_payment") or caller.hasRole("staff")
    CompletePreparation requires caller.hasRole("system_kitchen") or caller.hasRole("staff")
}
```

Cinco papéis em `string` nua — `store_manager`, `staff`, `system_payment`,
`system_kitchen`, `system_sales` — e **nada no projeto diz quem os atribui, de
onde vem o token, ou como alguém se autentica**. `POST /orders` limita
`perUser: 5/min` sem que "user" tenha definição. O `read.ds` carrega uma
divergência declarada em comentário:

```ds
// ⚠️ DIVERGÊNCIA ABERTA … "caller.id == self.customerId" compara CallerId com
// um ValueObject, e a §4.3.1 só admite comparação de vínculo contra "ref T".
View OrderVW From Order {
    visibility {
        customer requires caller.id == self.customerId or caller.hasRole("staff")
        …
    }
}
```

E `CustomerId(string)` existe só para dar um tipo ao dono do pedido que a
linguagem não sabia expressar.

## 2. Os três papéis

| | Cliente | Gerente | Cozinheiro |
|---|---|---|---|
| Ver cardápio | ✅ (público) | ✅ | ✅ |
| Fazer pedido | ✅ | ❌ | ❌ |
| Ver dados do **próprio** pedido | ✅ | ✅ (todos) | ❌ |
| Criar item / mudar preço | ❌ | ✅ | ❌ |
| Confirmar pagamento | ❌ | ✅ | ❌ |
| Ver painel da cozinha | ❌ | ✅ | ✅ |
| Assumir e finalizar ticket | ❌ | ❌ | ✅ |
| Cadastrar cozinheiro | ❌ | ✅ | ❌ |

Mais três principais que não são gente: `Sales`, `Kitchen` e o gateway de
pagamento (`Payments`) — hoje representados pelos papéis `system_*` sem dono.

## 3. `topology.ds` — o bloco `Identity`

Serviço único (`PizzeriaMonolith`), então um `Identity` só, e é nele que os
três papéis nascem:

```ds
Topology {
    services {
        PizzeriaMonolith {
            modules: [Sales, Kitchen]

            Identity {
                provider: local
                id: User                       // a pizzaria não modela Customer

                store {
                    provider: "postgres"
                    connection: env("IDENTITY_DB_URL")
                }

                password     { hasher: argon2id, minLength: 12, requireDigit: true }
                lockout      { maxAttempts: 5, window: 15min, duration: 15min }
                tokens       { access { ttl: 15min }, refresh { ttl: 30d, rotation: true } }
                confirmation { email: required }

                // sementes: o que o código cita. O catálogo segue mutável por
                // API — a rede de pizzarias pode criar "supervisor" sem recompilar.
                roles { customer, manager, cook, system_sales, system_kitchen, system_payment }

                serviceAccounts {
                    Sales    { roles: [system_sales] }
                    Kitchen  { roles: [system_kitchen] }
                    Payments { roles: [system_payment], credentials: clientSecret }
                }

                expose {
                    register      { public }                        // cliente se cadastra
                    login         { public }
                    refresh       { public }
                    logout        { requires caller.authenticated }
                    confirmEmail  { public }
                    resetPassword { public }
                    manage        { requires caller.authenticated }
                    users         { requires Manager }              // gerente cadastra cozinheiro
                }
            }
        }
    }

    channels { /* inalterado */ }
}
```

`id: User` é a decisão que dispensa o `Aggregate Customer` que este bounded
context nunca quis modelar ([[Identity]] §4.3): o principal é o `User` da lib
padrão, e é contra ele que a posse do pedido compara.

## 4. `access.ds` (raiz) — políticas de serviço

Só `caller`, logo escopo de serviço e execução na borda
([[Identity-API]] §5):

```ds
AccessPolicy Customer      { requires caller.hasRole(Role.customer) }
AccessPolicy Manager       { requires caller.hasRole(Role.manager) }
AccessPolicy Cook          { requires caller.hasRole(Role.cook) }

AccessPolicy Staff         { requires Manager or Cook }
AccessPolicy KitchenAccess { requires Cook or Manager }

AccessPolicy SalesSystem   { requires caller.hasRole(Role.system_sales) }
AccessPolicy KitchenSystem { requires caller.hasRole(Role.system_kitchen) }
AccessPolicy PaymentSystem { requires caller.hasRole(Role.system_payment) }
```

Oito nomes de domínio no lugar de dezenove ocorrências de `string` espalhadas
— e cada um testável isoladamente.

## 5. `sales/` — o que muda

### 5.1. `access.ds` do módulo — a posse

Referencia `self`, logo pertence ao módulo do agregado e executa no domínio:

```ds
AccessPolicy OrderOwner   { requires caller.id == self.customerId }
AccessPolicy OrderVisible { requires OrderOwner or Manager }
```

### 5.2. `domain.ds`

```diff
-// Identidade do cliente autenticado que fez o pedido …
-ValueObject CustomerId(string) {
-    Valid { value.length() > 0 }
-}
```

O VO some: o dono passa a ser uma referência tipada.

```diff
 Aggregate Order {
     state {
-        customerId CustomerId
+        customerId ref User          // caller.id : ref User — comparação nominal
         customer   CustomerName
         …
     }

     access {
-        Place               requires caller.authenticated
-        ConfirmPayment      requires caller.hasRole("system_payment") or caller.hasRole("staff")
-        StartPreparing      requires caller.hasRole("system_payment") or caller.hasRole("staff")
-        CompletePreparation requires caller.hasRole("system_kitchen") or caller.hasRole("staff")
+        Place               requires Customer
+        ConfirmPayment      requires PaymentSystem or Manager
+        StartPreparing      requires PaymentSystem or Manager
+        CompletePreparation requires KitchenSystem or Manager
     }

-    Handle Place(customerId CustomerId, …)
+    Handle Place(customerId ref User, …)
 }

 Aggregate MenuItem {
     access {
-        Create      requires caller.hasRole("store_manager")
-        UpdatePrice requires caller.hasRole("store_manager")
+        Create      requires Manager
+        UpdatePrice requires Manager
     }
 }
```

`Event OrderPlaced` acompanha: `customerId ref User`.

Repare que `Place requires Customer` **aperta** a regra de hoje
(`caller.authenticated`): um cozinheiro logado deixa de conseguir abrir pedido
em nome próprio pela API do cliente. É a granularidade que a `string` nua não
oferecia sem duplicar condição.

### 5.3. `application.ds` — de onde vem o dono

```diff
 UseCase PlaceOrder handles Place {
     execute {
         …
-        order.Place(customerId: cmd.customerId, customer: cmd.customer, …)
+        order.Place(customerId: caller.id, customer: cmd.customer, …)
     }
 }
```

A linha que o [[Identity-API]] §4.1 (Q1) precisa liberar. Ela é o ponto do
sistema inteiro em que o principal entra no domínio — e o motivo de
`cmd.customerId` ter que **deixar de existir**: enquanto o dono vier do
payload, qualquer cliente faz pedido em nome de outro.

### 5.4. `read.ds` — a divergência fecha

```diff
 View OrderVW From Order {
     visibility {
-        customer requires caller.id == self.customerId or caller.hasRole("staff")
-        phone    requires caller.id == self.customerId or caller.hasRole("staff")
-        total    requires caller.id == self.customerId or caller.hasRole("staff")
+        customer requires OrderVisible
+        phone    requires OrderVisible
+        total    requires OrderVisible
     }
 }
```

`caller.id : ref User` contra `self.customerId : ref User` é comparação nominal
— o comentário de divergência do fixture sai junto com o código que o motivava.

### 5.5. `interface.ds` — autorização de borda

```diff
 GET "/menu" -> GetAvailableMenu {
     rateLimit { perIp: 200/min }
 }

 POST "/orders" -> PlaceOrder {
+    requires Customer
     rateLimit { perUser: 5/min }        // perUser agora tem definição: caller.id
 }

-POST  "/admin/menu"            -> CreateMenuItem
-PATCH "/admin/menu/{id}/price" -> UpdateMenuItemPrice
-POST  "/admin/orders/{id}/pay" -> ConfirmOrderPayment
+POST  "/admin/menu"            -> CreateMenuItem       { requires Manager }
+PATCH "/admin/menu/{id}/price" -> UpdateMenuItemPrice  { requires Manager }
+POST  "/admin/orders/{id}/pay" -> ConfirmOrderPayment  { requires PaymentSystem or Manager }
```

As rotas `/admin/*` passam a rejeitar na borda, sem carregar agregado. O
`access` do `Aggregate` continua valendo — as duas checagens rodam
([[Identity-API]] §9, Q3), e é isso que mantém a regra viva quando o mesmo
Handle é disparado por uma `Policy` em vez de por HTTP.

## 6. `kitchen/` — o cozinheiro

```diff
-ValueObject CookId(string) {
-    Valid { value.length() > 0 }
-}
-
 Aggregate KitchenTicket {
     state {
         orderRef  ref Order
-        cookId    CookId
+        cookId    ref User
         …
     }

     access {
-        Create  requires caller.hasRole("system_sales")
-        AddItem requires caller.hasRole("system_sales")
-        Claim   requires caller.authenticated
-        Finish  requires caller.authenticated
+        Create  requires SalesSystem
+        AddItem requires SalesSystem
+        Claim   requires Cook
+        Finish  requires Cook
     }

-    Handle Claim(cookId CookId) { … }
+    Handle Claim(cookId ref User) { … }
 }
```

`Claim requires Cook` conserta um buraco real: hoje é `caller.authenticated`,
ou seja, **qualquer cliente logado pode assumir um ticket da cozinha**. E o
UseCase para de aceitar o cozinheiro por parâmetro:

```diff
 UseCase ClaimTicket handles Claim {
     execute {
         ticket = load KitchenTicket(cmd.id)
-        ticket.Claim(cookId: cmd.cookId)
+        ticket.Claim(cookId: caller.id)
     }
 }
```

Interface da cozinha:

```diff
-GET  "/kitchen/board"               -> GetBoardTickets
-POST "/kitchen/tickets/{id}/claim"  -> ClaimTicket
-POST "/kitchen/tickets/{id}/finish" -> FinishTicket
+GET  "/kitchen/board"               -> GetBoardTickets { requires KitchenAccess }
+POST "/kitchen/tickets/{id}/claim"  -> ClaimTicket     { requires Cook }
+POST "/kitchen/tickets/{id}/finish" -> FinishTicket    { requires Cook }
```

`Create`/`AddItem` sob `SalesSystem` param de depender de convenção: a `Policy`
`CreateTicketOnOrderPaid` roda sob a conta de serviço do módulo Sales
([[Identity-API]] §2.2), que **tem** `system_sales`. Hoje esse papel não é
atribuído por ninguém — o fixture funciona porque nada checa.

## 7. Os quatro fluxos, ponta a ponta

**Cliente se cadastra e pede** — nenhuma linha de código da pizzaria:

```http
POST /api/identity/register   { "email": "ana@x.com", "password": "…" }
POST /api/identity/confirm-email  { "token": "…" }
POST /api/identity/login      { "email": "ana@x.com", "password": "…" }
   → { "accessToken": "…", "refreshToken": "…" }
POST /api/v1/orders           Authorization: Bearer …
```

O papel `customer` sai de onde? Do `register`, que atribui o papel default do
provedor — e é a última decisão em aberto deste exemplo (§9, Q6).

**Gerente cadastra cozinheiro** — API de gestão, protegida por `Manager`:

```http
POST /api/identity/users            Authorization: Bearer <gerente>
     { "email": "joao@pizzaria.com", "roles": ["cook"] }
PUT  /api/identity/users/{id}/roles/cook
```

Equivale a `UserManager.CreateAsync` + `AddToRoleAsync`, sem uma linha de
código da aplicação — é o "framework de identity dentro da linguagem".

**Gateway de pagamento confirma** — client credentials da conta `Payments`,
papel `system_payment`, e a rota `/admin/orders/{id}/pay` o aceita sem que ele
seja `Manager`.

**Cozinha finaliza** — `POST /kitchen/tickets/{id}/finish` com token de
`cook`; o `PublicEvent TicketFinished` faz a `Policy` de Sales disparar
`CompletePreparation`, que roda sob `KitchenSystem`.

## 8. Testes

```ds
Test OrderVisibility {
    scenario "cliente vê os próprios dados" {
        given caller User("U1") { roles: [customer] }
        given Order("O1") from [ OrderPlaced(id: "O1", customerId: "U1", …) ]
        when query GetActiveOrders()
        then visible { customer, phone, total }
    }

    scenario "outro cliente não vê" {
        given caller User("U2") { roles: [customer] }
        given Order("O1") from [ OrderPlaced(id: "O1", customerId: "U1", …) ]
        when query GetActiveOrders()
        then hidden { customer, phone, total }
    }

    scenario "gerente vê tudo" {
        given caller User("U9") { roles: [manager] }
        given Order("O1") from [ OrderPlaced(id: "O1", customerId: "U1", …) ]
        when query GetActiveOrders()
        then visible { customer, phone, total }
    }
}

Test KitchenTicketAccess {
    scenario "cliente não assume ticket" {
        given caller User("U1") { roles: [customer] }
        given KitchenTicket("T1") state { status: TicketStatus.Pending, … }
        when Claim(cookId: "U1")
        then forbidden
    }

    scenario "cozinheiro assume" {
        given caller User("U3") { roles: [cook] }
        given KitchenTicket("T1") state { status: TicketStatus.Pending, … }
        when Claim(cookId: "U3")
        then [ TicketClaimed(id: "T1", cookId: "U3") ]
    }
}
```

O par positivo/negativo de cada regra é o que a convenção do repositório já
exige para regra de §23 — aqui vale igual, e `given caller` é o que torna
possível sem subir provedor.

## 9. O que o exemplo prova, e o que ele cobra

**Prova:**

- A divergência declarada no `read.ds` do fixture fecha sozinha quando
  `caller.id` é `ref User` (§5.4) — sem `Aggregate Customer`, que era o beco
  sem saída original.
- `perUser: 5/min` ganha definição.
- Dois furos reais aparecem só depois de nomear os papéis: qualquer logado
  assume ticket (§6), e o dono do pedido vem do payload (§5.3).
- Dezenove `string`s viram oito políticas nomeadas.

**Cobra**, e nenhuma delas é detalhe de sintaxe:

| | Questão | Onde |
|---|---|---|
| Q1 | `caller.id` legível em UseCase, senão não há como gravar o dono | [[Identity-API]] §4.1 |
| Q2 | `serviceAccounts`, senão `Policy` → `Handle` com `access` não tem caller | [[Identity-API]] §2.2 |
| Q3 | `requires` em rota convivendo com `access` do agregado | [[Identity-API]] §5 |
| Q6 | Papel default do `register` — quem nasce `customer`? Configurável no bloco (`register { public, defaultRoles: [customer] }`) é a proposta natural, e não está escrita em lugar nenhum | este documento |

Q1 e Q2 são bloqueantes: sem elas o exemplo não fecha nem no papel.
