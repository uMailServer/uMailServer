package auth

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// DMARCResult represents the result of DMARC evaluation
type DMARCResult int

const (
	DMARCNone      DMARCResult = iota // No DMARC record
	DMARCPass                        // DMARC check passed
	DMARCFail                        // DMARC check failed
	DMARCPermError                   // Permanent error
	DMARCTempError                   // Temporary error
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
	}
}

// Evaluate evaluates DMARC for the given message
func (e *DMARCEvaluator) Evaluate(ctx context.Context, fromDomain string, spfResult SPFResult, spfDomain string, dkimResult DKIMResult, dkimDomain string) (*DMARCEvaluation, error) {
	// Look up DMARC record
	record, err := e.lookupDMARC(ctx, fromDomain)
	if err != nil {
		if isTemporaryError(err) {
			return &DMARCEvaluation{
				Result:      DMARCTempError,
				Domain:      fromDomain,
				Explanation: "DNS lookup failed",
			}, nil
		}
		// No DMARC record found
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

		// Check if this is a subdomain and subdomain policy is set
		if record.SubdomainPolicy != "" && isSubdomain(fromDomain) {
			policyToApply = record.SubdomainPolicy
		}

		// Apply percentage sampling
		if record.Percentage < 100 {
			if !shouldApplyPolicy(record.Percentage) {
				// Skip enforcement for this message
				policyToApply = DMARCPolicyNone
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

	for _, record := range txtRecords {
		if strings.HasPrefix(record, "v=DMARC1") {
			return parseDMARCRecord(record)
		}
	}

	return nil, errors.New("no DMARC record found")
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

	if rec.Policy == "" {
		return nil, errors.New("missing required policy (p=)")
	}

	// Validate policy values
	if rec.Policy != DMARCPolicyNone && rec.Policy != DMARCPolicyQuarantine && rec.Policy != DMARCPolicyReject {
		return nil, errors.New("invalid policy value")
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
// This is a simplified implementation - in production, this would use the Public Suffix List
func isOrganizationalDomainMatch(domain1, domain2 string) bool {
	domain1 = strings.ToLower(domain1)
	domain2 = strings.ToLower(domain2)

	// Exact match
	if domain1 == domain2 {
		return true
	}

	// Check if one is a subdomain of the other
	if strings.HasSuffix(domain1, "."+domain2) {
		return true
	}
	if strings.HasSuffix(domain2, "."+domain1) {
		return true
	}

	// For subdomains of the same parent, check shared organizational domain
	// This is a simplified check - real implementation would use PSL
	parts1 := strings.Split(domain1, ".")
	parts2 := strings.Split(domain2, ".")

	// Need at least 2 parts for an organizational domain
	if len(parts1) < 2 || len(parts2) < 2 {
		return false
	}

	// Get the last two parts (organizational domain)
	org1 := parts1[len(parts1)-2] + "." + parts1[len(parts1)-1]
	org2 := parts2[len(parts2)-2] + "." + parts2[len(parts2)-1]

	return org1 == org2
}

// isSubdomain checks if the domain is a subdomain (has more than 2 labels)
func isSubdomain(domain string) bool {
	parts := strings.Split(domain, ".")
	return len(parts) > 2
}

// shouldApplyPolicy determines if DMARC policy should be applied based on percentage
func shouldApplyPolicy(percentage int) bool {
	if percentage >= 100 {
		return true
	}
	if percentage <= 0 {
		return false
	}
	// Random sampling
	return rand.Intn(100) < percentage
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

// GenerateDMARCReport generates a DMARC aggregate report.
// TODO: Wire into a DMARC aggregate report scheduler (RFC 7489) that sends reports to rua=mailto:dmarc@domain.
func GenerateDMARCReport(domain string, records []DMARCReportRecord) string {
	// This is a simplified report generator
	// In production, this would generate XML reports per RFC 7489

	report := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n")
	report += fmt.Sprintf("<feedback>\r\n")
	report += fmt.Sprintf("  <report_metadata>\r\n")
	report += fmt.Sprintf("    <org_name>uMailServer</org_name>\r\n")
	report += fmt.Sprintf("    <email>postmaster@%s</email>\r\n", domain)
	report += fmt.Sprintf("    <report_id>%d</report_id>\r\n", time.Now().Unix())
	report += fmt.Sprintf("    <date_range>\r\n")
	report += fmt.Sprintf("      <begin>%d</begin>\r\n", time.Now().Add(-24*time.Hour).Unix())
	report += fmt.Sprintf("      <end>%d</end>\r\n", time.Now().Unix())
	report += fmt.Sprintf("    </date_range>\r\n")
	report += fmt.Sprintf("  </report_metadata>\r\n")
	report += fmt.Sprintf("  <policy_published>\r\n")
	report += fmt.Sprintf("    <domain>%s</domain>\r\n", domain)
	report += fmt.Sprintf("  </policy_published>\r\n")
	report += fmt.Sprintf("</feedback>\r\n")

	return report
}

// DMARCReportRecord represents a single record in a DMARC report
type DMARCReportRecord struct {
	SourceIP   string
	Count      int
	PolicyEval string
	Disposition string
	SPFResult  string
	DKIMResult string
	HeaderFrom string
}
