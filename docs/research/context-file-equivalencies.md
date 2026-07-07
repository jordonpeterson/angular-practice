# AI Coding-Agent Context Files: Cross-Provider Equivalencies

Research notes for a tool that converts persistent agent context between **Claude Code**, **OpenAI Codex**, **Cursor**, and **OpenCode**.

*Compiled July 2026. Conventions drift; every claim is sourced below, and §6 lists the facts most likely to have changed.*

---

## 1. The big picture

Three design philosophies; each provider mixes them:

| Philosophy | What it looks like | Who uses it |
| --- | --- | --- |
| **A. Single memory file, concatenated up the tree** | One well-known filename (`CLAUDE.md` / `AGENTS.md`) discovered by walking the directory tree; all matches concatenated. Plain Markdown. | Claude Code, Codex, OpenCode |
| **B. The open `AGENTS.md` standard** | A vendor-neutral `AGENTS.md` many tools read, so one file serves several agents. Plain Markdown. | Codex, Cursor, OpenCode, + ~20 others |
| **C. Structured, glob-scoped rule packs** | A directory of small rule files with YAML frontmatter scoping each to file globs and activation modes. | Cursor (`.cursor/rules/*.mdc`), Claude Code (`.claude/rules/*.md`) |

Key insights for conversion:
- Most content is portable Markdown; the **scoping metadata** (which rule applies to which files, when) is where providers diverge and conversion goes lossy.
- **`AGENTS.md` is the lingua franca.** Codex, Cursor, and OpenCode read it natively; Claude Code is the holdout (reads only `CLAUDE.md`; official guidance is to `@import` or symlink `AGENTS.md`). So `AGENTS.md` is the natural canonical/interchange format.

---

## 2. Master equivalency chart

Core question: *"For concept X in provider P, what is the equivalent in Q?"*

| Concept | Claude Code | OpenAI Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- |
| **Primary project file** | `CLAUDE.md` or `.claude/CLAUDE.md` | `AGENTS.md` | `AGENTS.md` and/or `.cursor/rules/*.mdc` | `AGENTS.md` |
| **Reads `AGENTS.md` natively?** | ❌ No (import/symlink it) | ✅ Yes | ✅ Yes | ✅ Yes (preferred) |
| **Reads `CLAUDE.md` natively?** | ✅ Yes | ❌ No | ❌ No | ✅ Yes (fallback if no `AGENTS.md`) |
| **Personal / local (uncommitted)** | `CLAUDE.local.md` (gitignore) | `AGENTS.override.md` | `.cursor/rules/*.mdc` (gitignored) | — (use global) |
| **User / global (all projects)** | `~/.claude/CLAUDE.md`, `~/.claude/rules/*.md` | `~/.codex/AGENTS.md` | Cursor Settings → User Rules (UI, not a repo file) | `~/.config/opencode/AGENTS.md` |
| **Org / managed policy** | `/etc/claude-code/CLAUDE.md` (Linux), macOS/Windows paths; `managed-settings.json` `claudeMd` | — | Team Rules (dashboard) | — |
| **Nested / subdir scoping** | Nested `CLAUDE.md` (loaded on demand) | Nested `AGENTS.md` / `AGENTS.override.md` | Nested `AGENTS.md` or nested `.cursor/rules/` | Nested `AGENTS.md` / `CLAUDE.md` |
| **Path/glob-scoped rules** | `.claude/rules/*.md` with `paths:` frontmatter | ❌ (nesting only) | `.cursor/rules/*.mdc` with `globs:` frontmatter | via `opencode.json` `instructions` globs |
| **Include other files** | `@path` imports (max 4 hops) | ❌ (concatenation only) | `@rule` references / `@file` | `opencode.json` `instructions` array (globs + URLs) |
| **Legacy format still read** | — | fallbacks: `TEAM_GUIDE.md`, `.agents.md` | `.cursorrules` (deprecated) | `CLAUDE.md` |
| **File format** | Markdown | Markdown | Markdown + **YAML frontmatter** (`.mdc`) | Markdown |
| **Discovery direction** | Walk **up** cwd→root, concat root→cwd | Walk root→cwd, concat root→cwd | Nearest-file-wins + glob match | Walk **up** cwd→git root |
| **Multiple files** | Concatenated (all apply) | One file **per directory** (override order) | Merged; Team → Project → User precedence | First match wins per category |
| **Size limit** | Soft: aim < 200 lines (loaded in full) | Hard: `project_doc_max_bytes` ≈ 32 KiB default | Per-rule (soft) | Combined with instructions |
| **Bootstrap command** | `/init` | `codex` scaffolds / `/init` | "New Cursor Rule" UI | `/init` |

---

## 3. Per-provider deep dive

### 3.1 Claude Code — `CLAUDE.md`

- **Format:** plain Markdown. Block-level `<!-- HTML comments -->` are stripped before injection.
- **Locations, broadest → most specific:**
  1. **Managed policy** — `/Library/Application Support/ClaudeCode/CLAUDE.md` (macOS), `/etc/claude-code/CLAUDE.md` (Linux/WSL), `C:\Program Files\ClaudeCode\CLAUDE.md` (Windows). Not user-overridable. Can be inlined via `claudeMd` in `managed-settings.json`.
  2. **User** — `~/.claude/CLAUDE.md` and `~/.claude/rules/*.md`.
  3. **Project** — `./CLAUDE.md` **or** `./.claude/CLAUDE.md` (committed).
  4. **Local** — `./CLAUDE.local.md` (gitignored).
- **Loading:** walk up from cwd collecting every `CLAUDE.md` + `CLAUDE.local.md`; all **concatenated** (not overridden), ordered root→cwd, `CLAUDE.local.md` after `CLAUDE.md` at each level. Nested `CLAUDE.md` *below* cwd load on demand, not at launch.
- **Path-scoped rules:** `.claude/rules/*.md` (recursive). Optional YAML frontmatter:

  ```markdown
  ---
  paths:
    - "src/api/**/*.ts"
  ---
  # API rules
  - All endpoints must validate input.
  ```

  Rules without `paths` load unconditionally (same priority as `.claude/CLAUDE.md`); rules with `paths` load only when Claude touches a matching file. Analogue to Cursor's `globs`.
- **Imports:** `@path/to/file` expands at launch (max depth 4 hops, relative to importing file; skips code spans/fenced blocks). Recommended bridge to `AGENTS.md`:

  ```markdown
  @AGENTS.md

  ## Claude Code
  Use plan mode for changes under `src/billing/`.
  ```

- **`AGENTS.md` compatibility:** not read directly. Use `@AGENTS.md` import or `ln -s AGENTS.md CLAUDE.md`. `/init` reads existing `AGENTS.md` (plus `.cursorrules`, `.devin/rules/`, `.windsurfrules`) into a generated `CLAUDE.md`.
- **Not context:** *Auto memory* at `~/.claude/projects/<project>/memory/MEMORY.md` is Claude-authored — sync tool should ignore it.
- **Exclusions:** `claudeMdExcludes` (glob) skips ancestor `CLAUDE.md` files in monorepos.

### 3.2 OpenAI Codex — `AGENTS.md`

- **Format:** plain Markdown.
- **Per-directory pick (at most one), in order:** 1. `AGENTS.override.md`  2. `AGENTS.md`  3. fallbacks from `project_doc_fallback_filenames` (e.g. `TEAM_GUIDE.md`, `.agents.md`).
- **Scopes:** Global `~/.codex/AGENTS.md` (+ `~/.codex/AGENTS.override.md`); project + nested `AGENTS.md` at root and any subdirectory.
- **Loading:** walk **root → cwd**, one file per directory, **concatenated** (blank-line joined). Files closer to cwd appear later and override. `AGENTS.override.md` beats that directory's `AGENTS.md`.
- **Size limit:** capped by `project_doc_max_bytes` (~32 KiB default; set in `~/.codex/config.toml`). Empty files skipped; once cap hit, no more files added. **Hard constraint** — a large `CLAUDE.md` can silently truncate when converted.
- **Config file:** `~/.codex/config.toml` holds `project_doc_max_bytes`, `project_doc_fallback_filenames`, etc. Configures discovery; not itself context.
- **No glob rules, no imports** — pure file nesting + overrides. Biggest gap when converting Cursor `.mdc` → Codex.

### 3.3 Cursor — `.cursor/rules/*.mdc` (+ `AGENTS.md`)

Richest and most structured.

- **Three formats read:**
  1. **Project Rules** — `.cursor/rules/*.mdc` (current, recommended).
  2. **`AGENTS.md`** — plain-Markdown alternative, root + nested (nearest-file-wins).
  3. **`.cursorrules`** — single root file, **deprecated** (Cursor ≥ 0.43) but still read.
- **`.mdc` format** = Markdown + YAML frontmatter (`description`, `globs`, `alwaysApply`):

  ```markdown
  ---
  description: Standards for API route handlers
  globs: services/api/**/*.ts
  alwaysApply: false
  ---
  - Validate all inputs with zod.
  - Return the standard error envelope.
  ```

- **Four activation modes** (defining feature; hardest to represent elsewhere):

  | Mode | Frontmatter | Behavior |
  | --- | --- | --- |
  | **Always** | `alwaysApply: true` | Injected into every request. |
  | **Auto Attached** | `globs: <pattern>` | Injected when a matching file is in context. |
  | **Agent Requested** | `description:` set, `alwaysApply: false`, no globs | Agent decides based on description. |
  | **Manual** | none of the above | Only when `@ruleName` referenced. |

- **Scopes / precedence:** Team Rules (dashboard) → Project Rules (`.cursor/rules/`) → User Rules (Settings UI — plain text, all projects, not a repo file). All applicable rules merge; earlier sources win on conflict.
- **Nesting:** `.cursor/rules/` can be nested, but idiomatic pattern keeps rules central and scopes via `globs`.

### 3.4 OpenCode — `AGENTS.md` (+ `opencode.json`)

- **Format:** plain Markdown. `/init` generates it.
- **Discovery (first match wins per category):**
  1. **Local:** walk up cwd → git worktree root loading `AGENTS.md` and `CLAUDE.md`; also project `.opencode/`.
  2. **Global:** `~/.config/opencode/AGENTS.md`.
  3. **Claude Code fallback:** `~/.claude/CLAUDE.md` (unless disabled).
- **Precedence:** if both `AGENTS.md` and `CLAUDE.md` exist, **only `AGENTS.md` is used**. `~/.config/opencode/AGENTS.md` beats `~/.claude/CLAUDE.md`. This Claude compatibility makes OpenCode the easiest target.
- **`opencode.json` `instructions` field** — pointer/import mechanism accepting local paths, **globs**, and **remote URLs** (5 s fetch timeout), combined with `AGENTS.md`:

  ```json
  {
    "$schema": "https://opencode.ai/config.json",
    "instructions": ["CONTRIBUTING.md", "docs/guidelines.md", ".cursor/rules/*.md"]
  }
  ```

  Can point straight at Cursor rule files — designed to reuse other tools' context.

---

## 4. Concept-by-concept mapping (the conversion table)

### 4.1 Project-root instructions
Trivially portable — same Markdown body in `CLAUDE.md`, `AGENTS.md`, or a root `.mdc`. **Canonical = `AGENTS.md`.** For Claude Code, emit `CLAUDE.md` with `@AGENTS.md` + Claude-specific tail, or as a full copy.

### 4.2 Path/glob scoping (the hard one)

| Source | Mechanism | → Target mapping |
| --- | --- | --- |
| Cursor `globs: src/**` | `.mdc` frontmatter | → Claude `.claude/rules/x.md` with `paths: ["src/**"]` (clean). → Codex: **lossy** — nested `AGENTS.md` in `src/`, only if glob is a clean directory prefix. → OpenCode: `opencode.json instructions` glob or nested `AGENTS.md`. |
| Claude `paths:` rule | frontmatter | → Cursor `globs`. → Codex/OpenCode: nested file if directory-shaped. |
| Codex nested `AGENTS.md` in `src/` | directory nesting | → Cursor `globs: src/**`. → Claude nested `CLAUDE.md` or `paths` rule. |

**Rule of thumb:** glob ⇄ frontmatter is clean between Cursor and Claude Code; converting either into Codex (no glob rules) requires collapsing to directory nesting, only works for directory-prefix globs like `src/api/**`. Arbitrary globs (`**/*.test.ts`) have **no faithful Codex representation** — warn.

### 4.3 Activation modes (Cursor-only)
*Agent Requested* and *Manual* have no equivalent elsewhere. Converting out: *Always*/*Auto-Attached* translate; *Agent Requested*/*Manual* should be dropped with a warning or force-included (changing semantics — flag it).

### 4.4 Personal vs. committed
`CLAUDE.local.md` ⇄ `AGENTS.override.md` (Codex) are the closest pair. Cursor and OpenCode have no committed "local override" file (Cursor uses Settings UI; OpenCode uses global). Treat local/override as a separate, non-synced layer by default.

### 4.5 Includes / imports

| Provider | Mechanism | Portable? |
| --- | --- | --- |
| Claude Code | `@path` (max 4 hops) | Expand inline when targeting Codex. |
| OpenCode | `opencode.json instructions` (globs, URLs) | Expand inline for others. |
| Codex | none | Must inline everything. |
| Cursor | `@rule` / `@file` refs | Expand inline for others. |

**Strategy:** resolve/flatten all imports into one canonical document, then re-emit per-provider (re-introducing imports only where supported).

### 4.6 Size limits
Codex's ~32 KiB `project_doc_max_bytes` is the tightest hard cap. Measure the flattened canonical doc and **fail `check` with a clear error** if a Codex target would truncate; suggest splitting into nested/scoped files.

---

## 5. Proposed canonical model for the tool

1. **Canonical = `AGENTS.md`-style Markdown** with optional lightweight frontmatter superset capturing `scope` (path globs) and `applies-to` (providers).
2. **Flatten** all imports/includes into the canonical doc on ingest.
3. **Represent scoping abstractly** as (glob → Markdown block), then lower per provider:
   - Cursor → `.mdc` `globs`
   - Claude Code → `.claude/rules/*.md` `paths`
   - Codex → nested `AGENTS.md` (only if directory-prefix glob; else warn)
   - OpenCode → nested `AGENTS.md` + `opencode.json instructions`
4. **Emit a lossiness report** for anything that can't round-trip (arbitrary globs into Codex, Cursor Agent-Requested/Manual modes, size overflow).
5. **`check` mode** (CI): regenerate in memory, diff against disk, exit non-zero on drift.

---

## 6. Verify before shipping (facts most likely to have drifted)

- Codex `project_doc_max_bytes` default: sources disagree between **32 KiB** and **64 KiB (65536)**. Confirm against the running `codex` version.
- Codex fallback filename list (`TEAM_GUIDE.md`, `.agents.md`) is configurable and may vary.
- Cursor's exact precedence wording (Team → Project → User) and whether `AGENTS.md` and `.mdc` rules are additive or exclusive when both exist.
- OpenCode's "first match wins per category" — confirm whether a project `AGENTS.md` fully suppresses a sibling `CLAUDE.md` or merges.
- Claude Code `.claude/rules/` `paths` frontmatter is relatively new; confirm the minimum version your CI targets.

---

## 7. Sources

**Claude Code**
- [How Claude remembers your project — Claude Code Docs](https://code.claude.com/docs/en/memory)
- [The CLAUDE.md Configuration Hierarchy — AI Agent Factory](https://agentfactory.panaversity.org/docs/General-Agents-Foundations/claude-code-teams-cicd/claude-md-configuration-hierarchy)

**OpenAI Codex**
- [Custom instructions with AGENTS.md — OpenAI Developers](https://developers.openai.com/codex/guides/agents-md)
- [Configuration Reference — OpenAI Developers](https://developers.openai.com/codex/config-reference)
- [Codex Guide: AGENTS.md, Cascading Rules, and AGENTS.override.md](https://ai.sulat.com/codex-guide-agents-md-cascading-rules-and-the-optional-agents-override-md-1f4c81767e92)
- [AGENTS.md for Codex CLI (2026): Lookup Order, Limits & Monorepo Templates](https://www.codegateway.dev/en/blog/agents-md-playbook-2026)

**Cursor**
- [Rules — Cursor Docs](https://cursor.com/docs/context/rules)
- [Cursor Rules: .mdc Frontmatter, globs & alwaysApply — TECHSY](https://techsy.io/en/blog/cursor-rules-guide)
- [Cursor deprecated .cursorrules — migrate to Project Rules — FlowQL](https://www.flowql.com/en/blog/guides/cursor-rules-deprecated-libraries/)

**OpenCode**
- [Rules — OpenCode Docs](https://opencode.ai/docs/rules/)
- [Config — OpenCode Docs](https://opencode.ai/docs/config/)

**AGENTS.md standard**
- [AGENTS.md](https://agents.md/)
- [AGENTS.md Guide (2026): Copilot, Cursor & More](https://vibecoding.app/blog/agents-md-guide)
- [CLAUDE.md vs AGENTS.md vs GEMINI.md: How Each CLI Reads Project Context](https://inventivehq.com/blog/claude-md-vs-agents-md-vs-gemini-md)
