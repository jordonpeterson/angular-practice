# In-File Functionality Map

Converting the syntax and behavior *inside* context files between Claude Code, OpenAI Codex, Cursor, and OpenCode.

*Phase 1, the project's key document. [File-location research](context-file-equivalencies.md) answered "where do files live"; this answers "what does the markup inside a file **do**, and what's the equivalent in every other harness?"*

*Compiled July 2026. Uncertain items tagged **[verify]**, listed in §8.*

---

## 0. Why this is separate from file-location mapping

Same location, different behavior — depending on the markup inside:

- `@./style.md` in `CLAUDE.md` **inlines** the file's text.
- `@style.md` in a Cursor `.mdc` **attaches** the file as a reference.
- The same string in a Codex `AGENTS.md` is **literal text** — Codex has no imports, so the agent sees the characters `@style.md`.

Same syntax, three behaviors. The tool can't copy bytes; it must understand the behavior and re-express it.

---

## 1. Taxonomy of in-file capabilities

Every category is mapped in §2.

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

Adjacent surfaces (MCP servers, slash commands, subagents, skills) are **out of scope for v1** — separate config files, not in-context-file syntax. See §9.

---

## 2. Per-capability mapping

Each subsection: syntax per harness, behavioral differences, conversion rule, fidelity class (§4). Every conversion row is a future test fixture (§7).

### C1 — Includes / imports

| Harness | Syntax | Semantics |
| --- | --- | --- |
| **Claude Code** | `@path/to/file.md` (inline in body) | **Inline expansion.** Text spliced into context at load. Max 4 hops. Relative to importing file. |
| **Codex** | *(none)* | No includes. Only whole-file concatenation across directories. |
| **Cursor** | `@file.md` in a `.mdc`; `@RuleName` to pull another rule | **Attach by reference**, not a literal text splice. |
| **OpenCode** | `opencode.json` → `"instructions": [...]` (globs, paths) | **External include list**, not in-file. Combined with `AGENTS.md`. In-file `@import` **[verify]** — not documented. |

**Normalization:** four operations (splice / none / attach / external-list); lossless common denominator is inline expansion.

- **On read:** resolve every include to literal text, flatten into the IR block (also resolves Claude 4-hop chains and Cursor `@Rule` refs). IR holds no unresolved includes.
- **On write:** emit literal text by default. Re-externalize into a native include only if (a) target supports it losslessly and (b) config opts in (keep `opencode.json` instructions, or Claude `@imports` for humans).
- **Claude approval trap:** imports pointing outside the project (`@~/...`) trigger a one-time approval dialog; declined = permanently disabled. Tool-emitted imports must stay in-repo or risk silently never loading.

**Conversions**

| From → To | Rule | Fidelity |
| --- | --- | --- |
| Claude `@import` → Codex | Inline the imported text. | Lossless (content); loses modular split. |
| Claude `@import` → Cursor | Inline, or rewrite to `@file` if a real file. | Lossless |
| Cursor `@file` → Claude/Codex | Inline the referenced file. | Lossless if local + readable; else **drop + diagnostic**. |
| OpenCode `instructions` glob → all | Expand each matched file inline. | Lossless |
| any → OpenCode | Inline into `AGENTS.md`, or emit `opencode.json instructions`. | Lossless |

### C2 — Path / glob scoping

| Harness | Syntax | Notes |
| --- | --- | --- |
| **Claude Code** | `.claude/rules/*.md` with `paths:` frontmatter (glob list) | Full glob. Loads only when a matching file is touched. |
| **Codex** | *(directory nesting only)* | To scope to `src/api`, put `AGENTS.md` **in** `src/api/`. No glob expression. |
| **Cursor** | `.mdc` `globs:` frontmatter (comma-separated) | Full glob. Drives "Auto Attached." |
| **OpenCode** | *(none)* | No per-rule scoping, and **below-cwd `AGENTS.md` is ignored** (walk-up only; issues #6316/#11454) — nested files scope Codex but are invisible to OpenCode. `instructions` globs select always-loaded files, not scope. |

**Asymmetry:** Cursor `globs` ⇄ Claude `paths` is a clean frontmatter rename. Codex has no glob — only physical directory placement.

- **Directory-prefix glob** (`src/api/**`) → Codex nested `AGENTS.md` in `src/api/`. Lossy-recoverable.
- **Non-directory glob** (`**/*.test.ts`, `src/**/*.{ts,tsx}`) → no faithful Codex form. Attach to nearest common-ancestor dir (over-broad) or drop → **diagnostic**. Rule `portable-globs-only`.

**Conversions**

| From → To | Rule | Fidelity |
| --- | --- | --- |
| Cursor `globs` ⇄ Claude `paths` | Rename frontmatter key; keep patterns. | Lossless |
| glob → Codex (directory-prefix) | Lower to nested `AGENTS.md` at that dir. | Lossy-recoverable |
| glob → Codex (non-prefix) | Attach to common-ancestor dir + warn, or drop + warn. | Lossy-degrading |
| glob → OpenCode (any) | No scoped form: always-on (root/`instructions`) or omit; warn either way. | Lossy-degrading |
| Codex nested file → glob harness | Synthesize `globs/paths: <dir>/**`. | Lossless |

### C3 — Activation mode

Cursor is the only harness with an activation concept — its defining feature and the biggest source of unrepresentable behavior.

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
| Agent-Requested → non-Cursor | Force-include (→ "always") or drop; config picks, either way warn. | Lossy-degrading |
| Manual → non-Cursor | Drop + diagnostic. | Unrepresentable |
| any body content → Cursor | Emit as `alwaysApply: true`. | Lossless |

### C4 — Frontmatter / metadata

| Harness | Frontmatter? | Fields |
| --- | --- | --- |
| **Claude Code** | Yes, in `.claude/rules/*.md` | `paths` |
| **Cursor** | Yes, in `.mdc` | `description`, `globs`, `alwaysApply` |
| **Codex** | No | — |
| **OpenCode** | No (config in `opencode.json`) | — |

`description` (Cursor) has no consumer elsewhere; preserve as HTML comment or drop (config). `paths`↔`globs` per C2. Converting **to** Codex/OpenCode strips frontmatter; meaning re-expressed structurally (nesting / opencode.json) or lost.

### C5 — Hidden / maintainer comments

**A behavioral trap.** Claude Code **strips block-level `<!-- ... -->` HTML comments** before sending to the model (comments **inside code blocks are preserved**). No other harness documents this — Codex, Cursor, OpenCode pass raw markdown, so the agent **sees** the comment.

| Direction | Consequence | Rule |
| --- | --- | --- |
| Claude → Codex/Cursor/OpenCode | Comment invisible to Claude becomes **visible** to the other agent. | Never propagate comments across files. |
| Codex/etc. → Claude | Working content becomes **invisible** (stripped by Claude). | `comment-visibility` lint warns. |

"Copy verbatim" is wrong for any file with comments. **Bidirectional policy (design §3.1):** comments are *file-local metadata* — excluded from block content hashes, preserved in situ when their file's block is rewritten by a merge, never copied to other files. (A naive "strip everywhere" rule would let one file's winning edit wipe a maintainer note out of another file.)

### C6 — Escaping / literal vs active

Claude treats bare `@path` as an **import** but backtick-wrapped `` `@path` `` as **literal**; import parsing skips fenced code blocks. Other harnesses have no import, so `@path` is always literal there.

| Direction | Trap | Rule |
| --- | --- | --- |
| **any → Claude** | Literal `@foo` (email handle, decorator, `@types/node`) would be **interpreted as an import**. | **Backtick-escape** every `@token` that isn't a deliberate import before writing a Claude file. |
| Claude → any | A deliberate `@import` must be inlined (C1); a `` `@literal` `` stays literal. | Distinguish via Claude's own rules (backticks / code fences). |

Getting C6 wrong silently corrupts output (phantom imports or lost text) — high-priority fixtures.

### C7 — Directory-scoped overrides (nesting semantics)

All four support nesting, but merge behavior differs:

| Harness | Nested-file behavior |
| --- | --- |
| **Claude Code** | Concatenate; nested file loads **on demand** when a file in that dir is read. |
| **Codex** | One file per directory (`AGENTS.override.md` > `AGENTS.md` > fallbacks); root→cwd, **closer overrides**. |
| **Cursor** | Nearest `AGENTS.md` / applicable `.mdc` wins for that subtree. **[verify]** nested `.cursor/rules/` subdirs unreliable — keep `.mdc` flat. |
| **OpenCode** | Walk **up** to git root only; first match wins per category. **Below-cwd nested files are never loaded** (#6316). |

For the merge engine (design §2), "the `## Build` block for `src/api/`" may live in a different physical file per harness. The IR keys a block by `(scope, heading-path)` so identity survives layout differences.

### C8 — Layer precedence

| Layer | Claude Code | Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- |
| Managed/org | `/etc/claude-code/CLAUDE.md`, `managed-settings.json` | — | Team Rules (dashboard) | — |
| User/global | `~/.claude/CLAUDE.md`, `~/.claude/rules/` | `~/.codex/AGENTS.md` | User Rules (UI) | `~/.config/opencode/AGENTS.md` |
| Project | `CLAUDE.md`, `.claude/rules/` | `AGENTS.md` | `.cursor/rules/`, `AGENTS.md` | `AGENTS.md` |
| Local | `CLAUDE.local.md` | `AGENTS.override.md` | *(none in-repo)* | *(none in-repo)* |

Tool syncs the **project layer** by default (the only committed, team-shared layer everywhere). User/managed/local are per-machine, **not synced** unless configured — where two harnesses legitimately should differ.

### C9 — Size / truncation

Only **Codex** has a hard cap: `project_doc_max_bytes`, **32 KiB by default** (the `65536` seen in the wild is a documented *override* example in `~/.codex/config.toml`, not a changed default). It **silently stops** adding files at the cap. A large `CLAUDE.md` that converts fine byte-wise can be **truncated at load** by Codex. The tool measures the flattened per-directory total against the targeted Codex config and raises `size-within-codex-cap` before content drops, suggesting a split into nested files.

### C10 — Remote / URL includes

Only **OpenCode** supports remote instruction URLs (`instructions: ["https://..."]`, 5 s timeout). Remote content is **non-deterministic**, breaking merge/`--check` guarantees. Rule `no-remote-instructions`: in `strict`/CI, reject; else snapshot+pin the fetched content into the lockfile for reproducibility. Converting to other harnesses: fetch once and inline.

### C11 — Consumer overlap / dedup

One file, several consumers — and one consumer reading several files:

- `AGENTS.md` is read by **Codex + OpenCode + Cursor**. It must satisfy the intersection of their capabilities (Codex's floor governs), and a merge conflict "between Codex and OpenCode" cannot exist — one file.
- **Cursor double-injection trap:** Cursor reads `AGENTS.md` *and* `.cursor/rules/*.mdc`. Mirroring global content into both loads it **twice** in Cursor. Policy (design §2): global blocks → `AGENTS.md` only; `.mdc` emitted only for Cursor-specific features (globs, activation). OpenCode is safe natively (its `AGENTS.md` suppresses `CLAUDE.md`); Claude Code is safe (doesn't read `AGENTS.md`).

---

## 3. Conversion primitives

Every §2 conversion decomposes into reusable operations, implemented once; each conversion is a composition.

| Primitive | What it does | Used by |
| --- | --- | --- |
| **inline-expand** | Replace an include with the referenced file's literal text. | C1, C10 |
| **externalize** | Extract a block into a separate file + native include. | C1 (opt-in) |
| **hoist-to-frontmatter** | Express scope as `globs:`/`paths:` YAML. | C2 |
| **lower-to-nesting** | Move a scoped block into a nested per-directory file. | C2, C7 |
| **rename-key** | `globs` ⇄ `paths`, etc. | C2, C4 |
| **strip-comments** | Remove block HTML comments. | C5 |
| **escape-tokens** | Backtick `@tokens` that aren't deliberate imports. | C6 |
| **drop-with-diagnostic** | Remove unrepresentable content; emit warning/error. | C2, C3, C4 |
| **coerce-activation** | Force-include or drop Cursor-only modes. | C3 |
| **measure-and-warn** | Check byte budget; warn before silent truncation. | C9 |

---

## 4. Fidelity classification

- **Lossless** — round-trips exactly. (Body text; `globs`↔`paths`.)
- **Lossy-recoverable** — reshaped but semantically equal; reconstructable. (Directory-prefix glob ↔ nested file.)
- **Lossy-degrading** — meaning changes for the worse but content survives. (Agent-Requested forced to always; non-prefix glob attached over-broadly.)
- **Unrepresentable** — no equivalent; dropped with a diagnostic. (Cursor Manual mode; Claude's comment-stripping into a non-stripping harness.)

The `compatibility` config (`off | portable | strict`) sets reaction to anything worse than Lossless: `portable` warns, `strict` errors, keeping a repo to the lowest-common-denominator feature set.

---

## 5. Master support matrix

`✅` native · `⚠️` partial/structural · `❌` none · `→cfg` via sidecar config.

| Capability | Claude Code | Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- |
| C1 Includes | ✅ inline `@` | ❌ | ✅ ref `@` | →cfg `instructions` |
| C2 Glob scoping | ✅ `paths` | ⚠️ dir-nesting | ✅ `globs` | ❌ (nested files ignored; `instructions` = always-on) |
| C3 Activation modes | ⚠️ path-only | ❌ | ✅ 4 modes | ❌ |
| C4 Frontmatter | ✅ `paths` | ❌ | ✅ 3 fields | ❌ |
| C5 Hidden comments | ✅ strips `<!-- -->` | ❌ literal | ❌ literal | ❌ literal |
| C6 `@` escaping | ✅ backtick/fence | n/a (literal) | ⚠️ | n/a |
| C7 Nested overrides | ✅ concat | ✅ override | ⚠️ nearest | ✅ first-match |
| C8 Layers | ✅ 4 | ⚠️ 2–3 | ✅ 3 | ⚠️ 2 |
| C9 Size cap | ❌ (unbounded) | ✅ hard cap | ❌ | ❌ |
| C10 Remote includes | ❌ | ❌ | ⚠️ `@Docs`/`@Web` (chat) | ✅ URL instructions |

**Difficulty gradient:** Cursor is the feature superset (activation modes, references); Codex the floor (no includes, globs, or frontmatter; hard size cap). Hardest directions: anything → Codex (flatten, may truncate) and Cursor-only features → anyone (must drop). Claude Code and OpenCode sit in the middle and interconvert cleanly.

### 5.1 The matrix is version-scoped

Every ✅ is "✅ **as of some version**." Documented movement:

| Harness | Capability | Version behavior |
| --- | --- | --- |
| Claude Code | C2 `paths` matching via symlinks | added **v2.1.198** |
| Claude Code | (auto memory — adjacent) | added **v2.1.59** |
| Cursor | legacy `.cursorrules` | deprecated ~**0.43** |
| Cursor | `.cursor/rules/` layout | rules become **folders** as of **2.2** |
| Cursor | C3 `alwaysApply` | **3.0.16** regression: treated as "requestable", not auto-injected |
| Codex | C9 `project_doc_max_bytes` | default 32 KiB; fallback filename list is config-driven, has shifted |
| OpenCode | C1/C7 discovery | AGENTS.md + `instructions` recent; `.opencode/AGENTS.md` still landing |

A capability is `feature → [ {version-range → behavior} ]`, not a boolean. Conversions, lint, and `context` assembly resolve against the **targeted** version(s) (design §9). Portability across a declared range is the **intersection** over every version — e.g. a repo targeting Cursor `>=2.0` can't assume folder-rules (2.2+) and should avoid `alwaysApply` reliance if `3.0.16` is in range. The fixtures (§7) are parameterized by version where behavior differs (`c3-cursor3016-alwaysapply-regression`, etc.), and the conformance probe re-derives cells from the real CLIs so the table self-heals.

---

## 6. Canonical (interchange) feature set

The IR is the union superset; a block carries: literal content, scope (glob), activation, layer, provenance, resolved (inlined) includes. Lowering to a harness lacking a feature applies §3 primitives and records a fidelity note. This keeps the "understand deep intricacies" logic in **one** place (the lowering table), read by both linter and converter — so warnings and actions can never disagree.

---

## 7. How this map becomes tests (Phase 2) and docs (Phase 3)

Each §2 conversion row is a **test-case seed**. The e2e harness (Phase 2) turns each into a fixture directory driving the CLI as a black box (language-agnostic):

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

Naming: `<capability>-<from>-<to>-<case>`. C1–C10 × harness pairs enumerates the full matrix, so **coverage is countable**.

Phase 3 renders every `intent.md` + `input → expected` diff into the docs site. Because docs are generated from fixtures, **the website can never document behavior the tool doesn't have**.

---

## 8. Verify before shipping

- **[C1/OpenCode]** In-file `@import` inside `AGENTS.md` — confirm support vs. `opencode.json instructions` being the only include path.
- **[C7/Cursor]** Reliability of nested `.cursor/rules/` subdirectories (community reports say flat-only).
- **[C3/Cursor]** Exact trigger semantics of Agent-Requested (how `description` is used).
- **[C4/Cursor]** Exact `.mdc` `globs` serialization Cursor accepts (comma-separated string vs YAML list; quoting) — byte-exact emission requires one canonical form.

*(Resolved: OpenCode per-rule scoping — none, and below-cwd discovery — none; issues #6316/#11454.)*

## 9. Adjacent surfaces (future scope, not v1)

Not in-context-file markdown but part of the broader harness-context story; later phases: MCP server config (`.mcp.json` / `opencode.json` / Codex `config.toml` / Cursor `mcp.json`), slash commands (`.claude/commands/`), subagents (`.claude/agents/`), skills (`.claude/skills/`, Codex skills), hooks, ignore files (`.cursorignore` / `.codeiumignore` / `.aiexclude` — negative context), and Codex's config-side instruction channels (`model_instructions_file`, `developer_instructions`). Each is its own mapping project.

**Never synced (agent-authored memory):** Claude Code auto memory (`~/.claude/projects/<p>/memory/`) and Cursor Memories (sidecar-generated rules, Settings → Rules). These are per-machine learning artifacts, not team instructions — the tool ignores them and lints against committing them.

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
