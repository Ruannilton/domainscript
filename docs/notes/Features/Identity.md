# Identity — design de proposta

> Status: **proposta de design**. Nada implementado, nada escrito na spec ainda.
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
        subject: "sub"                 // qual claim vira caller.subject
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
serviço não podem ter noções diferentes de quem está chamando. Duas rotas:
declará-lo num módulo `Platform` sem tenancy (o mesmo precedente que a §14 já
usa para `Aggregate Tenant`), ou em [[12-topology]] no nível do `service`.
**Decisão em aberto** — ver §8.

## 4. Camada 2 — o contrato normativo de `caller`

O que hoje não existe. `caller` passa a ser **contexto ambiente tipado**, com
superfície fechada, e a [[02-type-system]] §2.8 ganha a entrada correspondente
(fechando o defeito 1 da §1).

| Membro | Tipo | Semântica | Onde é legível |
|--------|------|-----------|----------------|
| `caller.authenticated` | `boolean` | Há principal autenticado | Qualquer `access`/`visibility`/`AccessPolicy` |
| `caller.id` | `CallerId` | Identidade opaca do principal — inalterado, [[04-domain-core]] §4.3.1 | idem |
| `caller.subject` | `ref T` | **Novo.** O principal *como agregado do programa*, quando `Identity.subject: T` está declarado | idem |
| `caller.hasRole(r)` | `boolean` | `r` é `string` literal validado contra o catálogo de papéis | idem |
| `caller.hasClaim(k, v)` | `boolean` | Claim arbitrária — a escotilha para o que role não expressa | idem |
| `caller.satisfies(P)` | `boolean` | `P` é uma `AccessPolicy` declarada (§5) | idem |

Fora de `access`, `visibility` e `AccessPolicy` → erro de compilação, mesma
regra que a §4.3.1 já fixa para `caller.id`. Caller anônimo → todo membro é
`false` / não-vinculado, **fail-closed**, nunca erro de execução.

### 4.3. `caller.subject` — por que resolve o beco sem saída

`caller.id : CallerId` é opaco e só compara contra `ref T`. Isso é correto e
não muda. O problema é que muitos domínios **não modelam o principal como
agregado próprio** — o `pizzeria` é exatamente esse caso.

`Identity { subject: Customer }` declara, num único lugar, que o principal
autenticado *corresponde* a um `Aggregate Customer`. A partir daí
`caller.subject : ref Customer` e a comparação de posse vira nominal e
type-safe:

```ds
// hoje: proibido (CallerId contra ValueObject) e sem alternativa conforme
visibility { total requires caller.id == self.customerId }

// com Identity.subject declarado, e customerId : ref Customer
visibility { total requires caller.subject == self.customerId }
```

Sem `Identity` declarado, `caller.subject` simplesmente não existe (erro de
compilação ao usá-lo) — nenhuma semântica inventada por omissão.

## 5. Camada 3 — `AccessPolicy`: autorização nomeada

O empréstimo mais direto do ASP.NET (`AddPolicy` / `[Authorize(Policy=...)]`).
Hoje toda condição de acesso é escrita inline e duplicada — no `pizzeria` a
mesma condição de posse aparece **três vezes** no mesmo bloco `visibility`.

```ds
AccessPolicy OrderOwner {
    requires caller.subject == self.customerId
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

## 7. Integração com o que já existe

| Feature | Como se conecta | Atrito |
|---------|-----------------|--------|
| `access` de Aggregate ([[04-domain-core]] §4.3) | Passa a aceitar `AccessPolicy` além da condição inline | Nenhum — extensão compatível |
| `visibility` de View ([[06-read-side]] §6.2) | Idem, e ganha `caller.subject` | Destrava o bloqueio 1 de [[visibility-de-view-nao-implementado]] |
| Multi-tenancy ([[14-multi-tenancy]]) | `jwt_claim` já resolve tenant a partir do token; Identity define **quem emite** esse token | ⚠️ Ordem de resolução, ver §7.2 |
| `rateLimit { perUser }` ([[17-rate-limiting]]) | `perUser` passa a ter definição: chaveado por `caller.id` | Hoje é indefinido |
| Testing ([[24-testing]]) | `given caller ...` fixa principal/papéis do cenário, como alias de teste faz com `ref T` | Precisa de sintaxe nova |
| Erros ([[23-error-classification]]) | 401 (não autenticado) vs 403 (autenticado, sem permissão) — distinção que hoje não existe | Hoje só há `ErrForbidden` |
| Idempotência ([[15-idempotency]]) | Escopo da chave passa a poder incluir o principal | Decisão em aberto |
| `Identity` como serviço ([[12-topology]]) | `provider: oidc` apontando para outro serviço do próprio topology | Coerente com o modelo |
| Catálogo §2.8 ([[02-type-system]]) | Ganha a entrada `caller`, fechando a contradição da §1 | Correção necessária de qualquer forma |

### 7.2. O atrito real: ordem entre tenant e identity[^1]

A §14 resolve tenant na borda por `subdomain`/`header`/`jwt_claim`/`path`. Com
`jwt_claim`, **o token precisa ser validado antes de o tenant existir** — mas a
validação do token é responsabilidade do Identity, que num modelo multi-tenant
pode ser *por tenant* (issuer diferente por tenant, caso comum em SaaS B2B).

Isso é circular e a spec atual não enxerga o problema porque nunca definiu
quem valida o token. O design precisa fixar uma ordem:

1. Resolver tenant por meios que não dependem do token (`subdomain`, `header`,
   `path`) → validar token no contexto do tenant; ou
2. Validar token com issuer global → extrair tenant do claim.

`jwt_claim` só é legal na rota 2. Combinar `tenant { from: jwt_claim }` com
`Identity` por-tenant deveria ser **erro de compilação**.


## 8. Gaps que isto fecha

| Gap | Como fecha |
|-----|-----------|
| `caller` sem definição normativa — bloqueio 3 de [[usecase-access-block-nao-parseado]] | §4 dá o contrato completo e entra no catálogo fechado da §2.8 |
| `caller.hasRole` fora do catálogo §2.8 = erro ao pé da letra | idem |
| `visibility` sem `self`/`caller.id` utilizável — bloqueios 1 e 2 de [[visibility-de-view-nao-implementado]] | `caller.subject` + `AccessPolicy` dão a comparação de posse type-safe |
| Posse inexprimível quando o principal não é agregado (`pizzeria`) | `Identity { subject: T }` + `caller.subject` |
| `perUser` de rateLimit sem definição | Chaveado por `caller.id` |
| `access` de UseCase sem semântica de `caller` | Policies sem `self` são válidas ali |
| 401 vs 403 indistinguíveis | §7, tabela de erros |
| Condição de acesso duplicada e sem nome | `AccessPolicy` |

## 9. O que este design **não** resolve, e perguntas em aberto

Honestidade sobre o alcance:

1. [^2]**Onde o bloco `Identity` mora** — módulo `Platform` (precedente do
   `Aggregate Tenant`) ou nível de `service` em [[12-topology]]? Muda o modelo
   de compartilhamento entre módulos.
2. [^3]**Autorização é do domínio ou da borda?** `AccessPolicy` com `self` roda no
   domínio (precisa da instância carregada); sem `self` poderia rodar na borda,
   antes do UseCase. Duas execuções diferentes com uma sintaxe só — precisa de
   regra explícita, ou vira o mesmo problema de pushdown de
   [[spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos]].
3. [^8]**Catálogo de papéis.** `caller.hasRole("staff")` valida `"staff"` contra o
   quê? Um `Enum Role` declarado? Uma lista no bloco `Identity`? Texto livre
   (e então erro de digitação vira negação silenciosa — fail-closed correto,
   mas indepurável)? Recomendo enumerar no bloco `Identity`.
4. [^4]**Migração de `caller.id`.** Com `caller.subject` disponível, `caller.id`
   vira redundante em programas que declaram `subject`? Manter os dois é ter
   duas formas para a mesma pergunta, contra "Uma Forma Canônica".
5. [^5]**Revogação e sessão.** `refresh { rotation, reuseDetection }` pressupõe
   store de sessão. Com `provider: oidc` isso é do provedor externo; com
   `local` é nosso. A superfície declarada não pode ser a mesma nos dois.
6. [^6]**MFA e confirmação alteram fluxo de login**, ou seja, geram máquina de
   estados na borda. Isso é runtime vendorado (como o event store) ou é
   modelável em DomainScript? ASP.NET faz o primeiro.
7. [^7]**Identity é multi-tenant por si?** Um usuário pertence a um tenant, a
   vários, ou é global com papéis por tenant? Muda o schema do store local e é
   decisão de produto, não de coerência interna.

## 10. Recomendação de sequenciamento

Se isto virar spec, a ordem que minimiza retrabalho:

1. **Contrato de `caller` (§4) + entrada no catálogo §2.8.** É correção de uma
   contradição existente, vale mesmo que nada mais deste design avance, e
   desbloqueia duas issues já abertas.
2. **`AccessPolicy` (§5).** Puramente aditivo, não depende do bloco `Identity`,
   e unifica o lowering de acesso que hoje está espalhado.
3. **Bloco `Identity` com `provider: oidc` (§3.2).** O mais barato dos três
   provedores — só valida token e mapeia claim, sem store, sem endpoint.
4. **`subject:` + `caller.subject` (§4.3).** Depende de `ref T`
   ([[spec-v7-identidade-implicita-do-aggregate]]) estar implementado.
5. **`provider: local` + `expose` (§3.1, §6).** O maior, e o único que exige
   runtime novo (hash, token, store).

Os passos 1 e 2 são os de melhor relação valor/risco: consertam o que já está
quebrado hoje e não pressupõem nenhuma decisão das perguntas em aberto da §9.

[^1]: Nota do desenvolvedor: tenant por jwt na verdade se refere ao identificador do usuário/aplicação acessando o sistema, assim o modulo de identity precisa resolver ele primeiramente antes de ser aplicado o tenant, para evitar que um ataque DDoS deixe o modulo de identity sobrecarregado o serviço de identity pode ser utilizado apenas para extrair o identificador do jwt 

[^2]: O identity é definido a nível de serviço
	

[^3]: Autorizações que precisem acessar dados de domínio ficam dentro do escopo do domínio, autorizações  que não dependam de domínio (ex: acessar o recurso no path X com o token de acesso Y) devem viver fora do domínio

[^4]: Utilizaremos a notação caller.id  ao invés de subject

[^5]: se o provider é local as informações deve ser gravadas em um  banco de dados especificado pela infraestrutura

[^6]: Runtime vendorável, similar ao aspnet core identity

[^7]: Identity deve suportar multi tenancy

[^8]: Embora seja contra o design da linguagem usar strings diretamente, e declarar as claims e roles no modulo do identity seja uma solução mais "segura"  e testável, seria interessante termos flexibilidade em termos de poder cadastrar alterar claims/roles, e adicionar-las, remover-las de um usuário via  API
	
