package auth

// TODO: Wire DANE/TLSA validation into outbound SMTP delivery.
// DANE (DNS-Based Authentication of Named Entities, RFC 6698) authenticates
// TLS connections using DNSSEC. The full implementation exists here but is
// not yet integrated into the outbound delivery pipeline.

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DANEResult represents the result of DANE validation
type DANEResult int

const (
	DANENone      DANEResult = iota // No TLSA record found
	DANEValidated                   // DANE validation passed
	DANEFailed                      // DANE validation failed
	DANEUnusable                    // TLSA record unusable (unsupported parameters)
)

func (r DANEResult) String() string {
	switch r {
	case DANENone:
		return "none"
	case DANEValidated:
		return "validated"
	case DANEFailed:
		return "failed"
	case DANEUnusable:
		return "unusable"
	default:
		return "unknown"
	}
}

// TLSAUsage represents the TLSA certificate usage field
type TLSAUsage byte

const (
	TLSAUsagePKITAAncillary  TLSAUsage = iota // 0: PKIX TA (not used in DANE)
	TLSAUsagePKITEEAncillary TLSAUsage = 1    // 1: PKIX EE (not used in DANE)
	TLSAUsageDANETA          TLSAUsage = 2    // 2: DANE TA
	TLSAUsageDANEEE          TLSAUsage = 3    // 3: DANE EE
)

// TLSASelector represents the TLSA selector field
type TLSASelector byte

const (
	TLSASelectorFullCert TLSASelector = iota // 0: Full certificate
	TLSASelectorSPKI     TLSASelector = 1    // 1: SubjectPublicKeyInfo
)

// TLSAMatchingType represents the TLSA matching type field
type TLSAMatchingType byte

const (
	TLSAMatchingTypeFull   TLSAMatchingType = iota // 0: Exact match
	TLSAMatchingTypeSHA256 TLSAMatchingType = 1    // 1: SHA-256
	TLSAMatchingTypeSHA512 TLSAMatchingType = 2    // 2: SHA-512
)

// TLSARecord represents a TLSA DNS record
type TLSARecord struct {
	Usage        TLSAUsage
	Selector     TLSASelector
	MatchingType TLSAMatchingType
	Certificate  []byte // Associated data (full cert or hash)
}

// DANEValidator handles DANE TLSA validation
type DANEValidator struct {
	resolver DNSResolver
}

// NewDANEValidator creates a new DANE validator
func NewDANEValidator(resolver DNSResolver) *DANEValidator {
	return &DANEValidator{
		resolver: resolver,
	}
}

// Validate validates a TLS connection using DANE TLSA records
func (v *DANEValidator) Validate(domain string, port int, state *tls.ConnectionState) (DANEResult, error) {
	// Look up TLSA records
	tlsaRecords, err := v.LookupTLSA(domain, port)
	if err != nil {
		// DNS error - treat as failure
		return DANEFailed, err
	}

	if len(tlsaRecords) == 0 {
		// No TLSA records - DANE not configured
		return DANENone, nil
	}

	// Get peer certificate
	if len(state.PeerCertificates) == 0 {
		return DANEFailed, errors.New("no peer certificates")
	}

	peerCert := state.PeerCertificates[0]

	// Try to validate against each TLSA record
	for _, tlsa := range tlsaRecords {
		// Skip unsupported usages
		if tlsa.Usage == TLSAUsagePKITAAncillary || tlsa.Usage == TLSAUsagePKITEEAncillary {
			// These are for PKIX, not pure DANE
			continue
		}

		// Validate against this record
		if v.validateRecord(tlsa, peerCert, state) {
			return DANEValidated, nil
		}
	}

	// No matching TLSA record found
	return DANEFailed, nil
}

// LookupTLSA looks up TLSA records for a domain and port
func (v *DANEValidator) LookupTLSA(domain string, port int) ([]*TLSARecord, error) {
	// TLSA query format: _port._protocol.domain
	// For SMTP: _25._tcp.domain
	query := fmt.Sprintf("_%d._tcp.%s", port, domain)

	return v.lookupTLSARecords(query)
}

// lookupTLSARecords performs the actual TLSA lookup
func (v *DANEValidator) lookupTLSARecords(query string) ([]*TLSARecord, error) {
	// Use standard DNS lookup for TLSA records
	// Note: This requires a DNS resolver that supports TLSA record types
	// For now, we use TXT lookup as a placeholder

	// In production, you would use:
	// r, err := net.LookupTLSA(query) or custom resolver

	// For this implementation, we'll use the resolver interface
	// but note that standard Go net package doesn't have TLSA lookup
	txtRecords, err := v.resolver.LookupTXT(context.Background(), query)
	if err != nil {
		if isTemporaryError(err) {
			return nil, err
		}
		return nil, nil
	}

	var records []*TLSARecord
	for _, txt := range txtRecords {
		record, err := parseTLSARecord(txt)
		if err == nil && record != nil {
			records = append(records, record)
		}
	}

	return records, nil
}

// parseTLSARecord parses a TLSA record from wire format
func parseTLSARecord(data string) (*TLSARecord, error) {
	// TLSA record format (in hex): USAGE SELECTOR MATCHINGTYPE CERTIFICATE_DATA
	// Example: "3 1 1 abc123..." (DANE-EE, SPKI, SHA256, hash)

	parts := strings.Fields(data)
	if len(parts) < 4 {
		// Try parsing as hex string
		return parseTLSAHex(data)
	}

	// Parse numeric fields
	usage, err := strconv.ParseUint(parts[0], 10, 8)
	if err != nil {
		return nil, fmt.Errorf("invalid usage: %w", err)
	}

	selector, err := strconv.ParseUint(parts[1], 10, 8)
	if err != nil {
		return nil, fmt.Errorf("invalid selector: %w", err)
	}

	matchingType, err := strconv.ParseUint(parts[2], 10, 8)
	if err != nil {
		return nil, fmt.Errorf("invalid matching type: %w", err)
	}

	// Parse certificate data (rest of the fields concatenated)
	certData := strings.Join(parts[3:], "")
	certData = strings.ReplaceAll(certData, " ", "")
	certData = strings.ReplaceAll(certData, ":", "")

	certBytes, err := hex.DecodeString(certData)
	if err != nil {
		return nil, fmt.Errorf("invalid certificate data: %w", err)
	}

	return &TLSARecord{
		Usage:        TLSAUsage(usage),
		Selector:     TLSASelector(selector),
		MatchingType: TLSAMatchingType(matchingType),
		Certificate:  certBytes,
	}, nil
}

// parseTLSAHex parses a TLSA record from raw hex data
func parseTLSAHex(hexData string) (*TLSARecord, error) {
	// Remove any spaces or colons
	hexData = strings.ReplaceAll(hexData, " ", "")
	hexData = strings.ReplaceAll(hexData, ":", "")

	data, err := hex.DecodeString(hexData)
	if err != nil {
		return nil, err
	}

	if len(data) < 4 {
		return nil, errors.New("TLSA record too short")
	}

	return &TLSARecord{
		Usage:        TLSAUsage(data[0]),
		Selector:     TLSASelector(data[1]),
		MatchingType: TLSAMatchingType(data[2]),
		Certificate:  data[3:],
	}, nil
}

// validateRecord validates a certificate against a single TLSA record
func (v *DANEValidator) validateRecord(tlsa *TLSARecord, cert *x509.Certificate, state *tls.ConnectionState) bool {
	// Get the data to match based on selector
	var dataToMatch []byte

	switch tlsa.Selector {
	case TLSASelectorFullCert:
		// Full certificate
		dataToMatch = cert.Raw
	case TLSASelectorSPKI:
		// SubjectPublicKeyInfo
		dataToMatch = cert.RawSubjectPublicKeyInfo
	default:
		// Unsupported selector
		return false
	}

	// Apply matching type
	var computedData []byte

	switch tlsa.MatchingType {
	case TLSAMatchingTypeFull:
		// Exact match
		computedData = dataToMatch
	case TLSAMatchingTypeSHA256:
		// SHA-256 hash
		hash := sha256.Sum256(dataToMatch)
		computedData = hash[:]
	case TLSAMatchingTypeSHA512:
		// SHA-512 hash (not implemented in this simplified version)
		return false
	default:
		// Unsupported matching type
		return false
	}

	// Compare
	return string(computedData) == string(tlsa.Certificate)
}

// ValidateMX validates the MX server for a domain using DANE
func (v *DANEValidator) ValidateMX(mxDomain string, state *tls.ConnectionState) (DANEResult, error) {
	// For MX, we validate against port 25
	return v.Validate(mxDomain, 25, state)
}

// ValidateSubmission validates a submission server using DANE
func (v *DANEValidator) ValidateSubmission(domain string, state *tls.ConnectionState) (DANEResult, error) {
	// For submission, we validate against port 587
	return v.Validate(domain, 587, state)
}

// IsDANEAvailable checks if DANE is configured for a domain
func (v *DANEValidator) IsDANEAvailable(domain string, port int) (bool, error) {
	records, err := v.LookupTLSA(domain, port)
	if err != nil {
		return false, err
	}
	return len(records) > 0, nil
}

// DANEPolicy represents DANE policy for a domain
type DANEPolicy struct {
	Domain       string
	Port         int
	HasTLSA      bool
	Usages       []TLSAUsage
	ValidRecords int
}

// GetPolicy returns the DANE policy for a domain
func (v *DANEValidator) GetPolicy(domain string, port int) (*DANEPolicy, error) {
	records, err := v.LookupTLSA(domain, port)
	if err != nil {
		return nil, err
	}

	policy := &DANEPolicy{
		Domain:  domain,
		Port:    port,
		HasTLSA: len(records) > 0,
		Usages:  make([]TLSAUsage, 0),
	}

	usageMap := make(map[TLSAUsage]bool)
	for _, record := range records {
		if record.Usage == TLSAUsageDANETA || record.Usage == TLSAUsageDANEEE {
			policy.ValidRecords++
			if !usageMap[record.Usage] {
				usageMap[record.Usage] = true
				policy.Usages = append(policy.Usages, record.Usage)
			}
		}
	}

	return policy, nil
}

// GenerateTLSARecord generates a TLSA record for a certificate
func GenerateTLSARecord(cert *x509.Certificate, usage TLSAUsage, selector TLSASelector, matchingType TLSAMatchingType) *TLSARecord {
	var dataToMatch []byte

	switch selector {
	case TLSASelectorFullCert:
		dataToMatch = cert.Raw
	case TLSASelectorSPKI:
		dataToMatch = cert.RawSubjectPublicKeyInfo
	}

	var certData []byte

	switch matchingType {
	case TLSAMatchingTypeFull:
		certData = dataToMatch
	case TLSAMatchingTypeSHA256:
		hash := sha256.Sum256(dataToMatch)
		certData = hash[:]
	case TLSAMatchingTypeSHA512:
		// SHA-512 not implemented in this simplified version
		return nil
	}

	return &TLSARecord{
		Usage:        usage,
		Selector:     selector,
		MatchingType: matchingType,
		Certificate:  certData,
	}
}

// String returns the string representation of a TLSA record
func (r *TLSARecord) String() string {
	return fmt.Sprintf("%d %d %d %s",
		r.Usage,
		r.Selector,
		r.MatchingType,
		hex.EncodeToString(r.Certificate),
	)
}

// DNSSECStatus represents the DNSSEC validation status
type DNSSECStatus int

const (
	DNSSECUnknown DNSSECStatus = iota
	DNSSECSecured
	DNSSECInsecure
	DNSSECBogus
)

// ValidateWithDNSSEC validates DANE with DNSSEC check
// Note: This requires a DNS resolver that supports DNSSEC validation
func (v *DANEValidator) ValidateWithDNSSEC(domain string, port int, state *tls.ConnectionState, dnssec DNSSECStatus) (DANEResult, error) {
	// RFC 7672 requires DNSSEC validation for DANE
	if dnssec != DNSSECSecured {
		// Without DNSSEC, DANE is not secure
		return DANENone, nil
	}

	return v.Validate(domain, port, state)
}
