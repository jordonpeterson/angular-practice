# Context-Acquisition Channels

Every way each harness gets context into the model, compared. Extends the
[functionality map](functionality-map.md) (in-file syntax of rules/memory files) to the
full channel landscape: **skills, commands, subagents, hooks, memory**.

*Compiled July 2026. `[verify]` items in §8.*

---

## 1. Channel taxonomy

| # | Channel | Loaded | Who triggers | Claude Code | Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- | --- | --- | --- |
| K1 | **Always-on instructions** | session start, full text | nobody (ambient) | `CLAUDE.md` | `AGENTS.md` | `AGENTS.md`, `.mdc` alwaysApply | `AGENTS.md` |
| K2 | **Scoped rules** | when files match | file activity | `.claude/rules/` `paths` | dir-nesting | `.mdc` `globs` | ❌ |
| K3 | **Skills** | frontmatter at start; body on demand | model *or* user | `.claude/skills/` | `.agents/skills/`, `~/.codex/skills/` | `.cursor/skills/` | `.opencode/skills/` (+ reads `.claude/` & `.agents/` skills) |
| K4 | **Commands / prompts** | on invocation only | user (`/cmd`) | `.claude/commands/` | `~/.codex/prompts/` (**deprecated → skills**) | `.cursor/commands` | `.opencode/commands/` |
| K5 | **Subagents** | own isolated context | model delegation | `.claude/agents/` | config-defined | `.cursor/agents` (2.4+) | `.opencode/agents/` |
| K6 | **Agent-authored memory** | session start (capped) | agent writes it | auto memory | ❌ | Memories (1.0+) | ❌ |
| K7 | **Runtime injection** | hook stdout → context | lifecycle events | `SessionStart`/`UserPromptSubmit` hooks | ❌ | hooks (limited) **[verify]** | plugins **[verify]** |
| K8 | **Config-side prompt channels** | replaces/augments system prompt | config | `--append-system-prompt` | `model_instructions_file`, `developer_instructions` | Team Rules | agent `prompt` field |

Sync scope: **v1 = K1+K2** (the file map). **K3–K5 are the natural v2** — file-based,
committed, portable. K6 is never synced (agent-authored). K7/K8 are detect-and-warn.

---

## 2. Skills (K3) — a converged standard

**All four harnesses adopted Anthropic's Agent Skills spec**: a directory per skill with
`SKILL.md` — YAML frontmatter (`name`, `description`, optional extras) + Markdown body —
and **progressive disclosure**: only frontmatter loads at startup (~tens of tokens/skill);
the body loads when the model or user invokes the skill.

| | Claude Code | Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- |
| Project location | `.claude/skills/` | `.agents/skills/` (scanned cwd→repo root) | `.cursor/skills/` | `.opencode/skills/` |
| Personal location | `~/.claude/skills/` | `~/.codex/skills/` (+ admin/system) | `~/.cursor/skills/` **[verify]** | `~/.opencode/skills/` |
| Reads other tools' skills | ❌ | ❌ | ❌ | ✅ `.claude/skills/`, `.agents/skills/` |
| `name` limit | ≤64 chars | — | — | 1–64, lowercase-hyphen, must match dir |
| `description` limit | ≤200 chars | — | — | 1–1024 chars |
| Extra frontmatter | `disable-model-invocation`, `user-invocable`, `allowed-tools` | — | `paths`, `disable-model-invocation`, `metadata` | permissions in *agent* frontmatter |
| Startup budget | — | skill list ≤2% of context or 8 KB | — | — |
| Invocation | `/name`, model-implicit | `/skills`, `$mention`, model-implicit | model-implicit | model-implicit |

**Conversion implications:**
- **Near-lossless across all four**: same file format; conversion is mostly a *location
  remap* plus frontmatter normalization (strictest caps win: name ≤64 lowercase-hyphen
  matching dir; description ≤200).
- **`.agents/skills/` is the AGENTS.md of skills** — Codex *and* OpenCode read it. One
  emitted copy can serve both.
- **Dedup trap (C11 pattern again):** OpenCode reads `.opencode/`, `.claude/`, *and*
  `.agents/` skills. Mirroring one skill into all three = triple-loaded frontmatter in
  OpenCode. Emission policy needed; whether same-name skills dedupe **[verify]**.
- Harness-specific frontmatter (`allowed-tools`, `paths`) is dropped/kept per target
  capability table, warn on drop.

**Skills unlock Cursor's "unrepresentable" activation modes** (upgrades functionality-map
C3): a *Manual* rule ≈ a skill with `disable-model-invocation: true` (user-only); an
*Agent-Requested* rule ≈ a plain skill (model decides from `description`). Lowering
Cursor-only rules to skills in the other three harnesses is **lossy-recoverable**, not a
drop. Cursor itself distinguishes rules ("static context, every conversation") from skills
("dynamic capabilities") the same way.

---

## 3. Commands (K4)

| | Claude Code | Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- |
| Location | `.claude/commands/`, `~/.claude/commands/` | `~/.codex/prompts/` (top-level only, personal-only) | `.cursor/commands` | `.opencode/commands/` |
| Format | md; filename = command; `$ARGUMENTS`; frontmatter `description`, `allowed-tools` | md; frontmatter `description`, `argument-hint` | md **[verify]** | md; frontmatter `description`, `agent`, `subtask` |
| Status | current | **deprecated → skills** | current | current |

Conversion: command → command where supported; **command → skill for Codex** (its official
migration). `$ARGUMENTS`-style templating portability **[verify]** per target. Codex's
personal-only prompts can't be committed — project commands lowered to Codex become
`.agents/skills/` entries.

## 4. Subagents (K5)

| | Claude Code | Codex | Cursor | OpenCode |
| --- | --- | --- | --- | --- |
| Location | `.claude/agents/`, `~/.claude/agents/` | `config.toml` | `.cursor/agents` (2.4+) | `.opencode/agents/` |
| Format | frontmatter (`name`, `description`, `tools`, `model`, …) + body = **system prompt** | TOML config | file-based **[verify]** details | frontmatter (`name`, `description`, `model`, `tools`, permissions) + body |

Three of four use "frontmatter + body-as-system-prompt" markdown — mutually convertible
(field renames; permission models differ). Codex is config-based: lossy, likely
detect-and-warn in v2 rather than convert.

## 5. Runtime injection & memory (K6/K7)

- **Claude hooks are a deterministic context channel:** `SessionStart` /
  `UserPromptSubmit` / `UserPromptExpansion` stdout is injected into model context —
  unlike CLAUDE.md, it can't be ignored, and it can be dynamic (git status, TODOs). **No
  equivalent in any other harness** → unrepresentable; `foreign-context-channel` lint
  (context built by hooks silently diverges across harnesses and our `context` oracle
  can't fully model it without executing the hook).
- **Agent-authored memory** (Claude auto memory; Cursor Memories): never synced; lint
  against committing.

---

## 6. Impact on the tool

1. **v2 sync surface = skills first** — converged spec, near-lossless, highest value.
   Commands and file-based subagents next. (v1 stays K1+K2.)
2. **New lowering for Cursor C3 modes → skills** (Manual/Agent-Requested no longer
   dropped). Update functionality-map C3.
3. **Skill emission policy** mirrors C11 dedup: prefer `.agents/skills/` for
   Codex+OpenCode, `.claude/skills/` for Claude, `.cursor/skills/` for Cursor; never
   double-emit into locations one harness multi-reads.
4. **`context` oracle** gains a skills dimension: effective *skill list* at a path (names +
   descriptions + which body would load) compared across harnesses; Codex's 2%/8 KB list
   budget is a truncation risk to lint (like C9).
5. **K7/K8 channels get detection lints**, not conversion: hooks, `model_instructions_file`,
   `developer_instructions`, Team Rules — anything that changes effective context outside
   synced files.

## 7. Fixture seeds (next set)

`k3-skill-roundtrip-all-four` · `k3-skill-frontmatter-caps` (description 1024→200) ·
`k3-dedup-opencode-multiread` · `k3-cursor-manual-rule-to-skill` ·
`k4-command-to-codex-skill` · `k5-subagent-md-roundtrip` · `k7-hook-context-lint` ·
`k3-codex-skill-list-budget`.

## 8. Verify

- Cursor personal skills dir (`~/.cursor/skills/`) — sources conflict.
- OpenCode same-name skill dedup across its three read locations.
- Cursor commands/agents file formats (2.4/2.5 docs).
- `$ARGUMENTS` templating equivalents outside Claude.
- Hook-like context injection in Cursor hooks / OpenCode plugins.

## Sources

- [Extend Claude with skills — Claude Code Docs](https://code.claude.com/docs/en/skills)
- [Agent Skills overview — Claude Platform Docs](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview)
- [Agent Skills — Codex, OpenAI Developers](https://developers.openai.com/codex/skills)
- [Custom Prompts — Codex, OpenAI Developers](https://developers.openai.com/codex/custom-prompts)
- [Skills in OpenAI Codex — fsck.com](https://blog.fsck.com/2025/12/19/codex-skills/)
- [Agent Skills — Cursor Docs](https://cursor.com/docs/context/skills)
- [Cursor 2.4 changelog: Subagents, Skills](https://cursor.com/changelog/2-4)
- [Agent Skills — OpenCode Docs](https://opencode.ai/docs/skills/)
- [Create custom subagents — Claude Code Docs](https://code.claude.com/docs/en/sub-agents)
- [Hooks reference — Claude Code Docs](https://code.claude.com/docs/en/hooks)
- [Slash commands — Claude Code Docs](https://code.claude.com/docs/en/agent-sdk/slash-commands)
- [Equipping agents for the real world with Agent Skills — Anthropic](https://www.anthropic.com/engineering/equipping-agents-for-the-real-world-with-agent-skills)
