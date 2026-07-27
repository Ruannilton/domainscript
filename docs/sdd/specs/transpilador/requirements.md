# Requirements — Front-end do Transpilador DomainScript

> Documento 1 de 3 do fluxo spec-driven (`requirements` → `design` → `tasks`).
> Define **o quê** e **por quê**. Não define implementação (isso é `design.md`).

## 1. Introdução

### 1.1. Objetivo

Construir o **front-end** de um transpilador para DomainScript (spec v6.0): o
estágio que vai do texto-fonte até um veredito de validação. O front-end recebe
um ou mais arquivos DomainScript e produz (a) uma AST validada e (b) um relatório
de diagnósticos (erros e avisos) com localização precisa. Geração de código Go é
um estágio posterior, **fora do escopo** deste documento.

```
texto-fonte ──▶ [LEXER] ──▶ tokens ──▶ [PARSER] ──▶ AST ──▶ [SEMÂNTICA] ──▶ AST validada
                                                                  │
                                                                  ▼
                                                          relatório de diagnósticos
```

### 1.2. Alinhamento filosófico com o spec

O front-end deve encarnar os mesmos princípios que a linguagem impõe ao usuário:

- **Fail-fast:** todo erro detectável estaticamente é reportado na compilação,
  nunca empurrado para runtime.
- **Diagnósticos como produto:** a mensagem de erro é uma feature de primeira
  classe, não um efeito colateral. Precisa de posição, severidade e texto acionável.
- **Cobertura sobre interrupção:** uma execução reporta o máximo de problemas
  reais possível, em vez de parar no primeiro.
- **Uma forma canônica:** cada construto tem uma única árvore sintática válida; o
  parser não aceita variações "equivalentes".

### 1.3. Escopo

| Em escopo | Fora de escopo |
|---|---|
| Lexer, parser, análise semântica, relatório | Geração de código Go (back-end) |
| Todos os construtos de `*.ds`, `mod.ds`, `interface.ds`, `topology.ds`, `contracts/`, `versions/`, `*.test.ds` | Otimização, runtime, deploy |
| Todas as regras ❌/⚠️ da §23 do spec | Editor/LSP (consome a saída, mas é outro projeto) |
| API programática + CLI | Formatação automática (`fmt`) |

### 1.4. Baseline já existente

A fatia `ValueObject` (lexer + parser + recovery + testes, em Go) já está
implementada e serve de **referência de arquitetura**. Os requisitos abaixo
generalizam aquele padrão para a linguagem inteira e adicionam a fase semântica.

### 1.5. Glossário

| Termo | Definição |
|---|---|
| Diagnóstico | Mensagem localizada (posição + severidade + texto) emitida por qualquer fase |
| Span | Intervalo (início, fim) no source associado a um nó da AST |
| Recovery | Capacidade do parser de continuar após um erro de sintaxe |
| Cascata | Erros falsos gerados como consequência de um erro real anterior |
| Write Side | Aggregates, Commands, Events (§2.1 do spec) |
| Read Side | Views, Queries, Projections |
| EARS | Easy Approach to Requirements Syntax (formato dos critérios abaixo) |

---

## 2. Requisitos Funcionais

> Formato EARS: cada critério usa **WHEN/WHILE/IF … THE SYSTEM SHALL …** para ser
> verificável. "O SISTEMA" = o front-end do transpilador.

### REQ-1 — Análise Léxica

**User story:** Como autor do compilador, quero que o texto-fonte seja convertido
em tokens com posição, para que todas as fases seguintes tenham unidades léxicas
precisas e diagnósticos localizáveis.

**Critérios de aceitação:**

1. WHEN recebe um arquivo-fonte, THE SYSTEM SHALL produzir uma sequência de
   tokens terminada por um token `EOF`.
2. THE SYSTEM SHALL reconhecer identificadores, keywords, literais inteiros,
   decimais, strings, booleanos, durações (ex.: `5s`, `48h`), taxas (ex.:
   `300/min`) e tamanhos (ex.: `100MB`).
3. THE SYSTEM SHALL reconhecer toda a pontuação e operadores do spec (`{ } ( )
   [ ] , . : -> + - * / == != > < >= <= =`).
4. THE SYSTEM SHALL anexar a cada token sua posição (linha e coluna, 1-based).
5. THE SYSTEM SHALL descartar espaços em branco e comentários de linha (`//`)
   sem produzir tokens para eles.
6. WHEN encontra um caractere que não inicia nenhum token válido, THE SYSTEM
   SHALL emitir um diagnóstico de erro e continuar a tokenização (consumindo ao
   menos aquele caractere).
7. WHEN encontra uma string não terminada antes do fim da linha ou do arquivo,
   THE SYSTEM SHALL emitir um diagnóstico de erro localizado no início da string.
8. THE SYSTEM SHALL suportar sequências de escape `\n`, `\t`, `\"` e `\\` dentro
   de strings.

### REQ-2 — Análise Sintática (Parser)

**User story:** Como autor do compilador, quero que os tokens sejam organizados
numa AST tipada que reflita a gramática do spec, para que a fase semântica opere
sobre estrutura e não sobre texto.

**Critérios de aceitação:**

1. THE SYSTEM SHALL reconhecer toda declaração de topo de `*.ds`: `ValueObject`,
   `Enum`, `Error`, `Event`, `PublicEvent`, `Upcast`, `Aggregate`, `Command`,
   `UseCase`, `View`, `Projection`, `Query`, `Policy`, `Worker`, `Notification`,
   `Adapter`, `Foreign`, `Saga`, `Metric`.
2. THE SYSTEM SHALL reconhecer os arquivos de configuração `mod.ds` (`Module` e
   seus blocos), `interface.ds` (`Interface HTTP/GRPC`, `RateLimitTier`),
   `topology.ds` (`Topology`), `contracts/*.ds` (`PublicEvent`) e `versions/*.ds`
   (`Version`).
3. THE SYSTEM SHALL reconhecer os arquivos de teste `*.test.ds` (`Test`,
   `scenario`, `given`/`when`/`then`, `mock`, `fail step`, `property`, `Fixture`).
4. THE SYSTEM SHALL reconhecer os construtos de controle de fluxo `ensure`,
   `match` (statement e expressão), `for`, `log`, `emit`, `return`, e os controles
   de laço `break`, `break all`, `continue`.
5. THE SYSTEM SHALL parsear expressões respeitando a precedência: `or` < `and` <
   igualdade < relacional < aditivo < multiplicativo < unário < pós-fixo (acesso a
   membro, chamada de método, chamada/construção, indexação de range).
6. THE SYSTEM SHALL associar a cada nó da AST um span correspondente à sua
   extensão no source.
7. THE SYSTEM SHALL produzir uma AST mesmo na presença de erros de sintaxe,
   usando nós de erro tipados no lugar de subárvores que não puderam ser parseadas.
8. WHEN a entrada é sintaticamente válida, THE SYSTEM SHALL produzir zero
   diagnósticos de erro de sintaxe.

### REQ-3 — Error Recovery do Parser

**User story:** Como usuário do transpilador, quero ver vários erros de sintaxe
numa única execução, para corrigir mais rápido sem o ciclo "corrige um, recompila,
descobre o próximo".

**Critérios de aceitação:**

1. WHEN encontra um token inesperado, THE SYSTEM SHALL emitir um diagnóstico e
   retomar o parsing em vez de abortar.
2. WHEN um token esperado está ausente mas o token seguinte corresponde ao
   esperado, THE SYSTEM SHALL tratar o token atual como ruído (deleção de token
   único) e continuar.
3. WHEN um token esperado está ausente e não há correspondência adjacente, THE
   SYSTEM SHALL reportar o esperado e continuar sem consumir (inserção virtual).
4. WHILE em modo de pânico, THE SYSTEM SHALL descartar tokens apenas até um token
   de sincronização do nível corrente ou de um nível ancestral, nunca consumindo o
   delimitador de fechamento de um bloco externo.
5. WHEN um erro é emitido, THE SYSTEM SHALL suprimir novos diagnósticos até que ao
   menos N tokens (N ≥ 1, configurável) tenham sido consumidos com sucesso, para
   evitar cascata.
6. THE SYSTEM SHALL garantir progresso: nenhum caminho de parsing pode deixar o
   cursor estacionado, sob pena de laço infinito.
7. WHEN uma declaração de topo falha, THE SYSTEM SHALL reancorar na próxima
   declaração de topo e reconhecê-la independentemente.

### REQ-4 — Tabela de Símbolos e Resolução de Nomes

**User story:** Como usuário, quero que referências a tipos, eventos, comandos e
erros inexistentes sejam detectadas, para não descobrir o erro só em runtime.

**Critérios de aceitação:**

1. THE SYSTEM SHALL registrar, numa passagem de coleta, todos os símbolos
   declarados (ValueObjects, Enums, Aggregates, Events, Commands, UseCases,
   Queries, Notifications, Adapters, Errors, etc.), com escopo por módulo.
2. WHEN uma referência aponta para um símbolo não declarado, THE SYSTEM SHALL
   emitir um diagnóstico de erro na posição da referência.
3. WHEN dois símbolos do mesmo tipo têm o mesmo nome no mesmo escopo, THE SYSTEM
   SHALL emitir um diagnóstico de erro de declaração duplicada.
4. THE SYSTEM SHALL resolver referências por `ref` (ex.: `walletId ref Wallet`),
   por `handles` (UseCase/Saga → Command), por `on` (Policy → Event), e por nome
   de tipo em campos e parâmetros.
5. WHILE resolve nomes, THE SYSTEM SHALL pular subárvores marcadas com nós de
   erro, sem reportar a ausência como erro semântico adicional.

### REQ-5 — Validação Semântica (regras da §23)

**User story:** Como usuário, quero que toda regra "fail-fast" da linguagem seja
verificada estaticamente, porque a promessa do DomainScript é "se compila, a
arquitetura está correta".

**Critérios de aceitação (erros ❌):**

1. WHEN um tipo primitivo (`integer`, `decimal`, `string`, `boolean`, `datetime`)
   é usado no Write Side (Aggregate, Command, Event), THE SYSTEM SHALL emitir erro.
2. WHEN um `Handle` não tem entrada correspondente no bloco `access` do Aggregate,
   THE SYSTEM SHALL emitir erro.
3. WHEN existe uma `Notification` sem `Adapter` correspondente, THE SYSTEM SHALL
   emitir erro.
4. WHEN `remove()` ou `clear()` é invocado sobre um `AppendList<T>`, THE SYSTEM
   SHALL emitir erro.
5. WHEN um `match` sobre conjunto fechado não é exaustivo, ou usa `_` sobre enum
   sem guards, ou usa guards (`when`) sem `_`, THE SYSTEM SHALL emitir erro.
6. WHEN `Nop` é usado como ação em `Handle` ou `UseCase`, THE SYSTEM SHALL emitir
   erro.
7. WHEN `break`, `break all` ou `continue` aparece fora de um `for`, THE SYSTEM
   SHALL emitir erro.
8. WHEN uma `Policy` cross-module escuta um `Event` que não é `PublicEvent`, THE
   SYSTEM SHALL emitir erro.
9. WHEN um UseCase opera cross-database sem ambos os bancos suportarem XA, THE
   SYSTEM SHALL emitir erro; WHEN opera cross-service sem Saga, THE SYSTEM SHALL
   emitir erro.
10. WHEN há um `JOIN` cross-database numa Query, THE SYSTEM SHALL emitir erro.
11. WHEN módulos estão em services diferentes sem canal declarado na topologia,
    THE SYSTEM SHALL emitir erro.
12. WHEN há acesso cross-tenant sem o opt-in `tenancy: cross_tenant`, THE SYSTEM
    SHALL emitir erro.
13. WHEN um upcast de versão referencia um campo obrigatório sem default, THE
    SYSTEM SHALL emitir erro.
14. WHEN um teste referencia um evento ou comando inexistente, ou usa shape de
    evento incorreta, ou `fail step X` onde X não existe, ou um mock com tipo de
    retorno errado, THE SYSTEM SHALL emitir erro.
15. WHEN um `Foreign`/`Adapter` é usado com assinatura incompatível, THE SYSTEM
    SHALL emitir erro.

**Critérios de aceitação (avisos ⚠️):**

16. WHEN um canal `queue`/`stream` não declara `orderBy`, THE SYSTEM SHALL emitir
    aviso.
17. WHEN uma Saga `await` opera sobre canal `queue`, THE SYSTEM SHALL emitir aviso.
18. WHEN um `Upcast` poderia ser substituído por um `default`, THE SYSTEM SHALL
    emitir aviso.
19. WHEN um ValueObject poderia ser um Enum, THE SYSTEM SHALL emitir aviso.
20. WHEN há cache em listagem de alta cardinalidade, THE SYSTEM SHALL emitir aviso.
21. WHEN um UseCase cross-tenant é declarado, THE SYSTEM SHALL emitir aviso (trilha
    de auditoria).
22. WHEN um `Handle` não tem cenário de erro testado, THE SYSTEM SHALL emitir aviso
    de cobertura.
23. WHEN um UseCase ou Query não é exposto em nenhuma interface, THE SYSTEM SHALL
    emitir aviso.

> Nota: regras dependentes de múltiplos arquivos (9–12, 16–17, 23) exigem que a
> análise opere sobre o programa inteiro, não arquivo a arquivo. Ver REQ-7.

### REQ-6 — Relatório de Diagnósticos

**User story:** Como usuário, quero um relatório claro e ordenado de tudo que está
errado, para corrigir de cima para baixo no arquivo.

**Critérios de aceitação:**

1. THE SYSTEM SHALL acumular diagnósticos de todas as fases (lexer, parser,
   semântica) numa coleção única.
2. THE SYSTEM SHALL classificar cada diagnóstico por severidade: `error` ou
   `warning`.
3. THE SYSTEM SHALL ordenar os diagnósticos por posição (linha, depois coluna)
   antes de apresentá-los.
4. THE SYSTEM SHALL descartar diagnósticos duplicados exatos (mesma posição, mesma
   severidade, mesma mensagem).
5. WHEN o número de erros excede um teto configurável (padrão 100), THE SYSTEM
   SHALL parar de coletar e emitir um diagnóstico final indicando supressão.
6. THE SYSTEM SHALL renderizar cada diagnóstico no formato
   `linha:coluna: severidade: mensagem`.
7. WHEN há ao menos um diagnóstico de severidade `error`, THE SYSTEM SHALL sinalizar
   falha (ex.: exit code ≠ 0 na CLI; flag de erro na API).
8. THE SYSTEM SHALL produzir mensagens acionáveis (o que se esperava e o que se
   encontrou), nunca apenas "erro de sintaxe".

### REQ-7 — Análise Cross-File de Programa

**User story:** Como usuário com um sistema multi-módulo, quero que regras que
cruzam arquivos (transações, canais, exposição) sejam validadas, porque é onde os
erros arquiteturais reais acontecem.

**Critérios de aceitação:**

1. THE SYSTEM SHALL aceitar um conjunto de arquivos (um diretório de projeto) e
   construir um modelo de programa unificado antes da validação cross-file.
2. THE SYSTEM SHALL construir o grafo de módulos, services e canais a partir de
   `topology.ds` e `mod.ds`.
3. WHILE valida regras cross-file, THE SYSTEM SHALL ter acesso simultâneo a todos
   os símbolos de todos os módulos do programa.
4. THE SYSTEM SHALL distinguir `Event` (privado ao módulo) de `PublicEvent`
   (compartilhado via `contracts/`) ao validar visibilidade cross-module.

### REQ-8 — Interface de Uso (API + CLI)

**User story:** Como integrador, quero invocar o front-end tanto por linha de
comando quanto programaticamente, para usá-lo em CI e em ferramentas.

**Critérios de aceitação:**

1. THE SYSTEM SHALL expor uma função programática que recebe fonte(s) e retorna
   a AST e a coleção de diagnósticos.
2. THE SYSTEM SHALL prover uma CLI que aceita um arquivo ou diretório e imprime o
   relatório de diagnósticos.
3. WHEN invocado via CLI sem erros, THE SYSTEM SHALL retornar exit code 0; WHEN com
   erros, exit code ≠ 0.
4. THE SYSTEM SHALL processar um diretório de projeto inteiro numa única invocação.

---

## 3. Requisitos Não-Funcionais

### NFR-1 — Qualidade de Diagnóstico
Toda mensagem de erro deve indicar posição precisa e ser acionável. Um erro de
sintaxe nunca deve gerar mais de um pequeno número fixo de diagnósticos de cascata.

### NFR-2 — Robustez
O front-end nunca entra em pânico de runtime (crash) nem em laço infinito, para
nenhuma entrada — válida, inválida, truncada ou adversária. Entrada malformada
sempre resulta em diagnósticos, jamais em travamento.

### NFR-3 — Determinismo
A mesma entrada produz exatamente o mesmo conjunto de diagnósticos, na mesma ordem,
em toda execução.

### NFR-4 — Testabilidade
Cada fase é testável isoladamente. Cada regra ❌/⚠️ da §23 tem ao menos um teste
positivo (dispara) e um negativo (não dispara em código correto).

### NFR-5 — Extensibilidade
Adicionar um novo construto à linguagem deve exigir mudanças localizadas (um novo
caso no parser, um novo nó de AST, uma nova regra semântica), sem reescrever as
fases existentes.

### NFR-6 — Separação de Fases
Sintaxe e semântica permanecem estritamente separadas: o parser não conhece regras
semânticas; a semântica não re-tokeniza nem re-parseia. O contrato entre elas é a
AST mais a coleção de diagnósticos.

### NFR-7 — Performance
O front-end completa a análise de um projeto de tamanho típico em tempo
proporcional ao tamanho da entrada (linear no número de tokens para lexer e parser).
Sem metas de latência rígidas neste estágio, mas sem comportamento superlinear
evitável.

---

## 4. Rastreabilidade

Cada requisito mapeia para itens de `design.md` e `tasks.md`:

| Requisito | Tema | Fase |
|---|---|---|
| REQ-1 | Lexer | Léxica |
| REQ-2 | Parser + AST | Sintática |
| REQ-3 | Recovery | Sintática |
| REQ-4 | Símbolos / nomes | Semântica |
| REQ-5 | Regras §23 | Semântica |
| REQ-6 | Diagnósticos | Transversal |
| REQ-7 | Programa cross-file | Semântica |
| REQ-8 | API + CLI | Driver |

---

## 5. Critérios de Pronto (Definition of Done)

O front-end está completo quando:

1. Todos os construtos do spec v6 são reconhecidos pelo parser (REQ-2).
2. Toda regra ❌ da §23 é detectada e reportada como erro (REQ-5).
3. Toda regra ⚠️ da §23 é detectada e reportada como aviso (REQ-5).
4. O parser recupera de erros e reporta múltiplos problemas por execução (REQ-3).
5. Cada fase e cada regra têm cobertura de teste positivo e negativo (NFR-4).
6. A CLI processa um diretório de projeto e retorna exit code coerente (REQ-8).
7. Nenhuma entrada causa crash ou laço infinito (NFR-2).
