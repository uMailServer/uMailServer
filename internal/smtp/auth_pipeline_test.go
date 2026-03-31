package smtp

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"testing"

	"github.com/umailserver/umailserver/internal/auth"
)

// ---------------------------------------------------------------------------
// mapSPFResult tests
// ---------------------------------------------------------------------------

func TestMapSPFResult(t *testing.T) {
	tests := []struct {
		input    string
		expected auth.SPFResult
	}{
		{"pass", auth.SPFPass},
		{"PASS", auth.SPFPass},
		{"Pass", auth.SPFPass},
		{"fail", auth.SPFFail},
		{"FAIL", auth.SPFFail},
		{"softfail", auth.SPFSoftFail},
		{"SOFTFAIL", auth.SPFSoftFail},
		{"neutral", auth.SPFNeutral},
		{"NEUTRAL", auth.SPFNeutral},
		{"temperror", auth.SPFTempError},
		{"TEMPERROR", auth.SPFTempError},
		{"permerror", auth.SPFPermError},
		{"PERMERROR", auth.SPFPermError},
		{"none", auth.SPFNone},
		{"", auth.SPFNone},
		{"unknown", auth.SPFNone},
		{"anything", auth.SPFNone},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapSPFResult(tt.input)
			if got != tt.expected {
				t.Errorf("mapSPFResult(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mapDKIMResult tests
// ---------------------------------------------------------------------------

func TestMapDKIMResult(t *testing.T) {
	tests := []struct {
		name     string
		input    DKIMResult
		expected auth.DKIMResult
	}{
		{
			name:     "valid DKIM",
			input:    DKIMResult{Valid: true, Domain: "example.com", Selector: "default"},
			expected: auth.DKIMPass,
		},
		{
			name:     "failed DKIM with error",
			input:    DKIMResult{Valid: false, Domain: "example.com", Error: "verification failed"},
			expected: auth.DKIMFail,
		},
		{
			name:     "no DKIM signature",
			input:    DKIMResult{Valid: false},
			expected: auth.DKIMNone,
		},
		{
			name: "valid with empty domain",
			input: DKIMResult{Valid: true},
			expected: auth.DKIMPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapDKIMResult(tt.input)
			if got != tt.expected {
				t.Errorf("mapDKIMResult(%+v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PipelineLogger tests
// ---------------------------------------------------------------------------

func TestPipelineLogger(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	pl := NewPipelineLogger(logger)

	if pl == nil {
		t.Fatal("NewPipelineLogger returned nil")
	}

	t.Run("Debug", func(t *testing.T) {
		buf.Reset()
		pl.Debug("test debug message", "key", "value")
		output := buf.String()
		if output == "" {
			t.Error("Expected debug log output, got empty string")
		}
	})

	t.Run("Info", func(t *testing.T) {
		buf.Reset()
		pl.Info("test info message", "key", "value")
		output := buf.String()
		if output == "" {
			t.Error("Expected info log output, got empty string")
		}
	})

	t.Run("Warn", func(t *testing.T) {
		buf.Reset()
		pl.Warn("test warn message", "key", "value")
		output := buf.String()
		if output == "" {
			t.Error("Expected warn log output, got empty string")
		}
	})

	t.Run("Error", func(t *testing.T) {
		buf.Reset()
		pl.Error("test error message", "key", "value")
		output := buf.String()
		if output == "" {
			t.Error("Expected error log output, got empty string")
		}
	})
}

func TestPipelineLoggerNil(t *testing.T) {
	// Ensure that calling methods on a PipelineLogger does not panic
	// even when created with a real logger (always non-nil for slog)
	pl := NewPipelineLogger(slog.Default())
	pl.Debug("debug")
	pl.Info("info")
	pl.Warn("warn")
	pl.Error("error")
}

// ---------------------------------------------------------------------------
// argsToAttrs tests
// ---------------------------------------------------------------------------

func TestArgsToAttrs(t *testing.T) {
	tests := []struct {
		name     string
		input    []interface{}
		expected []interface{}
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []interface{}{},
			expected: []interface{}{},
		},
		{
			name:     "key-value pairs",
			input:    []interface{}{"key1", "value1", "key2", 42},
			expected: []interface{}{"key1", "value1", "key2", 42},
		},
		{
			name:     "single value",
			input:    []interface{}{"onlykey"},
			expected: []interface{}{"onlykey"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := argsToAttrs(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("argsToAttrs length = %d, want %d", len(got), len(tt.expected))
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("argsToAttrs[%d] = %v, want %v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Mock types for AuthSPFStage / AuthDKIMStage / AuthDMARCStage tests
// ---------------------------------------------------------------------------

// mockSPFChecker implements enough to stand in for *auth.SPFChecker.
// Because AuthSPFStage.checker is a concrete *auth.SPFChecker, we
// construct a real SPFChecker with a mock DNSResolver.
type mockAuthDNSResolver struct {
	txtRecords map[string][]string
	ipRecords  map[string][]net.IP
	mxRecords  map[string][]*net.MX
	txtErr     map[string]error
}

func (m *mockAuthDNSResolver) LookupTXT(ctx context.Context, domain string) ([]string, error) {
	if err, ok := m.txtErr[domain]; ok {
		return nil, err
	}
	if r, ok := m.txtRecords[domain]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *mockAuthDNSResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	if r, ok := m.ipRecords[host]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *mockAuthDNSResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	if r, ok := m.mxRecords[domain]; ok {
		return r, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// AuthSPFStage.Process tests
// ---------------------------------------------------------------------------

func TestAuthSPFStage_Process(t *testing.T) {
	t.Run("empty sender domain", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{}
		checker := auth.NewSPFChecker(resolver)
		stage := NewAuthSPFStage(checker, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "", []string{"rcpt@example.com"}, []byte("data"))
		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		if ctx.SPFResult.Result != "none" {
			t.Errorf("Expected SPF result 'none', got %q", ctx.SPFResult.Result)
		}
	})

	t.Run("SPF pass", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{
				"example.com": {"v=spf1 ip4:1.2.3.4 -all"},
			},
		}
		checker := auth.NewSPFChecker(resolver)
		stage := NewAuthSPFStage(checker, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
	})

	t.Run("SPF fail increases spam score", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{
				"example.com": {"v=spf1 ip4:10.0.0.1 -all"},
			},
		}
		checker := auth.NewSPFChecker(resolver)
		stage := NewAuthSPFStage(checker, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		// SPF fail should add 2.5 to spam score
		if ctx.SpamScore < 2.0 {
			t.Errorf("Expected spam score >= 2.0 for SPF fail, got %f", ctx.SpamScore)
		}
	})

	t.Run("SPF softfail increases spam score", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{
				"example.com": {"v=spf1 ip4:10.0.0.1 ~all"},
			},
		}
		checker := auth.NewSPFChecker(resolver)
		stage := NewAuthSPFStage(checker, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		if ctx.SpamScore < 1.0 {
			t.Errorf("Expected spam score >= 1.0 for SPF softfail, got %f", ctx.SpamScore)
		}
	})

	t.Run("no SPF record", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{},
		}
		checker := auth.NewSPFChecker(resolver)
		stage := NewAuthSPFStage(checker, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
	})

	t.Run("stage name", func(t *testing.T) {
		stage := NewAuthSPFStage(nil, nil)
		if stage.Name() != "SPF" {
			t.Errorf("Expected stage name 'SPF', got %q", stage.Name())
		}
	})
}

// ---------------------------------------------------------------------------
// AuthDKIMStage.Process tests
// ---------------------------------------------------------------------------

func TestAuthDKIMStage_Process(t *testing.T) {
	t.Run("no DKIM signature header", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{}
		verifier := auth.NewDKIMVerifier(resolver)
		stage := NewAuthDKIMStage(verifier, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		if ctx.DKIMResult.Valid {
			t.Error("Expected DKIM valid=false for no signature")
		}
		if ctx.DKIMResult.Error != "no DKIM signature" {
			t.Errorf("Expected 'no DKIM signature' error, got %q", ctx.DKIMResult.Error)
		}
	})

	t.Run("with Dkim-Signature header (lowercase d)", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{}
		verifier := auth.NewDKIMVerifier(resolver)
		stage := NewAuthDKIMStage(verifier, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		ctx.Headers["Dkim-Signature"] = []string{"v=1; d=example.com; s=default; a=rsa-sha256; bh=abc; b=xyz; h=from:to"}
		result := stage.Process(ctx)

		// Even with invalid signature, stage should return ResultAccept
		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		// DKIM should not be valid (no actual public key in DNS)
		if ctx.DKIMResult.Valid {
			t.Error("Expected DKIM valid=false with invalid signature data")
		}
	})

	t.Run("DKIM-Signature header (canonical case)", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{}
		verifier := auth.NewDKIMVerifier(resolver)
		stage := NewAuthDKIMStage(verifier, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		ctx.Headers["DKIM-Signature"] = []string{"v=1; d=example.com; s=default; a=rsa-sha256; bh=abc; b=xyz; h=from:to"}
		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		if ctx.DKIMResult.Valid {
			t.Error("Expected DKIM valid=false with unverifiable signature")
		}
	})

	t.Run("stage name", func(t *testing.T) {
		stage := NewAuthDKIMStage(nil, nil)
		if stage.Name() != "DKIM" {
			t.Errorf("Expected stage name 'DKIM', got %q", stage.Name())
		}
	})
}

// ---------------------------------------------------------------------------
// AuthDMARCStage.Process tests
// ---------------------------------------------------------------------------

func TestAuthDMARCStage_Process(t *testing.T) {
	t.Run("empty from domain", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{}
		evaluator := auth.NewDMARCEvaluator(resolver)
		stage := NewAuthDMARCStage(evaluator, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "", []string{"rcpt@example.com"}, []byte("data"))
		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		if ctx.DMARCResult.Result != "none" {
			t.Errorf("Expected DMARC result 'none', got %q", ctx.DMARCResult.Result)
		}
	})

	t.Run("no DMARC record", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{},
		}
		evaluator := auth.NewDMARCEvaluator(resolver)
		stage := NewAuthDMARCStage(evaluator, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		ctx.SPFResult = SPFResult{Result: "pass", Domain: "example.com"}
		ctx.DKIMResult = DKIMResult{Valid: true, Domain: "example.com"}

		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
	})

	t.Run("DMARC pass with SPF aligned", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{
				"_dmarc.example.com": {"v=DMARC1; p=reject;"},
			},
		}
		evaluator := auth.NewDMARCEvaluator(resolver)
		stage := NewAuthDMARCStage(evaluator, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		ctx.SPFResult = SPFResult{Result: "pass", Domain: "example.com"}

		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		if ctx.DMARCResult.Result != "pass" {
			t.Errorf("Expected DMARC result 'pass', got %q", ctx.DMARCResult.Result)
		}
	})

	t.Run("DMARC fail with reject policy", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{
				"_dmarc.example.com": {"v=DMARC1; p=reject;"},
			},
		}
		evaluator := auth.NewDMARCEvaluator(resolver)
		stage := NewAuthDMARCStage(evaluator, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		ctx.SPFResult = SPFResult{Result: "fail", Domain: "other.com"}
		ctx.DKIMResult = DKIMResult{Valid: false, Error: "no signature"}

		result := stage.Process(ctx)

		if result != ResultReject {
			t.Errorf("Expected ResultReject, got %v", result)
		}
		if !ctx.Rejected {
			t.Error("Expected Rejected=true")
		}
		if ctx.RejectionCode != 550 {
			t.Errorf("Expected rejection code 550, got %d", ctx.RejectionCode)
		}
	})

	t.Run("DMARC fail with quarantine policy", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{
				"_dmarc.example.com": {"v=DMARC1; p=quarantine;"},
			},
		}
		evaluator := auth.NewDMARCEvaluator(resolver)
		stage := NewAuthDMARCStage(evaluator, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		ctx.SPFResult = SPFResult{Result: "fail", Domain: "other.com"}
		ctx.DKIMResult = DKIMResult{Valid: false, Error: "failed"}

		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept (quarantine does not reject at stage level), got %v", result)
		}
		if !ctx.Quarantine {
			t.Error("Expected Quarantine=true")
		}
		if ctx.SpamScore < 2.0 {
			t.Errorf("Expected spam score >= 2.0 for quarantine, got %f", ctx.SpamScore)
		}
	})

	t.Run("DMARC pass with DKIM aligned", func(t *testing.T) {
		resolver := &mockAuthDNSResolver{
			txtRecords: map[string][]string{
				"_dmarc.example.com": {"v=DMARC1; p=reject;"},
			},
		}
		evaluator := auth.NewDMARCEvaluator(resolver)
		stage := NewAuthDMARCStage(evaluator, nil)

		ctx := NewMessageContext(net.ParseIP("1.2.3.4"), "sender@example.com", []string{"rcpt@example.com"}, []byte("data"))
		ctx.DKIMResult = DKIMResult{Valid: true, Domain: "example.com", Selector: "default"}

		result := stage.Process(ctx)

		if result != ResultAccept {
			t.Errorf("Expected ResultAccept, got %v", result)
		}
		if ctx.DMARCResult.Result != "pass" {
			t.Errorf("Expected DMARC result 'pass', got %q", ctx.DMARCResult.Result)
		}
	})

	t.Run("stage name", func(t *testing.T) {
		stage := NewAuthDMARCStage(nil, nil)
		if stage.Name() != "DMARC" {
			t.Errorf("Expected stage name 'DMARC', got %q", stage.Name())
		}
	})
}

// ---------------------------------------------------------------------------
// NetDNSResolver basic construction test
// ---------------------------------------------------------------------------

func TestNewNetDNSResolver(t *testing.T) {
	r := NewNetDNSResolver()
	if r == nil {
		t.Fatal("NewNetDNSResolver returned nil")
	}
	if r.resolver == nil {
		t.Error("Expected resolver to be initialized")
	}
}

// ---------------------------------------------------------------------------
// NewPipelineLogger test
// ---------------------------------------------------------------------------

func TestNewPipelineLogger(t *testing.T) {
	logger := slog.Default()
	pl := NewPipelineLogger(logger)
	if pl == nil {
		t.Fatal("NewPipelineLogger returned nil")
	}
	if pl.logger != logger {
		t.Error("Expected logger to be set")
	}
}
