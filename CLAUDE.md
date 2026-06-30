# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state

**The front-end is implemented and green** (`go build ./...` / `go test ./...`),
committed in a Go module named `domainscript`. The original plan in
`.claude/specs/{requirements,design,tasks}.md` (Fases 0–11, REQ-1..8) is done,
and the follow-up plan in `.claude/specs/type-checking/` — full name & type
resolution (REQ-9..13) — is also complete: bodies and config refs are resolved,
a type model backs member-access and type-compatibility checks, and the new
name/type diagnostics carry stable codes (`E100`..`E103`). Both spec sets are the
source of truth and are written in Portuguese:

- `.claude/specs/requirements.md` / `design.md` / `tasks.md` — the front-end
  (REQ-1..8, NFR-1..7).
- `.claude/specs/type-checking/{requirements,design,tasks}.md` — name & type
  resolution (REQ-9..13, NFR-8..10).

Work now is maintenance and extension, not greenfield. Still follow the spec
flow: a task references the REQ it satisfies (`(REQ-n)`) and the design section
(`(§design x)`). Do not invent architecture that contradicts `design.md`; if a
change is needed, update the spec. Go code generation (the back-end) remains
out of scope.

## What is being built

The **front-end of a transpiler for DomainScript** (spec v6.0): the stage from
source text to a validation verdict. It takes DomainScript files and produces
(a) a validated AST and (b) a diagnostics report. Go code generation is a later,
out-of-scope stage.

Pipeline (a shared, accumulating `DiagnosticBag` runs across all stages):

```
source ─▶ LEXER ─▶ tokens ─▶ PARSER ─▶ AST ─▶ RESOLVER ─▶ CHECKER ─▶ validated AST
          REQ-1              REQ-2/3           REQ-4/9/10   REQ-5/12/13
```

The RESOLVER now does three passes: type/ref resolution (REQ-4), then name
resolution in executable bodies (REQ-9), then config-ref resolution (REQ-10). The
CHECKER runs the §23 rules (REQ-5) plus, over a shared `types.Model`, member-access
(REQ-12) and type-compatibility (REQ-13) checks. The ordering is deliberate: an
unresolved name becomes `types.ErrorType` downstream, so it never spawns a second
type diagnostic (anti-cascade, NFR-9).

For multi-file projects, a **program aggregation** stage (REQ-7) runs between
PARSER and RESOLVER: every file is parsed, then ASTs are merged into one program
model before global resolution and cross-file rules.

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

## Package layout (per design.md §2 — all implemented)

```
cmd/dsc/        CLI (REQ-8)
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
driver/         pipeline orchestration + public API (REQ-8)
```

Public API surface: `driver.CheckSource(src) (*ast.File, *diag.DiagnosticBag)`
and `driver.CheckProject(dir) (*program.Program, *diag.DiagnosticBag)`.

## Commands

The module is named `domainscript` (Go):

```sh
go build ./...                         # build all packages
go test ./...                          # run the whole suite
go test ./parser/ -run TestRecovery    # run one package / one test by regex
go vet ./...                           # static checks
gofmt -l .                             # list unformatted files
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
determinism, full §23 coverage (Fase 11).
