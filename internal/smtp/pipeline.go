package smtp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unicode"

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

	// AddedHeaders are header fields (name, value) that stages want written
	// into the delivered message; the session emits them after stripping any
	// sender-supplied field of the same name (F6614).
	AddedHeaders [][2]string

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

	// Context is cancelled when the running stage exceeds its time budget;
	// stages that do I/O should honour it. Set by the pipeline per stage.
	Context context.Context
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
	stageTimeout    time.Duration
}

// DefaultStageTimeout bounds one pipeline stage when no other timeout is set.
const DefaultStageTimeout = 30 * time.Second

// errStageTimeout is returned by Process when a stage ran past its budget.
// The stage may still be running and writing to the MessageContext, so the
// caller must not read the context after receiving it.
var errStageTimeout = errors.New("pipeline stage timed out")

// SetStageTimeout sets the per-stage time budget; 0 selects
// DefaultStageTimeout and a negative value disables the limit.
func (p *Pipeline) SetStageTimeout(d time.Duration) {
	p.stageTimeout = d
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
	return p.ProcessWithTimeout(ctx, 0)
}

// ProcessWithTimeout is Process with a per-stage budget override (0 keeps the
// pipeline's own setting). A stage that exceeds it is abandoned and the
// message is answered with errStageTimeout so a stuck stage cannot hold the
// session forever.
func (p *Pipeline) ProcessWithTimeout(ctx *MessageContext, timeout time.Duration) (PipelineResult, error) {
	if timeout == 0 {
		timeout = p.stageTimeout
	}
	if timeout == 0 {
		timeout = DefaultStageTimeout
	}
	p.logger.Info("Starting message pipeline",
		"from", ctx.From,
		"to", ctx.To,
		"remote_ip", ctx.RemoteIP,
	)

	traceCtx := context.Background()
	for _, stage := range p.stages {
		ctx.Stage = stage.Name()

		result, timedOut := p.runStageTimed(traceCtx, stage, ctx, timeout)
		if timedOut {
			p.logger.Error("pipeline stage timed out", "stage", stage.Name(), "timeout", timeout)
			return ResultReject, fmt.Errorf("%w: %s", errStageTimeout, stage.Name())
		}

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

// runStageTimed runs a stage with a deadline (timeout < 0: none). On timeout
// the stage goroutine is left to finish on its own and timedOut is true.
func (p *Pipeline) runStageTimed(traceCtx context.Context, stage PipelineStage, msgCtx *MessageContext, timeout time.Duration) (PipelineResult, bool) {
	if timeout < 0 {
		msgCtx.Context = traceCtx
		return p.runStage(traceCtx, stage, msgCtx), false
	}
	stageCtx, cancel := context.WithTimeout(traceCtx, timeout)
	defer cancel()
	msgCtx.Context = stageCtx
	done := make(chan PipelineResult, 1)
	go func() { done <- p.runStage(stageCtx, stage, msgCtx) }()
	select {
	case r := <-done:
		return r, false
	case <-stageCtx.Done():
		select { // prefer a result that arrived at the same moment
		case r := <-done:
			return r, false
		default:
		}
		return ResultReject, true
	}
}

// runStage executes a single pipeline stage, wrapping it in a tracing span
// when a provider is configured. The span name is "smtp.pipeline.<stage>" and
// carries the stage name as an attribute, the textual result as the status,
// and stage-specific outputs (SPF/DKIM/DMARC/ARC verdicts, spam score) pulled
// from the message context after the stage runs.
func (p *Pipeline) runStage(traceCtx context.Context, stage PipelineStage, msgCtx *MessageContext) (result PipelineResult) {
	// A panicking stage must not unwind the session; fail closed with a
	// rejection instead of delivering an unchecked message (F6156).
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("pipeline stage panicked", "stage", stage.Name(), "panic", r)
			msgCtx.RejectionMessage = "internal error in " + stage.Name()
			msgCtx.RejectionCode = 451
			result = ResultReject
		}
	}()
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

	result = stage.Process(msgCtx)

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

	// The recipient limit is checked first: it is stateless, and a message
	// refused for too many recipients was never sent. (F5128)
	user := ""
	if ctx.Authenticated && ctx.Username != "" {
		user = strings.ToLower(ctx.Username) // usernames are case-insensitive keys
		if len(ctx.To) > 0 {
			recipResult := s.limiter.CheckRecipients(user, len(ctx.To))
			if !recipResult.Allowed {
				ctx.Rejected = true
				ctx.RejectionCode = 451 // 421 would mean the channel is closing (F7312)
				ctx.RejectionMessage = recipResult.Reason
				return ResultReject
			}
		}
	}

	// User (authenticated only), IP and global limits are checked together
	// and the message is counted against them only if all allow it, so an
	// IP or global refusal does not spend the user's daily quota. (F5205)
	result := s.limiter.CheckMessage(user, ctx.RemoteIP.String())
	if !result.Allowed {
		ctx.Rejected = true
		ctx.RejectionCode = 451 // 421 would mean the channel is closing (F7312)
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
	switch result.Result {
	case "fail":
		ctx.SpamScore += 2.0
	case "softfail":
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
	delay       time.Duration
	now         func() time.Time // injectable clock (tests)
}

// DefaultGreylistDelay is the retry delay used when none is configured.
const DefaultGreylistDelay = 5 * time.Minute

// GreylistOption customizes a GreylistStage.
type GreylistOption func(*GreylistStage)

// WithGreylistDelay sets the minimum delay before a retried triplet is
// accepted. Non-positive values keep the default.
func WithGreylistDelay(d time.Duration) GreylistOption {
	return func(s *GreylistStage) {
		if d > 0 {
			s.delay = d
		}
	}
}

// WithGreylistClock injects a clock (for tests).
func WithGreylistClock(now func() time.Time) GreylistOption {
	return func(s *GreylistStage) {
		if now != nil {
			s.now = now
		}
	}
}

type greylistEntry struct {
	firstSeen time.Time
	lastSeen  time.Time
	allowed   bool
}

// NewGreylistStage creates a new greylisting stage
func NewGreylistStage(opts ...GreylistOption) *GreylistStage {
	s := &GreylistStage{
		greylist:   make(map[string]*greylistEntry),
		maxEntries: 50000, // Maximum entries before emergency cleanup
		delay:      DefaultGreylistDelay,
		now:        time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	s.lastCleanup = s.now()
	return s
}

func (s *GreylistStage) Name() string { return "Greylist" }

func (s *GreylistStage) Process(ctx *MessageContext) PipelineResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()

	// Emergency cleanup if we've exceeded max entries
	if len(s.greylist) >= s.maxEntries {
		cutoff := now.Add(-5 * time.Minute)
		for key, entry := range s.greylist {
			if entry.firstSeen.Before(cutoff) {
				delete(s.greylist, key)
			}
			if len(s.greylist) < s.maxEntries/2 {
				break
			}
		}
		// If everything is recent, still bound memory by evicting arbitrary entries.
		for key := range s.greylist {
			if len(s.greylist) < s.maxEntries {
				break
			}
			delete(s.greylist, key)
		}
	}

	// Periodically clean up stale greylist entries to prevent unbounded growth.
	// Pending entries expire 6h after first sight; passed entries expire
	// after 36 days without being seen (F5832).
	if now.Sub(s.lastCleanup) > 10*time.Minute {
		for key, entry := range s.greylist {
			if greylistExpired(entry, now) {
				delete(s.greylist, key)
			}
		}
		s.lastCleanup = now
	}

	// Create triplet key: sender IP + sender email + recipient email
	// (addresses are case-insensitive for greylisting purposes).
	deferred := false
	from := strings.ToLower(ctx.From)
	client := greylistClient(ctx.RemoteIP)
	for _, recipient := range ctx.To {
		key := client + "|" + from + "|" + strings.ToLower(recipient)

		entry, exists := s.greylist[key]
		if exists && greylistExpired(entry, now) {
			delete(s.greylist, key)
			exists = false
		}
		if !exists {
			// First time seeing this triplet
			s.greylist[key] = &greylistEntry{firstSeen: now, lastSeen: now}
			deferred = true
			continue
		}

		if !entry.allowed {
			// Check if the configured delay has passed
			if now.Sub(entry.firstSeen) < s.delay {
				deferred = true
				continue
			}
			entry.allowed = true
		}
		entry.lastSeen = now
	}

	if deferred {
		ctx.Rejected = true
		ctx.RejectionCode = 451
		ctx.RejectionMessage = "Greylisted, please try again later"
		return ResultReject
	}

	return ResultAccept
}

// greylistClient names the sending network for a triplet: the /24 of an IPv4
// client or the /64 of an IPv6 one, so that retries from another host of the
// same sending farm are recognised (F6613).
func greylistClient(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String() + "/24"
	}
	if v6 := ip.To16(); v6 != nil {
		return v6.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return "unknown"
}

func greylistExpired(e *greylistEntry, now time.Time) bool {
	if e.allowed {
		last := e.lastSeen
		if last.IsZero() {
			last = e.firstSeen
		}
		return now.Sub(last) > 36*24*time.Hour
	}
	return now.Sub(e.firstSeen) > 6*time.Hour
}

// rblLookupTimeout bounds each DNSBL lookup.
const rblLookupTimeout = 3 * time.Second

// RBLStage checks DNS blocklists
type RBLStage struct {
	servers  []string
	resolver RBLDNSResolver

	cacheMu sync.Mutex
	cache   map[string]rblCacheEntry
}

// rblCacheEntry is a cached lookup outcome; ip == nil means "not listed".
type rblCacheEntry struct {
	ip  net.IP
	exp time.Time
}

const (
	rblMaxParallel  = 4
	rblListedTTL    = 5 * time.Minute
	rblNotListedTTL = time.Minute
	rblCacheMax     = 4096
)

// lookup resolves one DNSBL name through the TTL cache. Errors other than a
// definite "not listed" are not cached.
func (s *RBLStage) lookup(parent context.Context, host string) net.IP {
	now := time.Now()
	s.cacheMu.Lock()
	if e, ok := s.cache[host]; ok && now.Before(e.exp) {
		s.cacheMu.Unlock()
		return e.ip
	}
	s.cacheMu.Unlock()

	lctx, cancel := context.WithTimeout(parent, rblLookupTimeout)
	ip, err := s.resolver.LookupHost(lctx, host)
	cancel()
	var dnsErr *net.DNSError
	switch {
	case err == nil && ip != nil:
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		ip = nil
	default:
		return nil // transient: do not cache
	}
	ttl := rblNotListedTTL
	if ip != nil {
		ttl = rblListedTTL
	}
	s.cacheMu.Lock()
	if s.cache == nil || len(s.cache) >= rblCacheMax {
		s.cache = make(map[string]rblCacheEntry)
	}
	s.cache[host] = rblCacheEntry{ip: ip, exp: now.Add(ttl)}
	s.cacheMu.Unlock()
	return ip
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
	// Context-aware lookup so a slow DNSBL cannot stall the SMTP session
	// (F5834). Prefer IPv4 answers: DNSBL listings are A records.
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no DNS records found")
	}
	for _, a := range addrs {
		if a.IP.To4() != nil {
			return a.IP, nil
		}
	}
	return addrs[0].IP, nil
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

	// Query the lists in parallel (bounded), then score in list order.
	parent := ctx.Context
	if parent == nil {
		parent = context.Background()
	}
	answers := make([]net.IP, len(s.servers))
	sem := make(chan struct{}, rblMaxParallel)
	var wg sync.WaitGroup
	for i, server := range s.servers {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, host string) {
			defer wg.Done()
			defer func() { <-sem }()
			answers[i] = s.lookup(parent, host)
		}(i, fmt.Sprintf("%s.%s", reversedIP, server))
	}
	wg.Wait()

	for _, ip := range answers {
		if ip == nil {
			continue
		}
		// Only the documented 127.0.0.x return codes are listings; other
		// 127.x answers (127.255.255.x operator errors, wildcard or
		// hijacked responses) are ignored (F5835, F6616).
		if _, documented := rblResultCodes[ip.String()]; !documented {
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
		fmt.Fprintf(&reversed, "%x.%x.", low, high)
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
				return len(ctx.Headers["Subject"]) == 0
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
				if len(subject) <= 5 {
					return false
				}
				return strings.IndexFunc(subject, unicode.IsLower) == -1
			},
		},
		{
			Name:        "MISSING_DATE",
			Description: "No Date header",
			Score:       1.0,
			Check: func(ctx *MessageContext) bool {
				return ctx.Headers["Date"] == nil
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
	// failOpen accepts messages when the scan errors. Default false:
	// scan errors temp-fail (451) so mail is retried rather than delivered
	// unscanned (F5830).
	failOpen bool
}

// SetFailOpen configures whether scan errors accept the message.
func (s *AVStage) SetFailOpen(v bool) { s.failOpen = v }

// AVScanner interface for virus scanning
type AVScanner interface {
	IsEnabled() bool
	Scan(data []byte) (*AVScanResult, error)
}

// AVScanResult holds virus scan result
type AVScanResult struct {
	Infected bool
	Virus    string
	// Skipped means no scan took place; Infected=false then is not "clean".
	Skipped bool
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
	if err != nil || result == nil {
		if s.failOpen {
			ctx.Headers["X-Virus-Scan"] = []string{"error"}
			ctx.AddedHeaders = append(ctx.AddedHeaders, [2]string{"X-Virus-Scan", "error"})
			return ResultAccept
		}
		ctx.Rejected = true
		ctx.RejectionCode = 451
		ctx.RejectionMessage = "4.7.1 Virus scanner unavailable, please try again later"
		return ResultReject
	}

	if result.Skipped {
		// Not scanned: accept, but never claim the message is clean.
		ctx.Headers["X-Virus-Scan"] = []string{"skipped"}
		ctx.AddedHeaders = append(ctx.AddedHeaders, [2]string{"X-Virus-Scan", "skipped"})
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
			// The signature name comes from the scanner reply: keep it to
			// one printable line so it cannot inject header fields.
			name := strings.Map(func(r rune) rune {
				if r < 0x20 || r == 0x7f {
					return -1
				}
				return r
			}, result.Virus)
			if len(name) > 200 {
				name = name[:200]
			}
			ctx.AddedHeaders = append(ctx.AddedHeaders, [2]string{"X-Virus", strings.TrimSpace(name)})
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
