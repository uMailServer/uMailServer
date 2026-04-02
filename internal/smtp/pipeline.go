package smtp

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
)

// MessageContext holds the context for a message being processed
type MessageContext struct {
	// Connection info
	RemoteIP      net.IP
	RemoteHost    string
	TLS           bool
	Authenticated bool
	Username      string

	// Envelope
	From      string
	To        []string
	MessageID string

	// Message data
	Data    []byte
	Headers map[string][]string

	// Processing results
	SpamScore   float64
	SpamResult  SpamResult
	DKIMResult  DKIMResult
	SPFResult   SPFResult
	DMARCResult DMARCResult

	// Flags
	Rejected         bool
	RejectionCode    int
	RejectionMessage string
	Quarantine       bool

	// Metadata
	ReceivedAt time.Time
	Stage      string
}

// SpamResult holds spam check results
type SpamResult struct {
	Score   float64
	Verdict string // inbox, junk, quarantine, reject
	Reasons []string
}

// DKIMResult holds DKIM verification results
type DKIMResult struct {
	Valid    bool
	Domain   string
	Selector string
	Error    string
}

// SPFResult holds SPF check results
type SPFResult struct {
	Result      string // pass, fail, softfail, neutral, none, temperror, permerror
	Domain      string
	Explanation string
}

// DMARCResult holds DMARC check results
type DMARCResult struct {
	Result     string // pass, fail, none
	Policy     string // none, quarantine, reject
	Percentage int
}

// NewMessageContext creates a new message context
func NewMessageContext(remoteIP net.IP, from string, to []string, data []byte) *MessageContext {
	return &MessageContext{
		RemoteIP:   remoteIP,
		From:       from,
		To:         to,
		Data:       data,
		Headers:    make(map[string][]string),
		ReceivedAt: time.Now(),
		SpamScore:  0,
	}
}

// PipelineResult represents the result of pipeline processing
type PipelineResult int

const (
	ResultAccept PipelineResult = iota
	ResultReject
	ResultQuarantine
)

// PipelineStage is an interface for pipeline stages
type PipelineStage interface {
	Name() string
	Process(ctx *MessageContext) PipelineResult
}

// Pipeline manages message processing stages
type Pipeline struct {
	stages []PipelineStage
	logger Logger
}

// Logger interface for pipeline logging
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// NewPipeline creates a new message pipeline
func NewPipeline(logger Logger) *Pipeline {
	if logger == nil {
		logger = &defaultLogger{}
	}
	return &Pipeline{
		stages: make([]PipelineStage, 0),
		logger: logger,
	}
}

// AddStage adds a stage to the pipeline
func (p *Pipeline) AddStage(stage PipelineStage) {
	p.stages = append(p.stages, stage)
}

// Process processes a message through all pipeline stages
func (p *Pipeline) Process(ctx *MessageContext) (PipelineResult, error) {
	p.logger.Info("Starting message pipeline",
		"from", ctx.From,
		"to", ctx.To,
		"remote_ip", ctx.RemoteIP,
	)

	for _, stage := range p.stages {
		ctx.Stage = stage.Name()
		p.logger.Debug("Running pipeline stage",
			"stage", stage.Name(),
			"from", ctx.From,
		)

		result := stage.Process(ctx)

		if result == ResultReject {
			p.logger.Info("Message rejected by pipeline",
				"stage", stage.Name(),
				"from", ctx.From,
				"reason", ctx.RejectionMessage,
			)
			return ResultReject, fmt.Errorf("rejected by %s: %s", stage.Name(), ctx.RejectionMessage)
		}

		if result == ResultQuarantine {
			ctx.Quarantine = true
			p.logger.Info("Message quarantined by pipeline",
				"stage", stage.Name(),
				"from", ctx.From,
			)
		}
	}

	p.logger.Info("Message pipeline completed",
		"from", ctx.From,
		"spam_score", ctx.SpamScore,
		"quarantine", ctx.Quarantine,
	)

	if ctx.Quarantine {
		return ResultQuarantine, nil
	}

	return ResultAccept, nil
}

// Default pipeline stages

// RateLimitStage checks rate limits
type RateLimitStage struct {
	limits map[string]*rateLimiter
}

type rateLimiter struct {
	count  int
	window time.Time
}

// NewRateLimitStage creates a new rate limit stage
func NewRateLimitStage() *RateLimitStage {
	return &RateLimitStage{
		limits: make(map[string]*rateLimiter),
	}
}

func (s *RateLimitStage) Name() string { return "RateLimit" }

func (s *RateLimitStage) Process(ctx *MessageContext) PipelineResult {
	// Simple rate limiting by IP
	key := ctx.RemoteIP.String()
	now := time.Now()

	limiter, exists := s.limits[key]
	if !exists || now.Sub(limiter.window) > time.Minute {
		s.limits[key] = &rateLimiter{
			count:  1,
			window: now,
		}
		return ResultAccept
	}

	limiter.count++
	if limiter.count > 30 { // 30 messages per minute per IP
		ctx.Rejected = true
		ctx.RejectionCode = 421
		ctx.RejectionMessage = "Rate limit exceeded"
		return ResultReject
	}

	return ResultAccept
}

// SPFStage checks SPF records
type SPFStage struct {
	resolver DNSResolver
}

// DNSResolver interface for DNS lookups
type DNSResolver interface {
	LookupTXT(domain string) ([]string, error)
}

// NewSPFStage creates a new SPF check stage
func NewSPFStage(resolver DNSResolver) *SPFStage {
	return &SPFStage{resolver: resolver}
}

func (s *SPFStage) Name() string { return "SPF" }

func (s *SPFStage) Process(ctx *MessageContext) PipelineResult {
	// Extract domain from sender
	domain := extractDomain(ctx.From)
	if domain == "" {
		ctx.SPFResult = SPFResult{Result: "none"}
		return ResultAccept
	}

	// Look up SPF record
	txtRecords, err := s.resolver.LookupTXT(domain)
	if err != nil {
		ctx.SPFResult = SPFResult{Result: "temperror"}
		return ResultAccept
	}

	// Find SPF record
	var spfRecord string
	for _, record := range txtRecords {
		if strings.HasPrefix(record, "v=spf1") {
			spfRecord = record
			break
		}
	}

	if spfRecord == "" {
		ctx.SPFResult = SPFResult{Result: "none"}
		return ResultAccept
	}

	// Evaluate SPF record
	result := s.evaluateSPF(spfRecord, ctx.RemoteIP, domain)
	ctx.SPFResult = result

	// Add to spam score if failed
	if result.Result == "fail" {
		ctx.SpamScore += 2.0
	} else if result.Result == "softfail" {
		ctx.SpamScore += 1.0
	}

	return ResultAccept
}

func (s *SPFStage) evaluateSPF(record string, ip net.IP, domain string) SPFResult {
	// Simplified SPF evaluation
	// In production, this would fully implement RFC 7208

	// Check for basic mechanisms - order matters: check -all and ~all before bare "all"
	if strings.Contains(record, "-all") {
		return SPFResult{
			Result: "fail",
			Domain: domain,
		}
	}

	if strings.Contains(record, "~all") {
		return SPFResult{
			Result: "softfail",
			Domain: domain,
		}
	}

	if strings.Contains(record, "+all") || strings.Contains(record, "all") {
		// Check if IP matches any allowed mechanisms
		// This is simplified - real implementation would check ip4, ip6, a, mx, etc.
		return SPFResult{
			Result: "pass",
			Domain: domain,
		}
	}

	return SPFResult{
		Result: "neutral",
		Domain: domain,
	}
}

// GreylistStage implements greylisting
type GreylistStage struct {
	greylist map[string]*greylistEntry
}

type greylistEntry struct {
	firstSeen time.Time
	allowed   bool
}

// NewGreylistStage creates a new greylisting stage
func NewGreylistStage() *GreylistStage {
	return &GreylistStage{
		greylist: make(map[string]*greylistEntry),
	}
}

func (s *GreylistStage) Name() string { return "Greylist" }

func (s *GreylistStage) Process(ctx *MessageContext) PipelineResult {
	// Create triplet key: sender IP + sender email + recipient email
	for _, recipient := range ctx.To {
		key := fmt.Sprintf("%s:%s:%s", ctx.RemoteIP.String(), ctx.From, recipient)

		entry, exists := s.greylist[key]
		if !exists {
			// First time seeing this triplet
			s.greylist[key] = &greylistEntry{
				firstSeen: time.Now(),
				allowed:   false,
			}
			ctx.Rejected = true
			ctx.RejectionCode = 451
			ctx.RejectionMessage = "Greylisted, please try again later"
			return ResultReject
		}

		if !entry.allowed {
			// Check if enough time has passed (5 minutes)
			if time.Since(entry.firstSeen) < 5*time.Minute {
				ctx.Rejected = true
				ctx.RejectionCode = 451
				ctx.RejectionMessage = "Greylisted, please try again later"
				return ResultReject
			}
			entry.allowed = true
		}
	}

	return ResultAccept
}

// RBLStage checks DNS blocklists
type RBLStage struct {
	servers []string
}

// NewRBLStage creates a new RBL check stage
func NewRBLStage(servers []string) *RBLStage {
	return &RBLStage{servers: servers}
}

func (s *RBLStage) Name() string { return "RBL" }

func (s *RBLStage) Process(ctx *MessageContext) PipelineResult {
	if len(s.servers) == 0 {
		return ResultAccept
	}

	// Reverse IP
	ip := ctx.RemoteIP.String()
	reversedIP := reverseIP(ip)

	// Check each RBL
	for _, server := range s.servers {
		lookup := fmt.Sprintf("%s.%s", reversedIP, server)
		// In production, perform actual DNS lookup
		// For now, just check format
		if lookup != "" {
			// If listed, add to spam score
			// ctx.SpamScore += 2.0
		}
	}

	return ResultAccept
}

func reverseIP(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return ""
	}
	return fmt.Sprintf("%s.%s.%s.%s", parts[3], parts[2], parts[1], parts[0])
}

func extractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// HeuristicStage implements heuristic spam checking
type HeuristicStage struct {
	rules []HeuristicRule
}

// HeuristicRule represents a spam check rule
type HeuristicRule struct {
	Name        string
	Description string
	Score       float64
	Check       func(ctx *MessageContext) bool
}

// NewHeuristicStage creates a new heuristic checking stage
func NewHeuristicStage() *HeuristicStage {
	return &HeuristicStage{
		rules: defaultHeuristicRules(),
	}
}

func (s *HeuristicStage) Name() string { return "Heuristic" }

func (s *HeuristicStage) Process(ctx *MessageContext) PipelineResult {
	for _, rule := range s.rules {
		if rule.Check(ctx) {
			ctx.SpamScore += rule.Score
			if ctx.SpamResult.Reasons == nil {
				ctx.SpamResult.Reasons = make([]string, 0)
			}
			ctx.SpamResult.Reasons = append(ctx.SpamResult.Reasons, rule.Name)
		}
	}

	return ResultAccept
}

func defaultHeuristicRules() []HeuristicRule {
	return []HeuristicRule{
		{
			Name:        "EMPTY_SUBJECT",
			Description: "Message has no subject",
			Score:       1.0,
			Check: func(ctx *MessageContext) bool {
				return ctx.Headers["Subject"] == nil || len(ctx.Headers["Subject"]) == 0
			},
		},
		{
			Name:        "ALL_CAPS_SUBJECT",
			Description: "Subject is all uppercase",
			Score:       2.0,
			Check: func(ctx *MessageContext) bool {
				subjects := ctx.Headers["Subject"]
				if len(subjects) == 0 {
					return false
				}
				subject := subjects[0]
				return subject != "" && strings.ToUpper(subject) == subject && len(subject) > 5
			},
		},
		{
			Name:        "MISSING_DATE",
			Description: "No Date header",
			Score:       1.0,
			Check: func(ctx *MessageContext) bool {
				return ctx.Headers["Date"] == nil || len(ctx.Headers["Date"]) == 0
			},
		},
		{
			Name:        "MISSING_MESSAGE_ID",
			Description: "No Message-ID header",
			Score:       1.0,
			Check: func(ctx *MessageContext) bool {
				return ctx.Headers["Message-Id"] == nil && ctx.Headers["Message-ID"] == nil
			},
		},
	}
}

// ScoreStage determines final spam verdict
type ScoreStage struct {
	rejectThreshold float64
	junkThreshold   float64
}

// NewScoreStage creates a new scoring stage
func NewScoreStage(rejectThreshold, junkThreshold float64) *ScoreStage {
	return &ScoreStage{
		rejectThreshold: rejectThreshold,
		junkThreshold:   junkThreshold,
	}
}

func (s *ScoreStage) Name() string { return "Score" }

func (s *ScoreStage) Process(ctx *MessageContext) PipelineResult {
	ctx.SpamResult.Score = ctx.SpamScore

	m := metrics.Get()

	if ctx.SpamScore >= s.rejectThreshold {
		ctx.SpamResult.Verdict = "reject"
		ctx.Rejected = true
		ctx.RejectionCode = 550
		ctx.RejectionMessage = "Message rejected as spam"
		if m != nil {
			m.SpamDetected()
		}
		return ResultReject
	}

	if ctx.SpamScore >= s.junkThreshold {
		ctx.SpamResult.Verdict = "junk"
		if m != nil {
			m.SpamDetected()
		}
		return ResultAccept
	}

	ctx.SpamResult.Verdict = "inbox"
	if m != nil {
		m.HamDetected()
	}
	return ResultAccept
}

// AVStage scans messages for viruses using ClamAV
type AVStage struct {
	scanner AVScanner
	action  string // "reject", "quarantine", "tag"
}

// AVScanner interface for virus scanning
type AVScanner interface {
	IsEnabled() bool
	Scan(data []byte) (*AVScanResult, error)
}

// AVScanResult holds virus scan result
type AVScanResult struct {
	Infected bool
	Virus    string
}

// NewAVStage creates a new antivirus scanning stage
func NewAVStage(scanner AVScanner, action string) *AVStage {
	return &AVStage{
		scanner: scanner,
		action:  action,
	}
}

func (s *AVStage) Name() string { return "AV" }

func (s *AVStage) Process(ctx *MessageContext) PipelineResult {
	if s.scanner == nil || !s.scanner.IsEnabled() {
		return ResultAccept
	}

	result, err := s.scanner.Scan(ctx.Data)
	if err != nil {
		// Scan error — accept but log
		return ResultAccept
	}

	if result.Infected {
		switch s.action {
		case "reject":
			ctx.Rejected = true
			ctx.RejectionCode = 550
			ctx.RejectionMessage = fmt.Sprintf("Message rejected: virus detected: %s", result.Virus)
			return ResultReject
		case "quarantine":
			ctx.Quarantine = true
			return ResultQuarantine
		default: // "tag"
			// Add tag header
			ctx.Headers["X-Virus"] = []string{result.Virus}
			return ResultAccept
		}
	}

	return ResultAccept
}

// DefaultLogger implements Logger interface
type defaultLogger struct{}

func (l *defaultLogger) Debug(msg string, args ...interface{}) {}
func (l *defaultLogger) Info(msg string, args ...interface{})  {}
func (l *defaultLogger) Warn(msg string, args ...interface{})  {}
func (l *defaultLogger) Error(msg string, args ...interface{}) {}
