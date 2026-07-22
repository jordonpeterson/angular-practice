# agentsync

> Working name (subject to change). CLI that keeps AI coding-agent context files in sync
> across providers — bidirectionally.

## The problem

Each AI coding agent reads its own context files:

- **Claude Code**: `CLAUDE.md`, `.claude/rules/*.md`
- **OpenAI Codex**: `AGENTS.md` (root + nested)
- **Cursor**: `.cursor/rules/*.mdc` (plus `AGENTS.md`)
- **OpenCode**: `AGENTS.md`

Teams using more than one hand-maintain the same guidance across formats. They drift:
edit `CLAUDE.md`, forget `AGENTS.md`, and the agents disagree.

## How it works

No file is privileged. Edit whichever context file you like; `agentsync` reconciles the
rest via a lockfile-based 3-way block merge, converting formats per each harness's real
semantics (imports, glob scoping, comment stripping, size caps — see the research docs).

```
$ agentsync sync           # reconcile all managed files (bidirectional)
$ agentsync sync --check   # CI gate: exit 1 if unpropagated edits exist
$ agentsync lint           # best-practice + portability rules
$ agentsync context <path> # show the effective context each harness loads at a path
```

Architecture (design §3): **e2e framework** (implementation-blind) · **context graph
generators** (per harness) · **equivalence engine** (harness-agnostic, graph-based) ·
**config** (`agentsync.toml`) · **lockfile** (`agentsync.lock`, the merge base).

## Running the tests

```
go test ./test/e2e/ -v
```

`spec/` is the executable spec (Given `base/` + config, When `edit/` + `cmd`, Then
`expected/` + `report.json`). Currently **16 of 20 pass**; the rest are red until their
features land. Implementation work is queued in [`Tasks.md`](Tasks.md).

## Docs

1. [`docs/research/context-file-equivalencies.md`](docs/research/context-file-equivalencies.md) — where context files live per provider
2. [`docs/research/functionality-map.md`](docs/research/functionality-map.md) — what in-file syntax does; conversion rules and fidelity
3. [`docs/research/context-channels.md`](docs/research/context-channels.md) — skills, commands, subagents, hooks, memory
4. [`docs/design/tool-design.md`](docs/design/tool-design.md) — architecture and semantics
5. [`docs/design/implementation-options.md`](docs/design/implementation-options.md) — Go stack choices
6. [`docs/spec/top-20-test-cases.md`](docs/spec/top-20-test-cases.md) — the v1 test spec
