package spam

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// RBLChecker checks IP addresses against DNS blacklists
type RBLChecker struct {
	servers []string
	timeout time.Duration
}

// RBLResult holds the result of an RBL check
type RBLResult struct {
	Listed    bool
	Server    string
	Reason    string
	Response  string
	Error     error
}

// NewRBLChecker creates a new RBL checker
func NewRBLChecker(servers []string) *RBLChecker {
	return &RBLChecker{
		servers: servers,
		timeout: 5 * time.Second,
	}
}

// SetTimeout sets the DNS query timeout
func (r *RBLChecker) SetTimeout(timeout time.Duration) {
	r.timeout = timeout
}

// Check checks an IP address against all configured RBL servers
func (r *RBLChecker) Check(ctx context.Context, ip net.IP) []RBLResult {
	var results []RBLResult

	for _, server := range r.servers {
		result := r.checkServer(ctx, ip, server)
		results = append(results, result)
	}

	return results
}

// IsListed checks if an IP is listed on any RBL
func (r *RBLChecker) IsListed(ctx context.Context, ip net.IP) (bool, []RBLResult) {
	results := r.Check(ctx, ip)

	var listed []RBLResult
	isListed := false

	for _, result := range results {
		if result.Listed {
			isListed = true
			listed = append(listed, result)
		}
	}

	return isListed, listed
}

// checkServer checks an IP against a single RBL server
func (r *RBLChecker) checkServer(ctx context.Context, ip net.IP, server string) RBLResult {
	result := RBLResult{
		Server: server,
		Listed: false,
	}

	// Reverse IP for DNS query
	reversedIP := reverseIP(ip)
	query := fmt.Sprintf("%s.%s", reversedIP, server)

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Perform DNS lookup
	addrs, err := net.DefaultResolver.LookupHost(timeoutCtx, query)
	if err != nil {
		// No record means not listed
		if dnsErr, ok := err.(*net.DNSError); ok && dnsErr.IsNotFound {
			result.Listed = false
			return result
		}
		result.Error = err
		return result
	}

	// If we got here, the IP is listed
	result.Listed = true
	if len(addrs) > 0 {
		result.Response = addrs[0]
		result.Reason = rblCodeToReason(result.Response)
	}

	return result
}

// reverseIP reverses an IP address for RBL queries
func reverseIP(ip net.IP) string {
	// Convert to 4-byte representation
	ip = ip.To4()
	if ip == nil {
		// IPv6
		ip = ip.To16()
		if ip == nil {
			return ""
		}
		// For IPv6, reverse nibble format
		return reverseIPv6(ip)
	}

	// IPv4: reverse octets
	return fmt.Sprintf("%d.%d.%d.%d", ip[3], ip[2], ip[1], ip[0])
}

// reverseIPv6 reverses an IPv6 address for RBL queries
func reverseIPv6(ip net.IP) string {
	// Convert to nibble format and reverse
	var nibbles []string
	for i := len(ip) - 1; i >= 0; i-- {
		b := ip[i]
		nibbles = append(nibbles, fmt.Sprintf("%x", b&0x0f))
		nibbles = append(nibbles, fmt.Sprintf("%x", b>>4))
	}
	return strings.Join(nibbles, ".")
}

// rblCodeToReason converts an RBL response code to a human-readable reason
func rblCodeToReason(code string) string {
	// Common RBL return codes
	// These vary by RBL provider
	switch code {
	case "127.0.0.1":
		return "Listed as spam source"
	case "127.0.0.2":
		return "Listed as spam source (direct)"
	case "127.0.0.3":
		return "Listed as spam source (indirect)"
	case "127.0.0.4":
		return "Listed for policy violation"
	case "127.0.0.10":
		return "Listed as dynamic IP"
	case "127.0.0.11":
		return "Listed as compromised"
	default:
		if strings.HasPrefix(code, "127.0.0.") {
			return "Listed (code: " + code + ")"
		}
		return "Unknown"
	}
}

// DefaultRBLServers returns a list of default RBL servers
func DefaultRBLServers() []string {
	return []string{
		"zen.spamhaus.org",
		"bl.spamcop.net",
		"b.barracudacentral.org",
		"dnsbl.sorbs.net",
		"spam.dnsbl.sorbs.net",
	}
}

// GetStats returns RBL statistics
func (r *RBLChecker) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"servers": len(r.servers),
		"timeout": r.timeout.Seconds(),
	}
}
