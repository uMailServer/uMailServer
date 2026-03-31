package smtp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/umailserver/umailserver/internal/auth"
)

// AuthSPFStage uses the real auth.SPFChecker for SPF verification
type AuthSPFStage struct {
	checker *auth.SPFChecker
	logger  *slog.Logger
}

// NewAuthSPFStage creates a new SPF stage using the real SPF checker
func NewAuthSPFStage(checker *auth.SPFChecker, logger *slog.Logger) *AuthSPFStage {
	return &AuthSPFStage{checker: checker, logger: logger}
}

func (s *AuthSPFStage) Name() string { return "SPF" }

func (s *AuthSPFStage) Process(ctx *MessageContext) PipelineResult {
	domain := extractDomain(ctx.From)
	if domain == "" {
		ctx.SPFResult = SPFResult{Result: "none"}
		return ResultAccept
	}

	spfResult, explanation := s.checker.CheckSPF(context.Background(), ctx.RemoteIP, domain, ctx.From)
	ctx.SPFResult = SPFResult{
		Result:      spfResult.String(),
		Domain:      domain,
		Explanation: explanation,
	}

	switch spfResult {
	case auth.SPFFail:
		ctx.SpamScore += 2.5
	case auth.SPFSoftFail:
		ctx.SpamScore += 1.5
	case auth.SPFPermError:
		ctx.SpamScore += 0.5
	}

	if s.logger != nil {
		s.logger.Debug("SPF check completed",
			"domain", domain,
			"result", spfResult.String(),
			"score", ctx.SpamScore,
		)
	}

	return ResultAccept
}

// AuthDKIMStage uses the real auth.DKIMVerifier for DKIM verification
type AuthDKIMStage struct {
	verifier *auth.DKIMVerifier
	logger   *slog.Logger
}

// NewAuthDKIMStage creates a new DKIM stage using the real verifier
func NewAuthDKIMStage(verifier *auth.DKIMVerifier, logger *slog.Logger) *AuthDKIMStage {
	return &AuthDKIMStage{verifier: verifier, logger: logger}
}

func (s *AuthDKIMStage) Name() string { return "DKIM" }

func (s *AuthDKIMStage) Process(ctx *MessageContext) PipelineResult {
	// Look for DKIM-Signature header
	dkimHeaders, ok := ctx.Headers["DKIM-Signature"]
	if !ok {
		dkimHeaders = ctx.Headers["Dkim-Signature"]
	}

	if len(dkimHeaders) == 0 {
		ctx.DKIMResult = DKIMResult{Valid: false, Error: "no DKIM signature"}
		return ResultAccept
	}

	// Verify each DKIM signature
	for _, dkimHeader := range dkimHeaders {
		result, sig, err := s.verifier.Verify(ctx.Headers, ctx.Data, dkimHeader)
		if err != nil && s.logger != nil {
			s.logger.Debug("DKIM verification error", "error", err)
		}

		if result == auth.DKIMPass {
			ctx.DKIMResult = DKIMResult{
				Valid:    true,
				Domain:   sig.Domain,
				Selector: sig.Selector,
			}
			ctx.SpamScore -= 1.0 // Reward for valid DKIM
			if s.logger != nil {
				s.logger.Debug("DKIM verified",
					"domain", sig.Domain,
					"selector", sig.Selector,
				)
			}
			return ResultAccept
		}

		ctx.DKIMResult = DKIMResult{
			Valid:  false,
			Domain: sig.Domain,
			Error:  fmt.Sprintf("verification failed: %v", err),
		}
		ctx.SpamScore += 1.0
	}

	return ResultAccept
}

// AuthDMARCStage uses the real auth.DMARCEvaluator for DMARC evaluation
type AuthDMARCStage struct {
	evaluator *auth.DMARCEvaluator
	logger    *slog.Logger
}

// NewAuthDMARCStage creates a new DMARC stage using the real evaluator
func NewAuthDMARCStage(evaluator *auth.DMARCEvaluator, logger *slog.Logger) *AuthDMARCStage {
	return &AuthDMARCStage{evaluator: evaluator, logger: logger}
}

func (s *AuthDMARCStage) Name() string { return "DMARC" }

func (s *AuthDMARCStage) Process(ctx *MessageContext) PipelineResult {
	fromDomain := extractDomain(ctx.From)
	if fromDomain == "" {
		ctx.DMARCResult = DMARCResult{Result: "none"}
		return ResultAccept
	}

	// Map pipeline results to auth results
	spfResult := mapSPFResult(ctx.SPFResult.Result)
	dkimResult := mapDKIMResult(ctx.DKIMResult)

	evaluation, err := s.evaluator.Evaluate(
		context.Background(),
		fromDomain,
		spfResult, ctx.SPFResult.Domain,
		dkimResult, ctx.DKIMResult.Domain,
	)
	if err != nil {
		if s.logger != nil {
			s.logger.Debug("DMARC evaluation error", "error", err)
		}
		ctx.DMARCResult = DMARCResult{Result: "temperror"}
		return ResultAccept
	}

	ctx.DMARCResult = DMARCResult{
		Result:     evaluation.Result.String(),
		Policy:     string(evaluation.AppliedPolicy),
		Percentage: 100,
	}

	// Apply DMARC policy
	switch evaluation.AppliedPolicy {
	case auth.DMARCPolicyReject:
		ctx.SpamScore += 3.0
		ctx.Rejected = true
		ctx.RejectionCode = 550
		ctx.RejectionMessage = fmt.Sprintf("DMARC policy rejection: %s", evaluation.Explanation)
		if s.logger != nil {
			s.logger.Info("DMARC reject policy applied",
				"domain", fromDomain,
				"explanation", evaluation.Explanation,
			)
		}
		return ResultReject
	case auth.DMARCPolicyQuarantine:
		ctx.SpamScore += 2.0
		ctx.Quarantine = true
		if s.logger != nil {
			s.logger.Info("DMARC quarantine policy applied",
				"domain", fromDomain,
				"explanation", evaluation.Explanation,
			)
		}
	}

	return ResultAccept
}

// mapSPFResult maps string SPF result to auth.SPFResult
func mapSPFResult(result string) auth.SPFResult {
	switch strings.ToLower(result) {
	case "pass":
		return auth.SPFPass
	case "fail":
		return auth.SPFFail
	case "softfail":
		return auth.SPFSoftFail
	case "neutral":
		return auth.SPFNeutral
	case "temperror":
		return auth.SPFTempError
	case "permerror":
		return auth.SPFPermError
	default:
		return auth.SPFNone
	}
}

// mapDKIMResult maps pipeline DKIMResult to auth.DKIMResult
func mapDKIMResult(dkim DKIMResult) auth.DKIMResult {
	if dkim.Valid {
		return auth.DKIMPass
	}
	if dkim.Error != "" {
		return auth.DKIMFail
	}
	return auth.DKIMNone
}

// NetDNSResolver wraps net.Resolver to implement auth.DNSResolver
type NetDNSResolver struct {
	resolver *net.Resolver
}

// NewNetDNSResolver creates a new DNS resolver using the net package
func NewNetDNSResolver() *NetDNSResolver {
	return &NetDNSResolver{
		resolver: &net.Resolver{},
	}
}

func (r *NetDNSResolver) LookupTXT(ctx context.Context, domain string) ([]string, error) {
	return r.resolver.LookupTXT(ctx, domain)
}

func (r *NetDNSResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := r.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, len(addrs))
	for i, addr := range addrs {
		ips[i] = addr.IP
	}
	return ips, nil
}

func (r *NetDNSResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	return r.resolver.LookupMX(ctx, domain)
}

// PipelineLogger adapts slog.Logger to the smtp.Logger interface
type PipelineLogger struct {
	logger *slog.Logger
}

// NewPipelineLogger creates a new pipeline logger adapter
func NewPipelineLogger(logger *slog.Logger) *PipelineLogger {
	return &PipelineLogger{logger: logger}
}

func (l *PipelineLogger) Debug(msg string, args ...interface{}) {
	l.logger.Debug(msg, argsToAttrs(args)...)
}

func (l *PipelineLogger) Info(msg string, args ...interface{}) {
	l.logger.Info(msg, argsToAttrs(args)...)
}

func (l *PipelineLogger) Warn(msg string, args ...interface{}) {
	l.logger.Warn(msg, argsToAttrs(args)...)
}

func (l *PipelineLogger) Error(msg string, args ...interface{}) {
	l.logger.Error(msg, argsToAttrs(args)...)
}

func argsToAttrs(args []interface{}) []interface{} {
	// slog already accepts key-value pairs, just pass through
	return args
}
