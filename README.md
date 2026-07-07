# agentsync

> Working name (subject to change). CLI that keeps AI coding-agent context files in sync across providers.

## The problem

Each AI coding agent reads its own context file:

- **Claude Code**: `CLAUDE.md`
- **OpenAI Codex**: `AGENTS.md`
- **Cursor**: `.cursor/rules/*.mdc` (plus `AGENTS.md` and legacy `.cursorrules`)
- **OpenCode**: `AGENTS.md` (falls back to `CLAUDE.md`)

Teams using more than one hand-maintain the same guidance across files and formats. They drift: edit `CLAUDE.md`, forget `AGENTS.md`, and the agents disagree.

## The goal

A CLI for local and CI use. When one provider's context file changes, `agentsync` regenerates the equivalents for every other provider — respecting each tool's format, path conventions, and scoping.

```
$ agentsync check      # CI gate: fail if context files are out of sync
$ agentsync sync       # rewrite all provider files from the canonical source
$ agentsync diff       # show what would change
```

## Status

Early research. Before writing code, we're mapping how each provider discovers, loads, scopes, and formats its context files — including lossy edges where one provider's feature has no clean equivalent.

**Start here:** [`docs/research/context-file-equivalencies.md`](docs/research/context-file-equivalencies.md)
