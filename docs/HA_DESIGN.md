# High Availability: Design Draft

Status: draft for decision (2026-10-27). Option A is implemented (see
"Option A: done" below); B and C are not.

## Problem

uMailServer runs as exactly one process. When that process or its host is
down, no mail is accepted, no one can read mail, and nothing is delivered.
The Helm chart (`deploy/helm/umailserver`) pins `replicas: 1` because a
second replica cannot work with today's storage. This document lists what
ties the server to one process, the options for removing that limit, and a
recommended order.

## What ties the server to one process

### Persistent state on the data volume

| State | Where | Notes |
|-------|-------|-------|
| Domains, accounts, aliases, revoked tokens, queue entries | `umailserver.db` (bbolt, `internal/db`) | Exclusive file lock; a second opener times out after 1 s |
| IMAP metadata (UIDs, UIDVALIDITY, flags, ACLs), search index | `mail/mail.db` (bbolt, `internal/storage`) | Exclusive lock, 5 s timeout |
| Message bodies | Maildir++ under `data_dir` (`internal/storage.MessageStore`) | Plain files |
| Outbound queue messages | `data_dir/queue` | Plain files, paired with queue entries in `umailserver.db` |
| Calendars, address books | `data_dir/caldav`, `data_dir/carddav` | Files guarded by in-process locks |
| Vacation state, push subscriptions, alert state, MCP token | `data_dir/vacation*`, `push`, `alert_state.json`, MCP token file | Files |
| ACME certificates | autocert directory cache | Files |

bbolt maps its file into memory and relies on `flock`. Two processes on two
hosts cannot share one bbolt file, and putting it on NFS or another
shared filesystem is unsafe: the lock is not reliable there, and corruption,
not an error, is the likely result.

### Process-local state

Kept in memory and lost or diverging across replicas:

- Rate limiter counters (per IP, user, global) and greylisting triplets.
- Failed-login counters and lockouts (API, IMAP, POP3, CalDAV/CardDAV auth
  guards).
- The SSE hub (`/api/v1/events`) and IMAP IDLE notifications: a message
  delivered by one replica would not notify clients connected to another.
- DMARC aggregate-report data and the Bayesian learner's in-memory model.

### Work that must run once

Started in `Server.Start` and written for a single instance: the queue
manager's delivery workers, search indexing workers, vacation cleanup, the
alert checker, the spam learner, the DMARC reporter, and ACME renewals.

### Existing cluster code

`internal/cluster` implements a Redis session store, leader election, a
distributed lock and heartbeats, and `api.Server.SetClusterManager` accepts
it, but nothing constructs a `ClusterManager`. It covers only a small part of
the list above (none of the persistent state), so it is not a starting point
for active/active by itself.

## Options

### A. Fast failover of the single instance (active/passive)

Keep one active process; make its replacement start quickly and safely.

- On Kubernetes this is mostly what the chart already does: when the node
  fails, the StatefulSet pod is rescheduled and the volume re-attached.
  Downtime is the detection time plus volume re-attach (typically minutes).
- To shorten it: a replicated block volume (e.g. Longhorn, Ceph RBD, cloud
  regional disks) so the volume can attach on another node at once.
- Correctness requirement: never two writers. The bbolt lock prevents this on
  one host; across hosts the volume's single-attach (ReadWriteOnce) does.
  Document that `ReadWriteMany` volumes must not be used.

Code changes: small. Shorter startup (avoid rebuilding state that can be
loaded), a readiness check that turns true only once SMTP accepts mail, and
documentation.

What it gives: recovery without manual work. What it does not: zero downtime;
inbound mail during the outage relies on sending servers retrying, which they
do for days.

### B. Redundant inbound MX (store-and-forward edge)

Add an optional "edge" mode: a stateless SMTP node that accepts mail for
the served domains, runs the inbound checks it can run without user data,
queues the message on its own disk and forwards it to the primary when the
primary is reachable. Publish the edge nodes as additional MX records.

- Uses the existing SMTP server, pipeline and queue; needs a recipient list
  (or accept-all with later bounce) and a "deliver to primary" transport
  instead of MX lookup.
- Mail is never refused while the primary is down; users still cannot read
  mail until it is back.
- Backup MX nodes are a known spam path (they are less strict than the
  primary), so the edge should get the recipient list from the primary and
  reject unknown recipients.

Code changes: moderate (a new run mode, a relay transport, recipient sync),
no change to storage.

### C. Active/active

Run several full replicas behind a load balancer.

- Replace bbolt with a shared database (PostgreSQL is the natural choice)
  for accounts, IMAP metadata and queue state, behind interfaces the
  current code mostly does not have yet; `imap.BboltMailstore` and `db.DB`
  are used directly in many places.
- Move message bodies to object storage (S3-compatible) or a shared
  filesystem with a per-message write discipline; IMAP UID assignment and
  MODSEQ must become database transactions.
- Move rate limits, greylisting, login lockouts and SSE/IDLE fan-out to
  Redis (the existing `internal/cluster` code can serve part of this).
- Run the single-instance work under leader election, or make the queue
  claim entries per worker with leases.
- Add a migration tool from bbolt/Maildir to the new stores.

Code changes: very large, touching most packages, and it adds two required
external services (PostgreSQL, Redis) to a project whose selling point is a
single binary with no dependencies. It would have to remain optional, which
means maintaining both storage backends.

## Recommendation

1. **A now.** Document the failover model, add the readiness/startup
   improvements, and state in the chart that `ReadWriteMany` must not be
   used. Small and keeps the single-binary design.
2. **B next, if inbound availability matters.** It addresses the most
   costly failure (lost or delayed inbound mail for many senders) without
   touching storage.
3. **C only as a separate product decision.** It changes what the project is.
   If it is pursued, the first step is introducing storage interfaces in
   front of `db.DB` and the mailstore with the bbolt implementation behind
   them, which is useful on its own and can land incrementally.

Independently: `internal/cluster` and `api.Server.SetClusterManager` are
unused. Either remove them (dead code) or keep them explicitly for C; they
should not stay as code that looks like a working feature.

## Option A: done

- `Server.Stop` marks the API as draining first, so `/health/ready` answers
  503 for the whole shutdown, not only after the API stops last.
- The Helm chart's readiness probe uses `/health/ready` (a TCP check with
  ACME-only TLS, whose certificates need SNI), adds a `preStop` delay so the
  pod is removed from the Service before it stops accepting, and refuses
  `ReadWriteMany` volumes.
- `docs/DEPLOYMENT.md` → Failover describes recovery from node failure (the
  out-of-service taint) and the storage requirements.

Not done: startup time was not measured or shortened.

## Open questions

- Is a few minutes of downtime on node failure acceptable (A), or must
  inbound mail be accepted continuously (A + B)?
- Is adding PostgreSQL and Redis as optional dependencies acceptable for the
  project at all (C)?
- Remove or keep `internal/cluster`?
