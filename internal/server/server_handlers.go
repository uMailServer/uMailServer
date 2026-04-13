package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/webhook"
	"golang.org/x/crypto/bcrypt"
)

// authenticate validates user credentials
func (s *Server) authenticate(username, password string) (bool, error) {
	// Try LDAP authentication first if enabled
	if s.ldapClient != nil {
		ldapUser, err := s.ldapClient.Authenticate(username, password)
		if err == nil {
			s.logger.Debug("LDAP authentication successful",
				"username", username,
				"email", ldapUser.Email,
				"is_admin", ldapUser.IsAdmin,
			)
			return true, nil
		}
		// If LDAP returns "user not found", fall back to local DB
		// Other errors (connection failure, etc.) also fall back to local DB
		s.logger.Debug("LDAP auth failed, falling back to local DB", "username", username, "error", err)
	}

	// Fall back to local database authentication
	user, domain := parseEmail(username)

	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return false, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)); err != nil {
		return false, nil
	}

	if !account.IsActive {
		return false, fmt.Errorf("account is not active")
	}

	return true, nil
}

// getUserSecret returns the password hash for a user, used by CRAM-MD5 authentication
func (s *Server) getUserSecret(username string) (string, error) {
	user, domain := parseEmail(username)
	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return "", err
	}
	if account == nil || !account.IsActive {
		return "", fmt.Errorf("user not found or inactive")
	}
	return account.PasswordHash, nil
}


// loginResult handles login success/failure events and triggers webhooks
func (s *Server) loginResult(username string, success bool, ip string) {
	if s.webhookMgr != nil {
		eventType := "auth.login.success"
		if !success {
			eventType = "auth.login.failed"
		}
		s.webhookMgr.Trigger(eventType, map[string]interface{}{
			"username": username,
			"ip":       ip,
		})
	}
}

// deliverMessage delivers an incoming message
func (s *Server) deliverMessage(from string, to []string, data []byte) error {
	return s.deliverMessageWithSieve(from, to, data, nil)
}

// deliverMessageWithSieve delivers an incoming message with optional Sieve filtering actions
func (s *Server) deliverMessageWithSieve(from string, to []string, data []byte, sieveActions []string) error {
	// Parse sieve actions for fileinto and redirect
	var targetFolder string
	var redirectAddrs []string

	for _, action := range sieveActions {
		if strings.HasPrefix(action, "fileinto:") {
			targetFolder = strings.TrimPrefix(action, "fileinto:")
		} else if strings.HasPrefix(action, "redirect:") {
			redirectAddr := strings.TrimPrefix(action, "redirect:")
			if redirectAddr != "" {
				redirectAddrs = append(redirectAddrs, redirectAddr)
			}
		}
	}

	// Handle redirects - queue copies to redirect addresses
	for _, redirectAddr := range redirectAddrs {
		if err := s.relayMessage(from, redirectAddr, data); err != nil {
			s.logger.Error("Failed to queue redirect message", "to", redirectAddr, "error", err)
			// Continue with other deliveries even if redirect fails
		} else {
			s.logger.Debug("Message queued for redirect", "from", from, "to", redirectAddr)
		}
	}

	var errs []error
	for _, recipient := range to {
		user, domain := parseEmail(recipient)

		domainData, err := s.database.GetDomain(domain)
		if err != nil || domainData == nil || !domainData.IsActive {
			if relayErr := s.relayMessage(from, recipient, data); relayErr != nil {
				s.logger.Error("Failed to relay message", "to", recipient, "error", relayErr)
				errs = append(errs, fmt.Errorf("relay %s: %w", recipient, relayErr))
			}
			continue
		}

		// Resolve alias
		target, aliasErr := s.database.ResolveAlias(domain, user)
		if aliasErr != nil {
			s.logger.Debug("Alias resolution failed, trying direct delivery", "domain", domain, "user", user, "error", aliasErr)
		}
		if target != "" {
			tUser, tDomain := parseEmail(target)
			if tUser != "" && tDomain != "" {
				user = tUser
				domain = tDomain
			}
		}

		// Deliver with optional target folder from sieve
		if err := s.deliverLocal(user, domain, from, data, targetFolder); err != nil {
			s.logger.Error("Failed to deliver locally", "user", user, "domain", domain, "error", err)
			errs = append(errs, fmt.Errorf("deliver %s: %w", recipient, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("delivery had %d failure(s): %w", len(errs), errors.Join(errs...))
	}
	return nil
}

// relayMessage relays a message to a remote server
func (s *Server) relayMessage(from, to string, data []byte) error {
	if s.queue != nil {
		_, err := s.queue.Enqueue(from, []string{to}, data)
		if err != nil {
			s.logger.Error("Failed to enqueue relay message", "error", err)
			return fmt.Errorf("failed to queue message: %w", err)
		}
		s.logger.Debug("Message queued for relay", "from", from, "to", to)
		return nil
	}
	s.logger.Debug("Relaying message (queue not available)", "from", from, "to", to)
	return nil
}

// deliverLocal delivers a message to a local mailbox
func (s *Server) deliverLocal(user, domain, from string, data []byte, targetFolders ...string) error {
	email := user + "@" + domain

	// Determine target folder - default to INBOX if not specified
	folder := "INBOX"
	if len(targetFolders) > 0 && targetFolders[0] != "" {
		folder = targetFolders[0]
	}

	// Check if user exists
	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return fmt.Errorf("user does not exist: %s", email)
	}

	if account == nil || !account.IsActive {
		// Check catch-all target for the domain
		if domainData, derr := s.database.GetDomain(domain); derr == nil && domainData != nil && domainData.CatchAllTarget != "" {
			tUser, tDomain := parseEmail(domainData.CatchAllTarget)
			if tUser != "" && tDomain != "" {
				return s.deliverLocal(tUser, tDomain, from, data, targetFolders...)
			}
		}
		return fmt.Errorf("user does not exist or is not active: %s", email)
	}

	// Check quota
	if account.QuotaLimit > 0 && account.QuotaUsed >= account.QuotaLimit {
		return fmt.Errorf("quota exceeded for user: %s", email)
	}

	// Handle mail forwarding (before storing, so we skip local store if not keeping copy)
	if account.ForwardTo != "" {
		forwardTargets := strings.Split(account.ForwardTo, ",")
		for _, fwd := range forwardTargets {
			fwd = strings.TrimSpace(fwd)
			if fwd == "" {
				continue
			}
			if s.queue != nil {
				if _, err := s.queue.Enqueue(email, []string{fwd}, data); err != nil {
					s.logger.Error("Failed to enqueue forwarded message", "from", email, "to", fwd, "error", err)
				}
			}
		}
		if !account.ForwardKeepCopy {
			s.logger.Debug("Message forwarded (no local copy)",
				"to", email,
				"from", from,
			)
			return nil
		}
	}

	// Store message locally
	messageID, err := s.msgStore.StoreMessage(email, data)
	if err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	// Update quota atomically
	if err := s.database.IncrementQuota(domain, user, int64(len(data))); err != nil {
		s.logger.Error("Failed to update quota", "email", email, "error", err)
	}

	s.logger.Debug("Message delivered",
		"to", email,
		"from", from,
		"message_id", messageID,
	)

	// Store metadata and index message for search
	if s.storageDB != nil {
		uid, uidErr := s.storageDB.GetNextUID(email, folder)
		if uidErr == nil {
			subject, fromAddr, toAddr, dateStr := parseBasicHeaders(data)
			meta := &storage.MessageMetadata{
				MessageID:    messageID,
				UID:          uid,
				Flags:        []string{"\\Recent"},
				InternalDate: time.Now(),
				Size:         int64(len(data)),
				Subject:      subject,
				Date:         dateStr,
				From:         fromAddr,
				To:           toAddr,
			}
			if err := s.storageDB.StoreMessageMetadata(email, folder, uid, meta); err != nil {
				s.logger.Error("Failed to store message metadata", "email", email, "uid", uid, "folder", folder, "error", err)
			}

			if s.searchSvc != nil {
				select {
				case s.indexWork <- indexJob{email: email, uid: uid}:
				default:
					s.logger.Warn("Search index queue full, dropping index job", "email", email, "uid", uid)
				}
			}
		}
	}

	// Trigger webhook for mail received
	if s.webhookMgr != nil {
		s.webhookMgr.Trigger(webhook.EventMailReceived, map[string]interface{}{
			"message_id": messageID,
			"to":         email,
			"from":       from,
			"size":       len(data),
		})
	}

	// Send push notification for new mail
	if s.pushSvc != nil {
		select {
		case s.bgSem <- struct{}{}:
			go func() {
				defer func() {
					<-s.bgSem
					if r := recover(); r != nil {
						s.logger.Error("Panic in push notification", "error", r)
					}
				}()
				// Extract subject from message for notification
				subject, _, _, _ := parseBasicHeaders(data)
				if subject == "" {
					subject = "(No subject)"
				}
				// Send push notification (non-blocking)
				if err := s.pushSvc.SendNewMailNotification(email, from, subject, ""); err != nil {
					s.logger.Debug("Failed to send push notification", "to", email, "error", err)
				}
			}()
		default:
			s.logger.Warn("Background task semaphore full, dropping push notification", "email", email)
		}
	}

	// Track delivery metric
	metrics.Get().DeliverySuccess()

	// Send vacation auto-reply if configured
	if account.VacationSettings != "" && s.queue != nil {
		select {
		case s.bgSem <- struct{}{}:
			go func() {
				defer func() {
					<-s.bgSem
					if r := recover(); r != nil {
						s.logger.Error("Panic in vacation reply", "error", r)
					}
				}()
				s.sendVacationReply(email, from, account.VacationSettings)
			}()
		default:
			s.logger.Warn("Background task semaphore full, dropping vacation reply", "email", email)
		}
	}
	return nil
}

// parseEmail splits an email address into user and domain
func parseEmail(email string) (user, domain string) {
	at := -1
	for i := len(email) - 1; i >= 0; i-- {
		if email[i] == '@' {
			at = i
			break
		}
	}
	if at == -1 {
		return email, ""
	}
	return email[:at], email[at+1:]
}

// parseBasicHeaders extracts subject, from, to, date from raw message data.
func parseBasicHeaders(data []byte) (subject, from, to, date string) {
	msg, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return "", "", "", ""
	}
	subject = msg.Header.Get("Subject")
	from = msg.Header.Get("From")
	to = msg.Header.Get("To")
	date = msg.Header.Get("Date")
	return
}

// generateSecureToken generates a cryptographically random 32-byte hex token.
func generateSecureToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
