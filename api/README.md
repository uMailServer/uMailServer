# uMailServer API Documentation

This directory contains the OpenAPI 3.0 specification for the uMailServer REST API.

## Files

- `openapi.yaml` - Complete OpenAPI 3.0.3 specification

## Overview

The uMailServer API provides REST endpoints for:

- **Authentication** - JWT-based login/logout, TOTP 2FA setup
- **Domain Management** - Create, update, delete email domains
- **Account Management** - User account CRUD operations
- **Alias Management** - Email alias configuration
- **Queue Management** - Mail queue monitoring and retry
- **Health & Metrics** - Health checks and Prometheus metrics

## Authentication

All API endpoints (except health checks and login) require JWT authentication:

```http
Authorization: Bearer <jwt-token>
```

Obtain a token by calling `POST /api/auth/login` with valid credentials.

## Base URL

```
http://localhost:8080/api
```

## Rate Limiting

- Authenticated requests: 100 requests per minute
- Unauthenticated requests: 20 requests per minute

## Response Format

All responses are JSON with the following structure:

### Success (2xx)
```json
{
  "data": { ... }
}
```

### Error (4xx, 5xx)
```json
{
  "error": "error_code",
  "message": "Human-readable error message",
  "code": 400
}
```

## Common HTTP Status Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 201 | Created |
| 204 | No Content (delete success) |
| 400 | Bad Request |
| 401 | Unauthorized |
| 403 | Forbidden |
| 404 | Not Found |
| 409 | Conflict |
| 429 | Too Many Requests |
| 500 | Internal Server Error |
| 503 | Service Unavailable |

## Using the OpenAPI Spec

### With Swagger UI

```bash
# Using Docker
docker run -p 8080:8080 -e SWAGGER_JSON=/api/openapi.yaml \
  -v $(pwd)/openapi.yaml:/api/openapi.yaml \
  swaggerapi/swagger-ui
```

### With Redoc

```bash
# Using Docker
docker run -p 8080:80 -e SPEC_URL=/api/openapi.yaml \
  -v $(pwd)/openapi.yaml:/usr/share/nginx/html/api/openapi.yaml \
  redocly/redoc
```

### Code Generation

Generate client libraries using OpenAPI Generator:

```bash
# Generate Go client
openapi-generator-cli generate -i openapi.yaml -g go -o ./client-go

# Generate Python client
openapi-generator-cli generate -i openapi.yaml -g python -o ./client-python

# Generate TypeScript client
openapi-generator-cli generate -i openapi.yaml -g typescript-fetch -o ./client-ts
```

## API Endpoints Summary

### Authentication
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/auth/login` | Authenticate user |
| POST | `/api/auth/logout` | Logout user |
| POST | `/api/auth/refresh` | Refresh JWT token |
| POST | `/api/auth/totp/setup` | Setup TOTP 2FA |
| POST | `/api/auth/totp/verify` | Verify TOTP code |

### Health & Metrics
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Full health check |
| GET | `/health/live` | Liveness probe |
| GET | `/health/ready` | Readiness probe |
| GET | `/metrics` | Prometheus metrics |

### Admin
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/dashboard` | Dashboard statistics |

### Domains
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/domains` | List all domains |
| POST | `/api/admin/domains` | Create domain |
| GET | `/api/admin/domains/{domain}` | Get domain details |
| PUT | `/api/admin/domains/{domain}` | Update domain |
| DELETE | `/api/admin/domains/{domain}` | Delete domain |

### Accounts
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/domains/{domain}/accounts` | List accounts |
| POST | `/api/admin/domains/{domain}/accounts` | Create account |
| GET | `/api/admin/domains/{domain}/accounts/{localPart}` | Get account |
| PUT | `/api/admin/domains/{domain}/accounts/{localPart}` | Update account |
| DELETE | `/api/admin/domains/{domain}/accounts/{localPart}` | Delete account |
| GET | `/api/account/me` | Get current account |
| PUT | `/api/account/me` | Update current account |

### Aliases
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/domains/{domain}/aliases` | List aliases |
| POST | `/api/admin/domains/{domain}/aliases` | Create alias |
| GET | `/api/admin/domains/{domain}/aliases/{alias}` | Get alias |
| PUT | `/api/admin/domains/{domain}/aliases/{alias}` | Update alias |
| DELETE | `/api/admin/domains/{domain}/aliases/{alias}` | Delete alias |

### Queue
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/queue` | Queue statistics |
| GET | `/api/admin/queue/messages` | List queued messages |
| POST | `/api/admin/queue/messages/{id}/retry` | Retry message |
| DELETE | `/api/admin/queue/messages/{id}` | Remove from queue |
