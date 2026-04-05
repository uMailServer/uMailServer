package auth

// MTA-STS (RFC 8461) and TLSRPT (RFC 8460) enable TLS policy enforcement and
// failure reporting. Integrated into queue manager via MTASTSValidator
// in internal/queue/manager.go deliverToMX function.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MTASTSMode represents the MTA-STS policy mode
type MTASTSMode string

const (
	MTASTSModeEnforce  MTASTSMode = "enforce"
	MTASTSModeTesting  MTASTSMode = "testing"
	MTASTSModeNone     MTASTSMode = "none"
	MTASTSModeDisabled MTASTSMode = "disabled"
)

// MTASTSPolicy represents a parsed MTA-STS policy
type MTASTSPolicy struct {
	Version string     // Must be "STSv1"
	Mode    MTASTSMode // enforce, testing, or none
	MX      []string   // List of MX patterns
	MaxAge  int        // Policy max age in seconds
	Raw     string     // Raw policy text
}

// MTASTSRecord represents the DNS TXT record
type MTASTSRecord struct {
	Version string // Must be "STSv1"
	ID      string // Policy ID (base64 encoded hash)
}

// MTASTSCacheEntry represents a cached policy
type MTASTSCacheEntry struct {
	Policy    *MTASTSPolicy
	Domain    string
	FetchedAt time.Time
	ExpiresAt time.Time
}

// MTASTSValidator handles MTA-STS policy validation
type MTASTSValidator struct {
	resolver   DNSResolver
	cache      map[string]*MTASTSCacheEntry
	cacheMu    sync.RWMutex
	httpClient *http.Client
}

// NewMTASTSValidator creates a new MTA-STS validator
func NewMTASTSValidator(resolver DNSResolver) *MTASTSValidator {
	return &MTASTSValidator{
		resolver: resolver,
		cache:    make(map[string]*MTASTSCacheEntry),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CheckPolicy checks if a given MX matches the MTA-STS policy for a domain
func (v *MTASTSValidator) CheckPolicy(ctx context.Context, domain string, mx string) (bool, *MTASTSPolicy, error) {
	// Get policy for domain
	policy, err := v.GetPolicy(ctx, domain)
	if err != nil {
		return false, nil, err
	}

	if policy == nil || policy.Mode == MTASTSModeNone {
		// No policy or policy is "none" - allow any MX
		return true, policy, nil
	}

	// Check if MX matches any of the allowed patterns
	matched := false
	for _, pattern := range policy.MX {
		if matchMX(pattern, mx) {
			matched = true
			break
		}
	}

	return matched, policy, nil
}

// GetPolicy gets the MTA-STS policy for a domain
func (v *MTASTSValidator) GetPolicy(ctx context.Context, domain string) (*MTASTSPolicy, error) {
	// Check cache first
	v.cacheMu.RLock()
	entry, ok := v.cache[domain]
	v.cacheMu.RUnlock()

	if ok && time.Now().Before(entry.ExpiresAt) {
		return entry.Policy, nil
	}

	// Fetch fresh policy
	policy, err := v.fetchPolicy(ctx, domain)
	if err != nil {
		return nil, err
	}

	if policy == nil {
		// No policy found - cache negative result for a short time
		v.cacheMu.Lock()
		v.cache[domain] = &MTASTSCacheEntry{
			Policy:    nil,
			Domain:    domain,
			FetchedAt: time.Now(),
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}
		v.cacheMu.Unlock()
		return nil, nil
	}

	// Cache the policy
	v.cacheMu.Lock()
	v.cache[domain] = &MTASTSCacheEntry{
		Policy:    policy,
		Domain:    domain,
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Duration(policy.MaxAge) * time.Second),
	}
	v.cacheMu.Unlock()

	return policy, nil
}

// fetchPolicy fetches the MTA-STS policy for a domain
func (v *MTASTSValidator) fetchPolicy(ctx context.Context, domain string) (*MTASTSPolicy, error) {
	// Step 1: Check for MTA-STS TXT record
	record, err := v.lookupMTASTSRecord(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup failed: %w", err)
	}

	if record == nil {
		return nil, nil // No MTA-STS record
	}

	if record.Version != "STSv1" {
		return nil, errors.New("unsupported MTA-STS version")
	}

	// Step 2: Fetch policy file via HTTPS
	policy, err := v.fetchPolicyFile(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch policy: %w", err)
	}

	// Step 3: Validate policy ID matches
	policyID := computePolicyID(policy.Raw)
	if policyID != record.ID {
		return nil, errors.New("policy ID mismatch")
	}

	return policy, nil
}

// lookupMTASTSRecord looks up the MTA-STS TXT record
func (v *MTASTSValidator) lookupMTASTSRecord(ctx context.Context, domain string) (*MTASTSRecord, error) {
	// Query: _mta-sts.domain
	query := "_mta-sts." + domain

	txtRecords, err := v.resolver.LookupTXT(ctx, query)
	if err != nil {
		if isTemporaryError(err) {
			return nil, err
		}
		return nil, nil // No record found
	}

	for _, record := range txtRecords {
		mtastsRecord, err := parseMTASTSRecord(record)
		if err == nil && mtastsRecord != nil {
			return mtastsRecord, nil
		}
	}

	return nil, nil
}

// parseMTASTSRecord parses an MTA-STS TXT record
func parseMTASTSRecord(record string) (*MTASTSRecord, error) {
	// Format: v=STSv1; id=base64hash
	params := parseTagValueList(record)

	version := params["v"]
	if version != "STSv1" {
		return nil, errors.New("invalid MTA-STS version")
	}

	id := params["id"]
	if id == "" {
		return nil, errors.New("missing policy ID")
	}

	return &MTASTSRecord{
		Version: version,
		ID:      id,
	}, nil
}

// fetchPolicyFile fetches the MTA-STS policy file via HTTPS
func (v *MTASTSValidator) fetchPolicyFile(ctx context.Context, domain string) (*MTASTSPolicy, error) {
	// URL: https://mta-sts.domain/.well-known/mta-sts.txt
	url := fmt.Sprintf("https://mta-sts.%s/.well-known/mta-sts.txt", domain)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read policy with size limit (64KB max)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}

	return parseMTASTSPolicy(string(body))
}

// parseMTASTSPolicy parses an MTA-STS policy file
func parseMTASTSPolicy(policyText string) (*MTASTSPolicy, error) {
	policy := &MTASTSPolicy{
		Raw: policyText,
		MX:  []string{},
	}

	lines := strings.Split(policyText, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key: value pairs
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.ToLower(strings.TrimSpace(line[:idx]))
			value := strings.TrimSpace(line[idx+1:])

			switch key {
			case "version":
				policy.Version = value
			case "mode":
				policy.Mode = MTASTSMode(strings.ToLower(value))
			case "max_age":
				maxAge, err := strconv.Atoi(value)
				if err == nil {
					policy.MaxAge = maxAge
				}
			case "mx":
				policy.MX = append(policy.MX, value)
			}
		}
	}

	// Validate required fields
	if policy.Version != "STSv1" {
		return nil, errors.New("invalid policy version")
	}

	// Validate mode
	if policy.Mode != MTASTSModeEnforce &&
		policy.Mode != MTASTSModeTesting &&
		policy.Mode != MTASTSModeNone {
		return nil, errors.New("invalid policy mode")
	}

	// Validate max_age (must be at least 86400 seconds)
	if policy.MaxAge < 86400 {
		policy.MaxAge = 86400 // Minimum
	}
	if policy.MaxAge > 31557600 {
		policy.MaxAge = 31557600 // Maximum (~1 year)
	}

	return policy, nil
}

// matchMX checks if an MX hostname matches an MTA-STS MX pattern
func matchMX(pattern, mx string) bool {
	// Convert to lowercase for comparison
	pattern = strings.ToLower(pattern)
	mx = strings.ToLower(mx)

	// Handle exact match
	if pattern == mx {
		return true
	}

	// Handle wildcard patterns (*.example.com)
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[2:] // Remove "*."
		return strings.HasSuffix(mx, "."+suffix) || mx == suffix
	}

	return false
}

// computePolicyID computes the SHA-256 hash of the policy for ID validation
func computePolicyID(policy string) string {
	hash := sha256.Sum256([]byte(policy))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// IsMTASTSEnforced checks if MTA-STS is in enforce mode for a domain
func (v *MTASTSValidator) IsMTASTSEnforced(ctx context.Context, domain string) (bool, error) {
	policy, err := v.GetPolicy(ctx, domain)
	if err != nil {
		return false, err
	}

	if policy == nil {
		return false, nil
	}

	return policy.Mode == MTASTSModeEnforce, nil
}

// GetCacheStats returns statistics about the policy cache
func (v *MTASTSValidator) GetCacheStats() (total, expired int) {
	v.cacheMu.RLock()
	defer v.cacheMu.RUnlock()

	now := time.Now()
	for _, entry := range v.cache {
		total++
		if now.After(entry.ExpiresAt) {
			expired++
		}
	}
	return total, expired
}

// ClearCache clears all cached policies
func (v *MTASTSValidator) ClearCache() {
	v.cacheMu.Lock()
	v.cache = make(map[string]*MTASTSCacheEntry)
	v.cacheMu.Unlock()
}

// MTASTSReport represents an MTA-STS TLSRPT report
type MTASTSReport struct {
	OrganizationName string               `json:"organization-name"`
	DateRange        MTASTSDateRange      `json:"date-range"`
	ContactInfo      string               `json:"contact-info"`
	ReportID         string               `json:"report-id"`
	Policies         []MTASTSPolicyReport `json:"policies"`
}

// MTASTSDateRange represents the date range for a report
type MTASTSDateRange struct {
	StartDate int64 `json:"start-date"`
	EndDate   int64 `json:"end-date"`
}

// MTASTSPolicyReport represents policy-specific report data
type MTASTSPolicyReport struct {
	Policy         MTASTSPolicyDetails    `json:"policy"`
	Summary        MTASTSSummary          `json:"summary"`
	FailureDetails []MTASTSFailureDetails `json:"failure-details"`
}

// MTASTSPolicyDetails represents policy details in a report
type MTASTSPolicyDetails struct {
	PolicyType   string   `json:"policy-type"`
	PolicyString string   `json:"policy-string,omitempty"`
	PolicyDomain string   `json:"policy-domain"`
	MXHostnames  []string `json:"mx-hostnames,omitempty"`
}

// MTASTSSummary represents summary statistics
type MTASTSSummary struct {
	TotalSuccessfulSessionCount int `json:"total-successful-session-count"`
	TotalFailureSessionCount    int `json:"total-failure-session-count"`
}

// MTASTSFailureDetails represents detailed failure information
type MTASTSFailureDetails struct {
	ResultType            string `json:"result-type"`
	SendingMTAIP          string `json:"sending-mta-ip,omitempty"`
	ReceivingMXHelo       string `json:"receiving-mx-helo,omitempty"`
	ReceivingMXAlias      string `json:"receiving-mx-alias,omitempty"`
	ReceivingIPAddress    string `json:"receiving-ip-address,omitempty"`
	FailedSessionCount    int    `json:"failed-session-count"`
	AdditionalInformation string `json:"additional-information,omitempty"`
}

// GenerateTLSRPT generates a TLSRPT report for MTA-STS
func GenerateTLSRPT(domain string, failures []MTASTSFailureDetails) string {
	report := MTASTSReport{
		OrganizationName: "uMailServer",
		DateRange: MTASTSDateRange{
			StartDate: time.Now().Add(-24 * time.Hour).Unix(),
			EndDate:   time.Now().Unix(),
		},
		ContactInfo: "postmaster@" + domain,
		ReportID:    fmt.Sprintf("%s-%d", domain, time.Now().Unix()),
		Policies: []MTASTSPolicyReport{
			{
				Policy: MTASTSPolicyDetails{
					PolicyType:   "sts",
					PolicyDomain: domain,
				},
				Summary: MTASTSSummary{
					TotalSuccessfulSessionCount: 0, // Would be populated from actual data
					TotalFailureSessionCount:    len(failures),
				},
				FailureDetails: failures,
			},
		},
	}

	jsonData, _ := json.MarshalIndent(report, "", "  ")
	return string(jsonData)
}
