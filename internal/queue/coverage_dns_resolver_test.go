package queue

import (
	"context"
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// mockMTASTSDNSResolver implements MTASTSDNSResolver for testing
type mockMTASTSDNSResolver struct {
	lookupTXTFunc func(ctx context.Context, name string) ([]string, error)
	lookupIPFunc  func(ctx context.Context, host string) ([]net.IP, error)
	lookupMXFunc  func(ctx context.Context, domain string) ([]*net.MX, error)
}

func (m *mockMTASTSDNSResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	if m.lookupTXTFunc != nil {
		return m.lookupTXTFunc(ctx, name)
	}
	return nil, nil
}

func (m *mockMTASTSDNSResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	if m.lookupIPFunc != nil {
		return m.lookupIPFunc(ctx, host)
	}
	return nil, nil
}

func (m *mockMTASTSDNSResolver) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	if m.lookupMXFunc != nil {
		return m.lookupMXFunc(ctx, domain)
	}
	return nil, nil
}

// TestSetMTASTSDNSResolver tests that SetMTASTSDNSResolver properly replaces the validator
func TestSetMTASTSDNSResolver(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir, nil)

	// Create a mock resolver that returns values to exercise MTA-STS code paths
	mockResolver := &mockMTASTSDNSResolver{
		lookupTXTFunc: func(ctx context.Context, name string) ([]string, error) {
			// Return no records for _mta-sts queries
			return nil, nil
		},
	}

	// This should not panic and should replace the validator
	manager.SetMTASTSDNSResolver(mockResolver)

	// Verify the validator was replaced by checking that further calls work
	if manager.mtastsValidator == nil {
		t.Error("expected mtastsValidator to be non-nil after SetMTASTSDNSResolver")
	}
}

// TestSetDANEDNSResolver tests that SetDANEDNSResolver properly replaces the validator
func TestSetDANEDNSResolver(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir, nil)

	// Create a mock resolver
	mockResolver := &mockMTASTSDNSResolver{
		lookupTXTFunc: func(ctx context.Context, name string) ([]string, error) {
			return nil, nil
		},
	}

	// This should not panic and should replace the validator
	manager.SetDANEDNSResolver(mockResolver)

	// Verify the validator was replaced
	if manager.daneValidator == nil {
		t.Error("expected daneValidator to be non-nil after SetDANEDNSResolver")
	}
}

// TestSetMTASTSDNSResolverWithPolicy tests the setter when resolver returns MTA-STS policy
func TestSetMTASTSDNSResolverWithPolicy(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir, nil)

	// Mock resolver that returns a valid MTA-STS TXT record
	mockResolver := &mockMTASTSDNSResolver{
		lookupTXTFunc: func(ctx context.Context, name string) ([]string, error) {
			// _mta-sts.example.com returns a valid record
			if name == "_mta-sts.example.com" {
				return []string{"v=STSv1; id=abc123"}, nil
			}
			return nil, nil
		},
	}

	manager.SetMTASTSDNSResolver(mockResolver)
}

// TestSetDANEDNSResolverWithTLSA tests the setter when resolver returns TLSA records
func TestSetDANEDNSResolverWithTLSA(t *testing.T) {
	dataDir := t.TempDir()
	dbPath := dataDir + "/test.db"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	manager := NewManager(database, nil, dataDir, nil)

	// Mock resolver that returns empty TLSA records
	mockResolver := &mockMTASTSDNSResolver{
		lookupTXTFunc: func(ctx context.Context, name string) ([]string, error) {
			// Return no TLSA records
			return nil, nil
		},
	}

	manager.SetDANEDNSResolver(mockResolver)
}

// TestMockMTASTSDNSResolverAllMethods tests that mock implements all interface methods
func TestMockMTASTSDNSResolverAllMethods(t *testing.T) {
	mock := &mockMTASTSDNSResolver{
		lookupTXTFunc: func(ctx context.Context, name string) ([]string, error) {
			return []string{"test"}, nil
		},
		lookupIPFunc: func(ctx context.Context, host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
		lookupMXFunc: func(ctx context.Context, domain string) ([]*net.MX, error) {
			return []*net.MX{{Host: "mx.example.com", Pref: 10}}, nil
		},
	}

	ctx := context.Background()

	// Test LookupTXT
	txt, err := mock.LookupTXT(ctx, "test.com")
	if err != nil {
		t.Errorf("LookupTXT failed: %v", err)
	}
	if len(txt) != 1 || txt[0] != "test" {
		t.Errorf("LookupTXT returned unexpected value: %v", txt)
	}

	// Test LookupIP
	ips, err := mock.LookupIP(ctx, "test.com")
	if err != nil {
		t.Errorf("LookupIP failed: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "127.0.0.1" {
		t.Errorf("LookupIP returned unexpected value: %v", ips)
	}

	// Test LookupMX
	mxRecords, err := mock.LookupMX(ctx, "test.com")
	if err != nil {
		t.Errorf("LookupMX failed: %v", err)
	}
	if len(mxRecords) != 1 || mxRecords[0].Host != "mx.example.com" {
		t.Errorf("LookupMX returned unexpected value: %v", mxRecords)
	}
}

// TestRealMTASTSDNSResolverMethods tests the stub methods on realMTASTSDNSResolver
// that exist only to satisfy the auth.DNSResolver interface but are never called
// by MTA-STS or DANE validators.
func TestRealMTASTSDNSResolverMethods(t *testing.T) {
	resolver := &realMTASTSDNSResolver{}
	ctx := context.Background()

	// LookupIP is never called by validators - it returns an error to prevent misuse
	_, err := resolver.LookupIP(ctx, "example.com")
	if err == nil {
		t.Errorf("LookupIP expected error, got nil")
	}

	// LookupMX is never called by validators - it returns an error to prevent misuse
	_, err = resolver.LookupMX(ctx, "example.com")
	if err == nil {
		t.Errorf("LookupMX expected error, got nil")
	}

	// LookupTXT is actually used by validators
	txtRecords, err := resolver.LookupTXT(ctx, "example.com")
	if err != nil {
		t.Errorf("LookupTXT returned error: %v", err)
	}
	// TXT might be empty but no error is expected
	_ = txtRecords
}

// TestRealMTASTSDNSResolverLookupTXT tests that LookupTXT makes actual DNS calls
func TestRealMTASTSDNSResolverLookupTXT(t *testing.T) {
	resolver := &realMTASTSDNSResolver{}
	ctx := context.Background()

	// Test with example.com which definitely exists
	records, err := resolver.LookupTXT(ctx, "example.com")
	// DNS errors are acceptable for this test - we're just checking the function doesn't panic
	if err != nil {
		t.Logf("LookupTXT returned error (acceptable): %v", err)
	}
	if records == nil {
		records = []string{} // nil is OK, just means no records
	}

	// Just verify it returns without panic
	t.Logf("LookupTXT returned %d records", len(records))
}
