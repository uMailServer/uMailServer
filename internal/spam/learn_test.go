package spam

import (
	"fmt"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func newLearnTestClassifier(t *testing.T) *Classifier {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "s.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := NewClassifier(db)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}
	return c
}

func learnMsgs(msgs map[string]string) FetchFunc {
	return func(user, id string) ([]byte, error) {
		m, ok := msgs[id]
		if !ok {
			return nil, fmt.Errorf("no message %s", id)
		}
		return []byte(m), nil
	}
}

func counts(t *testing.T, c *Classifier) (hamMsgs, spamMsgs, totalHam, totalSpam uint64) {
	t.Helper()
	hamMsgs, spamMsgs, err := c.GetMessageCounts()
	if err != nil {
		t.Fatal(err)
	}
	totalHam, totalSpam, err = c.GetTotalCounts()
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestLearnerIdempotentAndFlips(t *testing.T) {
	c := newLearnTestClassifier(t)
	l := &Learner{c: c, fetch: learnMsgs(map[string]string{
		"m1": "Subject: cheap pills\r\n\r\nbuy cheap pills online now\r\n",
	})}

	for i := 0; i < 2; i++ {
		if err := l.Learn("alice@test.com", "m1", true); err != nil {
			t.Fatal(err)
		}
	}
	hamMsgs, spamMsgs, totalHam, totalSpam := counts(t, c)
	if spamMsgs != 1 || hamMsgs != 0 || totalSpam == 0 || totalHam != 0 {
		t.Fatalf("after reporting spam twice: msgs ham=%d spam=%d tokens ham=%d spam=%d, want one spam message",
			hamMsgs, spamMsgs, totalHam, totalSpam)
	}
	spamTokens := totalSpam

	// Reported as not spam: the spam training is undone, not doubled.
	if err := l.Learn("alice@test.com", "m1", false); err != nil {
		t.Fatal(err)
	}
	hamMsgs, spamMsgs, totalHam, totalSpam = counts(t, c)
	if spamMsgs != 0 || hamMsgs != 1 || totalSpam != 0 || totalHam != spamTokens {
		t.Fatalf("after flipping to ham: msgs ham=%d spam=%d tokens ham=%d spam=%d, want ham=1/%d spam=0/0",
			hamMsgs, spamMsgs, totalHam, totalSpam, spamTokens)
	}
	if h, s, _ := c.GetTokenFrequency("pills"); h != 1 || s != 0 {
		t.Fatalf("token pills: ham=%d spam=%d, want 1/0", h, s)
	}

	// Another user's report of the same message counts on its own.
	if err := l.Learn("bob@test.com", "m1", true); err != nil {
		t.Fatal(err)
	}
	if hamMsgs, spamMsgs, _, _ = counts(t, c); hamMsgs != 1 || spamMsgs != 1 {
		t.Fatalf("second user: msgs ham=%d spam=%d, want 1/1", hamMsgs, spamMsgs)
	}
}

// Classify stays neutral until both classes hold MinTrainedMessages
// messages, then separates them.
func TestLearnerTrainingDrivesClassify(t *testing.T) {
	c := newLearnTestClassifier(t)
	msgs := map[string]string{}
	for i := 0; i < MinTrainedMessages; i++ {
		msgs[fmt.Sprintf("s%d", i)] = fmt.Sprintf("Subject: viagra casino %d\r\n\r\nwin money casino viagra jackpot\r\n", i)
		msgs[fmt.Sprintf("h%d", i)] = fmt.Sprintf("Subject: meeting notes %d\r\n\r\nagenda for the project meeting tomorrow\r\n", i)
	}
	l := &Learner{c: c, fetch: learnMsgs(msgs)}
	probe := MessageTokens([]byte("Subject: casino jackpot\r\n\r\nwin money at the casino\r\n"))

	for i := 0; i < MinTrainedMessages; i++ {
		if res, _ := c.Classify(probe); res.SpamProbability != 0.5 {
			t.Fatalf("after %d messages per class: probability %v, want neutral 0.5", i, res.SpamProbability)
		}
		if err := l.Learn("u", fmt.Sprintf("s%d", i), true); err != nil {
			t.Fatal(err)
		}
		if err := l.Learn("u", fmt.Sprintf("h%d", i), false); err != nil {
			t.Fatal(err)
		}
	}
	res, err := c.Classify(probe)
	if err != nil {
		t.Fatal(err)
	}
	// 0.7 is where BayesianStage starts adding to the spam score.
	if res.SpamProbability <= 0.7 {
		t.Fatalf("trained classifier: spam probability %v, want > 0.7", res.SpamProbability)
	}
}

func TestLearnerReportCloseDrains(t *testing.T) {
	c := newLearnTestClassifier(t)
	l := NewLearner(c, learnMsgs(map[string]string{"m1": "Subject: x\r\n\r\nhello there friend\r\n"}), nil)
	l.Report("alice@test.com", "m1", true)
	l.Close()
	l.Close()                               // idempotent
	l.Report("alice@test.com", "m1", false) // after Close: dropped, no panic
	if _, spamMsgs, _ := c.GetMessageCounts(); spamMsgs != 1 {
		t.Fatalf("queued report not trained before Close returned: spam msgs = %d", spamMsgs)
	}
}
