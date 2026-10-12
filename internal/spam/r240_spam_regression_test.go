package spam

import (
	"fmt"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

// F7170: a token never seen in training carries no evidence. With unbalanced
// training sets it used to get a strongly skewed probability, and such tokens
// then dominated the 20 most significant ones, drowning real signal.
func TestR240UnseenTokensAreNeutral(t *testing.T) {
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "s.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := NewClassifier(db)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	// Spam-heavy corpus: many spam tokens, few ham tokens.
	var spamToks, hamToks []string
	for i := 0; i < 500; i++ {
		spamToks = append(spamToks, fmt.Sprintf("spamword%d", i))
	}
	for i := 0; i < 12; i++ {
		hamToks = append(hamToks, fmt.Sprintf("hamword%d", i))
	}
	if err := c.TrainSpam(spamToks); err != nil {
		t.Fatal(err)
	}
	if err := c.TrainHam(hamToks); err != nil {
		t.Fatal(err)
	}
	var unknown []string
	for i := 0; i < 30; i++ {
		unknown = append(unknown, fmt.Sprintf("neverseen%d", i))
	}
	res, err := c.Classify(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if res.SpamProbability < 0.49 || res.SpamProbability > 0.51 {
		t.Fatalf("unknown tokens should be neutral, got %v", res.SpamProbability)
	}
	// One strong ham token among many unknown ones must not be drowned.
	mixed := append(append([]string{}, unknown...), "hamword1")
	res, _ = c.Classify(mixed)
	if res.SpamProbability > 0.5 {
		t.Fatalf("ham evidence lost among unknown tokens: %v", res.SpamProbability)
	}
}
