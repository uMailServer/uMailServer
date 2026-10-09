package server

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/umailserver/umailserver/internal/sieve"
)

// maxSieveRedirects bounds the redirects one script run may queue, so a
// script cannot turn one inbound message into a mail flood.
const maxSieveRedirects = 10

// sieveOutcome is what a mailbox owner's Sieve script decided for one
// message (RFC 5228 §2.10.2 implicit keep, §4 actions).
type sieveOutcome struct {
	keep      bool     // explicit keep, or the implicit keep was not cancelled
	folders   []string // fileinto targets, in script order
	redirects []string
	rejected  bool
	rejectMsg string
	vacation  *sieve.VacationAction
}

// activeSieveUser returns the key under which mailbox's active script is
// stored (ManageSieve stores scripts under the login, i.e. the address), or
// "" when it has none.
func (s *Server) activeSieveUser(mailbox string) string {
	if s.sieveManager == nil {
		return ""
	}
	if s.sieveManager.HasActiveScript(mailbox) {
		return mailbox
	}
	if lower := strings.ToLower(mailbox); lower != mailbox && s.sieveManager.HasActiveScript(lower) {
		return lower
	}
	return ""
}

// runUserSieve executes scriptUser's active script for a message to rcpt.
// ok is false when the script fails at run time; the caller then keeps the
// message (RFC 5228 §2.10.6).
func (s *Server) runUserSieve(scriptUser, rcpt, from string, data []byte) (out sieveOutcome, ok bool) {
	headers := map[string][]string{}
	if msg, err := mail.ReadMessage(strings.NewReader(string(data))); err == nil {
		headers = msg.Header
	}
	env := from
	if env == "" {
		env = "<>"
	}
	actions, err := s.sieveManager.ProcessMessage(scriptUser, &sieve.MessageContext{
		From:    env,
		To:      []string{rcpt},
		Headers: headers,
		Body:    data,
		Size:    int64(len(data)),
	})
	if err != nil {
		s.logger.Warn("Sieve script failed, keeping message", "user", scriptUser, "error", err)
		return sieveOutcome{}, false
	}
	implicitKeep := true
	for _, action := range actions {
		switch a := action.(type) {
		case sieve.KeepAction:
			out.keep = true
		case sieve.FileintoAction:
			out.folders = append(out.folders, a.Folder)
			implicitKeep = false
		case sieve.RedirectAction:
			out.redirects = append(out.redirects, a.Address)
			implicitKeep = false
		case sieve.DiscardAction:
			implicitKeep = false
		case sieve.RejectAction:
			out.rejected = true
			out.rejectMsg = a.Message
			implicitKeep = false
		case sieve.VacationAction:
			v := a
			out.vacation = &v
		}
	}
	out.keep = out.keep || implicitKeep
	return out, true
}

// deliverLocalFiltered delivers a message for recipient rcpt (resolved to
// the local mailbox user@domain) under the control of the mailbox owner's
// active Sieve script. Before F5115 the actions the script produced never
// reached delivery: fileinto, redirect, discard and reject were ignored and
// every message landed in INBOX (or Junk). folder is the default folder
// ("" for INBOX, "Junk" for a spam verdict) used by keep.
func (s *Server) deliverLocalFiltered(user, domain, rcpt, from, notify string, data []byte, folder string) error {
	mailbox := user + "@" + domain
	scriptUser := s.activeSieveUser(mailbox)
	if scriptUser == "" {
		return s.deliverLocal(user, domain, from, data, folder)
	}
	out, ok := s.runUserSieve(scriptUser, rcpt, from, data)
	if !ok {
		return s.deliverLocal(user, domain, from, data, folder)
	}

	keep := out.keep
	if out.rejected && !keep && len(out.folders) == 0 && len(out.redirects) == 0 {
		// RFC 5429: after DATA the refusal goes back to the sender as a DSN.
		reason := strings.Join(strings.Fields(out.rejectMsg), " ")
		if len(reason) > 200 {
			reason = reason[:200]
		}
		if reason == "" {
			reason = "Message rejected by recipient filter"
		}
		return s.bounceFailedRecipients(from, data, []rcptFailure{{
			rcpt: rcpt, notify: notify, diag: "smtp; 550 5.7.1 " + reason,
		}})
	}

	if s.redirectBySieve(mailbox, from, data, out.redirects) {
		// A redirect that could not be queued keeps a local copy so the
		// message is not lost (as for account forwarding, F4879).
		keep = true
	}

	if out.vacation != nil && from != "" && folder != "Junk" &&
		s.sieveManager.CheckAndRecordVacation(fmt.Sprintf("%q:%q", rcpt, from), out.vacation.Days) {
		s.handleSieveVacation(from, rcpt, *out.vacation)
	}

	targets := make([]string, 0, len(out.folders)+1)
	seen := map[string]bool{}
	add := func(f string) {
		if strings.EqualFold(f, "INBOX") {
			f = ""
		}
		if !seen[f] {
			seen[f] = true
			targets = append(targets, f)
		}
	}
	if keep {
		add(folder)
	}
	for _, f := range out.folders {
		if f == "" {
			f = folder
		}
		add(f)
	}
	for _, f := range targets {
		if err := s.deliverLocal(user, domain, from, data, f); err != nil {
			return err
		}
	}
	return nil
}

// redirectBySieve queues the script's redirects from mailbox and reports
// whether any of them failed (loop, bad address or queue error).
func (s *Server) redirectBySieve(mailbox, from string, data []byte, redirects []string) (failed bool) {
	if len(redirects) == 0 {
		return false
	}
	for _, loopAddr := range getMailLoopHeaders(data) {
		if strings.EqualFold(loopAddr, mailbox) {
			s.logger.Warn("Sieve redirect loop detected, keeping message", "mailbox", mailbox)
			return true
		}
	}
	if len(redirects) > maxSieveRedirects {
		s.logger.Warn("Too many Sieve redirects, extra ones dropped", "mailbox", mailbox, "count", len(redirects))
		redirects = redirects[:maxSieveRedirects]
		failed = true
	}
	dataWithLoop := addMailLoopHeader(data, mailbox)
	for _, raw := range redirects {
		addr, err := mail.ParseAddress(raw)
		if err != nil || strings.ContainsAny(addr.Address, "\r\n<>") || !strings.Contains(addr.Address, "@") {
			s.logger.Warn("Invalid Sieve redirect address", "mailbox", mailbox, "address", raw)
			failed = true
			continue
		}
		if s.queue == nil {
			failed = true
			continue
		}
		if _, err := s.queue.Enqueue(mailbox, []string{addr.Address}, dataWithLoop); err != nil {
			s.logger.Error("Failed to queue Sieve redirect", "mailbox", mailbox, "to", addr.Address, "error", err)
			failed = true
			continue
		}
		s.logger.Debug("Message redirected by Sieve", "mailbox", mailbox, "from", from, "to", addr.Address)
	}
	return failed
}
