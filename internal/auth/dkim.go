package auth

// DKIM key management utilities.
// GenerateDKIMKeyPair is wired into the CLI via cmdDomain "domain add".
// GetPublicKeyForDNS is used by the same CLI to display DNS records.

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
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
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
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

// Package-level precompiled regex for canonicalization (hot path optimization)
var (
	whitespaceRegex = regexp.MustCompile(`[ \t]+`)
	bTagRegex       = regexp.MustCompile(`b=([^;]*)`)
)

// DKIMSignature represents a parsed DKIM-Signature header
type DKIMSignature struct {
	Domain         string
	Selector       string
	Algorithm      string
	Canonicalize   string            // c= header/body canonicalization
	HeaderCanon    string            // simple or relaxed
	BodyCanon      string            // simple or relaxed
	QueryMethod    string            // q= query method
	Timestamp      int64             // t= timestamp
	Expiration     int64             // x= expiration
	SignedHeaders  []string          // h= signed header fields
	BodyHash       string            // bh= body hash
	Signature      string            // b= signature
	BodyLength     int               // l= body length limit (-1 if not specified)
	CopiedHeaders  map[string]string // z= copied headers
	OriginalHeader string            // Original header value for verification
}

// DKIMSigner handles DKIM signing and verification
type DKIMSigner struct {
	resolver     DNSResolver
	privateKey   any // *rsa.PrivateKey or ed25519.PrivateKey
	domain       string
	selector     string
	keyAlgorithm string // "rsa-sha256" or "ed25519-sha256"
}

// NewDKIMSigner creates a new DKIM signer with RSA key
func NewDKIMSigner(resolver DNSResolver, privateKey *rsa.PrivateKey, domain, selector string) *DKIMSigner {
	return &DKIMSigner{
		resolver:     resolver,
		privateKey:   privateKey,
		domain:       domain,
		selector:     selector,
		keyAlgorithm: "rsa-sha256",
	}
}

// NewDKIMSignerEd25519 creates a new DKIM signer with Ed25519 key
func NewDKIMSignerEd25519(resolver DNSResolver, privateKey ed25519.PrivateKey, domain, selector string) *DKIMSigner {
	return &DKIMSigner{
		resolver:     resolver,
		privateKey:   privateKey,
		domain:       domain,
		selector:     selector,
		keyAlgorithm: "ed25519-sha256",
	}
}

// DKIMVerifier handles DKIM verification
type DKIMVerifier struct {
	resolver DNSResolver
	cache    *dkimCache
}

// dkimCacheEntry represents a cached DKIM public key
type dkimCacheEntry struct {
	key        *rsa.PublicKey
	ed25519Key ed25519.PublicKey
	expiresAt  time.Time
}

// dkimCache caches DKIM public key lookups
type dkimCache struct {
	entries map[string]*dkimCacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
	maxSize int
}

// newDKIMCache creates a new DKIM cache
func newDKIMCache() *dkimCache {
	return &dkimCache{
		entries: make(map[string]*dkimCacheEntry),
		ttl:     5 * time.Minute,
		maxSize: 10000,
	}
}

// get retrieves a cached key
func (c *dkimCache) get(selector, domain string) (*rsa.PublicKey, ed25519.PublicKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := selector + "._domainkey." + domain
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, nil, false
	}
	return entry.key, entry.ed25519Key, true
}

// set stores a key in the cache
func (c *dkimCache) set(selector, domain string, rsaKey *rsa.PublicKey, ed25519Key ed25519.PublicKey) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	key := selector + "._domainkey." + domain
	c.entries[key] = &dkimCacheEntry{
		key:        rsaKey,
		ed25519Key: ed25519Key,
		expiresAt:  time.Now().Add(c.ttl),
	}
}

// evictOldest removes expired entries, then random entries if still full
func (c *dkimCache) evictOldest() {
	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}

	// If still at capacity, remove random entry
	if len(c.entries) >= c.maxSize {
		for key := range c.entries {
			delete(c.entries, key)
			break
		}
	}
}

// NewDKIMVerifier creates a new DKIM verifier
func NewDKIMVerifier(resolver DNSResolver) *DKIMVerifier {
	return &DKIMVerifier{
		resolver: resolver,
		cache:    newDKIMCache(),
	}
}

// Sign signs a message with DKIM
func (s *DKIMSigner) Sign(headers map[string][]string, body []byte) (string, error) {
	// Check if privateKey is nil (handles both nil interface and nil pointer)
	if s.privateKey == nil {
		return "", errors.New("no private key configured")
	}

	// For RSA key, check if the underlying pointer is nil
	if s.keyAlgorithm == "rsa-sha256" {
		if pk, ok := s.privateKey.(*rsa.PrivateKey); !ok || pk == nil {
			return "", errors.New("no private key configured")
		}
	}
	// For Ed25519 key, check if the underlying pointer is nil
	if s.keyAlgorithm == "ed25519-sha256" {
		if pk, ok := s.privateKey.(ed25519.PrivateKey); !ok || pk == nil || len(pk) == 0 {
			return "", errors.New("no private key configured")
		}
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
		Algorithm:     s.keyAlgorithm,
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
	if sig.Algorithm != "rsa-sha256" && sig.Algorithm != "ed25519-sha256" {
		return DKIMFail, sig, fmt.Errorf("unsupported algorithm: %s", sig.Algorithm)
	}

	// Fetch public key from DNS
	pubKey, keyType, err := v.fetchPublicKey(sig.Domain, sig.Selector)
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

	switch keyType {
	case "ed25519":
		err = verifyEd25519Signature(pubKey.(ed25519.PublicKey), []byte(sigData), sig.Signature)
	case "rsa":
		err = verifyRSASignature(pubKey.(*rsa.PublicKey), []byte(sigData), sig.Signature)
	default:
		return DKIMFail, sig, fmt.Errorf("unsupported key type: %s", keyType)
	}
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

// canonicalizeBodyRelaxed implements RFC 6376 §3.4.4 relaxed body
// canonicalization:
//   - Reduce all sequences of WSP (SP/TAB) within a line to a single SP.
//   - Strip trailing WSP from each line.
//   - Strip trailing empty lines (i.e. collapse multiple trailing CRLF runs
//     down to exactly one CRLF when the body is non-empty).
//   - Empty body becomes a single CRLF.
//
// This implementation walks `body` byte-by-byte without splitting into a
// `[]string`; it does not allocate per-line and never converts the body to a
// string. For a 50 MB message this avoids the previous O(N) string-slice
// allocation and the regex replacement cost on every signed delivery.
func canonicalizeBodyRelaxed(body []byte) []byte {
	if len(body) == 0 {
		return []byte("\r\n")
	}

	// Output is bounded above by len(body)+2 — collapse/trim only shrinks.
	out := make([]byte, 0, len(body)+2)

	i := 0
	isFirst := true
	for {
		// Carve off the next line up to the nearest '\n' (or EOF).
		end := i
		for end < len(body) && body[end] != '\n' {
			end++
		}
		line := body[i:end]
		// Drop the trailing CR of a CRLF terminator if present.
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		// Insert the line separator BEFORE every line except the first so
		// the trailing-CRLF post-processing below still works for input that
		// did or didn't end with a newline.
		if !isFirst {
			out = append(out, '\r', '\n')
		}
		isFirst = false

		// Canonicalize the line: collapse WSP runs to a single SP and drop
		// trailing WSP. A leading WSP run is preserved as a single SP per
		// RFC (we emit the SP when the next non-WSP byte arrives).
		inWS := false
		for _, b := range line {
			if b == ' ' || b == '\t' {
				inWS = true
				continue
			}
			if inWS {
				out = append(out, ' ')
				inWS = false
			}
			out = append(out, b)
		}
		// `inWS` true at end-of-line ⇒ trailing WSP; naturally discarded.

		if end >= len(body) {
			break
		}
		i = end + 1
	}

	// Ensure exactly one trailing CRLF (matches the original post-processing
	// loop). The body is non-empty here so we always end with at most one.
	if !endsWithCRLF(out) {
		out = append(out, '\r', '\n')
	}
	for hasDoubleCRLFSuffix(out) {
		out = out[:len(out)-2]
	}
	return out
}

// endsWithCRLF reports whether the byte slice ends with CRLF.
func endsWithCRLF(b []byte) bool {
	return len(b) >= 2 && b[len(b)-2] == '\r' && b[len(b)-1] == '\n'
}

// hasDoubleCRLFSuffix reports whether the byte slice ends with "\r\n\r\n".
func hasDoubleCRLFSuffix(b []byte) bool {
	return len(b) >= 4 &&
		b[len(b)-4] == '\r' && b[len(b)-3] == '\n' &&
		b[len(b)-2] == '\r' && b[len(b)-1] == '\n'
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
	value = whitespaceRegex.ReplaceAllString(value, " ")

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

	// Sign based on algorithm
	var signature string
	var err error

	switch sig.Algorithm {
	case "ed25519-sha256":
		signature, err = signEd25519(s.privateKey.(ed25519.PrivateKey), []byte(sigData))
	case "rsa-sha256":
		signature, err = signRSA(s.privateKey.(*rsa.PrivateKey), []byte(sigData))
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", sig.Algorithm)
	}
	if err != nil {
		return "", err
	}

	return signature, nil
}

// dkimHeaderWithoutSig returns the DKIM header value without the signature
func dkimHeaderWithoutSig(header string) string {
	// Remove the b= value from the header
	return bTagRegex.ReplaceAllString(header, "b=")
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
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// signEd25519 signs data using Ed25519
func signEd25519(privateKey ed25519.PrivateKey, data []byte) (string, error) {
	signature := ed25519.Sign(privateKey, data)
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

// verifyEd25519Signature verifies an Ed25519 signature
func verifyEd25519Signature(publicKey ed25519.PublicKey, data []byte, signatureB64 string) error {
	signature, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	if !ed25519.Verify(publicKey, data, signature) {
		return fmt.Errorf("ed25519 verification failed")
	}

	return nil
}

// fetchPublicKey fetches the DKIM public key from DNS
func (v *DKIMVerifier) fetchPublicKey(domain, selector string) (any, string, error) {
	// Check cache first
	if rsaKey, ed25519Key, ok := v.cache.get(selector, domain); ok {
		if rsaKey != nil {
			metrics.Get().DKIMCacheHit()
			return rsaKey, "rsa", nil
		}
		if ed25519Key != nil {
			metrics.Get().DKIMCacheHit()
			return ed25519Key, "ed25519", nil
		}
	}

	metrics.Get().DKIMCacheMiss()

	// DNS query: selector._domainkey.domain
	query := fmt.Sprintf("%s._domainkey.%s", selector, domain)

	// Look up TXT record
	txtRecords, err := net.LookupTXT(query)
	if err != nil {
		return nil, "", err
	}

	for _, record := range txtRecords {
		pubKey, keyType, err := parseDKIMPublicKey(record)
		if err == nil && pubKey != nil {
			// Cache the key
			switch keyType {
			case "rsa":
				v.cache.set(selector, domain, pubKey.(*rsa.PublicKey), nil)
			case "ed25519":
				v.cache.set(selector, domain, nil, pubKey.(ed25519.PublicKey))
			}
			return pubKey, keyType, nil
		}
	}

	return nil, "", errors.New("no valid DKIM public key found")
}

// parseDKIMPublicKey parses a DKIM public key from DNS TXT record
func parseDKIMPublicKey(record string) (any, string, error) {
	// Parse tag-value pairs
	tags := parseTagValueList(record)

	// Check version
	if v, ok := tags["v"]; ok && v != "DKIM1" {
		return nil, "", fmt.Errorf("unsupported DKIM key version: %s", v)
	}

	// Get key type
	keyType := tags["k"]
	if keyType == "" {
		keyType = "rsa" // Default
	}

	// Get public key data
	keyData := tags["p"]
	if keyData == "" {
		return nil, "", errors.New("no public key data")
	}

	// Decode base64 key
	keyBytes, err := base64.StdEncoding.DecodeString(keyData)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode key: %w", err)
	}

	switch keyType {
	case "ed25519":
		// Ed25519 public key is 32 bytes
		if len(keyBytes) != 32 {
			return nil, "", fmt.Errorf("invalid ed25519 key length: %d", len(keyBytes))
		}
		return ed25519.PublicKey(keyBytes), "ed25519", nil

	case "rsa":
		// Parse RSA public key
		pubKey, err := x509.ParsePKIXPublicKey(keyBytes)
		if err != nil {
			// Try parsing as RSA key directly
			pubKey, err = parseRSAPublicKey(keyBytes)
			if err != nil {
				return nil, "", fmt.Errorf("failed to parse public key: %w", err)
			}
		}

		rsaKey, ok := pubKey.(*rsa.PublicKey)
		if !ok {
			return nil, "", errors.New("not an RSA public key")
		}
		return rsaKey, "rsa", nil

	default:
		return nil, "", fmt.Errorf("unsupported key type: %s", keyType)
	}
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
	if bits < 2048 {
		return nil, nil, fmt.Errorf("DKIM key size must be at least 2048 bits, got %d", bits)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
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

// GenerateEd25519DKIMKeyPair generates a new Ed25519 key pair for DKIM
func GenerateEd25519DKIMKeyPair() (ed25519.PrivateKey, []byte, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}

	return privateKey, publicKey, nil
}

// GetPublicKeyForDNS returns the public key in base64 format suitable for DNS TXT record
func GetPublicKeyForDNS(privateKey *rsa.PrivateKey) string {
	pubKeyBytes, _ := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	return base64.StdEncoding.EncodeToString(pubKeyBytes)
}

// GetEd25519PublicKeyForDNS returns the Ed25519 public key in base64 format suitable for DNS TXT record
func GetEd25519PublicKeyForDNS(privateKey ed25519.PrivateKey) string {
	// Ed25519 private key: first 32 bytes are the seed, last 32 bytes are the public key
	pubKey := privateKey.Public()
	return base64.StdEncoding.EncodeToString([]byte(pubKey.(ed25519.PublicKey)))
}
