# Requirements — Resolução Completa de Nomes & Tipos

> Documento 1 de 3 de um **novo** ciclo spec-driven (`requirements` → `design` →
> `tasks`), independente da spec do front-end em `.claude/specs/`. Define **o quê**
> e **por quê** desta etapa. Não define implementação (isso é `design.md`).
>
> Continuidade de numeração: este ciclo continua a série do projeto a partir de
> `REQ-9` e `NFR-8` (o front-end vai até `REQ-8`/`NFR-7`), para um namespace de
> rastreabilidade único.

## 1. Introdução

### 1.1. Contexto e problema

O front-end do transpilador (spec em `.claude/specs/`) está marcado como concluído
e "pronto para produção" (Fases 0–11, Marco D). Apesar disso, ele **deixa passar
erros reais e triviais** porque a fase de resolução de nomes (REQ-4) foi
especificada e implementada de forma incompleta.

Evidência concreta, no projeto de exemplo `docs/examples/wallet`:

| Local | Escrito | Correto | Classe do erro |
|---|---|---|---|
| `domain.ds:76` | `emit DepositPerformed(self.id, amoun, ...)` | `amount` | identificador solto não resolvido num corpo executável |
| `domain.ds:71` | `caller.id == self.i` | `self.id` | acesso a membro a campo inexistente do receptor |
| `mod.ds:7` | `manages: [Walle]` | `Wallet` | referência a símbolo numa lista de configuração |

`go run ./cmd/dsc docs/examples/wallet` hoje retorna **zero diagnósticos e exit
code 0** — o validador aprova um programa com três typos.

**Causa-raiz (lacuna de especificação, não só de código).** O `design.md` §3.7 do
front-end define a passagem de resolução *literalmente* só para `ref`, `handles`,
`on` e nomes de tipo em campos/parâmetros. Em consequência:

- O resolver **nunca percorre os corpos executáveis** (`Handle`, `Apply`, `Valid`,
  `Operator`, `coerce`, `execute`, `source`, steps de `Saga`, …). Identificadores
  soltos dentro deles não são resolvidos contra escopo algum.
- O resolver **não resolve referências a símbolos em arquivos de configuração**
  (`manages`, alvos de rota/rpc, módulos de service, endpoints de canal, alvos de
  versão).
- **Não existe modelo de tipos.** Acesso a membro (`x.campo`) nunca é validado
  contra a forma do tipo de `x`.

### 1.2. Alinhamento filosófico

A lacuna viola dois princípios que o próprio front-end declara (requirements.md
§1.2 do front-end):

- **Fail-fast:** todo erro detectável estaticamente é reportado na compilação,
  nunca empurrado para runtime. Um nome inexistente é o caso mais básico disso.
- **Diagnósticos como produto:** a promessa do DomainScript ("se compila, está
  correto") é falsa enquanto typos passam silenciosos.

### 1.3. Escopo

| Em escopo | Fora de escopo |
|---|---|
| Resolução de identificadores soltos em **todos** os corpos executáveis | Geração de código Go (back-end) |
| Resolução de referências a símbolos em **config** (`mod.ds`, `interface.ds`, `topology.ds`, `versions/`) | Runtime, deploy, otimização |
| **Modelo de tipos** para declarações e expressões | Inferência além do necessário para checar nomes/membros/compatibilidade |
| Checagem de **acesso a membro** contra o tipo do receptor | Editor/LSP |
| Checagem de **compatibilidade de tipos** (nível definido no design) | Reescrita das fases existentes do front-end |

Esta etapa **estende** o front-end existente; não o substitui. As fases lexer,
parser e as regras da §23 permanecem como estão.

### 1.4. Glossário (incremental)

| Termo | Definição |
|---|---|
| Escopo léxico | Conjunto de nomes visíveis num ponto do corpo (params, vars de `for`, params de lambda, bindings de `match`/query, mais os símbolos do módulo) |
| Receptor contextual | Nome implícito disponível dentro de um construto: `self`, `state`, `event`, `caller`, `value`, `ok` |
| Tipo | Modelo estático de um valor: primitivo, VO (wrapper/composto), Enum, coleção genérica, shape de Aggregate/Event/Command, função/operador |
| Membro | Campo, propriedade ou método acessível por `.` sobre um valor de um tipo |
| Binder | Construto que introduz nomes novos no escopo: `for`, lambda, braço de `match`, binding de query (`list T t`, `as`) |

---

## 2. Requisitos Funcionais

> Formato EARS (**WHEN/WHILE/IF … THE SYSTEM SHALL …**). "O SISTEMA" = o front-end
> estendido com este trabalho.

### REQ-9 — Resolução de Nomes em Corpos Executáveis

**User story:** Como usuário, quero que um identificador digitado errado dentro de
um `Handle`, `Apply`, `Valid`, `execute` ou qualquer corpo seja detectado, para não
descobrir o typo só em runtime.

**Critérios de aceitação:**

1. THE SYSTEM SHALL percorrer todos os corpos executáveis de toda declaração
   (`Handle`, `Apply`, `Valid`, `Operator`, `coerce`, `execute`, `source`, e os
   `up`/`down`/`onInfraError` de `Saga`) e resolver cada identificador solto.
2. THE SYSTEM SHALL resolver um identificador solto contra, em ordem: o escopo
   léxico local (parâmetros, vars de `for`, params de lambda, bindings de `match` e
   de query/`as`), os receptores contextuais aplicáveis ao construto, e os símbolos
   do módulo (incluindo membros de Enum acessíveis por nome).
3. THE SYSTEM SHALL disponibilizar os receptores contextuais corretos por
   construto: `self`/`state` em `Handle`/`Apply`/`access` de Aggregate; `event` em
   `Apply`; `caller` onde há controle de acesso; `value`/`ok` em `Valid`; e os
   parâmetros nomeados de `Handle`/`Operator`/`UseCase`/`Query`.
4. WHEN um identificador solto não resolve em nenhum escopo, THE SYSTEM SHALL emitir
   um diagnóstico de erro na posição do identificador, indicando o nome não
   declarado.
5. THE SYSTEM SHALL respeitar o aninhamento de binders: um nome introduzido por
   `for`/lambda/`match`/query é visível apenas dentro do seu corpo e some ao sair.
6. WHILE resolve corpos, THE SYSTEM SHALL pular subárvores marcadas com nós de erro,
   sem reportar a ausência como erro adicional (preserva REQ-4.5).

### REQ-10 — Resolução de Referências em Configuração

**User story:** Como usuário, quero que um nome de Aggregate/UseCase/Module/Service
digitado errado num arquivo de configuração seja detectado, porque é onde a
topologia do sistema é amarrada.

**Critérios de aceitação:**

1. THE SYSTEM SHALL resolver toda referência a símbolo presente em arquivos de
   configuração contra o `Kind` esperado, no mínimo: `manages` (→ Aggregate),
   alvo de `Route`/`rpc` (→ UseCase/Query), módulos de `service` (→ Module),
   endpoints `From`/`To` de canal (→ Module — os canais ligam módulos), e alvo de
   `route` de versão (→ UseCase).
2. WHEN uma referência de config aponta para um nome não declarado, THE SYSTEM
   SHALL emitir erro na posição da referência.
3. WHEN uma referência de config resolve a um símbolo do `Kind` errado (ex.:
   `manages` apontando para um Event), THE SYSTEM SHALL emitir erro indicando o
   `Kind` esperado vs. o encontrado.

### REQ-11 — Modelo de Tipos

**User story:** Como autor do compilador, quero que cada declaração e cada
expressão tenha um tipo estático conhecido, para poder checar acesso a membro e
compatibilidade.

**Critérios de aceitação:**

1. THE SYSTEM SHALL atribuir um tipo a cada declaração nomeada: VO (wrapper ou
   composto, com seus campos), Enum (com seus membros), Aggregate/Event/Command
   (shape de campos), Query/Operator (assinatura), coleção genérica (`List<T>` etc.).
2. THE SYSTEM SHALL inferir um tipo para cada expressão de um corpo resolvido, a
   partir do tipo de suas subexpressões (literais, construções, acessos, chamadas,
   operadores).
3. WHEN o tipo de uma expressão não pode ser determinado por causa de um erro
   anterior, THE SYSTEM SHALL marcá-la com um tipo de erro e **não** propagar
   diagnósticos de cascata a partir dela (preserva NFR-1/anti-cascata).

### REQ-12 — Checagem de Acesso a Membro

**User story:** Como usuário, quero que `self.i` (campo inexistente) seja detectado,
para que acessos a membro inválidos não cheguem ao runtime.

**Critérios de aceitação:**

1. THE SYSTEM SHALL validar todo acesso `X.nome`: `nome` deve ser um membro
   (campo/propriedade/método) válido do tipo de `X`.
2. THE SYSTEM SHALL cobrir prioritariamente os receptores estaticamente conhecidos:
   `self`/`state` resolvem aos campos do `state` do Aggregate; `event` aos campos do
   Event do `Apply`; `caller` à shape do chamador.
3. WHEN `X.nome` usa um membro inexistente para o tipo de `X`, THE SYSTEM SHALL
   emitir erro na posição do membro, idealmente sugerindo o membro mais próximo.
4. WHILE o tipo de `X` for um tipo de erro, THE SYSTEM SHALL **não** reportar o
   acesso a membro (anti-cascata).

### REQ-13 — Checagem de Compatibilidade de Tipos

**User story:** Como usuário, quero que usos com tipos incompatíveis (atribuição,
argumento, operador) sejam reportados, para apanhar a classe de erro que nomes
sozinhos não pegam.

**Critérios de aceitação:**

1. THE SYSTEM SHALL checar compatibilidade de tipo em: atribuição (`a = b`),
   argumentos de construção/chamada, operandos de operadores, e valor de `return`,
   no nível de profundidade definido pelo `design.md`.
2. WHEN um uso é incompatível, THE SYSTEM SHALL emitir erro acionável (tipo
   esperado vs. tipo encontrado), na posição do uso.
3. THE SYSTEM SHALL tratar a Regra de Ouro do Write Side (REQ-5.1) e este sistema de
   tipos de forma consistente, sem diagnósticos duplicados para a mesma causa.

---

## 3. Requisitos Não-Funcionais (incrementais)

> Os NFR-1..7 do front-end continuam valendo integralmente. Abaixo, os adicionais.

### NFR-8 — Reuso de Infraestrutura
A travessia de AST deve reusar os utilitários existentes (`sema/walk.go`:
`forEachExpr`, `forEachStmt`, `forEachExprInBlock`, `declBlocks`, `stateField`) em
vez de reescrever percursos. Adicionar um construto novo continua exigindo mudança
localizada (preserva NFR-5).

### NFR-9 — Determinismo e Anti-Cascata Preservados
A mesma entrada produz o mesmo conjunto de diagnósticos na mesma ordem (NFR-3). Um
único erro de nome/tipo não pode gerar mais que um pequeno número fixo de
diagnósticos derivados (NFR-1): um tipo de erro absorve a cascata.

### NFR-10 — Cobertura de Teste Pareada
Cada regra nova (REQ-9..13) tem ao menos um teste positivo (dispara o diagnóstico) e
um negativo (silêncio em código correto), espelhando NFR-4.

---

## 4. Rastreabilidade

| Requisito | Tema | Fase (tasks.md) |
|---|---|---|
| REQ-9 | Nomes em corpos | Fase A |
| REQ-10 | Refs em config | Fase B |
| REQ-11 | Modelo de tipos | Fase C |
| REQ-12 | Acesso a membro | Fase D |
| REQ-13 | Compatibilidade de tipos | Fase E |

---

## 5. Critérios de Pronto (Definition of Done)

Esta etapa está completa quando:

1. `emit DepositPerformed(self.id, amoun, ...)` (`domain.ds:76`) gera um erro de
   **nome não declarado** localizado em `amoun` (REQ-9).
2. `manages: [Walle]` (`mod.ds:7`) gera um erro de **referência não declarada**
   localizado em `Walle` (REQ-10).
3. `caller.id == self.i` (`domain.ds:71`) gera um erro de **membro inexistente**
   localizado em `.i`, com `self`/`state` resolvidos aos campos do Aggregate (REQ-12).
4. Corrigidos os três typos, `dsc docs/examples/wallet` volta a **silêncio** (exit 0).
5. Cada regra REQ-9..13 tem par de testes positivo+negativo (NFR-10).
6. Determinismo e limite de cascata revalidados (NFR-9); `go build ./...` e
   `go test ./...` verdes.
