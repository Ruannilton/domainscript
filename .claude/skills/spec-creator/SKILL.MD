---
name: spec-creator
description: Use this when creating a new spec under .claude/specs/ for a new development cycle (Marco) in this repo — scaffolds requirements.md, design.md, one file per task under tasks/, and a state.md tracking pending/blocked tasks, from the templates in assets/.
---

# Spec Creator

Scaffolds a new spec directory under `.claude/specs/<spec-slug>/`, following
the spec-driven flow described in root `CLAUDE.md`. `requirements.md` and
`design.md` are created as a single file each, same as every spec before
this one. **Tasks are not** — each task gets its own file under `tasks/`,
and the spec gets one additional file, `state.md`, that tracks which tasks
are pending and which are blocked (and why). This model applies only to
specs created from now on; existing specs (`transpilador`, `codegen`,
`type-checking`, `read-side`, `infra-providers`,
`correcoes-issues-9-10-11`, `correcoes-issues-6-7-8`) keep their
consolidated `tasks.md` untouched — do not migrate them as part of this
skill.

## Resources

- `assets/spec-requirements-template.md` — `requirements.md` skeleton (REQ-N
  / NFR-N in EARS format, scope, traceability, Definition of Done).
- `assets/spec-design-template.md` — `design.md` skeleton (architecture,
  package layout delta, contracts, decision log, mermaid diagrams).
- `assets/spec-task-template.md` — one task's skeleton: YAML frontmatter
  (`id`, `title`, `status`, `depends_on`, `references`, `target_files`) plus
  the body (context, implementation steps, tests, commit message).
- `assets/spec-state-template.md` — the spec's `state.md` skeleton: two
  lists, `PENDING TASKS` and `BLOCKED TASKS` (with a reason for each blocked
  entry).

## Resulting layout

```
.claude/specs/<spec-slug>/
├── requirements.md
├── design.md
├── state.md              # aggregate index: pending vs. blocked task codes
└── tasks/
    ├── <CODE-1>.md        # e.g. M1.1.md — one file per task
    ├── <CODE-2>.md
    └── ...
```

A task's `depends_on`/`references` frontmatter is what used to be the
"mapa de dependências" diagram and the "rastreabilidade REQ → tasks" table
at the bottom of the old `tasks.md` — don't recreate those as separate
documents; they're now distributed per-task. Phase/fase grouping (what used
to be `## Fase L1`, `## Fase L2` headers) is expressed through the task code
itself (see below) plus prose in `requirements.md` §1.3/§5 and `design.md`.

## Procedure

1. **Gather the spec's scope.** Pull what you can from the conversation; ask
   the user for anything missing:
   - **Name** — short, human-readable name for the spec/Marco (e.g.
     "Providers reais de infraestrutura").
   - **Goal** — one or two sentences: what this cycle delivers and why.
   - **Builds on** — which prior spec(s) this extends, if any (most specs
     do — check `CLAUDE.md`'s "Current state" section for the chain).
   - **Marco letter** — read `.claude/state.md`'s "Resumo por spec" table
     and the existing `.claude/specs/*/tasks.md` files to find the last
     Marco letter used (K, L, ...); the new spec gets the next one. Confirm
     with the user if it's ambiguous.

2. **Derive `<spec-slug>`** — kebab-case, same convention as existing spec
   directories (e.g. `correcoes-issues-6-7-8`, `infra-providers`).

3. **Check for collisions.** `.claude/specs/<spec-slug>/` must not already
   exist. If it does, stop and tell the user instead of overwriting.

4. **Number REQs/NFRs.** Scan every existing `requirements.md` (`grep -h
   'REQ-\|NFR-' .claude/specs/*/requirements.md`) for the highest `REQ-N`/
   `NFR-N` in use; this spec's requirements continue the sequence — never
   restart at 1 and never reuse a number, mirroring the `ISSUE-<n>`
   convention used for issues.

5. **Write `requirements.md`** from `assets/spec-requirements-template.md`,
   filled in with the gathered scope, REQ/NFR numbers from step 4, and a
   `## Entrega incremental (marcos)` section naming this spec's Marco
   letter.

6. **Write `design.md`** from `assets/spec-design-template.md`, referencing
   the REQs from step 5. Keep `## 7. Decisões e Trade-offs Registrados` and
   `## 8. Riscos e Mitigações` — they're where design-level rationale that
   would otherwise clutter individual task files belongs.

7. **Break the work into tasks.** Follow `CLAUDE.md`'s working conventions:
   slice vertically, small, independently verifiable, in dependency order —
   refine as far as practical now so execution doesn't need to re-plan
   mid-spec. For each task:
   - **Derive its code**: `<MarcoLetter><phase-number>.<index>` (e.g.
     `M1.1`, `M1.2`, `M2.1`), matching the scheme already used by
     `correcoes-issues-6-7-8` (`L1.1`, `L2.2a`, ...) — the phase number
     groups related tasks (what used to be a `## Fase` header); a trailing
     letter (`L2.2a`) is reserved for chains discovered *during* execution
     when a task's premise turns out wrong, not used at creation time.
   - **Create `tasks/<CODE>.md`** from `assets/spec-task-template.md`:
     `id: TASK-<CODE>`, `status: pending`, `depends_on` listing the
     `TASK-<CODE>` of any prerequisite task in this spec,
     `references.requirements`/`references.design` pointing at the REQ(s)
     and design section(s) this task satisfies, `target_files` listing the
     files it's expected to touch, implementation steps, the paired
     positive/negative tests it must add (NFR-4 in this repo — every rule
     needs a test that violates it and one that doesn't), and a commit
     message following the repo's Conventional Commits/Portuguese-imperative
     convention (see `CLAUDE.md`).

8. **Write `state.md`** from `assets/spec-state-template.md`: list every
   task code created in step 7 under `PENDING TASKS`, in dependency order.
   `BLOCKED TASKS` starts empty — it only gains entries later, when
   execution discovers a task can't proceed (record the reason inline, and
   register an issue via the `issue-generator` skill if the blocker is
   out of this spec's scope).

9. **Update the root `.claude/state.md`.** Add a row to the "Resumo por
   spec" table for the new spec (`Diretório` → `.claude/specs/<spec-slug>/`,
   `Status` → `pending`, `Próxima task` → the first code in the new
   `state.md`'s `PENDING TASKS`).

10. **Confirm** the created paths back to the user.

## Keeping `state.md` current during execution

Not part of creation, but the convention this skill sets up: whenever a
task's `status` changes in its own `tasks/<CODE>.md` frontmatter, mirror
that in the spec's `state.md` — drop it from `PENDING TASKS` on
`completed`, move it to `BLOCKED TASKS` with a reason if execution can't
continue, move it back to `PENDING TASKS` if unblocked. Keep the root
`.claude/state.md` summary row in sync the same way `CLAUDE.md`'s
execution rules already require for the old model.
