# agentsync — Design

How the tool is used, and the architecture that follows from it.

*Companion to [`../research/context-file-equivalencies.md`](../research/context-file-equivalencies.md).*

---

## 1. Use cases → modes

Five business needs, three commands, one engine.

| Use case | Command | Notes |
| --- | --- | --- |
| **Migrate** Claude Code ↔ OpenCode (or any pair) | `agentsync sync --to Y --prune X` | One-shot. Propagates into Y's files, removes X's. |
| **Support multiple agents**, keep them in sync in CI | `agentsync sync` (write mode) | Reconciles all managed files via 3-way merge (§2); propagates edits. |
| **CI gate** — fail the build if files are out of sync | `agentsync sync --check` | Dry-run; exits non-zero if any file isn't the reconciled result. |
| **Restrict to portable context** | `agentsync lint` (compatibility rules) | warn or error per config. |
| **Enforce best practices** (≤200 lines, etc.) | `agentsync lint` (quality rules) | warn or error per config. |
| **See/validate what an agent loads** at a path | `agentsync context <path> --as Y` | Assembles the effective context a harness loads there (§8). |

`sync` reconciles; `lint` validates; `context` introspects; `check` is `sync` in verify
mode. That's the whole surface.

---

## 2. Core model: bidirectional 3-way merge

**No file is privileged. A maintainer edits whichever context file they like, and the
tool propagates that edit to the others.** The mental model is git's merge, not a
compiler: the tool reconciles several files against a common ancestor.

The common ancestor is a **lockfile** (`agentsync.lock`) recording the last-synced state
of every managed file. Each run is a 3-way merge:

```
              agentsync.lock                    (base = last synced state)
                    │
        ┌───────────┼───────────┐
        ▼           ▼           ▼
   CLAUDE.md    AGENTS.md    *.mdc             (current working files)
        └───────────┼───────────┘
                    ▼
             per-block reconcile
                    ▼
   CLAUDE.md    AGENTS.md    *.mdc  +  updated agentsync.lock
```

Reconciliation runs **per logical block** (a section, keyed by its heading path — see
§3.1), not per whole file. For each block the tool compares each file's current content
against the base:

| Situation | Result |
| --- | --- |
| Block unchanged everywhere | leave as-is |
| Block changed in **exactly one** file | **propagate** that version to all others |
| Same block changed identically in several files | already agree; adopt it |
| Same block changed **divergently** in two+ files | **conflict** → resolve (§2.1) |

The everyday path — *maintainer edits one section of one file* — is always the
single-changed-file case, which propagates cleanly with no conflict. Conflicts require the
**same section** to be edited **differently** in **two different providers** since the last
sync, which is rare by construction.

**Determinism holds.** Output is still a pure function of
`(lockfile base + current files + config)`. Same inputs → same bytes and the same conflict
verdicts, so `--check` in CI and golden-file tests (§7) remain exact. Bidirectional refers
to *which file may be edited*, not to any nondeterminism in the result.

The canonical/interchange format inside the IR defaults to **`AGENTS.md`** conventions —
three of the four tools read it natively (see research §1) — but on disk no file is the
"source."

### 2.1 Conflict resolution (business logic)

Conflicts are a first-class, configurable policy — not a crash. `[conflict] strategy`:

| Strategy | Behavior | Best for |
| --- | --- | --- |
| **`fail`** (default in CI) | Report the conflicting block + files, exit non-zero, change nothing. | CI gate — a human decides. |
| **`priority`** | A configured provider order wins the block; others are rewritten to match. Deterministic. | Teams with a designated "primary" agent. |
| **`markers`** | Write git-style `<<<<<<<`/`>>>>>>>` markers into each file's block for manual resolution. | Local interactive use. |
| **`newest`** | The file with the most recent mtime wins. **Opt-in only** — mtime isn't reproducible, so it's rejected in `strict`/CI. | Quick local convenience. |

Because conflicts key on blocks, a conflict in `## Testing` never blocks a clean
propagation of `## Build` in the same run. The tool resolves everything it can and reports
only the genuinely divergent blocks.

> First run (no lockfile): the tool treats the union of files as the base. If they already
> disagree, that's surfaced as an initial conflict to reconcile once; afterward the
> lockfile makes single-file edits unambiguous.

---

## 3. Architecture

A three-stage pipeline over a shared intermediate representation (IR).

```
  read (adapters)          normalize            write (adapters)
 ┌──────────────┐      ┌──────────────┐      ┌──────────────┐
 │ CLAUDE.md    │      │              │      │ CLAUDE.md    │
 │ AGENTS.md    │─────▶│  canonical   │─────▶│ *.mdc        │
 │ *.mdc        │      │     IR       │      │ opencode.json│
 │ opencode.json│      │              │      │ ...          │
 │ .cursorrules │      └──────────────┘      └──────────────┘
 └──────────────┘             │
                              ▼
                       lint engine (§6)
```

### 3.1 The IR (the crux)

Everything hinges on an IR that is the **union** of provider features. Convert
source → IR → target; the target adapter reports what it cannot express.

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

**Block identity (what makes the merge work).** Each block carries a stable **key** — its
heading path, e.g. `Testing / Unit tests`. That key is how the tool knows the `## Testing`
section in `CLAUDE.md` *is the same block as* `## Testing` in `AGENTS.md`, so it can 3-way
merge them (§2). Keys are human-editable and survive across formats. A rename is detected
as "old key deleted + new key added"; when that's ambiguous the tool falls back to content
similarity and, if still unsure, raises it as a conflict rather than guessing.

Imports/includes (`@path`, `opencode.json instructions`, `.cursorrules`) are **resolved
and flattened** into blocks on read, then **re-introduced only where the target supports
them** on write. Anything a target can't represent becomes a **lossiness diagnostic**
(see §6), never a silent drop.

### 3.2 Adapters = maintainability

Each provider is one read adapter + one write adapter behind a stable interface. Adding a
provider, or absorbing a new context pattern, is a new adapter — **the IR and core don't
change.** This is the answer to "maintain the tool over time as patterns evolve." Adapter
capabilities are declared as data (supports-globs? supports-imports? max-bytes?) so the
lowering logic and the compatibility linter read from one table.

---

## 4. Change detection & determinism

### 4.1 Only run when relevant files change

Context files link to other docs (`@imports`, `opencode.json instructions`, Cursor
`@file`). A change to a *linked* doc must also trigger a sync. So:

1. Build the **dependency graph**: context files + the transitive closure of everything
   they import/link.
2. The **trigger set** = that closure.
3. In CI, intersect the git diff with the trigger set. Empty intersection → exit 0
   immediately (fast no-op). This is what makes "only activate when agent context or its
   linked docs change" precise rather than a filename guess.

### 4.2 Lockfile = merge base + speed

`agentsync.lock` serves two jobs. As the **merge base** (§2) it records the last-synced
content hash of every managed block, so the tool can tell which side of a 3-way merge
actually changed. As a **speed cache** those same hashes let `sync` skip blocks that are
unchanged everywhere and let `--check` bail early when nothing in the trigger set moved.
The lockfile is committed to the repo — it's the shared "we all agreed on this" snapshot.

### 4.3 Determinism rules (non-negotiable)

- No timestamps, no absolute paths, no RNG in generated output.
- Canonical Markdown serializer: stable heading order, sorted globs, normalized
  whitespace. Byte-for-byte reproducible.
- **Remote instructions break determinism.** OpenCode's `instructions` can point at URLs.
  In `strict` mode we refuse them; otherwise we snapshot+pin them into the lockfile so a
  build is reproducible from committed state.

---

## 5. Configuration

Zero-config default: detect which provider files are present and keep them all in sync
with each other. Override in `agentsync.toml`:

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
priority = ["agents-md", "claude-code"] # used when strategy = "priority"

[lint]
claude-md-max-lines      = { level = "error", max = 200 }
portable-globs-only      = "warn"       # non-directory globs won't survive to Codex
no-remote-instructions   = "error"
broken-import            = "error"

[paths]
ignore = ["vendor/**", "**/node_modules/**"]
```

Every rule takes a severity (`off | warn | error`), so the same binary is a soft advisor
or a hard gate depending on config. That single knob covers "warn or error based on
configuration."

---

## 6. Lint & compatibility

The linter runs over the IR + raw files. Two rule families:

**Quality** (best practices):
- `claude-md-max-lines` (200 default), oversized-block, `broken-import`,
  `out-of-sync` (files disagree and haven't been reconciled — the `--check` gate).

**Compatibility** (portable-only): every entry in the research §4 lossiness table becomes a
rule. When a company wants "context compatible with everything," `compatibility = "portable"`
turns these to warnings and `"strict"` to errors:
- `portable-globs-only` — glob isn't a directory prefix, so it has no faithful Codex form.
- `no-cursor-only-activation` — `AgentRequested`/`Manual` modes exist nowhere else.
- `size-within-codex-cap` — flattened doc would exceed Codex's `project_doc_max_bytes`.
- `no-remote-instructions` — non-reproducible.

The compatibility linter and the `sync` lowering step share the same adapter-capability
table, so "what won't convert" and "what we warn about" can never disagree.

---

## 7. Tests are the spec, and the spec is the docs

Requirement stated once, used three ways: as a **test**, as the **spec**, and as **docs**.

Each requirement is a fixture directory:

```
spec/
  propagate-single-edit/
    intent.md            # human statement of the requirement (the "why")
    agentsync.toml       # config for this case
    input/               # working tree: context files (+ agentsync.lock as merge base)
    expected/            # exact expected tree after sync  (golden)
    expected-report.txt  # expected diagnostics + exit code
```

The `input/` tree carries the working files *and* the `agentsync.lock` merge base, so a
fixture can express "base said X, someone edited CLAUDE.md to Y → every file becomes Y."
Conflict fixtures put divergent edits in two files and assert the chosen `[conflict]`
strategy's output.

- **As tests:** the runner executes `agentsync` on `input/` with the config and asserts
  `output == expected/` byte-for-byte and report/exit match. Determinism (§4.3) makes this
  exact — no fuzzy matching.
- **As spec:** the fixture *is* the requirement. To change behavior you change a fixture;
  an un-fixtured behavior isn't a requirement.
- **As docs:** a generator renders each `intent.md` plus its `input → expected` diff into
  `docs/` as a worked example. **The documentation is the test corpus, rendered.** New
  requirement → new fixture → new test → new doc page, from one edit.

Golden trees beat prose assertions here because the tool's entire job is producing exact
files; the natural assertion is "these exact files."

---

## 8. Context introspection & validation (`agentsync context`)

"Did the sync actually produce equivalent context?" can't be answered by diffing files —
the files are *supposed* to differ per harness. It's answered by comparing the **effective
context each harness assembles at a given path**. Native support for that is uneven:

| Harness | Native "what's loaded here" | Machine-readable? |
| --- | --- | --- |
| **Claude Code** | `/memory`, `/context`, **`InstructionsLoaded` hook** | ✅ hook is programmatic |
| **OpenCode** | `opencode debug config`, `opencode debug agent build` | ⚠️ config-level; rule visibility partial **[verify]** |
| **Codex** | TUI/session logs (`codex -c log_dir=…`) or ask-the-model probe | ⚠️ logs only; probe non-deterministic |
| **Cursor** | GUI "Rules" indicator only | ❌ no headless equivalent |

Only Claude Code answers cleanly and programmatically; Cursor can't answer headlessly at
all. So the tool provides its own, deterministic:

```
agentsync context <path> --as <harness>     # print the effective, assembled context
agentsync context <path> --diff             # all harnesses side by side at that path
```

It replays each read-adapter plus that harness's own assembly rules (walk direction,
concat vs override, glob/`paths` matching, on-demand subdir loading — research + §3.1) to
produce the fully-resolved context a harness *would* load at `<path>`. Two uses:

1. **Validation oracle.** After a sync, the effective context at a path should be
   equivalent across harnesses **modulo the declared fidelity losses** (functionality-map
   §4). A fixture asserts this, so "the sync worked" is a checkable property, not just
   "bytes were written."
2. **Docs.** Render the compiled context per harness at a location, side by side — the
   "contrast the compiled context at a specific place" view.

**Calibration.** Where a harness *is* machine-readable (Claude's hook + `/memory`,
`opencode debug config`, Codex session logs) we periodically diff our computed context
against the real one to keep adapters honest. Cursor has no headless introspection, so for
Cursor the tool is the *only* way to see effective context in CI — a gap we fill outright.

---

## 9. Version-aware behavior

> **Current scope:** we target the **latest** version of each harness as of today and ship
> capability data for those versions only. The model below is built version-aware so more
> versions slot in later **without rework** — but we do not author historical version data
> now. `[versions]` defaults to `latest`; everything resolves to a single version set.

Harness behavior is **version-dependent**, and these tools ship weekly. Real examples:

| Harness | Version-sensitive behavior |
| --- | --- |
| **Claude Code** | `.claude/rules/` `paths` matching through symlinks: **v2.1.198+**. Auto memory: **v2.1.59+**. `.claude/CLAUDE.md` project location and `.claude/rules/` were added over the 2.x line. |
| **Cursor** | `.cursorrules` deprecated ~**0.43**. New rules created as **folders** in `.cursor/rules/` as of **2.2**. **3.0.16** regression: `alwaysApply: true` silently treated as "requestable" (not auto-injected). |
| **Codex** | `project_doc_max_bytes` default 32 KiB (configurable); fallback filename list is config-driven and has shifted. |
| **OpenCode** | AGENTS.md + `opencode.json instructions` are recent; `.opencode/AGENTS.md` discovery is still landing. |

So "does feature X work" is meaningless without a version. The model:

### 9.1 Capabilities are versioned

The adapter-capability table (§3.2) is not `feature → bool`; it's
`feature → [ {version-range, behavior} ]`. Each entry records introduced-in / changed-in /
deprecated-in / removed-in, with a source. The engine resolves the **targeted** version(s)
to a concrete capability set before doing any conversion, lint, or `context` assembly.

### 9.2 Targeting: pin or detect

`[versions]` in config pins ranges (reproducible, the CI default). Alternatively `detect`
reads installed CLI versions (`claude --version`, `codex --version`, `opencode --version`,
Cursor build). Pinned versions are written into `agentsync.lock` so a sync is reproducible
regardless of what's installed on a given machine — versions are an **explicit input**, not
ambient state (preserves determinism, §4.3).

### 9.3 Portability is across the version *range*, not just across harnesses

When a target is a range (`cursor = ">=2.0"`), the portable/LCD feature set is the
**intersection over every version in the range**. If `.cursor/rules/`-as-folders only works
at ≥2.2, then a repo targeting `>=2.0` can't rely on it. The `compatibility` linter reads
the resolved range, so "portable" means "portable across the harnesses **and versions** you
declared." Known-broken builds can be excluded (the `!= 3.0.16` example above).

### 9.4 Conformance probing keeps the data honest

Curated capability data drifts as tools ship. Because the e2e harness already drives the
real CLIs as black boxes (§7), the same fixtures double as a **conformance probe**: run an
actual installed harness version against probe fixtures, observe its behavior, and emit a
capability profile for that version. This (a) self-updates the versioned table from ground
truth, and (b) acts as a **regression alarm** — a scheduled probe against the latest release
catches the day a new version changes behavior (exactly the Cursor 3.0.16 case). This is the
concrete mechanism behind "maintain the tool over time as patterns evolve."

### 9.5 `context` is version-parameterized

`agentsync context <path> --as cursor@2.1` vs `--as cursor@2.2` can legitimately differ.
The introspection command (§8) takes an optional version so validation and docs can show
behavior for a specific release, not just "latest."

---

## 10. Decisions (resolved)

1. **Sync model** — ✅ **bidirectional**, via a lockfile-based 3-way merge (§2). Any file
   may be edited; edits propagate; divergent edits to the same block are conflicts resolved
   by configurable business logic (§2.1).
2. **Language** — ✅ **Go**. Single static binary, instant startup, great CI ergonomics.
3. **Interchange format** — `AGENTS.md` conventions inside the IR (widest native support);
   no on-disk file is privileged.
4. **Config format** — `TOML` (`agentsync.toml`).
