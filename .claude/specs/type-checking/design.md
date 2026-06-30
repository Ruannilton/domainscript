# Design — Resolução Completa de Nomes & Tipos

> Documento 2 de 3. Define **como** atender `requirements.md` (REQ-9..13,
> NFR-8..10). Cada decisão referencia os REQ/NFR que satisfaz. Estende o
> `design.md` do front-end (`.claude/specs/design.md`); reusa seus invariantes.

## 1. Visão Arquitetural

### 1.1. Onde o trabalho mora

O pipeline atual é `LEXER → PARSER → RESOLVER → CHECKER`. Este trabalho **engrossa
o RESOLVER e adiciona um sub-estágio de tipos ao CHECKER**, sem novos estágios de
topo e sem tocar lexer/parser.

```
... PARSER ──▶ AST ──▶ RESOLVER ───────────────▶ CHECKER ──────────────▶ AST validada
                       │  REQ-4 (existente)        │  REQ-5 (existente)
                       ├─ + corpos    (REQ-9)      ├─ + tipos     (REQ-11)
                       └─ + config    (REQ-10)     ├─ + membro    (REQ-12)
                                                   └─ + compat    (REQ-13)
```

Decisão de fronteira:

- **Resolução de nomes (REQ-9/REQ-10)** é puramente léxica (não precisa de tipos) →
  vive no `resolver/`, estendendo a passagem de resolução existente.
- **Tipos, membro e compatibilidade (REQ-11/12/13)** precisam do modelo de tipos →
  vivem num novo subsistema `types/` consumido por uma nova regra no `sema/`
  (`rules_typecheck.go`). Mantém o invariante de dependências (`sema → resolver →
  … → ast`): `types/` depende só de `ast`/`symbols`/`token`.

### 1.2. Invariantes preservados (do front-end)

- **NFR-6 (separação de fases):** nada re-tokeniza nem re-parseia. O contrato entre
  fases continua sendo `(AST, DiagnosticBag)`. O modelo de tipos é derivado da AST,
  não um novo parse.
- **REQ-4.5 (pula erros):** subárvores com nós de erro são ignoradas em todas as
  novas passagens.
- **NFR-1/NFR-9 (anti-cascata):** um `errorType` sentinela absorve a propagação —
  toda operação sobre `errorType` produz `errorType` e **não** emite diagnóstico.
- **NFR-5/NFR-8 (extensibilidade/reuso):** a travessia reusa `sema/walk.go`; um
  construto novo muda um lugar só.

---

## 2. Estrutura de Pacotes (delta)

```
resolver/
├── resolver.go              # (existente) coleta + resolução de tipos/refs
├── scope.go                 # NOVO  — modelo de Scope com push/pop (REQ-9)
├── resolve_body.go          # NOVO  — percorre corpos, resolve idents (REQ-9)
├── receivers.go             # NOVO  — receptores contextuais por construto (REQ-9.3)
└── resolve_config.go        # NOVO  — refs de config contra Kind esperado (REQ-10)

types/                       # NOVO PACOTE — modelo de tipos (REQ-11)
├── type.go                  # representação de Type (interface + variantes)
├── model.go                 # Decl → Type; catálogo de membros por tipo
└── infer.go                 # tipo de uma Expr (inferência local)

sema/
└── rules_typecheck.go       # NOVO — acesso a membro (REQ-12) + compat (REQ-13)
```

Dependências: `resolver/scope.go` e `resolve_body.go` dependem de `ast`/`symbols`;
`types/` depende de `ast`/`symbols`/`token`; `sema/rules_typecheck.go` depende de
`types/` e `symbols/`. Nenhuma seta nova aponta "para cima".

---

## 3. Componentes

### 3.1. Modelo de Escopo (`resolver/scope.go`) — REQ-9.2/9.5

```go
type Scope struct {
    parent *Scope
    names  map[string]Binding   // nome → o que ele liga (param, var, binding)
}

func (s *Scope) Child() *Scope            // push: entra num binder
func (s *Scope) Define(name string, b Binding)
func (s *Scope) Lookup(name string) (Binding, bool)  // sobe a cadeia
```

- Um `Scope` raiz por corpo, semeado com os parâmetros do construto e os receptores
  contextuais aplicáveis (§3.2).
- `for`/lambda/braço de `match`/binding de query criam um `Child()`; ao sair, o
  filho é descartado (REQ-9.5). Push/pop é estrutural, não uma pilha mutável global
  — mais simples de tornar determinístico (NFR-9).
- Fallback final: símbolos do módulo via `symbols.SymbolTable.Lookup` (e o nível
  público, como o resolver já faz). Membros de Enum acessíveis por nome entram aqui.

### 3.2. Receptores Contextuais (`resolver/receivers.go`) — REQ-9.3

Tabela construto → nomes implícitos semeados no `Scope` raiz:

| Construto | Receptores semeados |
|---|---|
| `Handle` (Aggregate) | params do Handle, `self`, `state`, `caller` |
| `Apply` (Aggregate) | `state`, `event` |
| `access` (Aggregate) | `self`, `caller` |
| `Valid` (VO) | `value`, `ok` |
| `Operator` (VO) | params do operator, `value` |
| `execute` (UseCase/Policy) | params/command, `caller` |
| `coerce` (Enum) | `value` |
| step de `Saga` | `state`, params do step |

Esta tabela é o **único** ponto a editar quando um construto novo ganha um receptor
(NFR-5). Os receptores são `Binding`s com o tipo já apontado para o §3.3, para que a
checagem de membro (REQ-12) os use direto.

### 3.3. Resolução de Corpos (`resolver/resolve_body.go`) — REQ-9.1/9.4/9.6

Para cada `Decl`, obtém os blocos via `declBlocks(d)` (já existe em `sema/walk.go`;
mover/exportar para reuso — ver §5). Para cada bloco:

1. Cria o `Scope` raiz com os receptores de §3.2.
2. Percorre statements com `forEachStmt`; ao entrar num `for`/lambda/`match`/query,
   abre um `Child()` e define o binder.
3. Em cada `*ast.Ident` **em posição de valor** (não nome de campo de construção,
   não keyword), chama `Scope.Lookup`. Não resolveu em escopo nem no módulo →
   `bag.Errorf(pos, "nome não declarado: %q", id.Name)` (REQ-9.4).
4. Nós de erro fazem a subárvore ser pulada (REQ-9.6).

Cuidado de precisão: distinguir **uso** de **definição**. `t => t.x` define `t`;
`StatementEntry(type: ..., amount: ...)` — `type`/`amount` são nomes de campo, não
idents a resolver. O percurso só resolve `*ast.Ident` que estão em posição de valor;
nomes de campo de `Arg.Name` e chaves de `match`-pattern são tratados à parte.

### 3.4. Refs de Configuração (`resolver/resolve_config.go`) — REQ-10

Catálogo declarativo (config ref → `Kind` esperado):

| Origem | Campo | Kind esperado |
|---|---|---|
| `ModuleDecl` → `Database` block | `manages: [...]` | Aggregate |
| `Route` | `Target` | UseCase \| Query |
| `GrpcRPC` | `Target` | UseCase \| Query |
| `ServiceDef` | `modules: [...]` | Module |
| `ChannelDef` | `From` / `To` | Module |
| `VersionRoute` | `Target` | UseCase |
| `VersionUpcast`/`Downcast` | `Target` | Command \| View |

> Correção vs. o rascunho inicial: `ChannelDef.From/To` resolve a **Module**, não a
> Service. O front-end modela canais como ligações módulo→módulo (`program/graph.go`:
> um service agrupa módulos; os canais ligam módulos que vivem em services
> distintos). Resolver From/To contra services produziria falsos positivos contra a
> própria topologia válida do projeto.

Módulos e services **não vivem na tabela de símbolos**; a resolução de
`modules`/`From`/`To` consulta o conjunto de módulos declarados (das `ModuleDecl`).
As demais refs (`manages`, alvos de rota/rpc/versão) resolvem na tabela de símbolos.

Para cada entrada: extrai o(s) nome(s) (idents dentro de `ListExpr`/`Ident`/campo
textual), faz `Lookup`; ausente → erro (REQ-10.2); `Kind` divergente → erro com
esperado vs. encontrado (REQ-10.3). A tabela acima é o ponto único de extensão.

> Nota: alguns alvos (`From`/`To`, `modules`) já são lidos pelo `program/` para o
> grafo de topologia, mas **sem** reportar inexistência. Esta passagem adiciona o
> diagnóstico; o `program/` continua dono do grafo.

### 3.5. Modelo de Tipos (`types/`) — REQ-11

```go
type Type interface{ typeNode() }

type Primitive struct{ Name string }                 // integer, decimal, string, ...
type VOType    struct{ Name string; Fields []Field } // wrapper: 1 campo base; composto: N
type EnumType  struct{ Name string; Base Type; Members []string }
type ShapeType struct{ Name string; Kind symbols.Kind; Fields []Field } // Aggregate/Event/Command
type Generic   struct{ Ctor string; Args []Type }    // List<T>, AppendList<T>, Map<K,V>
type FuncType  struct{ Params []Type; Result Type }  // Operator/Query/método
type errorType struct{}                              // sentinela anti-cascata (REQ-11.3)
```

- **`model.go`**: `TypeOf(sym *symbols.Symbol) Type` constrói o tipo de uma
  declaração e seu **catálogo de membros** (nome → tipo do membro). Para um
  Aggregate, os membros de `self`/`state` são os campos de `state`.
- **`infer.go`**: `Infer(e ast.Expr, sc *Scope) Type` desce na expressão:
  literal → primitivo; `Ident` → tipo do `Binding`; `CallExpr` de um VO → o VOType;
  `MemberExpr` → tipo do membro (via catálogo); operador → tipo do resultado.
  Qualquer subexpressão `errorType` ⇒ resultado `errorType`, sem diagnóstico
  (REQ-11.3 / NFR-9).

### 3.6. Acesso a Membro e Compatibilidade (`sema/rules_typecheck.go`) — REQ-12/13

- **Membro (REQ-12):** para cada `*ast.MemberExpr`, computa `Infer(X)`; se for
  `errorType`, ignora (REQ-12.4); senão consulta o catálogo de membros do tipo. Não
  achou → `bag.Errorf(memberPos, "membro inexistente: %q em %s", name, typeName)`,
  com sugestão do membro mais próximo por distância de edição (REQ-12.3). `self`/
  `state`/`event` caem aqui naturalmente porque seus `Binding`s já carregam o tipo
  (§3.2).
- **Compatibilidade (REQ-13):** em atribuição, argumento, operador e `return`,
  compara `Infer(esperado)` vs. `Infer(encontrado)` por uma relação de
  atribuibilidade simples (igualdade de tipo + regras explícitas de coerção do
  spec). Incompatível → erro esperado-vs-encontrado. Coordena com a regra existente
  REQ-5.1 (primitivo no Write Side) para não duplicar diagnóstico (REQ-13.3).

### 3.7. Diagnósticos e códigos

As mensagens novas seguem o padrão acionável (esperado vs. encontrado) de REQ-6.8.
Esta etapa é a oportunidade de **começar a preencher o campo `diag.Code`** hoje
reservado (`diag/diagnostic.go`): atribuir códigos estáveis às novas famílias
(ex.: `E100` nome em corpo, `E101` ref de config, `E102` membro inexistente, `E103`
incompatibilidade de tipo). Catálogo de códigos definido junto com as tasks.

---

## 4. Ordem de Execução no Pipeline

1. Coleta de símbolos (existente).
2. Resolução de tipos/refs existente (existente).
3. **Resolução de corpos (REQ-9)** — nova passagem do resolver, após a coleta global
   (precisa de todos os símbolos do módulo/programa).
4. **Resolução de refs de config (REQ-10)** — nova passagem do resolver.
5. Regras §23 existentes (CHECKER).
6. **Tipos + membro + compat (REQ-11/12/13)** — nova regra no CHECKER, depois que os
   nomes já resolveram (um nome não resolvido não deve virar erro de tipo: o erro de
   nome já cobre; o tipo vira `errorType`).

A ordem garante anti-cascata: erro de nome (passo 3/4) ⇒ `errorType` no passo 6 ⇒
sem segundo diagnóstico para a mesma causa.

---

## 5. Notas de Refatoração

- `declBlocks`, `forEachStmt`, `forEachExpr`, `forEachExprInBlock` e `stateField`
  hoje são **não-exportados** em `sema/walk.go`. Como `resolver/` precisa deles e
  não pode importar `sema/` (violaria a direção de dependências), mover a travessia
  genérica para um pacote neutro (ex.: `ast/walk` ou `astutil/`) do qual tanto
  `resolver/` quanto `sema/` dependem. Decisão a fixar na primeira task.
- Nenhuma mudança em lexer/parser/AST nodes é esperada; se a checagem revelar falta
  de span em algum nó (ex.: posição do membro em `MemberExpr`), corrigir
  pontualmente no `ast/` é aceitável.
