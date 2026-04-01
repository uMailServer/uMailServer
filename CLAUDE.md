# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
- **Dependencies:** Minimal — only `bbolt`, `jwt/v5`, `uuid`, `crypto`, `yaml.v3`, `net/textproto`

## Architecture

### Single Binary Monolith

uMailServer compiles to a single binary that embeds all frontend assets (React/Vite) via `embed.FS` (see `embed.go`). The `internal/server/server.go` `Server` struct is the orchestrator — it initializes and wires all subsystems together.

### Startup Flow

`cmd/umailserver/main.go` → `config.Load()` → `server.New(cfg)` → `srv.Start()`

`Start()` sequentially initializes: DB → MessageStore → TLS Manager → Queue → Mailstore → SMTP → Submission SMTP (587/465) → IMAP → POP3 → MCP → HTTP API. Each is a goroutine managed by `sync.WaitGroup`.

### Internal Package Map

| Package | Role |
|---------|------|
| `config` | YAML config loading, setup wizard (`Config` struct is the central config type) |
| `db` | bbolt-based persistence for domains, accounts, aliases, queue entries |
| `server` | Top-level orchestrator — creates and wires all other subsystems |
| `smtp` | SMTP server (inbound MX + submission). Pipeline pattern for message processing |
| `smtp/pipeline` | Pluggable message processing stages: SPF, DKIM, DMARC, greylisting, RBL, heuristics, AV |
| `imap` | IMAP4rev1 server with `BboltMailstore` backend |
| `pop3` | POP3 server (adapts IMAP mailstore) |
| `api` | REST API server (admin panel backend) with JWT auth |
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
| `security` | Security middleware and JWT handling |
| `sieve` | Sieve mail filtering script support |
| `vacation` | Vacation/auto-responder functionality |
| `websocket` | WebSocket support for real-time webmail updates |

### Key Patterns

- **SMTP Pipeline:** `smtp.NewPipeline()` → `pipeline.AddStage(stage)`. Each stage implements a `Stage` interface. Stages are wired in `server.go:Start()` in order: SPF → DKIM → DMARC → Greylist → RBL → Heuristic → Score → AV.
- **Handler injection:** SMTP/IMAP/POP3 servers use `SetAuthHandler`, `SetDeliveryHandler`, `SetAuthFunc` etc. to inject dependencies from the orchestrator.
- **Config types:** `config.Config` is the root YAML-mapped struct with nested configs for each subsystem. Custom `Size` and `Duration` types handle byte/time parsing.
- **Database:** bbolt (key-value) for all persistence. `db.DB` manages domains/accounts/aliases. `storage.Database` wraps a separate bbolt instance for search indexing. `imap.BboltMailstore` wraps another for IMAP mailbox metadata.
- **Frontend:** Three separate React+Vite apps under `web/` (account, admin) and `webmail/`. Built to `dist/` and embedded via `//go:embed`.

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
- The `web/` directory contains multiple independent npm projects (admin, account) each with their own `package.json` and build. `webmail/` is a third separate project.
- SMTP pipeline stages are stateless — state is passed through a `PipelineContext` struct. New stages must not store per-message state in struct fields.
- Config defaults are applied in `config.Load()` — nil/zero config values may not be valid. Always check `umailserver.yaml.example` for the expected structure.
- On Windows, signal handling (`SIGTERM`) in `cmdStop`/`cmdRestart` uses `os.Interrupt` which maps to SIGINT on Unix — the code is cross-platform but primarily targets Linux in production.
