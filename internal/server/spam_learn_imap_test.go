package server

// Regression test: the Bayesian classifier was never trained, so its stage
// never scored anything. Filing a message into Junk over IMAP now trains it
// as spam, and taking it out again (not into Trash) retrains it as ham.

import (
	"testing"

	"github.com/umailserver/umailserver/internal/imap"
)

func TestIMAPJunkMovesTrainClassifier(t *testing.T) {
	srv := junkRTServer(t, true)
	srv.mailstore = imap.NewBboltMailstoreWithInterfaces(srv.storageDB, srv.msgStore)
	srv.startSpamLearner()
	srv.mailstore.SetJunkReportFunc(srv.reportJunk)
	if srv.spamLearner == nil {
		t.Fatal("INVALID: learner not started with default spam config")
	}
	const user = "alice@test.com"
	for _, mb := range []string{"Junk", "Archive", "Trash"} {
		if err := srv.storageDB.CreateMailbox(user, mb); err != nil {
			t.Fatalf("INVALID: create %s: %v", mb, err)
		}
	}
	sieveDTSend(t, srv, "", []string{user}, junkRTClean)

	move := func(from, to string) {
		t.Helper()
		if _, _, _, err := srv.mailstore.MoveMessagesUIDs(user, from, to, "1"); err != nil {
			t.Fatalf("move %s -> %s: %v", from, to, err)
		}
		if err := srv.mailstore.Expunge(user, from); err != nil {
			t.Fatalf("expunge %s: %v", from, err)
		}
	}
	move("INBOX", "Junk")    // reported as spam
	move("Junk", "Archive")  // reported as ham: the spam training is undone
	move("Archive", "Trash") // not a Junk move: no report
	srv.stopSpamLearner()

	hamMsgs, spamMsgs, err := srv.bayesClassifier().GetMessageCounts()
	if err != nil {
		t.Fatal(err)
	}
	if hamMsgs != 1 || spamMsgs != 0 {
		t.Fatalf("trained messages ham=%d spam=%d, want 1/0", hamMsgs, spamMsgs)
	}
}

func TestSpamLearnerOffWithoutAutoTrain(t *testing.T) {
	srv := junkRTServer(t, true)
	srv.config.Spam.Bayesian.AutoTrain = false
	srv.startSpamLearner()
	if srv.spamLearner != nil {
		t.Fatal("learner started with spam.bayesian.auto_train: false")
	}
	srv.reportJunk("alice@test.com", "x", true) // must be a no-op
}
