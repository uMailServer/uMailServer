# uMailServer API Specification

> OpenAPI 3.0.3 specification for uMailServer REST API
> Version: 1.0.0

```yaml
openapi: 3.0.3
info:
  title: uMailServer API
  description: |
    REST API for uMailServer - A modern, secure email server with webmail, admin panel, and full email protocol support.

    ## Authentication
    The API supports two authentication methods:
    1. **HttpOnly Cookie** (Web clients): JWT token stored in HttpOnly cookie, sent automatically with requests
    2. **Bearer Token** (API clients): JWT token passed in `Authorization: Bearer <token>` header

    ## Rate Limiting
    API requests are rate-limited per IP address and per user:
    - Login attempts: 5 per minute
    - General API: 100 requests per minute per user
    - Admin API: 60 requests per minute per admin

    ## Error Responses
    All errors follow the standard format:
    ```json
    {
      "error": "Error message description",
      "code": "ERROR_CODE"
    }
    ```

    ## Pagination
    List endpoints support pagination via `offset` and `limit` query parameters:
    - `offset`: Number of items to skip (default: 0)
    - `limit`: Maximum items to return (default: 50, max: 500)

  version: 1.0.0
  contact:
    name: uMailServer Team
  license:
    name: MIT

servers:
  - url: https://{hostname}/api/v1
    variables:
      hostname:
        default: localhost
        description: Server hostname

security:
  - bearerAuth: []
  - cookieAuth: []

tags:
  - name: Authentication
    description: User authentication and session management
  - name: Mail
    description: Email operations (send, receive, manage)
  - name: Folders
    description: Mailbox folder management
  - name: Filters
    description: Sieve filter rules
  - name: Vacation
    description: Auto-reply/vacation message settings
  - name: Push Notifications
    description: WebPush subscription management
  - name: Search
    description: Full-text email search
  - name: Threads
    description: Email thread management
  - name: Admin - Domains
    description: Domain administration (admin only)
  - name: Admin - Accounts
    description: Account administration (admin only)
  - name: Admin - Aliases
    description: Email alias administration (admin only)
  - name: Admin - Queue
    description: Mail queue management (admin only)
  - name: Admin - Webhooks
    description: Webhook configuration (admin only)
  - name: Health
    description: Health and readiness checks

paths:
  # ============================================================================
  # Authentication
  # ============================================================================
  /auth/login:
    post:
      tags: [Authentication]
      summary: User login
      description: Authenticate a user and receive a JWT token
      security: []  # No auth required for login
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/LoginRequest'
      responses:
        '200':
          description: Login successful
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/LoginResponse'
          headers:
            Set-Cookie:
              description: HttpOnly JWT cookie (when using cookie-based auth)
              schema:
                type: string
        '401':
          description: Invalid credentials
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Error'
        '429':
          description: Too many login attempts

  /auth/logout:
    post:
      tags: [Authentication]
      summary: User logout
      description: Invalidate the current session/token
      responses:
        '200':
          description: Logout successful

  /auth/refresh:
    post:
      tags: [Authentication]
      summary: Refresh JWT token
      description: Get a new JWT token before the current one expires
      responses:
        '200':
          description: Token refreshed successfully
          content:
            application/json:
              schema:
                type: object
                properties:
                  token:
                    type: string
                    description: New JWT token

  /auth/me:
    get:
      tags: [Authentication]
      summary: Get current user info
      description: Retrieve information about the currently authenticated user
      responses:
        '200':
          description: User information
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'

  # ============================================================================
  # Mail Operations
  # ============================================================================
  /mail/{folder}:
    get:
      tags: [Mail]
      summary: List emails in folder
      description: Get paginated list of emails in a folder (inbox, sent, drafts, trash, etc.)
      parameters:
        - name: folder
          in: path
          required: true
          schema:
            type: string
            enum: [inbox, sent, drafts, trash, spam, archive]
        - name: offset
          in: query
          schema:
            type: integer
            default: 0
        - name: limit
          in: query
          schema:
            type: integer
            default: 50
            maximum: 500
      responses:
        '200':
          description: List of emails
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/EmailList'

  /mail/send:
    post:
      tags: [Mail]
      summary: Send email
      description: Send an email to specified recipients
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/SendEmailRequest'
      responses:
        '200':
          description: Email sent successfully
          content:
            application/json:
              schema:
                type: object
                properties:
                  message:
                    type: string
                    example: "Email sent successfully"
        '400':
          description: Invalid request (no recipients, subject too long, etc.)
        '429':
          description: Rate limit exceeded (too many emails sent)

  /mail/{id}:
    get:
      tags: [Mail]
      summary: Get email by ID
      description: Retrieve full email content including body and attachments
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Email details
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Email'
        '404':
          description: Email not found

    delete:
      tags: [Mail]
      summary: Delete email
      description: Move email to trash or permanently delete
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
        - name: permanent
          in: query
          schema:
            type: boolean
            default: false
      responses:
        '204':
          description: Email deleted

  /mail/{id}/read:
    put:
      tags: [Mail]
      summary: Mark email as read/unread
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                read:
                  type: boolean
      responses:
        '200':
          description: Read status updated

  /mail/{id}/star:
    put:
      tags: [Mail]
      summary: Star/unstar email
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                starred:
                  type: boolean
      responses:
        '200':
          description: Star status updated

  /mail/{id}/move:
    post:
      tags: [Mail]
      summary: Move email to folder
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                folder:
                  type: string
                  description: Target folder name
      responses:
        '200':
          description: Email moved

  /mail/attachments/{id}:
    get:
      tags: [Mail]
      summary: Download attachment
      description: Download email attachment by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Attachment file
          content:
            application/octet-stream:
              schema:
                type: string
                format: binary

  # ============================================================================
  # Folders
  # ============================================================================
  /folders:
    get:
      tags: [Folders]
      summary: List folders
      description: Get all mailbox folders for the current user
      responses:
        '200':
          description: List of folders
          content:
            application/json:
              schema:
                type: object
                properties:
                  folders:
                    type: array
                    items:
                      $ref: '#/components/schemas/Folder'

    post:
      tags: [Folders]
      summary: Create folder
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                  pattern: '^[a-zA-Z0-9_.-]+$'
      responses:
        '201':
          description: Folder created

  /folders/{name}:
    delete:
      tags: [Folders]
      summary: Delete folder
      parameters:
        - name: name
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: Folder deleted

  /folders/{name}/rename:
    put:
      tags: [Folders]
      summary: Rename folder
      parameters:
        - name: name
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                newName:
                  type: string
      responses:
        '200':
          description: Folder renamed

  # ============================================================================
  # Filters
  # ============================================================================
  /filters:
    get:
      tags: [Filters]
      summary: List filters
      description: Get all Sieve filter rules for the current user
      responses:
        '200':
          description: List of filters
          content:
            application/json:
              schema:
                type: object
                properties:
                  filters:
                    type: array
                    items:
                      $ref: '#/components/schemas/Filter'

    post:
      tags: [Filters]
      summary: Create filter
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/FilterInput'
      responses:
        '201':
          description: Filter created
          content:
            application/json:
              schema:
                type: object
                properties:
                  filter:
                    $ref: '#/components/schemas/Filter'

  /filters/{id}:
    put:
      tags: [Filters]
      summary: Update filter
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/FilterInput'
      responses:
        '200':
          description: Filter updated

    delete:
      tags: [Filters]
      summary: Delete filter
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: Filter deleted

  /filters/reorder:
    post:
      tags: [Filters]
      summary: Reorder filters
      description: Change the execution order of filters
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                order:
                  type: array
                  items:
                    type: string
                  description: Array of filter IDs in desired order
      responses:
        '200':
          description: Order updated

  # ============================================================================
  # Vacation Auto-Reply
  # ============================================================================
  /vacation:
    get:
      tags: [Vacation]
      summary: Get vacation settings
      responses:
        '200':
          description: Vacation settings
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/VacationSettings'

    post:
      tags: [Vacation]
      summary: Set vacation settings
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/VacationSettings'
      responses:
        '200':
          description: Settings updated

    delete:
      tags: [Vacation]
      summary: Disable vacation
      description: Turn off vacation auto-reply
      responses:
        '204':
          description: Vacation disabled

  # ============================================================================
  # Push Notifications
  # ============================================================================
  /push/vapid-public-key:
    get:
      tags: [Push Notifications]
      summary: Get VAPID public key
      description: Get the server's VAPID public key for WebPush subscription
      responses:
        '200':
          description: VAPID public key
          content:
            application/json:
              schema:
                type: object
                properties:
                  key:
                    type: string

  /push/subscribe:
    post:
      tags: [Push Notifications]
      summary: Subscribe to push notifications
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/PushSubscription'
      responses:
        '201':
          description: Subscription created

  /push/unsubscribe:
    delete:
      tags: [Push Notifications]
      summary: Unsubscribe from push notifications
      parameters:
        - name: endpoint
          in: query
          required: true
          schema:
            type: string
            format: uri
      responses:
        '204':
          description: Subscription removed

  /push/subscriptions:
    get:
      tags: [Push Notifications]
      summary: List push subscriptions
      description: Get all active push subscriptions for the current user
      responses:
        '200':
          description: List of subscriptions
          content:
            application/json:
              schema:
                type: object
                properties:
                  subscriptions:
                    type: array
                    items:
                      $ref: '#/components/schemas/PushSubscription'

  # ============================================================================
  # Search
  # ============================================================================
  /search:
    get:
      tags: [Search]
      summary: Search emails
      description: Full-text search across all emails
      parameters:
        - name: q
          in: query
          required: true
          schema:
            type: string
          description: Search query
        - name: folder
          in: query
          schema:
            type: string
          description: Limit search to specific folder
        - name: from
          in: query
          schema:
            type: string
          description: Filter by sender
        - name: to
          in: query
          schema:
            type: string
          description: Filter by recipient
        - name: subject
          in: query
          schema:
            type: string
          description: Filter by subject
        - name: date_from
          in: query
          schema:
            type: string
            format: date
        - name: date_to
          in: query
          schema:
            type: string
            format: date
        - name: has_attachments
          in: query
          schema:
            type: boolean
        - name: offset
          in: query
          schema:
            type: integer
            default: 0
        - name: limit
          in: query
          schema:
            type: integer
            default: 50
      responses:
        '200':
          description: Search results
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/SearchResponse'

  # ============================================================================
  # Threads
  # ============================================================================
  /threads:
    get:
      tags: [Threads]
      summary: List threads
      description: Get email conversation threads
      parameters:
        - name: folder
          in: query
          schema:
            type: string
            default: inbox
        - name: offset
          in: query
          schema:
            type: integer
            default: 0
        - name: limit
          in: query
          schema:
            type: integer
            default: 50
      responses:
        '200':
          description: List of threads
          content:
            application/json:
              schema:
                type: object
                properties:
                  threads:
                    type: array
                    items:
                      $ref: '#/components/schemas/Thread'

  /threads/{id}:
    get:
      tags: [Threads]
      summary: Get thread by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Thread details
          content:
            application/json:
              schema:
                type: object
                properties:
                  thread:
                    $ref: '#/components/schemas/Thread'

  # ============================================================================
  # Admin - Domains
  # ============================================================================
  /admin/domains:
    get:
      tags: [Admin - Domains]
      summary: List domains
      description: Get all domains (admin only)
      responses:
        '200':
          description: List of domains
          content:
            application/json:
              schema:
                type: object
                properties:
                  domains:
                    type: array
                    items:
                      $ref: '#/components/schemas/Domain'

    post:
      tags: [Admin - Domains]
      summary: Create domain
      description: Add a new domain (admin only)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/DomainInput'
      responses:
        '201':
          description: Domain created

  /admin/domains/{name}:
    get:
      tags: [Admin - Domains]
      summary: Get domain details
      parameters:
        - name: name
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Domain details
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Domain'

    put:
      tags: [Admin - Domains]
      summary: Update domain
      parameters:
        - name: name
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/DomainInput'
      responses:
        '200':
          description: Domain updated

    delete:
      tags: [Admin - Domains]
      summary: Delete domain
      parameters:
        - name: name
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: Domain deleted

  # ============================================================================
  # Admin - Accounts
  # ============================================================================
  /admin/accounts:
    get:
      tags: [Admin - Accounts]
      summary: List accounts
      description: Get all accounts or filter by domain (admin only)
      parameters:
        - name: domain
          in: query
          schema:
            type: string
      responses:
        '200':
          description: List of accounts
          content:
            application/json:
              schema:
                type: object
                properties:
                  accounts:
                    type: array
                    items:
                      $ref: '#/components/schemas/Account'

    post:
      tags: [Admin - Accounts]
      summary: Create account
      description: Create a new email account (admin only)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/AccountInput'
      responses:
        '201':
          description: Account created

  /admin/accounts/{email}:
    get:
      tags: [Admin - Accounts]
      summary: Get account details
      parameters:
        - name: email
          in: path
          required: true
          schema:
            type: string
            format: email
      responses:
        '200':
          description: Account details

    put:
      tags: [Admin - Accounts]
      summary: Update account
      parameters:
        - name: email
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/AccountInput'
      responses:
        '200':
          description: Account updated

    delete:
      tags: [Admin - Accounts]
      summary: Delete account
      parameters:
        - name: email
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: Account deleted

  /admin/accounts/{email}/password:
    put:
      tags: [Admin - Accounts]
      summary: Change account password
      parameters:
        - name: email
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                password:
                  type: string
                  minLength: 8
      responses:
        '200':
          description: Password changed

  /admin/accounts/{email}/quota:
    put:
      tags: [Admin - Accounts]
      summary: Set account quota
      parameters:
        - name: email
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                quota_bytes:
                  type: integer
                  description: Quota in bytes (0 = unlimited)
      responses:
        '200':
          description: Quota updated

  # ============================================================================
  # Admin - Aliases
  # ============================================================================
  /admin/aliases:
    get:
      tags: [Admin - Aliases]
      summary: List aliases
      description: Get all email aliases (admin only)
      parameters:
        - name: domain
          in: query
          schema:
            type: string
      responses:
        '200':
          description: List of aliases
          content:
            application/json:
              schema:
                type: object
                properties:
                  aliases:
                    type: array
                    items:
                      $ref: '#/components/schemas/Alias'

    post:
      tags: [Admin - Aliases]
      summary: Create alias
      description: Create a new email alias (admin only)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/AliasInput'
      responses:
        '201':
          description: Alias created

  /admin/aliases/{id}:
    delete:
      tags: [Admin - Aliases]
      summary: Delete alias
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: Alias deleted

  # ============================================================================
  # Admin - Queue
  # ============================================================================
  /admin/queue:
    get:
      tags: [Admin - Queue]
      summary: Get queue statistics
      description: Get mail queue statistics (admin only)
      responses:
        '200':
          description: Queue statistics
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/QueueStats'

  /admin/queue/entries:
    get:
      tags: [Admin - Queue]
      summary: List queue entries
      description: Get pending/failed queue entries (admin only)
      parameters:
        - name: status
          in: query
          schema:
            type: string
            enum: [pending, sending, failed, delivered, bounced]
        - name: offset
          in: query
          schema:
            type: integer
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        '200':
          description: Queue entries

  /admin/queue/flush:
    post:
      tags: [Admin - Queue]
      summary: Flush queue
      description: Retry all failed/pending messages immediately (admin only)
      responses:
        '200':
          description: Queue flush initiated

  /admin/queue/entries/{id}/retry:
    post:
      tags: [Admin - Queue]
      summary: Retry queue entry
      description: Retry a specific queue entry (admin only)
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Retry initiated

  /admin/queue/entries/{id}:
    delete:
      tags: [Admin - Queue]
      summary: Drop queue entry
      description: Remove a message from the queue (admin only)
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: Entry removed

  # ============================================================================
  # Admin - Webhooks
  # ============================================================================
  /admin/webhooks:
    get:
      tags: [Admin - Webhooks]
      summary: List webhooks
      description: Get all configured webhooks (admin only)
      responses:
        '200':
          description: List of webhooks
          content:
            application/json:
              schema:
                type: object
                properties:
                  webhooks:
                    type: array
                    items:
                      $ref: '#/components/schemas/Webhook'

    post:
      tags: [Admin - Webhooks]
      summary: Create webhook
      description: Configure a new webhook (admin only)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/WebhookInput'
      responses:
        '201':
          description: Webhook created

  /admin/webhooks/{id}:
    delete:
      tags: [Admin - Webhooks]
      summary: Delete webhook
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: Webhook deleted

  /admin/webhooks/{id}/test:
    post:
      tags: [Admin - Webhooks]
      summary: Test webhook
      description: Send a test event to the webhook (admin only)
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Test event sent

  # ============================================================================
  # Health Checks
  # ============================================================================
  /health:
    get:
      tags: [Health]
      summary: Liveness probe
      description: Check if the server is running
      security: []
      responses:
        '200':
          description: Server is alive
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: "ok"

  /health/ready:
    get:
      tags: [Health]
      summary: Readiness probe
      description: Check if the server is ready to accept traffic (database, queue, etc.)
      security: []
      responses:
        '200':
          description: Server is ready
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: "ready"
                  checks:
                    type: object
        '503':
          description: Server is not ready
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: "not_ready"
                  errors:
                    type: array
                    items:
                      type: string

  /metrics:
    get:
      tags: [Health]
      summary: Prometheus metrics
      description: Get Prometheus-formatted metrics
      security: []
      responses:
        '200':
          description: Prometheus metrics
          content:
            text/plain:
              schema:
                type: string

components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
      description: JWT token for API clients

    cookieAuth:
      type: apiKey
      in: cookie
      name: token
      description: HttpOnly JWT cookie for web clients

  schemas:
    # ============================================================================
    # Authentication
    # ============================================================================
    LoginRequest:
      type: object
      required: [email, password]
      properties:
        email:
          type: string
          format: email
        password:
          type: string
          format: password
          minLength: 1

    LoginResponse:
      type: object
      properties:
        token:
          type: string
          description: JWT token (when not using cookie auth)
        user:
          $ref: '#/components/schemas/User'

    User:
      type: object
      properties:
        email:
          type: string
          format: email
        is_admin:
          type: boolean
        quota_used:
          type: integer
        quota_limit:
          type: integer

    # ============================================================================
    # Email
    # ============================================================================
    Email:
      type: object
      properties:
        id:
          type: string
        from:
          type: string
        from_name:
          type: string
        to:
          type: array
          items:
            type: string
        subject:
          type: string
        body:
          type: string
        preview:
          type: string
        date:
          type: string
          format: date-time
        read:
          type: boolean
        starred:
          type: boolean
        folder:
          type: string
        has_attachments:
          type: boolean
        size:
          type: integer
        attachments:
          type: array
          items:
            $ref: '#/components/schemas/Attachment'

    EmailList:
      type: object
      properties:
        emails:
          type: array
          items:
            $ref: '#/components/schemas/Email'
        total:
          type: integer
        offset:
          type: integer
        limit:
          type: integer

    SendEmailRequest:
      type: object
      required: [to, subject]
      properties:
        to:
          type: array
          items:
            type: string
            format: email
          minItems: 1
        cc:
          type: array
          items:
            type: string
            format: email
        bcc:
          type: array
          items:
            type: string
            format: email
        subject:
          type: string
          maxLength: 998
        body:
          type: string
        attachments:
          type: array
          items:
            $ref: '#/components/schemas/AttachmentInput'

    Attachment:
      type: object
      properties:
        id:
          type: string
        filename:
          type: string
        content_type:
          type: string
        size:
          type: integer

    AttachmentInput:
      type: object
      properties:
        filename:
          type: string
        content_type:
          type: string
        data:
          type: string
          format: base64

    # ============================================================================
    # Folders
    # ============================================================================
    Folder:
      type: object
      properties:
        name:
          type: string
        total:
          type: integer
        unread:
          type: integer

    # ============================================================================
    # Filters
    # ============================================================================
    Filter:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        enabled:
          type: boolean
        priority:
          type: integer
        conditions:
          type: array
          items:
            $ref: '#/components/schemas/FilterCondition'
        actions:
          type: array
          items:
            $ref: '#/components/schemas/FilterAction'

    FilterInput:
      type: object
      required: [name, conditions, actions]
      properties:
        name:
          type: string
        enabled:
          type: boolean
        conditions:
          type: array
          items:
            $ref: '#/components/schemas/FilterCondition'
        actions:
          type: array
          items:
            $ref: '#/components/schemas/FilterAction'

    FilterCondition:
      type: object
      required: [field, operator]
      properties:
        field:
          type: string
          enum: [from, to, subject, body, header]
        operator:
          type: string
          enum: [contains, equals, starts_with, ends_with, exists, not_exists]
        value:
          type: string
        header_name:
          type: string

    FilterAction:
      type: object
      required: [type]
      properties:
        type:
          type: string
          enum: [move, copy, label, star, mark_read, forward, reject, discard]
        destination:
          type: string
        label:
          type: string

    # ============================================================================
    # Vacation
    # ============================================================================
    VacationSettings:
      type: object
      properties:
        enabled:
          type: boolean
        subject:
          type: string
        body:
          type: string
        start_date:
          type: string
          format: date
        end_date:
          type: string
          format: date
        contacts_only:
          type: boolean

    # ============================================================================
    # Push Notifications
    # ============================================================================
    PushSubscription:
      type: object
      required: [endpoint, keys]
      properties:
        endpoint:
          type: string
          format: uri
        keys:
          type: object
          properties:
            p256dh:
              type: string
            auth:
              type: string

    # ============================================================================
    # Search & Threads
    # ============================================================================
    SearchResponse:
      type: object
      properties:
        emails:
          type: array
          items:
            $ref: '#/components/schemas/Email'
        total:
          type: integer
        query:
          type: string

    Thread:
      type: object
      properties:
        id:
          type: string
        subject:
          type: string
        participants:
          type: array
          items:
            type: string
        last_date:
          type: string
          format: date-time
        unread:
          type: boolean
        email_count:
          type: integer

    # ============================================================================
    # Admin - Domains
    # ============================================================================
    Domain:
      type: object
      properties:
        name:
          type: string
        max_accounts:
          type: integer
        max_mailbox_size:
          type: integer
        dkim_selector:
          type: string
        dkim_public_key:
          type: string
        catch_all_target:
          type: string
        is_active:
          type: boolean
        created_at:
          type: string
          format: date-time

    DomainInput:
      type: object
      required: [name]
      properties:
        name:
          type: string
        max_accounts:
          type: integer
        max_mailbox_size:
          type: integer
        dkim_selector:
          type: string
        catch_all_target:
          type: string
        is_active:
          type: boolean

    # ============================================================================
    # Admin - Accounts
    # ============================================================================
    Account:
      type: object
      properties:
        email:
          type: string
        local_part:
          type: string
        domain:
          type: string
        is_active:
          type: boolean
        is_admin:
          type: boolean
        quota_used:
          type: integer
        quota_limit:
          type: integer
        created_at:
          type: string
          format: date-time

    AccountInput:
      type: object
      required: [email, password]
      properties:
        email:
          type: string
          format: email
        password:
          type: string
          minLength: 8
        is_active:
          type: boolean
        is_admin:
          type: boolean
        quota_bytes:
          type: integer

    # ============================================================================
    # Admin - Aliases
    # ============================================================================
    Alias:
      type: object
      properties:
        id:
          type: string
        alias:
          type: string
        target:
          type: string
        domain:
          type: string
        is_active:
          type: boolean

    AliasInput:
      type: object
      required: [alias, target]
      properties:
        alias:
          type: string
        target:
          type: string
          format: email
        is_active:
          type: boolean

    # ============================================================================
    # Admin - Queue
    # ============================================================================
    QueueStats:
      type: object
      properties:
        pending:
          type: integer
        sending:
          type: integer
        failed:
          type: integer
        delivered:
          type: integer
        bounced:
          type: integer
        total:
          type: integer

    # ============================================================================
    # Admin - Webhooks
    # ============================================================================
    Webhook:
      type: object
      properties:
        id:
          type: string
        url:
          type: string
          format: uri
        events:
          type: array
          items:
            type: string
        active:
          type: boolean
        created_at:
          type: string
          format: date-time

    WebhookInput:
      type: object
      required: [url, events]
      properties:
        url:
          type: string
          format: uri
        events:
          type: array
          items:
            type: string
            enum: ["*", "mail.received", "mail.sent", "delivery.success", "delivery.failed", "auth.login.success", "auth.login.failed", "spam.detected"]
        active:
          type: boolean

    # ============================================================================
    # Error
    # ============================================================================
    Error:
      type: object
      properties:
        error:
          type: string
        code:
          type: string
```
