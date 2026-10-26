package spam

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"sync"

	"go.etcd.io/bbolt"
)

// LearnedBucket records, per user and message, the class the message was
// last trained as, so that reporting the same message twice does not count
// it twice and reporting it the other way first untrains it.
const LearnedBucket = "spam_learned"

// learnQueueSize bounds the reports waiting for the training worker.
const learnQueueSize = 1024

// FetchFunc returns the raw message stored as messageID for user.
type FetchFunc func(user, messageID string) ([]byte, error)

type learnJob struct {
	user, messageID string
	spam            bool
}

// Learner trains the classifier from what users report: a message moved into
// Junk is spam, one moved out of it is ham. Reports are processed by one
// background worker so that mailbox operations do not wait for training.
type Learner struct {
	c      *Classifier
	fetch  FetchFunc
	logger *slog.Logger

	jobs   chan learnJob
	done   chan struct{}
	mu     sync.RWMutex // guards closed against Report sending on jobs
	closed bool
}

// NewLearner starts a learner training c with messages read through fetch.
// Close stops it.
func NewLearner(c *Classifier, fetch FetchFunc, logger *slog.Logger) *Learner {
	if logger == nil {
		logger = slog.Default()
	}
	l := &Learner{
		c:      c,
		fetch:  fetch,
		logger: logger,
		jobs:   make(chan learnJob, learnQueueSize),
		done:   make(chan struct{}),
	}
	go l.run()
	return l
}

func (l *Learner) run() {
	defer close(l.done)
	for job := range l.jobs {
		if err := l.Learn(job.user, job.messageID, job.spam); err != nil {
			l.logger.Warn("spam training failed", "user", job.user, "message_id", job.messageID, "spam", job.spam, "error", err)
		}
	}
}

// Report queues messageID of user for training as spam or ham. It never
// blocks: when the queue is full the report is dropped and logged.
func (l *Learner) Report(user, messageID string, spam bool) {
	if l == nil || user == "" || messageID == "" {
		return
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return
	}
	select {
	case l.jobs <- learnJob{user: user, messageID: messageID, spam: spam}:
	default:
		l.logger.Warn("spam training queue full, report dropped", "user", user, "message_id", messageID)
	}
}

// Close stops accepting reports and waits for the queued ones.
func (l *Learner) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.jobs)
	}
	l.mu.Unlock()
	<-l.done
}

// learnedKey is the LearnedBucket key of messageID for user.
func learnedKey(user, messageID string) []byte {
	return []byte(user + "\x00" + messageID)
}

// Learn trains messageID of user as spam or ham. It does nothing when the
// message was already trained that way, and untrains the previous class when
// it was trained the other way.
func (l *Learner) Learn(user, messageID string, spam bool) error {
	c := l.c
	if c == nil || c.bolt == nil {
		return nil
	}
	want := []byte("h")
	if spam {
		want = []byte("s")
	}
	key := learnedKey(user, messageID)

	var prev []byte
	if err := c.bolt.View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket([]byte(LearnedBucket)); b != nil {
			prev = append([]byte(nil), b.Get(key)...)
		}
		return nil
	}); err != nil {
		return err
	}
	if bytes.Equal(prev, want) {
		return nil
	}

	raw, err := l.fetch(user, messageID)
	if err != nil {
		return fmt.Errorf("read message: %w", err)
	}
	tokens := MessageTokens(raw)

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bolt.Update(func(tx *bbolt.Tx) error {
		learned, err := tx.CreateBucketIfNotExists([]byte(LearnedBucket))
		if err != nil {
			return err
		}
		// Re-read under the write lock: another report may have won.
		cur := learned.Get(key)
		if bytes.Equal(cur, want) {
			return nil
		}
		switch string(cur) {
		case "s":
			err = trainTx(tx, SpamBucket, tokens, true)
		case "h":
			err = trainTx(tx, HamBucket, tokens, true)
		}
		if err != nil {
			return err
		}
		bucket := HamBucket
		if spam {
			bucket = SpamBucket
		}
		if err := trainTx(tx, bucket, tokens, false); err != nil {
			return err
		}
		return learned.Put(key, want)
	})
}

// MessageTokens returns the classifier tokens of a raw message, extracted
// the way the Bayesian pipeline stage extracts them at delivery.
func MessageTokens(raw []byte) []string {
	var headers map[string][]string
	if msg, err := mail.ReadMessage(bytes.NewReader(raw)); err == nil {
		headers = msg.Header
	}
	tokens := ExtractTokensFromHeaders(headers)
	return append(tokens, ExtractTokensFromBody(raw)...)
}

// JunkMoveVerdict reports what moving or copying a message from mailbox
// from to mailbox to tells the classifier: filing it into Junk reports spam,
// taking it out of Junk (other than into Trash) reports ham. report is false
// for any other move.
func JunkMoveVerdict(from, to string) (spam, report bool) {
	fromJunk, toJunk := IsJunkMailbox(from), IsJunkMailbox(to)
	switch {
	case toJunk && !fromJunk:
		return true, true
	case fromJunk && !toJunk && !strings.EqualFold(to, "Trash"):
		return false, true
	}
	return false, false
}

// IsJunkMailbox matches the server's Junk folder and the "Spam" name some
// clients use for it.
func IsJunkMailbox(name string) bool {
	return strings.EqualFold(name, "Junk") || strings.EqualFold(name, "Spam")
}
