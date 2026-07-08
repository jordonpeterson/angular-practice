# Top 20 Test Cases

The initial fixture set — the reviewable definition of "done" for v1.

**Fixture layout** (design §7): `base/` = Given (runner syncs it to *generate* the lock;
absent = first run), `edit/` = When (overlay; deletions in `edit/_delete`), `expected/` +
`report.json` = Then. Reports asserted structurally; exit codes exact
(`0` clean · `1` findings · `2` usage error).

**Defaults:** providers = all four → managed files `CLAUDE.md`, `AGENTS.md`
(Codex + OpenCode + Cursor), `.claude/rules/*.md`, `.cursor/rules/*.mdc`.
Emission policy (design §2): global blocks → `CLAUDE.md` + `AGENTS.md` only; `.mdc` only
for Cursor-specific features. `conflict = fail`.

---

## A. Propagation, change detection, merge base

**1. propagate-single-edit**
Given `CLAUDE.md` + `AGENTS.md` in sync, both `## Build: make`.
When `CLAUDE.md` `## Build` → `make all`.
Then `AGENTS.md` → `make all`; lock updated; exit 0.

**2. propagate-add-and-delete**
Given both files with `## Build` and `## Legacy`.
When `AGENTS.md` adds `## Security` and deletes `## Legacy`.
Then `CLAUDE.md` gains `## Security`, loses `## Legacy`; exit 0.

**3. sync-noop-idempotent**
Given in-sync tree. When only `README.md` (unmanaged) changes, run `sync` twice.
Then both runs write nothing; trees byte-identical; lock unchanged; exit 0. *(Pins
idempotency — also what makes the runner's generated-lock scheme sound.)*

**4. linked-doc-edit-propagates**
Given `CLAUDE.md` with `@./docs/style.md`; `AGENTS.md` carries the inlined copy (in sync).
When `docs/style.md` content changes.
Then `AGENTS.md` inlined copy updates; `CLAUDE.md` untouched (import intact); exit 0.
*(The "documents they link to" trigger — dependency graph, design §4.1.)*

**5. check-unpropagated-edits**
Given in-sync tree. When `AGENTS.md` edited, then `sync --check`.
Then nothing written; exit 1; `report.json` lists rule `unpropagated-edits`, block, files.

**6. first-run-initial-reconcile**
Given **no `base/`** (no lockfile); `CLAUDE.md` and `AGENTS.md` disagree on `## Build`.
When `sync` (strategy `fail`).
Then nothing written; exit 1; initial-conflict report naming block + both files.

**7. rename-heading**
Given in-sync files with `## Testing`. When `CLAUDE.md` renames it to `## Tests` (body unchanged).
Then `AGENTS.md` shows `## Tests`; report records deterministic rename (delete+add with
similarity match); exit 0.

## B. Conflicts

**8. conflict-fail-isolated**
Given base `## Build: make`, `## Testing: jest`.
When `CLAUDE.md` `## Testing`→`vitest`; `AGENTS.md` `## Testing`→`mocha` **and** `## Build`→`make -j8`.
Then `## Build` propagates to `CLAUDE.md` (clean single-side edit); `## Testing` unchanged
in both; exit 1; conflict report names only `Testing`. *(Conflicts isolate per block.)*

**9. conflict-priority**
Config `[conflict] strategy="priority", priority=["agents-md","claude-md"]`. Same `## Testing` divergence as #8.
Then `mocha` (AGENTS.md) wins everywhere; exit 0; report records the resolution.

## C. In-file conversions (functionality map)

**10. c1-import-inlined-into-agents**
Given `CLAUDE.md` containing `@./docs/style.md` (+ that file); no `AGENTS.md` yet.
When sync. Then `AGENTS.md` created with the imported text **inlined literally**; report
fidelity `lossless`; exit 0.

**11. c1-preserve-import-structure**
Given #10's result in sync. When `AGENTS.md` `## Build` edited (unrelated to the import).
Then `CLAUDE.md` `## Build` updates; its `@./docs/style.md` line intact; `docs/style.md`
untouched; exit 0. *(Flatten-for-merge, restore-on-write.)*

**12. c2-glob-scoped-rule-full-tree**
Given `.cursor/rules/api.mdc` (`globs: src/api/**`, body). When sync (all providers).
Then `.claude/rules/api.md` with `paths: ["src/api/**"]`; nested `src/api/AGENTS.md` for
Codex (lowered); root `AGENTS.md` does **not** duplicate the rule (dedup, C11); report:
`lossy-recoverable` for the Codex lowering; exit 0. *(Every provider's output pinned.)*

**13. c2-nonprefix-glob-warn**
Given `.cursor/rules/tests.mdc` (`globs: **/*.test.ts`). When sync.
Then `.claude/rules/tests.md` gets `paths`; Codex side attaches at root `AGENTS.md`;
report `portable-globs-only` warn, fidelity `lossy-degrading`; exit 0.

**14. c3-cursor-manual-dropped**
Given `.cursor/rules/manual.mdc` with no `globs`/`description`/`alwaysApply`.
When sync. Then omitted from `CLAUDE.md`/`AGENTS.md`; `.mdc` untouched; report
`no-cursor-only-activation`, fidelity `unrepresentable`; exit 0 (portable=warn).

**15. c5-comment-file-local**
Given in-sync files; `CLAUDE.md` `## Build` contains `<!-- maintainer note -->`
(excluded from block hash), `AGENTS.md` has none.
When `AGENTS.md` `## Build` body edited.
Then `CLAUDE.md` body updates **and keeps the comment in place**; comment never appears
in `AGENTS.md`; exit 0.

**16. c6-at-token-escaped**
Given in sync. When `AGENTS.md` gains "Use @types/node for typings."
Then `CLAUDE.md` writes `` `@types/node` `` (backtick-escaped — no phantom import);
`AGENTS.md` keeps it bare; exit 0.

**17. c9-codex-size-cap**
Given content whose flattened `AGENTS.md` exceeds 32 KiB. When sync.
Then `size-within-codex-cap` error with split suggestion; exit 1.

## D. Lint, migrate, introspect

**18. lint-claude-md-max-lines**
Config `lint.claude-md-max-lines={level="error",max=200}`. Given 250-line `CLAUDE.md`.
When `lint`. Then error in report; exit 1.

**19. migrate-claude-to-opencode-prune**
Given no lock; only `CLAUDE.md`. When `sync --to opencode --prune claude-code`.
Then `AGENTS.md` created with its content; `CLAUDE.md` deleted; exit 0.

**20. context-oracle-equivalence**
Given synced repo incl. the `src/api/**` rule from #12.
When `context src/api/handler.ts --diff`.
Then stdout (golden): normalized effective-context blocks identical across harnesses,
minus losses declared in the last sync report; exit 0. Twin: after an unpropagated
hand-edit, exit 1.

---

**Deferred to the next set:** C10 remote-instruction strict reject, Agent-Requested
coercion (C3), layer scoping (C8), heading-slug collisions, Cursor 3.0.16 version case,
markers/newest conflict strategies, `.cursorrules` legacy ingest.
