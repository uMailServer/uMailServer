package auth

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DMARCReport represents a DMARC aggregate report
type DMARCReport struct {
	XMLName    xml.Name        `xml:"feedback"`
	Version    string          `xml:"version"`
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
	Disposition    string `xml:"policy_evaluated>disposition,omitempty"`
	DKIM           string `xml:"policy_evaluated>dkim,omitempty"`
	SPF            string `xml:"policy_evaluated>spf,omitempty"`
	PolicyOverride string `xml:"policy_evaluated>reason>type,omitempty"`
}

// Identifiers contains message identifiers
type Identifiers struct {
	EnvelopeFrom string `xml:"envelope_from,omitempty"`
	HeaderFrom   string `xml:"header_from"`
}

// AuthResult contains authentication results
type AuthResult struct {
	DKIM []DKIMResultEntry `xml:"dkim"`
	SPF  []SPFResultEntry  `xml:"spf"`
}

// SPFResultEntry represents an SPF authentication result
type SPFResultEntry struct {
	Domain string `xml:"domain"`
	Result string `xml:"result"`
}

// DKIMResultEntry represents a DKIM authentication result
type DKIMResultEntry struct {
	Domain string `xml:"domain"`
	Result string `xml:"result"`
}

// ReportRow is one aggregate report row: the messages from one source IP
// that share identifiers and results.
type ReportRow struct {
	SourceIP     string
	HeaderFrom   string
	EnvelopeFrom string // SPF-checked domain (MAIL FROM, or HELO)
	Disposition  string // none/quarantine/reject
	DKIMEval     string // policy_evaluated dkim: pass/fail
	SPFEval      string // policy_evaluated spf: pass/fail
	DKIMDomain   string // d= of the evaluated signature; empty when unsigned
	DKIMResult   string
	SPFResult    string
}

// ReportInput is everything one aggregate report covers.
type ReportInput struct {
	OrgName  string // reporting organization
	Email    string // reporting organization's contact address
	ReportID string
	Domain   string // policy domain
	Record   DMARCRecord
	Begin    time.Time
	End      time.Time
	Rows     map[ReportRow]int // row -> message count
}

// BuildReport renders a DMARC aggregate report (RFC 7489 Appendix C).
func BuildReport(in ReportInput) ([]byte, error) {
	adkim, aspf := string(in.Record.AlignmentDKIM), string(in.Record.AlignmentSPF)
	if adkim == "" {
		adkim = string(DMARCAlignmentRelaxed)
	}
	if aspf == "" {
		aspf = string(DMARCAlignmentRelaxed)
	}
	report := DMARCReport{
		Version: "1.0",
		ReportMeta: ReportMeta{
			OrgName:   in.OrgName,
			Email:     in.Email,
			ReportID:  in.ReportID,
			DateRange: DateRange{Begin: in.Begin.Unix(), End: in.End.Unix()},
		},
		PolicyPUB: PolicyPublished{
			Domain:    in.Domain,
			Adkim:     adkim,
			Aspf:      aspf,
			Policy:    string(in.Record.Policy),
			Subdomain: string(in.Record.SubdomainPolicy),
			Pct:       in.Record.Percentage,
		},
		Records: make([]Record, 0, len(in.Rows)),
	}

	rows := make([]ReportRow, 0, len(in.Rows))
	for row := range in.Rows {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return fmt.Sprint(rows[i]) < fmt.Sprint(rows[j]) })
	for _, row := range rows {
		rec := Record{
			Row: Row{
				SourceIP:    row.SourceIP,
				Count:       in.Rows[row],
				Disposition: row.Disposition,
				DKIM:        row.DKIMEval,
				SPF:         row.SPFEval,
			},
			Identifiers: Identifiers{
				HeaderFrom:   row.HeaderFrom,
				EnvelopeFrom: row.EnvelopeFrom,
			},
			AuthResult: AuthResult{
				SPF: []SPFResultEntry{{Domain: row.EnvelopeFrom, Result: row.SPFResult}},
			},
		}
		if row.DKIMDomain != "" {
			rec.AuthResult.DKIM = []DKIMResultEntry{{Domain: row.DKIMDomain, Result: row.DKIMResult}}
		}
		report.Records = append(report.Records, rec)
	}

	out, err := xml.MarshalIndent(&report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}

// BuildReportMessage wraps a report in the RFC 7489 §7.2.1.1 mail format: a
// multipart message whose attachment is the gzip-compressed XML, named
// receiver!policy-domain!begin!end.xml.gz.
func BuildReportMessage(from, to, receiver string, in ReportInput, xmlData []byte, now time.Time) ([]byte, error) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(xmlData); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}

	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, err
	}
	boundary := "dmarc-" + hex.EncodeToString(rnd[:])
	filename := fmt.Sprintf("%s!%s!%d!%d.xml.gz", receiver, in.Domain, in.Begin.Unix(), in.End.Unix())
	fromDomain := receiver
	if at := strings.LastIndex(from, "@"); at >= 0 {
		fromDomain = from[at+1:]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: Report Domain: %s Submitter: %s Report-ID: <%s>\r\n", in.Domain, receiver, in.ReportID)
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(rnd[:]), fromDomain)
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	fmt.Fprintf(&b, "This is a DMARC aggregate report for %s from %s.\r\n\r\n", in.Domain, receiver)
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: application/gzip\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&b, "Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", filename)
	enc := base64.StdEncoding.EncodeToString(gz.Bytes())
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String()), nil
}

// ParseRUAEmail parses a mailto: URI and extracts email address. The scheme is
// case-insensitive and an RFC 7489 §6.2 "!size" limit suffix is ignored.
func ParseRUAEmail(uri string) (string, error) {
	uri = strings.TrimSpace(uri)
	if len(uri) >= len("mailto:") && strings.EqualFold(uri[:len("mailto:")], "mailto:") {
		uri = uri[len("mailto:"):]
	}
	// Extract email from mailto:user@example.com?subject=...
	if idx := strings.Index(uri, "?"); idx > 0 {
		uri = uri[:idx]
	}
	if idx := strings.LastIndex(uri, "!"); idx > 0 {
		switch strings.ToLower(uri[idx+1:]) {
		case "", "k", "m", "g", "t":
			uri = uri[:idx]
		default:
			if _, err := strconv.Atoi(strings.TrimRight(strings.ToLower(uri[idx+1:]), "kmgt")); err == nil {
				uri = uri[:idx]
			}
		}
	}
	addr, err := mail.ParseAddress(uri)
	if err != nil {
		return "", fmt.Errorf("invalid email address: %w", err)
	}
	return addr.Address, nil
}

// VerifyReportDestination implements RFC 7489 §7.1 external destination
// verification: a report for domain may go to an address in another
// organizational domain only if <domain>._report._dmarc.<dest-domain>
// publishes a v=DMARC1 TXT record.
func VerifyReportDestination(ctx context.Context, resolver DNSResolver, domain, destEmail string) (bool, error) {
	at := strings.LastIndex(destEmail, "@")
	if at < 0 || at == len(destEmail)-1 {
		return false, fmt.Errorf("invalid destination %q", destEmail)
	}
	destDomain := strings.ToLower(strings.TrimSuffix(destEmail[at+1:], "."))
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if isOrganizationalDomainMatch(domain, destDomain) {
		return true, nil
	}
	if resolver == nil {
		return false, errors.New("no resolver for external destination verification")
	}
	txts, err := resolver.LookupTXT(ctx, domain+"._report._dmarc."+destDomain)
	if err != nil {
		return false, err
	}
	for _, t := range txts {
		if isDMARCRecord(t) {
			return true, nil
		}
	}
	return false, errors.New("no authorization record")
}
