package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
	"golang.org/x/net/publicsuffix"
)

// DMARCResult represents the result of DMARC evaluation
type DMARCResult int

const (
	DMARCNone      DMARCResult = iota // No DMARC record
	DMARCPass                         // DMARC check passed
	DMARCFail                         // DMARC check failed
	DMARCPermError                    // Permanent error
	DMARCTempError                    // Temporary error
)

func (r DMARCResult) String() string {
	switch r {
	case DMARCNone:
		return "none"
	case DMARCPass:
		return "pass"
	case DMARCFail:
		return "fail"
	case DMARCPermError:
		return "permerror"
	case DMARCTempError:
		return "temperror"
	default:
		return "unknown"
	}
}

// DMARCPolicy represents the DMARC policy
type DMARCPolicy string

const (
	DMARCPolicyNone       DMARCPolicy = "none"
	DMARCPolicyQuarantine DMARCPolicy = "quarantine"
	DMARCPolicyReject     DMARCPolicy = "reject"
)

// DMARCAlignment represents the alignment mode
type DMARCAlignment string

const (
	DMARCAlignmentRelaxed DMARCAlignment = "r"
	DMARCAlignmentStrict  DMARCAlignment = "s"
)

// DMARCRecord represents a parsed DMARC policy record
type DMARCRecord struct {
	Version            string         // v=DMARC1 (required)
	Policy             DMARCPolicy    // p= (required)
	SubdomainPolicy    DMARCPolicy    // sp= (optional, defaults to p)
	AlignmentDKIM      DMARCAlignment // adkim= (optional, defaults to r)
	AlignmentSPF       DMARCAlignment // aspf= (optional, defaults to r)
	Percentage         int            // pct= (optional, defaults to 100)
	ReportAggregateURI []string       // rua= (optional)
	ReportForensicURI  []string       // ruf= (optional)
	ReportInterval     int            // ri= (optional, defaults to 86400)
	FailureReports     []string       // fo= (optional)
}

// DMARCEvaluator evaluates DMARC policy for a message
type DMARCEvaluator struct {
	resolver DNSResolver
	clock    func() time.Time
	cache    *dmarcCache
}

// dmarcCacheEntry represents a cached DMARC record
type dmarcCacheEntry struct {
	record    *DMARCRecord
	expiresAt time.Time
}

// dmarcCache caches DMARC lookup results
type dmarcCache struct {
	entries map[string]*dmarcCacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
	maxSize int
}

// newDMARCCache creates a new DMARC cache
func newDMARCCache() *dmarcCache {
	return &dmarcCache{
		entries: make(map[string]*dmarcCacheEntry),
		ttl:     5 * time.Minute,
		maxSize: 10000,
	}
}

// get retrieves a cached record
func (c *dmarcCache) get(domain string) (*DMARCRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[domain]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.record, true
}

// set stores a record in the cache
func (c *dmarcCache) set(domain string, record *DMARCRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[domain] = &dmarcCacheEntry{
		record:    record,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// evictOldest removes expired entries, then random entries if still full
func (c *dmarcCache) evictOldest() {
	now := time.Now()
	for domain, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, domain)
		}
	}

	// If still at capacity, remove random entry
	if len(c.entries) >= c.maxSize {
		for domain := range c.entries {
			delete(c.entries, domain)
			break
		}
	}
}

// DMARCEvaluation holds the results of DMARC evaluation
type DMARCEvaluation struct {
	Result        DMARCResult
	Policy        DMARCPolicy
	AppliedPolicy DMARCPolicy // The policy actually applied (may differ due to pct)
	Domain        string
	Explanation   string
	Disposition   string // none/quarantine/reject
}

// NewDMARCEvaluator creates a new DMARC evaluator
func NewDMARCEvaluator(resolver DNSResolver) *DMARCEvaluator {
	return &DMARCEvaluator{
		resolver: resolver,
		clock:    time.Now,
		cache:    newDMARCCache(),
	}
}

// Evaluate evaluates DMARC for the given message
func (e *DMARCEvaluator) Evaluate(ctx context.Context, fromDomain string, spfResult SPFResult, spfDomain string, dkimResult DKIMResult, dkimDomain string) (*DMARCEvaluation, error) {
	record, err := e.policyRecord(ctx, fromDomain)
	if err != nil {
		return &DMARCEvaluation{
			Result:      DMARCTempError,
			Domain:      fromDomain,
			Explanation: "DNS lookup failed",
		}, nil
	}
	if record == dmarcMultipleRecords {
		return dmarcMultipleRecordsEvaluation(fromDomain), nil
	}
	fromOrg := false

	// F4893: RFC 7489 §6.6.3 — with no record at the From domain, use the
	// organizational domain's record.
	if record == nil {
		org, perr := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(fromDomain))
		if perr == nil && !strings.EqualFold(org, fromDomain) {
			record, err = e.policyRecord(ctx, org)
			if err != nil {
				return &DMARCEvaluation{
					Result:      DMARCTempError,
					Domain:      fromDomain,
					Explanation: "DNS lookup failed",
				}, nil
			}
			if record == dmarcMultipleRecords {
				return dmarcMultipleRecordsEvaluation(fromDomain), nil
			}
			fromOrg = record != nil
		}
	}

	return e.evaluateWithRecord(fromDomain, spfResult, spfDomain, dkimResult, dkimDomain, record, fromOrg)
}

// errDMARCMultipleRecords and dmarcMultipleRecords mark a _dmarc TXT set
// holding more than one "v=DMARC1" record. F5411: RFC 7489 §6.6.3 step 4 —
// policy discovery then terminates and DMARC is not applied (no
// organizational-domain fallback). The sentinel is what policyRecord caches.
var (
	errDMARCMultipleRecords = errors.New("multiple DMARC records")
	dmarcMultipleRecords    = &DMARCRecord{}
)

func dmarcMultipleRecordsEvaluation(fromDomain string) *DMARCEvaluation {
	return &DMARCEvaluation{
		Result:      DMARCNone,
		Policy:      DMARCPolicyNone,
		Domain:      fromDomain,
		Explanation: "Multiple DMARC records found",
	}
}

// policyRecord returns the DMARC record published at _dmarc.<domain>
// (nil when there is none), using the cache. Only temporary DNS failures
// are returned as errors.
func (e *DMARCEvaluator) policyRecord(ctx context.Context, domain string) (*DMARCRecord, error) {
	// Check cache first
	if record, ok := e.cache.get(domain); ok {
		metrics.Get().DMARCCacheHit()
		return record, nil
	}

	metrics.Get().DMARCCacheMiss()

	// Look up DMARC record
	record, err := e.lookupDMARC(ctx, domain)
	if err != nil {
		if isTemporaryError(err) {
			return nil, err
		}
		if errors.Is(err, errDMARCMultipleRecords) {
			e.cache.set(domain, dmarcMultipleRecords)
			return dmarcMultipleRecords, nil
		}
		// No DMARC record found - cache negative result
		e.cache.set(domain, nil)
		return nil, nil
	}

	// Cache the record
	e.cache.set(domain, record)
	return record, nil
}

// evaluateWithRecord evaluates DMARC using a cached or looked-up record
// fromOrg reports that record was found at the organizational domain
// rather than at fromDomain itself.
func (e *DMARCEvaluator) evaluateWithRecord(fromDomain string, spfResult SPFResult, spfDomain string, dkimResult DKIMResult, dkimDomain string, record *DMARCRecord, fromOrg bool) (*DMARCEvaluation, error) {
	// Handle negative cache entry (nil record means no DMARC record found)
	if record == nil {
		return &DMARCEvaluation{
			Result:      DMARCNone,
			Policy:      DMARCPolicyNone,
			Domain:      fromDomain,
			Explanation: "No DMARC record found",
		}, nil
	}

	// Validate the record
	if record.Version != "DMARC1" {
		return &DMARCEvaluation{
			Result:      DMARCPermError,
			Domain:      fromDomain,
			Explanation: "Invalid DMARC version",
		}, nil
	}

	// Check alignment
	spfAligned := checkAlignment(spfDomain, fromDomain, record.AlignmentSPF)
	dkimAligned := checkAlignment(dkimDomain, fromDomain, record.AlignmentDKIM)

	// DMARC passes if either SPF or DKIM passes AND is aligned
	spfPassed := spfResult == SPFPass && spfAligned
	dkimPassed := dkimResult == DKIMPass && dkimAligned

	evaluation := &DMARCEvaluation{
		Domain: fromDomain,
		Policy: record.Policy,
	}

	if spfPassed || dkimPassed {
		evaluation.Result = DMARCPass
		evaluation.AppliedPolicy = DMARCPolicyNone
		evaluation.Disposition = "none"
		if spfPassed {
			evaluation.Explanation = "SPF aligned"
		} else {
			evaluation.Explanation = "DKIM aligned"
		}
	} else {
		evaluation.Result = DMARCFail

		// Determine which policy to apply
		policyToApply := record.Policy

		// F4894: sp= applies only when the record came from the
		// organizational domain (RFC 7489 §6.3, §6.6.3), not by label count.
		if record.SubdomainPolicy != "" && fromOrg {
			policyToApply = record.SubdomainPolicy
		}

		// Apply percentage sampling
		if record.Percentage < 100 {
			if !shouldApplyPolicy(record.Percentage) {
				// F5078: RFC 7489 §6.6.4 — an unsampled message gets the
				// next-lower policy: reject → quarantine, quarantine → none.
				if policyToApply == DMARCPolicyReject {
					policyToApply = DMARCPolicyQuarantine
				} else {
					policyToApply = DMARCPolicyNone
				}
				evaluation.Explanation = fmt.Sprintf("DMARC policy not applied due to pct=%d", record.Percentage)
			} else {
				evaluation.Explanation = fmt.Sprintf("DMARC policy applied (pct=%d)", record.Percentage)
			}
		} else {
			evaluation.Explanation = "DMARC check failed"
		}

		evaluation.AppliedPolicy = policyToApply

		// Determine disposition
		switch policyToApply {
		case DMARCPolicyNone:
			evaluation.Disposition = "none"
		case DMARCPolicyQuarantine:
			evaluation.Disposition = "quarantine"
		case DMARCPolicyReject:
			evaluation.Disposition = "reject"
		default:
			evaluation.Disposition = "none"
		}
	}

	return evaluation, nil
}

// lookupDMARC looks up the DMARC record for a domain
func (e *DMARCEvaluator) lookupDMARC(ctx context.Context, domain string) (*DMARCRecord, error) {
	// DMARC records are at _dmarc.domain
	queryDomain := "_dmarc." + domain

	txtRecords, err := e.resolver.LookupTXT(ctx, queryDomain)
	if err != nil {
		return nil, err
	}

	var found string
	count := 0
	for _, record := range txtRecords {
		if isDMARCRecord(record) {
			found = record
			count++
		}
	}
	if count > 1 {
		return nil, errDMARCMultipleRecords
	}
	if count == 1 {
		return parseDMARCRecord(found)
	}

	return nil, errors.New("no DMARC record found")
}

// isDMARCRecord reports whether a TXT string starts with the "v=DMARC1" tag
// (F5707: "v=DMARC10" is not a DMARC record, RFC 7489 §6.3).
func isDMARCRecord(record string) bool {
	if !strings.HasPrefix(record, "v=DMARC1") {
		return false
	}
	rest := strings.TrimLeft(record[len("v=DMARC1"):], " \t")
	return rest == "" || rest[0] == ';'
}

// parseDMARCRecord parses a DMARC DNS TXT record
func parseDMARCRecord(record string) (*DMARCRecord, error) {
	rec := &DMARCRecord{
		Version:        "DMARC1",
		Policy:         "", // Required - will error if not set
		AlignmentDKIM:  DMARCAlignmentRelaxed,
		AlignmentSPF:   DMARCAlignmentRelaxed,
		Percentage:     100,
		ReportInterval: 86400, // 24 hours
		FailureReports: []string{"0"},
	}

	// Parse tag-value pairs
	tags := parseTagValueList(record)

	for tag, value := range tags {
		switch tag {
		case "v":
			rec.Version = value
		case "p":
			rec.Policy = DMARCPolicy(strings.ToLower(value))
		case "sp":
			rec.SubdomainPolicy = DMARCPolicy(strings.ToLower(value))
		case "adkim":
			rec.AlignmentDKIM = DMARCAlignment(strings.ToLower(value))
		case "aspf":
			rec.AlignmentSPF = DMARCAlignment(strings.ToLower(value))
		case "pct":
			pct, err := strconv.Atoi(value)
			if err == nil && pct >= 0 && pct <= 100 {
				rec.Percentage = pct
			}
		case "rua":
			rec.ReportAggregateURI = parseURIList(value)
		case "ruf":
			rec.ReportForensicURI = parseURIList(value)
		case "ri":
			ri, err := strconv.Atoi(value)
			if err == nil && ri > 0 {
				rec.ReportInterval = ri
			}
		case "fo":
			rec.FailureReports = parseFailureOptions(value)
		}
	}

	// Validate required fields
	if rec.Version != "DMARC1" {
		return nil, errors.New("invalid DMARC version")
	}

	validPolicy := rec.Policy == DMARCPolicyNone || rec.Policy == DMARCPolicyQuarantine || rec.Policy == DMARCPolicyReject
	if !validPolicy {
		// F6193: RFC 7489 §6.6.3 — without a valid p=, a record with a valid
		// rua is treated as p=none; otherwise it is discarded.
		if !dmarcHasValidRua(rec.ReportAggregateURI) {
			if rec.Policy == "" {
				return nil, errors.New("missing required policy (p=)")
			}
			return nil, errors.New("invalid policy value")
		}
		rec.Policy = DMARCPolicyNone
	}

	// F5410: RFC 7489 §6.3 — an invalid sp= is discarded in favour of its
	// default (p=); it must not become the applied policy.
	if sp := rec.SubdomainPolicy; sp != DMARCPolicyNone && sp != DMARCPolicyQuarantine && sp != DMARCPolicyReject {
		rec.SubdomainPolicy = ""
	}

	// Validate alignment modes
	if rec.AlignmentDKIM != DMARCAlignmentRelaxed && rec.AlignmentDKIM != DMARCAlignmentStrict {
		rec.AlignmentDKIM = DMARCAlignmentRelaxed // Default
	}
	if rec.AlignmentSPF != DMARCAlignmentRelaxed && rec.AlignmentSPF != DMARCAlignmentStrict {
		rec.AlignmentSPF = DMARCAlignmentRelaxed // Default
	}

	return rec, nil
}

// checkAlignment checks if the auth domain aligns with the From domain
func checkAlignment(authDomain, fromDomain string, mode DMARCAlignment) bool {
	if authDomain == "" {
		return false
	}

	if mode == DMARCAlignmentStrict {
		// Strict: exact match
		return strings.EqualFold(authDomain, fromDomain)
	}

	// Relaxed: organizational domain match (default)
	return isOrganizationalDomainMatch(authDomain, fromDomain)
}

// isOrganizationalDomainMatch checks if two domains share the same organizational domain
func isOrganizationalDomainMatch(domain1, domain2 string) bool {
	domain1 = strings.ToLower(domain1)
	domain2 = strings.ToLower(domain2)

	// Exact match
	if domain1 == domain2 {
		return true
	}

	org1, err := publicsuffix.EffectiveTLDPlusOne(domain1)
	if err != nil {
		return false
	}
	org2, err := publicsuffix.EffectiveTLDPlusOne(domain2)
	return err == nil && org1 == org2
}

// shouldApplyPolicy determines if DMARC policy should be applied based on percentage
func shouldApplyPolicy(percentage int) bool {
	if percentage >= 100 {
		return true
	}
	if percentage <= 0 {
		return false
	}
	// F6191: uniform draw in [0,100) via rand.Int; byte%100 was biased.
	n, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		// If crypto/rand fails, default to applying policy (fail open for availability)
		return true
	}
	return int(n.Int64()) < percentage
}

// parseURIList parses a comma-separated list of URIs
func parseURIList(s string) []string {
	var result []string
	for _, uri := range strings.Split(s, ",") {
		uri = strings.TrimSpace(uri)
		if uri != "" {
			result = append(result, uri)
		}
	}
	return result
}

// parseFailureOptions parses the fo= tag value
func parseFailureOptions(s string) []string {
	var result []string
	for _, opt := range strings.Split(s, ":") {
		opt = strings.TrimSpace(opt)
		if opt != "" {
			result = append(result, opt)
		}
	}
	if len(result) == 0 {
		return []string{"0"}
	}
	return result
}

// dmarcHasValidRua reports whether any rua URI is a mailto: URI.
func dmarcHasValidRua(uris []string) bool {
	for _, u := range uris {
		if len(u) > len("mailto:") && strings.EqualFold(u[:len("mailto:")], "mailto:") {
			return true
		}
	}
	return false
}
