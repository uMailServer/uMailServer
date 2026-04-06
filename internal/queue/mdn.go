package queue

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// MDNDisposition represents the disposition type for MDN
type MDNDisposition int

const (
	MDNDispositionDisplayed   MDNDisposition = iota
	MDNDispositionDeleted
	MDNDispositionDispatched
	MDNDispositionDenied
	MDNDispositionFailed
)

// MDN represents a Message Disposition Notification (RFC 3798)
type MDN struct {
	ReportType    string
	MessageID     string
	OriginalTo    string
	Disposition   string
	DispositionModifier string
	SentBy        string
	SentByDomain  string
	ReportingUA   string
	ArrivalDate   time.Time
	Extension     map[string]string
}

// MDNAddress represents an address from Disposition-Notification-To header
type MDNAddress struct {
	Original string
	Parsed   *mail.Address
}

// ParseMDNAddress parses a Disposition-Notification-To header value
func ParseMDNAddress(header string) (*MDNAddress, error) {
	// Can be a single address or a list
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, fmt.Errorf("empty MDN address")
	}

	// Try to parse as mail address
	addr, err := mail.ParseAddress(header)
	if err != nil {
		// Try without the angle brackets
		if strings.Contains(header, "<") {
			parts := strings.Split(header, "<")
			if len(parts) == 2 {
				addr, err = mail.ParseAddress("<" + parts[1])
				if err != nil {
					return &MDNAddress{Original: header}, nil
				}
			}
		}
		return &MDNAddress{Original: header}, nil
	}

	return &MDNAddress{
		Original: header,
		Parsed:   addr,
	}, nil
}

// GenerateMDN generates a Message Disposition Notification per RFC 3798
func GenerateMDN(originalMsg []byte, from, to, messageID, inReplyTo string, disposition MDNDisposition, reportingDomain string) ([]byte, error) {
	// Determine disposition string
	var dispStr string
	var dispMod string
	switch disposition {
	case MDNDispositionDisplayed:
		dispStr = "displayed"
	case MDNDispositionDeleted:
		dispStr = "deleted"
	case MDNDispositionDispatched:
		dispStr = "dispatched"
	case MDNDispositionDenied:
		dispStr = "denied"
	case MDNDispositionFailed:
		dispStr = "failed"
		dispMod = "system"
	default:
		dispStr = "displayed"
	}

	// Generate unique Message-ID for the MDN
	mdnMessageID := generateMDNMessageID(reportingDomain)

	// Generate boundary for multipart
	boundary := generateBoundary()

	// Extract headers from original message if available
	var originalHeaders string
	if len(originalMsg) > 0 {
		msg, err := mail.ReadMessage(strings.NewReader(string(originalMsg)))
		if err == nil {
			for key, values := range msg.Header {
				for _, v := range values {
					originalHeaders += fmt.Sprintf("%s: %s\r\n", key, v)
				}
			}
		}
	}

	// Build the MDN message
	var dispositionStr string
	if dispMod != "" {
		dispositionStr = fmt.Sprintf("manual-action/MDN-sent-manually/%s; %s", dispMod, dispStr)
	} else {
		dispositionStr = fmt.Sprintf("manual-action/MDN-sent-manually; %s", dispStr)
	}

	mdn := fmt.Sprintf(
		"From: %s\r\n"+
			"To: %s\r\n"+
			"Subject: Disposition: %s\r\n"+
			"Content-Type: multipart/report; report-type=disposition-notification; boundary=%s\r\n"+
			"Date: %s\r\n"+
			"Message-ID: %s\r\n"+
			"References: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"\r\n"+
			"--%s\r\n"+
			"Content-Type: text/plain\r\n"+
			"\r\n"+
			"Your message was displayed.\r\n"+
			"\r\n"+
			"--%s\r\n"+
			"Content-Type: message/disposition-notification\r\n"+
			"\r\n"+
			"Reporting-UA: %s; umailserver\r\n"+
			"Original-Recipient: rfc822; %s\r\n"+
			"Final-Recipient: rfc822; %s\r\n"+
			"Original-Message-ID: %s\r\n"+
			"Disposition: %s\r\n",
		reportingDomain+"@umailserver",
		to,
		dispStr,
		boundary,
		time.Now().Format(time.RFC1123Z),
		mdnMessageID,
		inReplyTo,
		boundary,
		boundary,
		reportingDomain,
		to,
		to,
		messageID,
		dispositionStr,
	)

	mdn += fmt.Sprintf(
		"\r\n--%s\r\n"+
			"Content-Type: message/rfc822\r\n"+
			"\r\n"+
			"%s\r\n"+
			"\r\n--%s--\r\n",
		boundary,
		originalHeaders,
		boundary,
	)

	return []byte(mdn), nil
}

func generateMDNMessageID(domain string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), hex.EncodeToString(b), domain)
}

// ParseDispositionHeader parses the Disposition-Notification-To header from a message
func ParseDispositionHeader(msg []byte) (string, error) {
	m, err := mail.ReadMessage(strings.NewReader(string(msg)))
	if err != nil {
		return "", err
	}

	header := m.Header.Get("Disposition-Notification-To")
	if header == "" {
		return "", fmt.Errorf("no Disposition-Notification-To header found")
	}

	return header, nil
}
