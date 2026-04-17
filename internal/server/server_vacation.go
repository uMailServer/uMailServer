package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/sieve"
)

// handleSieveVacation handles Sieve vacation action by sending a vacation auto-reply
func (s *Server) handleSieveVacation(sender, recipient string, vacation sieve.VacationAction) {
	if s.queue == nil {
		return
	}

	// Don't send vacation to mailing lists or bounces
	senderLower := strings.ToLower(sender)
	for _, prefix := range []string{"mailer-daemon@", "postmaster@", "noreply@", "no-reply@", "bounce@"} {
		if strings.HasPrefix(senderLower, prefix) {
			return
		}
	}

	// Build vacation message content
	subject := vacation.Subject
	if subject == "" {
		subject = "Automated reply"
	}
	body := vacation.Body
	if body == "" {
		body = "I'm currently on vacation and will reply when I return."
	}

	// Create vacation message - From is the recipient (who's on vacation)
	fromAddr := recipient
	if vacation.From != "" {
		fromAddr = vacation.From
	}
	vacationMsg := fmt.Sprintf("From: %s\r\nSubject: %s\r\n\r\n%s",
		fromAddr,
		subject,
		body)

	// Enqueue vacation reply TO the sender FROM the recipient
	if _, err := s.queue.Enqueue(fromAddr, []string{sender}, []byte(vacationMsg)); err != nil {
		s.logger.Error("Failed to enqueue vacation reply", "to", sender, "from", fromAddr, "error", err)
	} else {
		s.logger.Debug("Vacation reply enqueued", "to", sender, "from", fromAddr)
	}
}

// sendVacationReply generates and enqueues an auto-reply message.
func (s *Server) sendVacationReply(recipientEmail, senderEmail, settingsJSON string) {
	senderLower := strings.ToLower(senderEmail)
	for _, prefix := range []string{"mailer-daemon@", "postmaster@", "noreply@", "no-reply@", "bounce@"} {
		if strings.HasPrefix(senderLower, prefix) {
			return
		}
	}

	// Parse settings first to get SendInterval for deduplication
	var settings struct {
		Enabled      bool          `json:"enabled"`
		Message      string        `json:"message"`
		StartDate    string        `json:"start_date"`
		EndDate      string        `json:"end_date"`
		SendInterval time.Duration `json:"send_interval"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil || !settings.Enabled {
		return
	}

	// Use SendInterval from settings, default to 24h if not set or too small
	sendInterval := settings.SendInterval
	if sendInterval < 24*time.Hour {
		sendInterval = 24 * time.Hour
	}

	key := recipientEmail + "|" + senderEmail
	s.vacationRepliesMu.Lock()
	if s.vacationReplies == nil {
		s.vacationReplies = make(map[string]time.Time)
	}
	if lastSent, ok := s.vacationReplies[key]; ok && time.Since(lastSent) < sendInterval {
		s.vacationRepliesMu.Unlock()
		return
	}
	s.vacationReplies[key] = time.Now()

	// Cleanup old entries every 100 entries to prevent unbounded growth
	if len(s.vacationReplies) > 100 {
		s.cleanupVacationRepliesLocked()
	}

	s.vacationRepliesMu.Unlock()

	now := time.Now()
	if settings.StartDate != "" {
		if start, err := time.Parse("2006-01-02", settings.StartDate); err == nil && now.Before(start) {
			return
		}
	}
	if settings.EndDate != "" {
		if end, err := time.Parse("2006-01-02", settings.EndDate); err == nil && !now.Before(end.Add(24*time.Hour)) {
			return
		}
	}

	// Guard against nil queue
	if s.queue == nil {
		return
	}

	autoReply := "From: " + recipientEmail + "\r\n" +
		"To: " + senderEmail + "\r\n" +
		"Subject: Auto: Out of Office\r\n" +
		"Auto-Submitted: auto-replied\r\n" +
		"Precedence: bulk\r\n" +
		"Date: " + now.Format(time.RFC1123Z) + "\r\n" +
		"\r\n" +
		settings.Message

	if _, err := s.queue.Enqueue(recipientEmail, []string{senderEmail}, []byte(autoReply)); err != nil {
		s.logger.Error("Failed to enqueue vacation reply", "error", err)
	}
}

// cleanupVacationReplies removes entries older than 48 hours from vacationReplies map
func (s *Server) cleanupVacationReplies() {
	s.vacationRepliesMu.Lock()
	defer s.vacationRepliesMu.Unlock()
	s.cleanupVacationRepliesLocked()
}

// cleanupVacationRepliesLocked removes entries older than 48 hours.
// Caller must hold s.vacationRepliesMu.
func (s *Server) cleanupVacationRepliesLocked() {
	cutoff := time.Now().Add(-48 * time.Hour)
	for key, lastSent := range s.vacationReplies {
		if lastSent.Before(cutoff) {
			delete(s.vacationReplies, key)
		}
	}
}

// startVacationCleanup runs a hourly goroutine that removes vacation reply entries
// older than 48 hours, preventing unbounded map growth independent of the
// threshold-based cleanup in trackVacationReply.
func (s *Server) startVacationCleanup() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.cleanupVacationReplies()
			}
		}
	}()
}
