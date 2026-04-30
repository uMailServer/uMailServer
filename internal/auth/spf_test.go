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

// LookupTLSA implements TLSAResolver for testing
// It parses TXT records as TLSA data for backward compatibility
func (m *mockDNSResolver) LookupTLSA(domain string) ([]*TLSARecord, error) {
	// Permanent lookup failure returns no records (swallow error)
	if m.failLookup[domain] {
		return nil, nil
	}
	// Temporary failures return error
	if m.tempFail[domain] {
		return nil, errors.New("timeout")
	}
	// Use TXT records as TLSA fallback for tests
	if txtRecords, ok := m.txtRecords[domain]; ok {
		var records []*TLSARecord
		for _, txt := range txtRecords {
			if record, err := parseTLSARecord(txt); err == nil && record != nil {
				records = append(records, record)
			}
		}
		return records, nil
	}
	return nil, nil
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
	if checker.cache.ttl != defaultSPFCacheTTL {
		t.Errorf("default cache TTL = %v, want %v", checker.cache.ttl, defaultSPFCacheTTL)
	}
}

func TestSPFCheckerSetCacheTTL(t *testing.T) {
	checker := NewSPFChecker(newMockDNSResolver())

	// Custom TTL is applied
	checker.SetCacheTTL(30 * time.Minute)
	if checker.cache.ttl != 30*time.Minute {
		t.Errorf("after SetCacheTTL: ttl = %v, want %v", checker.cache.ttl, 30*time.Minute)
	}

	// Zero/negative TTLs are ignored to keep the existing value
	checker.SetCacheTTL(0)
	if checker.cache.ttl != 30*time.Minute {
		t.Errorf("zero TTL should be ignored: ttl = %v", checker.cache.ttl)
	}
	checker.SetCacheTTL(-1 * time.Second)
	if checker.cache.ttl != 30*time.Minute {
		t.Errorf("negative TTL should be ignored: ttl = %v", checker.cache.ttl)
	}

	// Verify the TTL is actually used when populating the cache
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 +all"}
	checker = NewSPFChecker(resolver)
	checker.SetCacheTTL(2 * time.Hour)

	_, _ = checker.CheckSPF(context.Background(), net.ParseIP("192.0.2.1"), "example.com", "sender@example.com")

	checker.cache.mu.RLock()
	entry, ok := checker.cache.records["example.com"]
	checker.cache.mu.RUnlock()
	if !ok {
		t.Fatal("expected example.com to be cached")
	}
	expected := time.Now().Add(2 * time.Hour)
	delta := entry.expiresAt.Sub(expected)
	if delta < -5*time.Second || delta > 5*time.Second {
		t.Errorf("cache entry expiresAt %v not within 5s of expected %v", entry.expiresAt, expected)
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
		ip       string
		value    string
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
		ip       string
		value    string
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

func TestEvaluateExists_NonExistingDomain(t *testing.T) {
	// evaluateExists returns false, true (void) when the domain does not resolve
	resolver := newMockDNSResolver()
	resolver.failLookup["nonexistent.example.com"] = true
	checker := NewSPFChecker(resolver)

	match, void, err := checker.evaluateExists(context.Background(), "nonexistent.example.com", 0, 0)
	if match {
		t.Error("Expected match=false for non-existing domain")
	}
	if !void {
		t.Error("Expected void=true for non-existing domain (non-temp error)")
	}
	if err != nil {
		t.Errorf("Expected no error for non-temp lookup failure, got %v", err)
	}
}

func TestEvaluateExists_TempError(t *testing.T) {
	// evaluateExists returns error when a temporary DNS error occurs
	resolver := newMockDNSResolver()
	resolver.tempFail["tempfail.example.com"] = true
	checker := NewSPFChecker(resolver)

	match, void, err := checker.evaluateExists(context.Background(), "tempfail.example.com", 0, 0)
	if match {
		t.Error("Expected match=false for temp error")
	}
	if void {
		t.Error("Expected void=false for temp error")
	}
	if err == nil {
		t.Error("Expected error for temp DNS failure")
	}
}

func TestEvaluateExists_ExistingDomain(t *testing.T) {
	// evaluateExists returns true when the domain resolves (even to empty IP list is not enough;
	// mock returns error if no records, so we set records)
	resolver := newMockDNSResolver()
	resolver.ipRecords["exists.example.com"] = []net.IP{net.ParseIP("1.2.3.4")}
	checker := NewSPFChecker(resolver)

	match, void, err := checker.evaluateExists(context.Background(), "exists.example.com", 0, 0)
	if !match {
		t.Error("Expected match=true for existing domain")
	}
	if void {
		t.Error("Expected void=false for existing domain")
	}
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestEvaluateMX_ExplicitDomain(t *testing.T) {
	// evaluateMX with an explicit domain value (value != "")
	resolver := newMockDNSResolver()
	resolver.mxRecords["other.example.com"] = []*net.MX{
		{Host: "mx.other.example.com"},
	}
	resolver.ipRecords["mx.other.example.com"] = []net.IP{net.ParseIP("10.0.0.5")}
	checker := NewSPFChecker(resolver)

	ip := net.ParseIP("10.0.0.5")
	match, void, err := checker.evaluateMX(context.Background(), ip, "other.example.com", "example.com", 0, 0)
	if !match {
		t.Error("Expected match=true for MX record with explicit domain")
	}
	if void {
		t.Error("Expected void=false for successful MX match")
	}
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestEvaluateMX_NoMXRecords(t *testing.T) {
	// evaluateMX returns void when no MX records found (non-temp error)
	resolver := newMockDNSResolver()
	resolver.failLookup["example.com"] = true
	checker := NewSPFChecker(resolver)

	ip := net.ParseIP("192.168.1.1")
	match, void, err := checker.evaluateMX(context.Background(), ip, "", "example.com", 0, 0)
	if match {
		t.Error("Expected match=false when MX lookup fails")
	}
	if !void {
		t.Error("Expected void=true for failed MX lookup")
	}
	if err != nil {
		t.Errorf("Expected no error for non-temp failure, got %v", err)
	}
}

func TestEvaluateMX_MXTempError(t *testing.T) {
	// evaluateMX returns temp error when MX lookup times out
	resolver := newMockDNSResolver()
	resolver.tempFail["example.com"] = true
	checker := NewSPFChecker(resolver)

	ip := net.ParseIP("192.168.1.1")
	match, void, err := checker.evaluateMX(context.Background(), ip, "", "example.com", 0, 0)
	if match {
		t.Error("Expected match=false for temp error")
	}
	if void {
		t.Error("Expected void=false for temp error")
	}
	if err == nil {
		t.Error("Expected error for temp DNS failure")
	}
}

func TestEvaluateMX_MXHostNonResolving(t *testing.T) {
	// evaluateMX: MX record exists but the host IP lookup fails (non-temp error)
	resolver := newMockDNSResolver()
	resolver.mxRecords["example.com"] = []*net.MX{
		{Host: "mail.example.com"},
	}
	resolver.failLookup["mail.example.com"] = true
	checker := NewSPFChecker(resolver)

	ip := net.ParseIP("192.168.1.1")
	match, void, err := checker.evaluateMX(context.Background(), ip, "", "example.com", 0, 0)
	if match {
		t.Error("Expected match=false when MX host IP lookup fails")
	}
	if void {
		t.Error("Expected void=false when MX host IP lookup fails (non-temp)")
	}
	if err != nil {
		t.Errorf("Expected no error for non-temp failure, got %v", err)
	}
}

func TestEvaluateMX_MXHostTempError(t *testing.T) {
	// evaluateMX returns temp error when resolving MX host IP times out
	resolver := newMockDNSResolver()
	resolver.mxRecords["example.com"] = []*net.MX{
		{Host: "mail.example.com"},
	}
	resolver.tempFail["mail.example.com"] = true
	checker := NewSPFChecker(resolver)

	ip := net.ParseIP("192.168.1.1")
	match, void, err := checker.evaluateMX(context.Background(), ip, "", "example.com", 0, 0)
	if match {
		t.Error("Expected match=false for temp error")
	}
	if void {
		t.Error("Expected void=false for temp error")
	}
	if err == nil {
		t.Error("Expected error for temp DNS failure on MX host lookup")
	}
}

func TestEvaluateMX_EmptyMXRecords(t *testing.T) {
	// evaluateMX with empty MX record list (returns void)
	resolver := newMockDNSResolver()
	resolver.mxRecords["example.com"] = []*net.MX{}
	checker := NewSPFChecker(resolver)

	ip := net.ParseIP("192.168.1.1")
	match, void, err := checker.evaluateMX(context.Background(), ip, "", "example.com", 0, 0)
	if match {
		t.Error("Expected match=false for empty MX records")
	}
	if !void {
		t.Error("Expected void=true for empty MX records")
	}
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestEvaluateInclude_VoidLookup(t *testing.T) {
	// evaluateInclude: included domain has no SPF record (void lookup)
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 include:nospf.example.com -all"}
	resolver.failLookup["nospf.example.com"] = true

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	// The include results in void, then -all matches -> SPFFail
	if result != SPFFail {
		t.Errorf("Expected SPFFail (include void + -all), got %s", result.String())
	}
}

func TestEvaluateInclude_PermError(t *testing.T) {
	// evaluateInclude propagates permanent error from nested evaluation
	resolver := newMockDNSResolver()
	// Create a deep include chain that exceeds lookup limit
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
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPermError {
		t.Errorf("Expected SPFPermError from deep include chain, got %s", result.String())
	}
}

func TestEvaluateInclude_TempError(t *testing.T) {
	// evaluateInclude returns temp error when included domain lookup times out
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 include:tempfail.example.com -all"}
	resolver.tempFail["tempfail.example.com"] = true

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFTempError {
		t.Errorf("Expected SPFTempError for include temp error, got %s", result.String())
	}
}

func TestEvaluateInclude_NeutralResult(t *testing.T) {
	// evaluateInclude: included SPF returns neutral (not pass), so include does not match
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 include:_spf.example.com -all"}
	resolver.txtRecords["_spf.example.com"] = []string{"v=spf1 ?all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail (include neutral -> no match, then -all), got %s", result.String())
	}
}

func TestEvaluateInclude_SoftFailResult(t *testing.T) {
	// evaluateInclude: included SPF returns softfail (not pass), so include does not match
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 include:_spf.example.com -all"}
	resolver.txtRecords["_spf.example.com"] = []string{"v=spf1 ~all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail (include softfail -> no match, then -all), got %s", result.String())
	}
}

func TestEvaluateA_ExplicitHost(t *testing.T) {
	// evaluateA with an explicit host value (not empty, not domain)
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 a:mail.example.com -all"}
	resolver.ipRecords["mail.example.com"] = []net.IP{net.ParseIP("10.0.0.1")}
	resolver.ipRecords["example.com"] = []net.IP{net.ParseIP("192.168.1.1")}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("10.0.0.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPass {
		t.Errorf("Expected SPFPass for A record with explicit host match, got %s", result.String())
	}
}

func TestEvaluateA_EmptyIPs(t *testing.T) {
	// evaluateA: DNS lookup returns empty IP list -> void lookup
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 a -all"}
	resolver.ipRecords["example.com"] = []net.IP{}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail for A record with empty IPs, got %s", result.String())
	}
}

func TestEvaluateA_TempError(t *testing.T) {
	// evaluateA returns error when DNS lookup times out
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 a -all"}
	resolver.tempFail["example.com"] = true

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFTempError {
		t.Errorf("Expected SPFTempError for A record temp error, got %s", result.String())
	}
}

func TestEvaluateA_NonTempError(t *testing.T) {
	// evaluateA returns void for non-temp DNS error
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 a:missing.example.com -all"}
	resolver.failLookup["missing.example.com"] = true

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail (void A lookup + -all), got %s", result.String())
	}
}

func TestEvaluateIP4_IPv6Input(t *testing.T) {
	// evaluateIP4 returns false when given an IPv6 address (ip.To4() == nil)
	checker := NewSPFChecker(nil)
	ip := net.ParseIP("2001:db8::1")
	got := checker.evaluateIP4(ip, "192.168.1.0/24")
	if got {
		t.Error("Expected false when evaluating IPv6 address against IPv4 CIDR")
	}
}

func TestEvaluateIP4_InvalidValue(t *testing.T) {
	// evaluateIP4 returns false for invalid value (not CIDR, not parseable IP)
	checker := NewSPFChecker(nil)
	ip := net.ParseIP("192.168.1.1")
	got := checker.evaluateIP4(ip, "not-an-ip")
	if got {
		t.Error("Expected false for invalid ip4 value")
	}
}

func TestEvaluateIP4_CIDRNoMatch(t *testing.T) {
	// evaluateIP4: valid CIDR but IP not in range
	checker := NewSPFChecker(nil)
	ip := net.ParseIP("10.0.0.1")
	got := checker.evaluateIP4(ip, "192.168.0.0/16")
	if got {
		t.Error("Expected false for IP outside CIDR range")
	}
}

func TestEvaluate_VoidLookupLimit(t *testing.T) {
	// The void lookup limit is checked at the top of evaluate(), so we need
	// to trigger re-entry via include or redirect after voids accumulate.
	// Use an include that has 2 void lookups in its SPF record, then includes
	// another domain -- that second include re-enters evaluate with voidLookups >= 2.
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 include:_spf.example.com -all"}
	resolver.txtRecords["_spf.example.com"] = []string{"v=spf1 a:fail1.example.com a:fail2.example.com include:deep.example.com -all"}
	resolver.failLookup["fail1.example.com"] = true
	resolver.failLookup["fail2.example.com"] = true
	resolver.txtRecords["deep.example.com"] = []string{"v=spf1 ip4:192.168.1.1 -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, explanation := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPermError {
		t.Errorf("Expected SPFPermError for too many void lookups, got %s: %s", result.String(), explanation)
	}
}

func TestEvaluate_RedirectTempError(t *testing.T) {
	// evaluate: redirect target has temp DNS error
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 redirect=_spf.example.com"}
	resolver.tempFail["_spf.example.com"] = true

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFTempError {
		t.Errorf("Expected SPFTempError for redirect temp error, got %s", result.String())
	}
}

func TestEvaluate_RedirectInvalidTarget(t *testing.T) {
	// evaluate: redirect target has no SPF record (non-temp error)
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 redirect=missing.example.com"}
	resolver.failLookup["missing.example.com"] = true

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, explanation := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPermError {
		t.Errorf("Expected SPFPermError for invalid redirect, got %s", result.String())
	}
	if explanation != "Invalid redirect" {
		t.Errorf("Expected 'Invalid redirect', got %q", explanation)
	}
}

func TestEvaluate_DefaultNeutralNoMechanisms(t *testing.T) {
	// evaluate: SPF record with no mechanisms returns neutral
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFNeutral {
		t.Errorf("Expected SPFNeutral for v=spf1 with no mechanisms, got %s", result.String())
	}
}

func TestEvaluate_RedirectLookupLimit(t *testing.T) {
	// evaluate: redirect should trigger lookup limit check
	resolver := newMockDNSResolver()
	// Build a long redirect chain
	resolver.txtRecords["example.com"] = []string{"v=spf1 redirect=r1.example.com"}
	for i := 1; i <= 12; i++ {
		cur := fmt.Sprintf("r%d.example.com", i)
		if i >= 10 {
			resolver.txtRecords[cur] = []string{"v=spf1 ip4:192.168.1.0/24 -all"}
		} else {
			resolver.txtRecords[cur] = []string{fmt.Sprintf("v=spf1 redirect=r%d.example.com", i+1)}
		}
	}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, explanation := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFPermError {
		t.Errorf("Expected SPFPermError for redirect lookup limit, got %s: %s", result.String(), explanation)
	}
}

func TestEvaluate_PtrMechanism(t *testing.T) {
	// PTR mechanism always returns false (discouraged)
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 ptr -all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFFail {
		t.Errorf("Expected SPFFail (ptr always false + -all), got %s", result.String())
	}
}

func TestEvaluate_UnknownMechanism(t *testing.T) {
	// Unknown mechanism type returns false (no match)
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 unknown:foo ?all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFNeutral {
		t.Errorf("Expected SPFNeutral (unknown mech + ?all), got %s", result.String())
	}
}

func TestEvaluate_ExpModifier(t *testing.T) {
	// exp= modifier is parsed but treated as unknown mechanism type -> no match
	resolver := newMockDNSResolver()
	resolver.txtRecords["example.com"] = []string{"v=spf1 exp=explain.example.com ?all"}

	checker := NewSPFChecker(resolver)
	ip := net.ParseIP("192.168.1.1")
	result, _ := checker.CheckSPF(context.Background(), ip, "example.com", "sender@example.com")

	if result != SPFNeutral {
		t.Errorf("Expected SPFNeutral (exp modifier + ?all), got %s", result.String())
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

// ---------------------------------------------------------------------------
// spfCache coverage tests
// ---------------------------------------------------------------------------

func TestSPFCache_Set_Eviction(t *testing.T) {
	c := &spfCache{
		records:     make(map[string]*cacheEntry),
		maxSize:     3,
		nextCleanup: time.Now().Add(1 * time.Hour),
		ttl:         1 * time.Hour,
	}

	// Fill cache to maxSize
	c.set("d1.example.com", "v=spf1 ip4:1.1.1.1 -all", 1*time.Hour)
	c.set("d2.example.com", "v=spf1 ip4:2.2.2.2 -all", 1*time.Hour)
	c.set("d3.example.com", "v=spf1 ip4:3.3.3.3 -all", 1*time.Hour)

	if len(c.records) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(c.records))
	}

	// Add a 4th entry - should trigger eviction
	c.set("d4.example.com", "v=spf1 ip4:4.4.4.4 -all", 1*time.Hour)

	if len(c.records) > 3 {
		t.Errorf("expected eviction to keep cache at or below maxSize, got %d", len(c.records))
	}
}

func TestSPFCache_Set_PeriodicCleanup(t *testing.T) {
	c := &spfCache{
		records:     make(map[string]*cacheEntry),
		maxSize:     100,
		nextCleanup: time.Now().Add(-1 * time.Second), // overdue
		ttl:         1 * time.Hour,
	}

	// Add an already-expired entry manually
	c.records["old.example.com"] = &cacheEntry{
		record:    "v=spf1 -all",
		expiresAt: time.Now().Add(-1 * time.Hour),
	}

	// set should trigger cleanup because nextCleanup is past
	c.set("new.example.com", "v=spf1 ip4:1.2.3.4 -all", 1*time.Hour)

	if _, ok := c.records["old.example.com"]; ok {
		t.Error("expected expired entry to be cleaned up")
	}

	if _, ok := c.records["new.example.com"]; !ok {
		t.Error("expected new entry to exist after cleanup")
	}
}

func TestSPFCache_EvictOldest(t *testing.T) {
	c := &spfCache{
		records:     make(map[string]*cacheEntry),
		maxSize:     10,
		nextCleanup: time.Now().Add(1 * time.Hour),
		ttl:         1 * time.Hour,
	}

	now := time.Now()
	// Add entries with staggered expiry times
	for i := 0; i < 10; i++ {
		c.records[fmt.Sprintf("d%d.example.com", i)] = &cacheEntry{
			record:    fmt.Sprintf("v=spf1 ip4:%d.%d.%d.%d -all", i, i, i, i),
			expiresAt: now.Add(time.Duration(i) * time.Hour),
		}
	}

	c.evictOldest()

	// Should have removed ~10% (1 entry) and any expired (none)
	if len(c.records) != 9 {
		t.Errorf("expected 9 entries after evictOldest, got %d", len(c.records))
	}
}

func TestSPFCache_EvictOldest_WithExpired(t *testing.T) {
	c := &spfCache{
		records:     make(map[string]*cacheEntry),
		maxSize:     10,
		nextCleanup: time.Now().Add(1 * time.Hour),
		ttl:         1 * time.Hour,
	}

	now := time.Now()
	// Fill cache so that eviction + expired removal is needed
	for i := 0; i < 10; i++ {
		c.records[fmt.Sprintf("d%d.example.com", i)] = &cacheEntry{
			record:    fmt.Sprintf("v=spf1 ip4:%d.%d.%d.%d -all", i, i, i, i),
			expiresAt: now.Add(time.Duration(i+1) * time.Hour),
		}
	}
	// Make two entries expired; the oldest expired is removed by eviction,
	// the second-oldest expired is removed by cleanupExpiredLocked.
	c.records["d0.example.com"].expiresAt = now.Add(-2 * time.Hour)
	c.records["d1.example.com"].expiresAt = now.Add(-1 * time.Hour)

	c.evictOldest()

	// Evict removes oldest 10% (1 entry = d0) plus cleanupExpiredLocked removes d1
	// Net result: 8 entries remain
	if len(c.records) != 8 {
		t.Errorf("expected 8 entries, got %d", len(c.records))
	}
}

func TestSPFCache_CleanupExpired(t *testing.T) {
	c := &spfCache{
		records:     make(map[string]*cacheEntry),
		maxSize:     100,
		nextCleanup: time.Now().Add(1 * time.Hour),
		ttl:         1 * time.Hour,
	}

	now := time.Now()
	c.records["expired.example.com"] = &cacheEntry{
		record:    "v=spf1 -all",
		expiresAt: now.Add(-1 * time.Hour),
	}
	c.records["valid.example.com"] = &cacheEntry{
		record:    "v=spf1 ip4:1.1.1.1 -all",
		expiresAt: now.Add(1 * time.Hour),
	}

	c.cleanupExpired()

	if _, ok := c.records["expired.example.com"]; ok {
		t.Error("expected expired entry to be removed")
	}
	if _, ok := c.records["valid.example.com"]; !ok {
		t.Error("expected valid entry to remain")
	}
}

func TestSPFCache_CleanupExpiredLocked(t *testing.T) {
	c := &spfCache{
		records:     make(map[string]*cacheEntry),
		maxSize:     100,
		nextCleanup: time.Now().Add(1 * time.Hour),
		ttl:         1 * time.Hour,
	}

	now := time.Now()
	c.records["old.example.com"] = &cacheEntry{
		record:    "v=spf1 -all",
		expiresAt: now.Add(-1 * time.Minute),
	}
	c.records["future.example.com"] = &cacheEntry{
		record:    "v=spf1 ip4:8.8.8.8 -all",
		expiresAt: now.Add(1 * time.Hour),
	}

	c.cleanupExpiredLocked(now)

	if len(c.records) != 1 {
		t.Errorf("expected 1 entry after cleanup, got %d", len(c.records))
	}
	if _, ok := c.records["future.example.com"]; !ok {
		t.Error("expected future entry to remain")
	}
}

func TestSPFCache_Get_Expired(t *testing.T) {
	c := &spfCache{
		records:     make(map[string]*cacheEntry),
		maxSize:     100,
		nextCleanup: time.Now().Add(1 * time.Hour),
		ttl:         1 * time.Hour,
	}

	c.records["expired.example.com"] = &cacheEntry{
		record:    "v=spf1 -all",
		expiresAt: time.Now().Add(-1 * time.Hour),
	}

	_, ok := c.get("expired.example.com")
	if ok {
		t.Error("expected get to return false for expired entry")
	}
}
