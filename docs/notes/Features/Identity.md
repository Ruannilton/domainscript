# Identity — design de proposta

> Status: **proposta de design, revisada**. Nada implementado, nada escrito na
> spec ainda. As decisões do autor sobre as perguntas em aberto estão
> incorporadas ao corpo; as notas de rodapé `[^n]` são o registro original de
> cada decisão, e a §9 passou a listar o que elas fecham e o que sobra.
> Inspiração declarada: ASP.NET Core Identity (stores, claims, policies,
> external providers, endpoints scaffolded).
>
> **Continuações**: [[Identity-API]] detalha a superfície que o desenvolvedor
> escreve (as sete camadas de API); [[Identity-Exemplo-Pizzeria]] aplica tudo
> ao fixture `pizzeria` com cliente, gerente e cozinheiro.

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
   oferece ao domínio) ≠ domínio (o agregado que a aplicação cadastra é dela).
   Herdamos do ASP.NET Identity o *escopo* — um framework de identity completo,
   sem lib externa a aprender (§3.0) — mas não a conflação: lá as três camadas
   se misturam no mesmo `IdentityUser` que o desenvolvedor herda e estende, e
   aqui isso quebraria "Zero Infraestrutura" ([[01-overview]] §1). A §7.3
   sustenta a mesma linha entre principal do serviço e usuário do domínio.
3. **Uma Forma Canônica** ([[01-overview]] §1.1). Um mecanismo de autorização,
   não três.
4. **Fail-closed**, coerente com `access` closed-by-default e com tenant
   ausente → 400.
5. **Core vs. opt-in** (NFR-12): identity local não pode arrastar dependência
   de OIDC/JWKS para dentro de quem não declarou provedor externo.
6. **Determinismo e testabilidade** ([[24-testing]]): um cenário precisa poder
   fixar quem é o caller sem subir provedor.

## 3. Camada 1 — o bloco `Identity`

Segue exatamente o idioma dos outros blocos de [[13-module-infra]]
(`Database`, `Cache`, `RateLimit`, `Idempotency`, `Telemetry`): configuração
declarativa, valores enumerados nus, rótulos opacos entre aspas. Mora no nível
de `service`, não de módulo — §3.4.

### 3.0. O que o bloco escolhe é o *backend*, não o modelo[^8]

Antes da sintaxe, o enquadramento que decide tudo o que vem depois:
**DomainScript passa a ter um framework de identity próprio**, como o ASP.NET
Core Identity, o Cognito ou o Keycloak são para suas plataformas. O objetivo é
que o desenvolvedor **não precise aprender outra lib, outro framework ou outro
serviço** para ter autenticação: ele escreve DomainScript, e escolhe por
configuração qual backend sustenta aquilo.

Disso decorrem duas coisas:

- **O modelo vem da lib padrão.** `User`, `Role`, `Claim` e os VOs
  correspondentes são declarações da biblioteca padrão, implementadas pelo
  compilador e pelo runtime vendorado — o desenvolvedor não os escreve, não os
  versiona e não pode quebrá-los. Do ponto de vista dele, identity é
  *configuração e população de valores*, não modelagem.
- **`provider:` escolhe o backend.** Store local, um provedor OIDC gerenciado
  (Cognito, Auth0, Entra, Keycloak) ou os dois. Trocar de backend é trocar uma
  linha de configuração; o contrato de `caller` (§4) e as `AccessPolicy` (§5)
  não mudam.

Isso também resolve a tensão aparente com "não usar tipos primitivos"
([[02-type-system]]). Aquela regra existe para proteger *os agregados que o
desenvolvedor escreve*, onde uma `string` nua é convite a erro de domínio. Os
agregados e VOs de identity são internos ao compilador: o risco que a regra
combate — modelagem frouxa feita por quem está com pressa — não se aplica da
mesma forma, porque não há modelagem do usuário ali. É o que abre espaço para
papéis e claims mutáveis por API (§4.4) sem que isso vire "voltamos às strings
soltas".

### 3.1. Provedor local (store próprio)

```ds
Identity {
    provider: local
    // o store é declarado aqui, não num mod.ds: Identity é de serviço (§3.4)
    // e módulo nenhum é dono dele. Mesmas chaves de um Database de módulo.
    store {
        provider: "postgres"
        connection: env("IDENTITY_DB_URL")
    }

    id: Customer                   // <-- a integração decisiva, ver §4.3

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

    roles  { admin, staff, customer }    // sementes; clientes cadastram outros
    claims { tier, storeId }             // via API em runtime — §4.4[^8]
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
    id:       Customer

    claims {
        id:    "sub"                   // qual claim vira caller.id
        roles: "cognito:groups"        // qual claim vira caller.hasRole
        email: "email"
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
    store { provider: "postgres", connection: env("IDENTITY_DB_URL") }
    id: Customer

    external Google {
        issuer: env("GOOGLE_ISSUER")
        clientId: env("GOOGLE_CLIENT_ID")
        clientSecret: env("GOOGLE_CLIENT_SECRET")
        linkBy: email                  // email | providerId | manual
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
| `caller.authenticated` | `boolean` | Há principal autenticado | `access`, `visibility`, `AccessPolicy` e corpo de `UseCase` (§4.5) |
| `caller.id` | `ref T` com `Identity { id: T }` declarado; `CallerId` opaco sem ele | O principal — nominal e comparável quando há `Identity`, opaco como hoje quando não há (§4.3) | idem |
| `caller.hasRole(r)` | `boolean` | `r` é um `Role` (§4.4), não uma `string` nua | idem |
| `caller.hasClaim(k, v)` | `boolean` | `k` é um `Claim` (§4.4); `v` o valor esperado | idem |
| `caller.satisfies(P)` | `boolean` | `P` é uma `AccessPolicy` declarada (§5) | idem |
| `caller.isService(M)` | `boolean` | O principal é o módulo `M` deste serviço, executando reativamente (§4.5) | idem |

**Um só membro, com a mecânica do `subject`**[^4]. A proposta original tinha
`caller.id : CallerId` (opaco) *e* `caller.subject : ref T` (nominal) — duas
grafias para a mesma pergunta, contra "Uma Forma Canônica". A decisão mantém a
**mecânica** do `subject` — o principal é uma referência tipada ao agregado,
não um identificador opaco a ser comparado por fora — e descarta o **nome**,
porque `caller.id` é o que [[04-domain-core]] §4.3.1 e o resto da documentação
já escrevem. Mesma renomeação no bloco de configuração: `Identity { id: T }`,
e `claims { id: "sub" }` no mapeamento OIDC.

Fora das posições da tabela → erro de compilação, na mesma linha do que a
§4.3.1 já fixa para `caller.id`. Caller anônimo → todo membro é `false` /
não-vinculado, **fail-closed**, nunca erro de execução.

### 4.3. `Identity { id: T }` — o principal como referência tipada

Hoje `caller.id : CallerId` é opaco e a §4.3.1 só o deixa comparar contra
`ref T` — sem que nada na linguagem declare **quem é esse `T`**. A regra existe,
o referente não.

`Identity { id: Customer }` declara o referente num único lugar. A partir daí
`caller.id : ref Customer`, e a comparação de posse é nominal e verificada em
compilação:

```ds
// sem Identity: caller.id é CallerId opaco, e contra um ValueObject
// a comparação é proibida pela §4.3.1 — sem alternativa conforme
visibility { total requires caller.id == self.customerId }

// com Identity { id: Customer } e customerId : ref Customer,
// a mesma linha é a forma correta: ref Customer contra ref Customer
```

A linha escrita é a mesma; o que muda é o tipo de `caller.id`, e com ele o que
o compilador consegue provar. Sem `Identity` declarado nada é inventado por
omissão: `caller.id` continua com a superfície restrita de hoje.

`T` pode ser um agregado do próprio domínio (`Customer`, `Employee`) ou o
`User` da lib padrão (§3.0), para quem não quer modelar o principal. É o que
tira o `pizzeria` do beco sem saída sem obrigá-lo a declarar um
`Aggregate Customer` que aquele bounded context não quer: basta
`id: User` e `customerId : ref User`. Nos dois casos a peça que falta é `ref T`
existir de fato — [[spec-v7-identidade-implicita-do-aggregate]].

### 4.4. Papéis e claims — sementes declaradas, catálogo mutável por API[^8]

`Role` e `Claim` são VOs da lib padrão (§3.0), não `string`s. Isso é o que
permite ter as duas coisas que pareciam incompatíveis — segurança de tipo no
código e catálogo mutável pelos clientes em runtime:

- **O bloco `Identity` declara sementes**, os papéis e claims que o *programa*
  conhece e cita. Cada nome declarado vira um valor da lib padrão referenciável
  no código: `caller.hasRole(Role.staff)` é verificado em compilação, erro de
  digitação é erro de compilação, e nenhuma `string` nua aparece numa
  `AccessPolicy`.
- **O catálogo não é fechado.** A API de gestão (§6) cadastra, altera e remove
  papéis e claims em runtime, e os atribui a usuários — é assim que o cliente
  de uma plataforma define papéis que o desenvolvedor da plataforma nunca
  previu. Esses papéis existem no store, participam da autorização e são
  legítimos.
- **A diferença entre os dois não é de poder, é de verificabilidade.** Um papel
  criado em runtime não pode ser *citado por nome* numa `AccessPolicy` — o
  compilador não teria contra o que checá-lo. Ele autoriza pelos caminhos
  resolvidos em runtime (`caller.hasRole(r)` com `r` vindo de dado,
  `caller.hasClaim`), que falham fechado.

Ou seja: o desenvolvedor cadastra os tipos padrão de papel e claim de forma
declarada, e ainda assim permite que os clientes cadastrem os seus. Com
`provider: oidc` as sementes declaram os valores esperados no claim mapeado em
`claims { roles: ... }`; a fonte da verdade continua sendo o provedor externo.

### 4.5. Onde `caller` é legível — e quem é ele em execução reativa

Duas decisões que a superfície de API cobrou ([[Identity-API]]) e que fecham o
contrato da tabela da §4:

- **`caller` é legível dentro do corpo de `UseCase`**, não só em posição de
  autorização. Sem isso não há como gravar o principal no estado — o dono de um
  pedido teria que vir do payload do Command, que é precisamente o furo que
  este design existe para fechar. Continua **proibido dentro de `Handle`**: o
  agregado recebe o principal como parâmetro e permanece testável sem contexto
  ambiente. Ler `caller.id` ali é silencioso; usar um membro-predicado
  (`hasRole`, `hasClaim`, `satisfies`, `authenticated`, `isService`) gera
  ⚠️ aviso, porque autorização escrita em corpo de UseCase é autorização que o
  compilador não enxerga ([[Identity-API]] §4.1).
- **Em execução reativa o caller é o próprio módulo/serviço que disparou.**
  Uma `Policy` que reage a um `PublicEvent` e dispara um `Handle` protegido por
  `access` roda sob o principal daquele módulo — não sob caller anônimo, nem
  sob o usuário que originou a cadeia. É o que dá semântica ao `access` de
  `Handle`s que rota HTTP nenhuma alcança, hoje sustentado só por convenção de
  nome ([[Identity-API]] §2.2).

## 5. Camada 3 — `AccessPolicy`: autorização nomeada

O empréstimo mais direto do ASP.NET (`AddPolicy` / `[Authorize(Policy=...)]`).
Hoje toda condição de acesso é escrita inline e duplicada — no `pizzeria` a
mesma condição de posse aparece **três vezes** no mesmo bloco `visibility`.

```ds
AccessPolicy OrderOwner {
    requires caller.id == self.customerId
}

AccessPolicy Staff {
    requires caller.hasRole(Role.staff)     // semente declarada — §4.4
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
| Idempotência ([[15-idempotency]]) | Chave sempre escopada por `(tenant, principal, comando)` | Fecha um furo real, ver §7.4 |
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

1. A borda verifica a **assinatura** do token (`Identity`, issuer global do
   serviço — §3.4, um por serviço) e resolve o caller.
2. O tenant é extraído do token **já verificado**[^1], ou das fontes que não
   dependem dele (`subdomain`, `header`, `path`).
3. Só então o filtro de tenancy da §14 se aplica ao domínio.

O passo 1 é precondição do passo 2, e é a parte que a spec hoje não escreve:
ler tenant de um token cuja assinatura não foi verificada é escalada de
privilégio trivial — qualquer cliente forja o claim e atravessa o isolamento.
Um `tenant { from: jwt_claim }` sem `Identity` declarado no serviço deve ser
**erro de compilação**, não configuração aceita que confia no claim.

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


### 7.3. Multi-tenancy: duas populações, e a linguagem não escolhe por você[^7]

`Identity` suporta multi-tenancy, mas **qual modelo de tenancy vale é decisão
da aplicação**, não da linguagem — e os dois extremos são igualmente legítimos:

- **Pizzaria de esquina**: um único tenant, vários usuários. Tenancy
  simplesmente não é declarada; `Identity` funciona sozinho.
- **Plataforma de e-commerce vendida a outras empresas**: o tenant é o
  *cliente contratante*. Quem autentica contra a infraestrutura de identity do
  serviço são os operadores daquela empresa, e o tenant vem do token conforme
  a §7.2.

O que essa segunda forma revela é a distinção que precisa estar escrita:

| População | O que é | De quem é |
|-----------|---------|-----------|
| **Principal do serviço** | Quem chama o serviço e é autenticado por ele | Infraestrutura de identity — modelo da lib padrão (§3.0) |
| **Usuário do domínio** | Quem a aplicação cadastra como parte do negócio dela (o cliente do e-commerce) | Domínio do desenvolvedor — `Aggregate` escrito por ele |

Um e-commerce multi-tenant cadastra compradores: **esses compradores são
agregados do domínio da plataforma, não entradas da infraestrutura de identity
do serviço.** Eles obedecem às regras de tenancy da [[14-multi-tenancy]] como
qualquer outro agregado, e o `Identity` do serviço não sabe que existem.

As duas populações se cruzam só onde o desenvolvedor quiser: `Identity { id: T }`
(§4.3) é exatamente o ponto onde ele diz "o principal autenticado *é* este
agregado do meu domínio". Quem não declara mantém as duas separadas.

**Cardinalidade: um principal pertence a no máximo um tenant** — quem atende
várias empresas contratantes tem uma conta por empresa. É o que mantém
`hasRole` sendo uma pergunta só, sem qualificador de tenant, e o `0` do `0..1`
é obrigatório de qualquer forma porque root atravessa tenants
([[Identity-API]] §3.1).

### 7.4. Idempotência escopada pelo principal

A chave de idempotência é fornecida pelo cliente. Se o escopo dela não inclui
**quem** chamou, um cliente que adivinhe ou intercepte a chave de outro recebe a
resposta cacheada dele — vazamento de dados por colisão de namespace, não por
falha de autenticação.

**O escopo passa a ser `(tenant, principal, comando)`, fixo e não
configurável.** Não é preferência de design: é o único escopo em que a chave de
um caller não alcança o resultado de outro.

O caso incômodo é o chamador anônimo, que não tem principal para escopar. Em vez
de escopar por algo frágil como IP, **`Idempotency { required: true }` numa rota
pública é erro de compilação** — o conflito aparece no build, onde dá para
resolver, em vez de virar dedupe silenciosamente inseguro em produção.

## 8. Gaps que isto fecha

| Gap | Como fecha |
|-----|-----------|
| `caller` sem definição normativa — bloqueio 3 de [[usecase-access-block-nao-parseado]] | §4 dá o contrato completo e entra no catálogo fechado da §2.8 |
| `caller.hasRole` fora do catálogo §2.8 = erro ao pé da letra | idem |
| `visibility` sem `self`/`caller.id` utilizável — bloqueios 1 e 2 de [[visibility-de-view-nao-implementado]] | `Identity { id: T }` + `AccessPolicy` dão a comparação de posse type-safe |
| Comparação de posse sem referente declarado para `caller.id` | `Identity { id: T }` declara o `T` que a §4.3.1 exige — agregado do domínio ou `User` da lib padrão |
| Tenant lido de token não verificado | §7.2: `tenant { from: jwt_claim }` exige `Identity` no serviço |
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
| 3 | Catálogo de papéis | `Role`/`Claim` são VOs da lib padrão; o bloco declara sementes citáveis no código, e o catálogo é mutável por API em runtime[^8] | §3.0, §4.4 |
| 4 | `caller.id` vs `caller.subject` | Um membro só: a **mecânica** do `subject` (`ref T`) com o **nome** `id`, que a documentação já usa[^4] | §4, §4.3 |
| 5 | Revogação e sessão | Persistidas no `Database` de `store:`; `tokens`/`lockout`/`password` só com `local`/`federated`[^5] | §6.1 |
| 6 | MFA e confirmação | Máquina de estados do runtime vendorado, como no ASP.NET Core Identity[^6] | §6.1 |
| 7 | Identity é multi-tenant | Sim, e o *modelo* é da aplicação, não da linguagem: principal do serviço ≠ usuário do domínio[^7] | §7.3 |
| — | Ordem tenant × identity | Identity primeiro, sempre; tenant só de token com assinatura verificada; `Identity` por tenant é ilegal[^1] | §7.2 |
| — | O que o bloco escolhe | Identity é framework da lib padrão; `provider:` escolhe o backend, o modelo não é do desenvolvedor[^8] | §3.0 |
| — | Leitura de `caller` e caller reativo | Legível em corpo de `UseCase`, proibido em `Handle`; em reação o caller é o módulo que disparou | §4.5 |
| — | Autoridade sobre o catálogo | `Role.root` da lib padrão, bootstrap obrigatório por env; mutar catálogo de papéis é só de root, papel de cadastro vem explícito na request dentro de allowlist | [[Identity-API]] §2.3, §6.1 |

### 9.1. As quatro residuais, e como fecharam

| | Questão | Decisão |
|---|---|---|
| R1 | Cardinalidade principal ↔ tenant | `0..1`; quem atende N contratantes tem N contas. Mantém `hasRole` sem qualificador de tenant, e o `0` é exigido por root (§7.3) |
| R2 | Escopo da chave de idempotência | `(tenant, principal, comando)`, fixo. `required: true` em rota pública é erro de compilação (§7.4) |
| R3 | `given caller` em [[24-testing]] | Adotado, com anônimo como default e `given caller service M` para execução reativa ([[Identity-API]] §8) |
| R4 | Superfície da lib padrão | Enumerada em [[Identity-API]] §3; a leitura de identity pelo domínio continua proibida, e o padrão para telas é projeção local por reação a evento (§3.5 de lá) |

As questões que a superfície de API abriu também estão todas decididas —
índice em [[Identity-API]] §9.

**Não sobra decisão de design em aberto.** O que falta é trabalho de spec:
encaixar `caller`, `AccessPolicy` e os tipos da lib padrão no catálogo fechado
da [[02-type-system]] §2.8, e escrever as regras correspondentes em
[[25-compilation-rules]].

## 10. Recomendação de sequenciamento

Se isto virar spec, a ordem que minimiza retrabalho:

1. **Contrato de `caller` (§4) + entrada no catálogo §2.8.** É correção de uma
   contradição existente, vale mesmo que nada mais deste design avance, e
   desbloqueia duas issues já abertas.
2. **`AccessPolicy` (§5).** Puramente aditivo, não depende do bloco `Identity`,
   e unifica o lowering de acesso que hoje está espalhado.
3. **Bloco `Identity` com `provider: oidc` (§3.2).** O mais barato dos três
   provedores — só valida token e mapeia claim, sem store, sem endpoint.
4. **`Identity { id: T }` tipando `caller.id` (§4.3).** Depende de `ref T`
   ([[spec-v7-identidade-implicita-do-aggregate]]) estar implementado.
5. **`provider: local` + `expose` (§3.1, §6).** O maior, e o único que exige
   runtime novo (hash, token, store, máquina de estados de MFA/confirmação) e a
   lib padrão de identity (§3.0). Depende de R1 e R4, que definem o schema do
   store e a superfície dos tipos.

Os passos 1 e 2 são os de melhor relação valor/risco: consertam o que já está
quebrado hoje e não pressupõem nada do bloco `Identity` — o passo 2 inclusive
já incorpora a regra de execução da §5.1, decidível sem `Identity` existir.
Com a §9.1 fechada, os dois estão prontos para virar spec: as decisões que eles
precisam (contrato de `caller`, posições de leitura, `isService`, `requires` de
rota, `given caller`) estão todas tomadas.

## 11. Registro da segunda rodada de revisão

As notas `[^1]`–`[^8]` são a primeira rodada. A segunda precisou desfazer uma
leitura errada da nota 4 e fechar o que 1, 7 e 8 tinham deixado pela metade —
palavras do autor:

- **Sobre [^1]** — "extrair o tenant do JWT com assinatura verificada". A
  verificação de assinatura é precondição, não detalhe de implementação: §7.2,
  passo 1.
- **Sobre [^4]** — "diz *usar caller.id em vez de subject*, mas isso é só o
  nome: vamos usar a mecânica sugerida com o subject, mas ao invés de chamar de
  subject, chamaremos o campo de id para ficar em conformidade com o que já
  existe da documentação". Uma versão anterior deste documento tinha lido a nota
  como descarte da mecânica — era renomeação, e a §4.3 foi refeita.
- **Sobre [^8]** — o propósito da feature é oferecer um framework de identity
  dentro da linguagem, como ASP.NET Identity, Cognito ou Keycloak, sem que o
  desenvolvedor precise aprender outra lib ou serviço, escolhendo o backend por
  configuração. Como os agregados e VOs de identity são implementações internas
  do compilador, a filosofia de não expor tipos primitivos — que existe para
  proteger os agregados *do desenvolvedor* — não é violada por papéis e claims
  mutáveis: "o desenvolvedor pode por exemplo cadastrar os tipos de claims e
  roles padrões hardcoded mas permitir via api que os clientes cadastrem
  outros". Virou a §3.0 e a §4.4.
- **Sobre [^7]** — o modelo de tenancy "depende da aplicação que o
  desenvolvedor está montando": pizzaria de esquina é um tenant com vários
  usuários; uma plataforma de e-commerce separa por cliente contratante, e "os
  usuários que o ecommerce cadastra são do domínio da plataforma, não da
  infraestrutura identity do serviço". Virou a §7.3.

[^1]: Nota do desenvolvedor: tenant por jwt na verdade se refere ao identificador do usuário/aplicação acessando o sistema, assim o modulo de identity precisa resolver ele primeiramente antes de ser aplicado o tenant, para evitar que um ataque DDoS deixe o modulo de identity sobrecarregado o serviço de identity pode ser utilizado apenas para extrair o identificador do jwt 

[^2]: O identity é definido a nível de serviço
	

[^3]: Autorizações que precisem acessar dados de domínio ficam dentro do escopo do domínio, autorizações  que não dependam de domínio (ex: acessar o recurso no path X com o token de acesso Y) devem viver fora do domínio

[^4]: Utilizaremos a notação caller.id  ao invés de subject

[^5]: se o provider é local as informações deve ser gravadas em um  banco de dados especificado pela infraestrutura

[^6]: Runtime vendorável, similar ao aspnet core identity

[^7]: Identity deve suportar multi tenancy

[^8]: Embora seja contra o design da linguagem usar strings diretamente, e declarar as claims e roles no modulo do identity seja uma solução mais "segura"  e testável, seria interessante termos flexibilidade em termos de poder cadastrar alterar claims/roles, e adicionar-las, remover-las de um usuário via  API
	
