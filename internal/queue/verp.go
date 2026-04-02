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

// splitEmail splits an email into user and domain parts
func splitEmail(email string) (string, string) {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email, ""
	}
	return email[:at], email[at+1:]
}
