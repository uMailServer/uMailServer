package spam

import (
	"bufio"
	"math"
	"strings"
	"sync"
)

// BayesianClassifier implements a Bayesian spam filter
type BayesianClassifier struct {
	mu sync.RWMutex

	// Word counts
	spamWords    map[string]int
	hamWords     map[string]int
	spamTotal    int
	hamTotal     int
	spamMessages int
	hamMessages  int

	// Thresholds
	spamThreshold float64
	hamThreshold  float64
}

// NewBayesianClassifier creates a new Bayesian classifier
func NewBayesianClassifier() *BayesianClassifier {
	return &BayesianClassifier{
		spamWords:     make(map[string]int),
		hamWords:      make(map[string]int),
		spamThreshold: 0.9,
		hamThreshold:  0.1,
	}
}

// TrainSpam trains the classifier with a spam message
func (c *BayesianClassifier) TrainSpam(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.spamMessages++
	words := c.tokenize(text)
	for _, word := range words {
		c.spamWords[word]++
		c.spamTotal++
	}
}

// TrainHam trains the classifier with a ham (non-spam) message
func (c *BayesianClassifier) TrainHam(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hamMessages++
	words := c.tokenize(text)
	for _, word := range words {
		c.hamWords[word]++
		c.hamTotal++
	}
}

// Classify returns the spam probability of a message
func (c *BayesianClassifier) Classify(text string) (isSpam bool, probability float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// If we don't have enough training data, return neutral
	if c.spamMessages == 0 || c.hamMessages == 0 {
		return false, 0.5
	}

	words := c.tokenize(text)
	if len(words) == 0 {
		return false, 0.5
	}

	// Calculate spam probability using Bayes' theorem
	scores := make([]float64, 0, len(words))

	for _, word := range words {
		score := c.wordSpamProbability(word)
		scores = append(scores, score)
	}

	// Combine probabilities using the log-odds method
	// This avoids floating point underflow
	logSpam := 0.0
	logHam := 0.0

	for _, score := range scores {
		// Avoid log(0)
		if score < 0.01 {
			score = 0.01
		}
		if score > 0.99 {
			score = 0.99
		}

		logSpam += math.Log(score)
		logHam += math.Log(1 - score)
	}

	// Convert back to probability
	spamProb := math.Exp(logSpam)
	hamProb := math.Exp(logHam)

	if spamProb+hamProb == 0 {
		return false, 0.5
	}

	probability = spamProb / (spamProb + hamProb)

	// Classify based on thresholds
	if probability >= c.spamThreshold {
		return true, probability
	}
	if probability <= c.hamThreshold {
		return false, probability
	}

	// Uncertain - treat as ham
	return false, probability
}

// wordSpamProbability returns the spam probability of a single word
func (c *BayesianClassifier) wordSpamProbability(word string) float64 {
	spamCount := c.spamWords[word]
	hamCount := c.hamWords[word]

	// Apply Laplace smoothing
	spamFreq := float64(spamCount+1) / float64(c.spamTotal+len(c.spamWords))
	hamFreq := float64(hamCount+1) / float64(c.hamTotal+len(c.hamWords))

	// Calculate probability
	prob := spamFreq / (spamFreq + hamFreq)

	// Clamp to avoid extreme values
	if prob < 0.01 {
		prob = 0.01
	}
	if prob > 0.99 {
		prob = 0.99
	}

	return prob
}

// tokenize extracts words from text
func (c *BayesianClassifier) tokenize(text string) []string {
	var tokens []string
	scanner := bufio.NewScanner(strings.NewReader(strings.ToLower(text)))
	scanner.Split(bufio.ScanWords)

	for scanner.Scan() {
		word := scanner.Text()
		// Clean the word
		word = strings.TrimFunc(word, func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
		})

		if len(word) >= 3 && len(word) <= 30 {
			tokens = append(tokens, word)
		}
	}

	return tokens
}

// GetStats returns training statistics
func (c *BayesianClassifier) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"spam_messages": c.spamMessages,
		"ham_messages":  c.hamMessages,
		"spam_words":    len(c.spamWords),
		"ham_words":     len(c.hamWords),
		"total_words":   c.spamTotal + c.hamTotal,
	}
}

// SetThresholds sets the classification thresholds
func (c *BayesianClassifier) SetThresholds(spamThreshold, hamThreshold float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.spamThreshold = spamThreshold
	c.hamThreshold = hamThreshold
}

// Reset clears all training data
func (c *BayesianClassifier) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.spamWords = make(map[string]int)
	c.hamWords = make(map[string]int)
	c.spamTotal = 0
	c.hamTotal = 0
	c.spamMessages = 0
	c.hamMessages = 0
}
