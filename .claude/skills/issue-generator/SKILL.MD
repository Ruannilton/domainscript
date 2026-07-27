---
name: issue-generator
description: Use this when you need to register/cadastrar a new issue found during work in this repo — creates one markdown file per issue under docs/sdd/issues/ from assets/issue-template.md and indexes it in docs/sdd/issues/open-issues.md.
---

# Issue Generator

Registers a new issue as its own file under `docs/sdd/issues/`. This replaces
the old mechanism of sequential `ISSUE-<n>` blocks appended to
`.claude/issues.md` (now retired — all issues were migrated to
`docs/sdd/issues/`). Use this skill whenever the user asks to open/register/
cadastrar an issue found during work in this repo, including errors found
mid-task that are out of scope for the spec/task being worked on (see root
`CLAUDE.md`).

**Spec-revision issues.** The language spec
(`docs/sdd/steerings/domainscript-spec-v7/`) is this repo's source of truth,
so a second kind of issue is first-class here: the spec itself cannot be
implemented as written — it is silent, ambiguous or self-contradictory on the
point at hand — or something is implemented that the spec never describes.
File these the same way, with two differences: state plainly that it is a
**request to revise the specification**, and describe what the spec must
decide *without* proposing an interim behaviour to ship meanwhile (encoding
the guess in code is exactly what the rule forbids). Prove the finding by
running the spec's own example verbatim through `dsc` and quoting the output.
Name the file `spec-v7-<slug>.md` and index it under the "Revisão da
especificação" heading of `open-issues.md`; the existing `spec-v7-*.md` files
are the reference format.

## Resources

- `assets/issue-template.md` — the template every issue file must follow,
  verbatim structure (do not add/remove fields).

## Procedure

1. **Gather the issue's fields.** Pull what you can from the conversation;
   ask the user for anything missing:
   - **Title** — short, human-readable name for the issue. Used to derive
     the file name.
   - **SPEC** — the spec this issue relates to, or `N/A` if none.
   - **TASK** — the task code this issue relates to, or `N/A` if none.
   - **DESCRIPTION** — what's wrong, the context, and the impact.

2. **Derive `<issue-name>`** — a kebab-case slug of the title: lowercase,
   strip accents/diacritics to plain ASCII, replace anything that isn't
   `[a-z0-9]` with `-`, collapse repeated `-`, trim leading/trailing `-`.
   (e.g. "Erro de validação no parser" → `erro-de-validacao-no-parser`).

3. **Check for duplicates.** Read `docs/sdd/issues/open-issues.md`; if an
   entry already points at `<issue-name>.md` or an equivalent title, tell the
   user instead of creating a duplicate.

4. **Fill the template.** Read `assets/issue-template.md` and replace its
   placeholders, keeping the exact structure:
   - `[ISSUE-NAME]` → the title
   - `[SPEC-NAME]` → SPEC
   - `[TASK-CODE]` → TASK
   - `[DESCRIÇÃO DA ISSUE]` → DESCRIPTION
   - `SOLVED: []` → leave empty; this only gets filled in later, when the
     issue is actually resolved (e.g. `SOLVED: [commit <hash>]`)

5. **Write the file** to `docs/sdd/issues/<issue-name>.md`.

6. **Append to the index.** Add one line to `docs/sdd/issues/open-issues.md`:
   `- [<Title>](<issue-name>.md)`. If the file is empty/missing a header,
   start it with `# Open Issues` first.

7. **Confirm** the created path back to the user.
