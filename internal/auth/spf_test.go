package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// mockDNSResolver is a mock DNS resolver for testing
type mockDNSResolver struct {
	txtRecords map[string][]string
	ipRecords  map[string][]net.IP
	mxRecords  map[string][]*net.MX
	failLookup map[string]bool
	tempFail   map[string]bool
}

func newMockDNSResolver() *mockDNSResolver {
	return &mockDNSResolver{
		txtRecords: make(map[string][]string),
		ipRecords:  make(map[string][]net.IP),
		mxRecords:  make(map[string][]*net.MX),
		failLookup: make(map[string]bool),
		tempFail:   make(map[string]bool),
	}
}

func (m *mockDNSResolver) LookupTXT(ctx context.Context, domain string) ([]string, error) {
	if m.tempFail[domain] {
		return nil, errors.New("timeout")
	}
	if m.failLookup[domain] {
		return nil, errors.New("lookup failed")
	}
	records, ok := m.txtRecords[domain]
	if !ok {
		return nil, errors.New("no records")
	}
	return records, nil
}

func (m *mockDNSResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	if m.tempFail[host] {
		return nil, errors.New("timeout")
	}
	if m.failLookup[host] {
		return nil, errors.New("lookup failed")
	}
	records, ok := m.ipRecords[host]
	if !ok {
		return nil, errors.New("no records")
	}
	return records, nil
}

func (m *mockDNSResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	if m.tempFail[domain] {
		return nil, errors.New("timeout")
	}
	if m.failLookup[domain] {
		return nil, errors.New("lookup failed")
	}
	records, ok := m.mxRecords[domain]
	if !ok {
		return nil, errors.New("no records")
	}
	return records, nil
}

func TestSPFResultString(t *testing.T) {
	tests := []struct {
		result   SPFResult
		expected string
	}{
		{SPFNone, "none"},
		{SPFNeutral, "neutral"},
		{SPFPass, "pass"},
		{SPFFail, "fail"},
		{SPFSoftFail, "softfail"},
		{SPFTempError, "temperror"},
		{SPFPermError, "permerror"},
		{SPFResult(999), "unknown"},
	}

	for _, tt := range tests {
		got := tt.result.String()
		if got != tt.expected {
			t.Errorf("SPFResult(%d).String() = %q, want %q", tt.result, got, tt.expected)
		}
	}
}

func TestNewSPFChecker(t *testing.T) {
	resolver := newMockDNSResolver()
	checker := NewSPFChecker(resolver)

	if checker == nil {
		t.Fatal("NewSPFChecker returned nil")
	}
	if checker.resolver != resolver {
		t.Error("Resolver not set correctly")
	}
	if checker.cache == nil {
		t.Error("Cache not initialized")
	}
}

func TestSPFCheckNoRecord(t *testing.T) {
	resolver := newMockDNSResolver()
	checker := NewSPFChecker(resolver)

	ip := net.ParseIP("192.168.1.1")
	result, explanation := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFNone {
		t.Errorf("Expected SPFNone, got %s", result.String())
	}
	if explanation != "No SPF record found" {
		t.Errorf("Expected 'No SPF record found', got %q", explanation)
	}
}

func TestSPFCheckAllPass(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 +all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for +all, got %s", result.String())
	}
}

func TestSPFCheckAllFail(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail for -all, got %s", result.String())
	}
}

func TestSPFCheckAllSoftFail(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 ~all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFSoftFail {
		t.Errorf("Expected SPFSoftFail for ~all, got %s", result.String())
	}
}

func TestSPFCheckAllNeutral(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 ?all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFNeutral {
		t.Errorf("Expected SPFNeutral for ?all, got %s", result.String())
	}
}

func TestSPFCheckIP4Match(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 ip4:192.168.1.1 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for matching ip4, got %s", result.String())
	}
}

func TestSPFCheckIP4NoMatch(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 ip4:192.168.1.1 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.2")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail for non-matching ip4, got %s", result.String())
	}
}

func TestSPFCheckIP4CIDR(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 ip4:192.168.0.0/16 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for ip4 CIDR match, got %s", result.String())
	}
}

func TestSPFCheckIP6Match(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 ip6:2001:db8::1 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("2001:db8::1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for matching ip6, got %s", result.String())
	}
}

func TestSPFCheckAMatch(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 a -all"}
	resolver.ipRecords["example.com"] = []net.IP{
		net.ParseIP("192.168.1.1"),
		net.ParseIP("192.168.1.2"),
	}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for A record match, got %s", result.String())
	}
}

func TestSPFCheckANoMatch(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 a -all"}
	resolver.ipRecords["example.com"] = []net.IP{
		net.ParseIP("192.168.1.2"),
	}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail for A record no match, got %s", result.String())
	}
}

func TestSPFCheckMXMatch(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 mx -all"}
	resolver.mxRecords["example.com"] = []*net.MX{
		{Host: "mail1.example.com"},
		{Host: "mail2.example.com"},
	}
	resolver.ipRecords["mail1.example.com"] = []net.IP{net.ParseIP("192.168.1.1")}
	resolver.ipRecords["mail2.example.com"] = []net.IP{net.ParseIP("192.168.1.2")}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for MX record match, got %s", result.String())
	}
}

func TestSPFCheckIncludePass(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 include:_spf.example.com -all"}
	resolver.txtRecords["_spf.example.com"] = []string{"v=spf1 ip4:192.168.1.0/24 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for include match, got %s", result.String())
	}
}

func TestSPFCheckIncludeFail(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 include:_spf.example.com -all"}
	resolver.txtRecords["_spf.example.com"] = []string{"v=spf1 ip4:10.0.0.0/8 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail for include no match, got %s", result.String())
	}
}

func TestSPFCheckRedirect(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 redirect=_spf.example.com"}
	resolver.txtRecords["_spf.example.com"] = []string{"v=spf1 ip4:192.168.1.0/24 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for redirect match, got %s", result.String())
	}
}

func TestSPFCheckExists(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 exists:example.com -all"}
	resolver.ipRecords["example.com"] = []net.IP{net.ParseIP("192.168.1.1")}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("10.0.0.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for exists match, got %s", result.String())
	}
}

func TestSPFCheckTempError(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 a -all"}
	resolver.tempFail["example.com"] = true

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, explanation := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFTempError {
		t.Errorf("Expected SPFTempError for DNS timeout, got %s", result.String())
	}
	if explanation != "DNS lookup failed" {
		t.Errorf("Expected 'DNS lookup failed', got %q", explanation)
	}
}

func TestSPFCheckLookupLimit(t *testing.T) {
	resolver := newMockDNSResolver()
	// Create a chain of includes that exceeds 10 lookups
	// We need 11 includes to exceed the limit (lookup for each include + 1 more)
	resolver.txtRecords["example.com"] = []string{"v=spf1 include:spf1.example.com -all"}
	for i := 1; i <= 11; i++ {
		next := fmt.Sprintf("spf%d.example.com", i+1)
		if i == 11 {
			resolver.txtRecords[fmt.Sprintf("spf%d.example.com", i)] = []string{"v=spf1 -all"}
		} else {
			resolver.txtRecords[fmt.Sprintf("spf%d.example.com", i)] = []string{fmt.Sprintf("v=spf1 include:%s -all", next)}
		}
	}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, explanation := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPermError {
		t.Errorf("Expected SPFPermError for lookup limit exceeded, got %s", result.String())
	}
	if explanation != "Too many DNS lookups" {
		t.Errorf("Expected 'Too many DNS lookups', got %q", explanation)
	}
}

func TestSPFCache(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 +all"}

	checker := NewSPFChecker(resolver)

	// First lookup
	ip := net.ParseIP("192.168.1.1")
	result1, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	// Second lookup should use cache
	result2, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result1 != result2 {
		t.Errorf("Cache returned different results: %s vs %s", result1.String(), result2.String())
	}
}

func TestSPFCacheExpiration(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 +all"}

	checker := NewSPFChecker(resolver)

	// First lookup
	ip := net.ParseIP("192.168.1.1")
	checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	// Manually expire cache entry
	checker.cache.mu.Lock()
	checker.cache.records["example.com"].expiresAt = time.Now().Add(-time.Minute)
	checker.cache.mu.Unlock()

	// Second lookup should fetch again
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")
	if result != SPFPass {
		t.Errorf("Expected SPFPass after cache expiration, got %s", result.String())
	}
}

func TestParseSPF(t *testing.T) {
	tests := []struct {
		record   string
		expected int // number of mechanisms
	}{
		{"v=spf1 +all", 1},
		{"v=spf1 ip4:192.168.1.1 -all", 2},
		{"v=spf1 a mx include:_spf.example.com ~all", 4},
		{"v=spf1", 0},
	}

	for _, tt := range tests {
		mechanisms := parseSPF(tt.record)
		if len(mechanisms) != tt.expected {
			t.Errorf("parseSPF(%q) returned %d mechanisms, expected %d", tt.record, len(mechanisms), tt.expected)
		}
	}
}

func TestParseMechanism(t *testing.T) {
	tests := []struct {
		part         string
		expectedType string
		expectedVal  string
		expectedQual SPFResult
	}{
		{"+all", "all", "", SPFPass},
		{"-all", "all", "", SPFFail},
		{"~all", "all", "", SPFSoftFail},
		{"?all", "all", "", SPFNeutral},
		{"all", "all", "", SPFPass},
		{"ip4:192.168.1.1", "ip4", "192.168.1.1", SPFPass},
		{"-ip4:192.168.1.1", "ip4", "192.168.1.1", SPFFail},
		{"a", "a", "", SPFPass},
		{"a:mail.example.com", "a", "mail.example.com", SPFPass},
		{"mx", "mx", "", SPFPass},
		{"include:_spf.example.com", "include", "_spf.example.com", SPFPass},
		{"exists:example.com", "exists", "example.com", SPFPass},
		{"redirect=_spf.example.com", "redirect", "_spf.example.com", SPFPass},
	}

	for _, tt := range tests {
		m := parseMechanism(tt.part)
		if m.typ != tt.expectedType {
			t.Errorf("parseMechanism(%q).typ = %q, want %q", tt.part, m.typ, tt.expectedType)
		}
		if m.value != tt.expectedVal {
			t.Errorf("parseMechanism(%q).value = %q, want %q", tt.part, m.value, tt.expectedVal)
		}
		if m.qualifier != tt.expectedQual {
			t.Errorf("parseMechanism(%q).qualifier = %v, want %v", tt.part, m.qualifier, tt.expectedQual)
		}
	}
}

func TestEvaluateIP4(t *testing.T) {
	checker := NewSPFChecker(nil)

	tests := []struct {
		ip      string
		value   string
		expected bool
	}{
		{"192.168.1.1", "192.168.1.1", true},
		{"192.168.1.1", "192.168.1.2", false},
		{"192.168.1.1", "192.168.1.0/24", true},
		{"192.168.1.1", "10.0.0.0/8", false},
		{"10.0.0.1", "192.168.1.0/24", false}, // IPv4-mapped IPv6
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		got := checker.evaluateIP4(ip, tt.value)
		if got != tt.expected {
			t.Errorf("evaluateIP4(%q, %q) = %v, want %v", tt.ip, tt.value, got, tt.expected)
		}
	}
}

func TestEvaluateIP6(t *testing.T) {
	checker := NewSPFChecker(nil)

	tests := []struct {
		ip      string
		value   string
		expected bool
	}{
		{"2001:db8::1", "2001:db8::1", true},
		{"2001:db8::1", "2001:db8::2", false},
		{"2001:db8::1", "2001:db8::/32", true},
		{"2001:db8::1", "2001:db9::/32", false},
		{"192.168.1.1", "2001:db8::/32", false}, // IPv4 should not match
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		got := checker.evaluateIP6(ip, tt.value)
		if got != tt.expected {
			t.Errorf("evaluateIP6(%q, %q) = %v, want %v", tt.ip, tt.value, got, tt.expected)
		}
	}
}

func TestIsTemporaryError(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{errors.New("some error"), false},
		{errors.New("connection timeout"), true},
		{errors.New("temporary failure"), true},
		{errors.New("i/o timeout"), true},
	}

	for _, tt := range tests {
		got := isTemporaryError(tt.err)
		if got != tt.expected {
			t.Errorf("isTemporaryError(%v) = %v, want %v", tt.err, got, tt.expected)
		}
	}
}

func TestSPFMechanismString(t *testing.T) {
	tests := []struct {
		m        spfMechanism
		expected string
	}{
		{spfMechanism{typ: "all", qualifier: SPFPass}, "+all"},
		{spfMechanism{typ: "all", qualifier: SPFFail}, "-all"},
		{spfMechanism{typ: "all", qualifier: SPFSoftFail}, "~all"},
		{spfMechanism{typ: "all", qualifier: SPFNeutral}, "?all"},
		{spfMechanism{typ: "ip4", value: "192.168.1.1", qualifier: SPFPass}, "+ip4:192.168.1.1"},
		{spfMechanism{typ: "a", value: "example.com", qualifier: SPFPass}, "+a:example.com"},
	}

	for _, tt := range tests {
		got := tt.m.String()
		if got != tt.expected {
			t.Errorf("spfMechanism.String() = %q, want %q", got, tt.expected)
		}
	}
}
