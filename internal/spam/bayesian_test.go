package spam

import (
	"testing"
)

func TestNewBayesianClassifier(t *testing.T) {
	c := NewBayesianClassifier()

	if c == nil {
		t.Fatal("Classifier should not be nil")
	}

	if c.spamWords == nil {
		t.Error("spamWords should be initialized")
	}

	if c.hamWords == nil {
		t.Error("hamWords should be initialized")
	}

	if c.spamThreshold != 0.9 {
		t.Errorf("Expected spam threshold 0.9, got %f", c.spamThreshold)
	}

	if c.hamThreshold != 0.1 {
		t.Errorf("Expected ham threshold 0.1, got %f", c.hamThreshold)
	}
}

func TestBayesianTrainSpam(t *testing.T) {
	c := NewBayesianClassifier()

	c.TrainSpam("buy cheap viagra now")
	c.TrainSpam("get rich quick")
	c.TrainSpam("free money offer")

	if c.spamMessages != 3 {
		t.Errorf("Expected 3 spam messages, got %d", c.spamMessages)
	}

	if c.spamTotal == 0 {
		t.Error("Expected spamTotal > 0")
	}

	// Check words were added
	if _, exists := c.spamWords["viagra"]; !exists {
		t.Error("Expected 'viagra' in spam words")
	}
}

func TestBayesianTrainHam(t *testing.T) {
	c := NewBayesianClassifier()

	c.TrainHam("meeting scheduled for tomorrow")
	c.TrainHam("project update from team")
	c.TrainHam("lunch with colleagues")

	if c.hamMessages != 3 {
		t.Errorf("Expected 3 ham messages, got %d", c.hamMessages)
	}

	if c.hamTotal == 0 {
		t.Error("Expected hamTotal > 0")
	}
}

func TestBayesianClassifyUntrained(t *testing.T) {
	c := NewBayesianClassifier()

	// Without training, should return neutral
	isSpam, prob := c.Classify("any text")

	if isSpam {
		t.Error("Expected not spam when untrained")
	}

	if prob != 0.5 {
		t.Errorf("Expected probability 0.5 when untrained, got %f", prob)
	}
}

func TestBayesianClassify(t *testing.T) {
	c := NewBayesianClassifier()

	// Train with spam
	for i := 0; i < 10; i++ {
		c.TrainSpam("buy cheap viagra now free offer")
	}

	// Train with ham
	for i := 0; i < 10; i++ {
		c.TrainHam("meeting scheduled tomorrow project team")
	}

	// Test spam classification
	isSpam, prob := c.Classify("cheap viagra free")
	if !isSpam {
		t.Error("Expected spam for 'cheap viagra free'")
	}
	if prob < 0.9 {
		t.Errorf("Expected high spam probability, got %f", prob)
	}

	// Test ham classification
	isSpam, prob = c.Classify("meeting with project team")
	if isSpam {
		t.Error("Expected ham for 'meeting with project team'")
	}
	if prob > 0.5 {
		t.Errorf("Expected low spam probability, got %f", prob)
	}
}

func TestBayesianSetThresholds(t *testing.T) {
	c := NewBayesianClassifier()

	c.SetThresholds(0.8, 0.2)

	if c.spamThreshold != 0.8 {
		t.Errorf("Expected spam threshold 0.8, got %f", c.spamThreshold)
	}

	if c.hamThreshold != 0.2 {
		t.Errorf("Expected ham threshold 0.2, got %f", c.hamThreshold)
	}
}

func TestBayesianReset(t *testing.T) {
	c := NewBayesianClassifier()

	c.TrainSpam("spam message")
	c.TrainHam("ham message")

	c.Reset()

	if c.spamMessages != 0 {
		t.Errorf("Expected 0 spam messages after reset, got %d", c.spamMessages)
	}

	if c.hamMessages != 0 {
		t.Errorf("Expected 0 ham messages after reset, got %d", c.hamMessages)
	}

	if len(c.spamWords) != 0 {
		t.Errorf("Expected 0 spam words after reset, got %d", len(c.spamWords))
	}
}

func TestBayesianGetStats(t *testing.T) {
	c := NewBayesianClassifier()

	c.TrainSpam("spam")
	c.TrainHam("ham")

	stats := c.GetStats()

	if stats["spam_messages"] != 1 {
		t.Errorf("Expected 1 spam message, got %v", stats["spam_messages"])
	}

	if stats["ham_messages"] != 1 {
		t.Errorf("Expected 1 ham message, got %v", stats["ham_messages"])
	}
}

func TestTokenize(t *testing.T) {
	c := NewBayesianClassifier()

	tokens := c.tokenize("Hello, World! This is a test.")

	// Should have filtered out short words and punctuation
	foundHello := false
	foundWorld := false

	for _, token := range tokens {
		if token == "hello" {
			foundHello = true
		}
		if token == "world" {
			foundWorld = true
		}
	}

	if !foundHello {
		t.Error("Expected 'hello' in tokens")
	}

	if !foundWorld {
		t.Error("Expected 'world' in tokens")
	}
}
