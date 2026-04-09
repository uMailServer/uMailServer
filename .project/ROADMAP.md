# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-10
> This roadmap prioritizes work needed to bring the project to production quality.

---

## Current State Assessment

uMailServer is a **production-grade single-binary email server** with comprehensive SMTP/IMAP/POP3 implementation, full RFC compliance, spam filtering (SPF/DKIM/DMARC/ARC/RBL/Bayesian/greylisting), ClamAV antivirus integration, S/MIME + OpenPGP encryption, Sieve mail filtering, TOTP 2FA, TLS via ACME, CalDAV/CardDAV/JMAP support, MCP AI integration, and embedded React frontends.

**Key Strengths:**
- Complete RFC compliance for core email protocols
- Comprehensive test suite (104 test files, all 35 packages passing)
- Well-structured codebase with clean package boundaries
- Excellent documentation (20+ markdown docs)
- Active security hardening (17 vulnerabilities fixed recently)
- Multi-platform builds (Linux, macOS, Windows; amd64 + arm64)
- Docker and Helm chart support
- Only 1 real TODO in entire codebase

**Key Blockers:**
1. **vacationReplies cleanup is threshold-based** — `server.go:1214` only runs cleanup when map exceeds 100 entries; high diversity email can grow map significantly between cleanups
2. **LDAP auth dead code** — `internal/auth/ldap.go` (360 LOC) fully implemented but never wired into auth flow
3. **`check deliverability` not implemented** — CLI command exists but `Diagnostics.CheckDeliverability()` method does not exist

---

## Phase 1: Critical Fixes (Week 1-2)
### Must-fix items blocking basic functionality

- [x] **Fix vacationReplies time-based cleanup** — `internal/server/server.go:1214`. Added `startVacationCleanup()` goroutine (hourly sweep for entries >48h old) in addition to the existing 100-entry threshold. ✅ **Done.**
- [x] **Wire LDAP auth into main auth flow or remove** — `internal/auth/ldap.go` integrated into `authenticate()` in `server.go`. LDAP is tried first; falls back to local DB on failure. ✅ **Done.**
- [x] **Implement `check deliverability` command** — `cmd/umailserver/main.go:937`. `CheckDeliverability()` added to `diagnostics.go` with DNS, RBL, TLS, and SMTP connectivity checks. ✅ **Done.**
- [x] **Update IMPLEMENTATION.md antivirus section** — Already correct (YARA v2 is planned for v2.0, ClamAV is current implementation). No change needed. ✅ **Done.**

---

## Phase 2: Core Completion (Week 3-6)
### Complete missing core features from specification

- [ ] **Argon2id password support** — Add as alternative to bcrypt (configurable). **Effort:** 1 day.

---

## Phase 3: Hardening (Week 5-6)
### Security, error handling, edge cases

- [ ] **Fuzz testing for SMTP/IMAP parsers** — Add fuzz tests for SMTP command parser, IMAP command parser, MIME parser. **Effort:** 3-5 days.
- [ ] **Security audit for HTML email sanitization** — Verify DOMPurify configuration is strict enough. Test XSS vectors in webmail. **Effort:** 1-2 days.
- [ ] **Input validation gaps** — Audit HTTP handlers for missing input validation. Add bounds checking on all size limits. **Effort:** 2 days.
- [ ] **Error response format consistency** — Ensure all API endpoints return consistent error format (`{error: string, code: string}`). **Effort:** 1 day.
- [ ] **JWT secret rotation mechanism** — Currently no key rotation for JWT. Document limitation or implement rotation. **Effort:** 1 day.

---

## Phase 4: Testing (Week 7-8)
### Comprehensive test coverage

- [ ] **Unit tests for packages with 0% coverage** — Identify any packages without `_test.go` files. Add tests. **Effort:** 2-3 days.
- [ ] **Integration tests for API endpoints** — Add integration tests for all REST API endpoints (domains, accounts, mail, queue, filters). **Effort:** 2-3 days.
- [ ] **Frontend component tests** — Set up Vitest for webmail/admin. Add tests for critical components (mail list, compose, sidebar). **Effort:** 3-5 days.
- [ ] **E2E test fix and enablement** — Fix flakiness in `e2e/` tests. Enable in CI. Playwright tests for critical flows. **Effort:** 3-4 days.
- [ ] **Backup/restore automated testing** — Add integration tests for `internal/cli/backup.go`. **Effort:** 1-2 days.
- [ ] **Benchmark suite** — Document performance characteristics. Add benchmark tests for hot paths (delivery, fetch, search). **Effort:** 1 day.

---

## Phase 5: Performance & Optimization (Week 9-10)
### Performance tuning and optimization

- [ ] **Profile under load** — Run `make bench` and identify hot spots. Profile with `pprof` under concurrent load. **Effort:** 2 days.
- [ ] **bbolt write amplification** — Analyze bbolt write patterns. Consider batch operations where applicable. **Effort:** 1-2 days.
- [ ] **Maildir delivery optimization** — Profile `internal/store/maildir.go` delivery path. Consider write caching or batch fsync. **Effort:** 2-3 days.
- [ ] **Search indexing parallelism** — Currently 10 workers. Tune based on CPU cores and queue depth. **Effort:** 1 day.
- [ ] **Frontend bundle size** — Analyze Vite bundle. Add lazy loading for routes. Tree-shake unused components. **Effort:** 2-3 days.
- [ ] **Memory profiling** — Profile under 10K account load. Identify memory leaks. **Effort:** 2 days.
- [ ] **Connection pooling for outbound SMTP** — Add connection reuse for MX delivery. **Effort:** 2-3 days.

---

## Phase 6: Documentation & DX (Week 11-12)
### Documentation and developer experience

- [ ] **API documentation (OpenAPI spec)** — Generate OpenAPI 3.0 spec from REST API handlers. **Effort:** 2-3 days.
- [ ] **Updated README with accurate setup instructions** — Verify all quickstart steps work. **Effort:** 1 day.
- [ ] **Architecture decision records (ADRs)** — Document key decisions (why bbolt, why Maildir++, why embedded UI). **Effort:** 1 day.
- [ ] **Contributing guide** — `docs/CONTRIBUTING.md` already exists, verify accuracy. **Effort:** 1 hour.
- [ ] **MCP integration guide** — Document MCP tools and usage. **Effort:** 1 day.

---

## Phase 7: Release Preparation (Week 13-14)
### Final production preparation

- [ ] **CI/CD pipeline completion** — All tests (including E2E) running in CI. **Effort:** 1 day.
- [ ] **Docker production image optimization** — Verify multi-stage build is minimal. **Effort:** 1 day.
- [ ] **Release automation (.goreleaser)** — Configure and test `.goreleaser.yml`. **Effort:** 2 hours.
- [ ] **Monitoring and observability** — Verify Prometheus metrics are comprehensive. **Effort:** 1 day.
- [ ] **v1.0.0 release** — Tag and create GitHub release with release notes. **Effort:** 2 hours.

---

## Beyond v1.0: Future Enhancements
### Features and improvements for future versions

- [ ] **Multi-node clustering** — Shared storage backend (S3-compatible for mail blobs), Raft consensus for config
- [ ] **LDAP/Active Directory integration** — Full LDAP auth wired into main auth flow
- [ ] **JMAP calendar/contacts** — Expand JMAP support beyond email
- [ ] **YARA v2 antivirus** — Lightweight YARA rules-based scanning
- [ ] **DMARC aggregate report parsing** — Dashboard showing auth pass/fail rates per sending source
- [ ] **White-label support** — For hosting providers
- [ ] **Webhook events expansion** — new mail, bounce, spam, delivery notifications

---

## Effort Summary

| Phase | Estimated Hours | Priority | Dependencies |
|---|---|---|---|
| Phase 1 | 16h | CRITICAL | None |
| Phase 2 | 24h | HIGH | Phase 1 |
| Phase 3 | 40h | HIGH | Phase 1 |
| Phase 4 | 64h | HIGH | Phase 1 |
| Phase 5 | 56h | MEDIUM | Phase 4 |
| Phase 6 | 32h | MEDIUM | Phase 5 |
| Phase 7 | 16h | MEDIUM | Phase 6 |
| **Total** | **248h (~6 weeks)** | | |

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| E2E tests remain flaky | High | Low (manual testing possible) | Allocate more time for debugging Playwright |
| LDAP integration complexity | Medium | Medium (misleading dead code) | Remove if not planned for v1.0 |
| Memory leak in vacationReplies | Medium | High (production stability) | Fix in Phase 1 |
| Security vulnerabilities in dependencies | Low | High | Dependency audit already done (17 fixed) |
| Frontend embedding build order | Low | High (broken binary) | `make build` already正确的chains `build-web` |
