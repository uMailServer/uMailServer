package auth

// ARC (Authenticated Received Chain, RFC 8617) provides authentication results
// for messages relayed through intermediaries. Integrated into SMTP pipeline
// via internal/smtp/auth_pipeline.go AuthARCStage.

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ARCResult represents the result of ARC validation
type ARCResult int

const (
	ARCNone      ARCResult = iota // No ARC seal present
	ARCPass                       // ARC chain validated
	ARCFail                       // ARC chain failed
	ARCPermError                  // Permanent error
	ARCTempError                  // Temporary error
)

func (r ARCResult) String() string {
	switch r {
	case ARCNone:
		return "none"
	case ARCPass:
		return "pass"
	case ARCFail:
		return "fail"
	case ARCPermError:
		return "permerror"
	case ARCTempError:
		return "temperror"
	default:
		return "unknown"
	}
}

// ARCSet represents one ARC set (instance) in the chain
type ARCSet struct {
	Instance              int    // i= ARC instance number
	AAR                   string // ARC-Authentication-Results header
	AMS                   string // ARC-Message-Signature header
	AS                    string // ARC-Seal header
	Validated             bool
	MessageSignatureValid bool
	SealSignatureValid    bool
}

// ARCChain represents the complete ARC chain
type ARCChain struct {
	Sets         []ARCSet
	ChainValid   bool
	ChainLength  int
	CV           string // cv= chain validation status (none/fail/pass)
	SealDomain   string
	SealSelector string
}

// ARCValidator handles ARC chain validation
type ARCValidator struct {
	resolver DNSResolver
}

// ARCSigner handles ARC signing for forwarders
type ARCSigner struct {
	resolver   DNSResolver
	privateKey *rsa.PrivateKey
	domain     string
	selector   string
}

// NewARCValidator creates a new ARC validator
func NewARCValidator(resolver DNSResolver) *ARCValidator {
	return &ARCValidator{
		resolver: resolver,
	}
}

// NewARCSigner creates a new ARC signer
func NewARCSigner(resolver DNSResolver, privateKey *rsa.PrivateKey, domain, selector string) *ARCSigner {
	return &ARCSigner{
		resolver:   resolver,
		privateKey: privateKey,
		domain:     domain,
		selector:   selector,
	}
}

// Validate validates the ARC chain in message headers
func (v *ARCValidator) Validate(ctx context.Context, headers map[string][]string, body []byte) (*ARCChain, error) {
	chain := &ARCChain{
		Sets: make([]ARCSet, 0),
		CV:   "none",
	}

	// Extract all ARC headers
	arcHeaders := extractARCHeaders(headers)
	if len(arcHeaders) == 0 {
		return chain, nil // No ARC headers
	}

	// Group headers by instance number
	arcSets := groupARCHeaders(arcHeaders)
	chain.ChainLength = len(arcSets)

	// Validate each ARC set in order
	for i := 1; i <= len(arcSets); i++ {
		arcSet := arcSets[i]
		arcSet.Instance = i

		// Validate ARC-Message-Signature (AMS)
		amsValid, err := v.validateAMS(ctx, arcSet.AMS, headers, body)
		if err != nil {
			if isTemporaryError(err) {
				return nil, err
			}
			chain.CV = "fail"
			chain.ChainValid = false
			return chain, nil
		}
		arcSet.MessageSignatureValid = amsValid

		// Validate ARC-Seal (AS)
		asValid, err := v.validateAS(ctx, arcSet.AS, headers, i)
		if err != nil {
			if isTemporaryError(err) {
				return nil, err
			}
			chain.CV = "fail"
			chain.ChainValid = false
			return chain, nil
		}
		arcSet.SealSignatureValid = asValid

		// A set is valid if both signatures are valid
		arcSet.Validated = amsValid && asValid
		chain.Sets = append(chain.Sets, arcSet)

		// Extract seal info from the last valid set
		if arcSet.Validated {
			chain.CV = "pass"
			chain.SealDomain, chain.SealSelector = extractSealInfo(arcSet.AS)
		}
	}

	// Determine overall chain validity
	chain.ChainValid = chain.CV == "pass"

	return chain, nil
}

// Sign adds a new ARC set to the message for forwarding
func (s *ARCSigner) Sign(headers map[string][]string, body []byte, authResults string, instance int) (*ARCSet, error) {
	if s.privateKey == nil {
		return nil, errors.New("no private key configured")
	}

	// Determine chain validation status from existing ARC headers
	cv := determineCV(headers)

	// Create AAR (ARC-Authentication-Results)
	aar := fmt.Sprintf("i=%d; %s", instance, authResults)

	// Create AMS (ARC-Message-Signature)
	ams, err := s.createAMS(headers, body, instance)
	if err != nil {
		return nil, fmt.Errorf("failed to create AMS: %w", err)
	}

	// Create AS (ARC-Seal)
	as, err := s.createAS(headers, cv, instance)
	if err != nil {
		return nil, fmt.Errorf("failed to create AS: %w", err)
	}

	arcSet := &ARCSet{
		Instance: instance,
		AAR:      aar,
		AMS:      ams,
		AS:       as,
	}

	return arcSet, nil
}

// headerEntry represents a single header entry
type headerEntry struct {
	Name  string
	Value string
}

// extractARCHeaders extracts all ARC-related headers from message
func extractARCHeaders(headers map[string][]string) []headerEntry {
	var arcHeaders []headerEntry

	for name, values := range headers {
		lowerName := strings.ToLower(name)
		if lowerName == "arc-authentication-results" ||
			lowerName == "arc-message-signature" ||
			lowerName == "arc-seal" {
			for _, value := range values {
				arcHeaders = append(arcHeaders, headerEntry{
					Name:  lowerName,
					Value: value,
				})
			}
		}
	}

	return arcHeaders
}

// groupARCHeaders groups ARC headers by instance number
func groupARCHeaders(headers []headerEntry) map[int]ARCSet {
	sets := make(map[int]ARCSet)

	for _, h := range headers {
		instance := extractInstance(h.Value)
		if instance == 0 {
			continue
		}

		arcSet := sets[instance]
		arcSet.Instance = instance

		switch h.Name {
		case "arc-authentication-results":
			arcSet.AAR = h.Value
		case "arc-message-signature":
			arcSet.AMS = h.Value
		case "arc-seal":
			arcSet.AS = h.Value
		}

		sets[instance] = arcSet
	}

	return sets
}

// extractInstance extracts the instance number from an ARC header
func extractInstance(header string) int {
	// Look for i=N; at the start of the header
	if strings.HasPrefix(header, "i=") {
		end := strings.Index(header, ";")
		if end > 0 {
			instance, err := strconv.Atoi(header[2:end])
			if err == nil {
				return instance
			}
		}
	}
	return 0
}

// validateAMS validates the ARC-Message-Signature
func (v *ARCValidator) validateAMS(ctx context.Context, ams string, headers map[string][]string, body []byte) (bool, error) {
	if ams == "" {
		return false, nil
	}

	// Parse the AMS header
	params := parseTagValueList(ams)

	// Get signature data
	signature := params["b"]
	if signature == "" {
		return false, nil
	}

	// Get domain and selector
	domain := params["d"]
	selector := params["s"]
	if domain == "" || selector == "" {
		return false, nil
	}

	// Fetch public key from DNS
	pubKey, err := fetchARCPublicKey(v.resolver, domain, selector)
	if err != nil {
		return false, err
	}

	// Verify signature
	sigData := buildAMSSignatureData(ams, headers, body)
	err = verifyRSASignature(pubKey, sigData, signature)
	return err == nil, nil
}

// validateAS validates the ARC-Seal
func (v *ARCValidator) validateAS(ctx context.Context, as string, headers map[string][]string, instance int) (bool, error) {
	if as == "" {
		return false, nil
	}

	// Parse the AS header
	params := parseTagValueList(as)

	// Get signature data
	signature := params["b"]
	if signature == "" {
		return false, nil
	}

	// Get domain and selector
	domain := params["d"]
	selector := params["s"]
	if domain == "" || selector == "" {
		return false, nil
	}

	// Fetch public key from DNS
	pubKey, err := fetchARCPublicKey(v.resolver, domain, selector)
	if err != nil {
		return false, err
	}

	// Verify signature
	// In a full implementation, this would include the previous ARC headers
	sigData := []byte(as)
	err = verifyRSASignature(pubKey, sigData, signature)
	return err == nil, nil
}

// fetchARCPublicKey fetches the ARC public key from DNS
func fetchARCPublicKey(resolver DNSResolver, domain, selector string) (*rsa.PublicKey, error) {
	// Use same DNS query format as DKIM: selector._domainkey.domain
	query := fmt.Sprintf("%s._domainkey.%s", selector, domain)

	// Look up TXT record
	txtRecords, err := resolver.LookupTXT(context.Background(), query)
	if err != nil {
		return nil, err
	}

	for _, record := range txtRecords {
		pubKey, err := parseDKIMPublicKey(record)
		if err == nil && pubKey != nil {
			return pubKey, nil
		}
	}

	return nil, errors.New("no valid ARC public key found")
}

// createAMS creates an ARC-Message-Signature header
func (s *ARCSigner) createAMS(headers map[string][]string, body []byte, instance int) (string, error) {
	// Simplified AMS creation
	// Create canonicalized body hash
	bodyHash := computeBodyHash(body, "relaxed")

	// Build AMS header without signature
	ams := fmt.Sprintf("i=%d; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; t=%d; bh=%s; h=from:to:subject:date:message-id; b=",
		instance,
		s.domain,
		s.selector,
		0, // timestamp
		bodyHash,
	)

	// Sign the AMS
	sigData := buildAMSSignatureData(ams, headers, body)
	signature, err := signRSA(s.privateKey, sigData)
	if err != nil {
		return "", err
	}

	// Append signature
	ams += signature

	return ams, nil
}

// createAS creates an ARC-Seal header
func (s *ARCSigner) createAS(headers map[string][]string, cv string, instance int) (string, error) {
	// Build AS header without signature
	as := fmt.Sprintf("i=%d; a=rsa-sha256; d=%s; s=%s; cv=%s; b=",
		instance,
		s.domain,
		s.selector,
		cv,
	)

	// Sign the AS
	// In a real implementation, this would hash the previous ARC set headers
	sigData := []byte(as)
	signature, err := signRSA(s.privateKey, sigData)
	if err != nil {
		return "", err
	}

	// Append signature
	as += signature

	return as, nil
}

// buildAMSSignatureData builds the data to be signed for AMS
func buildAMSSignatureData(ams string, headers map[string][]string, body []byte) []byte {
	// Simplified - in production this would properly canonicalize headers and body
	var data strings.Builder

	// Add canonicalized headers
	for name, values := range headers {
		for _, value := range values {
			data.WriteString(fmt.Sprintf("%s: %s\r\n", strings.ToLower(name), value))
		}
	}

	// Add body
	data.Write(body)

	return []byte(data.String())
}

// extractSealInfo extracts domain and selector from ARC-Seal
func extractSealInfo(as string) (string, string) {
	params := parseTagValueList(as)
	return params["d"], params["s"]
}

// determineCV determines the chain validation status from existing ARC headers
func determineCV(headers map[string][]string) string {
	// Look for the highest instance ARC-Seal and get its cv value
	arcSeals := headers["arc-seal"]
	if len(arcSeals) == 0 {
		arcSeals = headers["ARC-Seal"]
	}

	if len(arcSeals) == 0 {
		return "none"
	}

	// Get the last ARC-Seal (highest instance)
	lastSeal := arcSeals[len(arcSeals)-1]
	params := parseTagValueList(lastSeal)
	cv := params["cv"]

	if cv == "" {
		return "none"
	}

	return cv
}
