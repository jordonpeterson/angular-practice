# agentsync — Design

How the tool is used, and the architecture that follows.

*Companion to [`../research/context-file-equivalencies.md`](../research/context-file-equivalencies.md).*

---

## 1. Use cases → modes

Five needs, three commands (`sync`, `lint`, `context`), one engine. `check` is `sync` in verify mode.

| Use case | Command | Notes |
| --- | --- | --- |
| **Migrate** Claude Code ↔ OpenCode (any pair) | `agentsync sync --to Y --prune X` | One-shot. Propagates into Y, removes X. |
| **Multiple agents** kept in sync in CI | `agentsync sync` (write mode) | Reconciles managed files via 3-way merge (§2). |
| **CI gate** — fail if out of sync | `agentsync sync --check` | Dry-run; non-zero exit if any file isn't reconciled. |
| **Restrict to portable context** | `agentsync lint` (compatibility rules) | warn/error per config. |
| **Enforce best practices** (≤200 lines, etc.) | `agentsync lint` (quality rules) | warn/error per config. |
| **See/validate what an agent loads** | `agentsync context <path> --as Y` | Assembles effective context (§8). |

`sync` reconciles; `lint` validates; `context` introspects; `check` verifies.

---

## 2. Core model: bidirectional 3-way merge

No file is privileged. A maintainer edits whichever context file they like; the tool propagates the edit to the others — git-style merge, not a compiler.

The common ancestor is a **lockfile** (`agentsync.lock`) recording the last-synced state of every managed file. Each run is a 3-way merge: base (lockfile) vs. current working files (`CLAUDE.md`, `AGENTS.md`, `*.mdc`) → per-block reconcile → updated files + lockfile.

Reconciliation runs **per logical block** (a section keyed by heading path — §3.1), not per file. Per block, comparing each file against base:

| Situation | Result |
| --- | --- |
| Block unchanged everywhere | leave as-is |
| Block changed in **exactly one** file | **propagate** that version to all others |
| Same block changed identically in several files | already agree; adopt it |
| Same block changed **divergently** in two+ files | **conflict** → resolve (§2.1) |

The everyday path (one section edited in one file) always propagates cleanly. Conflicts require the **same section** edited **differently** in **two providers** since last sync — rare by construction.

**Determinism holds.** Output is a pure function of `(lockfile base + current files + config)` — same inputs → same bytes and conflict verdicts, so `--check` and golden tests (§7) stay exact. "Bidirectional" means *which file may be edited*, not nondeterminism.

The IR's interchange format defaults to **`AGENTS.md`** conventions (three of four tools read it natively — research §1), but on disk no file is the "source."

**Merge participants are files, not providers.** Codex, OpenCode, and Cursor all read `AGENTS.md`, so the merge sides are the distinct managed *files* (`CLAUDE.md`, `AGENTS.md`, `.cursor/rules/*`, `.claude/rules/*`), each mapped N:1 to the providers that consume it. Consequences:
- A "conflict between Codex and OpenCode" is impossible — they share one file.
- A shared file is constrained to the **intersection of its consumers' capabilities** (Codex's feature floor governs `AGENTS.md`).
- Config ids in `[conflict] priority` are **file ids** (`agents-md`, `claude-md`, `cursor-rules`), not provider ids.

**Emission policy (no double-injection).** Cursor natively reads `AGENTS.md` *and* `.cursor/rules/*.mdc`; emitting global content to both would load it twice in Cursor. Default policy: global blocks live in `AGENTS.md` (+ `CLAUDE.md` for Claude); `.mdc` files are emitted **only** for content needing Cursor-specific features (globs, activation modes). Overridable via `[emit] cursor = "rules-only" | "agents-md-only" | "both"`.

### 2.1 Conflict resolution

Conflicts are a configurable policy, not a crash. `[conflict] strategy`:

| Strategy | Behavior | Best for |
| --- | --- | --- |
| **`fail`** (default in CI) | Report conflicting block + files, exit non-zero, change nothing. | CI gate — a human decides. |
| **`priority`** | Configured provider order wins the block; others rewritten to match. Deterministic. | Teams with a designated primary agent. |
| **`markers`** | Write git-style `<<<<<<<`/`>>>>>>>` markers into each file's block. | Local interactive use. |
| **`newest`** | Most recent mtime wins. **Opt-in only** — mtime isn't reproducible, rejected in `strict`/CI. | Quick local convenience. |

Because conflicts key on blocks, a conflict in `## Testing` never blocks clean propagation of `## Build` in the same run.

> First run (no lockfile): the union of files is treated as base. Pre-existing disagreement surfaces as an initial conflict to reconcile once; afterward the lockfile makes single-file edits unambiguous.

---

## 3. Architecture

Three-stage pipeline over a shared IR: **read adapters** (`CLAUDE.md`, `AGENTS.md`, `*.mdc`, `opencode.json`, `.cursorrules`) → **canonical IR** (also feeds the lint engine §6) → **write adapters**.

### 3.1 The IR (the crux)

The IR is the **union** of provider features. Convert source → IR → target; the target adapter reports what it can't express.

An IR document is an ordered list of **instruction blocks**:

```
Block {
  content:    Markdown          // the actual guidance
  scope:      Global
            | PathGlob(pattern)  // "src/api/**/*.ts"
            | Directory(path)    // "services/payments/"
  activation: Always            // Cursor superset; most tools only have Always
            | AutoAttached       //   (glob-triggered)
            | AgentRequested     //   (model decides from description)
            | Manual             //   (@-referenced)
  layer:      Managed | User | Project | Local
  provenance: source file + line   // for diagnostics and round-trip
}
```

**Block identity.** Each block carries a stable **key** — its heading path, e.g. `Testing / Unit tests`. That key is how the tool knows `## Testing` in `CLAUDE.md` is the same block as `## Testing` in `AGENTS.md`, enabling the 3-way merge (§2). Keys are human-editable and survive across formats. A rename = "old key deleted + new key added"; when ambiguous, fall back to content similarity, else raise a conflict rather than guess.

**Comments are file-local.** `<!-- -->` comments (invisible to Claude, visible to every other agent — functionality-map C5) do **not** participate in the merge: they're excluded from block content hashes, preserved in situ when their file's block is rewritten, and never propagated to other files. A `comment-visibility` lint warns when a comment sits in a file whose consumers would see it.

Imports/includes (`@path`, `opencode.json instructions`, `.cursorrules`) are **resolved and flattened** into blocks on read, then **re-introduced only where the target supports them** on write. Anything a target can't represent becomes a **lossiness diagnostic** (§6), never a silent drop.

### 3.2 Adapters = maintainability

Each provider = one read + one write adapter behind a stable interface. Adding a provider or a new context pattern is a new adapter — **the IR and core don't change.** Adapter capabilities are declared as data (supports-globs? supports-imports? max-bytes?) so lowering and the compatibility linter read from one table.

---

## 4. Change detection & determinism

### 4.1 Only run when relevant files change

Context files link to other docs (`@imports`, `opencode.json instructions`, Cursor `@file`); a change to a linked doc must also trigger a sync.

1. Build the **dependency graph**: context files + transitive closure of everything they import/link.
2. **Trigger set** = that closure.
3. In CI, intersect the git diff with the trigger set. Empty → exit 0 immediately (fast no-op).

### 4.2 Lockfile = merge base + speed

`agentsync.lock` does two jobs. As **merge base** (§2) it records the last-synced content hash of every managed block, so the tool knows which side changed. As a **speed cache** those hashes let `sync` skip unchanged blocks and `--check` bail early when nothing in the trigger set moved. It's committed to the repo — the shared "we agreed on this" snapshot.

### 4.3 Determinism rules (non-negotiable)

- No timestamps, absolute paths, or RNG in output.
- Canonical Markdown serializer: stable heading order, sorted globs, normalized whitespace. Byte-for-byte reproducible.
- **Remote instructions break determinism.** OpenCode's `instructions` can point at URLs. `strict` mode refuses them; otherwise snapshot+pin them into the lockfile so builds reproduce from committed state.

---

## 5. Configuration

Zero-config default: detect present provider files and keep them in sync. Override in `agentsync.toml`:

```toml
providers = ["claude-code", "cursor", "opencode"]  # which files to manage
compatibility = "portable"                          # off | portable (LCD) | strict

[versions]                                # optional; defaults to "latest" for every provider (§9)
# current scope targets latest-only, so this can be omitted entirely
# claude-code = ">=2.1.198"               # ranges/pins are supported when multi-version matters
# cursor      = ">=2.2 <3.0.16 || >3.0.16"# (exclude a known-broken build) — future use
# set "detect" to resolve installed CLI versions instead

[conflict]
strategy = "fail"                       # fail | priority | markers | newest
priority = ["agents-md", "claude-md"]   # FILE ids (§2), used when strategy = "priority"

[emit]
cursor = "rules-only"                   # avoid double-injection: Cursor reads AGENTS.md natively (§2)

[lint]
claude-md-max-lines      = { level = "error", max = 200 }
portable-globs-only      = "warn"       # non-directory globs won't survive to Codex
no-remote-instructions   = "error"
broken-import            = "error"

[paths]
ignore = ["vendor/**", "**/node_modules/**"]
```

Every rule takes a severity (`off | warn | error`), so the same binary is a soft advisor or a hard gate by config.

---

## 6. Lint & compatibility

The linter runs over the IR + raw files. Two rule families:

**Quality:** `claude-md-max-lines` (200 default), oversized-block, `broken-import`, `comment-visibility` (§3.1), `unpropagated-edits` (the `--check` gate).

> **`--check` semantics.** In a bidirectional tool a hand-edit isn't "drift" — it's the intended workflow. `--check` fails whenever **unpropagated edits exist**. Expected consequence: any PR touching a context file fails `--check` until `sync` runs (locally, or by CI in write mode). Document this for users or check-failures read as bugs.

**Compatibility (portable-only):** every entry in research §4's lossiness table becomes a rule. `compatibility = "portable"` → warnings, `"strict"` → errors:
- `portable-globs-only` — glob isn't a directory prefix, no faithful Codex form.
- `no-cursor-only-activation` — `AgentRequested`/`Manual` exist nowhere else.
- `size-within-codex-cap` — flattened doc would exceed Codex's `project_doc_max_bytes`.
- `no-remote-instructions` — non-reproducible.

The compatibility linter and `sync` lowering share the same adapter-capability table, so "what won't convert" and "what we warn about" can't disagree.

---

## 7. Tests are the spec, and the spec is the docs

Each requirement stated once, used three ways — test, spec, docs — as a fixture directory:

```
spec/
  propagate-single-edit/
    intent.md            # human statement of the requirement (the "why")
    agentsync.toml       # config for this case
    base/                # GIVEN: last-synced tree — runner runs `sync` here to GENERATE the lock
    edit/                # WHEN: overlay applied over base (deletions listed in edit/_delete)
    expected/            # THEN: exact tree after the command (golden)
    report.json          # THEN: diagnostics (rule, block, file, severity, fidelity) + exit code
```

The runner never ships hand-authored lockfiles (hashes would rot): it syncs `base/` to produce the lock, overlays `edit/`, then runs the command under test. `base/` absent = first-run (no lockfile). Given/When/Then maps 1:1 onto `base/`/`edit/`/`expected/`. Reports are asserted **structurally** (JSON), so diagnostic wording can change without breaking fixtures; exit codes are exact.

**Exit-code contract:** `0` clean · `1` findings (conflict, lint error, check failure) · `2` usage/config error.

- **Tests:** runner runs `agentsync` on `input/` and asserts `output == expected/` byte-for-byte plus report/exit. Determinism (§4.3) makes this exact.
- **Spec:** the fixture *is* the requirement; an un-fixtured behavior isn't a requirement.
- **Docs:** a generator renders each `intent.md` + its `input → expected` diff into `docs/` as a worked example. **The documentation is the test corpus, rendered.** New requirement → new fixture → new test → new doc, from one edit.

Golden trees beat prose assertions: the tool's job is producing exact files.

---

## 8. Context introspection & validation (`agentsync context`)

"Did the sync produce equivalent context?" can't be answered by diffing files — files are *supposed* to differ per harness. Answer it by comparing the **effective context each harness assembles at a path**. Native support is uneven:

| Harness | Native "what's loaded here" | Machine-readable? |
| --- | --- | --- |
| **Claude Code** | `/memory`, `/context`, **`InstructionsLoaded` hook** | ✅ hook is programmatic |
| **OpenCode** | `opencode debug config`, `opencode debug agent build` | ⚠️ config-level; rule visibility partial **[verify]** |
| **Codex** | TUI/session logs (`codex -c log_dir=…`) or ask-the-model probe | ⚠️ logs only; probe non-deterministic |
| **Cursor** | GUI "Rules" indicator only | ❌ no headless equivalent |

Only Claude Code answers cleanly and programmatically; Cursor can't answer headlessly. So the tool provides its own, deterministic:

```
agentsync context <path> --as <harness>     # print the effective, assembled context
agentsync context <path> --diff             # all harnesses side by side at that path
```

It replays each read-adapter plus that harness's assembly rules (walk direction, concat vs override, glob/`paths` matching, on-demand subdir loading — research + §3.1) to produce the fully-resolved context a harness *would* load at `<path>`. **The same file assembles differently per harness:** a nested `src/api/AGENTS.md` is in-context for Codex at `src/api/x.ts` but invisible to OpenCode (below-cwd files ignored) — the oracle must model per-consumer assembly, not per-file. Two uses:

1. **Validation oracle.** After a sync, effective context at a path should be equivalent across harnesses **modulo declared fidelity losses** (functionality-map §4). A fixture asserts this — "the sync worked" becomes checkable.
2. **Docs.** Render the compiled context per harness at a location, side by side.

**Calibration.** Where a harness is machine-readable (Claude's hook + `/memory`, `opencode debug config`, Codex logs) we periodically diff our computed context against the real one to keep adapters honest. Cursor has no headless introspection, so for Cursor the tool is the *only* way to see effective context in CI.

---

## 9. Version-aware behavior

> **Current scope:** we target the **latest** version of each harness as of today and ship capability data for those versions only. The model is built version-aware so more versions slot in later **without rework** — but we don't author historical version data now. `[versions]` defaults to `latest`; everything resolves to a single version set.

Harness behavior is **version-dependent**, and these tools ship weekly:

| Harness | Version-sensitive behavior |
| --- | --- |
| **Claude Code** | `.claude/rules/` `paths` matching through symlinks: **v2.1.198+**. Auto memory: **v2.1.59+**. `.claude/CLAUDE.md` location and `.claude/rules/` added over 2.x. |
| **Cursor** | `.cursorrules` deprecated ~**0.43**. New rules as **folders** in `.cursor/rules/` as of **2.2**. **3.0.16** regression: `alwaysApply: true` silently treated as "requestable". |
| **Codex** | `project_doc_max_bytes` default 32 KiB (configurable); fallback filename list config-driven and has shifted. |
| **OpenCode** | AGENTS.md + `opencode.json instructions` are recent; `.opencode/AGENTS.md` discovery still landing. |

So "does feature X work" is meaningless without a version. The model:

### 9.1 Capabilities are versioned

The adapter-capability table (§3.2) is `feature → [ {version-range, behavior} ]`, not `feature → bool`. Each entry records introduced/changed/deprecated/removed-in, with a source. The engine resolves the **targeted** version(s) to a concrete capability set before any conversion, lint, or `context` assembly.

### 9.2 Targeting: pin or detect

`[versions]` pins ranges (reproducible, CI default). `detect` reads installed CLI versions (`claude --version`, `codex --version`, `opencode --version`, Cursor build). Pinned versions are written into `agentsync.lock` so syncs reproduce regardless of what's installed — versions are an **explicit input**, not ambient state (§4.3).

### 9.3 Portability is across the version *range*, not just harnesses

When a target is a range (`cursor = ">=2.0"`), the portable/LCD feature set is the **intersection over every version in the range**. If folders-in-`.cursor/rules/` only work at ≥2.2, a repo targeting `>=2.0` can't rely on it. The `compatibility` linter reads the resolved range, so "portable" means across the harnesses **and versions** you declared. Known-broken builds can be excluded (`!= 3.0.16`).

### 9.4 Conformance probing keeps data honest

Curated data drifts as tools ship. Two **separate** harnesses — do not conflate:

1. **Fixture runner (§7)** — hermetic. Drives *our* binary only; no agent CLIs, no network, no credentials. Runs on every commit.
2. **Conformance probe** — scheduled + credentialed. Drives the *real* agent CLIs (real sessions: API keys, cost, nondeterministic output) against probe fixtures, observes which files/blocks actually load, and emits a capability profile. Updates the versioned table from ground truth; acts as a regression alarm (the Cursor 3.0.16 case). Allowed to be flaky; failures open an issue, never block CI. Cursor may not be automatable headlessly at all — its cells may need manual confirmation.

### 9.5 `context` is version-parameterized

`agentsync context <path> --as cursor@2.1` vs `--as cursor@2.2` can legitimately differ. The introspection command (§8) takes an optional version so validation and docs can show a specific release, not just "latest."

---

## 10. Decisions (resolved)

1. **Sync model** — ✅ **bidirectional**, via lockfile-based 3-way merge (§2). Any file may be edited; edits propagate; divergent edits to the same block are conflicts resolved by configurable logic (§2.1).
2. **Language** — ✅ **Go**. Single static binary, instant startup, great CI ergonomics.
3. **Interchange format** — `AGENTS.md` conventions inside the IR (widest native support); no on-disk file is privileged.
4. **Config format** — `TOML` (`agentsync.toml`).
