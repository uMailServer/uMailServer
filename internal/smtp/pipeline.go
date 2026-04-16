package smtp

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/ratelimit"
	"github.com/umailserver/umailserver/internal/spam"
	"github.com/umailserver/umailserver/internal/tracing"
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
	ARCResult   ARCResult

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
	Score        float64
	Verdict      string // inbox, junk, quarantine, reject
	Reasons      []string
	BayesianProb float64 // 0-1 probability from Bayesian classifier
	RBLListed    bool    // True if listed on any RBL
	BayesianSpam bool    // True if Bayesian classified as spam
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

// ARCResult holds ARC (Authenticated Received Chain) validation results
type ARCResult struct {
	Result       string // pass, fail, none, permerror, temperror
	ChainValid   bool
	ChainLength  int
	SealDomain   string
	SealSelector string
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
	stages          []PipelineStage
	logger          Logger
	tracingProvider *tracing.Provider
}

// SetTracingProvider attaches an OpenTelemetry tracing provider so each stage
// emits a child span. A nil provider disables tracing without overhead.
func (p *Pipeline) SetTracingProvider(provider *tracing.Provider) {
	p.tracingProvider = provider
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

	traceCtx := context.Background()
	for _, stage := range p.stages {
		ctx.Stage = stage.Name()
		p.logger.Debug("Running pipeline stage",
			"stage", stage.Name(),
			"from", ctx.From,
		)

		result := p.runStage(traceCtx, stage, ctx)

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

// runStage executes a single pipeline stage, wrapping it in a tracing span
// when a provider is configured. The span name is "smtp.pipeline.<stage>" and
// carries the stage name as an attribute, the textual result as the status,
// and stage-specific outputs (SPF/DKIM/DMARC/ARC verdicts, spam score) pulled
// from the message context after the stage runs.
func (p *Pipeline) runStage(traceCtx context.Context, stage PipelineStage, msgCtx *MessageContext) PipelineResult {
	if p.tracingProvider == nil || !p.tracingProvider.IsEnabled() {
		return stage.Process(msgCtx)
	}
	_, span := p.tracingProvider.StartSpanWithKind(
		traceCtx,
		"smtp.pipeline."+stage.Name(),
		tracing.SpanKindInternal,
	)
	defer span.End()
	tracing.SetStringAttribute(span, "smtp.stage", stage.Name())

	result := stage.Process(msgCtx)

	switch result {
	case ResultReject:
		tracing.SetStringAttribute(span, "smtp.result", "reject")
		tracing.SetStringAttribute(span, "smtp.rejection_message", msgCtx.RejectionMessage)
		tracing.SetStatus(span, tracing.StatusError, "rejected by "+stage.Name())
	case ResultQuarantine:
		tracing.SetStringAttribute(span, "smtp.result", "quarantine")
		tracing.SetStatus(span, tracing.StatusOk, "")
	default:
		tracing.SetStringAttribute(span, "smtp.result", "accept")
		tracing.SetStatus(span, tracing.StatusOk, "")
	}

	// Enrich with stage-specific outputs that the stage may have populated.
	// These are cheap reads — empty strings/zero scores are skipped.
	if r := msgCtx.SPFResult.Result; r != "" {
		tracing.SetStringAttribute(span, "smtp.spf.result", r)
	}
	if d := msgCtx.SPFResult.Domain; d != "" {
		tracing.SetStringAttribute(span, "smtp.spf.domain", d)
	}
	if d := msgCtx.DKIMResult.Domain; d != "" {
		tracing.SetStringAttribute(span, "smtp.dkim.domain", d)
		tracing.SetBoolAttribute(span, "smtp.dkim.valid", msgCtx.DKIMResult.Valid)
	}
	if r := msgCtx.DMARCResult.Result; r != "" {
		tracing.SetStringAttribute(span, "smtp.dmarc.result", r)
		if pol := msgCtx.DMARCResult.Policy; pol != "" {
			tracing.SetStringAttribute(span, "smtp.dmarc.policy", pol)
		}
	}
	if r := msgCtx.ARCResult.Result; r != "" {
		tracing.SetStringAttribute(span, "smtp.arc.result", r)
	}
	if msgCtx.SpamScore != 0 {
		tracing.SetFloatAttribute(span, "smtp.spam.score", msgCtx.SpamScore)
	}
	return result
}

// Default pipeline stages

// RateLimitStage checks rate limits using the comprehensive ratelimit package
type RateLimitStage struct {
	limiter *ratelimit.RateLimiter
}

// NewRateLimitStage creates a new rate limit stage with the provided RateLimiter
func NewRateLimitStage(limiter *ratelimit.RateLimiter) *RateLimitStage {
	return &RateLimitStage{
		limiter: limiter,
	}
}

// NewRateLimitStageWithDefaults creates a new rate limit stage with default in-memory limits
// Deprecated: Use NewRateLimitStage with a properly configured RateLimiter instead
func NewRateLimitStageWithDefaults() *RateLimitStage {
	return &RateLimitStage{
		limiter: ratelimit.New(nil, nil), // nil bbolt, nil config (uses defaults)
	}
}

func (s *RateLimitStage) Name() string { return "RateLimit" }

func (s *RateLimitStage) Process(ctx *MessageContext) PipelineResult {
	if s.limiter == nil {
		return ResultAccept // No rate limiting configured
	}

	// For authenticated users, check user-based rate limits
	if ctx.Authenticated && ctx.Username != "" {
		result := s.limiter.CheckUser(ctx.Username)
		if !result.Allowed {
			ctx.Rejected = true
			ctx.RejectionCode = 421
			ctx.RejectionMessage = result.Reason
			if result.RetryAfter > 0 {
				ctx.RejectionMessage = fmt.Sprintf("%s (retry in %ds)", ctx.RejectionMessage, result.RetryAfter)
			}
			return ResultReject
		}

		// Also check recipient limit for authenticated users
		if len(ctx.To) > 0 {
			recipResult := s.limiter.CheckRecipients(ctx.Username, len(ctx.To))
			if !recipResult.Allowed {
				ctx.Rejected = true
				ctx.RejectionCode = 421
				ctx.RejectionMessage = recipResult.Reason
				return ResultReject
			}
		}
	}

	// For all connections, check IP-based rate limits
	result := s.limiter.CheckIP(ctx.RemoteIP.String())
	if !result.Allowed {
		ctx.Rejected = true
		ctx.RejectionCode = 421
		ctx.RejectionMessage = result.Reason
		if result.RetryAfter > 0 {
			ctx.RejectionMessage = fmt.Sprintf("%s (retry in %ds)", ctx.RejectionMessage, result.RetryAfter)
		}
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
	mu          sync.Mutex
	greylist    map[string]*greylistEntry
	lastCleanup time.Time
	maxEntries  int // Maximum entries before emergency cleanup
}

type greylistEntry struct {
	firstSeen time.Time
	allowed   bool
}

// NewGreylistStage creates a new greylisting stage
func NewGreylistStage() *GreylistStage {
	return &GreylistStage{
		greylist:    make(map[string]*greylistEntry),
		lastCleanup: time.Now(),
		maxEntries:  50000, // Maximum entries before emergency cleanup
	}
}

func (s *GreylistStage) Name() string { return "Greylist" }

func (s *GreylistStage) Process(ctx *MessageContext) PipelineResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Emergency cleanup if we've exceeded max entries
	if len(s.greylist) >= s.maxEntries {
		// Remove oldest 50% of entries
		cutoff := now.Add(-5 * time.Minute) // Keep entries newer than 5 minutes
		// Collect keys to delete first to avoid modifying map during iteration
		keysToDelete := make([]string, 0)
		for key, entry := range s.greylist {
			if entry.firstSeen.Before(cutoff) {
				keysToDelete = append(keysToDelete, key)
			}
			if len(s.greylist)-len(keysToDelete) < s.maxEntries/2 {
				break // Stop when we've removed enough
			}
		}
		for _, key := range keysToDelete {
			delete(s.greylist, key)
		}
	}

	// Periodically clean up stale greylist entries to prevent unbounded growth
	if now.Sub(s.lastCleanup) > 10*time.Minute {
		keysToDelete := make([]string, 0)
		for key, entry := range s.greylist {
			if now.Sub(entry.firstSeen) > 6*time.Hour {
				keysToDelete = append(keysToDelete, key)
			}
		}
		for _, key := range keysToDelete {
			delete(s.greylist, key)
		}
		s.lastCleanup = now
	}

	// Create triplet key: sender IP + sender email + recipient email
	for _, recipient := range ctx.To {
		key := fmt.Sprintf("%s:%s:%s", ctx.RemoteIP.String(), ctx.From, recipient)

		entry, exists := s.greylist[key]
		if !exists {
			// First time seeing this triplet
			s.greylist[key] = &greylistEntry{
				firstSeen: now,
				allowed:   false,
			}
			ctx.Rejected = true
			ctx.RejectionCode = 451
			ctx.RejectionMessage = "Greylisted, please try again later"
			return ResultReject
		}

		if !entry.allowed {
			// Check if enough time has passed (5 minutes)
			if now.Sub(entry.firstSeen) < 5*time.Minute {
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
	servers  []string
	resolver RBLDNSResolver
}

// RBLDNSResolver interface for RBL DNS lookups
type RBLDNSResolver interface {
	LookupHost(ctx context.Context, host string) (net.IP, error)
}

// realRBLDNSResolver performs actual DNS lookups for RBL checks
type realRBLDNSResolver struct{}

// NewRealRBLDNSResolver creates a resolver that performs real DNS lookups
func NewRealRBLDNSResolver() RBLDNSResolver {
	return &realRBLDNSResolver{}
}

func (r *realRBLDNSResolver) LookupHost(ctx context.Context, host string) (net.IP, error) {
	// Use net.LookupIP which returns A/AAAA records
	// We use the first returned IP as the RBL response
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no DNS records found")
	}
	return ips[0], nil
}

// RBLResult codes (first octet of returned IP indicates listing type)
var rblResultCodes = map[string]string{
	"127.0.0.1": "generic positive",
	"127.0.0.2": "confirmed spam source",
	"127.0.0.3": "confirmed spam source (alternative)",
	"127.0.0.4": "spam domain",
	"127.0.0.5": "phishing domain",
	"127.0.0.6": "malware domain",
	"127.0.0.7": "botnet server",
}

// NewRBLStage creates a new RBL check stage
func NewRBLStage(servers []string, resolver RBLDNSResolver) *RBLStage {
	return &RBLStage{servers: servers, resolver: resolver}
}

func (s *RBLStage) Name() string { return "RBL" }

func (s *RBLStage) Process(ctx *MessageContext) PipelineResult {
	if len(s.servers) == 0 {
		return ResultAccept
	}

	// Reverse IP (IPv4 only for now)
	ip := ctx.RemoteIP.String()
	reversedIP := reverseIP(ip)
	if reversedIP == "" {
		return ResultAccept
	}

	// Check each RBL
	for _, server := range s.servers {
		lookupHost := fmt.Sprintf("%s.%s", reversedIP, server)

		ip, err := s.resolver.LookupHost(context.Background(), lookupHost)
		if err != nil {
			// Not listed or DNS error - continue to next RBL
			continue
		}

		// IP is listed in this RBL
		ipStr := ip.String()
		resultCode := "listed"
		if code, ok := rblResultCodes[ipStr]; ok {
			resultCode = code
		}

		// RBL listing detected - spam score added above
		_ = resultCode // resultCode available for future logging

		// Add spam score based on listing type
		// Higher scores for more severe listings
		switch ipStr {
		case "127.0.0.2", "127.0.0.3":
			ctx.SpamScore += 3.0 // confirmed spam source
		case "127.0.0.4":
			ctx.SpamScore += 2.0 // spam domain
		case "127.0.0.5":
			ctx.SpamScore += 2.5 // phishing domain
		case "127.0.0.6":
			ctx.SpamScore += 3.0 // malware domain
		case "127.0.0.7":
			ctx.SpamScore += 3.0 // botnet server
		default:
			ctx.SpamScore += 1.5 // generic positive or unknown
		}
	}

	return ResultAccept
}

func reverseIP(ip string) string {
	// Check if IPv4
	parts := strings.Split(ip, ".")
	if len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.%s", parts[3], parts[2], parts[1], parts[0])
	}

	// IPv6: use nibble-based reverse (RFC 3596)
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil || parsedIP.To4() != nil {
		return "" // Invalid or IPv4
	}

	ipv6 := parsedIP.To16()
	if ipv6 == nil {
		return ""
	}

	// Build reversed nibble string per RFC 3596
	// Each nibble (4 bits) of the IPv6 address is reversed
	// For 2001:db8::1 (20010db80000000000000000000000001):
	// nibble 31 is low nibble of last byte, nibble 0 is high nibble of first byte
	var reversed strings.Builder
	for i := 15; i >= 0; i-- {
		high := (ipv6[i] >> 4) & 0x0F
		low := ipv6[i] & 0x0F
		// RFC 3596: nibble N+1 is high nibble, nibble N is low nibble
		// When building reverse, we go byte-by-byte: low nibble first, then high
		reversed.WriteString(fmt.Sprintf("%d.%d.", low, high))
	}
	reversed.WriteString("ip6.arpa")
	return reversed.String()
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

// BayesianStage implements Bayesian spam classification
type BayesianStage struct {
	classifier *spam.Classifier
	enabled    bool
}

// NewBayesianStage creates a new Bayesian spam classification stage
func NewBayesianStage(classifier *spam.Classifier) *BayesianStage {
	return &BayesianStage{
		classifier: classifier,
		enabled:    classifier != nil,
	}
}

func (s *BayesianStage) Name() string { return "Bayesian" }

func (s *BayesianStage) Process(ctx *MessageContext) PipelineResult {
	if !s.enabled || s.classifier == nil {
		return ResultAccept
	}

	// Extract tokens from headers and body
	headerTokens := spam.ExtractTokensFromHeaders(ctx.Headers)
	bodyTokens := spam.ExtractTokensFromBody(ctx.Data)

	// Combine tokens
	var allTokens []string
	allTokens = append(allTokens, headerTokens...)
	allTokens = append(allTokens, bodyTokens...)

	if len(allTokens) == 0 {
		return ResultAccept
	}

	// Classify
	result, err := s.classifier.Classify(allTokens)
	if err != nil {
		// On error, don't reject - let other stages decide
		return ResultAccept
	}

	// Add to spam score based on probability
	// Probability of 0.7+ adds 3.0, 0.5-0.7 adds 1.5, etc.
	if result.SpamProbability > 0.9 {
		ctx.SpamScore += 4.0
		if ctx.SpamResult.Reasons == nil {
			ctx.SpamResult.Reasons = make([]string, 0)
		}
		ctx.SpamResult.Reasons = append(ctx.SpamResult.Reasons, fmt.Sprintf("bayesian=%.2f", result.SpamProbability))
	} else if result.SpamProbability > 0.7 {
		ctx.SpamScore += 3.0
		if ctx.SpamResult.Reasons == nil {
			ctx.SpamResult.Reasons = make([]string, 0)
		}
		ctx.SpamResult.Reasons = append(ctx.SpamResult.Reasons, fmt.Sprintf("bayesian=%.2f", result.SpamProbability))
	} else if result.SpamProbability > 0.5 {
		ctx.SpamScore += 1.0
	}

	return ResultAccept
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

func (l *defaultLogger) Debug(msg string, args ...interface{}) { _ = msg }
func (l *defaultLogger) Info(msg string, args ...interface{})  { _ = msg }
func (l *defaultLogger) Warn(msg string, args ...interface{})  { _ = msg }
func (l *defaultLogger) Error(msg string, args ...interface{}) { _ = msg }
