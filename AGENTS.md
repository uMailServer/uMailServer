# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## ⚠️ MANDATORY LOAD

**Before any work in this project, read and obey `AGENT_DIRECTIVES.md` in the project root.**

All rules in that file are hard overrides. They govern:
- Pre-work protocol (dead code cleanup, phased execution)
- Code quality (senior dev override, forced verification, type safety)
- Context management (sub-agent swarming, decay awareness, read budget)
- Edit safety (re-read before/after edit, grep-based rename, import hygiene)
- Commit discipline (atomic commits, no broken commits)
- Communication (state plan, report honestly, no hallucinated APIs)

**Violation of any rule is a blocking issue.**

---

## Language & Tooling

- **Language:** Go 1.25+
- **Build:** `go build ./...` or `make build` (binary: `./cmd/umailserver`)
- **Test all:** `go test ./... -count=1 -short` or `make test`
- **Test single package:** `go test ./internal/smtp/... -count=1 -v`
- **Test single test:** `go test ./internal/smtp/... -run TestSessionName -count=1 -v`
- **Test with race:** `make test-race`
- **Lint:** `go vet ./...` (or `make lint` if golangci-lint installed)
- **Format:** `gofmt -s -w .` or `make fmt`
- **Coverage:** `make coverage` (generates `coverage.out` + `coverage.html`)
- **Dev mode:** `make dev` (runs Go server with `air` hot reload; frontends require separate `npm run dev` in `webmail/` or `web/admin/`)
- **Setup:** `make setup` (installs deps + dev tools: `air`, `golangci-lint`, `goimports`)
- **Build all platforms:** `make build-all` (linux/darwin/windows, amd64+arm64 binaries in `dist/`)
- **Docker:** `make docker` (builds multi-stage Alpine image)
- **Dependencies:** Minimal — `bbolt` (embedded KV), `jwt/v5`, `uuid`, `yaml.v3`, `golang.org/x/crypto` (bcrypt/argon2/ed25519), `miekg/dns` (DNS), `go-ldap/ldap`, `go-imap`, `webpush-go`

## Architecture

### Single Binary Monolith

uMailServer compiles to a single binary that embeds all frontend assets (React/Vite) via `embed.FS` (see `embed.go`). Build chain: `make build` → `build-web` (builds `webmail/` and `web/admin` to `dist/` via npm) → `go build`. The `web/account` portal is NOT built by `build-web` (build manually if changed). The `internal/server/server.go` `Server` struct is the orchestrator — it initializes and wires all subsystems together.

### Startup Flow

`cmd/umailserver/main.go` → `config.Load()` → `server.New(cfg)` → `srv.Start()`

`Start()` sequentially initializes: DB → MessageStore → TLS Manager → Queue → Mailstore → SMTP → Submission SMTP (587/465) → IMAP → POP3 → MCP → HTTP API. Each is a goroutine managed by `sync.WaitGroup`.

### Port Reference

| Port | Protocol | Description |
|------|----------|-------------|
| 25 | SMTP | Inbound mail (MX) |
| 587 | SMTP | Submission (STARTTLS, auth required) |
| 465 | SMTP | Submission (implicit TLS, auth required) |
| 143 | IMAP | IMAP (STARTTLS) |
| 993 | IMAP | IMAP (implicit TLS) |
| 995 | POP3 | POP3 (implicit TLS) |
| 4190 | ManageSieve | Sieve script management |
| 443 | HTTPS | Webmail + Admin Panel + REST API (single server) |
| 3000 | HTTP | MCP Server (JSON-RPC) |

### Internal Package Map

| Package | Role |
|---------|------|
| `config` | YAML config loading, setup wizard (`Config` struct is the central config type) |
| `db` | bbolt-based persistence for domains, accounts, aliases, queue entries |
| `server` | Top-level orchestrator — creates and wires all other subsystems |
| `smtp` | SMTP server (inbound MX + submission) with pluggable pipeline stages: SPF, DKIM, DMARC, greylisting, RBL, heuristics, AV |
| `imap` | IMAP4rev1 server with `BboltMailstore` backend |
| `pop3` | POP3 server (adapts IMAP mailstore) |
| `api` | REST API server (webmail + admin panel + autoconfig endpoints) with JWT auth, SSE push, CSP headers |
| `auth` | Email auth: SPF checking, DKIM verification, DMARC evaluation, ARC, DANE |
| `spam` | Spam scoring and filtering logic |
| `av` | Antivirus scanning (ClamAV integration) |
| `queue` | Outbound delivery queue with retry logic |
| `storage` | `MessageStore` (Maildir++ on disk) + `Database` (bbolt wrapper for search indexing) |
| `store` | Maildir++ format helpers |
| `search` | TF-IDF full-text search service over stored messages |
| `tls` | TLS certificate manager (ACME/Let's Encrypt auto-renewal) |
| `cli` | CLI subcommand implementations: diagnostics, backup, migration |
| `mcp` | Model Context Protocol server for AI assistant integration |
| `webhook` | Outbound webhook/event notification manager |
| `metrics` | Prometheus-compatible metrics endpoint |
| `sieve` | Sieve mail filtering script support |
| `vacation` | Vacation/auto-responder functionality |
| `websocket` | WebSocket support for real-time webmail updates |
| `caldav` | CalDAV calendar server (RFC 4791) |
| `carddav` | CardDAV contacts server (RFC 6352) |
| `jmap` | JMAP email API (RFC 8620) |
| `health` | Health check monitors for DB, queue, disk, TLS certs |
| `logging` | Structured JSON logging with rotation |
| `tracing` | OpenTelemetry distributed tracing |
| `ratelimit` | Per-IP/user/global rate limiting |
| `circuitbreaker` | Circuit breaker for external services |
| `alert` | Alert/notification manager |
| `push` | WebPush notification support |
| `audit` | Audit logging for admin actions |
| `autoconfig` | Thunderbird/Outlook autoconfig XML endpoints |
| `integration` | Test helpers and integration test utilities |

### Key Patterns

- **SMTP Pipeline:** `smtp.NewPipeline()` → `pipeline.AddStage(stage)`. Each stage implements a `Stage` interface. Stages are wired in `server.go:Start()` in order: SPF → DKIM → DMARC → Greylist → RBL → Heuristic → Score → AV.
- **Handler injection:** SMTP/IMAP/POP3 servers use `SetAuthHandler`, `SetDeliveryHandler`, `SetAuthFunc` etc. to inject dependencies from the orchestrator.
- **Config types:** `config.Config` is the root YAML-mapped struct with nested configs for each subsystem. Custom `Size` and `Duration` types handle byte/time parsing.
- **Database:** bbolt (key-value) for all persistence. `db.DB` manages domains/accounts/aliases. `storage.Database` wraps a separate bbolt instance for search indexing. `imap.BboltMailstore` wraps another for IMAP mailbox metadata.
- **Frontend:** `webmail/` (React 19 + Tailwind v4 + @radix-ui primitives), `web/admin` (React 19 + Tailwind v4 + shadcn + Recharts), `web/account` (React 19 + Tailwind v3 + Zustand + TanStack Query). Each built to `dist/` and embedded via `//go:embed`. `embed.go` exports `WebmailFS`, `AdminFS`, `AccountFS`.

### Data Flow (Inbound Email)

1. SMTP listener accepts connection → `smtp.Session` handles SMTP protocol
2. After DATA command, message goes through the pipeline stages
3. Auth stages (SPF/DKIM/DMARC) validate sender
4. Spam stages score the message; above reject threshold → rejected, above junk → delivered to Junk folder
5. `deliverMessage` callback in `server.go` routes to the message store
6. Message stored in Maildir++ format on disk
7. IMAP/POP3 servers serve stored messages to clients

## Dependency Policy

Minimal external dependencies. The project avoids large frameworks — only well-maintained, purpose-specific libraries (bbolt, jwt, bcrypt, yaml). Do not add new dependencies without justification.

## Known Gotchas

- `internal/db` and `internal/storage` both open separate bbolt databases — they are not the same thing. `db.DB` is for accounts/domains, `storage.Database` is for search indexing.
- Tests create temporary bbolt databases and maildir directories in `testdata/` or use `t.TempDir()`. Tests should clean up after themselves.
- The frontend is split into three independent projects: `webmail/` (React 19 + Tailwind v4 + @radix-ui primitives), `web/admin` (React 19 + Tailwind v4 + shadcn + Recharts), and `web/account` (React 19 + Tailwind v3 + Zustand + TanStack Query). Each has its own `package.json`, built to `dist/`, and embedded via `embed.go`.
- SMTP pipeline stages are stateless — state is passed through a `PipelineContext` struct. New stages must not store per-message state in struct fields.
- Config defaults are applied in `config.Load()` — nil/zero config values may not be valid. Always check `umailserver.yaml.example` for the expected structure.
- On Windows, signal handling (`SIGTERM`) in `cmdStop`/`cmdRestart` uses `os.Interrupt` which maps to SIGINT on Unix — the code is cross-platform but primarily targets Linux in production.

<!-- code-review-graph MCP tools -->
## MCP Tools: code-review-graph

**IMPORTANT: This project has a knowledge graph. ALWAYS use the
code-review-graph MCP tools BEFORE using Grep/Glob/Read to explore
the codebase.** The graph is faster, cheaper (fewer tokens), and gives
you structural context (callers, dependents, test coverage) that file
scanning cannot.

### When to use graph tools FIRST

- **Exploring code**: `semantic_search_nodes` or `query_graph` instead of Grep
- **Understanding impact**: `get_impact_radius` instead of manually tracing imports
- **Code review**: `detect_changes` + `get_review_context` instead of reading entire files
- **Finding relationships**: `query_graph` with callers_of/callees_of/imports_of/tests_for
- **Architecture questions**: `get_architecture_overview` + `list_communities`

Fall back to Grep/Glob/Read **only** when the graph doesn't cover what you need.

### Key Tools

| Tool | Use when |
|------|----------|
| `detect_changes` | Reviewing code changes — gives risk-scored analysis |
| `get_review_context` | Need source snippets for review — token-efficient |
| `get_impact_radius` | Understanding blast radius of a change |
| `get_affected_flows` | Finding which execution paths are impacted |
| `query_graph` | Tracing callers, callees, imports, tests, dependencies |
| `semantic_search_nodes` | Finding functions/classes by name or keyword |
| `get_architecture_overview` | Understanding high-level codebase structure |
| `refactor_tool` | Planning renames, finding dead code |

### Workflow

1. The graph auto-updates on file changes (via hooks).
2. Use `detect_changes` for code review.
3. Use `get_affected_flows` to understand impact.
4. Use `query_graph` pattern="tests_for" to check coverage.
