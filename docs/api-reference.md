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
GET /api/v1/mail/messages
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

### POST /api/v1/auth/2fa/setup

Setup two-factor authentication.

**Response:**
```json
{
  "secret": "JBSWY3DPEHPK3PXP",
  "qr_code": "data:image/png;base64,...",
  "backup_codes": ["12345678", "87654321"]
}
```

### POST /api/v1/auth/2fa/verify

Verify TOTP code and enable 2FA.

**Request:**
```json
{
  "code": "123456"
}
```

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

Reset account password.

## Mail Endpoints

### GET /api/v1/mail/messages

List messages in a folder.

**Query Parameters:**
- `folder` - Folder name (default: INBOX)
- `page` - Page number
- `per_page` - Items per page
- `sort` - Sort field (date, from, subject)
- `order` - asc or desc

**Response:**
```json
{
  "messages": [
    {
      "id": "msg-123",
      "uid": 456,
      "folder": "INBOX",
      "flags": ["\\Seen"],
      "from": "sender@example.com",
      "to": ["user@example.com"],
      "subject": "Hello World",
      "date": "2024-01-15T10:30:00Z",
      "size": 2048,
      "has_attachments": false,
      "preview": "This is a preview of the message..."
    }
  ],
  "total": 150,
  "unread": 23
}
```

### GET /api/v1/mail/messages/{id}

Get full message details.

**Response:**
```json
{
  "id": "msg-123",
  "uid": 456,
  "folder": "INBOX",
  "flags": ["\\Seen", "\\Answered"],
  "from": {
    "name": "John Doe",
    "email": "john@example.com"
  },
  "to": [
    {
      "name": "",
      "email": "user@example.com"
    }
  ],
  "cc": [],
  "bcc": [],
  "subject": "Hello World",
  "date": "2024-01-15T10:30:00Z",
  "size": 2048,
  "body_html": "<html><body>...</body></html>",
  "body_text": "Plain text version...",
  "attachments": [
    {
      "filename": "document.pdf",
      "size": 1048576,
      "content_type": "application/pdf"
    }
  ],
  "headers": {
    "Message-ID": "<msg123@example.com>"
  }
}
```

### POST /api/v1/mail/messages

Send a new message.

**Request:**
```json
{
  "to": ["recipient@example.com"],
  "cc": ["cc@example.com"],
  "bcc": ["bcc@example.com"],
  "subject": "Hello",
  "body_html": "<p>HTML content</p>",
  "body_text": "Plain text content",
  "attachments": [
    {
      "filename": "file.pdf",
      "content": "base64encoded...",
      "content_type": "application/pdf"
    }
  ]
}
```

### PUT /api/v1/mail/messages/{id}/flags

Update message flags.

**Request:**
```json
{
  "flags": ["\\Seen", "\\Flagged"],
  "action": "set"  // set, add, remove
}
```

### PUT /api/v1/mail/messages/{id}/move

Move message to another folder.

**Request:**
```json
{
  "folder": "Archive"
}
```

### DELETE /api/v1/mail/messages/{id}

Delete a message (moves to Trash or permanent delete).

### GET /api/v1/mail/messages/{id}/raw

Get raw RFC 822 message.

### GET /api/v1/mail/messages/{id}/attachments/{filename}

Download attachment.

## Folders

### GET /api/v1/folders

List all folders.

**Response:**
```json
{
  "folders": [
    {
      "name": "INBOX",
      "display_name": "Inbox",
      "total": 150,
      "unread": 23,
      "special_use": "\\Inbox"
    },
    {
      "name": "Sent",
      "display_name": "Sent",
      "total": 45,
      "unread": 0,
      "special_use": "\\Sent"
    }
  ]
}
```

### POST /api/v1/folders

Create a new folder.

**Request:**
```json
{
  "name": "Projects",
  "parent": null
}
```

### PUT /api/v1/folders/{name}

Rename a folder.

### DELETE /api/v1/folders/{name}

Delete a folder.

## Search

### GET /api/v1/mail/search

Search messages.

**Query Parameters:**
- `q` - Search query
- `folder` - Search in specific folder
- `from` - From address
- `to` - To address
- `subject` - Subject contains
- `after` - After date (ISO 8601)
- `before` - Before date (ISO 8601)
- `has_attachments` - boolean

**Response:**
```json
{
  "results": [
    {
      "id": "msg-123",
      "score": 0.95,
      "highlights": {
        "subject": "...<mark>search</mark>...",
        "body": "...<mark>search</mark> term found..."
      }
    }
  ]
}
```

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

### POST /api/v1/admin/queue/retry-all

Retry all failed messages.

### DELETE /api/v1/admin/queue/{id}

Remove message from queue.

## Blocklist (Admin)

### GET /api/v1/admin/blocklist

List blocked IPs.

### POST /api/v1/admin/blocklist

Block an IP.

**Request:**
```json
{
  "ip": "192.168.1.1",
  "reason": "Spam source",
  "duration": "24h"
}
```

### DELETE /api/v1/admin/blocklist/{ip}

Unblock an IP.

## Statistics (Admin)

### GET /api/v1/admin/stats

Get server statistics.

**Response:**
```json
{
  "uptime": 86400,
  "version": "1.0.0",
  "connections": {
    "smtp": 25,
    "imap": 150,
    "http": 45
  },
  "messages": {
    "received": 10000,
    "sent": 8500,
    "queued": 23
  },
  "storage": {
    "used": 10737418240,
    "total": 53687091200
  },
  "domains": 5,
  "accounts": 125
}
```

### GET /api/v1/admin/stats/mail-volume

Get mail volume over time.

**Query Parameters:**
- `period` - 24h, 7d, 30d

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

## WebSocket (Real-time)

Connect to WebSocket for real-time updates:

```javascript
const ws = new WebSocket('wss://mail.example.com/api/v1/ws');
ws.onopen = () => {
  ws.send(JSON.stringify({
    type: 'auth',
    token: 'eyJhbGciOiJIUzI1NiIs...'
  }));
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  console.log('New message:', message);
};
```

Event types:
- `new_mail` - New message received
- `flags_changed` - Message flags updated
- `folder_update` - Folder counts changed
