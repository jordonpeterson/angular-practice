# Tasks

Implementation work order for agentsync, in five phases. Written for subagents with no
conversation context: read the [README](README.md) docs list first;
[`docs/design/tool-design.md`](docs/design/tool-design.md) is authoritative for semantics.

## Ground rules (apply to every phase)

- **The e2e suite is the gate:** `go test ./test/e2e/ -v`. Fixtures in `spec/` are the
  spec. Write the red fixture *before* the code that turns it green. Never break a green
  fixture; if behavior must change, change the fixture in the same commit and say why.
- **Determinism is non-negotiable** (design §4.3): byte-exact output, LF, no timestamps,
  no map-iteration ordering leaks. `gofmt` and `go vet` clean.
- One commit per task or coherent task group; imperative subject; explain *why* in body.
- Update docs in the same commit when behavior or architecture changes.
- Fixture layout: `base/` (Given; runner syncs it to generate the lock), `edit/` (When),
  `expected/` + `report.json` (Then), optional `cmd`, `expected-stdout.txt`, `agentsync.toml`.

---

## Phase 1 — Red fixtures for every known defect and designed-but-unbuilt behavior

Add `spec/` fixtures (all red at first) for the **10 confirmed bugs** from code review and
the **C1a/C1b designs**. Bug list (verified against the built binary; line numbers
approximate):

| # | Fixture id | Defect |
| --- | --- | --- |
| B1 | `21-escape-lock-idempotent` | `synthesize` escapes @-tokens for CLAUDE.md but the lock stores the unescaped value → second sync propagates backticks into AGENTS.md; `--check` fails on a just-synced tree (engine.go ~369). Expected: second sync is a byte-exact no-op. |
| B2 | `22-escape-skips-code-fences` | `EscapeAtTokens` corrupts `@Foo` inside fenced code blocks (markdown.go ~146). Expected: fence content untouched. |
| B3 | `23-empty-file-deletion-propagates` | Emptying one file doesn't propagate deletions; emptied lock resurrects content next sync (writeDoc nil-serialize skip, engine.go ~348). Expected: deletion propagates; lock consistent. |
| B4 | `24-write-failure-fails-loudly` | Write errors swallowed; lock advances anyway; next sync silently reverts the user's edit (engine.go ~352). Expected: failed write → error diag, non-zero exit, lock NOT advanced. (Fixture may need a read-only file; if the runner can't express it, cover with a Go unit test instead and note it.) |
| B5 | `25-check-sees-rule-attach-drift` | `--check` never compares rebuilt root AGENTS.md with rule sections attached → exits 0 while sync would rewrite (engine.go ~102). |
| B6 | `26-check-sees-missing-file` | `--check` exits 0 when an entire managed file was deleted, though sync recreates it (checkDiags skip, engine.go ~299). |
| B7 | `27-rule-name-collision` | A CLAUDE.md section whose heading equals a rule name is merged AND rule-attached → duplicate sections in AGENTS.md (exclusion only applied to agents-md, engine.go ~73). Expected: collision handled once, deterministically (pick: rule owns the key; main-merge section is a lint error). |
| B8 | `28-rule-deletion-propagates` | Deleting `.cursor/rules/api.mdc` resurrects it from the Claude copy instead of propagating the deletion (resolveRule ignores lock absence, rules.go ~140). |
| B9 | `29-rule-conflict-keeps-lock` | A conflicted rule drops its `rule:` lock entry, making the conflict unresolvable by reverting one side (rules.go ~95). Expected: base hash preserved, like main blocks. |
| B10 | `30-rule-priority-strategy` | `[conflict] strategy="priority"` resolves block conflicts but rule conflicts still hard-fail (rules.go ~136). Expected: same strategy semantics everywhere. |

Design fixtures (semantics in functionality-map C1a/C1b and design §3.1a):

- `31-c1a-thin-claude-delegates` — CLAUDE.md = `@AGENTS.md` + tail; edit AGENTS.md →
  CLAUDE.md untouched, no synthesized copies, no conflicts.
- `32-c1a-tail-is-provider-local` — tail sections never propagate to other files.
- `33-c1a-duplicate-import-content-lint` — literal section duplicating an imported key →
  `duplicate-import-content` diagnostic.
- `34-c1b-import-cycle-error` — CLAUDE.md ↔ AGENTS.md cycle → exit 1, `import-cycle`
  diag naming the path, nothing written.
- `35-generates-backflow` — edit a lowered `src/api/AGENTS.md`; default: edit flows back
  to the `.mdc` source (config `frozen` variant → drift error). Decides the `generates`
  edge policy (design §3.1a).

## Phase 2 — Refactor to the five-component architecture

Restructure per design §3 / implementation-options §10 layout. **All 16+ green fixtures
must stay green after every commit in this phase** (behavior-preserving).

1. Create `internal/graph`: `Block`, `CompiledPart{harness, location, blockRef,
   transform, lossLabel}`, typed edges (`materializes | delegates | generates |
   attaches-degraded`), loss labels.
2. Split `internal/engine` into `internal/adapter/<harness>` (generator: files → graph,
   incl. the compiled projection; emitter: planned blocks → files) and `internal/merge`
   (3-way decisions, lockfile, conflict strategies). The engine must contain **zero**
   `spec.id == "..."` harness conditionals — capability differences live in adapter data.
3. Unify the rule merge with the block merge: `resolveRule` is deleted; rules become
   blocks with `Scope=Glob` flowing through the same `decide()` path (fixes B8–B10
   structurally; their fixtures go green here or in Phase 4).
4. Move config to `internal/config`; replace the hand-rolled TOML subset with
   `pelletier/go-toml/v2` (if module fetch is impossible in the sandbox, keep a parser but
   it must handle inline tables — `{ level = "error", max = 200 }` — and quoted `#`).

**Delete in this phase** (superseded or flagged by review):
- `internal/engine/engine.go` + `internal/engine/rules.go` as files (contents migrate).
- `resolveRule` (unified into `decide`).
- Hand-rolled `loadConfig` TOML scanner (see above).
- `equalStrings` (→ `slices.Equal`), `distinctVals`/`distinctValsAll` pair (→ one
  counting helper), two of the three write-if-changed implementations (keep `writePath`).
- `test/e2e/runner_test.go`: the duplicated `diagnostic` struct (import the engine's
  type), `copyTree` (→ `os.CopyFS`), the `var _ = fmt.Sprintf` line.
- Cheap wins while touching the runner: `t.Parallel()` per fixture.

## Phase 3 — Compiled projection + verified writes

1. Generators emit the compiled projection: effective context per (harness × location) —
  walk direction, override vs concat, on-demand nested loading, OpenCode's walk-up-only
  behavior, per design §8 + research docs.
2. Wire the **verified-writes postcondition** into `sync` (design §3.1b): rebuild the
  compiled layer from planned writes; graphs must be equivalent modulo loss-labeled
  edges; on failure → refuse to write, report the compiled diff. Turns B5/B7-class bugs
  into runtime-impossible states.
3. `--check` gains the semantic layer (compiled-space drift) → fixtures 25/26 green.
4. Lockfile: versions/hashes unchanged; comments remain excluded from hashes (C5).

## Phase 4 — Fix the remaining confirmed bugs

Turn every Phase-1 fixture green that Phases 2–3 didn't already:
- B1/B2: store the *written form's* hash per file in the lock (or store per-file rendered
  hashes) so escaping doesn't read as an edit; make `EscapeAtTokens` fence- and
  span-aware in `internal/markdown`.
- B3: distinguish "empty doc" from "no doc" in emitters; deletions propagate; lock never
  silently empties.
- B4: all writes return errors; any failure → diagnostic + exit ≥1 + lock not advanced.
- B7: implement the chosen collision rule.
- C1a/C1b fixtures (31–34): delegation edges + cycle detection (explicit graph check, not
  the depth cap). 35: implement chosen backflow default.

## Phase 5 — Complete the v1 surface, then clean up

1. `lint` command via a **rule registry** (id → default severity, config-overridable —
   design §5/§6): `claude-md-max-lines` (fixture 18), `size-within-codex-cap` (17),
   plus existing sync-time rules routed through the registry.
2. `sync --to <provider> --prune <provider>` migration (fixture 19).
3. `context <path> [--as harness] [--diff]` — print the generators' compiled projection;
   `--diff` renders per-harness side-by-side and exits 1 on non-equivalence beyond
   declared losses (fixture 20 + its negative twin).
4. **Docs generator**: render each `spec/*/intent.md` + input→expected diff (+ `context
   --diff` output where present) into `docs/site/` — the user-facing documentation is the
   test corpus, rendered (design §7). Add a fixture or CI step asserting the site builds
   from the current spec set.
5. Update README (status, all-green count), prune stale `[verify]` items resolved along
   the way.
6. **Final task: delete this `Tasks.md`** — when every fixture is green and the docs
   generator runs, this file's job is done; the spec suite and design docs are the source
   of truth. Delete it in the same commit that flips the last fixture green.
