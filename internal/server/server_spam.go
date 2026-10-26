package server

import (
	"github.com/umailserver/umailserver/internal/spam"
)

// bayesClassifier returns the Bayesian classifier shared by the inbound
// pipeline stage and the learner, creating it on first use. It is nil when
// spam filtering or the classifier is disabled, there is no storage
// database, or its buckets cannot be created.
func (s *Server) bayesClassifier() *spam.Classifier {
	s.spamClassifierOnce.Do(func() {
		cfg := s.config.Spam
		if !cfg.Enabled || !cfg.Bayesian.Enabled || s.storageDB == nil {
			return
		}
		c := spam.NewClassifier(s.storageDB.Bolt())
		if err := c.Initialize(); err != nil {
			s.logger.Error("failed to initialize Bayesian classifier", "error", err)
			return
		}
		s.spamClassifier = c
	})
	return s.spamClassifier
}

// startSpamLearner starts training the classifier from the messages users
// file into or out of Junk (spam.bayesian.auto_train). Before this the
// classifier was never trained and its stage never scored anything.
func (s *Server) startSpamLearner() {
	if !s.config.Spam.Bayesian.AutoTrain || s.msgStore == nil {
		return
	}
	c := s.bayesClassifier()
	if c == nil {
		return
	}
	s.spamLearner = spam.NewLearner(c, s.msgStore.ReadMessage, s.logger)
	s.logger.Info("Bayesian training from Junk moves enabled")
}

// reportJunk hands a user's Junk / not-Junk filing to the learner; it is a
// no-op when training is off.
func (s *Server) reportJunk(user, messageID string, isSpam bool) {
	s.spamLearner.Report(user, messageID, isSpam)
}

// stopSpamLearner waits for the queued training reports; it must run before
// the storage database is closed.
func (s *Server) stopSpamLearner() {
	s.spamLearner.Close()
}
