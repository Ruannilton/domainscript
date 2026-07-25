# CLAUDE.md

Guidance for Claude Code working in this repository.

**Status and history do not live here.** `.claude/state.md` is the resume
pointer — the next spec-task and the next issue, nothing more. Per-spec status
lives in each spec's own task tracking; open defects in
`.claude/issues/open-issues.md`. Read the pointer before starting work.

## What is being built

A **two-stage transpiler for DomainScript**. The **front-end** turns source
text into a validation verdict — a validated AST plus a diagnostics report;
it answers *"is this correct?"*. The **back-end** consumes that validated
program and emits a complete, idiomatic, compilable Go project; it answers
*"here is the Go code that does this."*

A shared, accumulating `DiagnosticBag` runs across the front-end stages; the
back-end runs only when the bag has no errors.

```
                    ┌──────────────── FRONT-END ────────────────┐
source ─▶ LEXER ─▶ tokens ─▶ PARSER ─▶ AST ─▶ RESOLVER ─▶ CHECKER ─▶ validated program
          REQ-1              REQ-2/3           REQ-4/9/10   REQ-5/12/13      │
                                                                HasErrors()? no ▼
                                                          codegen.Generate ─▶ Go project
                                                                    REQ-14..32
```

- **RESOLVER**, three passes: type/ref resolution (REQ-4) → name resolution in
  executable bodies (REQ-9) → config-ref resolution (REQ-10).
- **CHECKER**: the §23 rules (REQ-5) plus, over a shared `types.Model`,
  member-access (REQ-12) and type-compatibility (REQ-13) checks. The ordering
  is deliberate — an unresolved name becomes `types.ErrorType` downstream, so
  it never spawns a second type diagnostic (anti-cascade, NFR-9).
- **Program aggregation** (REQ-7) runs between PARSER and RESOLVER on
  multi-file projects: parse every file, merge the ASTs into one program
  model, then resolve globally and run the cross-file rules.
- **Back-end** output is a real Go project: `go.mod`, a vendored `runtime/`
  package, one Go package per domain module, `contracts/` for shared
  `PublicEvent`s, and one `cmd/<service>/` per service in the topology (a
  single default group when there is no topology).

## The language spec

`.claude/steerings/domainscript-spec-v7/` — one file per section, indexed by
its `README.md`. **Load only the sections your task needs**, not the whole
spec.

⚠️ **`§N` in this repo's code and specs follows v6 numbering and does not
reliably match the v7 filenames.** v7 inserted sections, so from roughly §20
onward the numbers shift (+2 in every case checked). Resolve a citation by
*section title*, never by number alone. Verified pairs:

| Cited in code/specs | File in `domainscript-spec-v7/` |
|---|---|
| §23 — regras de compilação | `25-compilation-rules.md` |
| §22 — testing `*.test.ds` (e.g. §22.3, §22.7) | `24-testing.md` |
| §20 — Smart Partial Loading | `22-smart-partial-loading.md` |

## Repository layout for the spec-driven flow

```
.claude/state.md          resume pointer — next spec-task + next issue, only
.claude/specs/<spec>/     requirements.md, design.md + task tracking (see below)
.claude/issues/           one file per open issue; open-issues.md indexes them
.claude/steerings/        ambient reference docs (the language spec)
.claude/agents/           subagent definitions — the operative detail on who does what
.claude/hooks/            guard scripts referenced by agent frontmatter
.claude/skills/spec-creator/    scaffolds a new spec from templates
.claude/skills/issue-generator/ registers a new issue from a template
```

**Two task-tracking models.** Every spec on disk today uses the legacy model:
a single `tasks.md` per spec. Specs scaffolded from now on by `spec-creator`
use one `tasks/<task-code>.md` per task plus that spec's own `state.md`
(pending/blocked index). Mark a task done in whichever model its spec uses —
check the box in `tasks.md`, or set `status: completed` in the task file and
drop it from the spec's `state.md`.

**To see which specs exist and where each stands**, list `.claude/specs/*/`
and read their task tracking — that is the authoritative status, not the root
pointer.

**Specs and their REQ ranges** (scope only):

| Spec | REQs | Scope |
|---|---|---|
| `transpilador/` | REQ-1..8, NFR-1..7 | Front-end: lexer → parser → resolver → checker |
| `type-checking/` | REQ-9..13, NFR-8..10 | Name & type resolution |
| `codegen/` | REQ-14..32, NFR-11..17 | Back-end / Go code generation |
| `read-side/` | REQ-33..40 | Query clauses & Smart Partial Loading |
| `infra-providers/` | REQ-41..48, NFR-21..24 | Postgres, Outbox, RabbitMQ, Redis, S3 |
| `correcoes-issues-9-10-11/` | REQ-49..51 | Maintenance cycle |
| `correcoes-issues-6-7-8/` | REQ-52..54 | Maintenance cycle |

Specs are written in **Portuguese**. `.claude/specs/codegen/gaps.md` records
what the language spec promises and the transpiler does not yet deliver.

Work is maintenance and extension, not greenfield. A task references the REQ
it satisfies (`(REQ-n)`) and the design section (`(§design x)`). Do not invent
architecture that contradicts `design.md` — if a change is needed, update the
spec.

## Execution rules

- **One task at a time.** Never start a second task before the current one is
  committed. Pick it up from `.claude/state.md`.
- **Keep `.claude/state.md` lean.** Exactly two pointers — next spec-task,
  next issue — and nothing else: no per-spec table, no history, no running
  log. That detail already lives in each spec's task tracking and in
  `.claude/issues/open-issues.md`. Update by overwriting a line in place,
  never appending. The spec-task pointer advances mechanically as work
  completes; the issue pointer is a manual priority call — repoint it when
  priorities change, e.g. after filing a new issue or starting a spec that
  addresses one.
- **Errors found mid-task.** In scope of the current task → fix it here. Out
  of scope (another spec/task, pre-existing code) → register it with the
  `issue-generator` skill and keep going. If it *blocks* the current task,
  stop and report — do not work around it.
- **Finishing a task.** Update the spec's own task tracking, then overwrite
  the spec-task pointer in `.claude/state.md` with whatever comes next (the
  next pending task in this spec, or another spec's if this one has none
  left), then commit. At spec closure the pointer moves on, or is cleared if
  nothing does.
- **One branch and one PR per spec.** `claude/impl-<spec-slug>`, cut from
  `main`, carrying one atomic commit per completed task. The first completed
  task creates the branch and opens the PR; every later task adds a commit to
  it. CI runs the full suite on each push, so each task's diff is still
  validated on its own commit. When every task is done, comment `SPEC
  FINALIZADA` on the PR and it closes with the spec.
- **Never run the full suite locally.** `go test ./...` and `go vet ./...` are
  CI's job, on the PR — including at spec closure. When running tests at all,
  run only those that validate the current task (e.g.
  `go test ./parser/ -run TestX`).
- **Refine tasks at spec-creation time.** Break them down as far as practical
  up front — small, independently verifiable, vertically sliced — so execution
  never re-plans mid-spec.
- **Who does what** (definitions in `.claude/agents/` are the operative
  detail): `spec-writer` authors a spec (never touches code, never runs
  tests); `task-implementer` executes exactly one task and **never runs
  tests** — the spec's PR is its only test feedback, and a blocker becomes a
  registered issue plus a `blocked` task, never a workaround;
  `issue-registrar` investigates a suspected defect and files it.
- **Only `issue-registrar` runs tests**, because its job is proving a defect
  is real before it reaches `.claude/issues/` — and it files nothing when the
  problem does not reproduce. It never fixes what it finds: repro work happens
  in a copy outside the repository, and the versioned tree stays untouched.

## Architecture invariants

Load-bearing decisions — violating them breaks the design's core promises.

- **Hard syntax/semantics split (NFR-6).** The parser knows *zero* §23
  semantic rules; it accepts everything grammatically well-formed, including
  semantically impossible programs (primitive in Write Side, non-exhaustive
  `match`, `Nop` in Handle). The semantic phases never re-tokenize or
  re-parse. The *only* contract between phases is `(AST, DiagnosticBag)`.
- **The parser never returns `nil`.** On syntax error it emits typed error
  nodes (`ErrorDecl`/`ErrorStmt`/`ErrorExpr`) implementing the normal
  interfaces. Later phases skip subtrees containing an error node, so a syntax
  error never becomes a false semantic error (REQ-2.7, REQ-4.5).
- **Hand-written recursive-descent parser** (no generator) — the point is
  total control over error messages and recovery (REQ-3, NFR-1).
- **Recovery mechanics** (`design.md` §3.5): `expect` does single-token
  deletion + virtual insertion; `synchronize` *never* consumes the stop token
  or a closing `}`/EOF (the enclosing level closes its own block);
  hierarchical sync sets per level include ancestor sets; top-level keywords
  are high-confidence re-anchor points; a silence window suppresses cascade
  diagnostics; every parse loop guarantees cursor progress (NFR-2).
- **Dependencies point "downward"**: `driver → sema → resolver → parser →
  lexer → ast/token/diag`. One package per responsibility.
- **Determinism (NFR-3).** Same input → identical diagnostics in identical
  order. Ordering by `(line, col)` happens only at render time; insertion
  order is irrelevant, which lets syntax and semantic diagnostics merge.
- **Cross-file rules need the whole program.** REQ-5.9–12, 16–17, 23 cannot
  run file-by-file; they run after program aggregation (REQ-7).

### Back-end

- **Core vs. opt-in dependencies (NFR-12).** The transactional core
  (in-memory event store, dispatcher, unit of work, `net/http` edge) depends
  on the Go stdlib and the vendored `runtime/` only. A real DB driver, gRPC or
  OpenTelemetry enter `go.mod` **only** when the program declares them — a
  `Database` whose provider `codegen/sql_wiring.go` recognizes as real
  (`"sqlite"` → `modernc.org/sqlite`, `"postgres"` → `github.com/jackc/pgx/v5`),
  an `Interface GRPC`, a `Telemetry` block — always behind an interface. Any
  other provider label (e.g. `"pg"`, an inert placeholder in several fixtures)
  stays decorative and pulls nothing.
- **Golden test + smoke compile, paired (NFR-17).** Every emitter has a golden
  test (output vs. a versioned reference); on top of that the bundled examples
  (`docs/examples/wallet`, `docs/examples/shop`) are generated for real via
  `GenerateProject` and built/vetted/tested over the bytes actually written to
  disk — a golden test alone does not prove the output compiles.
- **Determinism (NFR-13).** Regenerating the same program is byte-identical:
  stable ordering of declarations, imports, map members and files.
  Regenerating into a populated output directory is idempotent — unchanged
  files are not rewritten, files orphaned by a removed declaration are deleted.
- **The generator never re-lexes/re-parses/re-validates.** Its only inputs are
  `program.Program`, `symbols.SymbolTable` and a `types.Model`; it refuses to
  run when the program's `DiagnosticBag` has errors (REQ-14.1).

## Package layout

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
codegen/grpcrt/ gRPC edge helpers, opt-in — only when `Interface GRPC` (REQ-29)
codegen/otelrt/ OpenTelemetry adapter, opt-in — only when `Telemetry` (REQ-30)
codegen/sqlrt/  `database/sql` adapter, opt-in — only for a real DB provider (REQ-26.2)
```

Public API: `driver.CheckSource(src) (*ast.File, *diag.DiagnosticBag)`,
`driver.CheckProject(dir) (*program.Program, *diag.DiagnosticBag)`,
`driver.GenerateProject(dir, out, codegen.Options) (*diag.DiagnosticBag, error)`.

## Commands

Go module named `domainscript`. A `Makefile` wraps these as
`build`/`test`/`lint`/`fmt` — prefer `make test`, `make lint`.

```sh
go build ./...                        # build all packages
go test ./parser/ -run TestRecovery   # one package / one test by regex
gofmt -l .                            # list unformatted files
dsc gen <dir> -o <out>                # validate <dir> and generate a Go project

go test ./... ; go vet ./...          # full suite — CI's job; locally only issue-registrar
```

## Conventions

- **Slice vertically.** Implement one construct end-to-end (lexer → parser →
  semantics → test) before widening to the next. Follow the task order; it
  respects dependencies.
- **Every §23 rule needs a positive *and* a negative test** — a program that
  violates it (expecting the exact diagnostic) and a correct one (expecting
  silence). This pairing is the central Definition of Done (NFR-4).
- **Green tree before commit**: `go build ./...` passing, one atomic commit
  per completed task.
- **Conventional Commits**, Portuguese imperative, e.g. `feat(parser):
  declaração Aggregate`. Types: `feat`/`test`/`refactor`/`chore`/`docs`/`fix`.
  Scopes: `lexer`/`parser`/`ast`/`diag`/`sema`/`resolver`/`symbols`/`types`/
  `program`/`cli`/`repo`.
