package server

import (
	"github.com/umailserver/umailserver/internal/av"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/smtp"
)

// sendMDN sends a Message Disposition Notification (read receipt)
func (s *Server) sendMDN(from, to, messageID, inReplyTo string, msgData []byte) error {
	// Generate MDN
	mdn, err := queue.GenerateMDN(msgData, from, to, messageID, inReplyTo, queue.MDNDispositionDisplayed, "umailserver")
	if err != nil {
		s.logger.Error("failed to generate MDN", "error", err)
		return err
	}

	// Enqueue MDN to be sent
	if s.queue != nil {
		if _, err := s.queue.Enqueue(from, []string{to}, mdn); err != nil {
			s.logger.Error("failed to enqueue MDN", "error", err)
			return err
		}
		s.logger.Info("MDN queued", "from", from, "to", to, "messageID", messageID)
	}

	return nil
}

// avScannerAdapter wraps an *av.Scanner to satisfy the smtp.AVScanner interface.
// The two packages define structurally identical but distinct result types,
// so a thin adapter is required.
type avScannerAdapter struct {
	inner *av.Scanner
}

func (a *avScannerAdapter) IsEnabled() bool { return a.inner.IsEnabled() }

func (a *avScannerAdapter) Scan(data []byte) (*smtp.AVScanResult, error) {
	res, err := a.inner.Scan(data)
	if err != nil {
		return nil, err
	}
	return &smtp.AVScanResult{
		Infected: res.Infected,
		Virus:    res.Virus,
	}, nil
}
