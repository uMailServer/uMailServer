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

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2024-01-15T10:30:00Z",
  "user": {
    "id": "user-123",
    "email": "user@example.com",
    "is_admin": false
  }
}
```

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

Refresh an expired access token.

**Request:**
```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

Two-factor authentication is managed through the `/api/v1/account/totp*`
endpoints (see Account endpoints below); there are no `/auth/2fa/*` routes.

## Domain Management (Admin)

### GET /api/v1/domains

List all domains.

**Response:**
```json
{
  "domains": [
    {
      "id": "domain-123",
      "name": "example.com",
      "max_accounts": 100,
      "account_count": 25,
      "is_active": true,
      "created_at": "2024-01-01T00:00:00Z"
    }
  ],
  "total": 1
}
```

### POST /api/v1/domains

Create a new domain.

**Request:**
```json
{
  "name": "newdomain.com",
  "max_accounts": 100,
  "max_mailbox_size": 5368709120
}
```

### GET /api/v1/domains/{domain}

Get domain details.

### PUT /api/v1/domains/{domain}

Update domain settings.

### DELETE /api/v1/domains/{domain}

Delete a domain and all accounts.

### GET /api/v1/domains/{domain}/dns

Get required DNS records for the domain.

**Response:**
```json
{
  "records": {
    "mx": "example.com. IN MX 10 mail.example.com.",
    "spf": "example.com. IN TXT \"v=spf1 mx ~all\"",
    "dkim": "default._domainkey.example.com. IN TXT \"v=DKIM1; k=rsa; p=...\"",
    "dmarc": "_dmarc.example.com. IN TXT \"v=DMARC1; p=quarantine; rua=mailto:dmarc@example.com\""
  }
}
```

## Account Management (Admin)

### GET /api/v1/accounts

List accounts (admin can filter by domain).

**Query Parameters:**
- `domain` - Filter by domain
- `page` - Page number (default: 1)
- `per_page` - Items per page (default: 20)

### POST /api/v1/accounts

Create a new account.

**Request:**
```json
{
  "email": "newuser@example.com",
  "password": "securepassword123",
  "is_admin": false,
  "quota": 1073741824
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
  "quota": 2147483648
}
```

### DELETE /api/v1/accounts/{email}

Delete an account.

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
| `new_password` | string | The new password (at least 8 characters). |

**Response (200):**
```json
{
  "message": "password changed"
}
```

**Errors:** `400` invalid body / new password shorter than 8 characters or
missing fields · `401` missing authentication · `403` wrong
`current_password` · `404` account not found.

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
the `otpauth://` URI for the authenticator app.

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

**Errors:** `400` TOTP not set up (call `/setup` first) or invalid body ·
`401` invalid or already-used code.

### POST /api/v1/account/totp/disable

Disable TOTP and clear the stored secret.

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
  "folder": "INBOX"
}
```

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
not part of the request body.

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
or a flag both added and removed, `404` when a message is not found,
`415` when the body is not JSON.

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
At most 100 recipients.

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

List queue entries.

**Query Parameters:**
- `status` - pending, deferred, failed
- `domain` - Filter by destination domain

**Response:**
```json
{
  "entries": [
    {
      "id": "queue-123",
      "from": "user@example.com",
      "to": ["recipient@other.com"],
      "status": "deferred",
      "next_retry": "2024-01-15T11:00:00Z",
      "attempts": 3,
      "last_error": "Connection refused"
    }
  ]
}
```

### POST /api/v1/admin/queue/{id}/retry

Retry a queued message.

### DELETE /api/v1/admin/queue/{id}

Remove message from queue.

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

## Error Responses

All errors follow this format:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "The request body is invalid",
    "details": {
      "field": "email",
      "issue": "must be a valid email address"
    }
  }
}
```

### Common Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `unauthorized` | 401 | Invalid or missing token |
| `forbidden` | 403 | Insufficient permissions |
| `not_found` | 404 | Resource not found |
| `invalid_request` | 400 | Invalid request parameters |
| `rate_limited` | 429 | Too many requests |
| `server_error` | 500 | Internal server error |

## Rate Limiting

API requests are rate limited per IP and per account:

- Authenticated: 1000 requests per hour
- Unauthenticated: 100 requests per hour

Rate limit headers:
```http
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 999
X-RateLimit-Reset: 1642243200
```

## Server-Sent Events (Real-time)

Real-time updates use Server-Sent Events (not WebSocket) at
`GET /api/v1/events` (`internal/api/server.go`, `internal/websocket/sse.go`).

```javascript
const es = new EventSource('/api/v1/events');
es.addEventListener('new_mail', (e) => console.log(JSON.parse(e.data)));
```

Event types: `connected`, `heartbeat`, `new_mail`, `expunge`,
`flags_changed`, `folder_update`.
