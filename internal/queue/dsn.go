package queue

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/mail"
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
func ParseDSNNotify(notify string) DSNNotify {
	notify = strings.ToUpper(notify)
	var result DSNNotify

	if notify == "NEVER" {
		return DSNNotifyNever
	}

	parts := strings.Split(notify, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch part {
		case "SUCCESS":
			result |= DSNNotifySuccess
		case "FAILURE":
			result |= DSNNotifyFailure
		case "DELAY":
			result |= DSNNotifyDelay
		}
	}

	return result
}

// HasNotify checks if the DSNNotify contains the given notification type
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
	rand.Read(b)
	return fmt.Sprintf("<%d.%s@umailserver>", time.Now().UnixNano(), hex.EncodeToString(b))
}

// GenerateDSN generates a Delivery Status Notification message
func GenerateDSN(dsn *DSN, originalMessage []byte, ret DSNRet) ([]byte, error) {
	// Determine what to include from original message
	var originalPart string
	if ret == DSNRetFull {
		originalPart = string(originalMessage)
	} else {
		// Headers only - extract headers from original message
		originalPart = extractHeaders(string(originalMessage))
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
			"\r\n"+
			"--%s\r\n"+
			"Content-Type: text/plain\r\n"+
			"\r\n"+
			"Delivery to the following recipient failed:\r\n"+
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
			"Remote-MTA: dns; %s\r\n",
		dsn.ReportedDomain,
		dsn.OriginalFrom,
		boundary,
		time.Now().Format(time.RFC1123Z),
		dsn.MessageID,
		boundary,
		dsn.OriginalTo,
		dsn.Action,
		dsn.Status,
		boundary,
		dsn.ReportedDomain,
		dsn.ArrivalDate.Format(time.RFC1123Z),
		dsn.Recipient.Original,
		dsn.Action,
		dsn.Status,
		dsn.RemoteMTA,
	)

	// Add diagnostic code if present
	if dsn.DiagnosticCode != "" {
		dsnMsg += fmt.Sprintf("Diagnostic-Code: smtp; %s\r\n", dsn.DiagnosticCode)
	}

	// Add DSN recipient address if different
	if dsn.Recipient.DSNAddress != "" && dsn.Recipient.DSNAddress != dsn.Recipient.Original {
		dsnMsg += fmt.Sprintf("X-Actual-Recipient: rfc822; %s\r\n", dsn.Recipient.DSNAddress)
	}

	dsnMsg += fmt.Sprintf(
		"\r\n--%s\r\n"+
			"Content-Type: message/rfc822\r\n"+
			"\r\n"+
			"%s\r\n"+
			"\r\n--%s--\r\n",
		boundary,
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

// extractHeaders extracts only the headers from a message
func extractHeaders(msg string) string {
	idx := strings.Index(msg, "\r\n\r\n")
	if idx < 0 {
		idx = strings.Index(msg, "\n\n")
		if idx < 0 {
			return msg
		}
		return msg[:idx]
	}
	return msg[:idx]
}

func generateBoundary() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
