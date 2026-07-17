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

## Running the tests

```
go test ./test/e2e/ -v
```

The `spec/` fixtures are the executable spec (Given `base/` + config, When `edit/` +
`cmd`, Then `expected/` + `report.json`). The CLI is a walking skeleton, so **19 of 20
fixtures currently fail — by design**: each failure is a missing behavior the
implementation must turn green. `03-sync-noop-idempotent` passes because a no-op binary
genuinely satisfies "sync on an in-sync tree writes nothing."

## Status

Early research. Before writing code, we're mapping how each provider discovers, loads, scopes, and formats its context files — including lossy edges where one provider's feature has no clean equivalent.

**Start here:**
1. [`docs/research/context-file-equivalencies.md`](docs/research/context-file-equivalencies.md) — where context files live per provider
2. [`docs/research/functionality-map.md`](docs/research/functionality-map.md) — what in-file syntax does, and conversion rules
3. [`docs/research/context-channels.md`](docs/research/context-channels.md) — skills, commands, subagents, hooks, memory
4. [`docs/design/tool-design.md`](docs/design/tool-design.md) — the tool's design
5. [`docs/spec/top-20-test-cases.md`](docs/spec/top-20-test-cases.md) — the v1 test spec
