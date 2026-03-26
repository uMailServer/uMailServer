package spam

import (
	"strings"
	"testing"
)

func BenchmarkBayesianTokenize(b *testing.B) {
	texts := []string{
		"This is a sample email",
		"Buy cheap viagra now!!!",
		"Meeting scheduled for tomorrow at 3pm",
		strings.Repeat("This is a longer text with many words to tokenize. ", 10),
	}

	c := NewBayesianClassifier()

	for i := 0; i < b.N; i++ {
		for _, text := range texts {
			c.tokenize(text)
		}
	}
}

func BenchmarkBayesianLearn(b *testing.B) {
	c := NewBayesianClassifier()

	for i := 0; i < b.N; i++ {
		c.Learn("spam", "buy cheap viagra now")
	}
}

func BenchmarkBayesianClassify(b *testing.B) {
	c := NewBayesianClassifier()
	c.Learn("spam", "buy cheap viagra now discount")
	c.Learn("spam", "get rich quick money")
	c.Learn("ham", "meeting scheduled for tomorrow")
	c.Learn("ham", "project update status report")

	text := "cheap viagra discount offer"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Classify(text)
	}
}

func BenchmarkGreylistingTripletKey(b *testing.B) {
	triplets := []struct {
		ip, sender, recipient string
	}{
		{"192.168.1.1", "sender@example.com", "recipient@example.org"},
		{"10.0.0.1", "user@domain.com", "admin@company.net"},
	}

	gl := NewGreylisting()

	for i := 0; i < b.N; i++ {
		for _, t := range triplets {
			gl.key(t.ip, t.sender, t.recipient)
		}
	}
}
