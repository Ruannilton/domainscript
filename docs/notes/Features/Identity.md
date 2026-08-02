# Identity — design de proposta

> Status: **proposta de design, revisada**. Nada implementado, nada escrito na
> spec ainda. As decisões do autor sobre as perguntas em aberto estão
> incorporadas ao corpo; as notas de rodapé `[^n]` são o registro original de
> cada decisão, e a §9 passou a listar o que elas fecham e o que sobra.
> Inspiração declarada: ASP.NET Core Identity (stores, claims, policies,
> external providers, endpoints scaffolded).

## 1. O buraco, com evidência

`caller` aparece **22 vezes** em toda a spec v7, sempre em uma de três formas:

| Forma | Onde aparece | Definição normativa |
|-------|--------------|---------------------|
| `caller.id` | [[04-domain-core]] §4.3.1, [[02-type-system]] | ✅ Única definida: tipo `CallerId`, opaco, um operador (comparação de vínculo contra `ref T`) |
| `caller.authenticated` | [[04-domain-core]] §4.3, §4.3.1 | ❌ Só em exemplo. Tipo não declarado |
| `caller.hasRole("...")` | [[04-domain-core]], [[06-read-side]] §6.2, [[14-multi-tenancy]] §14 | ❌ Só em exemplo. Assinatura, origem dos papéis e semântica não declaradas |

Três consequências que já mordem hoje:

1. **`caller.hasRole(...)` é, ao pé da letra, erro de compilação.** A
   [[02-type-system]] §2.8 é declarada *autoridade fechada* sobre "o que se
   pode invocar em um valor" e não lista `caller`. A única condição de acesso
   que a §14 escreve é chamada fora do catálogo. Isso já está registrado como
   bloqueio em [[usecase-access-block-nao-parseado]].
2. **Não há como expressar "o dono do recurso" quando o dono não é agregado
   deste programa.** A §4.3.1 restringe `caller.id` a comparar contra `ref T`.
   No fixture `pizzeria`, `caller.id == self.customerId` — com `customerId`
   sendo um ValueObject — é proibido, e a alternativa conforme exigiria um
   `Aggregate Customer` que aquele bounded context não modela. É um beco sem
   saída real, encontrado ao migrar o fixture para a v7.
3. **Assimetria com o tenant.** A [[14-multi-tenancy]] abre dizendo "Tenant é
   ambient context (**como `caller`**)" — e então dá ao tenant estratégias de
   isolamento, resolução na borda, filtro automático, opt-in cross-tenant,
   `Aggregate Tenant` num módulo Platform e provisionamento. Ao gêmeo que ela
   própria invoca como paradigma, nada. `perUser` em `rateLimit`
   ([[11-interface]], [[17-rate-limiting]]) e `POST "/login" -> Login` também
   pressupõem um conceito de usuário que a linguagem nunca define.

Não existe hoje: credencial, senha, hash, sessão, token, refresh, claim,
provedor externo, confirmação de e-mail, MFA, lockout, revogação. `jwt_claim`
existe na spec **apenas** para resolver tenant, não para autenticar.

## 2. Princípios de design

Antes da sintaxe, as restrições que o desenho não pode violar:

1. **Identity é ambient context, como tenant.** Nunca parâmetro explícito,
   nunca campo de Command. O domínio lê, jamais recebe.
2. **Três camadas separadas, e a separação é o ponto.** Infraestrutura (onde
   credenciais moram, quem emite/valida token) ≠ contrato (o que `caller`
   oferece ao domínio) ≠ domínio (se você quer um `Aggregate User`, é seu).
   ASP.NET Identity conflaciona as três; aqui isso quebraria "Zero
   Infraestrutura" ([[01-overview]] §1).
3. **Uma Forma Canônica** ([[01-overview]] §1.1). Um mecanismo de autorização,
   não três.
4. **Fail-closed**, coerente com `access` closed-by-default e com tenant
   ausente → 400.
5. **Core vs. opt-in** (NFR-12): identity local não pode arrastar dependência
   de OIDC/JWKS para dentro de quem não declarou provedor externo.
6. **Determinismo e testabilidade** ([[24-testing]]): um cenário precisa poder
   fixar quem é o caller sem subir provedor.

## 3. Camada 1 — o bloco `Identity` em `mod.ds`

Segue exatamente o idioma dos outros blocos de [[13-module-infra]]
(`Database`, `Cache`, `RateLimit`, `Idempotency`, `Telemetry`): configuração
declarativa, valores enumerados nus, rótulos opacos entre aspas.

### 3.1. Provedor local (store próprio)

```ds
Identity {
    provider: local
    store: IdentityDb              // Database declarado no mesmo mod.ds
    subject: Customer              // <-- a integração decisiva, ver §4.3

    password {
        hasher: argon2id           // argon2id (padrão) | bcrypt | pbkdf2
        minLength: 12
        requireDigit: true
        requireSymbol: true
        requireMixedCase: true
    }

    lockout {
        maxAttempts: 5
        window: 15min
        duration: 15min
    }

    tokens {
        access  { ttl: 15min }
        refresh { ttl: 30d, rotation: true, reuseDetection: revokeFamily }
    }

    confirmation { email: required }     // required | optional | none
    mfa          { totp: optional }      // required | optional | none

    roles { admin, staff, customer }     // catálogo fechado — ver §4.4[^8]
}
```

Equivale, em ASP.NET, a `AddIdentityCore` + `PasswordOptions` +
`LockoutOptions` + o store EF Core.

### 3.2. Provedor externo (Cognito, Auth0, Entra, Keycloak)

O caso que o usuário citou. A aplicação **não** guarda credencial: só valida
token e mapeia claims.

```ds
Identity {
    provider: oidc
    issuer:   env("COGNITO_ISSUER")
    audience: env("COGNITO_AUDIENCE")
    jwks:     env("COGNITO_JWKS_URL")
    subject:  Customer

    claims {
        subject: "sub"                 // qual claim vira caller.id
        roles:   "cognito:groups"      // qual claim vira caller.hasRole
        email:   "email"
    }
}
```

Aqui **nenhum** endpoint de identity é gerado (não há o que registrar nem
senha a trocar): o provedor externo é a fonte da verdade, e o programa é
apenas *relying party*. É o que torna a distinção `provider:` significativa em
vez de decorativa.

### 3.3. Provedor federado (local + externo)

Store local para conta e papéis; login social delegado — o caso
`ExternalLoginProvider` do ASP.NET.

```ds
Identity {
    provider: federated
    store: IdentityDb
    subject: Customer

    external Google {
        issuer: env("GOOGLE_ISSUER")
        clientId: env("GOOGLE_CLIENT_ID")
        clientSecret: env("GOOGLE_CLIENT_SECRET")
        linkBy: email                  // email | subject | manual
    }
}
```

### 3.4. Onde o bloco mora

`Identity` é **um por serviço**, não por módulo — dois módulos do mesmo
serviço não podem ter noções diferentes de quem está chamando.

**Decidido: o bloco é declarado no nível do `service`, em [[12-topology]]**[^2],
não num módulo `Platform`. A consequência que importa: `Identity` deixa de ser
declaração de módulo e passa a ser configuração de serviço, ao lado do que a
topologia já diz sobre cada serviço; os módulos daquele serviço apenas *leem*
`caller`, nenhum deles o configura. Um programa sem topologia declarada tem um
único serviço default, e é lá que o bloco mora.

## 4. Camada 2 — o contrato normativo de `caller`

O que hoje não existe. `caller` passa a ser **contexto ambiente tipado**, com
superfície fechada, e a [[02-type-system]] §2.8 ganha a entrada correspondente
(fechando o defeito 1 da §1).

| Membro | Tipo | Semântica | Onde é legível |
|--------|------|-----------|----------------|
| `caller.authenticated` | `boolean` | Há principal autenticado | Qualquer `access`/`visibility`/`AccessPolicy` |
| `caller.id` | `CallerId`, comparável a `ref T` quando `Identity { subject: T }` está declarado | Identidade opaca do principal — [[04-domain-core]] §4.3.1, agora com referente nominal declarado (§4.3) | idem |
| `caller.hasRole(r)` | `boolean` | `r` é `string` literal validado contra o catálogo de papéis declarado (§4.4) | idem |
| `caller.hasClaim(k, v)` | `boolean` | Claim arbitrária — a escotilha para o que role não expressa, **não** validada em compilação (§4.4) | idem |
| `caller.satisfies(P)` | `boolean` | `P` é uma `AccessPolicy` declarada (§5) | idem |

**Não há `caller.subject`**[^4]: `caller.id` é a única forma de nomear o
principal. Uma proposta anterior deste documento acrescentava
`caller.subject : ref T` ao lado de `caller.id`, o que dava duas grafias para a
mesma pergunta — contra "Uma Forma Canônica". O que `Identity { subject: T }`
faz é *tipar* a comparação de `caller.id`, não criar um segundo membro.

Fora de `access`, `visibility` e `AccessPolicy` → erro de compilação, mesma
regra que a §4.3.1 já fixa para `caller.id`. Caller anônimo → todo membro é
`false` / não-vinculado, **fail-closed**, nunca erro de execução.

### 4.3. `Identity { subject: T }` — o que ele resolve, e o que não

`caller.id : CallerId` é opaco e só compara contra `ref T`. Isso é correto e
não muda. O que falta hoje é **quem é esse `T`**: a §4.3.1 exige a comparação
nominal mas nada na linguagem declara contra qual agregado o principal
corresponde.

`Identity { subject: Customer }` declara isso num único lugar. A partir daí a
comparação de posse é nominal e type-safe, e continua escrita com `caller.id`:

```ds
// sem Identity: caller.id não tem referente declarado; contra um ValueObject
// a comparação é proibida pela §4.3.1 e não há alternativa conforme
visibility { total requires caller.id == self.customerId }

// com Identity { subject: Customer } e customerId : ref Customer,
// a mesma linha passa a ser a forma correta — nominal e verificada
```

Sem `Identity` declarado, `subject` não existe e `caller.id` continua com a
superfície restrita de hoje — nenhuma semântica inventada por omissão.

**O que isto ainda não resolve, e é honesto dizer:** o beco sem saída do
`pizzeria` só fecha se aquele bounded context tiver algum `T` a que
`subject:` possa apontar e um campo `customerId : ref Customer`. Se o contexto
deliberadamente não modela `Aggregate Customer`, `subject: Customer` não tem
referente e o problema volta inteiro. Ver a questão residual R1 na §9.1.

### 4.4. Papéis e claims — catálogo fechado, atribuição dinâmica[^8]

A tensão é real: usar `string` nua contra o design da linguagem, contra querer
criar papel e atribuí-lo a um usuário em runtime, via API, sem recompilar. As
duas coisas cabem porque são perguntas diferentes — *quais nomes o código cita*
e *quem tem qual papel*:

- **`Identity { roles { ... } }` é catálogo fechado do que o código pode
  citar.** `caller.hasRole("staff")` valida `"staff"` contra ele em compilação;
  erro de digitação vira erro de compilação, não negação silenciosa.
- **A atribuição é sempre runtime.** Quais usuários têm `staff` é dado no
  store, mexido pela API de gestão (§6) — nada disso é compilado.
- **Papel criado em runtime que não está no catálogo é legal e existe no
  store** — só não pode ser citado por nome numa `AccessPolicy`, porque o
  compilador não teria como verificá-lo. Para autorizar por ele existe
  `caller.hasClaim(k, v)`, explicitamente não verificada: a flexibilidade
  continua disponível, mas quem a usa escolhe abrir mão da verificação em vez
  de perdê-la por omissão.

Com `provider: oidc` o catálogo declara os valores esperados no claim mapeado
em `claims { roles: ... }`; a fonte da verdade continua sendo o provedor.

## 5. Camada 3 — `AccessPolicy`: autorização nomeada

O empréstimo mais direto do ASP.NET (`AddPolicy` / `[Authorize(Policy=...)]`).
Hoje toda condição de acesso é escrita inline e duplicada — no `pizzeria` a
mesma condição de posse aparece **três vezes** no mesmo bloco `visibility`.

```ds
AccessPolicy OrderOwner {
    requires caller.id == self.customerId
}

AccessPolicy Staff {
    requires caller.hasRole("staff")
}

AccessPolicy OrderVisible {
    requires OrderOwner or Staff        // composição de políticas
}
```

Uso:

```ds
View OrderVW From Order {
    visibility {
        customer requires OrderVisible
        phone    requires OrderVisible
        total    requires OrderVisible
    }
}

Aggregate Order {
    access {
        ConfirmPayment requires Staff
    }
}
```

Ganhos: uma forma canônica para autorização, condição testável isoladamente
([[24-testing]]), regra de negócio de acesso com nome de domínio, e — o mais
relevante para este repositório — **um único ponto de lowering** no codegen em
vez de condições espalhadas.

`self` dentro de uma `AccessPolicy` é o alvo onde ela é aplicada; uma policy
que referencia `self` só pode ser usada onde há instância projetada. Policies
sem `self` (só `caller`) valem em qualquer posição, inclusive num `access` de
UseCase — o que destrava, de graça, parte de
[[usecase-access-block-nao-parseado]].

### 5.1. Onde cada policy executa[^3]

A regra é a natureza da condição, e ela é decidível sintaticamente:

| Policy | Executa | Quando |
|--------|---------|--------|
| Depende de dado de domínio (referencia `self`) | **Dentro do domínio** | Depois de carregar a instância — não há como decidir posse antes de ter o agregado |
| Não depende de domínio (só `caller`, rota, token) | **Fora do domínio, na borda** | Antes de entrar no UseCase; a requisição é rejeitada sem tocar o domínio |

Não é escolha de quem escreve: uma policy sem `self` **sempre** executa na
borda, uma com `self` **sempre** no domínio. Uma sintaxe só, dois pontos de
lowering, e o critério é a presença de `self` — o compilador classifica, o
autor não anota. Isso evita o problema de pushdown ambíguo de
[[spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos]], onde a mesma
grafia podia significar dois lugares de execução sem regra que decidisse.

Composição segue a mesma classificação: `OrderVisible = OrderOwner or Staff`
referencia `self` através de `OrderOwner`, logo é policy de domínio inteira.
Uma composição de domínio **não** pode ser usada num `access` de UseCase, pelo
mesmo motivo de sempre: ali não há instância.

## 6. Camada 4 — endpoints gerados

Análogo ao *Identity API endpoints* do ASP.NET Core 8. Só com
`provider: local` ou `federated`; opt-in explícito, nunca por default:

```ds
Identity {
    provider: local
    // ...
    expose {
        register        // POST /identity/register
        login           // POST /identity/login
        refresh         // POST /identity/refresh
        logout          // POST /identity/logout
        confirmEmail    // POST /identity/confirm-email
        resetPassword   // POST /identity/reset-password
        manage          // GET|POST /identity/manage/*  (perfil, senha, 2FA)
    }
}
```

Cada item liga um handler do runtime vendorado, não um UseCase do usuário.
Rotas de identity são `{ tenancy: none }` por construção (o login precede a
resolução de tenant — ver §7.2), herdam o `basePath` da
[[11-interface]] e participam de `rateLimit` normalmente — a spec já escreve
`POST "/login" ... { rateLimit: { perIp: 10/min, onBackendFailure: closed } }`,
que passaria a ser default do endpoint gerado em vez de exemplo solto.

Quem não usa `expose` escreve o próprio `UseCase Login` e continua funcionando
— a spec já mostra essa forma, e ela não deixa de valer.

### 6.1. Onde o estado mora, e de quem é a máquina de estados

**Sessão, refresh e revogação são persistidos no `Database` apontado por
`store:`**[^5] — infraestrutura declarada, como qualquer outra: nada de store
implícito, nada de estado em memória do processo. Isso torna
`refresh { rotation, reuseDetection: revokeFamily }` implementável de fato, e
correto sob múltiplas réplicas do serviço.

Daí decorre uma regra de superfície: **`tokens`, `lockout`, `password`,
`confirmation` e `mfa` só são legais com `provider: local` ou `federated`.** Com
`provider: oidc` emissão, rotação e revogação são do provedor externo; declarar
esses blocos ali é erro de compilação, não configuração ignorada em silêncio.
Simetricamente, `store:` é obrigatório em `local`/`federated` e proibido em
`oidc`.

**MFA e confirmação de e-mail são máquina de estados do runtime
vendorado**[^6], como o event store — não são modeláveis em DomainScript e não
geram Aggregate no programa do usuário. É a escolha do ASP.NET Core Identity, e
é o que mantém o princípio 2 da §2: o fluxo de login é infraestrutura, o
domínio só lê `caller`.

## 7. Integração com o que já existe

| Feature | Como se conecta | Atrito |
|---------|-----------------|--------|
| `access` de Aggregate ([[04-domain-core]] §4.3) | Passa a aceitar `AccessPolicy` além da condição inline | Nenhum — extensão compatível |
| `visibility` de View ([[06-read-side]] §6.2) | Idem, e ganha `caller.id` com referente declarado | Destrava o bloqueio 1 de [[visibility-de-view-nao-implementado]] |
| Multi-tenancy ([[14-multi-tenancy]]) | Identity resolve o caller **primeiro**; o tenant é derivado do caller resolvido | Ordem fixada na §7.2 |
| `rateLimit { perUser }` ([[17-rate-limiting]]) | `perUser` passa a ter definição: chaveado por `caller.id` | Hoje é indefinido |
| Testing ([[24-testing]]) | `given caller ...` fixa principal/papéis do cenário, como alias de teste faz com `ref T` | Precisa de sintaxe nova |
| Erros ([[23-error-classification]]) | 401 (não autenticado) vs 403 (autenticado, sem permissão) — distinção que hoje não existe | Hoje só há `ErrForbidden` |
| Idempotência ([[15-idempotency]]) | Escopo da chave passa a poder incluir o principal | Em aberto — R3 (§9.1) |
| `Identity` como serviço ([[12-topology]]) | `provider: oidc` apontando para outro serviço do próprio topology | Coerente com o modelo |
| Catálogo §2.8 ([[02-type-system]]) | Ganha a entrada `caller`, fechando a contradição da §1 | Correção necessária de qualquer forma |

### 7.2. Ordem entre tenant e identity — resolvida[^1]

A §14 resolve tenant na borda por `subdomain`/`header`/`jwt_claim`/`path`. Com
`jwt_claim` isso parecia circular: o token precisa ser validado antes de o
tenant existir, mas quem valida o token seria um Identity que num SaaS B2B
poderia ser por tenant.

**A circularidade era falsa, e some com a ordem fixada: identity primeiro,
tenant depois, sempre.** O que `tenant { from: jwt_claim }` lê é o
identificador do usuário/aplicação que já está chamando — ou seja, um atributo
do caller **já resolvido**, não uma entrada independente. Daí:

1. A borda resolve o caller a partir do token (`Identity`, issuer global do
   serviço — §3.4, um por serviço).
2. O tenant é derivado do caller resolvido, ou das fontes que não dependem do
   token (`subdomain`, `header`, `path`).
3. Só então o filtro de tenancy da §14 se aplica ao domínio.

Duas consequências normativas:

- **`Identity` por tenant é ilegal.** Issuer diferente por tenant exigiria
  saber o tenant antes de validar o token, que é exatamente a ordem invertida.
  Um serviço tem um `Identity`; multi-tenancy vive *dentro* dele (§9.1, R2), não
  em cima dele.
- **`tenant { from: jwt_claim }` não valida token.** Ele lê um claim de um
  token que o Identity já validou. Hoje a §14 sugere o contrário e precisa ser
  corrigida junto.

**Resistência a DDoS**: no caminho de requisição a borda faz apenas validação
local do JWT (assinatura contra JWKS em cache e extração do identificador) —
sem ida ao store nem ao serviço de identity por requisição. Um pico de tráfego
não autenticado é rejeitado com trabalho O(1) e criptográfico apenas, e não
converte carga de borda em carga no Identity. Os endpoints que de fato tocam o
store (`login`, `register`, `refresh`) são justamente os que já nascem com
`rateLimit` por IP (§6).


## 8. Gaps que isto fecha

| Gap | Como fecha |
|-----|-----------|
| `caller` sem definição normativa — bloqueio 3 de [[usecase-access-block-nao-parseado]] | §4 dá o contrato completo e entra no catálogo fechado da §2.8 |
| `caller.hasRole` fora do catálogo §2.8 = erro ao pé da letra | idem |
| `visibility` sem `self`/`caller.id` utilizável — bloqueios 1 e 2 de [[visibility-de-view-nao-implementado]] | `Identity { subject: T }` + `AccessPolicy` dão a comparação de posse type-safe |
| Comparação de posse sem referente declarado para `caller.id` | `Identity { subject: T }` declara o `T` que a §4.3.1 exige (parcial no `pizzeria` — R1) |
| `perUser` de rateLimit sem definição | Chaveado por `caller.id` |
| `access` de UseCase sem semântica de `caller` | Policies sem `self` são válidas ali |
| 401 vs 403 indistinguíveis | §7, tabela de erros |
| Condição de acesso duplicada e sem nome | `AccessPolicy` |

## 9. Decisões da revisão

As sete perguntas em aberto da versão anterior deste documento, e o que a
revisão decidiu sobre cada uma:

| # | Pergunta | Decisão | Onde está no corpo |
|---|----------|---------|--------------------|
| 1 | Onde o bloco `Identity` mora | Nível de `service`, em [[12-topology]] — não um módulo `Platform`[^2] | §3.4 |
| 2 | Autorização é do domínio ou da borda | Depende de dado de domínio → domínio; não depende → borda. Critério sintático: presença de `self`[^3] | §5.1 |
| 3 | Catálogo de papéis | Catálogo fechado em `Identity { roles { … } }` para o que o código cita; atribuição e criação em runtime via API; `hasClaim` é a escotilha não verificada[^8] | §4.4 |
| 4 | `caller.id` vs `caller.subject` | Só `caller.id`. `subject:` tipa a comparação, não cria membro novo[^4] | §4 |
| 5 | Revogação e sessão | Persistidas no `Database` de `store:`; `tokens`/`lockout`/`password` só com `local`/`federated`[^5] | §6.1 |
| 6 | MFA e confirmação | Máquina de estados do runtime vendorado, como no ASP.NET Core Identity[^6] | §6.1 |
| 7 | Identity é multi-tenant | Sim, deve suportar[^7] — o *formato* segue em aberto (R2) | §9.1 |
| — | Ordem tenant × identity (§7.2) | Identity primeiro, sempre; tenant deriva do caller resolvido; `Identity` por tenant é ilegal[^1] | §7.2 |

### 9.1. Questões residuais

O que as decisões acima **não** fecham, e continua precisando de resposta antes
de virar spec:

- **R1 — `subject:` quando o principal não é agregado deste contexto.** A
  decisão 4 mantém a comparação nominal contra `ref T`, mas o caso que motivou
  a seção (`pizzeria`, sem `Aggregate Customer`) só fecha se houver um `T`
  declarado a que `subject:` aponte. Ou o contexto passa a declarar o agregado
  que ele evitava, ou `subject:` precisa poder nomear uma identidade externa
  (um tipo de referência sem agregado local) — e aí é a §4.3.1 da spec que muda.
  Depende de [[spec-v7-identidade-implicita-do-aggregate]].
- **R2 — formato do multi-tenancy do Identity.** "Deve suportar" fixa o
  requisito, não o schema: um usuário pertence a um tenant, a vários, ou é
  global com papéis por tenant? A ordem da §7.2 (tenant derivado do caller)
  favorece a terceira forma, mas é decisão de produto e muda o store local, o
  catálogo de papéis (papel por tenant?) e o significado de `hasRole` sob
  cross-tenant.
- **R3 — escopo da chave de idempotência.** Já estava marcado "decisão em
  aberto" na tabela da §7 e não foi tocado: a chave passa a incluir o
  principal, ou não?
- **R4 — `given caller ...` em [[24-testing]].** A sintaxe de teste é a única
  peça do princípio 6 (§2) ainda sem forma proposta.

## 10. Recomendação de sequenciamento

Se isto virar spec, a ordem que minimiza retrabalho:

1. **Contrato de `caller` (§4) + entrada no catálogo §2.8.** É correção de uma
   contradição existente, vale mesmo que nada mais deste design avance, e
   desbloqueia duas issues já abertas.
2. **`AccessPolicy` (§5).** Puramente aditivo, não depende do bloco `Identity`,
   e unifica o lowering de acesso que hoje está espalhado.
3. **Bloco `Identity` com `provider: oidc` (§3.2).** O mais barato dos três
   provedores — só valida token e mapeia claim, sem store, sem endpoint.
4. **`subject:` tipando `caller.id` (§4.3).** Depende de `ref T`
   ([[spec-v7-identidade-implicita-do-aggregate]]) estar implementado, e de R1
   resolvida.
5. **`provider: local` + `expose` (§3.1, §6).** O maior, e o único que exige
   runtime novo (hash, token, store, máquina de estados de MFA/confirmação).
   Depende de R2, que define o schema do store.

Os passos 1 e 2 são os de melhor relação valor/risco: consertam o que já está
quebrado hoje e não pressupõem nenhuma das questões residuais da §9.1 — o passo
2 inclusive já pode incorporar a regra de execução da §5.1, que é decidível sem
o bloco `Identity` existir.

[^1]: Nota do desenvolvedor: tenant por jwt na verdade se refere ao identificador do usuário/aplicação acessando o sistema, assim o modulo de identity precisa resolver ele primeiramente antes de ser aplicado o tenant, para evitar que um ataque DDoS deixe o modulo de identity sobrecarregado o serviço de identity pode ser utilizado apenas para extrair o identificador do jwt 

[^2]: O identity é definido a nível de serviço
	

[^3]: Autorizações que precisem acessar dados de domínio ficam dentro do escopo do domínio, autorizações  que não dependam de domínio (ex: acessar o recurso no path X com o token de acesso Y) devem viver fora do domínio

[^4]: Utilizaremos a notação caller.id  ao invés de subject

[^5]: se o provider é local as informações deve ser gravadas em um  banco de dados especificado pela infraestrutura

[^6]: Runtime vendorável, similar ao aspnet core identity

[^7]: Identity deve suportar multi tenancy

[^8]: Embora seja contra o design da linguagem usar strings diretamente, e declarar as claims e roles no modulo do identity seja uma solução mais "segura"  e testável, seria interessante termos flexibilidade em termos de poder cadastrar alterar claims/roles, e adicionar-las, remover-las de um usuário via  API
	
