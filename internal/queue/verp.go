package queue

import (
	"fmt"
	"strings"
)

// VERP (Variable Envelope Return Path) encodes the recipient address
// into the envelope sender for bounce tracking.
// Format: bounce-user=domain@hostname
// e.g., bounce-john=example.com@mail.example.com

// EncodeVERP encodes a recipient address into a VERP bounce sender.
// senderDomain is the mail server's hostname used in the bounce address.
func EncodeVERP(senderDomain, recipient string) string {
	user, domain := splitEmail(recipient)
	return fmt.Sprintf("bounce-%s=%s@%s", user, domain, senderDomain)
}

// DecodeVERP decodes a VERP bounce address back to the original recipient.
// Returns empty string if the address is not a VERP-encoded bounce address.
func DecodeVERP(bounceAddr string) string {
	if !strings.HasPrefix(strings.ToLower(bounceAddr), "bounce-") {
		return ""
	}

	// Remove "bounce-" prefix
	rest := bounceAddr[7:]

	// Split at @ to get user part and domain part
	atIdx := strings.LastIndex(rest, "@")
	if atIdx < 0 {
		return ""
	}

	userPart := rest[:atIdx]
	// userPart should be "user=domain"
	eqIdx := strings.Index(userPart, "=")
	if eqIdx < 0 {
		return ""
	}

	user := userPart[:eqIdx]
	domain := userPart[eqIdx+1:]

	return user + "@" + domain
}

// splitEmail splits an email into user and domain parts
func splitEmail(email string) (string, string) {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email, ""
	}
	return email[:at], email[at+1:]
}
