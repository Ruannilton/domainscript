# 02 — Write Side: o núcleo do domínio

Cobre **§4 (O Núcleo do Domínio)**: Errors, Events, versionamento e redação de
eventos, e Aggregates. Reusa os ValueObjects do exemplo `01`.

## As ideias que valem a leitura

**Erro de negócio ≠ erro de infraestrutura (§23).** `Error` no domínio é uma
regra que o usuário violou — vira 4xx. Timeout de banco, conexão recusada,
panic: nada disso se declara aqui. Vive no `mod.ds`, com retry e circuit
breaker. A separação é o que impede o domínio de virar tratamento de rede.

**Metadata de evento é implícito.** `timestamp`, `sequence`, `aggregateId` e
`eventType` estão em todo Event sem serem declarados. É por isso que o `Apply
DepositPerformed` consegue datar a entrada do extrato com `event.timestamp`
sem que `DepositPerformed` tenha um campo `date`.

**`Event` vs `PublicEvent` é uma fronteira de acoplamento.** Uma Policy de
outro módulo escutando um `Event` privado é erro de compilação. Publicar é uma
decisão explícita: o `PublicEvent` vira contrato, e contrato se versiona.

**Evento é imutável e eterno — então versionar não é opcional.** Campo novo
com `default` cobre o caso barato; `Upcast` cobre o que default não resolve
(derivar de outro campo, mudar forma). O compilador exige que toda versão
histórica tenha caminho até a atual, e avisa quando um `Upcast` era só um
`default` disfarçado.

**`redactable` resolve GDPR sem quebrar replay.** Apagar um evento corromperia
o histórico. Redigir um campo troca o valor por um placeholder tipado: a
estrutura fica, o replay roda, a PII sai.

**`access` é fechado por padrão.** Handle sem entrada no bloco é erro de
compilação. Não existe comando desprotegido por esquecimento — o compilador
não deixa.

**`events()` e `AppendList` não competem.** `events()` é o log técnico (o que
aconteceu no sistema); o `AppendList` do state é a visão de negócio (o extrato
do cliente). Modelam coisas diferentes e coexistem de propósito.

## Regras da §25 exercitadas

- Handle sem entrada no `access` → ❌
- Policy cross-module escutando `Event` não público → ❌
- `remove()`/`clear()` em `AppendList<T>` → ❌
- Primitivo no Write Side → ❌
- Upcast substituível por default → ⚠️
