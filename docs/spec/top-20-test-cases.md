# Top 20 Test Cases

The initial fixture set — the reviewable definition of "done" for v1. Each is a
Given/When/Then. These become `spec/<id>/` fixtures (design §7); the e2e runner drives the
built binary and diffs the result tree.

**Fixture layout**

```
spec/<id>/
  agentsync.toml   # config (omitted below when default)
  input/           # working tree AFTER the edit + agentsync.lock (= pre-edit merge base)
  expected/        # exact resulting tree
  report.txt       # diagnostics + exit code
```

"When … edit happens" = a working file in `input/` differs from the base recorded in
`input/agentsync.lock`. Providers: `CLAUDE.md` (Claude), `AGENTS.md` (Codex + OpenCode,
shared), `.cursor/rules/*.mdc` (Cursor). Default config = all detected providers, `conflict
= fail`.

---

## A. Propagation & change detection

**1. propagate-single-edit** — one edit fans out.
Given `CLAUDE.md`, `AGENTS.md`, `rules/build.mdc` all agree on `## Build: make`, lock = that.
When `CLAUDE.md` `## Build` → `make all`.
Then `AGENTS.md` and `build.mdc` bodies → `make all`; lock updated; exit 0.

**2. propagate-new-section** — added block appears everywhere.
Given the three in sync. When a new `## Security` section is added to `AGENTS.md`.
Then `CLAUDE.md` gains `## Security`; a new `rules/security.mdc` is created; exit 0.

**3. propagate-deletion** — removed block disappears everywhere.
Given all three have `## Legacy`. When `## Legacy` deleted from `CLAUDE.md`.
Then removed from `AGENTS.md` and `rules/legacy.mdc` (file deleted); exit 0.

**4. noop-unrelated-change** — change detection skips non-context edits.
Given in sync. When only `README.md` changes.
Then no context file or lock is written; exit 0 (fast path).

**5. check-detects-drift** — the CI gate fails on unsynced files.
Given `AGENTS.md` hand-edited but `CLAUDE.md`/`.mdc` not, no re-sync. When `sync --check`.
Then exit non-zero; report lists files differing from the reconciled result; nothing written.

## B. Conflicts

**6. conflict-fail-default** — divergent edits halt.
Given base `## Testing: jest`. When `CLAUDE.md`→`vitest` and `AGENTS.md`→`mocha`.
Then exit non-zero; report names block `Testing` + both files; no files changed.

**7. conflict-priority-resolves** — priority strategy picks a winner.
Config `conflict.strategy="priority"`, `priority=["agents-md","claude-code"]`. Same divergence as #6.
Then `AGENTS.md` value (`mocha`) wins; `CLAUDE.md` + `.mdc` rewritten to it; exit 0.

**8. conflict-isolated-to-block** — a conflict doesn't block clean blocks.
Given divergent `## Testing` (as #6) **and** a clean `## Build` edit in `CLAUDE.md` only.
Then `## Build` propagates to all; `## Testing` reported as conflict; exit non-zero.

## C. In-file functionality conversion (functionality-map C1–C10)

**9. c1-import-inlined-to-codex** — inline-expand where imports are unsupported.
Given `CLAUDE.md` with `@./style.md` and `style.md: "2-space indent"`.
When sync. Then `AGENTS.md` contains the literal `2-space indent` text (no `@`); fidelity lossless.

**10. c1-preserve-import-structure** — flatten-for-merge, restore-on-write.
Given #9 in sync. When an *unrelated* section of `AGENTS.md` is edited.
Then `CLAUDE.md` still uses `@./style.md` (not flattened); `style.md` untouched; exit 0.

**11. c2-glob-rename** — `globs` ⇄ `paths` is lossless.
Given `rules/api.mdc` frontmatter `globs: src/api/**`. When sync.
Then `.claude/rules/api.md` frontmatter `paths: ["src/api/**"]`; same body.

**12. c2-nonprefix-glob-to-codex-warn** — non-directory glob can't reach Codex.
Given `rules/tests.mdc` `globs: **/*.test.ts`. When sync (Codex/AGENTS.md target).
Then diagnostic `portable-globs-only` (warn); block attached to repo root; exit 0.

**13. c3-cursor-manual-dropped** — Manual activation is unrepresentable elsewhere.
Given `rules/manual.mdc` (no globs/description/alwaysApply). When sync.
Then omitted from `CLAUDE.md`/`AGENTS.md`; diagnostic `no-cursor-only-activation` (unrepresentable).

**14. c5-html-comment-stripped** — Claude-invisible comments must not leak.
Given `CLAUDE.md` with `<!-- maintainer note -->` block comment. When sync.
Then `AGENTS.md` omits the comment (not copied verbatim); diagnostic notes the strip.

**15. c6-literal-at-escaped-to-claude** — avoid phantom imports.
Given `AGENTS.md` body has literal `@types/node`. When sync.
Then `CLAUDE.md` writes `` `@types/node` `` (backtick-escaped); no import triggered.

**16. c9-codex-size-cap** — warn before silent truncation.
Given flattened `AGENTS.md` > 32 KiB. When lint (or sync).
Then `size-within-codex-cap` error; exit non-zero; suggests splitting into nested files.

**17. c10-remote-instruction-strict-reject** — non-determinism blocked in strict.
Config `compatibility="strict"`. Given `opencode.json` `instructions:["https://…"]`.
When sync. Then `no-remote-instructions` error; exit non-zero; nothing fetched.

## D. Lint, migrate, introspect

**18. lint-claude-md-max-lines** — best-practice gate.
Config `lint.claude-md-max-lines={level="error",max=200}`. Given 250-line `CLAUDE.md`.
When lint. Then error reported; exit non-zero.

**19. migrate-claude-to-opencode-prune** — one-shot migration.
Config target opencode, `--prune claude-code`. Given only `CLAUDE.md` exists.
When sync. Then `AGENTS.md` created with its content; `CLAUDE.md` deleted; exit 0.

**20. context-validation-equivalent** — effective context matches after sync.
Given synced repo with an `src/api/**`-scoped rule. When `context src/api/x.ts --diff`.
Then Claude and Cursor both show the api guidance active there → equivalent modulo declared
losses; exit 0. (Negative variant: an unsynced repo → non-equivalent, exit non-zero.)

---

**Coverage:** propagation (1–3), change detection & CI gate (4–5), conflicts + strategies
(6–8), each hard conversion C1/C2/C3/C5/C6/C9/C10 (9–17), lint (18), migrate+prune (19),
context-oracle validation (20). Deferred to a later set: managed/user layers (C8),
Cursor block↔file granularity variants, multi-version cases.
