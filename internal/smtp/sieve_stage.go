package smtp

import (
	"github.com/umailserver/umailserver/internal/sieve"
)

// VacationHandler is called when a Sieve vacation action is encountered
// Args: sender (original message from), recipient (vacation reply from), vacation action details
type VacationHandler func(sender, recipient string, vacation sieve.VacationAction)

// SieveStage is the pipeline slot for Sieve filtering. It accepts every
// message: each recipient's active script is run, and its actions (keep,
// fileinto, redirect, discard, reject, vacation) carried out, per recipient
// by the delivery handler, which resolves the mailbox owner by full address.
//
// Before F5125–F5127 this stage also ran scripts, looked up by the
// recipient's local part only: the script of a bare login "alice" ran for
// alice@ any domain (F5125), discard was answered with a 550 (F5126), one
// recipient's reject refused the whole multi-recipient transaction, and
// vacation replies were sent here as well as by the delivery handler — also
// for messages later filed as Junk or refused by a later stage (F5127).
type SieveStage struct {
	manager         *sieve.Manager
	vacationHandler VacationHandler
}

// NewSieveStage creates a new Sieve filtering stage
func NewSieveStage(manager *sieve.Manager) *SieveStage {
	return &SieveStage{
		manager: manager,
	}
}

// SetVacationHandler sets the callback for Sieve vacation actions.
//
// Deprecated: vacation replies are sent by the delivery handler, once per
// delivered recipient (F5127); the stage does not call this handler.
func (s *SieveStage) SetVacationHandler(h VacationHandler) {
	s.vacationHandler = h
}

func (s *SieveStage) Name() string { return "Sieve" }

// Process accepts the message; see SieveStage for where scripts run.
func (s *SieveStage) Process(_ *MessageContext) PipelineResult {
	return ResultAccept
}
