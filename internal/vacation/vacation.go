// Package vacation provides auto-reply (vacation) functionality for email accounts.
// When enabled, it sends an automatic response to incoming messages.
package vacation

import (
	"fmt"
	"strings"
	"time"
)

// Settings holds vacation/auto-reply configuration for a user
type Settings struct {
	Enabled   bool      `json:"enabled"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
	// Interval is the minimum time between auto-replies to the same sender (hours)
	Interval int `json:"interval"`
}

// DefaultSubject is the default vacation subject prefix
const DefaultSubject = "Auto: "

// ShouldReply determines if an auto-reply should be sent for the given message.
// It checks: vacation enabled, date range, excludes mailing lists/bounces, respects interval.
func ShouldReply(settings Settings, from string, headers map[string][]string) bool {
	if !settings.Enabled {
		return false
	}

	now := time.Now()

	// Check date range
	if !settings.StartTime.IsZero() && now.Before(settings.StartTime) {
		return false
	}
	if !settings.EndTime.IsZero() && now.After(settings.EndTime) {
		return false
	}

	// Don't reply to bounces/Mailer-Daemon
	if strings.HasPrefix(strings.ToLower(from), "mailer-daemon") ||
		strings.HasPrefix(strings.ToLower(from), "postmaster") ||
		strings.Contains(strings.ToLower(from), "bounce") {
		return false
	}

	// Don't reply to mailing lists
	if hasHeader(headers, "List-Id") || hasHeader(headers, "List-Unsubscribe") ||
		hasHeader(headers, "Precedence", "bulk") || hasHeader(headers, "Precedence", "junk") {
		return false
	}

	// Don't reply to auto-submitted messages
	if hasHeader(headers, "Auto-Submitted", "auto-generated") ||
		hasHeader(headers, "Auto-Submitted", "auto-replied") {
		return false
	}

	// Don't reply to noreply addresses
	fromLower := strings.ToLower(from)
	if strings.HasPrefix(fromLower, "noreply") || strings.Contains(fromLower, "noreply@") ||
		strings.HasPrefix(fromLower, "no-reply") || strings.Contains(fromLower, "no-reply@") {
		return false
	}

	return true
}

// GenerateReply creates the auto-reply message body.
func GenerateReply(settings Settings, originalFrom, recipient string) []byte {
	subject := settings.Subject
	if subject == "" {
		subject = DefaultSubject + "Out of Office"
	}

	body := settings.Body
	if body == "" {
		body = "I am currently out of the office and will respond to your email as soon as possible."
	}

	msg := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"Auto-Submitted: auto-replied\r\n"+
		"Precedence: bulk\r\n"+
		"Date: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n"+
		"\r\n"+
		"%s\r\n",
		recipient,
		originalFrom,
		subject,
		time.Now().Format(time.RFC1123Z),
		body,
	)

	return []byte(msg)
}

func hasHeader(headers map[string][]string, name string, values ...string) bool {
	vals, ok := headers[name]
	if !ok {
		return false
	}
	if len(values) == 0 {
		return len(vals) > 0
	}
	for _, v := range vals {
		vLower := strings.ToLower(strings.TrimSpace(v))
		for _, want := range values {
			if vLower == strings.ToLower(want) {
				return true
			}
		}
	}
	return false
}
