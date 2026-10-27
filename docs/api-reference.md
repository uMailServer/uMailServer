# API Reference

Complete REST API documentation for uMailServer.

## Base URL

```
https://mail.example.com/api/v1
```

## Authentication

All API requests require authentication via JWT token in the Authorization header.

### Login

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "secretpassword"
}
```

**Response** (`200`):
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expiresIn": 86400
}
```

The session is also set as an HttpOnly `jwt` cookie (`SameSite=Strict`).
Requests with a browser User-Agent (`Mozilla/`, `Chrome/`, `Firefox/`,
`Safari/`, `Edge/`) get only `expiresIn` in the body and rely on the cookie;
other clients also receive `token`. `expiresIn` is in seconds. Wrong
credentials return `401`; repeated failures return `429` with `Retry-After`.

### Using the Token

```http
GET /api/v1/mail/inbox
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

## Authentication Endpoints

### POST /api/v1/auth/login

Authenticate and receive JWT token.

**Request:**
```json
{
  "email": "user@example.com",
  "password": "secretpassword",
  "totp_code": "123456"  // Optional, if 2FA enabled
}
```

### POST /api/v1/auth/logout

Invalidate the current token.

### POST /api/v1/auth/refresh

Exchange the current (still valid) session for a new one. The request needs
no body; it is authenticated by the existing cookie or bearer token, which is
revoked on success. Response: `{"token": "...", "expiresIn": <seconds>}` (a
cookie session also receives the replacement cookie). A revoked token returns
`401`. There are no separate refresh tokens.

Two-factor authentication is managed through the `/api/v1/account/totp*`
endpoints (see Account endpoints below); there are no `/auth/2fa/*` routes.

## Domain Management (Admin)

### GET /api/v1/domains

List all domains, sorted by name.

**Query Parameters:** `limit` (1-1000, default 100), `offset` (default 0).
The total is returned in the `X-Total-Count` header.

**Response:** a JSON array:
```json
[
  {
    "name": "example.com",
    "max_accounts": 100,
    "is_active": true,
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z",
    "dkim_selector": "default",
    "dkim_public_key": "..."
  }
]
```

### POST /api/v1/domains

Create a new domain.

**Request:**
```json
{
  "name": "newdomain.com",
  "max_accounts": 100
}
```

### GET /api/v1/domains/{domain}

Get domain details.

### PUT /api/v1/domains/{domain}

Update domain settings (`max_accounts`, `is_active`).

### DELETE /api/v1/domains/{domain}

Delete a domain and its accounts (their mailbox data is purged too). `409` when the domain holds the last active administrator.

### GET /api/v1/domains/{domain}/dns

**Not implemented.** The route is not registered. The domain's DKIM selector
and public key are returned by `GET /api/v1/domains/{domain}`; the full record
set is printed by the CLI: `umailserver domain dns <domain>`.

## Account Management (Admin)

### GET /api/v1/accounts

List accounts, sorted by address. The response is a JSON array; the total is
in the `X-Total-Count` header.

**Query Parameters:**
- `domain` - Filter by domain
- `limit` - Page size, 1-1000 (default 100)
- `offset` - Entries to skip (default 0)

Each entry has `email`, `is_admin`, `is_active`, `quota_used`, `quota_limit`,
`forward_to`, `forward_keep_copy`, `created_at`, `updated_at`, `last_login` and,
when set, `vacation_settings`.

### POST /api/v1/accounts

Create a new account.

**Request:**
```json
{
  "email": "newuser@example.com",
  "password": "securepassword123",
  "is_admin": false
}
```

### GET /api/v1/accounts/{email}

Get account details.

### PUT /api/v1/accounts/{email}

Update account.

**Request:**
```json
{
  "password": "newpassword",
  "is_active": true,
  "quota_limit": 2147483648
}
```

Every field is optional; omitted fields keep their stored value. Also
accepted: `is_admin`, `forward_to`, `forward_keep_copy`, `vacation_settings`.
Changing `is_admin` requires the acting admin's password in
`current_admin_password` (`403` otherwise). Demoting or deactivating the last
active administrator is refused with `409`.

### DELETE /api/v1/accounts/{email}

Delete an account (admin only; `403` for everyone else). `409` when it is the
last active administrator, `404` when it does not exist, `204` on success. The
account's mailboxes, search index and vacation settings are purged too.

The handler also contains a re-authentication branch for a non-admin deleting
their *own* account (body `{"password": "..."}`, `400` when missing, `403` when
wrong, `429` after repeated failures), but the route sits behind the admin check,
so non-admins never reach it: self-service account deletion is not available
through the API today.

### POST /api/v1/accounts/{email}/password

**Not implemented.** This route is not registered; requests return `404 Not Found`. Password changes are self-service — see `POST /api/v1/account/password` below. (Admin password resets can be performed with the same fields via `PUT /api/v1/accounts/{email}`.)

## Account Self-Service

Endpoints for the **authenticated account** to manage its own credentials
and settings. Authentication accepts the session cookie (set by
`POST /api/v1/auth/login`) or a bearer token.

### POST /api/v1/account/password

Change the authenticated account's password. The current password must be
re-supplied.

**Request body** (JSON):

| Field | Type | Description |
|---|---|---|
| `current_password` | string | The account's current password. |
| `new_password` | string | The new password: 8-128 characters with at least one uppercase letter, lowercase letter, digit and special character (the same policy as account creation). |

**Response (200):**
```json
{
  "message": "password changed"
}
```

Every existing session of the account is revoked. The acting session is
replaced: cookie sessions receive a new `jwt` cookie, and non-browser clients
also get `token` and `expiresIn` in the response body.

**Errors:** `400` invalid body, missing fields or a password that fails the
policy · `401` missing authentication · `403` wrong `current_password` ·
`404` account not found · `429` too many wrong-password attempts (shared with
login and account self-delete).

### GET /api/v1/account/totp

Report the authenticated account's TOTP (2FA) state.

**Response (200):**
```json
{
  "enabled": false,
  "pending_setup": false
}
```

### POST /api/v1/account/totp/setup

Generate a new TOTP secret (stored encrypted, not yet enabled) and return
the `otpauth://` URI for the authenticator app. `409` when TOTP is already
enabled (disable it first).

**Response (200):**
```json
{
  "uri": "otpauth://totp/uMailServer:user@example.com?secret=...&issuer=uMailServer"
}
```

### POST /api/v1/account/totp/verify

Verify a 6-digit code against the pending secret and enable TOTP. Each
accepted code consumes its time step (RFC 6238 §5.2 replay protection) and
cannot be reused.

**Request:**
```json
{
  "code": "123456"
}
```

**Response (200):**
```json
{
  "enabled": true
}
```

Enabling revokes the account's other sessions (the acting session is replaced
as for a password change).

**Errors:** `400` TOTP not set up (call `/setup` first) or invalid body ·
`401` invalid or already-used code · `429` too many failed attempts (5
failures lock TOTP checks for 5 minutes).

### POST /api/v1/account/totp/disable

Disable TOTP and clear the stored secret. Disabling your own enabled 2FA
requires a current code: send `{"code": "123456"}` (`400` when missing, `401`
for a wrong or already-used code, `429` after repeated failures). Sessions are
revoked as for enabling.

**Response (200):**
```json
{
  "enabled": false
}
```

### GET /api/v1/account/forwarding

Report the authenticated account's mail-forwarding configuration.

**Response (200):**
```json
{
  "forward_to": "backup@example.com",
  "keep_copy": true,
  "forwarding_on": true
}
```

### PUT /api/v1/account/forwarding

Update the authenticated account's mail forwarding. An empty `forward_to`
disables forwarding and clears `keep_copy`. The response echoes the stored
configuration and is authoritative.

**Request:**
```json
{
  "forward_to": "backup@example.com",
  "keep_copy": true
}
```

**Response (200):**
```json
{
  "forward_to": "backup@example.com",
  "keep_copy": true,
  "forwarding_on": true
}
```

**Errors:** `400` invalid body or `forward_to` is not a valid email
address · `401` missing authentication · `404` account not found.

## Mail Endpoints

Routes are registered in `internal/api/server.go` (`/api/v1/mail/*`); handlers
live in `internal/api/mail.go`, `mail_ext.go` and `mail_move.go`. All of them
operate on the authenticated user's own mailboxes only.

| Method | Path | Purpose |
|--------|------|---------|
| GET/POST | `/api/v1/mail/inbox`, `/sent`, `/drafts`, `/trash`, `/spam` | List a folder (or `?folder=<name>`) |
| GET | `/api/v1/mail/message?id=&folder=` | Fetch one message |
| POST | `/api/v1/mail/send` | Send a message |
| POST | `/api/v1/mail/flags` | Add/remove flags on messages |
| POST / PUT | `/api/v1/mail/draft` | Save / update a draft |
| POST | `/api/v1/mail/move` | Move messages between folders |
| DELETE / POST | `/api/v1/mail/delete?id=` | Delete a message |

### GET /api/v1/mail/inbox (also sent, drafts, trash, spam)

List messages in a folder. The folder is taken from the path segment, or from
`?folder=` (default INBOX).

**Query Parameters:**
- `folder` - Folder name
- `limit` - Page size, 1-500 (default 100)
- `offset` - Number of newest messages to skip (default 0)
- `flag=flagged` - Only starred (`\\Flagged`) messages (`starred` is accepted as a synonym; any other value is `400`)
- `unread=1` - Only unread messages (`1`/`true`; `0`/`false` or omitted = no filter; any other value is `400`)

The filters combine, e.g. `/api/v1/mail/inbox?flag=flagged&unread=1`. With a
filter, `total` counts the matching messages; `unreadTotal` and `starredTotal`
are always folder-wide counts.

**Response:**
```json
{
  "emails": [
    {
      "id": "msg-123",
      "from": "sender@example.com",
      "fromName": "Sender",
      "to": ["user@example.com"],
      "subject": "Hello World",
      "preview": "This is a preview of the message...",
      "date": "2024-01-15T10:30:00Z",
      "read": true,
      "starred": false,
      "folder": "INBOX",
      "hasAttachments": false,
      "size": 2048
    }
  ],
  "total": 150,
  "limit": 100,
  "offset": 0,
  "folder": "INBOX",
  "unreadTotal": 12,
  "starredTotal": 3
}
```

**Message ids.** An id is the content hash of the stored message. When the
same hash is held by more than one message of the account (two copies in one
folder, or the same mail in INBOX and Sent), the list reports each copy under a
qualified id `<hash>_<uid>_<base64url(mailbox)>` that addresses exactly one
(folder, uid) entry. A hash held once keeps its plain id. Pass the id exactly as
listed to `message`, `flags`, `move` and `delete`.

### GET /api/v1/mail/message

Get one message. Query: `id` (required), `folder` (optional; when omitted all
of the caller's mailboxes are searched). Read-only: it does not set `\Seen`
(use `/mail/flags`). Returns the same object as a list entry, including
`body`. `400` invalid id/folder, `404` not found.

### POST /api/v1/mail/send

Send a new message (`Content-Type: application/json`).

```json
{
  "to": ["recipient@example.com"],
  "cc": ["cc@example.com"],
  "bcc": ["bcc@example.com"],
  "subject": "Hello",
  "body": "Plain text content"
}
```

Subject is limited to 998 characters and the body to 25 MB. Attachments are
not part of the request body. `507 Insufficient Storage` is returned when the
stored copy would exceed the sender's quota.

### POST /api/v1/mail/flags

Add or remove system flags (`\Seen`, `\Flagged`, `\Answered`, `\Draft`).

```json
{
  "id": "msg-123",
  "ids": ["msg-124"],
  "folder": "",
  "add": ["\\Seen"],
  "remove": ["\\Flagged"]
}
```

`id`/`ids` accept up to 1000 messages in total; `folder` is optional.
Response: `{"updated": ["msg-123", "msg-124"]}`. `400` for unsupported flags
or a flag both added and removed, `404` when a message is not found. Send
`Content-Type: application/json`; another content type is rejected with `400`.

### POST /api/v1/mail/draft, PUT /api/v1/mail/draft

Save a draft into the Drafts folder. `PUT` requires `id` (the draft to
update); `POST` creates a draft, or replaces one when `id` is given.

```json
{
  "id": "",
  "to": ["recipient@example.com"],
  "cc": [],
  "bcc": [],
  "subject": "Draft",
  "body": "Text"
}
```

Response: `{"id": "<current id>", "folder": "Drafts", "saved": true}`. The
id changes when the draft content changes (bodies are content-addressed).
At most 100 recipients. `507 Insufficient Storage` when saving the changed draft
would exceed the account's quota.

### POST /api/v1/mail/move

Move messages between folders, keeping flags (like IMAP MOVE).

```json
{
  "id": "msg-123",
  "ids": ["msg-124"],
  "from": "INBOX",
  "to": "Archive"
}
```

`404` folder or message not found, `409` message already exists in the
destination, `400` when `from` equals `to`.

### DELETE /api/v1/mail/delete?id=msg-123

Delete a message by id (query `id`, or form value `id` with `POST`).

## Contacts

The authenticated user's CardDAV address books: contacts added here appear in
CardDAV clients (phones, Thunderbird) and vice versa. A contact is created in
the `contacts` address book, which is created on first use. Editing a contact
replaces its name, emails, phones, organization and note and keeps every other
vCard property (photo, addresses, birthday).

### GET /api/v1/contacts

Lists all contacts, sorted by name. `?q=` keeps those whose name,
organization, email or phone contains the text (case-insensitive).

```json
{"contacts": [{"uid": "7f3c…", "addressbook": "contacts", "name": "Jane Doe",
  "emails": ["jane@example.com"], "phones": ["+1 555 0100"], "organization": "Acme"}]}
```

### POST /api/v1/contacts

```json
{"name": "Jane Doe", "emails": ["jane@example.com"], "phones": ["+1 555 0100"],
 "organization": "Acme", "note": ""}
```

A name or an email address is required; emails must be bare addresses. `201`
with the created contact.

### PUT /api/v1/contacts/{addressbook}/{uid}

Same body as POST. `200` with the updated contact, `404` when it does not
exist, `403` for a read-only address book.

### DELETE /api/v1/contacts/{addressbook}/{uid}

`204`, or `404` when the contact does not exist.

## Search

### GET /api/v1/search

Full-text search over the caller's messages (`internal/api/server_search.go`).

**Query Parameters:**
- `q` - Search query (required, max 500 characters)
- `folder` - Restrict to a folder (inbox, sent, drafts, trash, spam or a mailbox name)
- `limit` - Page size (default 20, max 100)
- `offset` - Results to skip

There are no `/api/v1/folders` endpoints; folders are selected with the
`folder` parameter or the `/api/v1/mail/<folder>` list routes.

## Queue Management (Admin)

### GET /api/v1/admin/queue

List undelivered queue entries (pending, sending, failed), oldest first. The
same handler is also mounted at `/api/v1/queue`.

**Query Parameters:**
- `status` - `pending`, `sending` or `failed` (anything else is `400`)
- `limit` - 1-1000 (default 100)
- `offset` - Entries to skip (default 0)

**Response:** a JSON array:
```json
[
  {
    "id": "queue-123",
    "from": "user@example.com",
    "to": ["recipient@other.com"],
    "status": "pending",
    "retry_count": 3,
    "last_error": "Connection refused",
    "created_at": "2024-01-15T10:00:00Z",
    "next_retry": "2024-01-15T11:00:00Z"
  }
]
```

### GET /api/v1/admin/queue/{id}

One entry (same fields); `404` when it does not exist.

### POST /api/v1/admin/queue/{id}

Retry a queued message (there is no `/retry` suffix). The retry counter is
reset and the entry becomes `pending` immediately. Returns `{"status":
"retrying"}`; `404` when it does not exist and `409` when the entry is
`sending`, `delivered` or `bounced`.

### DELETE /api/v1/admin/queue/{id}

Remove a message from the queue.

## Statistics (Admin)

### GET /api/v1/stats

Server statistics (admin only, `internal/api/server_stats.go`).

**Response:**
```json
{
  "domains": 5,
  "accounts": 125,
  "messages": 0,
  "queue_size": 23
}
```

`messages` is currently always `0`. There is no IP blocklist API; IP rate
limits are managed under `/api/v1/admin/ratelimits/*`.

## Server Settings (Admin)

### GET /api/v1/admin/settings

The configuration the server is running with, as shown on the admin
Settings page. Read-only: settings change in the config file and take effect
on restart. Secrets (JWT and TOTP keys, passwords, tokens, alert targets) are
never included.

```json
{
  "config_file": "/etc/umailserver/umailserver.yaml",
  "hostname": "mail.example.com",
  "data_dir": "/var/lib/umailserver",
  "listeners": [{"name": "SMTP (MX)", "enabled": true, "bind": "0.0.0.0", "port": 25}],
  "max_message_size": 52428800,
  "max_recipients": 100,
  "tls": {"min_version": "1.2", "acme": true, "manual_cert": false, "client_auth": false},
  "security": {"max_login_attempts": 5, "lockout_duration": "15m0s", "submission_require_tls": true,
               "ip_per_hour": 500, "user_per_hour": 1000, "http_requests_per_minute": 120, "audit_log": true},
  "spam": {"enabled": true, "reject_threshold": 9, "junk_threshold": 3, "greylisting": true,
           "bayesian": true, "auto_train": true, "rbl_servers": ["zen.spamhaus.org"],
           "antivirus": false, "dmarc_reports": false},
  "notifications": {"alerts": false, "alert_email": false, "alert_webhook": false, "push": true}
}
```

`config_file` is empty when the server runs on built-in defaults.

## Error Responses

Errors are a JSON object with a human-readable message string
(`sendError` in `internal/api/server.go` and `mail.go`); there are no error
codes or `details` object:

```json
{ "error": "access denied" }
```

### Common Status Codes

| HTTP Status | Meaning |
|-------------|---------|
| 400 | Invalid request parameters or body |
| 401 | Invalid or missing credentials |
| 403 | Insufficient permissions (non-admin on an admin route, wrong current password) |
| 404 | Resource not found |
| 405 | Method not allowed; an `Allow` header lists the permitted methods |
| 409 | Conflict (duplicate resource, message already in the move destination, deleting the last active administrator) |
| 429 | Too many requests or failed attempts; carries `Retry-After: 60` |
| 507 | Quota exceeded while storing a sent message or draft |
| 500 | Internal server error |

## Rate Limiting

API requests are rate limited per client IP in a one-minute window. The
limit is `security.rate_limit.http_requests_per_minute` (default 120; 0 disables it).
`/health` and `/health/ready` are exempt. An exceeded limit returns `429` with
`Retry-After: 60`; no `X-RateLimit-*` headers are sent. Client IPs behind a
reverse proxy are taken from `X-Forwarded-For` only for peers listed in
`http.trusted_proxies`. Login and password-guess lockouts are tracked
separately per account and also answer `429`.

## Server-Sent Events (Real-time)

Real-time updates use Server-Sent Events (not WebSocket) at
`GET /api/v1/events` (`internal/api/server.go`, `internal/websocket/sse.go`).

```javascript
const es = new EventSource('/api/v1/events');
es.addEventListener('new_mail', (e) => console.log(JSON.parse(e.data)));
```

Event types: `connected`, `heartbeat`, `new_mail`, `expunge`,
`flags_changed`, `folder_update`.
