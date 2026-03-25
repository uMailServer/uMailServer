# uMailServer — Implementation Guide

## Project Identity Update

| Field | Value |
|-------|-------|
| **GitHub Org** | `github.com/umailserver` |
| **Main Repo** | `github.com/umailserver/umailserver` |
| **Domain** | `umailserver.com` |
| **Go Module** | `github.com/umailserver/umailserver` |
| **Binary Name** | `umailserver` |

---

## Repository Structure

```
github.com/umailserver/
├── umailserver/          # Main Go server (this repo)
├── webmail/              # React webmail SPA (separate build, embedded at compile)
├── admin-ui/             # React admin panel SPA
├── account-ui/           # React account self-service portal
├── docs/                 # Documentation site (umailserver.com)
├── helm-chart/           # Kubernetes Helm chart
└── .github/              # Org-level configs, profile README
```

**Why separate UI repos?**
- Frontend devs can contribute without Go toolchain
- Independent CI/CD for UI (faster iteration)
- `umailserver` repo has a `Makefile` target that pulls pre-built UI bundles and embeds them via `embed.FS`
- For development: `make dev` runs Go server + Vite dev servers with hot reload proxy

---

## Phase 1: Foundation (Weeks 1-4)

### 1.1 Project Skeleton & CLI

**File: `cmd/umailserver/main.go`**

```go
package main

import (
    "fmt"
    "os"
)

var (
    Version   = "dev"
    BuildDate = "unknown"
    GitCommit = "unknown"
)

func main() {
    if len(os.Args) < 2 {
        printUsage()
        os.Exit(1)
    }

    switch os.Args[1] {
    case "serve":
        cmdServe(os.Args[2:])
    case "quickstart":
        cmdQuickstart(os.Args[2:])
    case "domain":
        cmdDomain(os.Args[2:])
    case "account":
        cmdAccount(os.Args[2:])
    case "queue":
        cmdQueue(os.Args[2:])
    case "check":
        cmdCheck(os.Args[2:])
    case "test":
        cmdTest(os.Args[2:])
    case "backup":
        cmdBackup(os.Args[2:])
    case "restore":
        cmdRestore(os.Args[2:])
    case "migrate":
        cmdMigrate(os.Args[2:])
    case "version":
        fmt.Printf("uMailServer %s (%s) built %s\n", Version, GitCommit, BuildDate)
    default:
        fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
        printUsage()
        os.Exit(1)
    }
}
```

**CLI follows Go stdlib `flag` package only.** No cobra, no urfave/cli. Each subcommand gets its own `flag.FlagSet`.

### 1.2 Configuration System

**File: `internal/config/config.go`**

Config is loaded from YAML, with environment variable overrides (`UMAILSERVER_SMTP_PORT=2525` overrides `smtp.inbound.port`).

```go
type Config struct {
    Server   ServerConfig   `yaml:"server"`
    TLS      TLSConfig      `yaml:"tls"`
    SMTP     SMTPConfig     `yaml:"smtp"`
    IMAP     IMAPConfig     `yaml:"imap"`
    POP3     POP3Config     `yaml:"pop3"`
    HTTP     HTTPConfig     `yaml:"http"`
    Admin    AdminConfig    `yaml:"admin"`
    Spam     SpamConfig     `yaml:"spam"`
    Security SecurityConfig `yaml:"security"`
    MCP      MCPConfig      `yaml:"mcp"`
    Domains  []DomainConfig `yaml:"domains"`
}

type ServerConfig struct {
    Hostname string `yaml:"hostname"` // FQDN: mail.example.com
    DataDir  string `yaml:"data_dir"` // /var/lib/umailserver
}

type DomainConfig struct {
    Name           string `yaml:"name"`
    MaxAccounts    int    `yaml:"max_accounts"`
    MaxMailboxSize string `yaml:"max_mailbox_size"` // "5GB"
    DKIM           struct {
        Selector string `yaml:"selector"`
    } `yaml:"dkim"`
}
```

**Config loading order:**
1. Built-in defaults (`internal/config/defaults.go`)
2. Config file (`umailserver.yaml`)
3. Environment variables (`UMAILSERVER_*`)
4. CLI flags (highest priority)

**YAML parser:** Use `gopkg.in/yaml.v3` — it's the de facto standard, maintained, and the only external dep for config.

### 1.3 Storage Layer

**File: `internal/store/maildir.go`**

Maildir++ implementation — the most battle-tested mail storage format:

```go
type MaildirStore struct {
    baseDir string // /var/lib/umailserver/domains
}

// Directory layout per user:
// {baseDir}/{domain}/users/{localpart}/Maildir/
//   new/          — newly delivered, not yet seen by IMAP
//   cur/          — messages seen by IMAP client
//   tmp/          — in-progress deliveries (atomic)
//   .Sent/new/cur/tmp/
//   .Drafts/new/cur/tmp/
//   .Junk/new/cur/tmp/
//   .Trash/new/cur/tmp/
//   .Archive/new/cur/tmp/

// Deliver writes a message atomically: tmp/ → new/
func (s *MaildirStore) Deliver(domain, user, folder string, msg []byte) (string, error) {
    // 1. Generate unique filename: {time}.{pid}.{hostname}:2,{flags}
    // 2. Write to tmp/
    // 3. fsync the file
    // 4. Rename (atomic) to new/ (or cur/ for specific folder delivery)
    // 5. fsync the directory
}

// Fetch reads a message by its Maildir filename
func (s *MaildirStore) Fetch(domain, user, folder, filename string) ([]byte, error)

// Move renames a message file between folders
func (s *MaildirStore) Move(domain, user, fromFolder, toFolder, filename string) error

// SetFlags updates the :2,{flags} suffix on the filename
// S=seen, R=replied, F=flagged, T=trashed, D=draft
func (s *MaildirStore) SetFlags(domain, user, folder, filename string, flags string) error

// List returns all messages in a folder
func (s *MaildirStore) List(domain, user, folder string) ([]MessageInfo, error)

// Delete removes a message file
func (s *MaildirStore) Delete(domain, user, folder, filename string) error

// Quota returns current usage and limit for a user
func (s *MaildirStore) Quota(domain, user string) (used int64, limit int64, err error)
```

**File: `internal/store/metadata.go`**

Embedded key-value store for metadata (message index, search, flags cache):

```go
// Using bbolt (pure Go, no CGO)
// Buckets:
//   messages/{domain}/{user}/{folder} → MessageMeta (UID, flags, size, date, headers cache)
//   index/{domain}/{user}             → Full-text search tokens
//   uidvalidity/{domain}/{user}/{folder} → IMAP UIDVALIDITY counter
//   uidnext/{domain}/{user}/{folder}     → IMAP UIDNEXT counter
```

**bbolt** is the only KV store dependency — pure Go, no CGO, battle-tested (used by etcd, Consul, InfluxDB).

### 1.4 Embedded Database

**File: `internal/db/db.go`**

```go
type DB struct {
    bolt *bbolt.DB
}

// Bucket hierarchy:
// accounts/{domain}/{localpart}     → AccountData (password hash, quota, 2fa, created_at)
// domains/{domain}                  → DomainData (settings, dkim keys)
// queue/{message-id}                → QueueEntry (envelope, retry count, next_retry)
// sessions/{session-id}             → SessionData (IMAP/SMTP state)
// ratelimits/{ip}                   → RateLimitData (counters, timestamps)
// spam/bayes/{domain}/{user}        → BayesianData (token frequencies)
// blocklist/ip/{ip}                 → BlockEntry (reason, expires)
// blocklist/domain/{domain}         → BlockEntry
// metrics/hourly/{timestamp}        → MetricsSnapshot
```

---

## Phase 2: SMTP Server (Weeks 3-6)

### 2.1 SMTP Protocol Implementation

**File: `internal/smtp/server.go`**

```go
type Server struct {
    config    *config.SMTPConfig
    tlsConfig *tls.Config
    store     *store.MaildirStore
    db        *db.DB
    auth      *auth.Authenticator
    spam      *spam.Engine
    queue     *queue.Manager
    pipeline  *Pipeline
}

func (s *Server) ListenAndServe(addr string, implicitTLS bool) error {
    ln, err := net.Listen("tcp", addr)
    if err != nil {
        return err
    }
    if implicitTLS {
        ln = tls.NewListener(ln, s.tlsConfig)
    }
    for {
        conn, err := ln.Accept()
        if err != nil {
            continue
        }
        go s.handleConnection(conn, implicitTLS)
    }
}
```

**File: `internal/smtp/session.go`**

SMTP state machine — implemented as explicit states, not a library:

```go
type Session struct {
    conn      net.Conn
    reader    *bufio.Reader
    writer    *bufio.Writer
    state     smtpState
    tls       bool
    authed    bool
    authUser  string
    ehlo      string
    mailFrom  string
    rcptTo    []string
    data      []byte
    server    *Server
    remoteIP  net.IP
}

type smtpState int

const (
    stateGreeting smtpState = iota
    stateReady              // After EHLO
    stateMailFrom           // After MAIL FROM
    stateRcptTo             // After at least one RCPT TO
    stateData               // During DATA
)

func (s *Session) handle() {
    s.writeLine("220 %s ESMTP uMailServer", s.server.config.Hostname)
    
    for {
        line, err := s.readLine()
        if err != nil {
            return
        }
        cmd, arg := parseCommand(line)
        
        switch cmd {
        case "EHLO", "HELO":
            s.handleEHLO(arg)
        case "STARTTLS":
            s.handleSTARTTLS()
        case "AUTH":
            s.handleAUTH(arg)
        case "MAIL":
            s.handleMAIL(arg)
        case "RCPT":
            s.handleRCPT(arg)
        case "DATA":
            s.handleDATA()
        case "RSET":
            s.handleRSET()
        case "NOOP":
            s.writeLine("250 OK")
        case "QUIT":
            s.writeLine("221 Bye")
            return
        default:
            s.writeLine("502 Command not implemented")
        }
    }
}

func (s *Session) handleEHLO(domain string) {
    s.ehlo = domain
    s.state = stateReady
    s.writeLine("250-%s", s.server.config.Hostname)
    s.writeLine("250-STARTTLS")
    s.writeLine("250-AUTH PLAIN LOGIN CRAM-MD5")
    s.writeLine("250-8BITMIME")
    s.writeLine("250-PIPELINING")
    s.writeLine("250-CHUNKING")
    s.writeLine("250-SMTPUTF8")
    s.writeLine("250-SIZE %d", s.server.config.MaxMessageSize)
    s.writeLine("250 ENHANCEDSTATUSCODES")
}
```

### 2.2 Message Pipeline

**File: `internal/smtp/pipeline.go`**

Every incoming message passes through these stages in order:

```go
type Pipeline struct {
    stages []PipelineStage
}

type PipelineStage interface {
    Name() string
    Process(ctx *MessageContext) PipelineResult
}

type PipelineResult struct {
    Action  PipelineAction // Accept, Reject, Quarantine, Modify
    Code    int            // SMTP response code
    Message string         // Human-readable
    Score   float64        // Spam score contribution
}

type PipelineAction int

const (
    ActionAccept PipelineAction = iota
    ActionReject
    ActionQuarantine
    ActionModify     // Message was modified (e.g., header added)
    ActionContinue   // Continue to next stage
)

// Default pipeline order:
func NewDefaultPipeline(deps PipelineDeps) *Pipeline {
    return &Pipeline{
        stages: []PipelineStage{
            NewRateLimitStage(deps.Security),      // 1. Rate limit check
            NewSPFStage(deps.DNS),                  // 2. SPF verification
            NewDKIMVerifyStage(),                    // 3. DKIM verification
            NewDMARCStage(deps.DNS),                // 4. DMARC policy
            NewARCVerifyStage(),                     // 5. ARC chain validation
            NewGreylistStage(deps.DB),               // 6. Greylisting
            NewRBLStage(deps.DNS, deps.Config),      // 7. RBL/DNSBL lookup
            NewHeuristicStage(deps.Config),          // 8. Heuristic rules
            NewBayesianStage(deps.SpamDB),           // 9. Bayesian classifier
            NewScoreAggregatorStage(deps.Config),    // 10. Final score decision
            NewDeliveryStage(deps.Store, deps.Queue),// 11. Local delivery or relay
        },
    }
}
```

### 2.3 Outbound Queue & Delivery

**File: `internal/queue/manager.go`**

```go
type Manager struct {
    db       *db.DB
    resolver *dns.Resolver
    signer   *auth.DKIMSigner
    tls      *tls.Manager
}

type QueueEntry struct {
    ID          string    `json:"id"`
    From        string    `json:"from"`
    To          []string  `json:"to"`
    MessagePath string    `json:"message_path"` // Path to message file
    CreatedAt   time.Time `json:"created_at"`
    NextRetry   time.Time `json:"next_retry"`
    RetryCount  int       `json:"retry_count"`
    LastError   string    `json:"last_error"`
    Status      string    `json:"status"` // pending, sending, failed, delivered
}

// Enqueue adds a message to the outbound queue
func (m *Manager) Enqueue(from string, to []string, message []byte) (string, error)

// ProcessQueue runs in a goroutine, checking for deliverable messages
func (m *Manager) ProcessQueue(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            entries := m.db.GetPendingQueue(time.Now())
            for _, entry := range entries {
                go m.deliver(entry)
            }
        }
    }
}

// deliver attempts to send a message via SMTP to the recipient's MX
func (m *Manager) deliver(entry QueueEntry) {
    // 1. Group recipients by domain
    // 2. For each domain:
    //    a. MX lookup (with fallback to A/AAAA)
    //    b. Check MTA-STS policy
    //    c. Check DANE/TLSA records
    //    d. Connect to MX (try each in priority order)
    //    e. STARTTLS (enforce based on MTA-STS/DANE)
    //    f. Send message
    // 3. On success: remove from queue, log
    // 4. On temp failure: increment retry, exponential backoff
    //    Retry schedule: 5m, 15m, 30m, 1h, 2h, 4h, 8h, 16h, 24h, 48h
    // 5. On perm failure: generate bounce (DSN), remove from queue
    // 6. After 72 hours of retrying: generate final bounce, remove
}
```

**Retry backoff schedule:**

| Attempt | Delay | Total Elapsed |
|---------|-------|---------------|
| 1 | 5 minutes | 5m |
| 2 | 15 minutes | 20m |
| 3 | 30 minutes | 50m |
| 4 | 1 hour | 1h 50m |
| 5 | 2 hours | 3h 50m |
| 6 | 4 hours | 7h 50m |
| 7 | 8 hours | 15h 50m |
| 8 | 16 hours | 31h 50m |
| 9 | 24 hours | 55h 50m |
| 10 | Final bounce | 72h |

---

## Phase 3: IMAP Server (Weeks 5-8)

### 3.1 IMAP Protocol Implementation

**File: `internal/imap/server.go`**

IMAP is significantly more complex than SMTP — it's a stateful protocol with concurrent mailbox access.

```go
type Server struct {
    config *config.IMAPConfig
    tls    *tls.Config
    store  *store.MaildirStore
    db     *db.DB
    auth   *auth.Authenticator
    
    // Active connections per mailbox (for IDLE notifications)
    mailboxWatchers map[string][]*Session
    watcherMu       sync.RWMutex
}
```

**File: `internal/imap/session.go`**

```go
type Session struct {
    conn     net.Conn
    reader   *bufio.Reader
    writer   *bufio.Writer
    state    imapState
    user     string
    domain   string
    selected *SelectedMailbox
    server   *Server
}

type imapState int

const (
    stateNotAuthenticated imapState = iota
    stateAuthenticated
    stateSelected
    stateLogout
)

type SelectedMailbox struct {
    Name        string
    ReadOnly    bool
    Messages    []MessageMeta
    UIDValidity uint32
    UIDNext     uint32
    Recent      int
    Unseen      int
}

// Command dispatch
func (s *Session) handleCommand(tag, cmd string, args []byte) {
    switch strings.ToUpper(cmd) {
    // Any state
    case "CAPABILITY":
        s.handleCapability(tag)
    case "NOOP":
        s.handleNoop(tag)
    case "LOGOUT":
        s.handleLogout(tag)
    
    // Not authenticated
    case "LOGIN":
        s.handleLogin(tag, args)
    case "AUTHENTICATE":
        s.handleAuthenticate(tag, args)
    case "STARTTLS":
        s.handleStartTLS(tag)
    
    // Authenticated
    case "SELECT":
        s.handleSelect(tag, args, false)
    case "EXAMINE":
        s.handleSelect(tag, args, true)
    case "CREATE":
        s.handleCreate(tag, args)
    case "DELETE":
        s.handleDelete(tag, args)
    case "RENAME":
        s.handleRename(tag, args)
    case "SUBSCRIBE":
        s.handleSubscribe(tag, args)
    case "UNSUBSCRIBE":
        s.handleUnsubscribe(tag, args)
    case "LIST":
        s.handleList(tag, args)
    case "STATUS":
        s.handleStatus(tag, args)
    case "APPEND":
        s.handleAppend(tag, args)
    
    // Selected
    case "CHECK":
        s.handleCheck(tag)
    case "CLOSE":
        s.handleClose(tag)
    case "EXPUNGE":
        s.handleExpunge(tag)
    case "SEARCH":
        s.handleSearch(tag, args, false)
    case "FETCH":
        s.handleFetch(tag, args, false)
    case "STORE":
        s.handleStore(tag, args, false)
    case "COPY":
        s.handleCopy(tag, args, false)
    case "MOVE":
        s.handleMove(tag, args, false)
    case "UID":
        s.handleUID(tag, args) // UID SEARCH/FETCH/STORE/COPY/MOVE
    case "IDLE":
        s.handleIdle(tag)
    case "SORT":
        s.handleSort(tag, args)
    case "THREAD":
        s.handleThread(tag, args)
    
    default:
        s.writeTagged(tag, "BAD", "Unknown command")
    }
}
```

### 3.2 IMAP IDLE (Push Notifications)

Critical for webmail real-time updates:

```go
func (s *Session) handleIdle(tag string) {
    if s.state != stateSelected {
        s.writeTagged(tag, "BAD", "No mailbox selected")
        return
    }
    
    s.writeLine("+ idling")
    
    // Register this session as a watcher for the selected mailbox
    watchKey := fmt.Sprintf("%s/%s/%s", s.domain, s.user, s.selected.Name)
    s.server.addWatcher(watchKey, s)
    defer s.server.removeWatcher(watchKey, s)
    
    // Create notification channel
    notify := make(chan MailboxEvent, 10)
    s.server.subscribe(watchKey, notify)
    defer s.server.unsubscribe(watchKey, notify)
    
    // Wait for either:
    // - A mailbox change event (new message, flag change)
    // - Client sends "DONE"
    // - Timeout (30 minutes)
    timer := time.NewTimer(30 * time.Minute)
    defer timer.Stop()
    
    for {
        select {
        case event := <-notify:
            switch event.Type {
            case EventNewMessage:
                s.writeLine("* %d EXISTS", event.Count)
                s.writeLine("* %d RECENT", event.Recent)
            case EventFlagChange:
                s.writeLine("* %d FETCH (FLAGS (%s))", event.SeqNum, event.Flags)
            case EventExpunge:
                s.writeLine("* %d EXPUNGE", event.SeqNum)
            }
        case <-timer.C:
            // Idle timeout, client must re-IDLE
            break
        }
        
        // Check if client sent DONE
        if s.hasPendingInput() {
            line, _ := s.readLine()
            if strings.ToUpper(strings.TrimSpace(line)) == "DONE" {
                s.writeTagged(tag, "OK", "IDLE terminated")
                return
            }
        }
    }
}
```

### 3.3 IMAP FETCH (Message Retrieval)

The most complex IMAP command — handles partial fetches, body structure, headers:

```go
// FETCH data items the server must support:
// FLAGS          — Message flags (\Seen, \Answered, \Flagged, \Deleted, \Draft)
// INTERNALDATE   — Delivery timestamp
// RFC822.SIZE    — Message size in bytes
// ENVELOPE       — Parsed envelope (from, to, subject, date, message-id, etc.)
// BODY           — MIME body structure
// BODYSTRUCTURE  — Extended MIME body structure
// BODY[section]  — Actual message content (headers, text, specific MIME parts)
// BODY.PEEK[section] — Same as BODY[] but doesn't set \Seen flag

func (s *Session) handleFetch(tag string, args []byte, uid bool) {
    // Parse sequence set (e.g., "1:*", "1,3,5", "1:10")
    // Parse fetch items (e.g., "(FLAGS BODY[HEADER] RFC822.SIZE)")
    // For each message in sequence:
    //   Read from Maildir
    //   Parse MIME structure
    //   Return requested items
    //   Set \Seen flag if BODY[] (not PEEK) was fetched
}
```

---

## Phase 4: Authentication Protocols (Weeks 4-7)

### 4.1 SPF Implementation

**File: `internal/auth/spf.go`**

```go
type SPFResult int

const (
    SPFNone SPFResult = iota
    SPFNeutral
    SPFPass
    SPFFail
    SPFSoftFail
    SPFTempError
    SPFPermError
)

// CheckSPF evaluates SPF for the given sender IP and domain
func CheckSPF(ctx context.Context, ip net.IP, domain string, sender string, resolver *dns.Resolver) (SPFResult, string) {
    // 1. Lookup TXT record for domain
    // 2. Find SPF record (starts with "v=spf1")
    // 3. Evaluate mechanisms in order:
    //    - ip4:, ip6: — IP range match
    //    - a: — A/AAAA record match
    //    - mx: — MX record match
    //    - include: — recursive SPF check
    //    - exists: — DNS existence check
    //    - redirect= — redirect to another domain's SPF
    // 4. Apply qualifier: + (pass), - (fail), ~ (softfail), ? (neutral)
    // 5. Limit DNS lookups to 10 (RFC 7208)
}
```

### 4.2 DKIM Implementation

**File: `internal/auth/dkim.go`**

```go
// DKIMSigner signs outbound messages
type DKIMSigner struct {
    keys map[string]*DKIMKey // domain → key
}

type DKIMKey struct {
    Domain     string
    Selector   string
    PrivateKey crypto.Signer // RSA or Ed25519
    Algorithm  string        // rsa-sha256 or ed25519-sha256
    Headers    []string      // Headers to sign
}

// Sign adds a DKIM-Signature header to the message
func (s *DKIMSigner) Sign(domain string, message []byte) ([]byte, error) {
    key := s.keys[domain]
    
    // 1. Canonicalize headers (relaxed/relaxed)
    // 2. Canonicalize body (relaxed)
    // 3. Hash body (SHA-256)
    // 4. Build DKIM-Signature header (without b= value)
    // 5. Hash headers + DKIM-Signature header
    // 6. Sign hash with private key
    // 7. Base64 encode signature
    // 8. Prepend DKIM-Signature header to message
    
    // Headers to sign (recommended set):
    // From, To, Subject, Date, Message-ID, MIME-Version, 
    // Content-Type, Content-Transfer-Encoding, Reply-To, CC
}

// DKIMVerifier verifies DKIM signatures on inbound messages
type DKIMVerifier struct {
    resolver *dns.Resolver
}

// Verify checks all DKIM-Signature headers in the message
func (v *DKIMVerifier) Verify(message []byte) ([]DKIMResult, error) {
    // 1. Extract all DKIM-Signature headers
    // 2. For each signature:
    //    a. Parse signature fields (d=, s=, h=, b=, bh=, a=, c=)
    //    b. Lookup public key: {selector}._domainkey.{domain} TXT record
    //    c. Canonicalize body, compute hash, compare with bh=
    //    d. Canonicalize signed headers, verify signature with public key
    // 3. Return results per signature
}
```

### 4.3 DMARC Implementation

**File: `internal/auth/dmarc.go`**

```go
// CheckDMARC evaluates DMARC policy for a message
func CheckDMARC(ctx context.Context, fromDomain string, spfResult SPFResult, spfDomain string, 
    dkimResults []DKIMResult, resolver *dns.Resolver) DMARCResult {
    
    // 1. Lookup _dmarc.{fromDomain} TXT record
    // 2. Parse policy: p= (none|quarantine|reject), sp=, rua=, ruf=, pct=, adkim=, aspf=
    // 3. Alignment check:
    //    - SPF alignment: MAIL FROM domain matches From header domain (relaxed: org domain)
    //    - DKIM alignment: DKIM d= domain matches From header domain (relaxed: org domain)
    // 4. DMARC passes if either SPF or DKIM is aligned AND passes
    // 5. Apply policy based on result and pct= value
}
```

### 4.4 MTA-STS & DANE

**File: `internal/auth/mtasts.go`**

```go
// CheckMTASTS fetches and evaluates MTA-STS policy for outbound delivery
func CheckMTASTS(ctx context.Context, domain string, resolver *dns.Resolver) (*MTASTSPolicy, error) {
    // 1. Check _mta-sts.{domain} TXT record for policy ID
    // 2. Fetch https://mta-sts.{domain}/.well-known/mta-sts.txt
    // 3. Parse policy: version, mode (enforce|testing|none), mx patterns, max_age
    // 4. Cache policy for max_age seconds
    // 5. On outbound delivery: if mode=enforce, require valid TLS to listed MX
}
```

**File: `internal/auth/dane.go`**

```go
// CheckDANE verifies TLS certificate against TLSA DNS record
func CheckDANE(ctx context.Context, host string, port int, cert *x509.Certificate, resolver *dns.Resolver) (bool, error) {
    // 1. Lookup _port._tcp.{host} TLSA record
    // 2. Parse: usage, selector, matching-type, certificate-data
    // 3. Verify certificate matches TLSA record
    //    Usage 2 (DANE-TA): Trust anchor
    //    Usage 3 (DANE-EE): End entity
}
```

---

## Phase 5: Spam Engine (Weeks 6-9)

### 5.1 Bayesian Classifier

**File: `internal/spam/bayesian.go`**

```go
type BayesianClassifier struct {
    db *db.DB
}

type TokenStats struct {
    SpamCount int
    HamCount  int
}

// Train updates token frequencies from a message
func (bc *BayesianClassifier) Train(domain, user string, message []byte, isSpam bool) error {
    tokens := tokenize(message)
    // For each token: increment spam or ham count in per-user DB bucket
}

// Classify returns spam probability (0.0 = ham, 1.0 = spam)
func (bc *BayesianClassifier) Classify(domain, user string, message []byte) (float64, error) {
    tokens := tokenize(message)
    // Robinson-Fisher method:
    // 1. For each token, compute spam probability: S(token) / (S(token) + H(token))
    // 2. Combine using Fisher's method (chi-squared inverse)
    // 3. Return combined probability
}

// tokenize extracts features from a message
func tokenize(message []byte) []string {
    // 1. Parse MIME to get text parts
    // 2. Decode content-transfer-encoding
    // 3. Strip HTML tags
    // 4. Split into words (unicode-aware)
    // 5. Lowercase
    // 6. Generate unigrams + bigrams
    // 7. Add meta-tokens: "FROM:domain.com", "SUBJECT:word", "HAS:attachment"
    // 8. Remove stop words
    // 9. Deduplicate
}
```

### 5.2 RBL/DNSBL Lookup

**File: `internal/spam/rbl.go`**

```go
// CheckRBL checks if an IP is listed in DNS-based blocklists
func CheckRBL(ctx context.Context, ip net.IP, servers []string, resolver *dns.Resolver) ([]RBLResult, error) {
    // Reverse IP: 1.2.3.4 → 4.3.2.1
    // For each RBL server:
    //   Lookup: 4.3.2.1.zen.spamhaus.org A record
    //   If exists → IP is listed
    //   Return code indicates list type (SBL, XBL, PBL, etc.)
    // Parallel lookups with context timeout (2 seconds max)
}
```

### 5.3 Heuristic Rules

**File: `internal/spam/heuristic.go`**

```go
type HeuristicRule struct {
    Name        string
    Description string
    Score       float64
    Check       func(msg *ParsedMessage) bool
}

var defaultRules = []HeuristicRule{
    {"ALL_CAPS_SUBJECT", "Subject is all uppercase", 2.0,
        func(msg *ParsedMessage) bool { return isAllCaps(msg.Subject) && len(msg.Subject) > 5 }},
    {"HTML_ONLY", "Message is HTML-only with no text part", 1.5,
        func(msg *ParsedMessage) bool { return msg.HasHTML && !msg.HasText }},
    {"EXCESSIVE_URLS", "More than 10 URLs in body", 1.0,
        func(msg *ParsedMessage) bool { return countURLs(msg.Body) > 10 }},
    {"URL_SHORTENER", "Contains URL shortener links", 1.5,
        func(msg *ParsedMessage) bool { return hasURLShortener(msg.Body) }},
    {"NO_FROM_NAME", "From header has no display name", 0.5,
        func(msg *ParsedMessage) bool { return msg.FromName == "" }},
    {"FORGED_OUTLOOK", "Claims to be Outlook but headers inconsistent", 2.5,
        func(msg *ParsedMessage) bool { return isForgedClient(msg, "Outlook") }},
    {"MISSING_DATE", "No Date header", 1.0,
        func(msg *ParsedMessage) bool { return msg.Date.IsZero() }},
    {"MISSING_MSGID", "No Message-ID header", 1.0,
        func(msg *ParsedMessage) bool { return msg.MessageID == "" }},
    {"EMPTY_BODY", "Empty or near-empty body", 1.5,
        func(msg *ParsedMessage) bool { return len(strings.TrimSpace(msg.TextBody)) < 10 }},
    {"ATTACHMENT_EXE", "Has executable attachment", 3.0,
        func(msg *ParsedMessage) bool { return hasExecutableAttachment(msg) }},
    {"FROM_TO_SAME", "From and To are the same", 0.5,
        func(msg *ParsedMessage) bool { return msg.From == msg.To[0] }},
    {"BULK_PRECEDENCE", "Precedence: bulk header present", 0.3,
        func(msg *ParsedMessage) bool { return msg.Precedence == "bulk" }},
}
```

---

## Phase 6: TLS & ACME (Weeks 3-5)

### 6.1 ACME Client

**File: `internal/tls/acme.go`**

```go
type ACMEManager struct {
    config   *config.TLSConfig
    certDir  string
    resolver *dns.Resolver
    
    // Certificate cache
    certs   map[string]*tls.Certificate
    certsMu sync.RWMutex
}

// GetCertificate is used as tls.Config.GetCertificate callback (SNI)
func (m *ACMEManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
    domain := hello.ServerName
    
    // Check cache
    m.certsMu.RLock()
    cert, ok := m.certs[domain]
    m.certsMu.RUnlock()
    if ok && time.Now().Before(cert.Leaf.NotAfter.Add(-30*24*time.Hour)) {
        return cert, nil
    }
    
    // Try to load from disk
    cert, err := m.loadFromDisk(domain)
    if err == nil {
        m.cacheCert(domain, cert)
        return cert, nil
    }
    
    // Obtain new certificate via ACME
    cert, err = m.obtainCert(domain)
    if err != nil {
        return nil, err
    }
    m.cacheCert(domain, cert)
    return cert, nil
}

// Auto-renewal goroutine
func (m *ACMEManager) StartAutoRenewal(ctx context.Context) {
    ticker := time.NewTicker(12 * time.Hour)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            m.renewExpiring() // Renew certs expiring within 30 days
        }
    }
}
```

---

## Phase 7: HTTP & Web UI (Weeks 7-12)

### 7.1 HTTP Server

**File: `internal/http/server.go`**

```go
type Server struct {
    config   *config.HTTPConfig
    mux      *http.ServeMux
    store    *store.MaildirStore
    db       *db.DB
    auth     *auth.Authenticator
    
    // Embedded frontend assets
    webmailFS  fs.FS // embed.FS for webmail SPA
    adminFS    fs.FS // embed.FS for admin panel SPA
    accountFS  fs.FS // embed.FS for account portal
}

func (s *Server) setupRoutes() {
    // API endpoints (JSON)
    s.mux.HandleFunc("/api/v1/mail/", s.authMiddleware(s.handleMailAPI))
    s.mux.HandleFunc("/api/v1/folders/", s.authMiddleware(s.handleFoldersAPI))
    s.mux.HandleFunc("/api/v1/contacts/", s.authMiddleware(s.handleContactsAPI))
    s.mux.HandleFunc("/api/v1/settings/", s.authMiddleware(s.handleSettingsAPI))
    s.mux.HandleFunc("/api/v1/compose/", s.authMiddleware(s.handleComposeAPI))
    
    // Admin API (separate auth)
    s.mux.HandleFunc("/api/admin/v1/", s.adminAuthMiddleware(s.handleAdminAPI))
    
    // Webmail SPA
    s.mux.Handle("/webmail/", http.StripPrefix("/webmail", s.spaHandler(s.webmailFS)))
    
    // Admin panel SPA
    s.mux.Handle("/admin/", http.StripPrefix("/admin", s.spaHandler(s.adminFS)))
    
    // Account self-service
    s.mux.Handle("/account/", http.StripPrefix("/account", s.spaHandler(s.accountFS)))
    
    // Autoconfig/Autodiscover for mail clients
    s.mux.HandleFunc("/mail/config-v1.1.xml", s.handleAutoconfig)           // Thunderbird
    s.mux.HandleFunc("/.well-known/autoconfig/mail/config-v1.1.xml", s.handleAutoconfig)
    s.mux.HandleFunc("/autodiscover/autodiscover.xml", s.handleAutodiscover) // Outlook
    s.mux.HandleFunc("/.well-known/mta-sts.txt", s.handleMTASTS)
    
    // ACME HTTP-01 challenge
    s.mux.HandleFunc("/.well-known/acme-challenge/", s.handleACMEChallenge)
    
    // Health & Metrics
    s.mux.HandleFunc("/health", s.handleHealth)
    s.mux.HandleFunc("/metrics", s.handleMetrics) // Prometheus
    
    // Root redirect
    s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        http.Redirect(w, r, "/webmail/", http.StatusTemporaryRedirect)
    })
}

// spaHandler serves an SPA — serves index.html for all non-file routes
func (s *Server) spaHandler(fsys fs.FS) http.Handler {
    fileServer := http.FileServer(http.FS(fsys))
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Try to serve the file directly
        f, err := fsys.Open(strings.TrimPrefix(r.URL.Path, "/"))
        if err != nil {
            // File not found — serve index.html for SPA routing
            r.URL.Path = "/"
        } else {
            f.Close()
        }
        fileServer.ServeHTTP(w, r)
    })
}
```

### 7.2 Webmail REST API

**File: `internal/http/mail_api.go`**

```go
// GET /api/v1/mail/messages?folder=INBOX&page=1&limit=50&sort=date&order=desc
// GET /api/v1/mail/messages/{id}
// GET /api/v1/mail/messages/{id}/raw          — Raw RFC 822
// GET /api/v1/mail/messages/{id}/attachments/{name}
// POST /api/v1/mail/messages                   — Send/save draft
// PUT /api/v1/mail/messages/{id}/flags         — Update flags (read, flagged, etc.)
// PUT /api/v1/mail/messages/{id}/move          — Move to folder
// DELETE /api/v1/mail/messages/{id}            — Move to trash / permanent delete
// POST /api/v1/mail/messages/bulk              — Bulk operations
// GET /api/v1/mail/search?q=...&from=...&to=...&after=...&before=...&has=attachment

// GET /api/v1/folders
// POST /api/v1/folders                         — Create folder
// PUT /api/v1/folders/{name}                   — Rename
// DELETE /api/v1/folders/{name}

// GET /api/v1/contacts?q=...                   — Search contacts (from address book + sent history)
// POST /api/v1/compose/send                    — Send email
// POST /api/v1/compose/draft                   — Save draft
// POST /api/v1/compose/attachments             — Upload attachment (multipart)

// GET /api/v1/settings
// PUT /api/v1/settings                         — Update settings (signature, vacation, etc.)
// POST /api/v1/settings/2fa/setup              — Generate TOTP QR code
// POST /api/v1/settings/2fa/verify             — Verify and enable 2FA
// DELETE /api/v1/settings/2fa                  — Disable 2FA
// POST /api/v1/settings/app-passwords          — Generate app password
// DELETE /api/v1/settings/app-passwords/{id}

// Message list response:
type MessageListResponse struct {
    Messages   []MessageSummary `json:"messages"`
    Total      int              `json:"total"`
    Page       int              `json:"page"`
    Limit      int              `json:"limit"`
    UnreadCount int             `json:"unread_count"`
}

type MessageSummary struct {
    ID          string    `json:"id"`
    From        Address   `json:"from"`
    To          []Address `json:"to"`
    Subject     string    `json:"subject"`
    Preview     string    `json:"preview"`     // First 200 chars of text body
    Date        time.Time `json:"date"`
    Size        int64     `json:"size"`
    Flags       []string  `json:"flags"`       // seen, flagged, answered, draft
    Labels      []string  `json:"labels"`      // Custom labels
    HasAttachment bool    `json:"has_attachment"`
    ThreadID    string    `json:"thread_id"`   // For conversation view
}
```

### 7.3 Admin REST API

**File: `internal/http/admin_api.go`**

```go
// Dashboard
// GET /api/admin/v1/dashboard                  — Server overview stats

// Domains
// GET /api/admin/v1/domains
// POST /api/admin/v1/domains
// GET /api/admin/v1/domains/{domain}
// PUT /api/admin/v1/domains/{domain}
// DELETE /api/admin/v1/domains/{domain}
// GET /api/admin/v1/domains/{domain}/dns       — Required DNS records

// Accounts
// GET /api/admin/v1/domains/{domain}/accounts
// POST /api/admin/v1/domains/{domain}/accounts
// GET /api/admin/v1/domains/{domain}/accounts/{localpart}
// PUT /api/admin/v1/domains/{domain}/accounts/{localpart}
// DELETE /api/admin/v1/domains/{domain}/accounts/{localpart}
// POST /api/admin/v1/domains/{domain}/accounts/{localpart}/reset-password

// Aliases
// GET /api/admin/v1/domains/{domain}/aliases
// POST /api/admin/v1/domains/{domain}/aliases
// DELETE /api/admin/v1/domains/{domain}/aliases/{alias}

// Queue
// GET /api/admin/v1/queue?status=pending&page=1
// POST /api/admin/v1/queue/{id}/retry
// DELETE /api/admin/v1/queue/{id}
// POST /api/admin/v1/queue/flush

// Security
// GET /api/admin/v1/blocklist
// POST /api/admin/v1/blocklist
// DELETE /api/admin/v1/blocklist/{entry}
// GET /api/admin/v1/security/ratelimits

// DKIM
// POST /api/admin/v1/domains/{domain}/dkim/rotate
// GET /api/admin/v1/domains/{domain}/dkim/dns-record

// TLS
// GET /api/admin/v1/tls/certificates
// POST /api/admin/v1/tls/certificates/renew

// Logs
// GET /api/admin/v1/logs?level=error&after=...&before=...&limit=100
```

---

## Phase 8: MCP Server (Weeks 10-11)

### 8.1 MCP Transport

**File: `internal/mcp/server.go`**

```go
type MCPServer struct {
    config *config.MCPConfig
    store  *store.MaildirStore
    db     *db.DB
    queue  *queue.Manager
}

// MCP tools registry
func (s *MCPServer) Tools() []Tool {
    return []Tool{
        {Name: "umailserver_send", Description: "Send an email", Handler: s.toolSend},
        {Name: "umailserver_search", Description: "Search emails", Handler: s.toolSearch},
        {Name: "umailserver_read", Description: "Read a specific email", Handler: s.toolRead},
        {Name: "umailserver_list", Description: "List emails in a folder", Handler: s.toolList},
        {Name: "umailserver_move", Description: "Move email(s) to folder", Handler: s.toolMove},
        {Name: "umailserver_delete", Description: "Delete email(s)", Handler: s.toolDelete},
        {Name: "umailserver_flag", Description: "Flag/star email(s)", Handler: s.toolFlag},
        {Name: "umailserver_folders", Description: "List folders", Handler: s.toolFolders},
        {Name: "umailserver_contacts", Description: "Search contacts", Handler: s.toolContacts},
        {Name: "umailserver_stats", Description: "Server statistics", Handler: s.toolStats},
        {Name: "umailserver_queue_status", Description: "Queue status (admin)", Handler: s.toolQueueStatus},
        {Name: "umailserver_domain_add", Description: "Add domain (admin)", Handler: s.toolDomainAdd},
        {Name: "umailserver_account_add", Description: "Create account (admin)", Handler: s.toolAccountAdd},
    }
}
```

---

## Phase 9: Webmail Frontend (Weeks 8-14)

### 9.1 Project Setup

```
web/webmail/
├── src/
│   ├── main.tsx
│   ├── App.tsx
│   ├── routes/
│   │   ├── inbox.tsx
│   │   ├── compose.tsx
│   │   ├── message.tsx
│   │   ├── search.tsx
│   │   └── settings.tsx
│   ├── components/
│   │   ├── layout/
│   │   │   ├── Sidebar.tsx          # Folder list + labels
│   │   │   ├── MailList.tsx         # Message list panel
│   │   │   ├── MailReader.tsx       # Message reader panel
│   │   │   ├── ComposeDrawer.tsx    # Compose email sheet
│   │   │   └── CommandPalette.tsx   # ⌘K quick actions
│   │   ├── mail/
│   │   │   ├── MessageItem.tsx      # Single message row
│   │   │   ├── MessageHeader.tsx    # From/To/Subject/Date
│   │   │   ├── MessageBody.tsx      # Rendered HTML/text
│   │   │   ├── AttachmentList.tsx   # File attachments
│   │   │   ├── ThreadView.tsx       # Conversation thread
│   │   │   └── MessageActions.tsx   # Reply/Forward/Archive/Delete
│   │   ├── compose/
│   │   │   ├── ComposeForm.tsx      # Email compose form
│   │   │   ├── RichTextEditor.tsx   # TipTap editor wrapper
│   │   │   ├── RecipientInput.tsx   # To/CC/BCC with autocomplete
│   │   │   ├── AttachmentUpload.tsx # Drag & drop file upload
│   │   │   └── SignatureEditor.tsx
│   │   └── shared/
│   │       ├── Avatar.tsx
│   │       ├── Badge.tsx
│   │       ├── EmptyState.tsx
│   │       └── LoadingState.tsx
│   ├── hooks/
│   │   ├── useMessages.ts          # TanStack Query for messages
│   │   ├── useFolders.ts
│   │   ├── useSearch.ts
│   │   ├── useCompose.ts
│   │   ├── useKeyboard.ts          # Keyboard shortcuts
│   │   └── useWebSocket.ts         # Real-time updates
│   ├── stores/
│   │   ├── uiStore.ts              # Zustand: panel sizes, view mode
│   │   ├── selectionStore.ts       # Multi-select state
│   │   └── draftsStore.ts          # Auto-save drafts
│   ├── lib/
│   │   ├── api.ts                  # API client
│   │   ├── sanitize.ts             # HTML email sanitizer (DOMPurify)
│   │   ├── keyboard.ts             # Keyboard shortcut map
│   │   └── format.ts               # Date, size, address formatters
│   └── types/
│       └── mail.ts
├── index.html
├── vite.config.ts
├── tailwind.config.ts
├── tsconfig.json
└── package.json
```

### 9.2 Key Components

**3-Panel Layout (ResizablePanel):**

```tsx
// App.tsx — Main layout
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@/components/ui/resizable"

export default function App() {
  return (
    <ResizablePanelGroup direction="horizontal" className="h-screen">
      {/* Sidebar: folders + labels */}
      <ResizablePanel defaultSize={15} minSize={10} maxSize={25}>
        <Sidebar />
      </ResizablePanel>
      <ResizableHandle />
      
      {/* Mail list */}
      <ResizablePanel defaultSize={35} minSize={25}>
        <MailList />
      </ResizablePanel>
      <ResizableHandle />
      
      {/* Mail reader */}
      <ResizablePanel defaultSize={50}>
        <MailReader />
      </ResizablePanel>
    </ResizablePanelGroup>
  )
}
```

**Keyboard Shortcuts:**

| Key | Action |
|-----|--------|
| `c` | Compose new email |
| `r` | Reply |
| `a` | Reply all |
| `f` | Forward |
| `e` | Archive |
| `#` | Delete |
| `s` | Star/flag |
| `u` | Mark as unread |
| `j` / `k` | Next / previous message |
| `/` | Focus search |
| `⌘K` | Command palette |
| `Escape` | Close compose / deselect |
| `⌘Enter` | Send email |
| `?` | Show keyboard shortcuts |

### 9.3 Admin Panel Structure

```
web/admin/
├── src/
│   ├── routes/
│   │   ├── dashboard.tsx          # Server overview
│   │   ├── domains/
│   │   │   ├── list.tsx
│   │   │   ├── detail.tsx
│   │   │   └── dns-helper.tsx     # DNS record display
│   │   ├── accounts/
│   │   │   ├── list.tsx
│   │   │   └── detail.tsx
│   │   ├── queue.tsx              # Outbound queue management
│   │   ├── security/
│   │   │   ├── blocklist.tsx
│   │   │   └── ratelimits.tsx
│   │   ├── tls.tsx                # Certificate management
│   │   └── logs.tsx               # Server logs viewer
│   └── components/
│       ├── charts/
│       │   ├── MailVolumeChart.tsx  # Incoming/outgoing over time
│       │   ├── SpamRatioChart.tsx
│       │   └── StorageChart.tsx
│       └── dns/
│           └── DNSRecordCard.tsx    # Copy-pasteable DNS records
```

---

## Build System

### Makefile

```makefile
VERSION := $(shell git describe --tags --always --dirty)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT := $(shell git rev-parse --short HEAD)
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE) -X main.GitCommit=$(GIT_COMMIT)

.PHONY: build dev test clean

# Build production binary with embedded UI
build: build-ui
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver ./cmd/umailserver

# Build UI assets (pulls from separate repos or local)
build-ui:
	cd web/webmail && npm ci && npm run build
	cd web/admin && npm ci && npm run build
	cd web/account && npm ci && npm run build

# Development mode: Go server + Vite dev servers
dev:
	@echo "Starting Go server on :8080..."
	@echo "Starting Vite webmail on :5173..."
	@echo "Starting Vite admin on :5174..."
	# Use a process manager or tmux to run all three

# Run all tests
test:
	go test -race -cover ./...

# Cross-compile for all targets
release:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-linux-amd64 ./cmd/umailserver
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-linux-arm64 ./cmd/umailserver
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-darwin-amd64 ./cmd/umailserver
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-darwin-arm64 ./cmd/umailserver
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-freebsd-amd64 ./cmd/umailserver

# Docker
docker:
	docker build -t umailserver/umailserver:$(VERSION) .
	docker tag umailserver/umailserver:$(VERSION) umailserver/umailserver:latest

clean:
	rm -rf bin/ web/*/dist/
```

### Embed Frontend

**File: `embed.go`** (root of Go module)

```go
package umailserver

import "embed"

//go:embed web/webmail/dist/*
var WebmailFS embed.FS

//go:embed web/admin/dist/*
var AdminFS embed.FS

//go:embed web/account/dist/*
var AccountFS embed.FS
```

### Dockerfile

```dockerfile
# Stage 1: Build UI
FROM node:22-alpine AS ui-builder
WORKDIR /app
COPY web/webmail/ web/webmail/
COPY web/admin/ web/admin/
COPY web/account/ web/account/
RUN cd web/webmail && npm ci && npm run build
RUN cd web/admin && npm ci && npm run build
RUN cd web/account && npm ci && npm run build

# Stage 2: Build Go binary
FROM golang:1.23-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui-builder /app/web/webmail/dist web/webmail/dist
COPY --from=ui-builder /app/web/admin/dist web/admin/dist
COPY --from=ui-builder /app/web/account/dist web/account/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /umailserver ./cmd/umailserver

# Stage 3: Minimal runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=go-builder /umailserver /usr/local/bin/umailserver
EXPOSE 25 465 587 143 993 80 443 8443 3000
VOLUME /var/lib/umailserver
ENTRYPOINT ["umailserver"]
CMD ["serve"]
```

---

## Testing Strategy

### Unit Tests

Every internal package has `_test.go` files:

```go
// internal/smtp/session_test.go
func TestSMTPSession_EHLO(t *testing.T)
func TestSMTPSession_MAIL_FROM(t *testing.T)
func TestSMTPSession_AUTH_PLAIN(t *testing.T)
func TestSMTPSession_Pipeline(t *testing.T)

// internal/auth/spf_test.go
func TestSPF_Pass(t *testing.T)
func TestSPF_Fail(t *testing.T)
func TestSPF_SoftFail(t *testing.T)
func TestSPF_Redirect(t *testing.T)
func TestSPF_Include(t *testing.T)
func TestSPF_LookupLimit(t *testing.T)

// internal/spam/bayesian_test.go
func TestBayesian_TrainAndClassify(t *testing.T)
func TestBayesian_Tokenizer(t *testing.T)
```

### Integration Tests

```go
// test/integration/smtp_delivery_test.go
// Starts a full uMailServer instance in a temp dir
// Sends a message via SMTP
// Verifies delivery to Maildir
// Checks DKIM signature
// Checks spam score

// test/integration/imap_session_test.go
// Connects via IMAP
// Authenticates
// SELECT INBOX
// FETCH messages
// STORE flags
// IDLE for new message notification
```

### RFC Compliance Tests

Use known test vectors from RFCs:
- SPF test suite from RFC 7208 Appendix A
- DKIM test vectors from RFC 6376
- DMARC test cases from RFC 7489

---

## Security Considerations

### Input Parsing Safety

- All protocol parsers have strict length limits (no unbounded reads)
- SMTP DATA command: hard limit at `max_message_size` config
- IMAP command line: 8192 byte limit
- HTTP request body: 50MB limit for attachments
- URL/path traversal protection in Maildir operations
- HTML email sanitized with strict allowlist before rendering in webmail

### Memory Safety

- Go's GC handles memory management
- No unsafe pointers in hot paths
- Bounded buffers for all network I/O
- Connection timeouts on all listeners (30s read, 60s write for SMTP)

### Webmail Security

- JWT tokens with short expiry (15 min) + refresh tokens (7 days)
- HttpOnly, Secure, SameSite=Strict cookies
- CSP headers: `script-src 'self'`; `style-src 'self' 'unsafe-inline'` (for TipTap)
- HTML email rendered in sandboxed iframe with `sandbox="allow-same-origin"`
- All external images proxied through server to prevent tracking pixels
- DOMPurify for HTML sanitization

---

## Migration Path

### From Postfix + Dovecot

```bash
# 1. Stop old services
systemctl stop postfix dovecot

# 2. Create uMailServer config with same domains/accounts
umailserver quickstart admin@example.com --import-config /etc/postfix/main.cf

# 3. Copy Maildir data (if already using Maildir format)
cp -r /var/mail/vhosts/* /var/lib/umailserver/domains/

# 4. Import user accounts
umailserver migrate --source dovecot --passwd /etc/dovecot/users

# 5. Update DNS: MX records stay the same (same hostname)
# 6. Start uMailServer
umailserver serve
```

### From Any IMAP Server

```bash
# IMAP-to-IMAP sync (built-in tool)
umailserver migrate \
  --source imap://old-server.com:993 \
  --source-user admin@example.com \
  --source-pass "..." \
  --domain example.com \
  --all-accounts
```
