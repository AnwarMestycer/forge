# forge — Roadmap & Progress

> **Ship AI-written code with proof, not hope.**
> One loop: **build → verify → break → learn**.

Pace: ~8–10 h/week, part-time. Two tracks run in parallel (Go on weekdays, Rust on a weekend session).

## Status overview

| # | Milestone | Track | Weeks | Status |
|---|---|---|---|---|
| M0 | Foundations | Go + Rust | 1 | 🟡 In progress |
| Spike | Rust TCP proxy spike | Rust | 1 | 🟢 Done |
| Testbed | Sample NestJS + Postgres app | — | 1 | ⚪ Not started |
| M1 | Forge core | Go | 2–3 | ⚪ Not started |
| M2 | Vouch v1 (rules) | Go | 4–5 | ⚪ Not started |
| M3a | Fault proxy: TCP | Rust | 3–6 | ⚪ Not started |
| M3b | Fault proxy: Postgres-aware | Rust | 7–8 | ⚪ Not started |
| M3c | Fault proxy: control API | Rust | 9 | ⚪ Not started |
| M4 | Chaos stage | Go → Rust | 7–8 | ⚪ Not started |
| M5 | The loop (`forge check`) | Go | 9–10 | ⚪ Not started |
| M6 | AI pass + MCP server | Go | 11–12 | ⚪ Not started |
| M7 | eBPF X-ray & deeper | Rust | later | ⚪ Not started |

Legend: ⚪ not started · 🟡 in progress · 🟢 done · 🔴 blocked

**Critical path:** M0 → Spike → M3 → M4 → M5. Watch M3 most closely — it's the riskiest part, in the newest language.

---

## M0 — Foundations
- [x] Monorepo layout (Go module + Rust crate)
- [ ] `justfile`: `build`, `test`, `lint`, `run-testbed`
- [ ] CI: Go build/test/`golangci-lint`, Rust build/test/`clippy`
- [ ] cobra root with stub commands: `add`, `list`, `switch`, `context`, `vouch`, `chaos`
- [ ] `~/.forge/` config + SQLite with `projects` table
- [ ] GoReleaser shipping `forge` + `forge-proxy` together

**Done when:** `forge --version` installs as one binary on Fedora; `forge add .` and `forge list` work.

## Spike — Rust TCP proxy (week 1)
- [x] `tokio` proxy: listen `:6544` → forward to Postgres `:5432`
- [x] Fixed 200 ms delay on every packet
- [x] `psql -p 6544` works and is visibly slow

**Done when:** real Postgres traffic flows through Rust and you can feel the latency.

## Testbed
- [ ] `testbed/docker-compose.yml`: Postgres + minimal NestJS API
- [ ] One `users` table, one auth endpoint (JWT), one migration, RLS-style policy
- [ ] API can run through the proxy

**Done when:** `just run-testbed` brings it up and it talks to Postgres through the spike proxy.

## M1 — Forge core (Go)
- [ ] `forge add` registers a project (path, stack, compose file, env)
- [ ] `forge switch <name>`: start containers, load env, print "where you left off"
- [ ] `forge context sync` generates `CLAUDE.md` from stack, structure, git history

**Done when:** you use `forge switch` daily on 2 real projects.

## M2 — Vouch v1, rules only (Go)
- [ ] Diff parsing (`go-git`) since last commit / branch
- [ ] YAML rule engine
- [ ] Rule packs: RLS/policies, migrations w/o rollback, auth/JWT code, new env vars, deleted tests, new deps
- [ ] `tree-sitter` parsing for TS / Dart / SQL
- [ ] Output: risk score + "the 3 places you must read"

**Done when:** it catches a real risky change in one of your projects.

## M3 — Fault proxy (Rust) — the systems core
**M3a — TCP**
- [ ] Latency + jitter
- [ ] Packet drops, connection resets
- [ ] Bandwidth throttling

**M3b — Postgres-aware**
- [ ] Parse Postgres wire protocol (`pgwire`)
- [ ] Per-query delays, error injection
- [ ] Query logging with timing

**M3c — Control API**
- [ ] `axum` on Unix socket `~/.forge/proxy.sock`
- [ ] `/v1/rules` CRUD, live rule changes
- [ ] Contract in `api/schema.json`

**Done when:** testbed API runs through the proxy and degrades on command.

## M4 — Chaos stage (Go → Rust)
- [ ] Go client for the proxy (HTTP over Unix socket)
- [ ] `forge chaos run <scenario>`
- [ ] Scenarios: `slow-db`, `flaky-network`, `expired-jwt`, `container-kill` (Docker SDK)
- [ ] Record results: what broke, error rates, recovery time

**Done when:** Chaos finds a real weakness in one of your apps.

## M5 — The loop (Go)
- [ ] Vouch findings → auto-select Chaos scenarios
- [ ] Lessons written back to `CLAUDE.md` + store
- [ ] TUI dashboard (`bubbletea`): projects, last vouch, last chaos run
- [ ] `forge check` = verify → break → report

**Done when:** one command runs the whole loop.

## M6 — AI + agents (Go)
- [ ] Optional LLM pass in Vouch (Anthropic API, behind a flag)
- [ ] MCP server exposing `vouch`, `chaos`, `context`

**Done when:** Claude Code can run Vouch and Chaos itself before finishing a task.

## M7 — Deeper (later)
- [ ] eBPF X-ray (`aya`): syscalls + connections during chaos runs
- [ ] HTTP-level faults for Supabase / external APIs
- [ ] Shareable block + rule packs

---

## Decisions log
| Date | Decision |
|---|---|
| 2026-10-29 | Merge Forge + Vouch + Chaos into one tool: `forge` |
| 2026-10-29 | Hybrid stack: Go (CLI, core, Vouch, Chaos control, store) + Rust (fault proxy) |
| 2026-10-29 | Go ↔ Rust contract: HTTP + JSON over Unix socket, versioned `/v1`, schema in `api/schema.json` |
| 2026-10-29 | Monorepo; in-repo testbed app for reproducible Vouch/Chaos tests |
| 2026-10-29 | Testbed in NestJS (closest to real client work) — *default, revisit if it slows things down* |

## Rules
- **Dogfood from M1.** If you don't use it daily, fix that first.
- **Vouch without AI first.** Deterministic rules → trustworthy and fast. AI on top.
- **Give M3 extra time.** That's where the systems learning lives.
