package auth

import (
	"encoding/xml"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// DMARCReport represents a DMARC aggregate report
type DMARCReport struct {
	XMLName    xml.Name        `xml:"feedback"`
	Version    string          `xml:"version,attr"`
	ReportMeta ReportMeta      `xml:"report_metadata"`
	PolicyPUB  PolicyPublished `xml:"policy_published"`
	Records    []Record        `xml:"record"`
}

// ReportMeta contains metadata about the report
type ReportMeta struct {
	OrgName      string    `xml:"org_name"`
	Email        string    `xml:"email"`
	ExtraContact string    `xml:"extra_contact,omitempty"`
	ReportID     string    `xml:"report_id"`
	DateRange    DateRange `xml:"date_range"`
	Errors       []string  `xml:"errors,omitempty"`
}

// DateRange specifies the time period covered by the report
type DateRange struct {
	Begin int64 `xml:"begin"`
	End   int64 `xml:"end"`
}

// PolicyPublished describes the DMARC policy
type PolicyPublished struct {
	Domain    string `xml:"domain"`
	Adkim     string `xml:"adkim"`
	Aspf      string `xml:"aspf"`
	Policy    string `xml:"p"`
	Subdomain string `xml:"sp,omitempty"`
	Pct       int    `xml:"pct"`
}

// Record represents a single message record in the report
type Record struct {
	Row         Row         `xml:"row"`
	Identifiers Identifiers `xml:"identifiers"`
	AuthResult  AuthResult  `xml:"auth_results"`
}

// Row contains source IP and message counts
type Row struct {
	SourceIP       string `xml:"source_ip"`
	Count          int    `xml:"count"`
	PolicyOverride string `xml:"policy_evaluated>policy_override,omitempty"`
	Disposition    string `xml:"policy_evaluated>disposition,omitempty"`
	SPF            string `xml:"policy_evaluated>spf,omitempty"`
	DKIM           string `xml:"policy_evaluated>dkim,omitempty"`
}

// Identifiers contains message identifiers
type Identifiers struct {
	HeaderFrom   string `xml:"header_from"`
	EnvelopeFrom string `xml:"envelope_from,omitempty"`
}

// AuthResult contains authentication results
type AuthResult struct {
	SPF  []SPFResultEntry  `xml:"spf"`
	DKIM []DKIMResultEntry `xml:"dkim"`
}

// SPFResultEntry represents an SPF authentication result
type SPFResultEntry struct {
	Domain string `xml:"domain,attr"`
	Result string `xml:"result,attr"`
}

// DKIMResultEntry represents a DKIM authentication result
type DKIMResultEntry struct {
	Domain string `xml:"domain,attr"`
	Result string `xml:"result,attr"`
}

// ReportEntry tracks message authentication results for a source IP
type ReportEntry struct {
	SourceIP     string
	Count        int
	Dispositions []string // none/quarantine/reject
	SPFResults   []string
	DKIMResults  []string
	EnvelopeFrom string
	HeaderFrom   string
}

// BuildReport creates a DMARC aggregate report XML
func BuildReport(orgName, email, domain, adkim, aspf string, policy DMARCPolicy, sp DMARCPolicy, pct int, begin, end time.Time, entries []ReportEntry) ([]byte, error) {
	reportID := fmt.Sprintf("R%d-%s", begin.Unix(), strings.ReplaceAll(domain, ".", "_"))

	report := DMARCReport{
		Version: "1.0",
		ReportMeta: ReportMeta{
			OrgName:   orgName,
			Email:     email,
			ReportID:  reportID,
			DateRange: DateRange{Begin: begin.Unix(), End: end.Unix()},
		},
		PolicyPUB: PolicyPublished{
			Domain:    domain,
			Adkim:     adkim,
			Aspf:      aspf,
			Policy:    string(policy),
			Subdomain: string(sp),
			Pct:       pct,
		},
		Records: make([]Record, 0, len(entries)),
	}

	for _, entry := range entries {
		record := Record{
			Row: Row{
				SourceIP: entry.SourceIP,
				Count:    entry.Count,
			},
			Identifiers: Identifiers{
				HeaderFrom:   entry.HeaderFrom,
				EnvelopeFrom: entry.EnvelopeFrom,
			},
			AuthResult: AuthResult{
				SPF:  make([]SPFResultEntry, 0),
				DKIM: make([]DKIMResultEntry, 0),
			},
		}

		// Set disposition from last entry (most common)
		if len(entry.Dispositions) > 0 {
			record.Row.Disposition = entry.Dispositions[len(entry.Dispositions)-1]
		}

		for _, spf := range entry.SPFResults {
			record.AuthResult.SPF = append(record.AuthResult.SPF, SPFResultEntry{
				Domain: entry.EnvelopeFrom,
				Result: spf,
			})
		}

		for _, dkim := range entry.DKIMResults {
			record.AuthResult.DKIM = append(record.AuthResult.DKIM, DKIMResultEntry{
				Domain: entry.HeaderFrom,
				Result: dkim,
			})
		}

		report.Records = append(report.Records, record)
	}

	return xml.MarshalIndent(&report, "", "  ")
}

// ParseRUAEmail parses a mailto: URI and extracts email address
func ParseRUAEmail(uri string) (string, error) {
	uri = strings.TrimSpace(uri)
	if strings.HasPrefix(uri, "mailto:") {
		uri = uri[7:]
	}
	// Extract email from mailto:user@example.com?subject=...
	if idx := strings.Index(uri, "?"); idx > 0 {
		uri = uri[:idx]
	}
	addr, err := mail.ParseAddress(uri)
	if err != nil {
		return "", fmt.Errorf("invalid email address: %w", err)
	}
	return addr.Address, nil
}
