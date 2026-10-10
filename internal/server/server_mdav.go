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

// avLogger is the logging subset avFailClosedStage needs.
type avLogger interface {
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// avFailClosedStage runs the antivirus scan itself so that a scanner error
// temp-fails the message (451) instead of being accepted unscanned, which
// is what smtp.AVStage does on error, and so a skipped scan is logged. The
// infected-verdict handling is delegated to smtp.AVStage.
type avFailClosedStage struct {
	scanner *av.Scanner
	action  string
	logger  avLogger
}

func (a *avFailClosedStage) Name() string { return "AV" }

func (a *avFailClosedStage) Process(ctx *smtp.MessageContext) smtp.PipelineResult {
	if a.scanner == nil || !a.scanner.IsEnabled() {
		return smtp.ResultAccept
	}
	res, err := a.scanner.Scan(ctx.Data)
	if err != nil {
		if a.logger != nil {
			a.logger.Error("Antivirus scan failed, temp-failing message", "from", ctx.From, "error", err)
		}
		ctx.Rejected = true
		ctx.RejectionCode = 451
		ctx.RejectionMessage = "Antivirus scan unavailable, try again later"
		return smtp.ResultReject
	}
	if res.Skipped {
		if a.logger != nil {
			a.logger.Warn("Antivirus scan skipped", "from", ctx.From)
		}
		return smtp.ResultAccept
	}
	return smtp.NewAVStage(staticAVScanner{res: &smtp.AVScanResult{Infected: res.Infected, Virus: res.Virus}}, a.action).Process(ctx)
}

// staticAVScanner replays an already-computed scan result.
type staticAVScanner struct{ res *smtp.AVScanResult }

func (staticAVScanner) IsEnabled() bool                           { return true }
func (s staticAVScanner) Scan([]byte) (*smtp.AVScanResult, error) { return s.res, nil }
