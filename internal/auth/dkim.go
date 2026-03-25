package auth

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// DKIMResult represents the result of DKIM verification
type DKIMResult int

const (
	DKIMNone      DKIMResult = iota // No DKIM signature
	DKIMPass                        // DKIM verification passed
	DKIMFail                        // DKIM verification failed
	DKIMPERMError                   // Permanent error
	DKIMTempError                   // Temporary error
)

func (r DKIMResult) String() string {
	switch r {
	case DKIMNone:
		return "none"
	case DKIMPass:
		return "pass"
	case DKIMFail:
		return "fail"
	case DKIMPERMError:
		return "permerror"
	case DKIMTempError:
		return "temperror"
	default:
		return "unknown"
	}
}

// DKIMSignature represents a parsed DKIM-Signature header
type DKIMSignature struct {
	Domain         string
	Selector       string
	Algorithm      string
	Canonicalize   string // c= header/body canonicalization
	HeaderCanon    string // simple or relaxed
	BodyCanon      string // simple or relaxed
	QueryMethod    string // q= query method
	Timestamp      int64  // t= timestamp
	Expiration     int64  // x= expiration
	SignedHeaders  []string // h= signed header fields
	BodyHash       string // bh= body hash
	Signature      string // b= signature
	BodyLength     int    // l= body length limit (-1 if not specified)
	CopiedHeaders  map[string]string // z= copied headers
	OriginalHeader string // Original header value for verification
}

// DKIMSigner handles DKIM signing and verification
type DKIMSigner struct {
	resolver DNSResolver
	privateKey *rsa.PrivateKey
	domain     string
	selector   string
}

// NewDKIMSigner creates a new DKIM signer
func NewDKIMSigner(resolver DNSResolver, privateKey *rsa.PrivateKey, domain, selector string) *DKIMSigner {
	return &DKIMSigner{
		resolver:   resolver,
		privateKey: privateKey,
		domain:     domain,
		selector:   selector,
	}
}

// DKIMVerifier handles DKIM verification
type DKIMVerifier struct {
	resolver DNSResolver
}

// NewDKIMVerifier creates a new DKIM verifier
func NewDKIMVerifier(resolver DNSResolver) *DKIMVerifier {
	return &DKIMVerifier{
		resolver: resolver,
	}
}

// Sign signs a message with DKIM
func (s *DKIMSigner) Sign(headers map[string][]string, body []byte) (string, error) {
	if s.privateKey == nil {
		return "", errors.New("no private key configured")
	}

	// Default canonicalization
	headerCanon := "relaxed"
	bodyCanon := "simple"

	// Compute body hash
	bodyHash := computeBodyHash(body, bodyCanon)

	// Determine which headers to sign (default set)
	signedHeaders := []string{"from", "to", "subject", "date", "message-id"}

	// Build signature
	sig := DKIMSignature{
		Domain:        s.domain,
		Selector:      s.selector,
		Algorithm:     "rsa-sha256",
		Canonicalize:  headerCanon + "/" + bodyCanon,
		HeaderCanon:   headerCanon,
		BodyCanon:     bodyCanon,
		QueryMethod:   "dns/txt",
		Timestamp:     0, // Set to current time in production
		SignedHeaders: signedHeaders,
		BodyHash:      bodyHash,
	}

	// Compute signature
	signature, err := s.computeSignature(headers, body, &sig)
	if err != nil {
		return "", err
	}
	sig.Signature = signature

	// Build DKIM-Signature header
	return s.buildHeader(&sig), nil
}

// Verify verifies a DKIM signature on a message
func (v *DKIMVerifier) Verify(headers map[string][]string, body []byte, dkimHeader string) (DKIMResult, *DKIMSignature, error) {
	// Parse the DKIM-Signature header
	sig, err := parseDKIMSignature(dkimHeader)
	if err != nil {
		return DKIMFail, nil, fmt.Errorf("failed to parse DKIM signature: %w", err)
	}
	sig.OriginalHeader = dkimHeader

	// Validate algorithm
	if sig.Algorithm != "rsa-sha256" {
		return DKIMFail, sig, fmt.Errorf("unsupported algorithm: %s", sig.Algorithm)
	}

	// Fetch public key from DNS
	pubKey, err := v.fetchPublicKey(sig.Domain, sig.Selector)
	if err != nil {
		if isTemporaryError(err) {
			return DKIMTempError, sig, fmt.Errorf("DNS lookup failed: %w", err)
		}
		return DKIMFail, sig, fmt.Errorf("public key not found: %w", err)
	}

	// Verify body hash
	canonicalBody := canonicalizeBody(body, sig.BodyCanon)
	if sig.BodyLength >= 0 {
		// Apply body length limit
		if len(canonicalBody) > sig.BodyLength {
			canonicalBody = canonicalBody[:sig.BodyLength]
		}
	}
	computedBodyHash := sha256Hash(canonicalBody)
	if computedBodyHash != sig.BodyHash {
		return DKIMFail, sig, fmt.Errorf("body hash mismatch")
	}

	// Verify signature
	canonicalHeaders := canonicalizeHeaders(headers, sig.SignedHeaders, sig.HeaderCanon)
	sigData := canonicalHeaders + dkimHeaderWithoutSig(dkimHeader)

	err = verifyRSASignature(pubKey, []byte(sigData), sig.Signature)
	if err != nil {
		return DKIMFail, sig, fmt.Errorf("signature verification failed: %w", err)
	}

	return DKIMPass, sig, nil
}

// parseDKIMSignature parses a DKIM-Signature header
func parseDKIMSignature(header string) (*DKIMSignature, error) {
	sig := &DKIMSignature{
		BodyLength: -1, // Default: no limit
	}

	// Normalize the header value (remove "DKIM-Signature:" prefix if present)
	header = strings.TrimPrefix(header, "DKIM-Signature:")
	header = strings.TrimSpace(header)

	// Parse tag-value pairs
	tagValues := parseTagValueList(header)

	for tag, value := range tagValues {
		switch tag {
		case "v":
			if value != "1" {
				return nil, fmt.Errorf("unsupported DKIM version: %s", value)
			}
		case "d":
			sig.Domain = value
		case "s":
			sig.Selector = value
		case "a":
			sig.Algorithm = value
		case "c":
			sig.Canonicalize = value
			parts := strings.Split(value, "/")
			if len(parts) >= 1 {
				sig.HeaderCanon = parts[0]
			}
			if len(parts) >= 2 {
				sig.BodyCanon = parts[1]
			} else {
				sig.BodyCanon = "simple" // Default
			}
		case "q":
			sig.QueryMethod = value
		case "t":
			sig.Timestamp = parseInt64(value)
		case "x":
			sig.Expiration = parseInt64(value)
		case "h":
			sig.SignedHeaders = parseHeaderList(value)
		case "bh":
			sig.BodyHash = value
		case "b":
			sig.Signature = value
		case "l":
			sig.BodyLength = parseInt(value)
		case "z":
			sig.CopiedHeaders = parseCopiedHeaders(value)
		}
	}

	// Validate required fields
	if sig.Domain == "" {
		return nil, errors.New("missing required tag: d=")
	}
	if sig.Selector == "" {
		return nil, errors.New("missing required tag: s=")
	}
	if sig.BodyHash == "" {
		return nil, errors.New("missing required tag: bh=")
	}
	if sig.Signature == "" {
		return nil, errors.New("missing required tag: b=")
	}

	// Set defaults
	if sig.Algorithm == "" {
		sig.Algorithm = "rsa-sha256"
	}
	if sig.HeaderCanon == "" {
		sig.HeaderCanon = "simple"
	}
	if sig.BodyCanon == "" {
		sig.BodyCanon = "simple"
	}

	return sig, nil
}

// parseTagValueList parses tag=value;tag=value format
func parseTagValueList(s string) map[string]string {
	result := make(map[string]string)
	// Split by semicolon, but handle b= value which can contain semicolons
	var current strings.Builder
	escaped := false

	for i, r := range s {
		if r == '\\' && i+1 < len(s) {
			escaped = true
			continue
		}
		if r == ';' && !escaped {
			// End of tag-value pair
			parsePair(current.String(), result)
			current.Reset()
		} else {
			if escaped {
				current.WriteByte('\\')
				escaped = false
			}
			current.WriteRune(r)
		}
	}
	// Parse last pair
	if current.Len() > 0 {
		parsePair(current.String(), result)
	}

	return result
}

func parsePair(s string, result map[string]string) {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "="); idx > 0 {
		tag := strings.TrimSpace(s[:idx])
		value := strings.TrimSpace(s[idx+1:])
		// Remove whitespace from b= and bh= values (folded header continuation)
		if tag == "b" || tag == "bh" {
			value = removeWhitespace(value)
		}
		result[tag] = value
	}
}

func removeWhitespace(s string) string {
	var result strings.Builder
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\r' && r != '\n' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func parseInt64(s string) int64 {
	var result int64
	for _, r := range s {
		if r >= '0' && r <= '9' {
			result = result*10 + int64(r-'0')
		}
	}
	return result
}

func parseInt(s string) int {
	return int(parseInt64(s))
}

func parseHeaderList(s string) []string {
	var result []string
	for _, h := range strings.Split(s, ":") {
		h = strings.TrimSpace(strings.ToLower(h))
		if h != "" {
			result = append(result, h)
		}
	}
	return result
}

func parseCopiedHeaders(s string) map[string]string {
	result := make(map[string]string)
	// Format: header=value|header2=value2
	pairs := strings.Split(s, "|")
	for _, pair := range pairs {
		if idx := strings.Index(pair, "="); idx > 0 {
			header := strings.TrimSpace(strings.ToLower(pair[:idx]))
			value := pair[idx+1:]
			result[header] = value
		}
	}
	return result
}

// canonicalizeBody canonicalizes the message body
func canonicalizeBody(body []byte, canon string) []byte {
	switch canon {
	case "relaxed":
		return canonicalizeBodyRelaxed(body)
	case "simple":
	default:
		return canonicalizeBodySimple(body)
	}
	return body
}

// canonicalizeBodySimple implements simple body canonicalization
// - Empty body is replaced with a single CRLF
// - Trailing CRLF is preserved
func canonicalizeBodySimple(body []byte) []byte {
	if len(body) == 0 {
		return []byte("\r\n")
	}
	// Remove trailing empty lines, but keep one CRLF if body was non-empty
	for len(body) >= 2 && body[len(body)-2] == '\r' && body[len(body)-1] == '\n' {
		if len(body) >= 4 && body[len(body)-4] == '\r' && body[len(body)-3] == '\n' {
			body = body[:len(body)-2]
		} else {
			break
		}
	}
	// If no trailing CRLF, add one
	if len(body) < 2 || body[len(body)-2] != '\r' || body[len(body)-1] != '\n' {
		body = append(body, []byte("\r\n")...)
	}
	return body
}

// canonicalizeBodyRelaxed implements relaxed body canonicalization
// - Ignores all whitespace at line endings
// - Replaces multiple whitespace within a line with single space
// - Empty body is replaced with CRLF
func canonicalizeBodyRelaxed(body []byte) []byte {
	if len(body) == 0 {
		return []byte("\r\n")
	}

	var result strings.Builder
	lines := strings.Split(string(body), "\n")

	for i, line := range lines {
		// Remove CR if present
		line = strings.TrimSuffix(line, "\r")

		// Replace multiple whitespace with single space
		re := regexp.MustCompile(`[ \t]+`)
		line = re.ReplaceAllString(line, " ")

		// Trim trailing whitespace
		line = strings.TrimRight(line, " \t")

		result.WriteString(line)
		if i < len(lines)-1 {
			result.WriteString("\r\n")
		}
	}

	// Ensure trailing CRLF
	s := result.String()
	if !strings.HasSuffix(s, "\r\n") {
		s += "\r\n"
	}

	// Remove all but one trailing CRLF
	for strings.HasSuffix(s, "\r\n\r\n") {
		s = s[:len(s)-2]
	}

	return []byte(s)
}

// canonicalizeHeaders canonicalizes headers for signing/verification
func canonicalizeHeaders(headers map[string][]string, signedHeaders []string, canon string) string {
	var result strings.Builder

	for _, headerName := range signedHeaders {
		headerNameLower := strings.ToLower(headerName)
		values := headers[headerName]

		if len(values) == 0 {
			// Try case-insensitive lookup
			for k, v := range headers {
				if strings.ToLower(k) == headerNameLower {
					values = v
					break
				}
			}
		}

		for _, value := range values {
			canonHeader := canonicalizeHeader(headerName, value, canon)
			result.WriteString(canonHeader)
		}
	}

	return result.String()
}

// canonicalizeHeader canonicalizes a single header
func canonicalizeHeader(name, value, canon string) string {
	switch canon {
	case "relaxed":
		return canonicalizeHeaderRelaxed(name, value)
	case "simple":
	default:
		return canonicalizeHeaderSimple(name, value)
	}
	return name + ": " + value
}

// canonicalizeHeaderSimple preserves header exactly as-is
func canonicalizeHeaderSimple(name, value string) string {
	return name + ": " + value + "\r\n"
}

// canonicalizeHeaderRelaxed applies relaxed header canonicalization
// - Converts header name to lowercase
// - Unfolds header continuation lines
// - Replaces multiple whitespace with single space
// - Trims leading/trailing whitespace from value
func canonicalizeHeaderRelaxed(name, value string) string {
	// Lowercase header name
	name = strings.ToLower(name)

	// Unfold continuation lines (replace CRLF WSP with single space)
	value = strings.ReplaceAll(value, "\r\n", "")
	value = strings.ReplaceAll(value, "\n", "")

	// Replace multiple whitespace with single space
	re := regexp.MustCompile(`[ \t]+`)
	value = re.ReplaceAllString(value, " ")

	// Trim leading/trailing whitespace
	value = strings.TrimSpace(value)

	return name + ":" + value + "\r\n"
}

// computeBodyHash computes the body hash
func computeBodyHash(body []byte, canon string) string {
	canonicalBody := canonicalizeBody(body, canon)
	return sha256Hash(canonicalBody)
}

func sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}

// computeSignature computes the RSA signature
func (s *DKIMSigner) computeSignature(headers map[string][]string, body []byte, sig *DKIMSignature) (string, error) {
	// Compute body hash
	canonicalBody := canonicalizeBody(body, sig.BodyCanon)
	sig.BodyHash = sha256Hash(canonicalBody)

	// Build signature data
	canonicalHeaders := canonicalizeHeaders(headers, sig.SignedHeaders, sig.HeaderCanon)

	// Build the DKIM-Signature header without the b= value
	partialHeader := s.buildHeaderWithoutSig(sig)
	sigData := canonicalHeaders + partialHeader

	// Sign with RSA-SHA256
	signature, err := signRSA(s.privateKey, []byte(sigData))
	if err != nil {
		return "", err
	}

	return signature, nil
}

// dkimHeaderWithoutSig returns the DKIM header value without the signature
func dkimHeaderWithoutSig(header string) string {
	// Remove the b= value from the header
	re := regexp.MustCompile(`b=([^;]*)`)
	return re.ReplaceAllString(header, "b=")
}

// buildHeader builds the complete DKIM-Signature header
func (s *DKIMSigner) buildHeader(sig *DKIMSignature) string {
	return "v=1; " +
		"a=" + sig.Algorithm + "; " +
		"c=" + sig.Canonicalize + "; " +
		"d=" + sig.Domain + "; " +
		"s=" + sig.Selector + "; " +
		"t=" + fmt.Sprintf("%d", sig.Timestamp) + "; " +
		"bh=" + sig.BodyHash + "; " +
		"h=" + strings.Join(sig.SignedHeaders, ":") + "; " +
		"b=" + sig.Signature
}

// buildHeaderWithoutSig builds the DKIM-Signature header without b= value
func (s *DKIMSigner) buildHeaderWithoutSig(sig *DKIMSignature) string {
	return "v=1; " +
		"a=" + sig.Algorithm + "; " +
		"c=" + sig.Canonicalize + "; " +
		"d=" + sig.Domain + "; " +
		"s=" + sig.Selector + "; " +
		"t=" + fmt.Sprintf("%d", sig.Timestamp) + "; " +
		"bh=" + sig.BodyHash + "; " +
		"h=" + strings.Join(sig.SignedHeaders, ":") + "; " +
		"b=" + "\r\n"
}

// signRSA signs data with RSA-SHA256
func signRSA(privateKey *rsa.PrivateKey, data []byte) (string, error) {
	hash := sha256.Sum256(data)
	signature, err := rsa.SignPKCS1v15(nil, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// verifyRSASignature verifies an RSA-SHA256 signature
func verifyRSASignature(publicKey *rsa.PublicKey, data []byte, signatureB64 string) error {
	signature, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	hash := sha256.Sum256(data)
	err = rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signature)
	if err != nil {
		return fmt.Errorf("RSA verification failed: %w", err)
	}

	return nil
}

// fetchPublicKey fetches the DKIM public key from DNS
func (v *DKIMVerifier) fetchPublicKey(domain, selector string) (*rsa.PublicKey, error) {
	// DNS query: selector._domainkey.domain
	query := fmt.Sprintf("%s._domainkey.%s", selector, domain)

	// Look up TXT record
	txtRecords, err := net.LookupTXT(query)
	if err != nil {
		return nil, err
	}

	for _, record := range txtRecords {
		pubKey, err := parseDKIMPublicKey(record)
		if err == nil && pubKey != nil {
			return pubKey, nil
		}
	}

	return nil, errors.New("no valid DKIM public key found")
}

// parseDKIMPublicKey parses a DKIM public key from DNS TXT record
func parseDKIMPublicKey(record string) (*rsa.PublicKey, error) {
	// Parse tag-value pairs
	tags := parseTagValueList(record)

	// Check version
	if v, ok := tags["v"]; ok && v != "DKIM1" {
		return nil, fmt.Errorf("unsupported DKIM key version: %s", v)
	}

	// Get key type
	keyType := tags["k"]
	if keyType == "" {
		keyType = "rsa" // Default
	}

	if keyType != "rsa" {
		return nil, fmt.Errorf("unsupported key type: %s", keyType)
	}

	// Get public key data
	keyData := tags["p"]
	if keyData == "" {
		return nil, errors.New("no public key data")
	}

	// Check for revoked key (empty p=)
	if keyData == "" {
		return nil, errors.New("key has been revoked")
	}

	// Decode base64 key
	keyBytes, err := base64.StdEncoding.DecodeString(keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}

	// Parse RSA public key
	pubKey, err := x509.ParsePKIXPublicKey(keyBytes)
	if err != nil {
		// Try parsing as RSA key directly
		pubKey, err = parseRSAPublicKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %w", err)
		}
	}

	rsaKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}

	return rsaKey, nil
}

// parseRSAPublicKey parses an RSA public key from raw bytes
func parseRSAPublicKey(data []byte) (*rsa.PublicKey, error) {
	// Try PEM format first
	block, _ := pem.Decode(data)
	if block != nil {
		data = block.Bytes
	}

	// Try PKIX format
	pubKey, err := x509.ParsePKIXPublicKey(data)
	if err == nil {
		if rsaKey, ok := pubKey.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
	}

	// Try PKCS1 format
	pubKey, err = x509.ParsePKCS1PublicKey(data)
	if err == nil {
		return pubKey.(*rsa.PublicKey), nil
	}

	return nil, errors.New("failed to parse RSA public key")
}

// GenerateDKIMKeyPair generates a new RSA key pair for DKIM
func GenerateDKIMKeyPair(bits int) (*rsa.PrivateKey, []byte, error) {
	privateKey, err := rsa.GenerateKey(nil, bits)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Encode public key
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	return privateKey, pubKeyBytes, nil
}

// GetPublicKeyForDNS returns the public key in base64 format suitable for DNS TXT record
func GetPublicKeyForDNS(privateKey *rsa.PrivateKey) string {
	pubKeyBytes, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	return base64.StdEncoding.EncodeToString(pubKeyBytes)
}
