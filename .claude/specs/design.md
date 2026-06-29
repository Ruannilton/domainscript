# Design — Front-end do Transpilador DomainScript

> Documento 2 de 3 do fluxo spec-driven (`requirements` → `design` → `tasks`).
> Define **como** os requisitos de `requirements.md` serão atendidos. Cada decisão
> referencia os REQ/NFR que satisfaz.

## 1. Visão Arquitetural

### 1.1. Pipeline

O front-end é um pipeline de quatro estágios com um canal de diagnósticos
transversal. Cada estágio consome a saída do anterior; o `DiagnosticBag` é
compartilhado e acumulativo (REQ-6.1, NFR-6).

```
                         ┌─────────────────────────────────────────────┐
                         │            DiagnosticBag (compartilhado)      │
                         └───▲──────────▲───────────▲──────────▲────────┘
                             │          │           │          │
   source ──▶ ┌────────┐ tokens ┌────────┐ AST ┌──────────┐ AST ┌──────────┐
              │ LEXER  │───────▶ │ PARSER │────▶│ RESOLVER │────▶│ CHECKER  │──▶ AST validada
              └────────┘         └────────┘     └──────────┘     └──────────┘
                 REQ-1            REQ-2/3         REQ-4            REQ-5
```

- **LEXER** (REQ-1): texto → tokens com span.
- **PARSER** (REQ-2, REQ-3): tokens → AST, com recovery.
- **RESOLVER** (REQ-4): coleta símbolos, resolve nomes, anota a AST com bindings.
- **CHECKER** (REQ-5): aplica as regras semânticas da §23 sobre a AST resolvida.

Para projetos multi-arquivo, um estágio de **agregação de programa** (REQ-7)
roda entre PARSER e RESOLVER: parseia cada arquivo e une as ASTs num modelo de
programa antes da resolução global.

### 1.2. Princípio de separação (NFR-6)

A fronteira sintaxe/semântica é dura e unidirecional:

- O **parser não conhece nenhuma regra da §23.** Ele aceita tudo que é
  gramaticalmente bem-formado, inclusive programas semanticamente impossíveis
  (primitivo no Write Side, `match` não-exaustivo, `Nop` em Handle). Esses são
  *sintaticamente válidos*.
- A **semântica não re-tokeniza nem re-parseia.** Opera só sobre a AST + spans.
- O **único contrato** entre fases é `(AST, DiagnosticBag)`.

Essa disciplina é o que torna cada fase testável isoladamente (NFR-4) e a
linguagem extensível (NFR-5).

### 1.3. Linguagem e justificativa

**Go**, alinhado com o alvo de transpilação do DomainScript e com a baseline
`ValueObject` já implementada. Parser **recursive descent escrito à mão** (não
gerador), pela razão central do projeto: controle total sobre mensagens de erro e
recovery (REQ-3, NFR-1) — geradores tendem a produzir diagnósticos genéricos.

---

## 2. Estrutura de Pacotes

```
ds/
├── go.mod
├── cmd/
│   └── dsc/
│       └── main.go            # CLI (REQ-8)
├── token/
│   └── token.go               # TokenKind, Token, Pos, keywords
├── diag/
│   └── diagnostic.go          # Diagnostic, Severity, DiagnosticBag
├── lexer/
│   └── lexer.go               # REQ-1
├── ast/
│   ├── ast.go                 # interfaces Node/Expr/Stmt/Decl
│   ├── decl.go                # nós de declaração (Aggregate, UseCase, ...)
│   ├── stmt.go                # nós de statement (ensure, match, for, ...)
│   ├── expr.go                # nós de expressão
│   └── error_nodes.go         # ErrorDecl/ErrorExpr/ErrorStmt
├── parser/
│   ├── parser.go              # cursor, expect, synchronize, janela de silêncio
│   ├── parse_decl.go          # REQ-2: declarações de topo
│   ├── parse_stmt.go          # REQ-2: statements e controle de fluxo
│   ├── parse_expr.go          # REQ-2: expressões com precedência
│   ├── parse_config.go        # REQ-2: mod.ds / interface.ds / topology.ds
│   ├── parse_test.go          # REQ-2: *.test.ds
│   └── sync_sets.go           # conjuntos de sincronização por nível (REQ-3)
├── symbols/
│   └── table.go               # REQ-4: SymbolTable, escopos
├── resolver/
│   └── resolver.go            # REQ-4: coleta + resolução de nomes
├── sema/
│   ├── checker.go             # orquestra as regras
│   ├── rules_types.go         # REQ-5: primitivo no write side, AppendList, ...
│   ├── rules_flow.go          # REQ-5: match exaustivo, Nop, break/continue
│   ├── rules_domain.go        # REQ-5: access, Notification/Adapter, Policy
│   ├── rules_program.go       # REQ-5/7: cross-db, cross-service, canais
│   └── rules_warnings.go      # REQ-5: todos os ⚠️
├── program/
│   └── program.go             # REQ-7: agrega arquivos num modelo unificado
└── driver/
    └── driver.go              # REQ-8: orquestra o pipeline, API pública
```

A baseline atual (pacote `main` chato) migra para esse layout: `token.go` →
`token/`, `lexer.go` → `lexer/`, etc. Decisão de design: **um pacote por
responsabilidade**, dependências apontando sempre "para baixo" (driver → sema →
resolver → parser → lexer → ast/token/diag).

---

## 3. Componentes

### 3.1. Token (`token/`) — REQ-1

```go
type TokenKind int   // EOF, IDENT, INT, FLOAT, STR, keywords, pontuação, operadores

type Pos struct { Line, Col int }      // 1-based (REQ-1.4)

type Token struct {
    Kind TokenKind
    Lit  string       // lexema (sem aspas, no caso de STR)
    Pos  Pos
}
```

Keywords num `map[string]TokenKind`. Literais compostos do domínio (duração,
taxa, tamanho) são reconhecidos pelo lexer e podem ser carregados como subtipos de
`Lit` ou kinds dedicados (`DURATION`, `RATE`, `SIZE`) — decisão: **kinds dedicados**,
para o parser distinguir sem re-parsear o lexema.

### 3.2. Lexer (`lexer/`) — REQ-1

Single-pass sobre `[]rune`, um rune de lookahead (`peek`/`peek2`). Decisões:

- **Números:** consome dígitos; só vira `FLOAT` se houver dígito após o `.` (evita
  engolir o ponto de acesso a membro `foo.bar`) (REQ-1.2).
- **Sufixos:** após um número, tenta casar unidade de tempo (`ms/s/min/h/d`),
  taxa (`/min`) ou tamanho (`KB/MB/GB`) para emitir `DURATION`/`RATE`/`SIZE`.
- **Strings:** terminam em `"` ou erro se cruzarem linha/EOF (REQ-1.7); escapes
  `\n \t \" \\` (REQ-1.8).
- **Comentários:** `//` até fim de linha, descartados (REQ-1.5).
- **Caractere inválido:** consome ao menos um rune e emite diagnóstico — garante
  progresso e evita laço (REQ-1.6, NFR-2).

Saída: `([]Token, []Diagnostic)`. Erros léxicos entram no `DiagnosticBag` no driver.

### 3.3. AST (`ast/`) — REQ-2.6, REQ-2.7

Hierarquia por interfaces marcadoras:

```go
type Node interface { Pos() Pos; Span() Span; node() }
type Decl interface { Node; decl() }
type Stmt interface { Node; stmt() }
type Expr interface { Node; expr() }
```

Decisões:

- **Span por nó** (REQ-2.6): cada nó guarda `(start, end)`, não só o início. A
  baseline guarda só `Pos`; o design estende para `Span` para diagnósticos
  semânticos que precisam sublinhar um intervalo.
- **Nós de erro tipados** (REQ-2.7, REQ-4.5): `ErrorDecl`, `ErrorStmt`, `ErrorExpr`
  implementam as interfaces respectivas. O parser **nunca retorna `nil`**; fases
  posteriores pulam subárvores que contêm um nó de erro (REQ-4.5), evitando que um
  erro de sintaxe vire um erro semântico falso.
- **Expressões uniformes:** acesso a membro, método e construção são todos
  `CallExpr`/`MemberExpr` encadeados em pós-fixo (REQ-2.5), espelhando a EBNF.

### 3.4. Diagnostics (`diag/`) — REQ-6

```go
type Severity int   // SeverityError | SeverityWarning

type Diagnostic struct { Severity; Pos; Msg string }

type DiagnosticBag struct {
    items   []Diagnostic
    seen    map[string]bool   // dedup exato (REQ-6.4)
    maxErrs int               // teto, padrão 100 (REQ-6.5)
    errs    int
}
```

Decisões:

- **Dedup** por chave `pos|severidade|msg` (REQ-6.4).
- **Teto** com mensagem-sentinela quando estourado (REQ-6.5).
- **Ordenação** estável por `(linha, coluna)` só na renderização (REQ-6.3); a
  ordem de inserção não importa, o que mescla naturalmente erros de sintaxe e
  semântica (REQ-6.1) e garante determinismo (NFR-3).
- **Render**: `linha:coluna: severidade: mensagem` (REQ-6.6).
- **Códigos de diagnóstico** (futuro): cada regra pode ganhar um código estável
  (`E001`, `W014`) para tooling; o design reserva o campo, a baseline ainda não usa.

### 3.5. Parser (`parser/`) — REQ-2, REQ-3

Recursive descent. O coração do recovery, já validado na fatia ValueObject e
generalizado aqui:

**Cursor e leitura:** `cur()`, `peek()`, `advance()`, `at(kind)`, `accept(kind)`.

**`expect(kind) bool`** (REQ-3.2, REQ-3.3):
- token presente → consome, retorna `true`;
- ausente mas o *próximo* casa → deleção de token único (descarta ruído,
  consome o esperado), retorna `true`;
- ausente sem correspondência → inserção virtual (reporta, não consome), `false`.

**`synchronize(stop set)`** (REQ-3.4): descarta tokens até um membro de `stop` ou
EOF. **Nunca consome** o token de parada nem `}`/EOF — o nível de cima fecha seu
próprio bloco. (Bug corrigido na baseline: versão anterior consumia o token de
parada e podia comer o `}` de fechamento.)

**Sync sets por nível** (`sync_sets.go`, REQ-3.4): cada nível define seu conjunto
de parada **incluindo os conjuntos ancestrais**, para o pânico nunca furar para
fora da estrutura:

```
topLevelStop  = { toda keyword de declaração de topo, EOF }
declMemberStop(Aggregate) = { Handle, Apply, state, access, ...,  '}' } ∪ topLevelStop
stmtStop      = { ensure, match, for, return, emit, ...,  '}' } ∪ ancestrais
listStop      = { ',',  ')',  ']',  '}' }
configLineStop= { IDENT (próxima chave),  '}' }   # mod.ds / interface.ds
```

As keywords de topo são **âncoras de máxima confiança** (REQ-3.7): ao vê-las
durante o pânico, o parser sempre reancora no nível de arquivo. Isso permite
reportar N declarações quebradas independentemente.

**Janela de silêncio** (REQ-3.5): `errorAt()` suprime um diagnóstico se menos de
`silenceWindow` (≥1) tokens foram consumidos desde o último erro emitido. Mata a
maior parte da cascata sem perder erros em regiões distintas (a sincronização
avança o cursor, "abrindo" a janela para o próximo erro real).

**Garantia de progresso** (REQ-3.6, NFR-2): todo loop de parsing captura `before :=
p.pos` e força `advance()` se `p.pos == before` ao fim da iteração. Rede de
segurança redundante com os sync sets.

**Estrutura modular** (NFR-5): `parse_decl.go` tem um `switch` na keyword de topo;
adicionar um construto = adicionar um `case` + uma função `parseX`, reusando
`parseExpr`, `parseBlock` e o recovery. `parse_config.go` e `parse_test.go`
isolam as gramáticas dos arquivos não-`.ds`.

### 3.6. Tabela de Símbolos (`symbols/`) — REQ-4

```go
type Kind int  // KindValueObject, KindEnum, KindAggregate, KindEvent, KindCommand, ...

type Symbol struct {
    Name   string
    Kind   Kind
    Module string
    Decl   ast.Decl   // nó declarante (para spans e detalhes)
    Public bool       // Event vs PublicEvent (REQ-7.4)
}

type SymbolTable struct {
    byModule map[string]map[string]*Symbol   // módulo → nome → símbolo
}
```

Decisão: **escopo por módulo** (REQ-4.1), com um nível global para símbolos
exportados (`PublicEvent` em `contracts/`). Resolução cross-module consulta o
módulo declarante e o conjunto público.

### 3.7. Resolver (`resolver/`) — REQ-4

Duas passagens (REQ-4.1, REQ-4.2):

1. **Coleta:** percorre todas as ASTs, registra cada declaração na `SymbolTable`.
   Declaração duplicada no mesmo escopo → erro (REQ-4.3).
2. **Resolução:** percorre referências (`ref`, `handles`, `on`, tipos em campos e
   parâmetros) e liga cada uma ao símbolo. Referência não resolvida → erro
   (REQ-4.2). Anota o nó com o `*Symbol` para o checker usar.

Subárvores com nós de erro são puladas (REQ-4.5).

### 3.8. Program (`program/`) — REQ-7

```go
type Program struct {
    Files    map[string]*ast.File   // caminho → AST
    Modules  map[string]*Module     // de mod.ds
    Services map[string]*Service    // de topology.ds
    Channels []*Channel             // de topology.ds
    Symbols  *symbols.SymbolTable
}
```

Construído agregando todos os arquivos do diretório (REQ-7.1, REQ-8.4). Expõe o
grafo módulo→service→canal (REQ-7.2) para as regras cross-file, com acesso global
a símbolos (REQ-7.3).

### 3.9. Checker (`sema/`) — REQ-5

Orquestrador (`checker.go`) que roda famílias de regras sobre a AST resolvida.
Cada regra é uma função `func(ctx *Context) ` que percorre os nós relevantes e
emite diagnósticos. Distribuição por arquivo:

| Arquivo | Regras (§23 / REQ-5) |
|---|---|
| `rules_types.go` | primitivo no Write Side (1); `remove/clear` em AppendList (4) |
| `rules_flow.go` | `match` exaustividade/wildcard (5); `Nop` em Handle/UseCase (6); `break/continue` fora de `for` (7) |
| `rules_domain.go` | `Handle` sem `access` (2); `Notification` sem `Adapter` (3); `Policy` cross-module sobre `Event` privado (8) |
| `rules_program.go` | cross-db sem XA / cross-service sem Saga (9); JOIN cross-db (10); módulos sem canal (11); cross-tenant sem opt-in (12); upcast sem default (13) |
| `rules_warnings.go` | todos os ⚠️ (16–23): `orderBy`, Saga await sobre queue, upcast→default, VO→Enum, cache alta cardinalidade, cross-tenant declarado, cobertura de erro, exposição |
| (teste) `rules_test_files.go` | validações de `*.test.ds` (14) e assinatura FFI (15) |

Decisões:

- **Regras locais vs. globais:** regras 1–8 e 14–15 operam por arquivo/declaração;
  9–12, 16–17, 23 exigem o `Program` inteiro (REQ-7) e rodam depois da agregação.
- **Exaustividade de `match`** (regra 5): o checker consulta a `SymbolTable` para
  obter o conjunto fechado (membros do Enum) e compara com os braços do `match`;
  decide se `_` é proibido (cobertura completa de enum) ou obrigatório (presença de
  `when`).
- **Cada regra é independente** (NFR-4, NFR-5): pode ser testada e adicionada sem
  tocar nas outras.

### 3.10. Driver e CLI (`driver/`, `cmd/dsc/`) — REQ-8

```go
// API pública (REQ-8.1)
func CheckSource(src string) (*ast.File, *diag.DiagnosticBag)
func CheckProject(dir string) (*program.Program, *diag.DiagnosticBag)
```

A CLI (`dsc`) recebe arquivo ou diretório, chama a API, imprime
`bag.Render()`, e retorna exit code 0/≠0 conforme `bag.HasErrors()` (REQ-8.2,
REQ-8.3, REQ-6.7).

---

## 4. Fluxos de Decisão Chave

### 4.1. Recovery de declaração de topo (REQ-3.7)

```
parseFile:
  loop até EOF:
    before = pos
    se keyword de topo conhecida → parseX()
    senão:
      erro "esperava declaração de topo"
      synchronize(topLevelStop)        # para na próxima keyword de topo ou EOF
      append ErrorDecl
    se pos == before → advance()        # progresso garantido
```

### 4.2. Exaustividade de `match` (REQ-5.5)

```
checkMatch(node):
  alvo = tipo da expressão do match (via resolver)
  se alvo é Enum:
    esperados = membros do Enum (SymbolTable)
    cobertos  = braços com pattern de valor
    temWhen   = algum braço usa 'when'
    temWild   = existe braço '_'
    se temWhen e não temWild → erro (guards exigem '_')
    se não temWhen e temWild → erro ('_' proibido sobre enum coberto)
    se não temWhen e cobertos ≠ esperados → erro (não-exaustivo, lista faltantes)
```

### 4.3. Transação cross-database (REQ-5.9, REQ-7)

```
checkUseCaseTransaction(uc, program):
  bancos = bancos dos aggregates tocados por uc   # via program.Modules
  se |bancos| > 1:
    se algum par sem suporte XA → erro "exige Saga"
  services = services dos módulos tocados
  se |services| > 1 e uc não é Saga → erro "cross-service exige Saga"
```

---

## 5. Estratégia de Testes (NFR-4)

Espelha a baseline ValueObject, expandida:

- **Lexer:** tabela de (fonte → tokens esperados); casos de erro (string aberta,
  char inválido) verificam diagnóstico + progresso.
- **Parser:** por construto, um teste de happy-path (AST esperada) e testes de
  recovery (chave faltando reancora, lixo no corpo, supressão de cascata,
  progresso). Reusa o padrão já escrito.
- **Resolver:** símbolo inexistente dispara; duplicado dispara; código correto não
  dispara.
- **Semântica:** **para cada regra da §23, um par de testes** — um programa que a
  viola (espera o diagnóstico exato) e um correto (espera silêncio). Este é o
  critério central de Done (REQ-5, NFR-4).
- **Robustez (NFR-2):** fuzzing leve / entradas truncadas e adversárias verificam
  ausência de crash e laço.
- **Determinismo (NFR-3):** mesma entrada, dois runs, diagnósticos idênticos.

---

## 6. Decisões e Trade-offs Registrados

| Decisão | Alternativa | Por quê |
|---|---|---|
| Recursive descent à mão | ANTLR / tree-sitter | Controle total de mensagens e recovery (REQ-3, NFR-1) |
| Go | Rust / TS | Alinhamento com o alvo de transpilação e a baseline |
| Span por nó | Só posição inicial | Diagnósticos semânticos sublinham intervalos |
| Kinds léxicos dedicados p/ duração/taxa/tamanho | Re-parsear o lexema | Parser não re-tokeniza (NFR-6) |
| `DiagnosticBag` compartilhado | Bag por fase + merge | Mescla natural e ordenação única (REQ-6, NFR-3) |
| Janela de silêncio por contagem de tokens | Sincronização perfeita | Simples e eficaz contra cascata (REQ-3.5) |
| Regra semântica = função independente | Visitor monolítico | Extensibilidade e teste isolado (NFR-4, NFR-5) |
| Agregação de programa antes da semântica | Validar arquivo a arquivo | Regras cross-file precisam do todo (REQ-7) |

---

## 7. Riscos e Mitigações

| Risco | Mitigação |
|---|---|
| Recovery varre demais e perde erros | Sync sets hierárquicos + âncoras de topo de alta confiança (REQ-3.4/3.7) |
| Cascata polui o relatório | Janela de silêncio + dedup + teto (REQ-3.5, REQ-6.4/6.5) |
| Regra cross-file sem o programa todo | Estágio de agregação obrigatório antes da semântica (REQ-7) |
| Exaustividade de `match` mal calculada | Fonte única de verdade: membros do Enum na SymbolTable (4.2) |
| Crescimento da gramática quebra fases | Separação dura sintaxe/semântica + um pacote por responsabilidade (NFR-5/6) |
| Entrada adversária trava o parser | Garantia de progresso em todo loop + lexer que sempre consome (NFR-2) |
