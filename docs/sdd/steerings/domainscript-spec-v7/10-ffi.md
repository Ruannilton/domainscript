# 10. FFI Geral (`Foreign`)

Mecanismo para usar bibliotecas da linguagem alvo em algoritmos custom, **desacoplado de Notifications**. É o único buraco controlado na pureza do domínio — por isso é explícito, tipado e restrito por contexto.

## 10.1. Pure vs Impure

O dev declara explicitamente a natureza de cada função:

| Natureza | Definição | Exemplos |
|----------|-----------|----------|
| `pure` | Determinística, sem efeitos colaterais. Mesmo input → mesmo output. | Hash, verificação de assinatura, parsing, compressão |
| `impure` | Efeito colateral ou não-determinismo. | Geração de PDF com temp files, leitura de hardware, estado global |

## 10.2. Declaração

Assinaturas em `foreign/*.ds`; implementação na linguagem alvo:

```ds
// foreign/crypto.ds
Foreign "go" from "foreign/crypto" {
    pure function ComputeMerkleRoot(items List<bytes>) -> bytes
    pure function VerifySignature(message bytes, signature bytes, publicKey string) -> boolean
    pure function ValidateTaxId(taxId string) -> boolean throws InvalidTaxIdError
}

Foreign "go" from "foreign/documents" {
    impure function GeneratePdf(template string, data Map<string, string>) -> bytes
        throws PdfGenerationError
}
```

```go
// foreign/crypto/crypto.go
package crypto

func ComputeMerkleRoot(items [][]byte) []byte { ... }
func VerifySignature(message, signature []byte, publicKey string) bool { ... }
```

## 10.3. Marshalling (o que cruza a fronteira)

O compilador gera todo o marshalling automaticamente:

| Tipo DomainScript | Cruza? | Mapeamento (Go) |
|-------------------|--------|-----------------|
| Primitivos | ✅ | Tipos nativos |
| `List<T>`, `Set<T>`, `Map<K,V>` | ✅ | slice, map |
| ValueObject | ✅ | struct |
| Enum | ✅ | Valor do tipo base |
| Event | ✅ | struct de dados |
| `FileRef` / `File` / `FileStream` | ✅ | struct / bytes / reader |
| **Aggregate** | ❌ | **Erro de compilação** |

**Aggregates nunca atravessam** — têm identidade, ciclo de vida e fronteira transacional. Passe ValueObjects ou campos específicos.

## 10.4. Onde cada tipo pode ser chamado

| Contexto | `pure` | `impure` |
|----------|--------|----------|
| ValueObject (Valid/Operator) | ✅ | ❌ |
| Handle | ✅ | ⚠️ só se resultado for capturado em evento |
| **Apply** | ❌ | ❌ |
| UseCase / Saga / Policy / Worker | ✅ | ✅ |
| Query | ✅ | ❌ |

**Apply é hermético** — nem FFI pura. Depende só do evento e de built-ins, garantindo que replay anos depois produza o mesmo estado mesmo se a biblioteca mudou.

**Impure no Handle exige captura em evento** (mesmo princípio de `now()`/`uuid()`):

```ds
Handle SignDocument(content bytes) {
    signature = sign_via_hsm(content)      // impure — DEVE ir para o evento
    emit DocumentSigned(self.id, signature)
}

Apply DocumentSigned {
    state.signature = event.signature      // lê do evento, nunca re-executa
}
```

Resultado de impure usado em controle de fluxo do Handle sem captura → erro de compilação.

## 10.5. Erros

`throws DomainError` declara erros de negócio (HTTP 4xx). Qualquer falha não-mapeada (panic, timeout) é `InfraError` — sujeita a retry/circuit breaker do `mod.ds`.

## 10.6. Testing de FFI

| Natureza | Comportamento no teste |
|----------|------------------------|
| `pure` | Executa de verdade (determinística) ou golden value via `mock` |
| `impure` | Mockada por padrão, como Adapters |

```ds
scenario "assinatura de documento" {
    mock sign_via_hsm returns "SIGNATURE_BYTES"
    when SignDocument(content: ...)
    then [ DocumentSigned(signature: "SIGNATURE_BYTES") ]
}
```

## 10.7. Diretrizes

- Função que precisa de config/credenciais → deveria ser **Adapter**, não FFI (compilador orienta).
- Volumes grandes → `FileStream`, não passagem em memória.
- Declarar `pure` para função com estado interno é bug do dev — não detectável pelo compilador.
- FFI impura dentro de transação → warning: efeito não é revertido em rollback; considere Saga.

