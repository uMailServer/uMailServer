package auth

// ARC (Authenticated Received Chain, RFC 8617) provides authentication results
// for messages relayed through intermediaries. Integrated into SMTP pipeline
// via internal/smtp/auth_pipeline.go AuthARCStage.

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// arcMaxInstance is the highest legal ARC instance number (RFC 8617 §4.2.1).
const arcMaxInstance = 50

// arcSignedHeaderCandidates lists, in signing order, the non-ARC header fields
// the AMS covers when present in the message (RFC 8617 §4.1.2).
var arcSignedHeaderCandidates = []string{
	"from", "to", "cc", "subject", "date", "message-id", "in-reply-to",
	"references", "mime-version", "content-type", "content-transfer-encoding",
	"list-id", "reply-to", "sender", "dkim-signature",
}

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

// Validate validates the ARC chain in message headers (RFC 8617 §5.2).
func (v *ARCValidator) Validate(ctx context.Context, headers map[string][]string, body []byte) (*ARCChain, error) {
	chain := &ARCChain{
		Sets: make([]ARCSet, 0),
		CV:   "none",
	}

	arcHeaders := extractARCHeaders(headers)
	if len(arcHeaders) == 0 {
		return chain, nil // No ARC headers
	}

	arcSets := groupARCHeaders(arcHeaders)
	chain.ChainLength = len(arcSets)

	structOK := arcStructureValid(arcHeaders, arcSets)

	instances := make([]int, 0, len(arcSets))
	for inst := range arcSets {
		instances = append(instances, inst)
	}
	sort.Ints(instances)
	highest := 0
	if len(instances) > 0 {
		highest = instances[len(instances)-1]
	}

	chainOK := structOK && len(instances) <= arcMaxInstance
	for idx, i := range instances {
		arcSet := arcSets[i]
		arcSet.Instance = i
		// Instances must be exactly 1..N.
		if i != idx+1 || i > arcMaxInstance {
			chainOK = false
		}

		amsValid, err := v.validateAMS(ctx, arcSet.AMS, headers, body)
		if err != nil && i == highest {
			if isTemporaryError(err) {
				return nil, err
			}
			amsValid = false
		}
		arcSet.MessageSignatureValid = amsValid

		asValid, err := v.validateAS(ctx, arcSet.AS, headers, i)
		if err != nil {
			if isTemporaryError(err) {
				return nil, err
			}
			asValid = false
		}
		arcSet.SealSignatureValid = asValid

		// Only the newest AMS must verify (earlier ones are routinely
		// invalidated by intermediaries); every seal must.
		arcSet.Validated = asValid && (amsValid || i != highest)
		chain.Sets = append(chain.Sets, arcSet)

		if !asValid || (i == highest && !amsValid) {
			chainOK = false
		}
		cv := strings.ToLower(parseTagValueList(arcSet.AS)["cv"])
		if i == 1 && cv != "none" {
			chainOK = false
		}
		if i > 1 && cv != "pass" {
			chainOK = false
		}
	}

	if chainOK {
		chain.CV = "pass"
		chain.ChainValid = true
		last := chain.Sets[len(chain.Sets)-1]
		chain.SealDomain, chain.SealSelector = extractSealInfo(last.AS)
	} else {
		chain.CV = "fail"
		chain.ChainValid = false
	}

	return chain, nil
}

// arcStructureValid reports whether every ARC header carries a valid instance
// and each instance has exactly one AAR, one AMS and one AS.
func arcStructureValid(all []headerEntry, sets map[int]ARCSet) bool {
	type counts struct{ aar, ams, as int }
	c := make(map[int]*counts)
	for _, h := range all {
		inst := extractInstance(h.Value)
		if inst == 0 {
			return false
		}
		if c[inst] == nil {
			c[inst] = &counts{}
		}
		switch h.Name {
		case "arc-authentication-results":
			c[inst].aar++
		case "arc-message-signature":
			c[inst].ams++
		case "arc-seal":
			c[inst].as++
		}
	}
	for inst, n := range c {
		if n.aar != 1 || n.ams != 1 || n.as != 1 {
			return false
		}
		if _, ok := sets[inst]; !ok {
			return false
		}
	}
	return true
}

// chainCVForSigning computes the cv= value the next seal must carry: the
// result of validating the existing chain (RFC 8617 §5.1.2).
func (s *ARCSigner) chainCVForSigning(headers map[string][]string, body []byte) (string, error) {
	if len(extractARCHeaders(headers)) == 0 {
		return "none", nil
	}
	if s.resolver == nil {
		return "fail", nil
	}
	chain, err := NewARCValidator(s.resolver).Validate(context.Background(), headers, body)
	if err != nil {
		return "", err
	}
	if chain.CV == "pass" {
		return "pass", nil
	}
	return "fail", nil
}

// Sign creates a new ARC set for forwarding.
func (s *ARCSigner) Sign(headers map[string][]string, body []byte, authResults string, instance int) (*ARCSet, error) {
	return s.signWithCV(headers, body, authResults, instance, "")
}

// signWithCV is Sign with an explicit chain-validation result. A non-empty cv
// (one of none/pass/fail, obtained by validating the chain as RECEIVED, before
// the forwarder modified the message) is used verbatim; empty means validate
// the headers passed in.
func (s *ARCSigner) signWithCV(headers map[string][]string, body []byte, authResults string, instance int, cv string) (*ARCSet, error) {
	if s.privateKey == nil {
		return nil, errors.New("no private key configured")
	}
	if instance < 1 || instance > arcMaxInstance {
		return nil, fmt.Errorf("invalid ARC instance %d", instance)
	}
	if next := determineNextInstance(headers); instance != next {
		return nil, fmt.Errorf("ARC instance %d does not follow existing chain (expected %d)", instance, next)
	}

	if cv == "" {
		var err error
		cv, err = s.chainCVForSigning(headers, body)
		if err != nil {
			return nil, fmt.Errorf("failed to validate existing ARC chain: %w", err)
		}
	} else if cv != "none" && cv != "pass" && cv != "fail" {
		return nil, fmt.Errorf("invalid cv value %q", cv)
	}

	// Prevent header injection through the caller-supplied results.
	authResults = strings.NewReplacer("\r", " ", "\n", " ").Replace(authResults)
	aar := fmt.Sprintf("i=%d; %s", instance, strings.TrimSpace(authResults))

	ams, err := s.createAMS(headers, body, instance)
	if err != nil {
		return nil, fmt.Errorf("failed to create AMS: %w", err)
	}

	as, err := s.createASWith(headers, aar, ams, cv, instance)
	if err != nil {
		return nil, fmt.Errorf("failed to create AS: %w", err)
	}

	return &ARCSet{Instance: instance, AAR: aar, AMS: ams, AS: as}, nil
}

// Seal adds ARC headers to the message for forwarding/relaying and returns
// the new header map.
func (s *ARCSigner) Seal(headers map[string][]string, body []byte, authResults string) (map[string][]string, error) {
	return s.SealWithCV(headers, body, authResults, "")
}

// SealWithCV is Seal for forwarders that modify the message: pass the cv
// (ARCChain.CV) obtained by validating the message as received. An empty cv
// validates the supplied headers instead.
func (s *ARCSigner) SealWithCV(headers map[string][]string, body []byte, authResults, cv string) (map[string][]string, error) {
	instance := determineNextInstance(headers)

	arcSet, err := s.signWithCV(headers, body, authResults, instance, cv)
	if err != nil {
		return nil, fmt.Errorf("failed to sign ARC set: %w", err)
	}

	newHeaders := make(map[string][]string)
	newHeaders["ARC-Authentication-Results"] = []string{arcSet.AAR}
	newHeaders["ARC-Message-Signature"] = []string{arcSet.AMS}
	newHeaders["ARC-Seal"] = []string{arcSet.AS}

	for name, values := range headers {
		newHeaders[name] = append(newHeaders[name], values...)
	}

	return newHeaders, nil
}

// determineNextInstance finds the next ARC instance number for the chain.
// Only ARC headers count: other fields (e.g. a Subject starting "i=9;")
// must not influence the instance.
func determineNextInstance(headers map[string][]string) int {
	maxInstance := 0
	for _, h := range extractARCHeaders(headers) {
		if inst := extractInstance(h.Value); inst > maxInstance {
			maxInstance = inst
		}
	}
	return maxInstance + 1
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

// extractInstance extracts the instance number from an ARC header's
// leading i= tag, tolerating folding whitespace. Returns 0 if absent/invalid
// (range is enforced by the callers).
func extractInstance(header string) int {
	h := strings.TrimSpace(header)
	if !strings.HasPrefix(h, "i") {
		return 0
	}
	rest := strings.TrimSpace(h[1:])
	if !strings.HasPrefix(rest, "=") {
		return 0
	}
	rest = rest[1:]
	end := strings.Index(rest, ";")
	if end < 0 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// arcStripB returns an ARC header value with its b= tag value emptied,
// leaving every other byte untouched.
func arcStripB(value string) string {
	parts := strings.Split(value, ";")
	for i, p := range parts {
		trimmed := strings.TrimSpace(p)
		if idx := strings.Index(trimmed, "="); idx > 0 && strings.TrimSpace(trimmed[:idx]) == "b" {
			lead := p[:len(p)-len(strings.TrimLeft(p, " \t\r\n"))]
			parts[i] = lead + "b="
		}
	}
	return strings.Join(parts, ";")
}

// arcFieldForHash returns a header field as it enters the hash: canonicalized,
// with the final CRLF removed (used for the signature header itself, whose b=
// must already be emptied).
func arcFieldForHash(name, value, canon string) string {
	return strings.TrimSuffix(canonicalizeHeader(name, value, canon), "\r\n")
}

// arcCanons splits c=header/body (default simple/simple).
func arcCanons(c string) (string, string) {
	if c == "" {
		return "simple", "simple"
	}
	h, b, ok := strings.Cut(c, "/")
	if !ok {
		b = "simple"
	}
	if h != "relaxed" {
		h = "simple"
	}
	if b != "relaxed" {
		b = "simple"
	}
	return h, b
}

// arcAMSInput builds the AMS hash input: the h= headers then the AMS field
// itself with empty b= and no trailing CRLF (RFC 8617 §4.1.2).
func arcAMSInput(ams string, headers map[string][]string, signed []string, hc string) []byte {
	var list []string
	for _, h := range signed {
		l := strings.ToLower(strings.TrimSpace(h))
		if l == "" || strings.HasPrefix(l, "arc-") {
			continue
		}
		list = append(list, l)
	}
	return []byte(canonicalizeHeaders(headers, list, hc) +
		arcFieldForHash("ARC-Message-Signature", arcStripB(ams), hc))
}

// arcSealInput builds the AS hash input for instance i (RFC 8617 §5.1.1):
// AAR, AMS, AS of every set 1..i in order, relaxed canonicalization, with the
// final AS carrying an empty b= and no trailing CRLF.
func arcSealInput(headers map[string][]string, sets map[int]ARCSet, aar, ams, as string, instance int) []byte {
	var sb strings.Builder
	for k := 1; k < instance; k++ {
		set := sets[k]
		sb.WriteString(canonicalizeHeaderRelaxed("ARC-Authentication-Results", set.AAR))
		sb.WriteString(canonicalizeHeaderRelaxed("ARC-Message-Signature", set.AMS))
		sb.WriteString(canonicalizeHeaderRelaxed("ARC-Seal", set.AS))
	}
	sb.WriteString(canonicalizeHeaderRelaxed("ARC-Authentication-Results", aar))
	sb.WriteString(canonicalizeHeaderRelaxed("ARC-Message-Signature", ams))
	sb.WriteString(arcFieldForHash("ARC-Seal", arcStripB(as), "relaxed"))
	return []byte(sb.String())
}

// validateAMS validates the ARC-Message-Signature
func (v *ARCValidator) validateAMS(ctx context.Context, ams string, headers map[string][]string, body []byte) (bool, error) {
	if ams == "" {
		return false, nil
	}

	params := parseTagValueList(ams)
	signature := params["b"]
	domain := params["d"]
	selector := params["s"]
	if signature == "" || domain == "" || selector == "" {
		return false, nil
	}
	if !strings.EqualFold(params["a"], "rsa-sha256") && params["a"] != "" {
		return false, nil
	}

	hc, bc := arcCanons(params["c"])
	// The body hash must match the (canonicalized) body.
	if params["bh"] == "" || computeBodyHash(body, bc) != params["bh"] {
		return false, nil
	}

	pubKey, err := fetchARCPublicKey(v.resolver, domain, selector)
	if err != nil {
		return false, err
	}

	sigData := arcAMSInput(ams, headers, parseHeaderList(params["h"]), hc)
	return verifyRSASignature(pubKey, sigData, signature) == nil, nil
}

// validateAS validates the ARC-Seal over all ARC sets up to its instance.
func (v *ARCValidator) validateAS(ctx context.Context, as string, headers map[string][]string, instance int) (bool, error) {
	if as == "" {
		return false, nil
	}

	params := parseTagValueList(as)
	signature := params["b"]
	domain := params["d"]
	selector := params["s"]
	if signature == "" || domain == "" || selector == "" {
		return false, nil
	}
	if !strings.EqualFold(params["a"], "rsa-sha256") && params["a"] != "" {
		return false, nil
	}
	if extractInstance(as) != instance {
		return false, nil
	}

	sets := groupARCHeaders(extractARCHeaders(headers))
	for k := 1; k <= instance; k++ {
		s, ok := sets[k]
		if !ok || s.AAR == "" || s.AMS == "" || s.AS == "" {
			return false, nil
		}
	}

	pubKey, err := fetchARCPublicKey(v.resolver, domain, selector)
	if err != nil {
		return false, err
	}

	cur := sets[instance]
	sigData := arcSealInput(headers, sets, cur.AAR, cur.AMS, as, instance)
	return verifyRSASignature(pubKey, sigData, signature) == nil, nil
}

// fetchARCPublicKey fetches the ARC public key from DNS
func fetchARCPublicKey(resolver DNSResolver, domain, selector string) (*rsa.PublicKey, error) {
	// Use same DNS query format as DKIM: selector._domainkey.domain
	query := fmt.Sprintf("%s._domainkey.%s", selector, domain)

	txtRecords, err := resolver.LookupTXT(context.Background(), query)
	if err != nil {
		return nil, err
	}

	for _, record := range txtRecords {
		pubKey, _, err := parseDKIMPublicKey(record)
		if err == nil && pubKey != nil {
			rsaKey, ok := pubKey.(*rsa.PublicKey)
			if ok {
				return rsaKey, nil
			}
		}
	}

	return nil, errors.New("no valid ARC public key found")
}

// arcSignedHeaders returns the h= list for headers actually present.
func arcSignedHeaders(headers map[string][]string) []string {
	present := make(map[string]bool)
	for name, values := range headers {
		if len(values) > 0 {
			present[strings.ToLower(name)] = true
		}
	}
	var out []string
	for _, h := range arcSignedHeaderCandidates {
		if present[h] {
			out = append(out, h)
		}
	}
	return out
}

// createAMS creates an ARC-Message-Signature header value
func (s *ARCSigner) createAMS(headers map[string][]string, body []byte, instance int) (string, error) {
	signed := arcSignedHeaders(headers)
	ams := fmt.Sprintf("i=%d; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; t=%d; h=%s; bh=%s; b=",
		instance,
		s.domain,
		s.selector,
		time.Now().Unix(),
		strings.Join(signed, ":"),
		computeBodyHash(body, "relaxed"),
	)

	signature, err := signRSA(s.privateKey, arcAMSInput(ams, headers, signed, "relaxed"))
	if err != nil {
		return "", err
	}
	return ams + signature, nil
}

// createAS creates an ARC-Seal header value, taking the instance's AAR/AMS
// from headers when present.
func (s *ARCSigner) createAS(headers map[string][]string, cv string, instance int) (string, error) {
	cur := groupARCHeaders(extractARCHeaders(headers))[instance]
	return s.createASWith(headers, cur.AAR, cur.AMS, cv, instance)
}

func (s *ARCSigner) createASWith(headers map[string][]string, aar, ams, cv string, instance int) (string, error) {
	as := fmt.Sprintf("i=%d; a=rsa-sha256; t=%d; cv=%s; d=%s; s=%s; b=",
		instance, time.Now().Unix(), cv, s.domain, s.selector)

	sets := groupARCHeaders(extractARCHeaders(headers))
	signature, err := signRSA(s.privateKey, arcSealInput(headers, sets, aar, ams, as, instance))
	if err != nil {
		return "", err
	}
	return as + signature, nil
}

// buildAMSSignatureData builds the data to be signed for AMS (relaxed header
// canonicalization over the h= headers followed by the AMS field itself).
func buildAMSSignatureData(ams string, headers map[string][]string, body []byte) []byte {
	params := parseTagValueList(ams)
	hc, _ := arcCanons(params["c"])
	return arcAMSInput(ams, headers, parseHeaderList(params["h"]), hc)
}

// extractSealInfo extracts domain and selector from ARC-Seal
func extractSealInfo(as string) (string, string) {
	params := parseTagValueList(as)
	return params["d"], params["s"]
}

// determineCV returns the cv= of the highest-instance ARC-Seal present.
func determineCV(headers map[string][]string) string {
	bestInst, cv := 0, ""
	for _, h := range extractARCHeaders(headers) {
		if h.Name != "arc-seal" {
			continue
		}
		if inst := extractInstance(h.Value); inst > bestInst {
			bestInst = inst
			cv = parseTagValueList(h.Value)["cv"]
		}
	}
	if cv == "" {
		return "none"
	}
	return cv
}
