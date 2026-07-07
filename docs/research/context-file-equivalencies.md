# AI Coding-Agent Context Files: Cross-Provider Equivalencies

Research notes for building a tool that converts persistent AI-agent context between
**Claude Code**, **OpenAI Codex**, **Cursor**, and **OpenCode**.

*Compiled July 2026. Providers change these conventions often — every claim below is
linked to a source at the bottom, and the "Verify before shipping" section lists the
facts most likely to drift.*

---

## 1. The big picture

There are really **three design philosophies** in play, and every provider is some
mix of them:

| Philosophy | What it looks like | Who uses it |
| --- | --- | --- |
| **A. Single memory file, concatenated up the tree** | One well-known filename (`CLAUDE.md` / `AGENTS.md`) discovered by walking the directory tree; all matches are concatenated into context. Plain Markdown. | Claude Code, Codex, OpenCode |
| **B. The open `AGENTS.md` standard** | A vendor-neutral `AGENTS.md` that many tools agree to read, so one file serves several agents. Plain Markdown. | Codex, Cursor, OpenCode, + ~20 others |
| **C. Structured, glob-scoped rule packs** | A directory of small rule files with YAML frontmatter that scopes each rule to file globs and activation modes. | Cursor (`.cursor/rules/*.mdc`), Claude Code (`.claude/rules/*.md`) |

The key insight for a conversion tool: **most content is portable Markdown, but the
*scoping* metadata (which rule applies to which files, and when) is where providers
diverge and where conversion becomes lossy.**

A second key insight: **`AGENTS.md` is the closest thing to a lingua franca.** Codex,
Cursor, and OpenCode all read it natively; Claude Code is the notable holdout (it reads
only `CLAUDE.md`, but the official guidance is to `@import` or symlink `AGENTS.md`).
That makes `AGENTS.md` the natural canonical/interchange format for the tool.

---

## 2. Master equivalency chart

The core question the tool must answer: *"For concept X in provider P, what is the
equivalent in provider Q?"*

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

**Format:** plain Markdown. `<!-- HTML comments -->` at block level are stripped before
injection (useful for maintainer notes that shouldn't cost tokens).

**Locations, in load order (broadest → most specific):**

1. **Managed policy** — `/Library/Application Support/ClaudeCode/CLAUDE.md` (macOS),
   `/etc/claude-code/CLAUDE.md` (Linux/WSL), `C:\Program Files\ClaudeCode\CLAUDE.md`
   (Windows). Cannot be overridden by users. Can also be inlined via the `claudeMd`
   key in `managed-settings.json`.
2. **User** — `~/.claude/CLAUDE.md` and `~/.claude/rules/*.md` (all projects).
3. **Project** — `./CLAUDE.md` **or** `./.claude/CLAUDE.md` (committed, team-shared).
4. **Local** — `./CLAUDE.local.md` (gitignored personal overrides).

**Loading algorithm:** walk up the directory tree from cwd, collecting every `CLAUDE.md`
and `CLAUDE.local.md`. All are **concatenated** (not overridden), ordered filesystem-root
→ cwd, with `CLAUDE.local.md` appended after `CLAUDE.md` at each level. Nested `CLAUDE.md`
files *below* cwd are **not** loaded at launch — they load on demand when Claude reads a
file in that subdirectory.

**Path-scoped rules:** `.claude/rules/*.md` (discovered recursively). Optional YAML
frontmatter:

```markdown
---
paths:
  - "src/api/**/*.ts"
---
# API rules
- All endpoints must validate input.
```

Rules **without** a `paths` field load unconditionally (same priority as
`.claude/CLAUDE.md`). Rules **with** `paths` load only when Claude touches a matching
file. This is Claude Code's analogue to Cursor's `globs`.

**Imports:** `@path/to/file` syntax expands and loads the referenced file at launch
(max depth 4 hops; relative to the importing file). Import parsing skips code spans and
fenced blocks. This is the officially recommended bridge to `AGENTS.md`:

```markdown
@AGENTS.md

## Claude Code
Use plan mode for changes under `src/billing/`.
```

**`AGENTS.md` compatibility:** Claude Code does **not** read `AGENTS.md` directly. Options:
`@AGENTS.md` import, or `ln -s AGENTS.md CLAUDE.md`. `/init` reads an existing `AGENTS.md`
(plus `.cursorrules`, `.devin/rules/`, `.windsurfrules`) and folds it into a generated
`CLAUDE.md`.

**Not context (but worth knowing):** *Auto memory* at
`~/.claude/projects/<project>/memory/MEMORY.md` is Claude-authored, not user-authored, so
the sync tool should ignore it.

**Exclusions:** `claudeMdExcludes` (glob) in settings skips ancestor `CLAUDE.md` files in
monorepos.

### 3.2 OpenAI Codex — `AGENTS.md`

**Format:** plain Markdown.

**Locations & filenames.** In each directory Codex checks, in order, and takes **at most
one file per directory**:

1. `AGENTS.override.md`
2. `AGENTS.md`
3. fallbacks from `project_doc_fallback_filenames` (e.g. `TEAM_GUIDE.md`, `.agents.md`)

**Scopes:**
- **Global:** `~/.codex/AGENTS.md` (and `~/.codex/AGENTS.override.md` for temporary global
  overrides).
- **Project & nested:** `AGENTS.md` at the repo root and in any subdirectory.

**Loading algorithm:** Codex walks from the **project root down to cwd**, taking one file
per directory, and **concatenates** them joined by blank lines. Files closer to cwd appear
**later**, so they override earlier guidance. `AGENTS.override.md` in a directory beats
that directory's `AGENTS.md`.

**Size limit:** combined docs are capped by `project_doc_max_bytes` (~32 KiB by default;
configurable in `~/.codex/config.toml`). Empty files are skipped; once the cap is hit,
no more files are added. **This is a hard constraint the tool must respect** — a large
`CLAUDE.md` can silently truncate when converted to Codex.

**Config file:** `~/.codex/config.toml` holds `project_doc_max_bytes`,
`project_doc_fallback_filenames`, etc. It configures discovery but is not itself context.

**No glob-scoped rules and no imports** — Codex relies purely on file nesting + overrides.
This is the biggest structural gap when converting Cursor `.mdc` rules → Codex.

### 3.3 Cursor — `.cursor/rules/*.mdc` (+ `AGENTS.md`)

The richest and most structured of the four.

**Three formats it reads:**
1. **Project Rules** — `.cursor/rules/*.mdc` (current, recommended).
2. **`AGENTS.md`** — plain-Markdown alternative, supported at root and in nested subdirs
   (nearest-file-wins).
3. **`.cursorrules`** — single root file, **deprecated** (Cursor ≥ 0.43) but still read.

**`.mdc` format** = Markdown + YAML frontmatter with three fields:

```markdown
---
description: Standards for API route handlers
globs: services/api/**/*.ts
alwaysApply: false
---
- Validate all inputs with zod.
- Return the standard error envelope.
```

**Four activation modes** (this is Cursor's defining feature and the hardest thing to
represent in other tools):

| Mode | Frontmatter | Behavior |
| --- | --- | --- |
| **Always** | `alwaysApply: true` | Injected into every request. |
| **Auto Attached** | `globs: <pattern>` | Injected when a matching file is in context. |
| **Agent Requested** | `description:` set, `alwaysApply: false`, no globs | Agent decides whether to pull it in, based on the description. |
| **Manual** | none of the above | Only when `@ruleName` is explicitly referenced. |

**Scopes / precedence:** Team Rules (dashboard) → Project Rules (`.cursor/rules/`) → User
Rules (Cursor Settings UI — plain text, all projects, **not a repo file**). All applicable
rules merge; earlier sources win on conflict.

**Nesting:** `.cursor/rules/` can live in subdirectories, but the idiomatic Cursor pattern
is to keep rules central and scope them with `globs` rather than nesting.

### 3.4 OpenCode — `AGENTS.md` (+ `opencode.json`)

**Format:** plain Markdown. `/init` generates the file.

**Discovery order (first match wins per category):**
1. **Local:** walk up from cwd to the git worktree root, loading `AGENTS.md` and
   `CLAUDE.md` along the way; also project `.opencode/`.
2. **Global:** `~/.config/opencode/AGENTS.md`.
3. **Claude Code fallback:** `~/.claude/CLAUDE.md` (unless disabled).

**Precedence:** if both `AGENTS.md` and `CLAUDE.md` exist, **only `AGENTS.md` is used**.
`~/.config/opencode/AGENTS.md` beats `~/.claude/CLAUDE.md`. This built-in Claude Code
compatibility means OpenCode is the easiest target — it can often consume Claude's files
directly.

**`opencode.json` `instructions` field** — the pointer/import mechanism. It accepts local
paths, **glob patterns**, and **remote URLs** (5 s fetch timeout), all combined with the
`AGENTS.md` content:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "instructions": ["CONTRIBUTING.md", "docs/guidelines.md", ".cursor/rules/*.md"]
}
```

Notice it can point straight at Cursor rule files — OpenCode is designed to reuse other
tools' context rather than duplicate it.

---

## 4. Concept-by-concept mapping (the tool's conversion table)

### 4.1 Project-root instructions
Trivially portable — the same Markdown body lives in `CLAUDE.md`, `AGENTS.md`, or a
root `.mdc`. **Canonical = `AGENTS.md`.** For Claude Code, either emit `CLAUDE.md` with
`@AGENTS.md` + Claude-specific tail, or emit `CLAUDE.md` as a full copy.

### 4.2 Path/glob scoping (the hard one)

| Source | Mechanism | → Target mapping |
| --- | --- | --- |
| Cursor `globs: src/**` | `.mdc` frontmatter | → Claude `.claude/rules/x.md` with `paths: ["src/**"]` (clean). → Codex: **lossy** — must become a nested `AGENTS.md` in `src/`, and only if the glob is a clean directory prefix. → OpenCode: `opencode.json instructions` glob or nested `AGENTS.md`. |
| Claude `paths:` rule | frontmatter | → Cursor `globs`. → Codex/OpenCode: nested file if directory-shaped. |
| Codex nested `AGENTS.md` in `src/` | directory nesting | → Cursor `globs: src/**`. → Claude nested `CLAUDE.md` or `paths` rule. |

**Rule of thumb:** glob ⇄ frontmatter is clean between Cursor and Claude Code; converting
*either* into Codex (which has no glob rules) requires collapsing to directory nesting and
only works when the glob is a directory prefix like `src/api/**`. Arbitrary globs
(`**/*.test.ts`) have **no faithful Codex representation** — the tool must warn.

### 4.3 Activation modes (Cursor-only)
Cursor's *Agent Requested* and *Manual* modes have no equivalent anywhere else. When
converting **to** other tools, *Always*/*Auto-Attached* rules translate; *Agent
Requested*/*Manual* rules should either be dropped with a warning or force-included
(changing their semantics — flag it).

### 4.4 Personal vs. committed
`CLAUDE.local.md` ⇄ `AGENTS.override.md` (Codex) are the closest pair. Cursor and OpenCode
have no committed-repo "local override" file (Cursor uses the Settings UI; OpenCode uses
global). The tool should treat local/override files as a separate, non-synced layer by
default.

### 4.5 Includes / imports

| Provider | Mechanism | Portable? |
| --- | --- | --- |
| Claude Code | `@path` (max 4 hops) | Expand inline when targeting Codex (no imports). |
| OpenCode | `opencode.json instructions` (globs, URLs) | Expand inline for others. |
| Codex | none | Must inline everything. |
| Cursor | `@rule` / `@file` refs | Expand inline for others. |

**Conversion strategy:** resolve/flatten all imports into a single canonical document,
then re-emit per-provider (re-introducing imports only where supported).

### 4.6 Size limits
Codex's ~32 KiB `project_doc_max_bytes` is the tightest hard cap. The tool should measure
the flattened canonical doc and **fail `check` with a clear error** if a Codex target
would truncate, suggesting the user split content into nested/scoped files.

---

## 5. Proposed canonical model for the tool

1. **Canonical format = `AGENTS.md`-style Markdown** with an optional lightweight
   frontmatter superset capturing `scope` (path globs) and `applies-to` (which providers).
2. **Flatten** all imports/includes into the canonical doc during ingest.
3. **Represent scoping abstractly** as (glob pattern → Markdown block), then lower to each
   provider:
   - Cursor → `.mdc` `globs`
   - Claude Code → `.claude/rules/*.md` `paths`
   - Codex → nested `AGENTS.md` (only if directory-prefix glob; else warn)
   - OpenCode → nested `AGENTS.md` + `opencode.json instructions`
4. **Emit a lossiness report** for anything that can't round-trip (arbitrary globs into
   Codex, Cursor Agent-Requested/Manual modes, size overflow).
5. **`check` mode** (CI): regenerate in memory, diff against on-disk files, exit non-zero
   on drift.

---

## 6. Verify before shipping (facts most likely to have drifted)

- Codex `project_doc_max_bytes` default: sources disagree between **32 KiB** and **64 KiB
  (65536)**. Confirm against the running `codex` version's `config.toml` defaults.
- Codex fallback filename list (`TEAM_GUIDE.md`, `.agents.md`) is configurable and may vary.
- Cursor's exact precedence wording (Team → Project → User) and whether `AGENTS.md` and
  `.mdc` rules are additive or exclusive when both exist.
- OpenCode's "first match wins per category" — confirm whether a project `AGENTS.md`
  fully suppresses a sibling `CLAUDE.md` or merges.
- Claude Code `.claude/rules/` `paths` frontmatter is relatively new; confirm the minimum
  version your CI targets.

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
