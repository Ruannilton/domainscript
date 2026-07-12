# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state

**The front-end and the back-end are implemented and green**
(`go build ./...` / `go test ./...`), committed in a Go module named
`domainscript`. The original plan in `.claude/specs/transpilador/` (Fases
0–11, REQ-1..8) is done, the follow-up plan in `.claude/specs/type-checking/`
— full name & type resolution (REQ-9..13) — is done, and the code-generation
plan in `.claude/specs/codegen/` — the back-end, Marcos E/F/G/H (REQ-14..32)
— is **also complete**: a validated program now generates an idiomatic,
compilable Go project (`driver.GenerateProject` / `dsc gen`). A fourth plan,
`.claude/specs/read-side/` — query clauses & Smart Partial Loading
(REQ-33..40) — is **in progress** (Marco I). Four spec sets are the source of
truth and are written in Portuguese:

- `.claude/specs/transpilador/{requirements,design,tasks}.md` — the front-end
  (REQ-1..8, NFR-1..7).
- `.claude/specs/type-checking/{requirements,design,tasks}.md` — name & type
  resolution (REQ-9..13, NFR-8..10).
- `.claude/specs/codegen/{requirements,design,tasks}.md` — the back-end / Go
  code generation (REQ-14..32, NFR-11..17).
- `.claude/specs/read-side/{requirements,design,tasks}.md` — query clauses &
  Smart Partial Loading (REQ-33..40).

Work now is maintenance and extension, not greenfield. Still follow the spec
flow: a task references the REQ it satisfies (`(REQ-n)`) and the design section
(`(§design x)`). Do not invent architecture that contradicts `design.md`; if a
change is needed, update the spec.

## Spec-driven development structure

```
.claude/                       root of the spec-driven flow
.claude/specs/<nome-da-spec>/  requirements.md, design.md, tasks.md per spec
.claude/steerings/             reference docs useful as ambient context
                                (e.g. domainscript-spec-v6.md, the language spec)
.claude/issues.md              errors found during execution that are out of
                                scope for the spec/task being worked on
.claude/state.md               tracks the status of every spec::task, so
                                execution can resume after an interruption
```

### Execution rules

- **One task at a time.** Never start a second task before the current one is
  committed. Pick the task up from `.claude/state.md`.
- **Errors found mid-task:**
  - If the error belongs to the spec/task currently being developed, fix it
    as part of the current task.
  - If the error comes from a different scope (another spec/task, pre-existing
    code), log it in `.claude/issues.md` (`ISSUE-<n>` header with `SPEC`,
    `TASK`, `DESCRIPTION` fields) and keep going — unless the error blocks
    the current task from being completed, in which case stop and report it
    instead of working around it.
- **Test scope per task.** At the end of a task, run only the tests needed to
  validate that task (e.g. `go test ./parser/ -run TestX`), not the whole
  suite. Once green, update `.claude/state.md` and the current spec's
  `tasks.md` (mark the task done), then commit.
- **Full suite only at spec closure.** `go test ./...` (and `go vet ./...`)
  runs only once, at the end of the *entire* spec — not after every task.
- **Refine `tasks.md` at spec-creation time.** When writing a new spec's
  `tasks.md`, break tasks down as far as practical up front — small,
  independently verifiable, vertically sliced — so execution never needs to
  re-plan mid-spec.

## What is being built

A **two-stage transpiler for DomainScript** (spec v6.0). The **front-end**
goes from source text to a validation verdict: it takes DomainScript files and
produces (a) a validated AST and (b) a diagnostics report. The **back-end**
consumes that validated program and produces (c) a complete, idiomatic,
compilable Go project — the front-end answers "is this correct?"; the back-end
answers "here is the Go code that does this."

Pipeline (a shared, accumulating `DiagnosticBag` runs across the front-end
stages; the back-end only runs when the bag has no errors):

```
                    ┌──────────────── FRONT-END ────────────────┐
source ─▶ LEXER ─▶ tokens ─▶ PARSER ─▶ AST ─▶ RESOLVER ─▶ CHECKER ─▶ validated program
          REQ-1              REQ-2/3           REQ-4/9/10   REQ-5/12/13      │
                                                                              ▼ HasErrors()? no
                                                                     ┌── BACK-END ──┐
                                                                     │  codegen.Generate│──▶ Go project
                                                                     └──────────────┘   (go build ✓)
                                                                     REQ-14..32
```

The RESOLVER does three passes: type/ref resolution (REQ-4), then name
resolution in executable bodies (REQ-9), then config-ref resolution (REQ-10). The
CHECKER runs the §23 rules (REQ-5) plus, over a shared `types.Model`, member-access
(REQ-12) and type-compatibility (REQ-13) checks. The ordering is deliberate: an
unresolved name becomes `types.ErrorType` downstream, so it never spawns a second
type diagnostic (anti-cascade, NFR-9).

For multi-file projects, a **program aggregation** stage (REQ-7) runs between
PARSER and RESOLVER: every file is parsed, then ASTs are merged into one program
model before global resolution and cross-file rules.

The **back-end** (`codegen` package, orchestrated by `driver.GenerateProject`)
never re-lexes/re-parses/re-validates: its only inputs are `program.Program`,
`symbols.SymbolTable` and a `types.Model` built over the table. Because the AST
doesn't carry resolved symbols per node, the generator re-queries the symbol
table and rebuilds a local type environment (`lower.TypeEnv`, §design codegen
3.6a) to lower expressions/statements to Go. Output is organized as a real Go
project: `go.mod`, a vendored `runtime/` package, one Go package per domain
module, `contracts/` for shared `PublicEvent`s, and one `cmd/<service>/` per
service in the topology (or a single default group when there's no topology,
as in the single-module Wallet example).

## Architecture invariants

These are the load-bearing decisions — violating them breaks the design's core promises.

- **Hard syntax/semantics split (NFR-6).** The parser knows *zero* §23 semantic
  rules; it accepts everything grammatically well-formed, including semantically
  impossible programs (primitive in Write Side, non-exhaustive `match`, `Nop` in
  Handle). The semantic phases never re-tokenize or re-parse. The *only* contract
  between phases is `(AST, DiagnosticBag)`.
- **The parser never returns `nil`.** On syntax error it emits typed error nodes
  (`ErrorDecl`/`ErrorStmt`/`ErrorExpr`) that implement the normal interfaces.
  Later phases skip subtrees containing an error node so a syntax error never
  becomes a false semantic error (REQ-2.7, REQ-4.5).
- **Hand-written recursive-descent parser** (no generator) — the whole point is
  total control over error messages and recovery (REQ-3, NFR-1).
- **Recovery mechanics** (see `design.md` §3.5): `expect` does single-token
  deletion + virtual insertion; `synchronize` *never* consumes the stop token or
  a closing `}`/EOF (the enclosing level closes its own block); hierarchical sync
  sets per level include ancestor sets; top-level keywords are high-confidence
  re-anchor points; a silence window suppresses cascade diagnostics; every parse
  loop guarantees cursor progress (no infinite loops, NFR-2).
- **Dependencies point "downward"**: `driver → sema → resolver → parser → lexer
  → ast/token/diag`. One package per responsibility.
- **Determinism (NFR-3):** same input → identical diagnostics in identical order.
  Ordering by `(line, col)` happens only at render time; insertion order is
  irrelevant, which lets syntax and semantic diagnostics merge naturally.
- **Cross-file rules need the whole program.** Rules REQ-5.9–12, 16–17, 23 cannot
  run file-by-file; they run after program aggregation (REQ-7).

### Back-end architecture invariants

- **Core vs. opt-in dependencies (NFR-12).** The transactional core (in-memory
  event store, dispatcher, unit of work, `net/http` HTTP edge) depends on the
  Go stdlib and the vendored `runtime/` only — `go build`/`go run` with no
  external module. A real DB driver, gRPC, or OpenTelemetry are added to
  `go.mod` **only** when the program actually declares them (a `Database` with
  a provider `codegen/sql_wiring.go` recognizes as real — currently just
  `"sqlite"`, so a decorative `provider: "postgres"` label pulls nothing; an
  `Interface GRPC`; a `Telemetry` block) — always isolated behind an interface.
- **Golden test + smoke compile, paired (NFR-17).** Every emitter has a golden
  test (generated output vs. a versioned reference); on top of that, the two
  bundled examples (`docs/examples/wallet`, `docs/examples/shop`) are generated
  for real via `GenerateProject` and `go build`/`go vet`/`go test` run over the
  actual bytes written to disk — a golden test alone doesn't prove the output
  compiles.
- **Determinism (NFR-13).** Regenerating the same program produces byte-identical
  output: stable ordering of declarations, imports, map members, and files.
  Regenerating into an already-populated output directory is idempotent (unchanged
  files aren't rewritten; files orphaned by a removed declaration are deleted).
- **The generator never re-lexes/re-parses/re-validates.** Its only inputs are
  `program.Program`, `symbols.SymbolTable`, and a `types.Model`; it refuses to
  run at all when the program's `DiagnosticBag` has errors (REQ-14.1).

## Package layout (per design.md §2 — all implemented)

```
cmd/dsc/        CLI (REQ-8, REQ-32: "check" and "gen" subcommands)
token/          TokenKind, Token, Pos (1-based), keywords
diag/           Diagnostic, Severity, DiagnosticBag (dedup, cap=100, render); codes E1xx
lexer/          single-pass over []rune (REQ-1)
ast/            Node/Decl/Stmt/Expr interfaces, Span, error nodes
astutil/        generic AST traversal shared by resolver & sema (NFR-8)
parser/         cursor, expect, synchronize, sync_sets, parse_{decl,stmt,expr,config,test}
symbols/        SymbolTable, per-module scope + public level
resolver/       symbol collection + name resolution (REQ-4); bodies (REQ-9) + config refs (REQ-10)
types/          Type model, TypeOf/Members catalog, expr inference, Assignable (REQ-11)
sema/           checker + rules_{types,flow,domain,program,warnings} (REQ-5);
                rules_typecheck (member, REQ-12) + rules_compat (compat, REQ-13)
program/        aggregates files into a unified model (REQ-7)
driver/         pipeline orchestration + public API (REQ-8); GenerateProject (REQ-32)
codegen/        back-end orchestrator: Generate(prog, model, opts) → []File (REQ-14)
codegen/emit/   Go emitter: buffer, managed imports, gofmt via go/format (REQ-15)
codegen/lower/  lowering of Expr/Stmt/Block → Go, incl. TypeEnv (REQ-22, §design 3.6a)
codegen/rtsrc/  vendored runtime source (event store, dispatcher, UoW, …), embedded (REQ-16)
codegen/grpcrt/ gRPC edge helpers, opt-in — only referenced when `Interface GRPC` (REQ-29)
codegen/otelrt/ OpenTelemetry adapter, opt-in — only referenced when `Telemetry` (REQ-30)
codegen/sqlrt/  `database/sql` adapter, opt-in — only referenced for a real DB provider (REQ-26.2)
```

Public API surface: `driver.CheckSource(src) (*ast.File, *diag.DiagnosticBag)`,
`driver.CheckProject(dir) (*program.Program, *diag.DiagnosticBag)`, and
`driver.GenerateProject(dir, out, codegen.Options) (*diag.DiagnosticBag, error)`.

## Commands

The module is named `domainscript` (Go):

```sh
go build ./...                         # build all packages
go test ./...                          # run the whole suite
go test ./parser/ -run TestRecovery    # run one package / one test by regex
go vet ./...                           # static checks
gofmt -l .                             # list unformatted files
dsc gen <dir> -o <out>                  # validate <dir> and generate a Go project into <out>
```

A `Makefile` wraps these with `build`/`test`/`lint`/`fmt` targets — prefer
`make test`, `make lint`, etc.

## Working conventions (from tasks.md)

- **Slice vertically.** Implement one construct end-to-end (lexer → parser →
  semantics → test) before widening to the next. Follow the task order; it
  respects dependencies.
- **Every §23 rule needs a positive *and* a negative test** — one program that
  violates it (expects the exact diagnostic) and one correct program (expects
  silence). This pairing is the central Definition of Done (NFR-4).
- **Green tree before commit.** Only commit with `go build ./...` and
  `go test ./...` passing. One atomic commit per completed task.
- **Conventional Commits**, in Portuguese imperative, e.g.
  `feat(parser): declaração Aggregate`. Types: `feat`/`test`/`refactor`/`chore`/
  `docs`/`fix`. Scopes: `lexer`/`parser`/`ast`/`diag`/`sema`/`resolver`/
  `symbols`/`types`/`program`/`cli`/`repo`.

## Delivery milestones

A: validates ValueObject & Enum (Fases 0–3, 4A, 4B.1–2, partial 6, single-file
API). B: validates a full domain module incl. per-file ❌ rules (Fases 4–6, 8).
C: validates a multi-module project — the cross-file architectural rules that are
DomainScript's differentiator (Fases 7, 9, 10). D: production-ready — robustness,
determinism, full §23 coverage (Fase 11) — front-end closes here.

Back-end (`.claude/specs/codegen/tasks.md`): E "gera e roda o núcleo transacional"
(VO/Enum/Error/Event/Aggregate/Command/UseCase/Query + lowering + in-memory
runtime + basic HTTP + CLI `gen`). F "reações e coordenação" (Policy/Worker/Saga/
dispatcher/outbox/Notifications/Adapters/Foreign). G "infraestrutura real"
(`database/sql`, FileStorage, idempotency, cache, rate limit, multi-tenancy,
advanced HTTP). H "exposição e observabilidade avançadas + testes" (gRPC, OTel,
`Metric`, `*.test.ds` → Go tests, and closure — determinism/idempotency audit,
docs) — back-end closes here.

Read side (`.claude/specs/read-side/tasks.md`): I "Read Side de verdade"
(query clauses — `orderBy`/`skip`/`take`/`in`/`join`, Smart Partial Loading
`distinct`/`sum`/`focus`, and the SQL/sqlite descent) — **in progress**, see
`.claude/state.md` for the exact next task.
