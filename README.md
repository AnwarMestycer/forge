# forge

**Ship AI-written code with proof, not hope.**

`forge` is a developer command center that runs one loop:

1. **Build** — scaffold projects from your own blocks, switch between projects in one command, keep AI context (`CLAUDE.md`) in sync.
2. **Verify** (`forge vouch`) — read an agent's diff and produce a risk report: auth/RLS changes, unsafe migrations, new env vars, deleted tests, new dependencies.
3. **Break** (`forge chaos`) — run the app behind a fault-injecting proxy: slow DB, dropped requests, expired JWTs, killed containers.
4. **Learn** — what broke and how it was fixed flows back into the project context and your blocks.

## Architecture

```
forge (Go CLI / TUI)
 ├─ core      → project registry, blocks, context sync, switch
 ├─ vouch     → diff → rules + tree-sitter → risk report
 ├─ chaos     → scenarios, drives the proxy
 ├─ store     → SQLite in ~/.forge
 └─ mcp       → exposes vouch/chaos/context to AI agents

forge-proxy (Rust)
 └─ TCP / Postgres fault injection, controlled over ~/.forge/proxy.sock
```

## Repo layout

| Path | What |
|---|---|
| `cmd/forge/` | Go entrypoint |
| `internal/` | Go packages: `core`, `vouch`, `chaos`, `store`, `mcp` |
| `proxy/` | Rust crate `forge-proxy` |
| `api/` | Go ↔ Rust contract (`schema.json`) |
| `rules/` | Vouch YAML rule packs |
| `scenarios/` | Chaos scenarios |
| `testbed/` | Sample NestJS + Postgres app used to test everything |

## Status

Early development. See [ROADMAP.md](ROADMAP.md) for milestones and progress.

## Progress tracking

Milestones and tasks live in [ROADMAP.md](ROADMAP.md) and on GitHub (one milestone per roadmap section, one issue per task).
To (re)create them on GitHub: `gh auth login`, then `./scripts/setup-github.sh` (add `DRY_RUN=1` to preview).
