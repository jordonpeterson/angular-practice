# Implementation Options (Go)

Survey of coding approaches per component, with a recommended stack. Language is settled (Go); this is about *how* to build it in Go.

*Scope: target the latest version of each harness (§9 of the design). Keep data structures version-capable, but ship one version's data.*

---

## 1. Review of what we have

**Solid — safe to build on:**

- **Two-doc split** — [file-location research](../research/context-file-equivalencies.md) and [in-file functionality map](../research/functionality-map.md) — separates "where files live" from "what markup does"; the C1–C10 taxonomy is a good spine.
- **Bidirectional 3-way merge via lockfile base** ([design §2](tool-design.md)) — consistent, deterministic.
- **Adapter-capability table as single source** for conversion and linting ([§3.2], [§6]) — no drift.
- **Fixtures = tests = docs** (§7) and **`agentsync context`** oracle (§8) — checkable correctness.

**Thin/unresolved — decide before/while coding (see §11):**

1. **Parsing model vs byte-exact output.** Golden-file tests demand byte-exact writes, arguing against a semantic Markdown AST (no round-trip). Needs a segment-preserving approach.
2. **Block granularity mismatch.** Merge keys blocks by heading path, but **Cursor's unit is a file-with-frontmatter, not a heading.** One `AGENTS.md` with five `##` sections may map to five `.mdc` files — mapping is not 1:1.
3. **Flatten-on-read vs preserve-authoring-structure.** Map says "inline every import on read" (C1), but merge must write back without destroying a deliberate `@import` split.
4. **Heading-key edge cases.** Preamble before first heading, duplicate sibling headings, non-heading-structured files — key scheme must handle these deterministically.

None block starting; they shape the IR and Markdown layer (§11).

---

## 2. Markdown parsing & block segmentation — the crux

No full semantic AST needed (not rendering HTML). We need: split into heading-delimited blocks, read/write YAML frontmatter, find block-level HTML comments, scan for `@tokens` (skipping code fences/spans), and **write back byte-exactly**.

| Option | What it gives | Cost |
| --- | --- | --- |
| **`goldmark`** (yuin) | Full CommonMark AST, extensible, most-used Go MD lib. | AST→Markdown lossy; needs custom renderer for byte-exact. Overkill. |
| **`goldmark` for *locating* only** | Find heading/fence offsets; keep raw byte slices. | Big dep used at 10%. |
| **Purpose-built structural scanner** ✅ | Line-oriented, fence- and frontmatter-aware; emits `(heading-path, raw-text-span)` segments. Content never reparsed — raw bytes preserved. | We write it (~few hundred lines), but exact semantics, trivially byte-exact. |

**Recommendation: purpose-built scanner** — file as raw segments keyed by heading path, frontmatter peeled off top. Verbatim content (determinism for free), encodes the constructs we care about (fences, comments, `@tokens`). Reach for `goldmark` only if a future feature needs real inline semantics. Resolves review-gap #1.

---

## 3. Supporting libraries

| Concern | Options | Recommendation |
| --- | --- | --- |
| **YAML frontmatter** | `gopkg.in/yaml.v3` | ✅ `yaml.v3` — standard, handles the 3 fields. |
| **TOML config** | `pelletier/go-toml/v2`, `BurntSushi/toml` | ✅ `go-toml/v2` — modern, fast, good errors. |
| **Glob matching** (C2 `paths`/`globs`) | stdlib `path.Match` (no `**`), `bmatcuk/doublestar`, `gobwas/glob` | ✅ `doublestar` — `**` support, matches Claude/Cursor behavior. |
| **Version ranges** (§9, future) | `Masterminds/semver/v3` | ✅ `semver/v3` — ranges/constraints; light use now (all = latest). |
| **Diff** (`--check` / test output) | `google/go-cmp`, `sergi/go-diff`, `hexops/gotextdiff` | ✅ `go-cmp` in tests; `gotextdiff` for CLI unified diffs. |

Keep deps small and boring — every dep is CI surface and determinism risk.

---

## 4. CLI framework

| Option | Style | Fit |
| --- | --- | --- |
| stdlib `flag` + manual dispatch | Zero deps, verbose wiring. | Fine, but rebuild help/usage. |
| **`spf13/cobra`** | De-facto standard; `sync`/`lint`/`context` commands; rich help, completion. | ✅ Ubiquitous. Slightly heavy. |
| **`alecthomas/kong`** | Struct-tag driven; minimal boilerplate. | ✅ Elegant, lighter than cobra. |
| `urfave/cli` | Middle ground. | Fine, less momentum. |

**Recommendation: `cobra`** for ecosystem familiarity and first-class subcommands/flags, or `kong` for minimalism. Leaning `cobra`.

---

## 5. IR & adapter interfaces

Shape that makes adapters pluggable (gaps #2, #3 live here):

```go
// Version-aware capability descriptor (latest-only data for now).
type Capability struct {
    Includes   IncludeMode // None | InlineExpand | Reference | ExternalList
    GlobScope  ScopeMode   // None | Frontmatter | DirectoryNesting
    Activation bool        // Cursor-style modes?
    Frontmatter bool
    StripsComments bool     // Claude
    MaxBytes   int          // 0 = unbounded; Codex = 32*1024
    // ...one row per C1–C10, resolved for targeted version
}

// A semantic unit after normalization.
type Block struct {
    Key        string      // heading path, e.g. "Testing/Unit"
    Scope      Scope       // Global | Glob(pat) | Dir(path)
    Activation Activation  // Always | AutoAttached | AgentRequested | Manual
    Layer      Layer       // Managed | User | Project | Local
    Body       string      // raw markdown, verbatim
    Includes   []Include   // provenance to re-externalize on write
    Origin     Provenance  // source file + span
}

type Document struct { Blocks []Block }

type Reader interface { // provider files -> IR
    Detect(root string) ([]string, error)
    Read(files []string) (Document, []Diagnostic, error)
    Capability(v Version) Capability
}
type Writer interface { // IR -> provider files (byte-exact)
    Write(doc Document, root string) ([]File, []Diagnostic, error)
}
```

Adapters register into a map keyed by provider id.

- **Resolves #3:** `Block.Includes` carries provenance so a Writer can *re-externalize* the `@import` split instead of emitting a flattened blob — flatten-for-merge, restore-on-write.
- **Resolves #2 (Cursor granularity):** Cursor writer owns block→file policy — one `.mdc` per top-level heading (or per `Scope`), named from the key. IR stays heading-block-centric; each adapter decides native file layout.

---

## 6. Merge engine

| Option | Behavior | Fit |
| --- | --- | --- |
| **Block-level 3-way** ✅ | Key by heading path; compare each side to lockfile base; propagate the single changed side; conflict when 2+ diverge. Matches §2. | Simple, deterministic. |
| Line-level diff3 (`git merge-file`-style) | Merge within text. | More granular but few solid Go libs; overkill for latest-only start. |
| Hybrid | Block identity + line diff inside a block. | Later enhancement if block-level feels coarse. |

**Recommendation: block-level 3-way now.** Treat differing content as a whole-block change; escalate to intra-block merge only if usage demands it.

---

## 7. Determinism & serialization

- One canonical writer per provider: **LF newlines, trailing-newline normalization, sorted glob lists, stable frontmatter key order**, no timestamps/paths/RNG.
- Single `serialize(Document) -> bytes` shared by `sync` (write) and `--check` (compare) — check can't disagree with write.
- Enforced by golden tests (§9).

---

## 8. E2E harness (Phase 2) — language-agnostic by construction

Requirement: harness drives the **built binary** as a black box.

| Option | Notes |
| --- | --- |
| **Exec-the-binary runner over `spec/*/` trees** ✅ | Copies `base/` to temp dir, runs `sync` to generate the lock, overlays `edit/`, runs the command, compares result tree to `expected/` and report/exit to `report.json`. Talks to the CLI only via subprocess — swappable. |
| `rogpeppe/go-testscript` (txtar) | Great for CLI tests, but Go-coupled; single-file txtar fights multi-file trees. |
| Bats / shell | Language-neutral, but weak tree-diffing and Windows story. |

**Recommendation: small exec-based runner** (Go now) treating the binary as opaque. Fixtures carry `base/` (Given), `edit/` (When), `expected/` + `report.json` (Then), `intent.md` — no hand-authored lockfiles (hashes would rot; the runner generates the lock by syncing `base/`). Hermetic: never invokes real agent CLIs (that's the separate conformance probe, design §9.4).

---

## 9. Docs generation (Phase 3)

Generator walks `spec/`, renders per fixture `intent.md` + `input → expected` diff (+ `context --diff` where relevant) into static Markdown/HTML. Pure function of fixtures → can't document unheld behavior. Library-light: `text/template` + Markdown→HTML (`goldmark`, used here where HTML rendering *is* the goal). Any static host.

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
  cmd/agentsync/        # main; commands: sync, lint, context
  internal/
    markdown/           # scanner: bytes <-> heading-keyed segments + frontmatter
    graph/              # Block, CompiledPart, typed edges, loss labels (design §3.1a)
    adapter/            # per-harness generator (files -> graph) + emitter (graph -> files)
      claudecode/  codex/  cursor/  opencode/
    merge/              # equivalence engine: 3-way decisions + lockfile + verified writes
    lint/               # rule registry (quality + compatibility severities)
    config/             # agentsync.toml
  spec/                 # fixtures (tests + docs source)
  test/e2e/             # exec-based runner (implementation-blind)
  docs/                 # research + design
```

*(The `context` command needs no package of its own — it prints the generators' compiled
projection.)*

---

## 11. Open modeling questions to settle (from the review)

1. **Block↔file granularity for Cursor** — confirm "one `.mdc` per top-level heading" default vs per-`Scope` or per-`H2`.
2. **Structure preservation** — how hard to preserve a maintainer's authored `@import`/file split on write vs emit normalized output? (Provenance in `Block.Includes` enables it; question is the default.)
3. **Heading-key normalization** — rule for preamble (content before first heading), duplicate sibling headings (append index?), and files with no headings (single implicit block?).
4. **First-run base** — with no lockfile and files already disagreeing, confirm "treat union as base, surface initial conflict once" (design §2 note).

These four decide the exact IR/Markdown-layer contracts; lock before writing adapters.
