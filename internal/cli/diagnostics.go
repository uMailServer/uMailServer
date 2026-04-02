package cli

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

// Diagnostics runs various diagnostic checks
type Diagnostics struct {
	config    *config.Config
	tlsConfig *tls.Config // optional override for TLS verification (used by tests)
}

// NewDiagnostics creates new diagnostics
func NewDiagnostics(cfg *config.Config) *Diagnostics {
	return &Diagnostics{
		config: cfg,
	}
}

// DNSCheckResult holds DNS check results
type DNSCheckResult struct {
	RecordType string `json:"record_type"`
	RecordName string `json:"record_name"`
	Expected   string `json:"expected"`
	Found      string `json:"found"`
	Status     string `json:"status"` // pass, fail, warning
	Message    string `json:"message"`
}

// TLSCheckResult holds TLS check results
type TLSCheckResult struct {
	Protocol string `json:"protocol"`
	Cipher   string `json:"cipher"`
	Version  string `json:"version"`
	Valid    bool   `json:"valid"`
	Expiry   string `json:"expiry"`
	Message  string `json:"message"`
}

// CheckDNS checks DNS records for a domain
func (d *Diagnostics) CheckDNS(domain string) ([]DNSCheckResult, error) {
	fmt.Printf("Checking DNS records for: %s\n\n", domain)

	results := []DNSCheckResult{}

	// Check MX record
	mxResults, err := d.checkMX(domain)
	if err != nil {
		return nil, err
	}
	results = append(results, mxResults...)

	// Check SPF record
	spfResult := d.checkSPF(domain)
	results = append(results, spfResult)

	// Check DKIM record
	dkimResult := d.checkDKIM(domain)
	results = append(results, dkimResult)

	// Check DMARC record
	dmarcResult := d.checkDMARC(domain)
	results = append(results, dmarcResult)

	// Check PTR record
	ptrResult := d.checkPTR(domain)
	results = append(results, ptrResult)

	return results, nil
}

// checkMX checks MX records
func (d *Diagnostics) checkMX(domain string) ([]DNSCheckResult, error) {
	results := []DNSCheckResult{}

	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		results = append(results, DNSCheckResult{
			RecordType: "MX",
			RecordName: domain,
			Status:     "fail",
			Message:    fmt.Sprintf("No MX records found: %v", err),
		})
		return results, nil
	}

	if len(mxRecords) == 0 {
		results = append(results, DNSCheckResult{
			RecordType: "MX",
			RecordName: domain,
			Status:     "fail",
			Message:    "No MX records found",
		})
		return results, nil
	}

	// Check the primary MX record
	primaryMX := mxRecords[0]
	expectedHost := ""
	if d.config != nil {
		expectedHost = d.config.Server.Hostname
	}

	if strings.EqualFold(primaryMX.Host, expectedHost) {
		results = append(results, DNSCheckResult{
			RecordType: "MX",
			RecordName: domain,
			Expected:   expectedHost,
			Found:      primaryMX.Host,
			Status:     "pass",
			Message:    fmt.Sprintf("MX record points to this server (priority: %d)", primaryMX.Pref),
		})
	} else {
		results = append(results, DNSCheckResult{
			RecordType: "MX",
			RecordName: domain,
			Expected:   expectedHost,
			Found:      primaryMX.Host,
			Status:     "warning",
			Message:    fmt.Sprintf("MX record points to different host: %s", primaryMX.Host),
		})
	}

	return results, nil
}

// checkSPF checks SPF record
func (d *Diagnostics) checkSPF(domain string) DNSCheckResult {
	txtRecords, err := net.LookupTXT(domain)
	if err != nil {
		return DNSCheckResult{
			RecordType: "SPF",
			RecordName: domain,
			Status:     "fail",
			Message:    fmt.Sprintf("Failed to lookup TXT records: %v", err),
		}
	}

	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=spf1") {
			// Check if it includes our server
			hostname := ""
			if d.config != nil {
				hostname = d.config.Server.Hostname
			}
			expected := fmt.Sprintf("v=spf1 mx a:%s -all", hostname)

			if strings.Contains(txt, hostname) || strings.Contains(txt, "mx") {
				return DNSCheckResult{
					RecordType: "SPF",
					RecordName: domain,
					Expected:   expected,
					Found:      txt,
					Status:     "pass",
					Message:    "SPF record found and configured",
				}
			}
		}
	}

	return DNSCheckResult{
		RecordType: "SPF",
		RecordName: domain,
		Status:     "fail",
		Message:    "No SPF record found",
	}
}

// checkDKIM checks DKIM record
func (d *Diagnostics) checkDKIM(domain string) DNSCheckResult {
	// Try to lookup default DKIM selector
	selector := "default._domainkey." + domain
	txtRecords, err := net.LookupTXT(selector)
	if err != nil {
		return DNSCheckResult{
			RecordType: "DKIM",
			RecordName: selector,
			Status:     "warning",
			Message:    "DKIM record not found (optional but recommended)",
		}
	}

	for _, txt := range txtRecords {
		if strings.Contains(txt, "v=DKIM1") {
			return DNSCheckResult{
				RecordType: "DKIM",
				RecordName: selector,
				Found:      txt[:min(len(txt), 50)] + "...",
				Status:     "pass",
				Message:    "DKIM record found",
			}
		}
	}

	return DNSCheckResult{
		RecordType: "DKIM",
		RecordName: selector,
		Status:     "warning",
		Message:    "DKIM record not found (optional but recommended)",
	}
}

// checkDMARC checks DMARC record
func (d *Diagnostics) checkDMARC(domain string) DNSCheckResult {
	record := "_dmarc." + domain
	txtRecords, err := net.LookupTXT(record)
	if err != nil {
		return DNSCheckResult{
			RecordType: "DMARC",
			RecordName: record,
			Status:     "warning",
			Message:    "DMARC record not found (optional but recommended)",
		}
	}

	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=DMARC1") {
			return DNSCheckResult{
				RecordType: "DMARC",
				RecordName: record,
				Found:      txt,
				Status:     "pass",
				Message:    "DMARC record found",
			}
		}
	}

	return DNSCheckResult{
		RecordType: "DMARC",
		RecordName: record,
		Status:     "warning",
		Message:    "DMARC record not found (optional but recommended)",
	}
}

// checkPTR checks reverse DNS (PTR) record
func (d *Diagnostics) checkPTR(domain string) DNSCheckResult {
	// Check if config is nil
	if d.config == nil {
		return DNSCheckResult{
			RecordType: "PTR",
			RecordName: domain,
			Status:     "warning",
			Message:    "Cannot check PTR: no configuration available",
		}
	}

	// Get our IP address
	hostname := d.config.Server.Hostname
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return DNSCheckResult{
			RecordType: "PTR",
			RecordName: hostname,
			Status:     "warning",
			Message:    fmt.Sprintf("Cannot lookup IP for hostname: %v", err),
		}
	}

	if len(ips) == 0 {
		return DNSCheckResult{
			RecordType: "PTR",
			RecordName: hostname,
			Status:     "warning",
			Message:    "No IP addresses found for hostname",
		}
	}

	// Check PTR for the first IP
	ip := ips[0]
	names, err := net.LookupAddr(ip.String())
	if err != nil {
		return DNSCheckResult{
			RecordType: "PTR",
			RecordName: ip.String(),
			Status:     "warning",
			Message:    fmt.Sprintf("No PTR record found: %v", err),
		}
	}

	for _, name := range names {
		if strings.EqualFold(strings.TrimSuffix(name, "."), hostname) {
			return DNSCheckResult{
				RecordType: "PTR",
				RecordName: ip.String(),
				Found:      name,
				Status:     "pass",
				Message:    "PTR record matches hostname",
			}
		}
	}

	return DNSCheckResult{
		RecordType: "PTR",
		RecordName: ip.String(),
		Found:      strings.Join(names, ", "),
		Expected:   hostname,
		Status:     "warning",
		Message:    "PTR record does not match hostname",
	}
}

// CheckTLS checks TLS configuration
func (d *Diagnostics) CheckTLS(hostname string) (*TLSCheckResult, error) {
	fmt.Printf("Checking TLS configuration for: %s\n\n", hostname)

	result := &TLSCheckResult{}

	// Test SMTP TLS
	fmt.Println("Testing SMTP TLS...")
	smtpResult, err := d.checkSMTPTLS(hostname)
	if err != nil {
		result.Message = fmt.Sprintf("SMTP TLS check failed: %v", err)
		return result, nil
	}

	// Test IMAP TLS
	fmt.Println("Testing IMAP TLS...")
	imapResult, err := d.checkIMAPTLS(hostname)
	if err != nil {
		result.Message = fmt.Sprintf("IMAP TLS check failed: %v", err)
		return result, nil
	}

	_ = smtpResult
	_ = imapResult

	result.Valid = true
	result.Message = "TLS configuration looks good"

	return result, nil
}

// checkSMTPTLS checks SMTP TLS
func (d *Diagnostics) checkSMTPTLS(hostname string) (*TLSCheckResult, error) {
	// Connect to SMTP server
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:587", hostname), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SMTP: %w", err)
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, hostname)
	if err != nil {
		return nil, fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// Try STARTTLS
	tlsConfig := d.tlsConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			ServerName: hostname,
		}
	} else {
		// Clone and set ServerName
		tlsConfig = tlsConfig.Clone()
		tlsConfig.ServerName = hostname
	}

	if err := client.StartTLS(tlsConfig); err != nil {
		return nil, fmt.Errorf("STARTTLS failed: %w", err)
	}

	return &TLSCheckResult{
		Protocol: "SMTP",
		Version:  "TLS 1.2+",
		Valid:    true,
		Message:  "SMTP STARTTLS is working",
	}, nil
}

// checkIMAPTLS checks IMAP TLS
func (d *Diagnostics) checkIMAPTLS(hostname string) (*TLSCheckResult, error) {
	// Connect to IMAP server
	tlsConfig := d.tlsConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{
			ServerName: hostname,
		}
	} else {
		tlsConfig = tlsConfig.Clone()
		tlsConfig.ServerName = hostname
	}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", fmt.Sprintf("%s:993", hostname), tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to IMAPS: %w", err)
	}
	defer conn.Close()

	state := conn.ConnectionState()

	return &TLSCheckResult{
		Protocol: "IMAP",
		Version:  tlsVersionName(state.Version),
		Cipher:   fmt.Sprintf("%x", state.CipherSuite),
		Valid:    true,
		Message:  "IMAPS is working",
	}, nil
}

// PrintDNSResults prints DNS check results
func PrintDNSResults(results []DNSCheckResult) {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    DNS Configuration Check                   ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	passed := 0
	failed := 0
	warnings := 0

	for _, r := range results {
		symbol := "✓"
		color := "\033[32m" // Green
		if r.Status == "fail" {
			symbol = "✗"
			color = "\033[31m" // Red
			failed++
		} else if r.Status == "warning" {
			symbol = "⚠"
			color = "\033[33m" // Yellow
			warnings++
		} else {
			passed++
		}

		reset := "\033[0m"
		fmt.Printf("%s%s%s [%s] %s: %s\n", color, symbol, reset, r.Status, r.RecordType, r.Message)
		if r.Expected != "" {
			fmt.Printf("       Expected: %s\n", r.Expected)
		}
		if r.Found != "" {
			fmt.Printf("       Found:    %s\n", r.Found)
		}
		fmt.Println()
	}

	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Printf("Results: %d passed, %d failed, %d warnings\n", passed, failed, warnings)
	fmt.Println()

	if failed > 0 {
		fmt.Println("⚠  Fix failed checks for proper mail server operation")
	} else if warnings > 0 {
		fmt.Println("ℹ  Consider addressing warnings for best practices")
	} else {
		fmt.Println("✓  All DNS checks passed!")
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// tlsVersionName returns human-readable TLS version name
func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionSSL30:
		return "SSL 3.0"
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (0x%x)", version)
	}
}
