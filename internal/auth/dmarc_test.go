package auth

import (
	"context"
	"testing"
)

func TestDMARCResultString(t *testing.T) {
	tests := []struct {
		result   DMARCResult
		expected string
	}{
		{DMARCNone, "none"},
		{DMARCPass, "pass"},
		{DMARCFail, "fail"},
		{DMARCPermError, "permerror"},
		{DMARCTempError, "temperror"},
		{DMARCResult(999), "unknown"},
	}

	for _, tt := range tests {
		got := tt.result.String()
		if got != tt.expected {
			t.Errorf("DMARCResult(%d).String() = %q, want %q", tt.result, got, tt.expected)
		}
	}
}

func TestNewDMARCEvaluator(t *testing.T) {
	resolver := newMockDNSResolver()
	evaluator := NewDMARCEvaluator(resolver)

	if evaluator == nil {
		t.Fatal("NewDMARCEvaluator returned nil")
	}
	if evaluator.resolver != resolver {
		t.Error("Resolver not set correctly")
	}
	if evaluator.clock == nil {
		t.Error("Clock function not set")
	}
}

func TestParseDMARCRecord(t *testing.T) {
	tests := []struct {
		name          string
		record        string
		wantPolicy    DMARCPolicy
		wantPct       int
		wantAdkim     DMARCAlignment
		wantAspf      DMARCAlignment
		wantSubdomain string
		wantErr       bool
	}{
		{
			name:       "basic none policy",
			record:     "v=DMARC1; p=none",
			wantPolicy: DMARCPolicyNone,
			wantPct:    100,
			wantAdkim:  DMARCAlignmentRelaxed,
			wantAspf:   DMARCAlignmentRelaxed,
			wantErr:    false,
		},
		{
			name:       "quarantine policy",
			record:     "v=DMARC1; p=quarantine",
			wantPolicy: DMARCPolicyQuarantine,
			wantPct:    100,
			wantAdkim:  DMARCAlignmentRelaxed,
			wantAspf:   DMARCAlignmentRelaxed,
			wantErr:    false,
		},
		{
			name:       "reject policy",
			record:     "v=DMARC1; p=reject",
			wantPolicy: DMARCPolicyReject,
			wantPct:    100,
			wantAdkim:  DMARCAlignmentRelaxed,
			wantAspf:   DMARCAlignmentRelaxed,
			wantErr:    false,
		},
		{
			name:          "with subdomain policy",
			record:        "v=DMARC1; p=reject; sp=quarantine",
			wantPolicy:    DMARCPolicyReject,
			wantSubdomain: "quarantine",
			wantPct:       100,
			wantAdkim:     DMARCAlignmentRelaxed,
			wantAspf:      DMARCAlignmentRelaxed,
			wantErr:       false,
		},
		{
			name:       "with percentage",
			record:     "v=DMARC1; p=reject; pct=50",
			wantPolicy: DMARCPolicyReject,
			wantPct:    50,
			wantAdkim:  DMARCAlignmentRelaxed,
			wantAspf:   DMARCAlignmentRelaxed,
			wantErr:    false,
		},
		{
			name:       "strict alignment",
			record:     "v=DMARC1; p=reject; adkim=s; aspf=s",
			wantPolicy: DMARCPolicyReject,
			wantPct:    100,
			wantAdkim:  DMARCAlignmentStrict,
			wantAspf:   DMARCAlignmentStrict,
			wantErr:    false,
		},
		{
			name:       "with report URIs",
			record:     "v=DMARC1; p=none; rua=mailto:dmarc@example.com",
			wantPolicy: DMARCPolicyNone,
			wantPct:    100,
			wantAdkim:  DMARCAlignmentRelaxed,
			wantAspf:   DMARCAlignmentRelaxed,
			wantErr:    false,
		},
		{
			name:    "missing policy",
			record:  "v=DMARC1",
			wantErr: true,
		},
		{
			name:    "wrong version",
			record:  "v=DMARC2; p=none",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := parseDMARCRecord(tt.record)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if rec.Policy != tt.wantPolicy {
				t.Errorf("Policy = %q, want %q", rec.Policy, tt.wantPolicy)
			}
			if rec.Percentage != tt.wantPct {
				t.Errorf("Percentage = %d, want %d", rec.Percentage, tt.wantPct)
			}
			if rec.AlignmentDKIM != tt.wantAdkim {
				t.Errorf("AlignmentDKIM = %q, want %q", rec.AlignmentDKIM, tt.wantAdkim)
			}
			if rec.AlignmentSPF != tt.wantAspf {
				t.Errorf("AlignmentSPF = %q, want %q", rec.AlignmentSPF, tt.wantAspf)
			}
		})
	}
}

func TestCheckAlignment(t *testing.T) {
	tests := []struct {
		name       string
		authDomain string
		fromDomain string
		mode       DMARCAlignment
		expected   bool
	}{
		{
			name:       "relaxed exact match",
			authDomain: "example.com",
			fromDomain: "example.com",
			mode:       DMARCAlignmentRelaxed,
			expected:   true,
		},
		{
			name:       "relaxed subdomain",
			authDomain: "mail.example.com",
			fromDomain: "example.com",
			mode:       DMARCAlignmentRelaxed,
			expected:   true,
		},
		{
			name:       "relaxed child subdomain",
			authDomain: "example.com",
			fromDomain: "mail.example.com",
			mode:       DMARCAlignmentRelaxed,
			expected:   true,
		},
		{
			name:       "relaxed different org",
			authDomain: "example.com",
			fromDomain: "other.com",
			mode:       DMARCAlignmentRelaxed,
			expected:   false,
		},
		{
			name:       "strict exact match",
			authDomain: "example.com",
			fromDomain: "example.com",
			mode:       DMARCAlignmentStrict,
			expected:   true,
		},
		{
			name:       "strict subdomain mismatch",
			authDomain: "mail.example.com",
			fromDomain: "example.com",
			mode:       DMARCAlignmentStrict,
			expected:   false,
		},
		{
			name:       "case insensitive",
			authDomain: "EXAMPLE.COM",
			fromDomain: "example.com",
			mode:       DMARCAlignmentRelaxed,
			expected:   true,
		},
		{
			name:       "empty auth domain",
			authDomain: "",
			fromDomain: "example.com",
			mode:       DMARCAlignmentRelaxed,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkAlignment(tt.authDomain, tt.fromDomain, tt.mode)
			if result != tt.expected {
				t.Errorf("checkAlignment(%q, %q, %q) = %v, want %v",
					tt.authDomain, tt.fromDomain, tt.mode, result, tt.expected)
			}
		})
	}
}

func TestIsOrganizationalDomainMatch(t *testing.T) {
	tests := []struct {
		domain1  string
		domain2  string
		expected bool
	}{
		{"example.com", "example.com", true},
		{"mail.example.com", "example.com", true},
		{"example.com", "mail.example.com", true},
		{"a.b.example.com", "c.d.example.com", true},
		{"example.com", "other.com", false},
		{"example.co.uk", "other.co.uk", true}, // Same org domain per simple implementation
		{"example", "example", true},           // single label
		{"example.com", "example.org", false},
	}

	for _, tt := range tests {
		result := isOrganizationalDomainMatch(tt.domain1, tt.domain2)
		if result != tt.expected {
			t.Errorf("isOrganizationalDomainMatch(%q, %q) = %v, want %v",
				tt.domain1, tt.domain2, result, tt.expected)
		}
	}
}

func TestIsSubdomain(t *testing.T) {
	tests := []struct {
		domain   string
		expected bool
	}{
		{"example.com", false},
		{"mail.example.com", true},
		{"a.b.example.com", true},
		{"example.co.uk", true}, // Note: simple implementation counts parts
		{"com", false},
	}

	for _, tt := range tests {
		result := isSubdomain(tt.domain)
		if result != tt.expected {
			t.Errorf("isSubdomain(%q) = %v, want %v", tt.domain, result, tt.expected)
		}
	}
}

func TestShouldApplyPolicy(t *testing.T) {
	// Test that shouldApplyPolicy returns consistent results
	// Note: This uses random sampling, so we can't test exact behavior

	// 100% should always apply
	if !shouldApplyPolicy(100) {
		t.Error("shouldApplyPolicy(100) should always return true")
	}

	// 0% should never apply
	if shouldApplyPolicy(0) {
		t.Error("shouldApplyPolicy(0) should always return false")
	}

	// 50% should apply roughly half the time (we'll just verify it doesn't panic)
	for i := 0; i < 10; i++ {
		_ = shouldApplyPolicy(50)
	}
}

func TestParseURIList(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"mailto:a@example.com", []string{"mailto:a@example.com"}},
		{"mailto:a@example.com,mailto:b@example.com", []string{"mailto:a@example.com", "mailto:b@example.com"}},
		{"  uri1  ,  uri2  ", []string{"uri1", "uri2"}},
		{"", []string{}},
	}

	for _, tt := range tests {
		result := parseURIList(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseURIList(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseURIList(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestParseFailureOptions(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"0", []string{"0"}},
		{"1", []string{"1"}},
		{"0:1:d", []string{"0", "1", "d"}},
		{"", []string{"0"}}, // default
	}

	for _, tt := range tests {
		result := parseFailureOptions(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseFailureOptions(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseFailureOptions(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestDMARCEvaluateNoRecord(t *testing.T) {
	resolver := newMockDNSResolver()
	evaluator := NewDMARCEvaluator(resolver)

	ctx := context.Background()
	eval, err := evaluator.Evaluate(ctx, "example.com", SPFPass, "example.com", DKIMNone, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if eval.Result != DMARCNone {
		t.Errorf("Expected DMARCNone, got %s", eval.Result.String())
	}
	if eval.Explanation != "No DMARC record found" {
		t.Errorf("Expected 'No DMARC record found', got %q", eval.Explanation)
	}
}

func TestDMARCEvaluateSPFPass(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject"}

	evaluator := NewDMARCEvaluator(resolver)
	ctx := context.Background()

	// SPF passes and is aligned
	eval, err := evaluator.Evaluate(ctx, "example.com", SPFPass, "example.com", DKIMNone, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if eval.Result != DMARCPass {
		t.Errorf("Expected DMARCPass, got %s", eval.Result.String())
	}
	if eval.AppliedPolicy != DMARCPolicyNone {
		t.Errorf("Expected policy none when pass, got %s", eval.AppliedPolicy)
	}
	if eval.Explanation != "SPF aligned" {
		t.Errorf("Expected 'SPF aligned', got %q", eval.Explanation)
	}
}

func TestDMARCEvaluateDKIMPass(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject"}

	evaluator := NewDMARCEvaluator(resolver)

	ctx := context.Background()
	// DKIM passes and is aligned
	eval, err := evaluator.Evaluate(ctx, "example.com", SPFNone, "other.com", DKIMPass, "example.com")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if eval.Result != DMARCPass {
		t.Errorf("Expected DMARCPass, got %s", eval.Result.String())
	}
	if eval.AppliedPolicy != DMARCPolicyNone {
		t.Errorf("Expected policy none when pass, got %s", eval.AppliedPolicy)
	}
}

func TestDMARCEvaluateFailWithReject(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject"}

	evaluator := NewDMARCEvaluator(resolver)

	ctx := context.Background()
	// Both SPF and DKIM fail
	eval, err := evaluator.Evaluate(ctx, "example.com", SPFNone, "", DKIMNone, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if eval.Result != DMARCFail {
		t.Errorf("Expected DMARCFail, got %s", eval.Result.String())
	}
	if eval.AppliedPolicy != DMARCPolicyReject {
		t.Errorf("Expected reject policy, got %s", eval.AppliedPolicy)
	}
	if eval.Disposition != "reject" {
		t.Errorf("Expected reject disposition, got %s", eval.Disposition)
	}
}

func TestDMARCEvaluateFailWithQuarantine(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=quarantine"}

	evaluator := NewDMARCEvaluator(resolver)

	ctx := context.Background()
	eval, err := evaluator.Evaluate(ctx, "example.com", SPFNone, "", DKIMNone, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if eval.Result != DMARCFail {
		t.Errorf("Expected DMARCFail, got %s", eval.Result.String())
	}
	if eval.AppliedPolicy != DMARCPolicyQuarantine {
		t.Errorf("Expected quarantine policy, got %s", eval.AppliedPolicy)
	}
	if eval.Disposition != "quarantine" {
		t.Errorf("Expected quarantine disposition, got %s", eval.Disposition)
	}
}

func TestDMARCEvaluateFailWithNone(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=none"}

	evaluator := NewDMARCEvaluator(resolver)

	ctx := context.Background()
	eval, err := evaluator.Evaluate(ctx, "example.com", SPFNone, "", DKIMNone, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if eval.Result != DMARCFail {
		t.Errorf("Expected DMARCFail, got %s", eval.Result.String())
	}
	if eval.AppliedPolicy != DMARCPolicyNone {
		t.Errorf("Expected none policy, got %s", eval.AppliedPolicy)
	}
	if eval.Disposition != "none" {
		t.Errorf("Expected none disposition, got %s", eval.Disposition)
	}
}

func TestDMARCEvaluateWithPercentage(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject; pct=0"}

	evaluator := NewDMARCEvaluator(resolver)

	ctx := context.Background()
	eval, err := evaluator.Evaluate(ctx, "example.com", SPFNone, "", DKIMNone, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if eval.Result != DMARCFail {
		t.Errorf("Expected DMARCFail, got %s", eval.Result.String())
	}
	// With pct=0, policy should be none (not applied)
	if eval.AppliedPolicy != DMARCPolicyNone {
		t.Errorf("Expected none policy with pct=0, got %s", eval.AppliedPolicy)
	}
}

func TestDMARCEvaluateSPFNotAligned(t *testing.T) {
	resolver := newMockDNSResolver()
	resolver.txtRecords["_dmarc.example.com"] = []string{"v=DMARC1; p=reject"}

	evaluator := NewDMARCEvaluator(resolver)

	ctx := context.Background()
	// SPF passes but is not aligned (different domain)
	eval, err := evaluator.Evaluate(ctx, "example.com", SPFPass, "other.com", DKIMNone, "")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// Should fail because SPF is not aligned
	if eval.Result != DMARCFail {
		t.Errorf("Expected DMARCFail for unaligned SPF, got %s", eval.Result.String())
	}
}

func TestGenerateDMARCReport(t *testing.T) {
	records := []DMARCReportRecord{
		{
			SourceIP:    "192.168.1.1",
			Count:       1,
			PolicyEval:  "fail",
			Disposition: "reject",
			SPFResult:   "fail",
			DKIMResult:  "fail",
			HeaderFrom:  "example.com",
		},
	}

	report := GenerateDMARCReport("example.com", records)

	if report == "" {
		t.Error("Report should not be empty")
	}

	if !contains(report, "<feedback>") {
		t.Error("Report should contain feedback element")
	}

	if !contains(report, "example.com") {
		t.Error("Report should contain domain")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
