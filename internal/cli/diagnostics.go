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

	// Combine sub-results into the overall result
	result.Protocol = imapResult.Protocol
	result.Cipher = imapResult.Cipher
	result.Version = imapResult.Version
	result.Valid = smtpResult.Valid && imapResult.Valid
	if !smtpResult.Valid || !imapResult.Valid {
		result.Message = "TLS issues detected"
		if !smtpResult.Valid {
			result.Message += fmt.Sprintf(" (SMTP: %s)", smtpResult.Message)
		}
		if !imapResult.Valid {
			result.Message += fmt.Sprintf(" (IMAP: %s)", imapResult.Message)
		}
	} else {
		result.Message = "TLS configuration looks good"
	}

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

// DeliverabilityResult holds the result of a deliverability check
type DeliverabilityResult struct {
	Domain       string               `json:"domain"`
	DNSResults   []DNSCheckResult     `json:"dns_results"`
	RBLResults   []RBLCheckResult     `json:"rbl_results"`
	TLSResult    *TLSCheckResult      `json:"tls_result"`
	SMTPResult   *SMTPCheckResult     `json:"smtp_result"`
	OverallScore string               `json:"overall_score"` // pass, warning, fail
	Issues       []string             `json:"issues"`
	Message      string               `json:"message"`
}

// RBLCheckResult holds RBL check result for a server
type RBLCheckResult struct {
	Server   string `json:"server"`
	Listed   bool   `json:"listed"`
	Code     string `json:"code"`
	Score    string `json:"score"`
	Message  string `json:"message"`
}

// SMTPCheckResult holds SMTP connectivity check result
type SMTPCheckResult struct {
	Reachable    bool   `json:"reachable"`
	STARTTLS     bool   `json:"starttls"`
	AuthSupported bool  `json:"auth_supported"`
	MaxMessageSize int64 `json:"max_message_size"`
	Message      string `json:"message"`
}

// CheckDeliverability runs a comprehensive deliverability audit for a domain
func (d *Diagnostics) CheckDeliverability(domain string) (*DeliverabilityResult, error) {
	fmt.Printf("Running comprehensive deliverability check for: %s\n\n", domain)

	result := &DeliverabilityResult{
		Domain: domain,
		Issues: []string{},
	}

	// 1. DNS checks (SPF, DKIM, DMARC, MX, PTR)
	fmt.Println("[1/4] Checking DNS records...")
	dnsResults, err := d.CheckDNS(domain)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("DNS check failed: %v", err))
	} else {
		result.DNSResults = dnsResults
		for _, r := range dnsResults {
			if r.Status == "fail" {
				result.Issues = append(result.Issues, fmt.Sprintf("DNS [%s]: %s", r.RecordType, r.Message))
			}
		}
	}

	// 2. RBL checks for server's IP
	fmt.Println("[2/4] Checking RBL listings...")
	rblResults, rblIssues := d.checkRBL()
	result.RBLResults = rblResults
	result.Issues = append(result.Issues, rblIssues...)

	// 3. TLS check
	fmt.Println("[3/4] Checking TLS configuration...")
	hostname := ""
	if d.config != nil {
		hostname = d.config.Server.Hostname
	}
	if hostname == "" {
		hostname = domain
	}
	tlsResult, err := d.CheckTLS(hostname)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("TLS check failed: %v", err))
	} else {
		result.TLSResult = tlsResult
		if !tlsResult.Valid {
			result.Issues = append(result.Issues, fmt.Sprintf("TLS: %s", tlsResult.Message))
		}
	}

	// 4. SMTP connectivity check
	fmt.Println("[4/4] Checking SMTP connectivity...")
	smtpResult, smtpIssues := d.checkSMTPConnectivity(hostname)
	result.SMTPResult = smtpResult
	result.Issues = append(result.Issues, smtpIssues...)

	// Compute overall score
	if len(result.Issues) == 0 {
		result.OverallScore = "pass"
		result.Message = "All deliverability checks passed"
	} else {
		failCount := 0
		for _, issue := range result.Issues {
			if strings.Contains(issue, "[fail]") || strings.HasPrefix(issue, "DNS [SPF]") || strings.HasPrefix(issue, "DNS [MX]") {
				failCount++
			}
		}
		if failCount > 0 {
			result.OverallScore = "fail"
			result.Message = fmt.Sprintf("Deliverability issues detected (%d critical)", failCount)
		} else {
			result.OverallScore = "warning"
			result.Message = "Minor issues detected that may affect reputation"
		}
	}

	return result, nil
}

// defaultRBLServers returns the default RBL servers to check
func defaultRBLServers() []string {
	return []string{
		"bl.spamcop.net",
		"b.barracudacentral.org",
		"dnsbl.sorbs.net",
	}
}

// checkRBL checks if the server's IP is listed on RBLs
func (d *Diagnostics) checkRBL() ([]RBLCheckResult, []string) {
	results := []RBLCheckResult{}
	issues := []string{}

	hostname := ""
	if d.config != nil {
		hostname = d.config.Server.Hostname
	}

	if hostname == "" {
		return results, issues
	}

	// Get the server's IP addresses
	ips, err := net.LookupIP(hostname)
	if err != nil || len(ips) == 0 {
		issues = append(issues, fmt.Sprintf("RBL: Could not resolve hostname %s: %v", hostname, err))
		return results, issues
	}

	// Use the first IPv4 address
	var serverIP net.IP
	for _, ip := range ips {
		if ip.To4() != nil {
			serverIP = ip
			break
		}
	}
	if serverIP == nil {
		issues = append(issues, fmt.Sprintf("RBL: No IPv4 address found for %s", hostname))
		return results, issues
	}

	fmt.Printf("      Checking IP: %s\n", serverIP.String())

	// Check each RBL
	servers := defaultRBLServers()
	for _, server := range servers {
		result := RBLCheckResult{Server: server}
		listed, code := d.checkRBLServer(serverIP.String(), server)
		result.Listed = listed
		result.Code = code
		if listed {
			result.Message = fmt.Sprintf("Listed on %s (code: %s)", server, code)
			result.Score = "spam"
			issues = append(issues, fmt.Sprintf("RBL [%s]: %s", server, code))
		} else {
			result.Message = "Not listed"
			result.Score = "clean"
		}
		results = append(results, result)
	}

	return results, issues
}

// checkRBLServer checks if an IP is listed on a specific RBL server
func (d *Diagnostics) checkRBLServer(ip, rblServer string) (bool, string) {
	reversed := reverseIP(ip)
	if reversed == "" {
		return false, ""
	}
	lookupHost := fmt.Sprintf("%s.%s", reversed, rblServer)

	ips, err := net.LookupIP(lookupHost)
	if err != nil {
		return false, ""
	}
	if len(ips) == 0 {
		return false, ""
	}

	ipStr := ips[0].String()
	// RBL result codes - first octet indicates listing type
	if len(ipStr) >= 8 {
		code := fmt.Sprintf("code-%s", ipStr)
		return true, code
	}
	return true, "listed"
}

// reverseIP reverses an IPv4 address for RBL lookups
func reverseIP(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return ""
	}
	return fmt.Sprintf("%s.%s.%s.%s", parts[3], parts[2], parts[1], parts[0])
}

// checkSMTPConnectivity checks if SMTP is reachable and supports STARTTLS
func (d *Diagnostics) checkSMTPConnectivity(hostname string) (*SMTPCheckResult, []string) {
	result := &SMTPCheckResult{}
	issues := []string{}

	// Try connecting to port 25 (MX)
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:25", hostname), 10*time.Second)
	if err != nil {
		result.Message = fmt.Sprintf("SMTP port 25 not reachable: %v", err)
		issues = append(issues, fmt.Sprintf("SMTP: Port 25 unreachable - remote servers may not be able to deliver mail"))
		return result, issues
	}
	defer conn.Close()
	result.Reachable = true

	// Try reading the SMTP greeting
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		result.Message = "Could not read SMTP greeting"
		issues = append(issues, "SMTP: Could not read server greeting")
		return result, issues
	}
	greeting := strings.TrimSpace(string(buf[:n]))
	result.Message = fmt.Sprintf("SMTP reachable, greeting: %s", greeting)

	// Try STARTTLS on port 587
	tlsConfig := &tls.Config{ServerName: hostname}
	starttlsConn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", fmt.Sprintf("%s:587", hostname), tlsConfig)
	if err != nil {
		result.STARTTLS = false
	} else {
		defer starttlsConn.Close()
		result.STARTTLS = true
	}

	return result, issues
}

// PrintDeliverabilityResults prints comprehensive deliverability results
func PrintDeliverabilityResults(r *DeliverabilityResult) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                Deliverability Check Results                  ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Overall status
	color := "\033[32m" // green
	symbol := "✓"
	if r.OverallScore == "fail" {
		color = "\033[31m" // red
		symbol = "✗"
	} else if r.OverallScore == "warning" {
		color = "\033[33m" // yellow
		symbol = "⚠"
	}
	reset := "\033[0m"
	fmt.Printf("%s%s Overall: %s — %s%s\n\n", color, symbol, strings.ToUpper(r.OverallScore), r.Message, reset)

	// DNS results
	fmt.Println("── DNS Records ─────────────────────────────────────────────")
	fmt.Println()
	if len(r.DNSResults) == 0 {
		fmt.Println("  (DNS check not run)")
	} else {
		for _, res := range r.DNSResults {
			sym := "✓"
			scolor := "\033[32m"
			if res.Status == "fail" {
				sym = "✗"
				scolor = "\033[31m"
			} else if res.Status == "warning" {
				sym = "⚠"
				scolor = "\033[33m"
			}
			fmt.Printf("  %s%s%s [%s] %s\n", scolor, sym, reset, res.RecordType, res.Message)
		}
	}
	fmt.Println()

	// RBL results
	fmt.Println("── RBL Listings ────────────────────────────────────────────")
	fmt.Println()
	if len(r.RBLResults) == 0 {
		fmt.Println("  (RBL check not run)")
	} else {
		for _, res := range r.RBLResults {
			sym := "✓"
			scolor := "\033[32m"
			if res.Listed {
				sym = "✗"
				scolor = "\033[31m"
			}
			fmt.Printf("  %s%s%s %s: %s\n", scolor, sym, reset, res.Server, res.Message)
		}
	}
	fmt.Println()

	// TLS result
	fmt.Println("── TLS Configuration ───────────────────────────────────────")
	fmt.Println()
	if r.TLSResult != nil {
		sym := "✓"
		scolor := "\033[32m"
		if !r.TLSResult.Valid {
			sym = "✗"
			scolor = "\033[31m"
		}
		fmt.Printf("  %s%s%s %s\n", scolor, sym, reset, r.TLSResult.Message)
	} else {
		fmt.Println("  (TLS check not run)")
	}
	fmt.Println()

	// SMTP connectivity
	fmt.Println("── SMTP Connectivity ───────────────────────────────────────")
	fmt.Println()
	if r.SMTPResult != nil {
		sym := "✓"
		scolor := "\033[32m"
		if !r.SMTPResult.Reachable {
			sym = "✗"
			scolor = "\033[31m"
		}
		fmt.Printf("  %s%s%s %s\n", scolor, sym, reset, r.SMTPResult.Message)
		if r.SMTPResult.STARTTLS {
			fmt.Printf("  %s✓%s STARTTLS on port 587 available\n", "\033[32m", reset)
		}
	} else {
		fmt.Println("  (SMTP check not run)")
	}
	fmt.Println()

	// Issues
	if len(r.Issues) > 0 {
		fmt.Println("── Issues Found ─────────────────────────────────────────────")
		fmt.Println()
		for _, issue := range r.Issues {
			fmt.Printf("  ⚠ %s\n", issue)
		}
		fmt.Println()
	}

	fmt.Println("──────────────────────────────────────────────────────────────")
	if r.OverallScore == "pass" {
		fmt.Println("✓ Domain is properly configured for email deliverability")
	} else if r.OverallScore == "warning" {
		fmt.Println("⚠ Domain has some configuration issues - see above for details")
	} else {
		fmt.Println("✗ Domain has critical deliverability issues that must be fixed")
	}
	fmt.Println()
}
