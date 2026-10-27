# uMailServer API Documentation

This directory contains the machine-readable description of the uMailServer REST API.
The specs are written against the real routes registered in `internal/api/server.go`
(main listener) and `internal/api/admin.go` (admin-only listener). A prose
walkthrough lives in `docs/api-reference.md`.

## Files

- `openapi.yaml` - OpenAPI 3.0.3 specification (primary, most complete)
- `swagger.yaml` / `swagger.json` - the same API as Swagger 2.0, generated from
  `openapi.yaml` (identical content in YAML and JSON). Swagger 2.0 cannot express
  the cookie scheme, so cookie auth is described in prose only there.

## Overview

The API provides REST endpoints for:

- **Authentication** - JWT login/logout/refresh, TOTP 2FA self-service
- **Account self-service** - password change, forwarding, TOTP, vacation, filters, push
- **Mail** - list folders, read, send, flag, move, delete, drafts, search, threads
- **Administration** - domains, accounts, aliases, mail queue, rate limits, JWT key
  rotation, statistics (all admin only)
- **Real time** - Server-Sent Events (`/api/v1/events`) and MCP (`/mcp`)
- **Operations** - `/health`, `/health/ready`, admin JSON `/metrics`
- **Client configuration** - Thunderbird autoconfig and Outlook autodiscover

## Base URL

```
https://<host>/api/v1
```

The main listener is `http.port` (default 443). The process itself speaks plain
HTTP on that listener; TLS is expected to be terminated in front of it (session
cookies get the `Secure` attribute when the request arrived over TLS). A second,
admin-only listener binds `admin.bind:admin.port` (default `127.0.0.1:8443`) and
serves the admin SPA plus a subset of the admin routes (domains, accounts, queue,
rate limits, metrics, stats, admin vacations list, push stats) and `/health`.
Prometheus text metrics and `/healthz` are on a third listener
(`metrics.bind:metrics.port`, default `127.0.0.1:8080`, path `metrics.path`), not
on the API ports.

Not routed on the API listener: `/health/live`, `/api/v1/domains/{domain}/dns`,
`/api/v1/folders`, password-reset routes under `/api/v1/accounts/{email}/password`.

## Authentication

`POST /api/v1/auth/login` with `{"email","password","totp_code"?}`. The response
sets an HttpOnly `jwt` cookie (SameSite=Strict) and, for non-browser clients
(User-Agent without Mozilla/Chrome/Firefox/Safari/Edge), returns
`{"token": "...", "expiresIn": <seconds>}`. Browsers get no token in the body.

Authenticate later requests with either:

```http
Authorization: Bearer <jwt-token>
```

or the `jwt` cookie (the cookie is preferred when both are present). Tokens are
HS256 and must carry `exp`; logout and refresh revoke the presented token. The
admin listener accepts Bearer tokens only.

Cookie-authenticated unsafe requests: if an `Origin` or `Referer` header is sent it
must match the request host or a configured CORS origin, otherwise 403.

All unsafe methods (POST/PUT/DELETE) under `/api/v1/` - except login, logout and
`/api/v1/account/*` - must send `Content-Type: application/json`, even when they
have no body (otherwise 400). JSON bodies reject unknown fields (400). Body size is
capped at 4 MB (about 26 MB for `/mail/send` and `/mail/draft`); an oversized body
fails decoding with 400 (there is no 413 from the API handlers).

## Rate Limiting

- General API limiter: per client IP, 1-minute window, limit taken from
  `security.rate_limit.http_requests_per_minute` (default 120; 0 disables). It
  covers the protected `/api/v1/` routes and logout. Exceeding it returns
  `429` with `Retry-After: 60`.
- Login has its own protection: per-IP failure lockout (`security.max_login_attempts`,
  default 5 failures, 5 minutes, doubling up to 60 minutes) and a per
  (IP, account) budget of 5 failures per 5 minutes; 5 failed TOTP codes lock TOTP
  for the account for 5 minutes. All answer `429`.
- `/api/v1/account/password` (and self-delete) share the per-account failure
  budget with login.
- Exempt from the HTTP limiter: `/health`, `/health/ready`, `/api/v1/events`, `/mcp`
  (has its own per-IP limiter), `/metrics`, login.
- `Retry-After` is always `60`, even when a lockout lasts longer. No
  `X-RateLimit-*` headers are sent.

## Response Format

Successful responses are the JSON object or array documented per operation (there
is no `{"data": ...}` envelope). List endpoints for domains, accounts and aliases
return plain arrays with the unpaginated size in the `X-Total-Count` header
(`limit` 1-1000, default 100, and `offset`). Mail list endpoints return an object
(`emails`, `total`, `limit`, `offset`, `folder`, `unreadTotal`, `starredTotal`).

### Error (4xx, 5xx)

```json
{
  "error": "Human-readable error message"
}
```

Exceptions: the admin listener's auth layer answers
`{"error": "unauthorized", "message": "...", "code": 401}` (same for 403);
`/api/v1/events` and a few CSRF rejections use plain text;
`/mcp` returns JSON-RPC 2.0 error objects; autoconfig/autodiscover errors are XML.
Unsupported methods return `405` with an `Allow` header.

## Common HTTP Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 201 | Created |
| 204 | No Content (delete success) |
| 400 | Bad request / validation error / missing JSON Content-Type |
| 401 | Missing, invalid, expired or revoked token; disabled account; wrong TOTP |
| 403 | Forbidden (admin only, wrong current password, cross-origin cookie request) |
| 404 | Not found |
| 405 | Method not allowed (see `Allow`) |
| 409 | Conflict (duplicate, last active admin, domain account limit, destination exists) |
| 429 | Rate limit or lockout (see `Retry-After`) |
| 500 | Internal Server Error |
| 503 | Service/backing store unavailable |
| 507 | Mailbox quota exceeded (`/mail/send`, `/mail/draft`) |

## Using the Specs

### With Swagger UI

```bash
# Run from the repository root
docker run -p 8081:8080 -e SWAGGER_JSON=/spec/openapi.yaml \
  -v $(pwd)/api:/spec swaggerapi/swagger-ui
```

### With Redoc

```bash
docker run -p 8081:80 -e SPEC_URL=/spec/openapi.yaml \
  -v $(pwd)/api:/usr/share/nginx/html/spec redocly/redoc
```

### Code Generation

```bash
openapi-generator-cli generate -i api/openapi.yaml -g go -o ./client-go
openapi-generator-cli generate -i api/openapi.yaml -g typescript-fetch -o ./client-ts
```

## API Endpoints Summary

All paths below are on the main listener. "Admin" = administrator only (403
`{"error":"admin access required"}` otherwise).

### Authentication
| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/api/v1/auth/login` | public | Log in (sets `jwt` cookie, returns token to non-browser clients) |
| POST, DELETE | `/api/v1/auth/logout` | user | Revoke token, clear cookie |
| POST | `/api/v1/auth/refresh` | user | Rotate token |

### Account self-service
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/account/password` | Change own password (`current_password`, `new_password`) |
| GET | `/api/v1/account/totp` | TOTP state |
| POST | `/api/v1/account/totp/setup` | Start enrolment (returns `uri`) |
| POST | `/api/v1/account/totp/verify` | Confirm with `code` |
| POST | `/api/v1/account/totp/disable` | Disable (needs current `code` when enabled) |
| GET, PUT | `/api/v1/account/forwarding` | Own forwarding (`forward_to`, `keep_copy`) |
| GET, PUT, DELETE | `/api/v1/vacation` | Own vacation auto-reply |
| GET, POST | `/api/v1/filters` | List / create filters |
| POST | `/api/v1/filters/reorder` | Reorder (`filterIds`) |
| GET, PUT, DELETE | `/api/v1/filters/{id}` | Filter detail |
| POST | `/api/v1/filters/{id}/toggle` | Toggle enabled |
| GET | `/api/v1/push/vapid-public-key` | VAPID key |
| POST | `/api/v1/push/subscribe` | Register Web Push subscription |
| DELETE | `/api/v1/push/unsubscribe` | Remove (`?id=` or body `endpoint`) |
| GET | `/api/v1/push/subscriptions` | List own subscriptions |
| POST | `/api/v1/push/test` | Send test notification |

### Mail (caller's own mailboxes)
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET, POST | `/api/v1/mail/{inbox,sent,drafts,trash,spam}` | List a folder. Query: `folder`, `limit` (1-500, default 100), `offset`, `flag=flagged` (or `starred`), `unread=1` (`true`/`0`/`false`) |
| GET | `/api/v1/mail/message?id=&folder=` | Read one message (does not set `\Seen`) |
| POST | `/api/v1/mail/send` | Send (`to`,`cc`,`bcc`,`subject`,`body`,`inReplyTo`,`references`) |
| POST | `/api/v1/mail/flags` | Add/remove `\Seen`, `\Flagged`, `\Answered`, `\Draft` |
| POST, PUT | `/api/v1/mail/draft` | Save / update a draft |
| POST | `/api/v1/mail/move` | Move messages (`id`/`ids`, `from`, `to`) |
| DELETE, POST | `/api/v1/mail/delete?id=` | Delete a message permanently |
| GET | `/api/v1/search?q=` | Full-text search (`folder`, `limit` max 100, `offset`, `date_from`, `date_to`, `has_attachment`) |
| GET | `/api/v1/threads` | List threads |
| GET | `/api/v1/threads/search?q=` | Search threads |
| GET, DELETE | `/api/v1/threads/{id}?mailbox=` | Thread messages / delete |
| POST | `/api/v1/threads/{id}/read?mailbox=` | Mark thread read |

Message ids are content hashes. When identical bytes exist more than once, each
copy is listed as `<hash>_<uid>_<base64url(mailbox)>`; use that qualified id to
address exactly one copy in delete/flags/move/message.

### Real time and tooling
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/events` | Server-Sent Events (`connected`, `heartbeat`, `new_mail`, `expunge`, `flags_changed`, `folder_update`) |
| POST | `/mcp` | Model Context Protocol, JSON-RPC 2.0 (JWT required) |

### Domains (Admin)
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET, POST | `/api/v1/domains` | List (`limit`,`offset`) / create (`name`, `max_accounts`) |
| GET, PUT, DELETE | `/api/v1/domains/{domain}` | Detail / update (`max_accounts`, `is_active`) / delete with cascade |

### Accounts (Admin)
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET, POST | `/api/v1/accounts` | List (`domain`,`limit`,`offset`) / create (`email`,`password`,`is_admin`) |
| GET, PUT, DELETE | `/api/v1/accounts/{email}` | Detail / partial update / delete (409 for the last active admin) |
| POST | `/api/v1/accounts/{email}/totp/{setup,verify,disable}` | Manage a non-admin account's TOTP |

`DELETE /api/v1/accounts/{email}` takes no password for administrators. The
handler contains a "non-admin deletes own account with `{"password": ...}`" branch
(400/403/429), but the route sits behind the admin guard, so ordinary users get
403 first.

### Aliases (Admin)
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET, POST | `/api/v1/aliases` | List / create (`alias`, `target`, `is_active`) |
| GET, PUT, DELETE | `/api/v1/aliases/{alias}` | Detail / update / delete |

### Queue (Admin)
`/api/v1/admin/queue` and `/api/v1/queue` are equivalent.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/admin/queue` | List entries (`status` = pending, sending, failed; `limit`, `offset`) |
| GET | `/api/v1/admin/queue/{id}` | Entry detail |
| POST | `/api/v1/admin/queue/{id}` | Retry now (409 when sending/delivered/bounced) |
| DELETE | `/api/v1/admin/queue/{id}` | Drop entry |

### Server administration (Admin)
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/stats` | `domains`, `accounts`, `messages` (always 0), `queue_size` |
| GET | `/api/v1/metrics` | JSON metrics snapshot |
| GET, PUT | `/api/v1/admin/ratelimits/config` | Mail rate-limit configuration |
| GET | `/api/v1/admin/ratelimits/ip/{ip}`, `/user/{user}` | Rate-limit counters |
| GET | `/api/v1/admin/vacations` | Accounts with active vacation replies |
| GET | `/api/v1/admin/push/stats` | Push statistics |
| POST | `/api/v1/admin/jwt/rotate` | Rotate JWT signing key |
| GET | `/api/v1/admin/jwt/status` | JWT key status |

### Operations and client configuration
| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/health` | public | Health report (503 when unhealthy) |
| GET | `/health/ready` | public | Readiness (503 when draining / DB down) |
| GET | `/metrics` | Admin | JSON metrics (same as `/api/v1/metrics`) |
| GET | `/.well-known/autoconfig/mail/config-v1.1.xml` | public | Thunderbird autoconfig (`?emailaddress=`) |
| GET, POST | `/autodiscover/autodiscover.xml` | public | Outlook autodiscover (`?email=` or XML body) |
