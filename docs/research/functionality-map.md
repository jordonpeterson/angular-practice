# In-File Functionality Map

How to convert the **syntax and behavior *inside* context files** between Claude Code,
OpenAI Codex, Cursor, and OpenCode.

*This is Phase 1 of the plan and the most important document in the project. The
[file-location research](context-file-equivalencies.md) answered "where do the files
live." This answers the harder question: "a maintainer wrote some markup **inside** a file
— what does it **do**, and what is the equivalent markup in every other harness?"*

*Compiled July 2026. Uncertain items are tagged **[verify]** and listed in §8.*

---

## 0. Why this is separate from file-location mapping

Two files can sit in equivalent locations and still behave completely differently because
of what's written inside them:

- `@./style.md` in `CLAUDE.md` **inlines** that file's text into context.
- `@style.md` in a Cursor `.mdc` **attaches** the file as a reference.
- The same string in an `AGENTS.md` that Codex reads is **literal text** — Codex has no
  import mechanism, so the agent just sees the characters `@style.md`.

Same syntax, three different behaviors. The tool cannot copy bytes between files; it must
**understand the behavior and re-express it**. This document maps every such behavior.

---

## 1. Taxonomy of in-file capabilities

Everything a context file can express falls into these categories. Each is mapped in §2.

| # | Capability | The question it answers |
| --- | --- | --- |
| C1 | **Includes / imports** | "Pull in another file's content." |
| C2 | **Path / glob scoping** | "Apply this only to matching files." |
| C3 | **Activation mode** | "*When* should this guidance be active?" |
| C4 | **Frontmatter / metadata** | Structured fields attached to a rule. |
| C5 | **Hidden / maintainer comments** | Text for humans the agent must *not* see. |
| C6 | **Escaping / literal vs active** | "Treat `@x` as text, not as a directive." |
| C7 | **Directory-scoped overrides** | Nested files that override/extend parents. |
| C8 | **Layer precedence** | managed / user / project / local ordering. |
| C9 | **Size / truncation** | Hard byte caps that silently drop content. |
| C10 | **Remote / URL includes** | Pull content from the network. |

Adjacent surfaces (MCP servers, slash commands, subagents, skills) are **out of scope for
v1** — they're separate config files, not in-context-file syntax. Listed in §9 as future
work.

---

## 2. Per-capability mapping

Each subsection gives: the syntax per harness, the behavioral differences, the conversion
rule, and the fidelity class (§4). Every conversion row is a future test fixture (§7).

### C1 — Includes / imports

| Harness | Syntax | Semantics |
| --- | --- | --- |
| **Claude Code** | `@path/to/file.md` (inline in body) | **Inline expansion.** File text is spliced into context at load. Max 4 hops. Relative to the importing file. |
| **Codex** | *(none)* | No include mechanism. Only whole-file concatenation across directories. |
| **Cursor** | `@file.md` inside a `.mdc`; `@RuleName` to pull another rule | **Attach by reference.** Points Cursor at the file/rule; not a literal text splice. |
| **OpenCode** | `opencode.json` → `"instructions": [...]` (globs, paths) | **External include list**, not in-file. Combined with `AGENTS.md`. In-file `@import` **[verify]** — not a documented feature. |

**The normalization insight:** these are four different operations (splice / none / attach /
external-list). The **lossless common denominator is inline expansion**. So:

- **On read:** resolve *every* include to its literal text and flatten it into the IR
  block. The IR holds no unresolved includes. (This also resolves Claude's 4-hop chains and
  Cursor `@Rule` references.)
- **On write:** emit literal text by default. Only re-externalize into a native include if
  (a) the target supports it losslessly and (b) config opts in (e.g. keep `opencode.json`
  instructions, or keep Claude `@imports` for humans' benefit).

**Conversions**

| From → To | Rule | Fidelity |
| --- | --- | --- |
| Claude `@import` → Codex | Inline the imported text (Codex can't reference). | Lossless (content); loses the modular split. |
| Claude `@import` → Cursor | Inline, or rewrite to `@file` if it's a real file. | Lossless |
| Cursor `@file` → Claude/Codex | Inline the referenced file's content. | Lossless if file is local + readable; else **drop + diagnostic**. |
| OpenCode `instructions` glob → all | Expand each matched file inline. | Lossless |
| any → OpenCode | Inline into `AGENTS.md`, or emit `opencode.json instructions`. | Lossless |

### C2 — Path / glob scoping

| Harness | Syntax | Notes |
| --- | --- | --- |
| **Claude Code** | `.claude/rules/*.md` with `paths:` frontmatter (glob list) | Full glob. Rule loads only when a matching file is touched. |
| **Codex** | *(directory nesting only)* | To scope to `src/api`, put an `AGENTS.md` **in** `src/api/`. No glob expression exists. |
| **Cursor** | `.mdc` `globs:` frontmatter (comma-separated) | Full glob. Drives "Auto Attached" activation. |
| **OpenCode** | `opencode.json instructions` glob (matches *instruction* files, not scope) | No per-rule "apply to these files" scoping like the others. **[verify]** |

**The hard asymmetry:** Cursor `globs` ⇄ Claude `paths` is a **clean frontmatter rename**.
But **Codex has no glob** — the *only* way to scope in Codex is to physically place a file
in a directory. So:

- A **directory-prefix glob** (`src/api/**`) → Codex nested `AGENTS.md` in `src/api/`.
  Lossy-recoverable.
- A **non-directory glob** (`**/*.test.ts`, `src/**/*.{ts,tsx}`) → **no faithful Codex
  form.** Options: attach to the nearest common ancestor directory (over-broad) or drop.
  Either way → **diagnostic**. This is rule `portable-globs-only`.

**Conversions**

| From → To | Rule | Fidelity |
| --- | --- | --- |
| Cursor `globs` ⇄ Claude `paths` | Rename frontmatter key; keep patterns. | Lossless |
| glob → Codex (directory-prefix) | Lower to nested `AGENTS.md` at that directory. | Lossy-recoverable |
| glob → Codex (non-prefix) | Attach to common-ancestor dir + warn, or drop + warn. | Lossy-degrading |
| Codex nested file → glob harness | Synthesize `globs/paths: <dir>/**`. | Lossless |

### C3 — Activation mode

Cursor is the only harness with an activation concept; this is its defining feature and the
single biggest source of unrepresentable behavior.

| Cursor mode | Frontmatter | Behavior | Equivalent elsewhere |
| --- | --- | --- | --- |
| **Always** | `alwaysApply: true` | In every request. | ✅ Everyone's default (plain body content). |
| **Auto Attached** | `globs: <pat>` | When a matching file is in context. | ✅ Claude `paths` rule; ⚠️ Codex dir-nesting. |
| **Agent Requested** | `description:` set, no globs | Model *chooses* whether to pull it in. | ❌ No equivalent anywhere. |
| **Manual** | none | Only on explicit `@Rule`. | ❌ No equivalent (closest: Claude skills / slash commands — out of scope). |

**Conversions**

| From → To | Rule | Fidelity |
| --- | --- | --- |
| Always / Auto-Attached → any | Map to body / `paths` / nesting per C2. | Lossless / recoverable |
| Agent-Requested → non-Cursor | Force-include (changes semantics to "always") **or** drop. Config picks; either way warn. | Lossy-degrading |
| Manual → non-Cursor | Drop + diagnostic (no on-demand concept). | Unrepresentable |
| any body content → Cursor | Emit as `alwaysApply: true`. | Lossless |

### C4 — Frontmatter / metadata

| Harness | Frontmatter? | Fields |
| --- | --- | --- |
| **Claude Code** | Yes, in `.claude/rules/*.md` | `paths` |
| **Cursor** | Yes, in `.mdc` | `description`, `globs`, `alwaysApply` |
| **Codex** | No | — |
| **OpenCode** | No (config lives in `opencode.json`) | — |

`description` (Cursor) has no consumer elsewhere; preserve it as an HTML comment or drop it
(config). `paths`↔`globs` handled in C2. Converting **to** Codex/OpenCode means frontmatter
is **stripped** and its meaning re-expressed structurally (nesting / opencode.json) or lost.

### C5 — Hidden / maintainer comments

**A genuine behavioral trap.** Claude Code **strips block-level `<!-- ... -->` HTML
comments** before sending the file to the model. No other harness documents this — Codex,
Cursor, and OpenCode pass the raw markdown, so the agent **sees** the comment text.

| Direction | Consequence | Rule |
| --- | --- | --- |
| Claude → Codex/Cursor/OpenCode | A comment invisible to Claude becomes **visible** to the other agent. | **Drop** block HTML comments so they stay invisible everywhere. |
| Codex/etc. → Claude | A comment that was working content becomes **invisible** (stripped by Claude). | Warn; optionally convert to visible text. |

This means "copy the file verbatim" is **wrong** for any file containing comments — the tool
must actively strip/relocate them.

### C6 — Escaping / literal vs active

Claude treats a bare `@path` as an **import** but a backtick-wrapped `` `@path` `` as
**literal**. Import parsing also skips fenced code blocks. Other harnesses have no import,
so `@path` is always literal there.

| Direction | Trap | Rule |
| --- | --- | --- |
| **any → Claude** | A literal `@foo` in the source (e.g. an email handle, a decorator, `@types/node`) would be **interpreted as an import** by Claude. | **Backtick-escape** every `@token` that isn't a deliberate import before writing a Claude file. |
| Claude → any | A deliberate `@import` must be inlined (C1); a `` `@literal` `` stays literal. | Distinguish the two by Claude's own rules (backticks / code fences). |

Getting C6 wrong silently corrupts output (phantom imports or lost text), so it's a
high-priority fixture set.

### C7 — Directory-scoped overrides (nesting semantics)

All four support nested files, but the *merge behavior* differs:

| Harness | Nested-file behavior |
| --- | --- |
| **Claude Code** | Concatenate; nested file loads **on demand** when a file in that dir is read. |
| **Codex** | One file per directory (`AGENTS.override.md` > `AGENTS.md` > fallbacks); root→cwd, **closer overrides**. |
| **Cursor** | Nearest `AGENTS.md` / applicable `.mdc` wins for that subtree. **[verify]** nested `.cursor/rules/` subdirs are unreliable — keep `.mdc` flat. |
| **OpenCode** | Walk up to git root; first match wins per category. |

For the merge engine (design §2) this matters because "the `## Build` block for `src/api/`"
may live in a *different physical file* per harness. The IR keys a block by
`(scope, heading-path)` so scope-plus-heading identity survives these layout differences.

### C8 — Layer precedence

| Layer | Claude Code | Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- |
| Managed/org | `/etc/claude-code/CLAUDE.md`, `managed-settings.json` | — | Team Rules (dashboard) | — |
| User/global | `~/.claude/CLAUDE.md`, `~/.claude/rules/` | `~/.codex/AGENTS.md` | User Rules (UI) | `~/.config/opencode/AGENTS.md` |
| Project | `CLAUDE.md`, `.claude/rules/` | `AGENTS.md` | `.cursor/rules/`, `AGENTS.md` | `AGENTS.md` |
| Local | `CLAUDE.local.md` | `AGENTS.override.md` | *(none in-repo)* | *(none in-repo)* |

The tool syncs the **project layer** by default (the only layer that's committed and
team-shared everywhere). User/managed/local layers are per-machine and are **not synced**
unless explicitly configured — they're where two harnesses legitimately *should* differ.

### C9 — Size / truncation

Only **Codex** has a hard cap: `project_doc_max_bytes` (~32 KiB default **[verify]**;
some builds report 64 KiB). It **silently stops** adding files once the cap is hit. A large
`CLAUDE.md` that converts fine byte-wise can be **truncated at load** by Codex. The tool
measures the flattened per-directory total and raises `size-within-codex-cap` before Codex
would drop content, suggesting a split into nested files.

### C10 — Remote / URL includes

Only **OpenCode** supports remote instruction URLs (`instructions: ["https://..."]`, 5 s
timeout). Remote content is **non-deterministic** (it can change between runs), which breaks
the merge/`--check` guarantees. Rule `no-remote-instructions`: in `strict`/CI, reject; else
snapshot+pin the fetched content into the lockfile so a run is reproducible from committed
state. Converting to other harnesses: fetch once and inline.

---

## 3. Conversion primitives

Every conversion in §2 decomposes into a small set of reusable operations. The engine
implements these once; each capability conversion is a composition.

| Primitive | What it does | Used by |
| --- | --- | --- |
| **inline-expand** | Replace an include with the referenced file's literal text. | C1, C10 |
| **externalize** | Extract a block into a separate file + native include. | C1 (opt-in) |
| **hoist-to-frontmatter** | Express scope as `globs:`/`paths:` YAML. | C2 |
| **lower-to-nesting** | Move a scoped block into a nested per-directory file. | C2, C7 |
| **rename-key** | `globs` ⇄ `paths`, etc. | C2, C4 |
| **strip-comments** | Remove block HTML comments. | C5 |
| **escape-tokens** | Backtick `@tokens` that aren't deliberate imports. | C6 |
| **drop-with-diagnostic** | Remove unrepresentable content; emit a warning/error. | C2, C3, C4 |
| **coerce-activation** | Force-include or drop Cursor-only modes. | C3 |
| **measure-and-warn** | Check byte budget; warn before silent truncation. | C9 |

---

## 4. Fidelity classification

Every conversion is tagged so the tool (and the user) knows what to expect:

- **Lossless** — round-trips exactly. (Body text; `globs`↔`paths`.)
- **Lossy-recoverable** — reshaped but semantically equal; can be reconstructed.
  (Directory-prefix glob ↔ nested file.)
- **Lossy-degrading** — meaning changes for the worse but content survives. (Agent-Requested
  forced to always; non-prefix glob attached over-broadly.)
- **Unrepresentable** — no equivalent; dropped with a diagnostic. (Cursor Manual mode;
  Claude's comment-stripping semantics into a non-stripping harness.)

The `compatibility` config (`off | portable | strict`) sets how the tool reacts to anything
worse than Lossless: `portable` warns, `strict` errors, keeping a repo to the
lowest-common-denominator feature set.

---

## 5. Master support matrix

Native support for each capability, per harness. `✅` native · `⚠️` partial/structural ·
`❌` none · `→cfg` via sidecar config.

| Capability | Claude Code | Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- |
| C1 Includes | ✅ inline `@` | ❌ | ✅ ref `@` | →cfg `instructions` |
| C2 Glob scoping | ✅ `paths` | ⚠️ dir-nesting | ✅ `globs` | ⚠️ →cfg |
| C3 Activation modes | ⚠️ path-only | ❌ | ✅ 4 modes | ❌ |
| C4 Frontmatter | ✅ `paths` | ❌ | ✅ 3 fields | ❌ |
| C5 Hidden comments | ✅ strips `<!-- -->` | ❌ literal | ❌ literal | ❌ literal |
| C6 `@` escaping | ✅ backtick/fence | n/a (literal) | ⚠️ | n/a |
| C7 Nested overrides | ✅ concat | ✅ override | ⚠️ nearest | ✅ first-match |
| C8 Layers | ✅ 4 | ⚠️ 2–3 | ✅ 3 | ⚠️ 2 |
| C9 Size cap | ❌ (unbounded) | ✅ hard cap | ❌ | ❌ |
| C10 Remote includes | ❌ | ❌ | ⚠️ `@Docs`/`@Web` (chat) | ✅ URL instructions |

**Reading the matrix as a difficulty gradient:** Cursor is the feature superset (activation
modes, references). Codex is the feature floor (no includes, no globs, no frontmatter, hard
size cap). **The two hardest conversion directions are therefore anything → Codex (must
flatten and may truncate) and Cursor-only features → anyone (must drop).** Claude Code and
OpenCode sit in the middle and interconvert cleanly.

---

## 6. Canonical (interchange) feature set

The IR is the union superset; a block can carry: literal content, scope (glob), activation,
layer, provenance, and resolved (inlined) includes. When lowering to a harness that lacks a
feature, the engine applies the §3 primitives and records a fidelity note. This keeps the
"understand deep intricacies" logic in **one** place (the lowering table), which the linter
and the converter both read — so what the tool *warns* about and what it *does* can never
disagree.

---

## 7. How this map becomes tests (Phase 2) and docs (Phase 3)

Each conversion row in §2 is a **test-case seed**. The e2e harness (Phase 2) turns each into
a fixture directory that drives the CLI as a black box (language-agnostic):

```
spec/
  c1-claude-import-to-codex/       # one capability × one direction
    intent.md                      # "Claude @import inlines into Codex; content preserved"
    input/    { CLAUDE.md with @import, imported file }
    expected/ { AGENTS.md with inlined text }
    expected-report.txt            # fidelity: lossless
  c6-literal-at-token-to-claude/   # the @-escaping trap
  c5-comment-stripping-to-agents/  # the hidden-comment trap
  ...
```

Naming: `<capability>-<from>-<to>-<case>`. The taxonomy (C1–C10) × harness pairs enumerates
the full matrix, so **coverage is countable** — we know exactly which conversions have a
fixture and which don't.

Phase 3 renders every `intent.md` + `input → expected` diff into the docs site. Because the
docs are generated from the fixtures, **the website can never document behavior the tool
doesn't actually have** — it's all the same source.

---

## 8. Verify before shipping

- **[C1/OpenCode]** In-file `@import` inside `AGENTS.md` for OpenCode — confirm whether it's
  supported or whether `opencode.json instructions` is the only include path.
- **[C2/OpenCode]** Whether OpenCode has any per-rule "apply to these files" scoping beyond
  instruction-file globbing.
- **[C7/Cursor]** Reliability of nested `.cursor/rules/` subdirectories (community reports
  say flat-only); confirm current behavior.
- **[C9/Codex]** `project_doc_max_bytes` default — 32 KiB vs 64 KiB across versions.
- **[C3/Cursor]** Exact trigger semantics of Agent-Requested (how the `description` is used).

## 9. Adjacent surfaces (future scope, not v1)

Not in-context-file markdown, but part of the broader harness-context story; candidates for
later phases: MCP server config (`.mcp.json` / `opencode.json` / Codex `config.toml` /
Cursor `mcp.json`), slash commands (`.claude/commands/`), subagents (`.claude/agents/`),
skills (`.claude/skills/`, Codex skills), and hooks. Each is its own mapping project.

---

## Sources

- [How Claude remembers your project — Claude Code Docs](https://code.claude.com/docs/en/memory)
- [Custom instructions with AGENTS.md — OpenAI Developers](https://developers.openai.com/codex/guides/agents-md)
- [Rules — Cursor Docs](https://cursor.com/docs/context/rules)
- [Cursor @-symbols overview — Cursor Docs](https://docs.cursor.com/en/context/@-symbols/overview)
- [Rules — OpenCode Docs](https://opencode.ai/docs/rules/)
- [Config — OpenCode Docs](https://opencode.ai/docs/config/)
- [AGENTS.md import/reference discussion (issue #11)](https://github.com/agentsmd/agents.md/issues/11)
- [Customize Gemini using AGENTS.md files — Android Developers](https://developer.android.com/studio/gemini/agent-files)
- [Mastering .mdc Files in Cursor: Best Practices](https://medium.com/@ror.venkat/mastering-mdc-files-in-cursor-best-practices-f535e670f651)
