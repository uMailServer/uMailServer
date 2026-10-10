package auth

// DANE (DNS-Based Authentication of Named Entities, RFC 6698) authenticates
// TLS connections using DNSSEC. Integrated into queue manager via DANEValidator
// in internal/queue/manager.go deliverToMX function after STARTTLS.

import (
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/miekg/dns"
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
	resolver  DNSResolver
	dnsServer string // Custom DNS server for TLSA lookups (defaults to system resolver)
}

// NewDANEValidator creates a new DANE validator
func NewDANEValidator(resolver DNSResolver) *DANEValidator {
	return &DANEValidator{
		resolver:  resolver,
		dnsServer: "", // Use system resolver by default
	}
}

// NewDANEValidatorWithDNS creates a new DANE validator with a custom DNS server
func NewDANEValidatorWithDNS(resolver DNSResolver, dnsServer string) *DANEValidator {
	return &DANEValidator{
		resolver:  resolver,
		dnsServer: dnsServer,
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
	usable := 0
	for _, tlsa := range tlsaRecords {
		// RFC 7672 §3.1.3: only usages 2 and 3 are used for SMTP; usage 0/1
		// and unknown selector/matching types are unusable (F6005).
		if !tlsaUsable(tlsa) {
			continue
		}
		usable++

		if tlsa.Usage == TLSAUsageDANETA {
			if v.validateTA(tlsa, domain, state) {
				return DANEValidated, nil
			}
			continue
		}

		// Validate against this record
		if v.validateRecord(tlsa, peerCert, state) {
			return DANEValidated, nil
		}
	}

	// RFC 7672 §2.2: if every TLSA record is unusable the host is treated
	// as not DANE-authenticated rather than as a validation failure (F6005).
	if usable == 0 {
		return DANEUnusable, nil
	}

	// No matching TLSA record found
	return DANEFailed, nil
}

// tlsaUsable reports whether a TLSA record uses parameters this validator
// supports for SMTP (RFC 7672 §3.1.3, RFC 7671 §5.1).
func tlsaUsable(t *TLSARecord) bool {
	if t.Usage != TLSAUsageDANETA && t.Usage != TLSAUsageDANEEE {
		return false
	}
	if t.Selector != TLSASelectorFullCert && t.Selector != TLSASelectorSPKI {
		return false
	}
	switch t.MatchingType {
	case TLSAMatchingTypeFull, TLSAMatchingTypeSHA256, TLSAMatchingTypeSHA512:
		return true
	}
	return false
}

// validateTA implements DANE-TA(2) (RFC 7671 §5.2): the record names a trust
// anchor that may be any certificate of the presented chain, and the leaf must
// chain to it and match the TLSA base domain (F6006).
func (v *DANEValidator) validateTA(tlsa *TLSARecord, domain string, state *tls.ConnectionState) bool {
	chain := state.PeerCertificates
	for i, c := range chain {
		if !tlsaMatchesCert(tlsa, c) {
			continue
		}
		if i == 0 {
			// The leaf itself is the named anchor.
			return true
		}
		roots := x509.NewCertPool()
		roots.AddCert(c)
		inter := x509.NewCertPool()
		for j := 1; j < len(chain); j++ {
			if j != i {
				inter.AddCert(chain[j])
			}
		}
		if _, err := chain[0].Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: inter,
			DNSName:       strings.TrimSuffix(domain, "."),
		}); err == nil {
			return true
		}
	}
	return false
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
	// Use resolver's TLSA lookup if available
	if v.resolver != nil {
		if tlsaResolver, ok := v.resolver.(TLSAResolver); ok {
			return tlsaResolver.LookupTLSA(query)
		}
	}

	// Fallback: use miekg/dns for proper TLSA (type 52) lookups
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(query), dns.TypeTLSA)
	msg.SetEdns0(4096, true) // DO bit: ask for DNSSEC data

	// Use configured DNS server or the system resolver. miekg/dns does not
	// read resolv.conf itself, so an empty address always failed (F6003).
	resolverAddr := v.dnsServer
	if resolverAddr == "" {
		cfg, cerr := dns.ClientConfigFromFile("/etc/resolv.conf")
		if cerr != nil || len(cfg.Servers) == 0 {
			return nil, errors.New("TLSA lookup failed: no system DNS resolver configured")
		}
		resolverAddr = net.JoinHostPort(cfg.Servers[0], cfg.Port)
	}

	reply, _, err := (&dns.Client{Net: "udp"}).Exchange(msg, resolverAddr)
	// A truncated UDP reply (common for full-certificate TLSA data) or a UDP
	// error is retried over TCP (F6004).
	if err != nil || (reply != nil && reply.Truncated) {
		reply, _, err = (&dns.Client{Net: "tcp"}).Exchange(msg, resolverAddr)
		if err != nil {
			return nil, fmt.Errorf("TLSA lookup failed: %w", err)
		}
	}

	// Only NOERROR and NXDOMAIN are definitive answers; SERVFAIL (e.g. a
	// DNSSEC-bogus zone) must not read as "no TLSA records", which would
	// silently downgrade DANE (F6002).
	if reply.Rcode != dns.RcodeSuccess && reply.Rcode != dns.RcodeNameError {
		return nil, fmt.Errorf("TLSA lookup failed: %s", dns.RcodeToString[reply.Rcode])
	}

	var records []*TLSARecord
	for _, rr := range reply.Answer {
		if tlsaRR, ok := rr.(*dns.TLSA); ok {
			// F5272: miekg/dns carries the association data as a hex
			// string; decode it to raw bytes. Undecodable data is an
			// unusable record and is skipped.
			data, err := hex.DecodeString(tlsaRR.Certificate)
			if err != nil {
				continue
			}
			records = append(records, &TLSARecord{
				Usage:        TLSAUsage(tlsaRR.Usage),
				Selector:     TLSASelector(tlsaRR.Selector),
				MatchingType: TLSAMatchingType(tlsaRR.MatchingType),
				Certificate:  data,
			})
		}
	}

	return records, nil
}

// TLSAResolver interface for DNS resolvers that support TLSA lookups
type TLSAResolver interface {
	LookupTLSA(domain string) ([]*TLSARecord, error)
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
	return tlsaMatchesCert(tlsa, cert)
}

// tlsaMatchesCert applies the TLSA selector and matching type to cert and
// compares the result with the record's association data.
func tlsaMatchesCert(tlsa *TLSARecord, cert *x509.Certificate) bool {
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
		// SHA-512 hash
		hash := sha512.Sum512(dataToMatch)
		computedData = hash[:]
	default:
		// Unsupported matching type
		return false
	}

	// Compare using constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare(computedData, tlsa.Certificate) == 1
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
		hash := sha512.Sum512(dataToMatch)
		certData = hash[:]
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

// ValidateWithDNSSEC validates DANE with DNSSEC check.
// RFC 7672 requires DNSSEC for DANE to provide security; without DNSSEC
// the TLSA records are not trustworthy.
func (v *DANEValidator) ValidateWithDNSSEC(domain string, port int, state *tls.ConnectionState, dnssec DNSSECStatus) (DANEResult, error) {
	// RFC 7672 requires DNSSEC validation for DANE
	if dnssec != DNSSECSecured {
		// Without DNSSEC, DANE is not secure
		return DANENone, nil
	}

	return v.Validate(domain, port, state)
}
