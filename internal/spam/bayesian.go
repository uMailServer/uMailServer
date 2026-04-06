package spam

import (
	"math"
	"strings"
	"unicode"
)

// Tokenizer splits text into tokens for Bayesian classification
type Tokenizer struct {
	minTokenLength int
	maxTokenLength int
}

// NewTokenizer creates a new tokenizer
func NewTokenizer() *Tokenizer {
	return &Tokenizer{
		minTokenLength: 3,
		maxTokenLength: 20,
	}
}

// Tokenize splits text into unigrams and bigrams
func (t *Tokenizer) Tokenize(text string) []string {
	// Normalize text: lowercase and remove special chars
	normalized := t.normalize(text)

	// Split into words
	words := strings.Fields(normalized)

	// Extract unigrams and bigrams
	var tokens []string

	// Unigrams
	for _, word := range words {
		if t.isValidToken(word) {
			tokens = append(tokens, word)
		}
	}

	// Bigrams (consecutive word pairs)
	for i := 0; i < len(words)-1; i++ {
		if t.isValidToken(words[i]) && t.isValidToken(words[i+1]) {
			bigram := words[i] + " " + words[i+1]
			tokens = append(tokens, bigram)
		}
	}

	return tokens
}

// normalize converts text to lowercase and removes punctuation
func (t *Tokenizer) normalize(text string) string {
	var result strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(unicode.ToLower(r))
		} else if unicode.IsSpace(r) {
			result.WriteRune(' ')
		}
	}
	return result.String()
}

// isValidToken checks if a token is valid for classification
func (t *Tokenizer) isValidToken(word string) bool {
	if len(word) < t.minTokenLength || len(word) > t.maxTokenLength {
		return false
	}
	// Reject pure numbers
	allDigits := true
	for _, r := range word {
		if !unicode.IsDigit(r) {
			allDigits = false
			break
		}
	}
	return !allDigits
}

// ExtractTokensFromHeaders extracts tokens from email headers
func ExtractTokensFromHeaders(headers map[string][]string) []string {
	var tokens []string
	tokenizer := NewTokenizer()

	for key, values := range headers {
		key = strings.ToLower(key)
		for _, value := range values {
			switch key {
			case "subject":
				tokens = append(tokens, tokenizer.Tokenize(value)...)
			case "from", "to", "cc", "bcc":
				tokens = append(tokens, extractEmails(value)...)
			default:
				tokens = append(tokens, tokenizer.Tokenize(value)...)
			}
		}
	}

	return tokens
}

// ExtractTokensFromBody extracts tokens from email body
func ExtractTokensFromBody(body []byte) []string {
	tokenizer := NewTokenizer()
	return tokenizer.Tokenize(string(body))
}

// extractEmails extracts email addresses as tokens
func extractEmails(s string) []string {
	var emails []string
	parts := strings.Split(s, "@")
	if len(parts) == 2 {
		local := strings.TrimSpace(parts[0])
		domain := strings.TrimSpace(parts[1])
		local = strings.Trim(local, "<>")
		domain = strings.Trim(domain, "<>")
		if local != "" && domain != "" {
			emails = append(emails, local+"@"+domain)
		}
	}
	return emails
}

// TokenFrequency stores token frequency information
type TokenFrequency struct {
	Token     string
	HamCount  uint32
	SpamCount uint32
}

// Robinson-Fisher probability calculation
// Based on: https://en.wikipedia.org/wiki/Naive_Bayes_spam_filtering#Robinson-Fisher_algorithm

// tokenProbability calculates the probability that a token indicates spam
func tokenProbability(hamCount, spamCount uint32, totalHam, totalSpam uint64) float64 {
	// Use adjusted frequency to avoid zero probabilities
	const smoothing = 0.5

	hamFreq := (float64(hamCount) + smoothing) / (float64(totalHam) + 1.0)
	spamFreq := (float64(spamCount) + smoothing) / (float64(totalSpam) + 1.0)

	if hamFreq+spamFreq == 0 {
		return 0.5
	}

	return spamFreq / (hamFreq + spamFreq)
}

// CombinedProbability combines individual token probabilities
// Uses the Robinson-Fisher combination method
func CombinedProbability(probs []float64) float64 {
	if len(probs) == 0 {
		return 0.5
	}

	var sumLogProb float64
	var sumLogOneMinusProb float64

	for _, p := range probs {
		if p <= 0 {
			p = 0.01
		}
		if p >= 1 {
			p = 0.99
		}
		sumLogProb += math.Log(p)
		sumLogOneMinusProb += math.Log(1 - p)
	}

	n := float64(len(probs))
	if n == 0 {
		return 0.5
	}

	h := -sumLogOneMinusProb / n
	s := -sumLogProb / n

	// Clamp to prevent overflow
	if h-s > 700 {
		return 0.99
	}
	if h-s < -700 {
		return 0.01
	}

	return 1.0 / (1.0 + math.Exp(h-s))
}

// ClassifyResult holds the result of Bayesian classification
type ClassifyResult struct {
	SpamProbability float64
	IsSpam          bool
	Confidence      float64
}