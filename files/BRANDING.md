# uMailServer — Branding Guide

## Brand Identity

| Element | Value |
|---------|-------|
| **Full Name** | uMailServer |
| **Short Name** | uMS |
| **Tagline** | One binary. Complete email. |
| **Alt Tagline** | The mail server that replaced them all. |
| **Domain** | umailserver.com |
| **GitHub** | github.com/umailserver |
| **Twitter/X** | @umailserver |

## Name Usage

- Always written as **uMailServer** (lowercase u, capital M and S)
- In code/CLI context: `umailserver` (all lowercase, no separators)
- Binary name: `umailserver`
- Go module: `github.com/umailserver/umailserver`
- Docker image: `umailserver/umailserver`
- Never: UMailServer, Umailserver, u-mail-server, uMail Server

## Brand Colors

| Color | Hex | Usage |
|-------|-----|-------|
| **Primary Blue** | `#2563EB` | Logo, primary actions, links |
| **Dark Navy** | `#0F172A` | Sidebar, dark backgrounds |
| **Light Blue** | `#DBEAFE` | Hover states, highlights |
| **Success Green** | `#16A34A` | Delivered, verified, connected |
| **Warning Amber** | `#D97706` | Queue warnings, pending |
| **Error Red** | `#DC2626` | Errors, bounced, blocked |
| **Spam Orange** | `#EA580C` | Spam indicators |
| **White** | `#FFFFFF` | Content backgrounds |
| **Gray 50** | `#F8FAFC` | Page backgrounds |
| **Gray 500** | `#64748B` | Secondary text |
| **Gray 900** | `#0F172A` | Primary text |

## Typography

| Context | Font | Weight |
|---------|------|--------|
| **Logo/Brand** | Inter | 700 Bold |
| **Headings** | Inter | 600 Semibold |
| **Body** | Inter | 400 Regular |
| **Code/CLI** | JetBrains Mono | 400 Regular |

## Logo Concept

The uMailServer logo should convey:
- **Unification** — the "u" represents bringing everything together
- **Simplicity** — clean, minimal, modern
- **Technical precision** — developer-friendly, not enterprise-bloated

**Primary mark:** The letter "u" stylized as an envelope opening, or the "u" containing a small mail icon. The descender of the "u" could curve into an envelope flap. Keep it geometric and flat.

**Wordmark:** "uMailServer" in Inter Bold, with the "u" in Primary Blue and "MailServer" in Dark Navy. On dark backgrounds, reverse to white.

**Icon (favicon/app icon):** The "u" mark alone, in a rounded square with Primary Blue background.

## Voice & Tone

### For Developer Audience

**Tone:** Technical, direct, confident, slightly irreverent. We respect the reader's intelligence. No marketing fluff, no "leverage synergies."

**Examples:**
- Good: "One binary. No stitching together Postfix + Dovecot + SpamAssassin + OpenDKIM + Roundcube. Just `./umailserver serve`."
- Good: "SMTP, IMAP, spam filtering, DKIM, webmail, admin panel — one Go binary, 50MB."
- Bad: "Revolutionizing email infrastructure with our cutting-edge unified platform."
- Bad: "Enterprise-grade email solutions for modern businesses."

### README Hero Section

```
# uMailServer

**One binary. Complete email.**

uMailServer is a self-hosted mail server written in Go that replaces 
Postfix + Dovecot + SpamAssassin + OpenDKIM + Roundcube with a single binary.

SMTP · IMAP · Spam filtering · DKIM/SPF/DMARC · Modern webmail · Admin panel · MCP server

curl -sSL https://umailserver.com/install.sh | bash
umailserver quickstart you@example.com
# → Your mail server is running. DNS records printed. That's it.
```

### GitHub Description

"Self-hosted mail server in a single Go binary. SMTP, IMAP, spam filtering, DKIM/SPF/DMARC, modern React webmail, admin panel, and MCP server. Replaces Postfix + Dovecot + SpamAssassin + Roundcube."

### X/Twitter Bio

"One binary. Complete email. Self-hosted mail server in Go. SMTP · IMAP · Spam · DKIM · Webmail · Admin · MCP. Open source."

## Key Messaging

### One-Liner
"The self-hosted mail server that replaced them all."

### Elevator Pitch
"uMailServer is a complete email server in a single Go binary — SMTP, IMAP, spam filtering, DKIM signing, a modern React webmail, and an admin panel. Download one file, run one command, and you have a production-ready mail server. No more managing 6 different daemons."

### Comparison Hook
"What if Postfix, Dovecot, SpamAssassin, OpenDKIM, and Roundcube were one Go binary with a Gmail-quality webmail?"

### Technical Differentiators (for content)

1. **Single binary** — Not Docker Compose with 8 containers. One file, one process.
2. **Modern webmail** — React 19 + shadcn/ui. Not PHP Roundcube from 2008.
3. **Zero-to-running in 60 seconds** — `quickstart` command generates config, prints DNS records, starts serving.
4. **MCP server built-in** — AI agents can send/read email through Model Context Protocol.
5. **Deliverability toolkit** — Built-in DNS checker, blocklist monitor, warm-up mode.
6. **Pure Go** — No CGO, no Perl, no PHP, no Python. Single static binary, cross-compile anywhere.

## Social Media Content Themes

### Launch Announcements
- "Tired of configuring Postfix + Dovecot + SpamAssassin + OpenDKIM + Roundcube? What if it was one binary?"
- "Just shipped uMailServer — a complete mail server in 50MB. SMTP, IMAP, spam filter, DKIM, webmail, admin panel. One Go binary."

### Developer Culture / Vibe Coding
- "Setting up a mail server in 2024: install 6 daemons, edit 10 config files, pray. Setting up in 2026: `./umailserver quickstart`"
- "Webmail UI should not look like it was designed in 2008. We built ours with React 19 + shadcn/ui."

### Technical Deep Dives
- Thread on SMTP protocol implementation in Go from scratch
- Thread on Bayesian spam filtering without SpamAssassin
- Thread on DKIM signing implementation
- Thread on embedding React SPA in Go binary

### MCP / AI Native
- "Your AI agent can now send and read emails natively. uMailServer has a built-in MCP server."
- "Claude, summarize my unread emails → MCP → uMailServer → done."

## GitHub Social Preview Image

1200x630px image with:
- uMailServer logo (left)
- Tagline: "One binary. Complete email."
- Terminal mockup showing `umailserver serve` with port listings
- Webmail screenshot (right side, at angle)
- Dark background (Navy `#0F172A`)

## Infographic Prompt (for Nano Banana 2)

```
Minimalist flat tech infographic, dark navy (#0F172A) background, white and blue (#2563EB) accent colors. Center: large stylized "u" logo made of geometric mail envelope shapes. Left side: crossed-out icons representing Postfix, Dovecot, SpamAssassin, OpenDKIM, Roundcube (old stack). Right side: single glowing binary icon representing uMailServer. Bottom: feature pills — "SMTP • IMAP • Spam • DKIM • Webmail • Admin • MCP". Clean, technical, developer-focused. No people, no stock photos. Geometric shapes only. Text: "One binary. Complete email."
```
