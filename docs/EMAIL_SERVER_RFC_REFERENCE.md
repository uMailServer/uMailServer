# Email Server RFC Reference

> Complete reference of IETF RFCs for building a full-featured email server — covering SMTP, POP3, IMAP, message format, MIME, authentication, security, anti-spam, webmail protocols, and related standards.

---

## 1. SMTP — Simple Mail Transfer Protocol

### Core Protocol

| RFC | Title | Status | Notes |
|-----|-------|--------|-------|
| RFC 5321 | Simple Mail Transfer Protocol | Standard | **Current SMTP spec** — supersedes RFC 2821 & RFC 821 |
| RFC 821 | Simple Mail Transfer Protocol | Obsoleted | Original 1982 SMTP spec |
| RFC 2821 | Simple Mail Transfer Protocol | Obsoleted | Superseded by RFC 5321 |
| RFC 1869 | SMTP Service Extensions | Obsoleted | Original ESMTP framework (now part of 5321) |
| RFC 5336 | SMTP Extension for Internationalized Email Addresses | Experimental | Early i18n work |
| RFC 6531 | SMTP Extension for Internationalized Email | Standard | UTF-8 in SMTP envelope (SMTPUTF8) |

### SMTP Extensions (EHLO Keywords)

| RFC | Title | EHLO Keyword | Notes |
|-----|-------|-------------|-------|
| RFC 1870 | SMTP Service Extension for Message Size Declaration | SIZE | Declare max message size |
| RFC 2920 | SMTP Service Extension for Command Pipelining | PIPELINING | Batch commands without waiting |
| RFC 3030 | SMTP Service Extensions for Transmission of Large and Binary MIME Messages | CHUNKING / BINARYMIME | BDAT command for large messages |
| RFC 3207 | SMTP Service Extension for Secure SMTP over TLS | STARTTLS | **TLS upgrade for SMTP** |
| RFC 4954 | SMTP Service Extension for Authentication | AUTH | **SMTP AUTH** — supersedes RFC 2554 |
| RFC 2554 | SMTP Service Extension for Authentication | AUTH | Obsoleted by RFC 4954 |
| RFC 3461 | SMTP Service Extension for Delivery Status Notifications | DSN | Request delivery receipts |
| RFC 6152 | SMTP Service Extension for 8-bit MIME Transport | 8BITMIME | 8-bit data transmission |
| RFC 6409 | Message Submission for Mail | — | **Port 587** — mail submission agent (MSA) spec |
| RFC 4468 | Message Submission BURL Extension | BURL | Submit by reference (with IMAP URLAUTH) |
| RFC 2034 | SMTP Service Extension for Returning Enhanced Error Codes | ENHANCEDSTATUSCODES | Structured error codes |
| RFC 3885 | SMTP Service Extension for Message Tracking | MTRK | Message tracking support |
| RFC 4141 | SMTP and MIME Extensions for Content Conversion | CONPERM / CONNEG | Content conversion negotiation |
| RFC 2852 | Deliver By SMTP Service Extension | DELIVERBY | Request delivery time limit |
| RFC 7293 | The Require-Recipient-Valid-Since Header Field and SMTP Service Extension | RRVS | Recipient address validity |
| RFC 8689 | SMTP Require TLS Option | REQUIRETLS | Mandate TLS for message delivery |

### SMTP Status Codes & Responses

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5248 | A Registry for SMTP Enhanced Mail System Status Codes | IANA registry for enhanced status codes |
| RFC 3463 | Enhanced Mail System Status Codes | Defines X.Y.Z status code structure |
| RFC 4468 | Message Submission BURL Extension | BURL-specific status codes |
| RFC 7372 | Email Authentication Status Codes | Status codes for auth failures (SPF/DKIM/DMARC) |

### SMTP Routing & Delivery

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5321 §5 | Address Resolution and Mail Handling | MX record lookup rules |
| RFC 7505 | A "Null MX" No Service Resource Record | Domain explicitly declares no mail service |
| RFC 5068 | Email Submission Operations: Access and Accountability | MSA/MTA architecture best practices |
| RFC 2505 | Anti-Spam Recommendations for SMTP MTAs | BCP 30 — relay control, best practices |
| RFC 7489 | Domain-based Message Authentication, Reporting, and Conformance (DMARC) | Policy alignment for SPF + DKIM |
| RFC 8461 | SMTP MTA Strict Transport Security (MTA-STS) | TLS policy enforcement via DNS + HTTPS |
| RFC 8460 | SMTP TLS Reporting | TLS-RPT — reporting for MTA-STS failures |

---

## 2. POP3 — Post Office Protocol v3

| RFC | Title | Status | Notes |
|-----|-------|--------|-------|
| RFC 1939 | Post Office Protocol — Version 3 | Standard | **Current POP3 spec** |
| RFC 2449 | POP3 Extension Mechanism | Standard | CAPA command, extension framework |
| RFC 2595 | Using TLS with IMAP, POP3 and ACAP | Obsoleted | STARTTLS for POP3 (superseded by RFC 8314) |
| RFC 8314 | Cleartext Considered Obsolete: Use of TLS for Email Submission and Access | BCP | **Implicit TLS on port 995** — deprecates STARTTLS |
| RFC 1734 | POP3 AUTHentication command | Obsoleted | Original AUTH — superseded by RFC 5034 |
| RFC 5034 | The Post Office Protocol (POP3) — Simple Authentication and Security Layer (SASL) | Standard | **POP3 SASL AUTH** |
| RFC 3206 | The SYS and AUTH POP Response Codes | Standard | Extended response codes |
| RFC 2384 | POP URL Scheme | Standard | `pop://` URI format |
| RFC 1strstrstrstrstrstrstr | POP3 UIDL | — | Unique-ID Listing (part of RFC 1939) |

---

## 3. IMAP — Internet Message Access Protocol

### Core Protocol

| RFC | Title | Status | Notes |
|-----|-------|--------|-------|
| RFC 9051 | Internet Message Access Protocol (IMAP) — Version 4rev2 | Standard | **Latest IMAP spec** (2021) |
| RFC 3501 | Internet Message Access Protocol — Version 4rev1 | Obsoleted | Previous standard, still widely implemented |
| RFC 2060 | IMAP4rev1 | Obsoleted | Superseded by RFC 3501 |
| RFC 1730 | IMAP4 | Obsoleted | Original IMAP4 |

### IMAP Extensions — Mailbox Management

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 2177 | IMAP4 IDLE command | **Server push notifications** — IDLE |
| RFC 5161 | The IMAP ENABLE Extension | Enable server capabilities per-session |
| RFC 2342 | IMAP4 Namespace | NAMESPACE command — personal/shared/public folders |
| RFC 3348 | IMAP4 Child Mailbox Extension | CHILDREN — list child mailbox existence |
| RFC 5258 | IMAP4 — LIST Command Extensions | Extended LIST (LSUB replacement) |
| RFC 5819 | IMAP4 Extension for Returning STATUS in LIST | STATUS data in LIST responses |
| RFC 6154 | IMAP LIST Extension for Special-Use Mailboxes | `\Sent`, `\Drafts`, `\Trash`, `\Junk`, etc. |
| RFC 4315 | IMAP UIDPLUS Extension | UID EXPUNGE, APPENDUID, COPYUID |
| RFC 7162 | IMAP Extensions: Quick Flag Changes Resynchronization (CONDSTORE) and Quick Mailbox Resynchronization (QRESYNC) | **Efficient sync** — MODSEQ, HIGHESTMODSEQ |
| RFC 4551 | IMAP Extension for Conditional STORE Operation | Original CONDSTORE (superseded by 7162) |
| RFC 5162 | IMAP4 Extensions for Quick Mailbox Resynchronization | Original QRESYNC (superseded by 7162) |
| RFC 3502 | IMAP MULTIAPPEND Extension | Append multiple messages atomically |
| RFC 4469 | IMAP CATENATE Extension | Server-side message composition |
| RFC 6851 | Internet Message Access Protocol (IMAP) — MOVE Extension | MOVE command |

### IMAP Extensions — Search & Sort

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5256 | IMAP SORT and THREAD Extensions | Server-side sorting & threading |
| RFC 6237 | IMAP4 Multimailbox SEARCH Extension | Search across multiple mailboxes |
| RFC 5267 | Contexts for IMAP4 | CONTEXT=SEARCH, CONTEXT=SORT — updated results |
| RFC 7377 | IMAP4 Multimailbox SEARCH Extension (update) | Updates RFC 6237 |
| RFC 4466 | Collected Extensions to IMAP4 ABNF | Formal grammar updates |

### IMAP Extensions — Fetch & Body

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 3516 | IMAP4 Binary Content Extension | BINARY fetch for decoded content |
| RFC 4466 | Collected Extensions to IMAP4 ABNF | FETCH modifiers syntax |
| RFC 5524 | Extended URLFETCH for Binary and Converted Parts | URLFETCH extensions |
| RFC 8474 | IMAP Extension for Object Identifiers | OBJECTID — unique message/mailbox/thread IDs |
| RFC 8438 | IMAP Extension for STATUS=SIZE | Mailbox size without fetching |
| RFC 8508 | IMAP REPLACE Extension | Atomic replace of existing message |
| RFC 8514 | IMAP SAVEDATE Extension | Server-received date |

### IMAP Extensions — Authentication & Security

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 4959 | IMAP Extension for Simple Authentication and Security Layer (SASL) Initial Client Response | SASL-IR — fewer round-trips |
| RFC 2595 | Using TLS with IMAP, POP3 and ACAP | Original STARTTLS — superseded by RFC 8314 |
| RFC 8314 | Cleartext Considered Obsolete | **Implicit TLS port 993** — best practice |
| RFC 7817 | Updated Transport Layer Security (TLS) Server Identity Check for Email Protocols | Certificate verification for IMAP/POP/SMTP |

### IMAP Extensions — Other

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5464 | The IMAP METADATA Extension | Per-mailbox and server metadata |
| RFC 5465 | The IMAP NOTIFY Extension | Asynchronous event notifications |
| RFC 4978 | The IMAP COMPRESS Extension | DEFLATE compression |
| RFC 5032 | WITHIN Search Extension for IMAP | Date-relative search (OLDER/YOUNGER) |
| RFC 5182 | IMAP Extension for Referencing the Last SEARCH Result | SEARCHRES — $ tag |
| RFC 5550 | The Internet Email to Support Diverse Service Environments (Lemonade) Profile | Mobile email profile |
| RFC 4467 | IMAP URLAUTH Extension | URL-based message access (for BURL) |
| RFC 5092 | IMAP URL Scheme | `imap://` URI format |
| RFC 5530 | IMAP Response Codes | Extended response codes (ALREADYEXISTS, etc.) |
| RFC 5738 | IMAP Support for UTF-8 | UTF8=ACCEPT, UTF8=ALL |
| RFC 6855 | IMAP Support for UTF-8 (updated) | Updates RFC 5738 |
| RFC 2971 | IMAP4 ID Extension | Client/server identification |
| RFC 5255 | IMAP Internationalization | I18NLEVEL=1/2 |
| RFC 4731 | IMAP4 Extension to SEARCH for Controlling What Is Returned | ESEARCH — extended search results |

---

## 4. Message Format

### Core Message Format

| RFC | Title | Status | Notes |
|-----|-------|--------|-------|
| RFC 5322 | Internet Message Format | Standard | **Current email message format** — supersedes RFC 2822 |
| RFC 2822 | Internet Message Format | Obsoleted | Superseded by RFC 5322 |
| RFC 822 | Standard for ARPA Internet Text Messages | Obsoleted | Original 1982 format |
| RFC 6532 | Internationalized Email Headers | Standard | UTF-8 in headers |
| RFC 6854 | Update to Internet Message Format to Allow Group Syntax in the "From:" and "Sender:" Header Fields | Standard | Group syntax in From/Sender |

### MIME — Multipurpose Internet Mail Extensions

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 2045 | MIME Part 1: Format of Internet Message Bodies | Content-Type, Content-Transfer-Encoding |
| RFC 2046 | MIME Part 2: Media Types | multipart/*, text/*, application/*, etc. |
| RFC 2047 | MIME Part 3: Message Header Extensions for Non-ASCII Text | Encoded-words (=?charset?encoding?text?=) |
| RFC 2048 | MIME Part 4: Registration Procedures | Media type registration (updated by RFC 6838) |
| RFC 2049 | MIME Part 5: Conformance Criteria and Examples | Conformance and examples |
| RFC 2183 | Content-Disposition Header Field | `inline`, `attachment`, `filename` |
| RFC 2231 | MIME Parameter Value and Encoded Word Extensions | Long/encoded parameter values |
| RFC 6838 | Media Type Specifications and Registration Procedures | Updated media type registry procedures |

### Message Structure & Threading

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 2557 | MIME Encapsulation of Aggregate Documents (MHTML) | Web page archiving in email |
| RFC 2392 | Content-ID and Message-ID Uniform Resource Locators | `cid:` and `mid:` URLs |
| RFC 5064 | The Archived-At Message Header Field | Link to web archive |
| RFC 4021 | Registration of Mail and MIME Header Fields | Comprehensive header field registry |
| RFC 2369 | The Use of URLs as Meta-Syntax for Core Mail List Headers | List-Unsubscribe, List-Help, etc. |
| RFC 8058 | Signaling One-Click Functionality for List Email Headers | List-Unsubscribe-Post (one-click) |

---

## 5. Authentication & Identity

### SPF — Sender Policy Framework

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 7208 | Sender Policy Framework (SPF) | **Current SPF spec** |
| RFC 4408 | Sender Policy Framework | Obsoleted by RFC 7208 |

### DKIM — DomainKeys Identified Mail

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 6376 | DomainKeys Identified Mail (DKIM) Signatures | **Current DKIM spec** |
| RFC 6377 | DomainKeys Identified Mail (DKIM) and Mailing Lists | DKIM with mailing lists |
| RFC 8301 | Cryptographic Algorithm and Key Usage Update to DKIM | RSA 1024-bit minimum, SHA-256 required |
| RFC 8463 | A New Cryptographic Signature Method for DKIM | Ed25519-SHA256 |
| RFC 8617 | The Authenticated Received Chain (ARC) Protocol | Preserve auth across forwarders |

### DMARC — Domain-based Message Authentication

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 7489 | Domain-based Message Authentication, Reporting, and Conformance (DMARC) | **DMARC spec** |
| RFC 7960 | Interoperability Issues between DMARC and Indirect Email Flows | DMARC mailing list issues |

### SASL — Simple Authentication and Security Layer

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 4422 | Simple Authentication and Security Layer (SASL) | **SASL framework** |
| RFC 4616 | The PLAIN SASL Mechanism | PLAIN (username + password over TLS) |
| RFC 7628 | A Set of SASL Mechanisms for OAuth | OAUTHBEARER |
| RFC 5802 | Salted Challenge Response Authentication Mechanism (SCRAM) | SCRAM-SHA-1 |
| RFC 7677 | SCRAM-SHA-256 SASL and GSS-API Mechanisms | SCRAM-SHA-256 |
| RFC 4752 | The Kerberos V5 ("GSSAPI") SASL Mechanism | GSSAPI (Kerberos) |
| RFC 4505 | Anonymous SASL Mechanism | ANONYMOUS |
| RFC 9051 | IMAP4rev2 | Incorporates SASL mechanisms |

### OAuth / Token-Based Auth for Email

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 7628 | A Set of SASL Mechanisms for OAuth | OAUTHBEARER for IMAP/SMTP |
| RFC 6749 | The OAuth 2.0 Authorization Framework | OAuth 2.0 core (used by Gmail, Outlook) |
| RFC 7523 | JSON Web Token (JWT) Profile for OAuth 2.0 | JWT bearer tokens |

---

## 6. Security & TLS

### Transport Security

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 8314 | Cleartext Considered Obsolete | **Implicit TLS on 465/993/995** — current BCP |
| RFC 3207 | SMTP Service Extension for Secure SMTP over TLS | STARTTLS for SMTP |
| RFC 8461 | SMTP MTA Strict Transport Security (MTA-STS) | DNS + HTTPS TLS policy |
| RFC 8460 | SMTP TLS Reporting | TLS failure reporting |
| RFC 7817 | Updated TLS Server Identity Check for Email | Certificate verification rules |
| RFC 8689 | SMTP Require TLS Option | Per-message TLS requirement |

### S/MIME — End-to-End Encryption

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 8551 | Secure/Multipurpose Internet Mail Extensions (S/MIME) v4.0 Message Specification | **S/MIME v4** |
| RFC 8550 | S/MIME v4.0 Certificate Handling | Certificate processing |
| RFC 5751 | S/MIME v3.2 Message Specification | Previous version |

### OpenPGP

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 3156 | MIME Security with OpenPGP | PGP/MIME |
| RFC 4880 | OpenPGP Message Format | OpenPGP format |
| RFC 9580 | OpenPGP | Updated OpenPGP (2024) |

### DANE — DNS-Based Authentication of Named Entities

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 7671 | The DNS-Based Authentication of Named Entities (DANE) Protocol | DANE for TLS |
| RFC 7672 | SMTP Security via Opportunistic DANE TLS | **DANE for SMTP** |
| RFC 6698 | The DNS-Based Authentication of Named Entities (DANE) Transport Layer Security Protocol: TLSA | TLSA records |

---

## 7. Delivery Status & Notifications

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 3461 | SMTP Service Extension for Delivery Status Notifications (DSN) | SMTP DSN extension |
| RFC 3464 | An Extensible Message Format for Delivery Status Notifications | DSN message format |
| RFC 3798 | Message Disposition Notification (MDN) | Read receipts / MDN |
| RFC 6533 | Internationalized Delivery Status and Disposition Notifications | UTF-8 DSN/MDN |
| RFC 3886 | An Extensible Message Format for Message Tracking Responses | Message tracking format |
| RFC 5765 | Security Issues in SMTP Message Transmission | Security analysis of delivery |
| RFC 6522 | The Multipart/Report Media Type for the Reporting of Mail System Administrative Messages | multipart/report format |

---

## 8. Anti-Spam, Filtering & Abuse

### Sieve Mail Filtering

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5228 | Sieve: An Email Filtering Language | **Sieve core spec** |
| RFC 5173 | Sieve Email Filtering: Body Extension | Body content tests |
| RFC 5229 | Sieve Email Filtering: Variables Extension | Variable support |
| RFC 5230 | Sieve Email Filtering: Vacation Extension | Auto-reply / vacation |
| RFC 5232 | Sieve Email Filtering: Imap4flags Extension | IMAP flag manipulation |
| RFC 5233 | Sieve Email Filtering: Subaddress Extension | user+tag@domain handling |
| RFC 5235 | Sieve Email Filtering: Spamtest and Virustest Extensions | Spam/virus score tests |
| RFC 5260 | Sieve Email Filtering: Date and Index Extensions | Date-based filtering |
| RFC 5293 | Sieve Email Filtering: Editheader Extension | Modify message headers |
| RFC 5429 | Sieve Email Filtering: Reject and Extended Reject Extensions | Reject with reason |
| RFC 5435 | Sieve Email Filtering: Extension for Notifications | External notifications |
| RFC 5490 | The Sieve Mail-Filtering Language — Extensions for Checking Mailbox Status | Mailbox existence tests |
| RFC 5703 | Sieve Email Filtering: MIME Part Tests, Iteration, Extraction, and Replacement | MIME processing |
| RFC 6609 | Sieve Email Filtering: Include Extension | Include/import scripts |
| RFC 6785 | Support for IMAP Events in Sieve | IMAP event triggers (IMAPSIEVE) |
| RFC 3028 | Sieve: A Mail Filtering Language | Original spec (obsoleted by 5228) |

### ManageSieve Protocol

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5804 | A Protocol for Remotely Managing Sieve Scripts | ManageSieve (port 4190) |

### Anti-Spam Technologies

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 2505 | Anti-Spam Recommendations for SMTP MTAs | BCP 30 |
| RFC 5782 | DNS Blacklists and Whitelists (DNSBL/DNSWL) | RBL lookup standardization |
| RFC 6647 | Email Greylisting: An Applicability Statement for SMTP | Greylisting practices |
| RFC 8601 | Message Header Field for Indicating Message Authentication Status | Authentication-Results header |
| RFC 7001 | Message Header Field for Indicating Message Authentication Status | Previous version (superseded by 8601) |
| RFC 7208 | Sender Policy Framework (SPF) | SPF — see §5 |
| RFC 6376 | DomainKeys Identified Mail (DKIM) | DKIM — see §5 |
| RFC 7489 | DMARC | DMARC — see §5 |

### Abuse Reporting

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5965 | An Extensible Format for Email Feedback Reports | ARF — Abuse Reporting Format |
| RFC 6430 | Email Feedback Report Type Value: not-spam | "Not spam" report type |
| RFC 6591 | Authentication Failure Reporting Using ARF | Auth failure feedback |
| RFC 6650 | Creation and Use of Email Feedback Reports: An Applicability Statement for the ARF | ARF usage guide |

---

## 9. Webmail & HTTP-Based Protocols

### JMAP — JSON Meta Application Protocol

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 8620 | The JSON Meta Application Protocol (JMAP) | **JMAP core spec** — modern webmail API |
| RFC 8621 | The JSON Meta Application Protocol (JMAP) for Mail | **JMAP Mail** — IMAP replacement for webmail |
| RFC 8887 | A JSON Meta Application Protocol (JMAP) Subprotocol for WebSocket | JMAP over WebSocket |

### Other Web/HTTP

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 8984 | JSCalendar: A JSON Representation of Calendar Data | Calendar data (CalDAV interop) |
| RFC 6186 | Use of SRV Records for Locating Email Submission/Access Services | **SRV autodiscovery** for IMAP/SMTP/POP |
| RFC 6764 | Locating Services for Calendaring Extensions to WebDAV (CalDAV) and vCard Extensions to WebDAV (CardDAV) | CalDAV/CardDAV discovery |

---

## 10. Address & Mailbox Standards

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5321 §2.3 | Terminology: Mailbox | `local-part@domain` format |
| RFC 5322 §3.4 | Address Specification | Addr-spec, display name |
| RFC 6530 | Overview and Framework for Internationalized Email | EAI architecture |
| RFC 6531 | SMTP Extension for Internationalized Email | SMTPUTF8 |
| RFC 6532 | Internationalized Email Headers | UTF-8 headers |
| RFC 6533 | Internationalized Delivery Status and Disposition Notifications | UTF-8 DSN/MDN |
| RFC 5233 | Sieve Subaddress Extension | `user+tag@domain` sub-addressing |
| RFC 2142 | Mailbox Names for Common Services, Roles and Functions | postmaster@, abuse@, etc. |
| RFC 5765 | Security Issues in SMTP | Address security considerations |

---

## 11. DNS Records for Email

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 1035 | Domain Names — Implementation and Specification | DNS core (A, CNAME, MX) |
| RFC 7208 | SPF | TXT records for SPF |
| RFC 6376 | DKIM | TXT records for DKIM public keys |
| RFC 7489 | DMARC | `_dmarc` TXT records |
| RFC 8461 | MTA-STS | `_mta-sts` TXT + HTTPS policy file |
| RFC 8460 | SMTP TLS Reporting | `_smtp._tls` TXT records |
| RFC 6186 | SRV Records for Email | `_submission._tcp`, `_imap._tcp`, `_pop3._tcp` |
| RFC 7505 | Null MX | "Null MX" — declare no mail service |
| RFC 6698 | DANE TLSA | TLSA records for certificate pinning |
| RFC 7672 | SMTP Security via DANE | DANE for MX servers |
| RFC 7929 | DNS-Based Authentication for S/MIME | SMIMEA records |

---

## 12. Mailing Lists

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 2369 | URLs as Meta-Syntax for Core Mail List Headers | List-Unsubscribe, List-Help, List-Post, etc. |
| RFC 2919 | List-Id: A Structured Field and Namespace for the Identification of Mailing Lists | List-Id header |
| RFC 8058 | Signaling One-Click Functionality for List Email | List-Unsubscribe-Post |
| RFC 6377 | DKIM and Mailing Lists | DKIM handling for list servers |
| RFC 6783 | Mailing Lists and Non-ASCII Addresses | EAI and mailing lists |

---

## 13. Calendar & Contacts (Common in Email Servers)

### CalDAV / iCalendar

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 5545 | Internet Calendaring and Scheduling (iCalendar) | iCalendar format (.ics) |
| RFC 4791 | Calendaring Extensions to WebDAV (CalDAV) | CalDAV protocol |
| RFC 6047 | iCalendar Message-Based Interoperability Protocol (iMIP) | Calendar invites via email |
| RFC 5546 | iCalendar Transport-Independent Interoperability Protocol (iTIP) | Scheduling protocol |
| RFC 6638 | Scheduling Extensions to CalDAV | CalDAV scheduling |
| RFC 6764 | Locating CalDAV and CardDAV Services | SRV-based discovery |
| RFC 7986 | New Properties for iCalendar | Additional iCal properties |

### CardDAV / vCard

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 6350 | vCard Format Specification | vCard 4.0 |
| RFC 6352 | CardDAV: vCard Extensions to WebDAV | CardDAV protocol |
| RFC 2426 | vCard MIME Directory Profile | vCard 3.0 |

---

## 14. Autoconfig & Service Discovery

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 6186 | Use of SRV Records for Locating Email Services | SRV records: `_imap._tcp`, `_submission._tcp` |
| RFC 6764 | Locating CalDAV and CardDAV Services | Well-known URIs |
| RFC 8615 | Well-Known Uniform Resource Identifiers (URIs) | `/.well-known/` framework |
| — | Mozilla Autoconfig | `autoconfig.domain.com/mail/config-v1.1.xml` (de facto) |
| — | Microsoft Autodiscover | `/autodiscover/autodiscover.xml` (de facto) |

---

## 15. Email Archival & Conversion

| RFC | Title | Notes |
|-----|-------|-------|
| RFC 4155 | The application/mbox Media Type | mbox format |
| RFC 5765 | Security Issues in SMTP | Archival considerations |
| RFC 6857 | Post-Delivery Message Downgrading for Internationalized Email | Downgrade EAI for legacy systems |
| RFC 5064 | The Archived-At Message Header Field | Link to archived copy |

---

## 16. Port Assignments Summary

| Port | Protocol | Transport | RFC | Notes |
|------|----------|-----------|-----|-------|
| 25 | SMTP | TCP | RFC 5321 | MTA-to-MTA relay (often blocked by ISPs) |
| 465 | SMTPS (Submissions) | TCP/TLS | RFC 8314 | **Implicit TLS submission** (re-assigned) |
| 587 | SMTP Submission | TCP | RFC 6409 | MSA submission (STARTTLS) |
| 110 | POP3 | TCP | RFC 1939 | Cleartext POP3 |
| 995 | POP3S | TCP/TLS | RFC 8314 | **Implicit TLS POP3** |
| 143 | IMAP | TCP | RFC 9051 | Cleartext IMAP |
| 993 | IMAPS | TCP/TLS | RFC 8314 | **Implicit TLS IMAP** |
| 4190 | ManageSieve | TCP | RFC 5804 | Sieve script management |
| 443 | HTTPS / JMAP | TCP/TLS | RFC 8620 | Webmail / JMAP API |

---

## 17. Key Obsolescence Chain

Understanding which RFCs supersede which:

```
SMTP:    RFC 821 → RFC 2821 → RFC 5321
Format:  RFC 822 → RFC 2822 → RFC 5322
IMAP:    RFC 1730 → RFC 2060 → RFC 3501 → RFC 9051
POP3:    RFC 1939 (still current)
MIME:    RFC 2045-2049 (still current)
SPF:     RFC 4408 → RFC 7208
DKIM:    RFC 4871 → RFC 6376
TLS:     RFC 2595 + RFC 3207 → RFC 8314 (implicit TLS preferred)
SASL:    RFC 2222 → RFC 4422
SMTP AUTH: RFC 2554 → RFC 4954
```

---

## 18. Recommended Reading Order for Implementors

1. **RFC 5321** — SMTP (core sending)
2. **RFC 5322** — Message format
3. **RFC 2045–2049** — MIME
4. **RFC 9051** — IMAP4rev2 (or 3501 for compatibility)
5. **RFC 1939** — POP3
6. **RFC 8314** — Implicit TLS (security baseline)
7. **RFC 4954** — SMTP AUTH
8. **RFC 7208** — SPF
9. **RFC 6376** — DKIM
10. **RFC 7489** — DMARC
11. **RFC 8461** — MTA-STS
12. **RFC 5228** — Sieve filtering
13. **RFC 8620 + 8621** — JMAP (modern webmail)
14. **RFC 6186** — SRV autodiscovery

---

*Last updated: April 2026. This document covers RFCs relevant to building a production email server with modern security and interoperability standards.*
