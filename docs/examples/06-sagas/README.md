# 06 — Transações e Sagas

Cobre **§19 (Transações e Sagas)** e **§23 (Erros: negócio vs. infraestrutura)**.

## As ideias que valem a leitura

**A transação é inferida, não declarada.** O compilador olha onde os
Aggregates vivem e decide: mesmo `Database` → commit local; bancos distintos
com XA → 2PC automático; bancos distintos sem XA → **erro**; cross-service sem
Saga → **erro**.

O ponto não é a conveniência de não escrever `begin`/`commit`. É que **não dá
para escrever acidentalmente uma "transação" distribuída que não é
transação**. Ou o compilador prova a atomicidade, ou te obriga a modelar a
compensação explicitamente. Esse é o caso de uso central do "Fail-Fast" da
§1.1.

**Saga é a resposta quando a atomicidade não existe.** Não é um recurso
avançado opcional: é o que o compilador exige quando você atravessa uma
fronteira que não suporta commit atômico.

**`up` / `down` / `onInfraError` separam três coisas que costumam se
confundir.** `up` avança. `down` compensa o que `up` fez. `onInfraError` trata
falha de *infraestrutura* — que é diferente de falha de negócio: negócio
aborta e compensa, infraestrutura pode simplesmente ser tentada de novo.

**`unrecoverable` é honestidade, não desistência.** Nem toda ação tem
compensação — e-mail enviado não desenvia. Declarar `down { unrecoverable }`
faz o runtime gerar alerta para intervenção humana, em vez de o sistema ficar
num estado inconsistente que ninguém percebeu. Fingir uma compensação que não
funciona é pior que declarar que ela não existe.

**`async` vs `await` é sobre quem espera.** `await timeout 60s` segura a
chamada. `async` devolve um `sagaId` na hora e o compilador gera o
`SagaStatus` para consultar depois — o desenho certo quando o fluxo é longo
demais para uma conexão HTTP aberta.

## Regras da §25 exercitadas

- UseCase cross-database sem XA / cross-service sem Saga → ❌
- Módulos em services diferentes sem canal → ❌
- Saga `await` sobre canal `queue` → ⚠️
- FFI impura dentro de transação → ⚠️
