# CLAUDE.md — forge

## What this is
`forge` is a developer command center that runs one loop: **build → verify → break → learn**.
Pitch: *Ship AI-written code with proof, not hope.*

- **Build (core):** scaffold projects from reusable blocks, `forge switch` between projects, keep per-project `CLAUDE.md` in sync.
- **Verify (`forge vouch`):** read a diff and produce a risk report (auth/RLS, migrations without rollback, new env vars, deleted tests, new deps) + "the 3 places you must read".
- **Break (`forge chaos`):** run the app behind a fault-injecting proxy (slow DB, dropped requests, expired JWTs, killed containers).
- **Learn:** what broke and how it was fixed is written back into project context.

The owner is a solo fullstack dev (NestJS, Next.js, Flutter, Supabase, n8n) learning systems programming through this project. Explain systems concepts (TCP, wire protocols, async I/O, Linux internals) when they come up — learning is a goal, not just shipping.

## Architecture (hybrid Go + Rust)
```
forge (Go CLI / TUI)            forge-proxy (Rust)
 ├─ internal/core   projects,    └─ TCP / Postgres fault injection
 ├─ internal/vouch  diff → rules     controlled over ~/.forge/proxy.sock
 ├─ internal/chaos  scenarios → drives the proxy
 ├─ internal/store  SQLite in ~/.forge
 └─ internal/mcp    exposes vouch/chaos/context to agents (later)
```

**Go ↔ Rust contract:** HTTP + JSON over a Unix socket (`~/.forge/proxy.sock`), versioned under `/v1/...`. The schema lives in `api/schema.json` — change it first, then both sides.

## Stack
- **Go:** cobra (CLI), viper (config), bubbletea + lipgloss (TUI), `modernc.org/sqlite` (no CGO), Docker Engine SDK, go-git, tree-sitter bindings, YAML rule packs.
- **Rust (`proxy/`):** tokio, later `pgwire` (Postgres protocol), `axum` (control API on Unix socket), later `aya` (eBPF).

## Repo layout
| Path | What |
|---|---|
| `cmd/forge/` | Go entrypoint |
| `internal/` | Go packages: core, vouch, chaos, store, mcp |
| `proxy/` | Rust crate `forge-proxy` |
| `api/` | Go ↔ Rust contract |
| `rules/` | Vouch YAML rule packs |
| `scenarios/` | Chaos scenarios |
| `testbed/` | Sample NestJS + Postgres app — the target Vouch and Chaos are tested against |
| `scripts/setup-github.sh` | Creates GitHub milestones/issues from ROADMAP.md |

## Commands
```bash
go build -o bin/forge ./cmd/forge && go test ./... && go vet ./...
cd proxy && cargo build && cargo test && cargo clippy -- -D warnings
cd proxy && cargo run -- 127.0.0.1:6544 127.0.0.1:5432 200   # spike proxy
just run-testbed / just stop-testbed                          # testbed (PROXY_DELAY_MS=50 default)
```
(`justfile` wraps these: `just build`, `just test`, `just lint`, `just run-testbed`.)

## Progress tracking
- **ROADMAP.md is the source of truth.** Milestones M0–M7 + Spike + Testbed, each with checkbox tasks and a "Done when".
- When a task is finished: tick its box in ROADMAP.md, update the status table, and close the matching GitHub issue (`gh issue close`). Issues are titled `<Milestone>: <task>`.
- Record any architectural decision in the **Decisions log** table at the bottom of ROADMAP.md, with the date.

## Current state
- M0: repo scaffold done; cobra commands, SQLite, GoReleaser still to do.
- Spike: done — Rust TCP proxy with fixed latency (`proxy/src/main.rs`) verified against real Postgres: `select 1` takes ~2 s via `psql -p 6544` vs ~26 ms direct.
- Testbed: done — `just run-testbed` brings up `testbed/` (NestJS API on :3000 → forge-proxy :6544 → Postgres :5432). Endpoints: `POST /auth/signup`, `POST /auth/login` (JWT), `GET /users/me` (RLS-scoped), `GET /health`. `just stop-testbed` tears it down.
- Recommended order: rest of M0 → M1. M3 (fault proxy) is the riskiest part of the critical path — start it early.

## Conventions
- Small commits, one task per commit, message prefixed with the milestone (`M0: ...`, `Spike: ...`).
- Vouch rules are deterministic first; the LLM pass (M6) is optional and behind a flag.
- Never test Vouch/Chaos on real client projects — use `testbed/`.
- Keep the Rust proxy free of `unwrap()` on network paths; log and drop the connection instead.
