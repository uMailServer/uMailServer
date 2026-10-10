package queue

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DSNNotify represents delivery status notification preferences
type DSNNotify int

const (
	DSNNotifyNever   DSNNotify = 1 << iota // NEVER - never send DSN
	DSNNotifySuccess                       // SUCCESS - notify on successful delivery
	DSNNotifyFailure                       // FAILURE - notify on failed delivery
	DSNNotifyDelay                         // DELAY - notify if delivery is delayed
)

// ParseDSNNotify converts an SMTP NOTIFY parameter value (NEVER, SUCCESS,
// FAILURE, DELAY) into a DSNNotify bitmask. Multiple values can be OR'd
// together. Empty or unrecognized tokens are silently ignored.
func ParseDSNNotify(value string) DSNNotify {
	if value == "" {
		return 0
	}
	var notify DSNNotify
	for _, token := range strings.Split(strings.ToUpper(value), ",") {
		token = strings.TrimSpace(token)
		switch token {
		case "NEVER":
			notify |= DSNNotifyNever
		case "SUCCESS":
			notify |= DSNNotifySuccess
		case "FAILURE":
			notify |= DSNNotifyFailure
		case "DELAY":
			notify |= DSNNotifyDelay
		}
	}
	return notify
}

// DSNRet represents what to return in DSN
type DSNRet int

const (
	DSNRetFull    DSNRet = iota // Return full message
	DSNRetHeaders               // Return headers only
)

// DSNAddress represents an address with DSN notification options
type DSNAddress struct {
	Original string    // Original address string
	Notify   DSNNotify // Notification options
}

// ParseDSNNotify parses the NOTIFY parameter from RCPT TO
// Format: NOTIFY=NEVER|SUCCESS|FAILURE|DELAY[,...]
// (deduplicated — canonical implementation is at line 25)
func (n DSNNotify) HasNotify(notify DSNNotify) bool {
	return n&notify != 0
}

// ParseDSNRet parses the RET parameter from MAIL FROM
// Format: RET=FULL| HDRS
func ParseDSNRet(ret string) DSNRet {
	ret = strings.ToUpper(ret)
	if strings.HasSuffix(ret, "HDRS") {
		return DSNRetHeaders
	}
	return DSNRetFull
}

// DSN represents a Delivery Status Notification
type DSN struct {
	ReportedDomain string    // Reporting MTA domain
	ReportedName   string    // Reporting MTA name
	ArrivalDate    time.Time // When message was received
	OriginalFrom   string    // Original MAIL FROM
	OriginalTo     string    // Original RCPT TO
	Recipient      DSNRecipient
	Action         string // failed, delayed, delivered, expanded
	Status         string // Status code (e.g., 2.0.0 for success)
	DiagnosticCode string // Diagnostic code (smtp, X.400, etc.)
	RemoteMTA      string // Remote MTA that attempted delivery
	FinalMTA       string // Final MTA
	MessageID      string // Message ID of the DSN
}

// DSNRecipient holds per-recipient DSN information
type DSNRecipient struct {
	Original       string    // Original recipient address
	DSNAddress     string    // DSN address (may differ for DSN)
	Notify         DSNNotify // What to notify
	Ret            DSNRet    // What to return
	OriginalFields *mail.Header
}

// GenerateMessageID generates a unique Message-ID for the DSN
func GenerateMessageID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: fill all 8 bytes from timestamp bits when crypto/rand fails.
		// crypto/rand failures are extremely rare (ENOMEM, EIO), but the
		// previous fallback only filled 2 bytes, leaking 0x000000000000 into
		// the Message-ID and making it collision-prone.
		ts := time.Now().UnixNano()
		for i := 0; i < 8; i++ {
			b[i] = byte(ts >> (i * 8))
		}
	}
	return fmt.Sprintf("<%d.%s@umailserver>", time.Now().UnixNano(), hex.EncodeToString(b))
}

// maxDSNOriginalSize bounds the original message a DSN returns in full.
const maxDSNOriginalSize = 128 * 1024

// GenerateDSN generates a Delivery Status Notification message
func GenerateDSN(dsn *DSN, originalMessage []byte, ret DSNRet) ([]byte, error) {
	// Determine what to include from original message
	// A full return larger than maxDSNOriginalSize is cut down to the headers
	// (RFC 3461 §4.3 lets the MTA return headers only), so a bounce of a
	// large attachment is not itself oversized and bounced again (F5902).
	// Headers-only content is labelled text/rfc822-headers (RFC 3462 §2).
	var originalPart string
	originalType := "message/rfc822"
	if ret == DSNRetFull && len(originalMessage) <= maxDSNOriginalSize {
		originalPart = string(originalMessage)
	} else {
		originalPart = extractHeaders(string(originalMessage))
		originalType = "text/rfc822-headers"
	}
	if dsn.MessageID == "" {
		dsn.MessageID = GenerateMessageID()
	}

	// Generate DSN message
	boundary := "boundary_" + generateBoundary()

	dsnMsg := fmt.Sprintf(
		"From: MAILER-DAEMON@%s\r\n"+
			"To: %s\r\n"+
			"Subject: Delivery Status Notification\r\n"+
			"Content-Type: multipart/report; report-type=delivery-status; boundary=%s\r\n"+
			"Date: %s\r\n"+
			"Message-ID: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Auto-Submitted: auto-generated\r\n"+
			"\r\n"+
			"--%s\r\n"+
			"Content-Type: text/plain\r\n"+
			"\r\n"+
			"Delivery to the following recipient %s:\r\n"+
			"    %s\r\n\r\n"+
			"Action: %s\r\n"+
			"Status: %s\r\n"+
			"\r\n"+
			"--%s\r\n"+
			"Content-Type: message/delivery-status\r\n"+
			"\r\n"+
			"Reporting-MTA: dns; %s\r\n"+
			"Arrival-Date: %s\r\n"+
			"\r\n"+
			"Final-Recipient: rfc822; %s\r\n"+
			"Action: %s\r\n"+
			"Status: %s\r\n"+
			"",
		dsn.ReportedDomain,
		dsn.OriginalFrom,
		boundary,
		time.Now().Format(time.RFC1123Z),
		dsn.MessageID,
		boundary,
		dsn.Action,
		dsn.OriginalTo,
		dsn.Action,
		dsn.Status,
		boundary,
		dsn.ReportedDomain,
		dsn.ArrivalDate.Format(time.RFC1123Z),
		dsn.Recipient.Original,
		dsn.Action,
		dsn.Status,
	)

	// Remote-MTA is optional (RFC 3464 §2.3.5): omit it when no remote host
	// is known instead of printing a bogus "dns; unknown" (F5684).
	if dsn.RemoteMTA != "" && dsn.RemoteMTA != "unknown" {
		dsnMsg += fmt.Sprintf("Remote-MTA: dns; %s\r\n", dsn.RemoteMTA)
	}

	// Add diagnostic code if present
	if diag := diagnosticText(dsn.DiagnosticCode); diag != "" {
		dsnMsg += fmt.Sprintf("Diagnostic-Code: smtp; %s\r\n", diag)
	}

	// Add DSN recipient address if different
	if dsn.Recipient.DSNAddress != "" && dsn.Recipient.DSNAddress != dsn.Recipient.Original {
		dsnMsg += fmt.Sprintf("X-Actual-Recipient: rfc822; %s\r\n", dsn.Recipient.DSNAddress)
	}

	dsnMsg += fmt.Sprintf(
		"\r\n--%s\r\n"+
			"Content-Type: %s\r\n"+
			"\r\n"+
			"%s\r\n"+
			"\r\n--%s--\r\n",
		boundary,
		originalType,
		originalPart,
		boundary,
	)

	return []byte(dsnMsg), nil
}

// GenerateSuccessDSN generates a DSN for successful delivery
func GenerateSuccessDSN(dsn *DSN, originalMessage []byte, ret DSNRet) ([]byte, error) {
	dsn.Action = "delivered"
	dsn.Status = "2.0.0"
	dsn.FinalMTA = dsn.ReportedDomain
	return GenerateDSN(dsn, originalMessage, ret)
}

// GenerateFailureDSN generates a DSN for failed delivery
func GenerateFailureDSN(dsn *DSN, originalMessage []byte, ret DSNRet, errorMsg string) ([]byte, error) {
	dsn.Action = "failed"
	dsn.Status = "5.0.0"
	dsn.DiagnosticCode = errorMsg
	dsn.FinalMTA = dsn.ReportedDomain
	return GenerateDSN(dsn, originalMessage, ret)
}

// GenerateDelayDSN generates a DSN for delayed delivery
func GenerateDelayDSN(dsn *DSN) ([]byte, error) {
	dsn.Action = "delayed"
	dsn.Status = "4.0.0"
	dsn.FinalMTA = dsn.ReportedDomain
	return GenerateDSN(dsn, []byte{}, DSNRetHeaders)
}

// quotedReplyRe matches net/textproto.Error's rendering `550 "text"`.
var quotedReplyRe = regexp.MustCompile(`^(\d{3}) ("(?:[^"\\]|\\.)*")$`)

// maxDiagnosticLen bounds the Diagnostic-Code field to one header line.
const maxDiagnosticLen = 500

// diagnosticText renders an SMTP reply as the text of a Diagnostic-Code field
// (RFC 3464 §2.3.2): textproto's quoted form is unquoted, a duplicate "smtp; "
// type prefix is dropped and line breaks or control characters are collapsed
// so a multi-line remote reply stays within one field (F5684).
func diagnosticText(s string) string {
	if m := quotedReplyRe.FindStringSubmatch(s); m != nil {
		if u, err := strconv.Unquote(m[2]); err == nil {
			s = m[1] + " " + u
		}
	}
	s = strings.TrimSpace(s)
	if len(s) >= 5 && strings.EqualFold(s[:5], "smtp;") {
		s = strings.TrimSpace(s[5:])
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxDiagnosticLen {
		s = s[:maxDiagnosticLen]
	}
	return s
}

// extractHeaders extracts only the headers from a message
func extractHeaders(msg string) string {
	idx := strings.Index(msg, "\r\n\r\n")
	if lfIdx := strings.Index(msg, "\n\n"); lfIdx >= 0 && (idx < 0 || lfIdx < idx) {
		idx = lfIdx
	}
	if idx < 0 {
		return msg
	}
	return msg[:idx]
}

func generateBoundary() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use timestamp if crypto/rand fails (extremely rare)
		ts := time.Now().UnixNano()
		for i := 0; i < 8; i++ {
			b[i] = byte(ts >> (i * 8))
		}
	}
	return hex.EncodeToString(b)
}
