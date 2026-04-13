package spam

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode"

	"go.etcd.io/bbolt"
)

// SpamBucket is the bbolt bucket name for spam token counts
const SpamBucket = "spam_tokens"

// HamBucket is the bbolt bucket name for ham token counts
const HamBucket = "ham_tokens"

// StatsBucket is the bbolt bucket name for total counts
const StatsBucket = "spam_stats"

// Classifier performs Bayesian spam classification
type Classifier struct {
	bolt      *bbolt.DB
	tokenizer *Tokenizer
	mu        sync.RWMutex
}

// NewClassifier creates a new Bayesian classifier
func NewClassifier(bolt *bbolt.DB) *Classifier {
	return &Classifier{
		bolt:      bolt,
		tokenizer: NewTokenizer(),
	}
}

// Initialize sets up the bbolt buckets if needed
func (c *Classifier) Initialize() error {
	if c.bolt == nil {
		return nil
	}
	return c.bolt.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(SpamBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(HamBucket))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(StatsBucket))
		return err
	})
}

// tokenKey creates a bucket key for a token
func tokenKey(token string) []byte {
	return []byte(token)
}

// GetTotalCounts returns total ham and spam token counts
func (c *Classifier) GetTotalCounts() (totalHam uint64, totalSpam uint64, err error) {
	if c.bolt == nil {
		return 1, 1, nil
	}
	err = c.bolt.View(func(tx *bbolt.Tx) error {
		spamBucket := tx.Bucket([]byte(SpamBucket))
		hamBucket := tx.Bucket([]byte(HamBucket))
		statsBucket := tx.Bucket([]byte(StatsBucket))

		if spamBucket != nil {
			totalSpam = countAllTokens(spamBucket)
		}
		if hamBucket != nil {
			totalHam = countAllTokens(hamBucket)
		}
		if statsBucket != nil {
			if v := statsBucket.Get([]byte("total_ham")); len(v) == 8 {
				totalHam = binary.BigEndian.Uint64(v)
			}
			if v := statsBucket.Get([]byte("total_spam")); len(v) == 8 {
				totalSpam = binary.BigEndian.Uint64(v)
			}
		}
		return nil
	})
	return
}

// countAllTokens counts all tokens in a bucket
func countAllTokens(bucket *bbolt.Bucket) uint64 {
	var count uint64
	cursor := bucket.Cursor()
	for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
		if len(v) >= 4 {
			count += uint64(binary.BigEndian.Uint32(v))
		}
	}
	return count
}

// IncrementToken increments the count for a token in the given bucket
func (c *Classifier) IncrementToken(bucketName string, token string, delta uint32) error {
	if c.bolt == nil {
		return nil
	}
	return c.bolt.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s not found", bucketName)
		}
		key := tokenKey(token)
		var count uint32
		if v := bucket.Get(key); len(v) >= 4 {
			count = binary.BigEndian.Uint32(v)
		}
		count += delta
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], count)
		return bucket.Put(key, buf[:])
	})
}

// UpdateStats updates the total ham/spam counts in stats bucket
func (c *Classifier) UpdateStats() error {
	if c.bolt == nil {
		return nil
	}
	// Get counts first (outside the Update transaction to avoid nested transactions)
	totalHam, totalSpam, err := c.GetTotalCounts()
	if err != nil {
		return err
	}
	// Now update in a separate transaction
	return c.bolt.Update(func(tx *bbolt.Tx) error {
		statsBucket := tx.Bucket([]byte(StatsBucket))
		if statsBucket == nil {
			return nil
		}
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], totalHam)
		if err := statsBucket.Put([]byte("total_ham"), buf[:]); err != nil {
			return err
		}
		binary.BigEndian.PutUint64(buf[:], totalSpam)
		return statsBucket.Put([]byte("total_spam"), buf[:])
	})
}

// TrainSpam trains the classifier with a spam email
func (c *Classifier) TrainSpam(tokens []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, token := range tokens {
		if err := c.IncrementToken(SpamBucket, token, 1); err != nil {
			return err
		}
	}
	return c.UpdateStats()
}

// TrainHam trains the classifier with a ham (non-spam) email
func (c *Classifier) TrainHam(tokens []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, token := range tokens {
		if err := c.IncrementToken(HamBucket, token, 1); err != nil {
			return err
		}
	}
	return c.UpdateStats()
}

// GetTokenFrequency retrieves the ham and spam counts for a token
func (c *Classifier) GetTokenFrequency(token string) (hamCount, spamCount uint32, err error) {
	if c.bolt == nil {
		return 1, 1, nil
	}
	err = c.bolt.View(func(tx *bbolt.Tx) error {
		spamBucket := tx.Bucket([]byte(SpamBucket))
		hamBucket := tx.Bucket([]byte(HamBucket))

		if hamBucket != nil {
			if v := hamBucket.Get(tokenKey(token)); len(v) >= 4 {
				hamCount = binary.BigEndian.Uint32(v)
			}
		}
		if spamBucket != nil {
			if v := spamBucket.Get(tokenKey(token)); len(v) >= 4 {
				spamCount = binary.BigEndian.Uint32(v)
			}
		}
		return nil
	})
	return
}

// Classify classifies a message as spam or ham based on tokens
func (c *Classifier) Classify(tokens []string) (*ClassifyResult, error) {
	if c.bolt == nil || len(tokens) == 0 {
		return &ClassifyResult{
			SpamProbability: 0.5,
			IsSpam:          false,
			Confidence:      0.0,
		}, nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	totalHam, totalSpam, err := c.GetTotalCounts()
	if err != nil {
		return nil, err
	}

	// If we have no training data, return neutral
	if totalHam < 10 || totalSpam < 10 {
		return &ClassifyResult{
			SpamProbability: 0.5,
			IsSpam:          false,
			Confidence:      0.0,
		}, nil
	}

	var probs []float64
	for _, token := range tokens {
		hamCount, spamCount, err := c.GetTokenFrequency(token)
		if err != nil {
			continue
		}
		prob := tokenProbability(hamCount, spamCount, totalHam, totalSpam)
		probs = append(probs, prob)
	}

	// Use only the most significant tokens (highest information gain)
	// Sort by distance from 0.5 (most informative)
	if len(probs) > 20 {
		// Simple approach: use first 20 tokens
		probs = probs[:20]
	}

	spamProb := CombinedProbability(probs)

	// Calculate confidence based on how many tokens we processed
	confidence := math.Min(float64(len(tokens))/50.0, 1.0)

	return &ClassifyResult{
		SpamProbability: spamProb,
		IsSpam:          spamProb > 0.7,
		Confidence:      confidence,
	}, nil
}

// TrainFromEmail trains the classifier from email headers and body
func (c *Classifier) TrainFromEmail(isSpam bool, headers map[string][]string, body []byte) error {
	var tokens []string
	tokens = append(tokens, ExtractTokensFromHeaders(headers)...)
	tokens = append(tokens, ExtractTokensFromBody(body)...)

	if isSpam {
		return c.TrainSpam(tokens)
	}
	return c.TrainHam(tokens)
}

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

	return 1.0 / (1.0 + math.Exp(s-h))
}

// ClassifyResult holds the result of Bayesian classification
type ClassifyResult struct {
	SpamProbability float64
	IsSpam          bool
	Confidence      float64
}
