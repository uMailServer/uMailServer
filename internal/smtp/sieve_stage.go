package smtp

import (
	"fmt"
	"strings"

	"github.com/umailserver/umailserver/internal/sieve"
)

// SieveStage implements Sieve mail filtering in the SMTP pipeline
type SieveStage struct {
	manager *sieve.Manager
}

// NewSieveStage creates a new Sieve filtering stage
func NewSieveStage(manager *sieve.Manager) *SieveStage {
	return &SieveStage{
		manager: manager,
	}
}

func (s *SieveStage) Name() string { return "Sieve" }

func (s *SieveStage) Process(ctx *MessageContext) PipelineResult {
	// Get the envelope from
	from := ctx.From
	if from == "" {
		from = "<>"
	}

	// Get the recipients
	to := ctx.To
	if len(to) == 0 {
		return ResultAccept
	}

	// Build Sieve message context
	msg := &sieve.MessageContext{
		From:    from,
		To:      to,
		Headers: ctx.Headers,
		Body:    ctx.Data,
		Size:    int64(len(ctx.Data)),
	}

	// For each recipient, check if they have a sieve script
	for _, recipient := range to {
		// Extract user from recipient (simple parsing)
		user := extractUserFromRecipient(recipient)
		if user == "" {
			continue
		}

		// Check if user has an active script
		if !s.manager.HasActiveScript(user) {
			continue
		}

		// Execute sieve script
		actions, err := s.manager.ProcessMessage(user, msg)
		if err != nil {
			// On error, continue with default action (keep)
			continue
		}

		// Process actions
		for _, action := range actions {
			switch a := action.(type) {
			case sieve.DiscardAction:
				// Silently discard
				return ResultReject
			case sieve.RejectAction:
				// Reject with message
				ctx.Rejected = true
				ctx.RejectionCode = 550
				ctx.RejectionMessage = a.Message
				return ResultReject
			case sieve.FileintoAction:
				// Mark for filing - this will be handled by deliverLocal
				if ctx.SpamResult.Reasons == nil {
					ctx.SpamResult.Reasons = make([]string, 0)
				}
				ctx.SpamResult.Reasons = append(ctx.SpamResult.Reasons, fmt.Sprintf("fileinto:%s", a.Folder))
			case sieve.RedirectAction:
				// Mark for redirect - handled by deliverLocal
				if ctx.SpamResult.Reasons == nil {
					ctx.SpamResult.Reasons = make([]string, 0)
				}
				ctx.SpamResult.Reasons = append(ctx.SpamResult.Reasons, fmt.Sprintf("redirect:%s", a.Address))
			case sieve.VacationAction:
				// Queue vacation reply - handled asynchronously
				// Note: vacation is typically processed after delivery
			case sieve.StopAction:
				// Stop processing
				return ResultAccept
			case sieve.KeepAction:
				// Default - keep in inbox
			}
		}
	}

	return ResultAccept
}

// extractUserFromRecipient extracts the local part from an email address
func extractUserFromRecipient(recipient string) string {
	// Remove any routing prefix
	if idx := strings.Index(recipient, "@"); idx > 0 {
		return recipient[:idx]
	}
	// Handle postmaster or other special addresses
	if recipient == "" {
		return ""
	}
	// Check for localhost style
	if idx := strings.Index(recipient, "!"); idx > 0 {
		return recipient[:idx]
	}
	return recipient
}
