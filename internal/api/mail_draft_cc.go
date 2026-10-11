package api

import (
	"net/mail"
	"strings"
)

// mailWithDraftRecipients is the /mail/message response for a draft: the
// regular Mail plus the Cc/Bcc headers the composer needs to restore it.
type mailWithDraftRecipients struct {
	*Mail
	CC  []string `json:"cc"`
	BCC []string `json:"bcc"`
}

// draftRecipients parses Cc and Bcc address lists from a raw draft message.
func draftRecipients(raw []byte) (cc, bcc []string) {
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return []string{}, []string{}
	}
	parse := func(name string) []string {
		out := []string{}
		for _, v := range msg.Header[name] {
			addrs, err := mail.ParseAddressList(v)
			if err != nil {
				continue
			}
			for _, a := range addrs {
				out = append(out, a.Address)
			}
		}
		return out
	}
	return parse("Cc"), parse("Bcc")
}

// draftResponse decorates a drafts-mailbox message with cc/bcc; other
// mailboxes are returned unchanged so Bcc is never exposed elsewhere.
func (h *MailHandler) draftResponse(user, mailbox string, email *Mail) any {
	if mailbox != "Drafts" || h.msgStore == nil {
		return email
	}
	data, err := h.msgStore.ReadMessage(user, email.ID)
	if err != nil {
		return email
	}
	cc, bcc := draftRecipients(data)
	return mailWithDraftRecipients{Mail: email, CC: cc, BCC: bcc}
}
