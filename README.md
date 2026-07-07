# agentsync

> **Working name — subject to change.** A command-line tool that keeps AI coding-agent
> context files in sync across providers.

## The problem

Every AI coding agent reads its own flavor of "persistent context" file:

- **Claude Code** reads `CLAUDE.md`
- **OpenAI Codex** reads `AGENTS.md`
- **Cursor** reads `.cursor/rules/*.mdc` (and `AGENTS.md`, and legacy `.cursorrules`)
- **OpenCode** reads `AGENTS.md` (falling back to `CLAUDE.md`)

A team that uses more than one of these has to hand-maintain the same guidance in
several files and formats. They drift. Someone edits `CLAUDE.md`, forgets the
`AGENTS.md`, and now the two agents disagree about how the codebase works.

## The goal

A CLI that runs locally and in CI. When a context file for one provider changes,
`agentsync` regenerates the equivalent files for every other provider so they all
carry the same instructions — respecting each tool's format, path conventions, and
scoping model.

```
$ agentsync check      # CI gate: fail if context files are out of sync
$ agentsync sync       # rewrite all provider files from the canonical source
$ agentsync diff       # show what would change
```

## Status

Early research. Before writing any code we are mapping exactly how each provider
discovers, loads, scopes, and formats its context files — including the lossy edges
where one provider's feature has no clean equivalent in another.

**Start here:** [`docs/research/context-file-equivalencies.md`](docs/research/context-file-equivalencies.md)
