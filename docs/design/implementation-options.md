# Implementation Options (Go)

A review of the current design and a survey of concrete coding approaches for each
component, with a recommended stack. Language is settled (Go); this is about *how* to build
it inside Go.

*Scope: target the latest version of each harness as of today (§9 of the design). Keep the
data structures version-capable, but ship one version's data.*

---

## 1. Review of what we have

**Solid and stable — safe to build on:**

- **The two-doc split** — [file-location research](../research/context-file-equivalencies.md)
  and [in-file functionality map](../research/functionality-map.md) — cleanly separates
  "where files live" from "what markup does," and the functionality map's C1–C10 taxonomy is
  a good spine.
- **Bidirectional 3-way merge via a lockfile base** ([design §2](tool-design.md)) is
  internally consistent and keeps determinism.
- **Adapter-capability table as the single source** for both conversion and linting ([§3.2],
  [§6]) avoids drift between "what we do" and "what we warn about."
- **Fixtures = tests = docs** (§7) and **`agentsync context`** as the validation oracle (§8)
  give us a checkable definition of correctness.

**Thin or unresolved — decide before/while coding (see §11):**

1. **Parsing model vs byte-exact output.** Golden-file tests demand byte-exact writes, which
   argues *against* a semantic Markdown AST (they don't round-trip). Needs an explicit
   segment-preserving approach.
2. **Block granularity mismatch.** Our merge keys blocks by heading path, but **Cursor's unit
   is a file-with-frontmatter, not a heading.** One `AGENTS.md` with five `##` sections may
   map to five `.mdc` files. The block↔file mapping is not 1:1 and needs a rule.
3. **Flatten-on-read vs preserve-authoring-structure.** The functionality map says "inline
   every import on read" (C1), but the merge must write files back *without* destroying a
   maintainer's deliberate `@import` split. Reconcile "flatten to IR" with "preserve
   structure on write."
4. **Heading-key edge cases.** Preamble before the first heading, duplicate sibling headings,
   non-heading-structured files — the key scheme must handle these deterministically.

None of these block starting; they shape the IR and the Markdown layer, so they're called
out in §11.

---

## 2. Markdown parsing & block segmentation — the crux

We do **not** need a full semantic AST (we're not rendering HTML). We need: split into
heading-delimited blocks, read/write YAML frontmatter, find block-level HTML comments, scan
for `@tokens` while skipping code fences and spans, and **write back byte-exactly**.

| Option | What it gives | Cost |
| --- | --- | --- |
| **`goldmark`** (yuin) | Full CommonMark AST, extensible, most-used Go MD lib. | AST→Markdown is lossy; would need a custom renderer for byte-exact output. Overkill for our needs. |
| **`goldmark` for *locating* only** | Use it to find heading/fence offsets; keep raw byte slices for content. | Pulls a big dep to use 10% of it. |
| **Purpose-built structural scanner** ✅ | A line-oriented pass that is fence-aware and frontmatter-aware, emitting `(heading-path, raw-text-span)` segments. Content is never reparsed — raw bytes are preserved. | We write it (~a few hundred lines), but it's exactly our semantics and trivially byte-exact. |

**Recommendation: purpose-built scanner** that treats a file as a sequence of raw segments
keyed by heading path, with frontmatter peeled off the top. It preserves content verbatim
(determinism for free) and encodes precisely the constructs we care about (fences, comments,
`@tokens`). Reach for `goldmark` only if a future feature needs real inline semantics.

This directly resolves review-gap #1.

---

## 3. Supporting libraries

| Concern | Options | Recommendation |
| --- | --- | --- |
| **YAML frontmatter** | `gopkg.in/yaml.v3` | ✅ `yaml.v3` — standard, handles the 3 fields we need. |
| **TOML config** | `pelletier/go-toml/v2`, `BurntSushi/toml` | ✅ `go-toml/v2` — modern, fast, good decode errors. |
| **Glob matching** (C2 `paths`/`globs`) | stdlib `path.Match` (no `**`), `bmatcuk/doublestar`, `gobwas/glob` | ✅ `doublestar` — supports `**`, matches how Claude/Cursor globs behave. |
| **Version ranges** (§9, future) | `Masterminds/semver/v3` | ✅ `semver/v3` — ranges/constraints; used lightly now (everything = latest). |
| **Diff (for `--check` / test output)** | `google/go-cmp`, `sergi/go-diff`, `hexops/gotextdiff` | ✅ `go-cmp` in tests; `gotextdiff` for human-readable unified diffs in CLI output. |

Keep the dependency set small and boring — every dep is CI surface and a determinism risk.

---

## 4. CLI framework

| Option | Style | Fit |
| --- | --- | --- |
| stdlib `flag` + manual dispatch | Zero deps, verbose subcommand wiring. | Fine but you rebuild help/usage. |
| **`spf13/cobra`** | De-facto standard; `sync`/`lint`/`context` as commands; rich help, completion. | ✅ Ubiquitous, well understood. Slightly heavy. |
| **`alecthomas/kong`** | Struct-tag driven; very little boilerplate; clean. | ✅ Elegant, lighter than cobra. |
| `urfave/cli` | Middle ground. | Fine, less momentum than cobra. |

**Recommendation: `cobra`** for ecosystem familiarity and first-class subcommands/flags, or
`kong` if we prefer minimalism. Either is a fine single-binary CI citizen. Leaning `cobra`.

---

## 5. IR & adapter interfaces

The shape that makes adapters pluggable (review-gap #2 and #3 live here). Sketch:

```go
// The version-aware capability descriptor (latest-only data for now).
type Capability struct {
    Includes   IncludeMode // None | InlineExpand | Reference | ExternalList
    GlobScope  ScopeMode   // None | Frontmatter | DirectoryNesting
    Activation bool        // Cursor-style modes supported?
    Frontmatter bool
    StripsComments bool     // Claude
    MaxBytes   int          // 0 = unbounded; Codex = 32*1024
    // ...one row per C1–C10, resolved for the targeted version
}

// A semantic unit after normalization.
type Block struct {
    Key        string      // heading path, e.g. "Testing/Unit"
    Scope      Scope       // Global | Glob(pat) | Dir(path)
    Activation Activation  // Always | AutoAttached | AgentRequested | Manual
    Layer      Layer       // Managed | User | Project | Local
    Body       string      // raw markdown, verbatim
    Includes   []Include   // resolved provenance so we can re-externalize on write
    Origin     Provenance  // source file + span (round-trip + diagnostics)
}

type Document struct { Blocks []Block }

type Reader interface { // provider files -> IR
    Detect(root string) ([]string, error)        // which files this provider owns
    Read(files []string) (Document, []Diagnostic, error)
    Capability(v Version) Capability
}
type Writer interface { // IR -> provider files (byte-exact)
    Write(doc Document, root string) ([]File, []Diagnostic, error)
}
```

Adapters register into a map keyed by provider id. **Resolves gap #3:** `Block.Includes`
carries provenance so a Writer can *re-externalize* (reconstruct the `@import` split) instead
of emitting one flattened blob — flatten-for-merge, restore-on-write.

**Resolves gap #2 (Cursor granularity):** the Cursor writer owns the block→file policy — one
`.mdc` per top-level heading (or per `Scope`), named from the key. The IR stays
heading-block-centric; each adapter decides how blocks land in its native file layout.

---

## 6. Merge engine

| Option | Behavior | Fit |
| --- | --- | --- |
| **Block-level 3-way** ✅ | Key by heading path; compare each side to the lockfile base; propagate the single changed side; conflict when 2+ diverge. Matches design §2. | Simple, deterministic, matches the model. |
| Line-level diff3 (`git merge-file`-style) | Merge within text. | More granular but few solid Go libs; overkill for latest-only start. |
| Hybrid | Block identity + line diff inside a block. | Later enhancement if block-level conflicts feel too coarse. |

**Recommendation: block-level 3-way now.** Within a block, treat differing content as a
whole-block change; only escalate to intra-block text merge if real usage demands it.

---

## 7. Determinism & serialization

- One canonical writer per provider: **LF newlines, trailing-newline normalization, sorted
  glob lists, stable frontmatter key order**, no timestamps/paths/RNG.
- A single `serialize(Document) -> bytes` path shared by `sync` (write) and `--check`
  (compare), so the check can never disagree with the write.
- Enforced by golden tests (§9 below).

---

## 8. E2E harness (Phase 2) — language-agnostic by construction

Requirement: the harness drives the **built binary** as a black box and doesn't care what
it's written in.

| Option | Notes |
| --- | --- |
| **Exec-the-binary runner over `spec/*/` trees** ✅ | Copies `input/` to a temp dir, runs `agentsync …`, compares the result tree to `expected/` and stdout/exit to `expected-report.txt`. The runner can be written in Go *today* but only ever talks to the CLI via subprocess — swappable later. |
| `rogpeppe/go-testscript` (txtar) | Great for CLI tests, but Go-coupled and single-file txtar fixtures fight our multi-file trees. |
| Bats / shell | Truly language-neutral, but weak tree-diffing and Windows story. |

**Recommendation: a small exec-based runner** (Go now) that treats the binary as opaque —
honors the "doesn't care what the CLI is written in" constraint while staying easy to run in
CI. Fixtures carry `input/` (working files + `agentsync.lock` base), `expected/`,
`expected-report.txt`, and `intent.md`.

---

## 9. Docs generation (Phase 3)

A generator walks `spec/`, and for each fixture renders `intent.md` + a rendered
`input → expected` diff (+ the `context --diff` view where relevant) into static Markdown/HTML
for the site. Pure function of the fixtures → the site can't document unheld behavior.
Library-light: Go `text/template` + a Markdown→HTML pass (`goldmark`, used here where HTML
rendering *is* the goal). Site can be any static host.

---

## 10. Recommended stack & layout

| Layer | Choice |
| --- | --- |
| CLI | `spf13/cobra` |
| Markdown | purpose-built scanner (+ `goldmark` only in docs-gen) |
| Frontmatter | `yaml.v3` |
| Config | `pelletier/go-toml/v2` |
| Globs | `bmatcuk/doublestar` |
| Versions | `Masterminds/semver/v3` (latest-only for now) |
| Diff | `go-cmp` (tests), `gotextdiff` (CLI) |
| E2E | exec-based fixture runner |

```
agentsync/
  cmd/agentsync/        # main, cobra commands: sync, lint, context
  internal/
    markdown/           # scanner: bytes <-> heading-keyed segments + frontmatter
    ir/                 # Block, Document, Scope, Activation, Layer
    adapter/            # Reader/Writer + registry; capability table
      claudecode/  codex/  cursor/  opencode/
    merge/              # 3-way block merge + lockfile
    lint/               # rule engine (quality + compatibility)
    contextcmd/         # effective-context assembly per harness
    config/             # agentsync.toml
  spec/                 # fixtures (tests + docs source)
  test/e2e/             # exec-based runner
  docs/                 # (this)
```

---

## 11. Open modeling questions to settle (from the review)

1. **Block↔file granularity for Cursor** — confirm "one `.mdc` per top-level heading" is the
   right default mapping, vs per-`Scope` or per-`H2`.
2. **Structure preservation** — how hard do we try to preserve a maintainer's authored
   `@import`/file split on write, vs emit normalized output? (Provenance in `Block.Includes`
   enables it; question is the default.)
3. **Heading-key normalization** — rule for preamble (content before first heading),
   duplicate sibling headings (append an index?), and files with no headings (single
   implicit block?).
4. **First-run base** — with no lockfile and files that already disagree, confirm the "treat
   union as base, surface initial conflict once" behavior (design §2 note).

These four decide the exact IR/Markdown-layer contracts; worth locking before writing the
adapters.
